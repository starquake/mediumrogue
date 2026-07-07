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
	// ErrNotWalkable rejects water, rock, and off-map destinations.
	ErrNotWalkable = errors.New("target is not walkable")
	// ErrNoPath rejects a walkable destination with no route from the
	// entity's current hex (walled off by impassable terrain).
	ErrNoPath = errors.New("no path to target")
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
	// path is the remaining route (steps excluding the current hex), consumed
	// one hex per turn. Empty when the entity is idle.
	path []protocol.Hex
}

// World is the authoritative game state: the map, every entity, and each
// entity's queued walk path. One World per process; all access is serialized
// through its mutex (15 players — contention is not a concern, simplicity is).
type World struct {
	interval time.Duration
	ticks    *hub.Hub

	mu       sync.Mutex
	turn     int64
	terrain  map[protocol.Hex]protocol.Terrain
	worldMap protocol.MapResponse
	entities map[int64]*entity
	byToken  map[string]*entity
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

// SubmitIntent sets the entity's route to Target: any walkable, reachable
// hex. The server pathfinds from the entity's current position; the walk
// advances one hex per turn in resolveTurn. The latest submission in an input
// window replaces the entity's route.
func (w *World) SubmitIntent(req protocol.IntentRequest) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	e, ok := w.entities[req.EntityID]
	if !ok || e.token != req.Token {
		return ErrUnauthorized
	}

	if !w.walkableLocked(req.Target) {
		return ErrNotWalkable
	}

	path := Pathfind(e.hex, req.Target, w.walkableLocked)
	if path == nil {
		return ErrNoPath
	}

	e.path = path

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

	return protocol.TurnEvent{Turn: w.turn, IntervalMs: w.interval.Milliseconds(), Entities: entities}
}

// resolveTurn advances every entity one hex along its queued path, then bumps
// the turn number. Entities apply in ascending-ID order with a per-move
// occupancy re-check — a placeholder ordering until milestone 6 lands the real
// phased resolution (all moves simultaneously, seeded tie-break on overflow).
// A step onto a full hex is skipped this turn and retried next turn (the path
// is retained).
func (w *World) resolveTurn() {
	w.mu.Lock()
	defer w.mu.Unlock()

	ids := make([]int64, 0, len(w.entities))
	for id := range w.entities {
		ids = append(ids, id)
	}

	slices.Sort(ids)

	for _, id := range ids {
		e := w.entities[id]
		if len(e.path) == 0 {
			continue
		}

		next := e.path[0]
		if w.occupancyLocked(next) < protocol.StackCap {
			e.hex = next
			e.path = e.path[1:]
		}
	}

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
