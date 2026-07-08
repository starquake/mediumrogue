package game

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	mrand "math/rand/v2"
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
	kind  string
	hp    int
	maxHP int
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
	// seed is the world's tie-break RNG seed, minted once at construction. Each
	// turn's move-resolution shuffle uses a PCG seeded from (seed, turn) — the
	// turn selects the stream — so it's reproducible given the world + turn but
	// unpredictable to players (they don't know the world seed).
	seed int64
}

// NewWorld builds the world on the static map. Run must be started for turns
// to advance.
func NewWorld(interval time.Duration, ticks *hub.Hub) *World {
	worldMap := StaticMap()

	terrain := make(map[protocol.Hex]protocol.Terrain, len(worldMap.Tiles))
	for _, t := range worldMap.Tiles {
		terrain[t.Hex] = t.Terrain
	}

	var seedBuf [8]byte
	// A failed crypto read leaves a zero seed — still valid, just less random.
	_, _ = rand.Read(seedBuf[:])
	//nolint:gosec // a random world seed can be any 64-bit value; the sign is irrelevant.
	seed := int64(binary.BigEndian.Uint64(seedBuf[:]))

	return &World{
		interval: interval,
		ticks:    ticks,
		terrain:  terrain,
		worldMap: worldMap,
		entities: make(map[int64]*entity),
		byToken:  make(map[string]*entity),
		seed:     seed,
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
	e := &entity{
		id: w.nextID, hex: spawn, token: hex.EncodeToString(buf),
		kind: protocol.EntityPlayer, hp: protocol.PlayerMaxHP, maxHP: protocol.PlayerMaxHP,
	}
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
		entities = append(entities, protocol.Entity{
			ID: e.id, Hex: e.hex, Kind: e.kind, HP: e.hp, MaxHP: e.maxHP,
		})
	}

	slices.SortFunc(entities, func(a, b protocol.Entity) int { return int(a.ID - b.ID) })

	return protocol.TurnEvent{Turn: w.turn, IntervalMs: w.interval.Milliseconds(), Entities: entities}
}

// opposing reports whether a and b are of different factions (player vs
// monster). Same-faction entities stack; opposing ones can't share a hex.
func opposing(a, b *entity) bool { return a.kind != b.kind }

func hasOpposing(occs []*entity, m *entity) bool {
	for _, o := range occs {
		if opposing(o, m) {
			return true
		}
	}

	return false
}

func opposingOccupants(occs []*entity, m *entity) []*entity {
	var out []*entity

	for _, o := range occs {
		if opposing(o, m) {
			out = append(out, o)
		}
	}

	return out
}

// removeEntity drops m from an occupant slice (by identity).
func removeEntity(occs []*entity, m *entity) []*entity {
	for i, o := range occs {
		if o == m {
			return append(occs[:i], occs[i+1:]...)
		}
	}

	return occs
}

func attackDamage(e *entity) int {
	if e.kind == protocol.EntityMonster {
		return protocol.MonsterAttackDamage
	}

	return protocol.PlayerAttackDamage
}

// pendingBump is a move onto an opposing-held hex, re-checked post-move.
type pendingBump struct {
	m      *entity
	target protocol.Hex
}

// pendingAttack is a bump that is still opposing-held after the move phase
// completes, and therefore lands as an attack.
type pendingAttack struct {
	attacker *entity
	target   protocol.Hex
}

// resolveTurn runs one full turn of the decided phased resolution:
// think → move (faction-aware, with bump deferral) → attack (simultaneous,
// post-move positions) → apply damage & deaths. Under w.mu.
func (w *World) resolveTurn() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.thinkMonstersLocked()

	//nolint:gosec // deterministic per-turn combat RNG, not security-sensitive; reproducibility is required.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), uint64(w.turn)))

	// Evolving board: who is on each hex as moves resolve.
	byHex := make(map[protocol.Hex][]*entity, len(w.entities))
	for _, e := range w.entities {
		byHex[e.hex] = append(byHex[e.hex], e)
	}

	attacks := w.moveAndBumpLocked(rng, byHex)
	w.attackLocked(rng, byHex, attacks)

	w.resolveDeathsLocked()

	w.turn++
}

