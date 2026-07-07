package game

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// Intent validation errors, mapped to HTTP statuses by the API layer.
var (
	// ErrUnauthorized covers unknown entities and bad tokens alike, so a
	// caller cannot probe which entity IDs exist.
	ErrUnauthorized = errors.New("unknown entity or bad token")
	// ErrNotAdjacent rejects a step target that is not one of the entity's
	// six neighbor hexes.
	ErrNotAdjacent = errors.New("target is not adjacent")
	// ErrNotWalkable rejects water, rock, and off-map targets.
	ErrNotWalkable = errors.New("target is not walkable")
	// ErrWorldFull means no walkable hex has room for another entity — only
	// plausible if joins vastly outnumber the map's capacity.
	ErrWorldFull = errors.New("world is full: no walkable hex with room left")
)

// tokenBytes sizes the bearer token: 16 random bytes = 128 bits.
const tokenBytes = 16

// entity is the server-side entity record. The wire shape is
// protocol.Entity; the token never leaves this package except via Join.
type entity struct {
	id    int64
	hex   protocol.Hex
	token string
}

// World is the authoritative game state: the map, every entity, and the
// queued intents for the current input window. One World per process; all
// access is serialized through its mutex (15 players — contention is not a
// concern, simplicity is).
type World struct {
	interval time.Duration
	ticks    *hub.Hub

	mu       sync.Mutex
	turn     int64
	terrain  map[protocol.Hex]protocol.Terrain
	worldMap protocol.MapResponse
	entities map[int64]*entity
	byToken  map[string]*entity
	intents  map[int64]protocol.Hex
	nextID   int64
}

// NewWorld builds the world on the static map. Run must be started for turns
// to advance.
func NewWorld(interval time.Duration, ticks *hub.Hub) *World {
	worldMap := StaticMap()

	terrain := make(map[protocol.Hex]protocol.Terrain, len(worldMap.Tiles))
	for _, t := range worldMap.Tiles {
		terrain[t.Hex] = t.Terrain
	}

	return &World{
		interval: interval,
		ticks:    ticks,
		terrain:  terrain,
		worldMap: worldMap,
		entities: make(map[int64]*entity),
		byToken:  make(map[string]*entity),
		intents:  make(map[int64]protocol.Hex),
	}
}

// Map returns the immutable world map.
func (w *World) Map() protocol.MapResponse {
	return w.worldMap
}

// Run advances the world until ctx is canceled: one resolved turn per
// interval, each announced on the tick hub. Blocks; run in a goroutine.
func (w *World) Run(ctx context.Context) {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.resolveTurn()
			w.ticks.Publish()
		}
	}
}

// Join returns the entity for token, creating a new one (empty or unknown
// token) at a free spawn hex. An unknown token quietly becomes a new player
// rather than an error: the stored identity of a restarted server is gone,
// and the client's right move is always "then give me a fresh entity".
func (w *World) Join(token string) (protocol.JoinResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if e, ok := w.byToken[token]; ok && token != "" {
		return protocol.JoinResponse{EntityID: e.id, Token: e.token, Hex: e.hex}, nil
	}

	spawn, err := w.spawnHexLocked()
	if err != nil {
		return protocol.JoinResponse{}, err
	}

	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return protocol.JoinResponse{}, fmt.Errorf("generate token: %w", err)
	}

	w.nextID++
	e := &entity{id: w.nextID, hex: spawn, token: hex.EncodeToString(buf)}
	w.entities[e.id] = e
	w.byToken[e.token] = e

	return protocol.JoinResponse{EntityID: e.id, Token: e.token, Hex: e.hex}, nil
}

// SubmitIntent queues "step to target" for the next turn. Validation runs
// against the entity's current position; the latest submission in an input
// window wins.
func (w *World) SubmitIntent(req protocol.IntentRequest) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.entities[req.EntityID]
	if !ok || e.token != req.Token {
		return ErrUnauthorized
	}

	if HexDistance(e.hex, req.Target) != 1 {
		return ErrNotAdjacent
	}

	if !w.walkableLocked(req.Target) {
		return ErrNotWalkable
	}

	w.intents[e.id] = req.Target

	return nil
}

// Snapshot is the current turn bundle: turn number plus every entity,
// sorted by ID for a deterministic wire shape.
func (w *World) Snapshot() protocol.TurnEvent {
	w.mu.Lock()
	defer w.mu.Unlock()

	entities := make([]protocol.Entity, 0, len(w.entities))
	for _, e := range w.entities {
		entities = append(entities, protocol.Entity{ID: e.id, Hex: e.hex})
	}

	slices.SortFunc(entities, func(a, b protocol.Entity) int { return int(a.ID - b.ID) })

	return protocol.TurnEvent{Turn: w.turn, Entities: entities}
}

// resolveTurn applies the input window's intents and advances the turn.
// Moves apply in ascending entity-ID order with an occupancy re-check per
// move — a placeholder ordering until milestone 6 lands the real phased
// resolution (all moves simultaneously, seeded tie-break on overflow).
func (w *World) resolveTurn() {
	w.mu.Lock()
	defer w.mu.Unlock()

	ids := make([]int64, 0, len(w.intents))
	for id := range w.intents {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	for _, id := range ids {
		target := w.intents[id]

		e, ok := w.entities[id]
		if !ok {
			continue
		}

		if w.occupancyLocked(target) < protocol.StackCap {
			e.hex = target
		}
	}

	clear(w.intents)
	w.turn++
}

// spawnHexLocked finds the free walkable hex nearest the origin, spiraling
// outward. Callers hold w.mu.
func (w *World) spawnHexLocked() (protocol.Hex, error) {
	origin := protocol.Hex{Q: 0, R: 0}

	for radius := 0; radius <= MapRadius; radius++ {
		for q := -radius; q <= radius; q++ {
			for r := -radius; r <= radius; r++ {
				h := protocol.Hex{Q: q, R: r}
				if HexDistance(origin, h) != radius {
					continue
				}

				if w.walkableLocked(h) && w.occupancyLocked(h) < protocol.StackCap {
					return h, nil
				}
			}
		}
	}

	return protocol.Hex{}, ErrWorldFull
}

func (w *World) walkableLocked(h protocol.Hex) bool {
	t, ok := w.terrain[h]

	return ok && (t == protocol.TerrainGrass || t == protocol.TerrainForest)
}

func (w *World) occupancyLocked(h protocol.Hex) int {
	n := 0

	for _, e := range w.entities {
		if e.hex == h {
			n++
		}
	}

	return n
}