// moveAndBumpLocked resolves the move phase: movers advance one hex from
// their path in seeded-shuffled order, unless the destination is
// opposing-held (deferred as a bump) or the destination hex is at StackCap
// for a same-faction move (waits, path retained). Deferred bumps are
// re-checked once every other move has landed — a bump target that emptied
// out this turn (the defender retreated) completes as a normal move instead
// of an attack. Returns the bumps that are still opposing-held after that
// re-check, i.e. the attacks to resolve this turn. Callers hold w.mu.
func (w *World) moveAndBumpLocked(rng *mrand.Rand, byHex map[protocol.Hex][]*entity) []pendingAttack {
	movers := make([]*entity, 0, len(w.entities))
	for _, e := range w.entities {
		if len(e.path) > 0 {
			movers = append(movers, e)
		}
	}

	slices.SortFunc(movers, func(a, b *entity) int { return int(a.id - b.id) })
	rng.Shuffle(len(movers), func(i, j int) { movers[i], movers[j] = movers[j], movers[i] })

	var bumps []pendingBump

	for _, m := range movers {
		next := m.path[0]
		occs := byHex[next]

		switch {
		case hasOpposing(occs, m):
			bumps = append(bumps, pendingBump{m, next}) // stay; resolve after move phase
		case len(occs) < protocol.StackCap:
			byHex[m.hex] = removeEntity(byHex[m.hex], m)
			byHex[next] = append(byHex[next], m)
			m.hex = next
			m.path = m.path[1:]
		}
		// else: same-faction hex full → wait (path retained).
	}

	// Post-move bump re-check: still opposing-held → attack; vacated → complete
	// the move (retreat dodge / follow into the empty hex).
	var attacks []pendingAttack

	for _, b := range bumps {
		occs := byHex[b.target]

		switch {
		case hasOpposing(occs, b.m):
			attacks = append(attacks, pendingAttack{b.m, b.target})
		case len(occs) < protocol.StackCap:
			byHex[b.m.hex] = removeEntity(byHex[b.m.hex], b.m)
			byHex[b.target] = append(byHex[b.target], b.m)
			b.m.hex = b.target
			b.m.path = b.m.path[1:]
		}
	}

	return attacks
}

// attackLocked resolves the attack phase: each attack accumulates damage
// against pre-attack HP (nothing applied yet), so order is irrelevant and
// mutual kills work, then applies it all at once. A stacked defending hex
// picks its victim with rng, so a bump against a stack damages exactly one
// occupant. Callers hold w.mu.
func (w *World) attackLocked(rng *mrand.Rand, byHex map[protocol.Hex][]*entity, attacks []pendingAttack) {
	damage := make(map[int64]int)

	for _, a := range attacks {
		victims := opposingOccupants(byHex[a.target], a.attacker)
		if len(victims) == 0 {
			continue // guard; the re-check ensured at least one
		}

		// Canonical order first, like the movers shuffle above: byHex was
		// populated by ranging w.entities (a map), whose iteration order is
		// unspecified and varies per range — without this sort, victim choice
		// would depend on that incidental order instead of the seed alone.
		slices.SortFunc(victims, func(a, b *entity) int { return int(a.id - b.id) })

		victim := victims[rng.IntN(len(victims))]
		damage[victim.id] += attackDamage(a.attacker)
	}

	for id, dmg := range damage {
		w.entities[id].hp -= dmg
	}
}

// resolveDeathsLocked removes dead monsters and respawns dead players (full HP,
// fresh spawn hex, same id + token — the client stays joined). Callers hold w.mu.
func (w *World) resolveDeathsLocked() {
	var dead []*entity

	for _, e := range w.entities {
		if e.hp <= 0 {
			dead = append(dead, e)
		}
	}

	for _, e := range dead {
		if e.kind == protocol.EntityMonster {
			delete(w.entities, e.id)

			continue
		}

		// Player: respawn in place of a re-join.
		if spawn, err := w.spawnHexLocked(); err == nil {
			e.hex = spawn
		}

		e.hp = e.maxHP
		e.path = nil
	}
}

// spawnStream is a fixed PCG stream for monster placement, distinct from the
// per-turn move-shuffle stream (which uses the turn number).
const spawnStream uint64 = 0x5EED

// minPathWithApproachHex is the shortest Pathfind result (from, to] that
// contains a hex strictly before the destination: one to approach into, one
// that is the destination itself. A path shorter than this means the mover
// is already adjacent (or at) the destination.
const minPathWithApproachHex = 2

// SpawnMonsters adds n monster entities at random walkable hexes, chosen with
// the world seed so a given seed is reproducible. Skips hexes already at
// StackCap. Intended for **startup, before any player joins** (server startup
// via MONSTER_COUNT, or tests) — it does not avoid player-occupied hexes, so a
// later caller (continuous spawning, respawn) must add that guard.
func (w *World) SpawnMonsters(n int) {
	w.mu.Lock()
	defer w.mu.Unlock()

	walkable := make([]protocol.Hex, 0, len(w.worldMap.Tiles))
	for _, t := range w.worldMap.Tiles {
		if w.walkableLocked(t.Hex) {
			walkable = append(walkable, t.Hex)
		}
	}

	slices.SortFunc(walkable, func(a, b protocol.Hex) int {
		if a.Q != b.Q {
			return a.Q - b.Q
		}

		return a.R - b.R
	})

	//nolint:gosec // deterministic seeded placement, not security-sensitive.
	rng := mrand.New(mrand.NewPCG(uint64(w.seed), spawnStream))
	rng.Shuffle(len(walkable), func(i, j int) { walkable[i], walkable[j] = walkable[j], walkable[i] })

	placed := 0

	for _, h := range walkable {
		if placed >= n {
			break
		}

		if w.occupancyLocked(h) >= protocol.StackCap {
			continue
		}

		w.nextID++
		w.entities[w.nextID] = &entity{
			id: w.nextID, hex: h,
			kind: protocol.EntityMonster, hp: protocol.MonsterMaxHP, maxHP: protocol.MonsterMaxHP,
		}
		placed++
	}
}

// thinkMonstersLocked sets each monster's path to a single step toward its
// nearest player, stopping when adjacent — moving onto a player is an attack,
// which is milestone 6.3. Recomputed every turn (players move). Callers hold w.mu.
//
// Note for 6.3: this only prevents a monster from *stepping onto* a player. The
// move phase has no hostile-anti-stacking rule yet, so a monster and a player
// can still converge onto the same hex in one turn; 6.3's attack phase must
// handle a monster already co-located with a player at turn start.
func (w *World) thinkMonstersLocked() {
	players := make([]*entity, 0, len(w.entities))

	for _, e := range w.entities {
		if e.kind == protocol.EntityPlayer {
			players = append(players, e)
		}
	}

	if len(players) == 0 {
		return
	}

	for _, m := range w.entities {
		if m.kind != protocol.EntityMonster {
			continue
		}

		target := nearestPlayer(m.hex, players)
		path := Pathfind(m.hex, target.hex, w.walkableLocked)
		// Pathfind ends at the player's hex; only advance if there's an approach
		// hex before it. Adjacent or unreachable → hold this turn.
		if len(path) >= minPathWithApproachHex {
			m.path = []protocol.Hex{path[0]}
		} else {
			m.path = nil
		}
	}
}

// nearestPlayer returns the player closest to `from` by hex distance, ties
// broken by lowest id for determinism. `players` must be non-empty.
func nearestPlayer(from protocol.Hex, players []*entity) *entity {
	best := players[0]
	bestDist := HexDistance(from, best.hex)

	for _, p := range players[1:] {
		d := HexDistance(from, p.hex)
		if d < bestDist || (d == bestDist && p.id < best.id) {
			best, bestDist = p, d
		}
	}

	return best
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
