# Milestone 4 — Playback & Feel Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the bare turn loop into something that *feels* like the game — entities glide between hexes, a countdown bar shows the turn rhythm, and clicking a hex walks your entity there over successive turns — proven end-to-end by a Playwright walk-to-arrival test.

**Architecture:** The server stays a plain ticker; the 3s-input / 2s-playback split is a client-side presentation convention re-synced on every bundle (each `TurnEvent` now carries `intervalMs`). Movement becomes a *destination* intent: the server runs BFS pathfinding in `internal/game`, stores a per-entity path queue, and walks one hex per turn. The client tweens moves per-entity during the playback phase and drives a DOM countdown bar from a local phase clock.

**Tech Stack:** Go (server, `internal/game`/`internal/protocol`/`internal/server`), TypeScript + PixiJS (client), tygo (protocol codegen), Playwright (e2e).

## Global Constraints

- **Protocol is the single source of truth.** After editing `internal/protocol`, run `make protocol` and stage the regenerated `client/src/protocol.gen.ts`. Never hand-edit the generated file. The `make check` protocol-drift gate diffs it against git.
- **Game-rule constants live in `internal/protocol`** (shared with the client); timing knobs (`TURN_INTERVAL`) stay env vars so tests can shrink them.
- **`window.game` is a design-mandated test surface** — every client feature keeps it in sync (`GameDebug` in `client/src/main.ts`).
- **Tests land at the right layer**: Go unit tests next to code (`internal/game`), real-HTTP tests in `test/integration`, browser tests in `client/e2e`.
- **Go may not be on PATH.** Prefer `make test` / `make check`; for a single package use `PATH=$PATH:/usr/local/go/bin go test ...`. The Makefile already falls back to `/usr/local/go/bin/go`.
- **Move-resolution ordering stays the ascending-entity-ID placeholder** — milestone 6 replaces it with phased resolution. Do not implement phased resolution here.
- **Full gate before the milestone is done:** `make check` (lint, protocol drift, tsc, Go unit + integration, build) and `make e2e` both green.

---

## File Structure

**Server (Go):**
- `internal/protocol/protocol.go` — modify: `TurnEvent.IntervalMs`; redefine `IntentRequest.Target` doc.
- `internal/game/pathfind.go` — create: pure `Pathfind(from, to, walkable)` BFS.
- `internal/game/pathfind_test.go` — create: BFS unit tests (`package game_test`).
- `internal/game/world.go` — modify: `entity.path`; drop `intents` map + `ErrNotAdjacent`; add `ErrNoPath`; destination `SubmitIntent`; path-consuming `resolveTurn`; `Snapshot.IntervalMs`.
- `internal/game/world_test.go` — modify: fix `TestIntentValidation`; add walk + destination-replace tests.
- `internal/server/api.go` — modify: map `ErrNoPath` to 422, drop `ErrNotAdjacent`.
- `test/integration/turnloop_test.go` — modify/add: multi-step walk over SSE; `intervalMs` on the wire.

**Client (TS):**
- `client/src/render/hex.ts` — modify: add `pixelToHex`.
- `client/e2e/hex.spec.ts` — create: pure round-trip unit test (no browser).
- `client/src/render/entities.ts` — modify: per-entity sprites + playback tween.
- `client/src/ui/timer.ts` — create: DOM countdown bar driven by a phase clock.
- `client/index.html` — modify: add the timer bar element + CSS.
- `client/src/main.ts` — modify: click-to-move, unified keyboard, phase clock, `GameDebug` additions, `tapHex`.
- `client/e2e/walk.spec.ts` — create: click(tapHex) → multi-turn walk → arrival; timer bar present/animates.

**Docs:**
- `docs/STATUS.md` — modify: mark milestone 4 done, set next step.

---

## Task 1: Protocol — `intervalMs` on the wire + destination intent semantics

**Files:**
- Modify: `internal/protocol/protocol.go`
- Regenerate: `client/src/protocol.gen.ts` (via `make protocol`)

**Interfaces:**
- Produces: `protocol.TurnEvent.IntervalMs int64` (json `intervalMs`); `IntentRequest.Target` now means "destination (any walkable hex)".

- [ ] **Step 1: Add `IntervalMs` to `TurnEvent`**

In `internal/protocol/protocol.go`, change the `TurnEvent` struct to:

```go
// TurnEvent is the payload of an EventTurn frame: the world state after a
// resolved turn. A full entity snapshot every turn keeps clients trivially
// resyncable at this player count; deltas are a later optimization if ever
// needed. It will grow (attacks, deaths, chat) as the game develops.
type TurnEvent struct {
	// Turn is the monotonically increasing world-turn number.
	Turn int64 `json:"turn"`
	// IntervalMs is the runtime turn period in milliseconds (the configured
	// TURN_INTERVAL). The client cannot derive this — TURN_INTERVAL is
	// env-configurable while the cadence constants are fixed — so it rides
	// each bundle and the client re-syncs its playback/input phase clock on
	// every arrival.
	IntervalMs int64 `json:"intervalMs"`
	// Entities is every entity in the world, sorted by ID.
	Entities []Entity `json:"entities"`
}
```

- [ ] **Step 2: Redefine `IntentRequest.Target` as a destination**

Replace the `IntentRequest` doc comment (struct fields unchanged):

```go
// IntentRequest is the body of POST /api/intent: "walk to Target". Target is
// any walkable hex, not just a neighbor — the server pathfinds from the
// entity's current position and walks the route one hex per turn. A keyboard
// step is simply a Target one hex away. One intent per entity per turn; a
// later submission in the same input window replaces the earlier route.
type IntentRequest struct {
	EntityID int64  `json:"entityId"`
	Token    string `json:"token"`
	Target   Hex    `json:"target"`
}
```

- [ ] **Step 3: Regenerate the TypeScript protocol**

Run: `make protocol`
Expected: `client/src/protocol.gen.ts` regenerates with no error.

- [ ] **Step 4: Verify the generated field exists**

Run: `grep -n 'intervalMs' client/src/protocol.gen.ts`
Expected: a line like `intervalMs: number;` inside the `TurnEvent` interface.

- [ ] **Step 5: Verify Go still builds**

Run: `PATH=$PATH:/usr/local/go/bin go build ./...`
Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
git add internal/protocol/protocol.go client/src/protocol.gen.ts
git commit -m "protocol: add intervalMs to turn bundle; Target is now a destination"
```

---

## Task 2: Server — BFS pathfinding (pure function)

**Files:**
- Create: `internal/game/pathfind.go`
- Test: `internal/game/pathfind_test.go`

**Interfaces:**
- Consumes: `protocol.Hex`, `game.HexNeighbors` (existing).
- Produces: `func Pathfind(from, to protocol.Hex, walkable func(protocol.Hex) bool) []protocol.Hex` — ordered steps from `from` to `to`, **excluding** `from` and **including** `to`. Returns an empty (non-nil) slice when `from == to`. Returns `nil` when `to` is not walkable or unreachable.

- [ ] **Step 1: Write the failing tests**

Create `internal/game/pathfind_test.go`:

```go
package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// allWalkable is an open plane: every hex is walkable.
func allWalkable(protocol.Hex) bool { return true }

func TestPathfindStraightLine(t *testing.T) {
	t.Parallel()

	from := protocol.Hex{Q: 0, R: 0}
	to := protocol.Hex{Q: 3, R: 0}

	path := game.Pathfind(from, to, allWalkable)

	if len(path) != 3 {
		t.Fatalf("path length = %d, want 3 (%v)", len(path), path)
	}
	if path[len(path)-1] != to {
		t.Fatalf("path does not end at destination: %v", path)
	}

	// Every step is adjacent to the previous position.
	prev := from
	for _, step := range path {
		if game.HexDistance(prev, step) != 1 {
			t.Fatalf("non-adjacent step %v after %v", step, prev)
		}
		prev = step
	}
}

func TestPathfindFromEqualsTo(t *testing.T) {
	t.Parallel()

	h := protocol.Hex{Q: 1, R: -2}
	path := game.Pathfind(h, h, allWalkable)

	if path == nil || len(path) != 0 {
		t.Fatalf("from==to must return an empty non-nil path, got %v", path)
	}
}

func TestPathfindUnwalkableDestinationIsNil(t *testing.T) {
	t.Parallel()

	to := protocol.Hex{Q: 2, R: 0}
	walkable := func(h protocol.Hex) bool { return h != to }

	if path := game.Pathfind(protocol.Hex{}, to, walkable); path != nil {
		t.Fatalf("unwalkable destination must be nil, got %v", path)
	}
}

func TestPathfindRoutesAroundAWall(t *testing.T) {
	t.Parallel()

	// A vertical wall at q==1 for r in [-2..2], with one gap at (1,3) forcing
	// a detour south. from (0,0) to (2,0) cannot go straight through q==1.
	wall := map[protocol.Hex]bool{
		{Q: 1, R: -2}: true, {Q: 1, R: -1}: true, {Q: 1, R: 0}: true,
		{Q: 1, R: 1}: true, {Q: 1, R: 2}: true,
	}
	walkable := func(h protocol.Hex) bool { return !wall[h] }

	path := game.Pathfind(protocol.Hex{Q: 0, R: 0}, protocol.Hex{Q: 2, R: 0}, walkable)
	if path == nil {
		t.Fatal("expected a detour path, got nil")
	}
	for _, step := range path {
		if wall[step] {
			t.Fatalf("path walked through the wall at %v", step)
		}
	}
	if path[len(path)-1] != (protocol.Hex{Q: 2, R: 0}) {
		t.Fatalf("path does not reach destination: %v", path)
	}
}

func TestPathfindUnreachableIsNil(t *testing.T) {
	t.Parallel()

	// (0,0) is fully walled in by an impassable ring; any outside target is
	// unreachable.
	ring := map[protocol.Hex]bool{}
	for _, n := range game.HexNeighbors(protocol.Hex{Q: 0, R: 0}) {
		ring[n] = true
	}
	walkable := func(h protocol.Hex) bool { return !ring[h] }

	if path := game.Pathfind(protocol.Hex{Q: 0, R: 0}, protocol.Hex{Q: 5, R: 0}, walkable); path != nil {
		t.Fatalf("unreachable destination must be nil, got %v", path)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `PATH=$PATH:/usr/local/go/bin go test ./internal/game/ -run TestPathfind -v`
Expected: FAIL — `undefined: game.Pathfind`.

- [ ] **Step 3: Implement `Pathfind`**

Create `internal/game/pathfind.go`:

```go
package game

import "github.com/starquake/mediumrogue/internal/protocol"

// Pathfind returns the shortest walkable route from `from` to `to` on the
// flat-top hex grid, as the ordered list of steps excluding `from` and
// including `to`. Movement is uniform-cost, so breadth-first search yields a
// shortest path; the deterministic HexNeighbors order makes the result
// reproducible. Returns an empty (non-nil) slice when from == to, and nil
// when `to` is not walkable or is unreachable.
//
// The walkable predicate gates which hexes the search may enter, so callers
// decide the terrain rules (the World passes walkableLocked).
func Pathfind(from, to protocol.Hex, walkable func(protocol.Hex) bool) []protocol.Hex {
	if from == to {
		return []protocol.Hex{}
	}
	if !walkable(to) {
		return nil
	}

	cameFrom := map[protocol.Hex]protocol.Hex{from: from}
	queue := []protocol.Hex{from}

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur == to {
			return reconstruct(cameFrom, from, to)
		}

		for _, n := range HexNeighbors(cur) {
			if _, seen := cameFrom[n]; seen || !walkable(n) {
				continue
			}
			cameFrom[n] = cur
			queue = append(queue, n)
		}
	}

	return nil
}

// reconstruct walks the cameFrom chain from `to` back to `from`, then reverses
// it into forward order (excluding `from`).
func reconstruct(cameFrom map[protocol.Hex]protocol.Hex, from, to protocol.Hex) []protocol.Hex {
	var reversed []protocol.Hex
	for cur := to; cur != from; cur = cameFrom[cur] {
		reversed = append(reversed, cur)
	}

	path := make([]protocol.Hex, len(reversed))
	for i, h := range reversed {
		path[len(reversed)-1-i] = h
	}

	return path
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=$PATH:/usr/local/go/bin go test ./internal/game/ -run TestPathfind -v`
Expected: PASS (all five).

- [ ] **Step 5: Commit**

```bash
git add internal/game/pathfind.go internal/game/pathfind_test.go
git commit -m "game: BFS hex pathfinding (pure, terrain via predicate)"
```

---

## Task 3: Server — path queue, destination `SubmitIntent`, path-consuming `resolveTurn`

**Files:**
- Modify: `internal/game/world.go`
- Modify: `internal/game/world_test.go`
- Modify: `internal/server/api.go`

**Interfaces:**
- Consumes: `game.Pathfind` (Task 2); `protocol.TurnEvent.IntervalMs` (Task 1).
- Produces: `game.ErrNoPath` (destination unreachable); `ErrNotAdjacent` removed. `SubmitIntent` accepts any walkable, reachable destination and stores a per-entity path. `resolveTurn` advances each entity one hex along its path. `Snapshot` sets `IntervalMs`.

- [ ] **Step 1: Fix the existing `TestIntentValidation` and add walk tests (write them first — they will fail to compile / fail)**

In `internal/game/world_test.go`, replace the `"not adjacent"` case in `TestIntentValidation` — adjacency is no longer a rule. The `cases` slice becomes:

```go
	cases := []struct {
		name string
		req  protocol.IntentRequest
		want error
	}{
		{
			"bad token",
			protocol.IntentRequest{EntityID: me.EntityID, Token: "wrong", Target: walkableNeighbor(t, w, me.Hex)},
			game.ErrUnauthorized,
		},
		{
			"unknown entity",
			protocol.IntentRequest{EntityID: 999, Token: me.Token, Target: walkableNeighbor(t, w, me.Hex)},
			game.ErrUnauthorized,
		},
	}
```

Then add two new tests at the end of the file:

```go
func TestIntentWalksMultiStepPath(t *testing.T) {
	t.Parallel()

	w := newWorld()
	me, _ := w.Join("")

	// A destination two hexes away: a walkable neighbor of a walkable neighbor
	// that sits at distance 2 from spawn (geometry-independent discovery).
	n1 := walkableNeighbor(t, w, me.Hex)

	var dest protocol.Hex
	for _, n2 := range game.HexNeighbors(n1) {
		if n2 != me.Hex && game.HexDistance(me.Hex, n2) == 2 && isWalkable(w, n2) {
			dest = n2

			break
		}
	}
	if dest == (protocol.Hex{}) {
		t.Skip("spawn has no reachable distance-2 hex on this map")
	}

	if err := w.SubmitIntent(protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: dest}); err != nil {
		t.Fatalf("SubmitIntent: %v", err)
	}

	// One hex per turn: after the first turn the entity is adjacent to spawn,
	// not yet at the destination.
	snap := step(t, w)
	mid := snap.Entities[0].Hex
	if game.HexDistance(me.Hex, mid) != 1 {
		t.Fatalf("after turn 1: hex %v is not one step from spawn %v", mid, me.Hex)
	}
	if mid == dest {
		t.Fatal("reached a distance-2 destination in a single turn")
	}

	// The second turn arrives.
	snap = step(t, w)
	if got := snap.Entities[0].Hex; got != dest {
		t.Fatalf("after turn 2: hex = %v, want destination %v", got, dest)
	}
}

func TestSnapshotCarriesInterval(t *testing.T) {
	t.Parallel()

	w := game.NewWorld(250*time.Millisecond, hub.New())
	if got := w.Snapshot().IntervalMs; got != 250 {
		t.Fatalf("IntervalMs = %d, want 250", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `PATH=$PATH:/usr/local/go/bin go test ./internal/game/ -run 'TestIntent|TestSnapshotCarriesInterval' -v`
Expected: FAIL — `w.Snapshot().IntervalMs` undefined and the walk test fails (still single-step / adjacency-limited).

- [ ] **Step 3: Add `path` to the entity and `ErrNoPath`; drop `ErrNotAdjacent` and the `intents` map**

In `internal/game/world.go`:

Change the error block — remove `ErrNotAdjacent`, add `ErrNoPath`:

```go
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
```

Add `path` to the `entity` struct:

```go
type entity struct {
	id    int64
	hex   protocol.Hex
	token string
	// path is the remaining route (steps excluding the current hex), consumed
	// one hex per turn. Empty when the entity is idle.
	path []protocol.Hex
}
```

Remove the `intents map[int64]protocol.Hex` field from `World`, and delete its initialization in `NewWorld` (drop the `intents: make(...)` line).

- [ ] **Step 4: Rewrite `SubmitIntent` for destinations**

Replace the whole `SubmitIntent` method:

```go
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
```

- [ ] **Step 5: Rewrite `resolveTurn` to consume paths**

Replace the whole `resolveTurn` method:

```go
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
```

- [ ] **Step 6: Set `IntervalMs` in `Snapshot`**

In `Snapshot`, change the return to include the interval:

```go
	return protocol.TurnEvent{Turn: w.turn, IntervalMs: w.interval.Milliseconds(), Entities: entities}
```

- [ ] **Step 7: Update the API error mapping**

In `internal/server/api.go`, change the `handleIntent` switch so the 422 branch matches walkable/no-path (drop `ErrNotAdjacent`):

```go
		err := deps.World.SubmitIntent(req)
		switch {
		case errors.Is(err, game.ErrUnauthorized):
			respondError(w, deps.Logger, http.StatusUnauthorized, err.Error())
		case errors.Is(err, game.ErrNotWalkable), errors.Is(err, game.ErrNoPath):
			respondError(w, deps.Logger, http.StatusUnprocessableEntity, err.Error())
		case err != nil:
			deps.Logger.Error("submit intent", "err", err)
			respondError(w, deps.Logger, http.StatusInternalServerError, "internal error")
		default:
			w.WriteHeader(http.StatusAccepted)
		}
```

- [ ] **Step 8: Run the game package tests**

Run: `PATH=$PATH:/usr/local/go/bin go test ./internal/game/ -v`
Expected: PASS — including `TestIntentWalksMultiStepPath`, `TestSnapshotCarriesInterval`, and the still-passing `TestIntentMovesEntityOnResolve`, `TestLatestIntentWins`, `TestIntentRejectsUnwalkableTarget`.

- [ ] **Step 9: Build the server (confirms api.go compiles)**

Run: `PATH=$PATH:/usr/local/go/bin go build ./...`
Expected: no output, exit 0.

- [ ] **Step 10: Commit**

```bash
git add internal/game/world.go internal/game/world_test.go internal/server/api.go
git commit -m "game: server-side path queues — destination intents walk one hex/turn"
```

---

## Task 4: Integration — walk over SSE + intervalMs on the wire

**Files:**
- Modify: `test/integration/turnloop_test.go`

**Interfaces:**
- Consumes: existing `startServer`, `join`, `postJSON`, `get`, `readFrames`, `neighborsOf` helpers; `protocol.TurnEvent.IntervalMs`.

- [ ] **Step 1: Write the failing tests**

Append to `test/integration/turnloop_test.go`:

```go
// TestTurnLoopWalksToDistantHex proves server-side pathing over real HTTP: a
// single destination intent to a hex two steps away walks the entity there
// across successive turn bundles.
func TestTurnLoopWalksToDistantHex(t *testing.T) {
	t.Parallel()

	ts := startServer(t, 20*time.Millisecond, time.Hour)
	me := join(t, ts, "")

	var worldMap protocol.MapResponse
	if err := json.NewDecoder(get(t, ts, "/api/map").Body).Decode(&worldMap); err != nil {
		t.Fatalf("decode map: %v", err)
	}

	walkable := make(map[protocol.Hex]bool)
	for _, tile := range worldMap.Tiles {
		if tile.Terrain == protocol.TerrainGrass || tile.Terrain == protocol.TerrainForest {
			walkable[tile.Hex] = true
		}
	}

	// A reachable hex exactly two steps from spawn.
	dist := func(a, b protocol.Hex) int {
		dq, dr := a.Q-b.Q, a.R-b.R
		ds := -dq - dr
		abs := func(n int) int {
			if n < 0 {
				return -n
			}

			return n
		}

		return (abs(dq) + abs(dr) + abs(ds)) / 2
	}

	dest := protocol.Hex{}
	found := false
	for _, n1 := range neighborsOf(me.Hex) {
		if !walkable[n1] {
			continue
		}
		for _, n2 := range neighborsOf(n1) {
			if walkable[n2] && dist(me.Hex, n2) == 2 {
				dest, found = n2, true

				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatalf("spawn %v has no reachable distance-2 hex", me.Hex)
	}

	intent := protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: dest}
	if resp := postJSON(t, ts, "/api/intent", intent); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("intent status = %d, want 202", resp.StatusCode)
	}

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		frames := readFrames(t, reader, 1, 5*time.Second)

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		if bundle.IntervalMs != 20 {
			t.Fatalf("IntervalMs = %d, want 20", bundle.IntervalMs)
		}

		for _, e := range bundle.Entities {
			if e.ID == me.EntityID && e.Hex == dest {
				return // walked the full path
			}
		}
	}

	t.Fatal("entity never reached the distance-2 destination via the turn stream")
}
```

- [ ] **Step 2: Run to verify it fails, then passes with the Task 3 server**

Run: `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ -run TestTurnLoopWalksToDistantHex -v`
Expected: PASS (Task 3 already implemented the behavior; this locks the wire contract, including `intervalMs == 20`).

> If it fails on `IntervalMs`, re-check Task 3 Step 6.

- [ ] **Step 3: Run the whole integration suite**

Run: `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ -v`
Expected: PASS (existing `TestTurnLoopMovesEntity` still green — a 1-hex destination).

- [ ] **Step 4: Commit**

```bash
git add test/integration/turnloop_test.go
git commit -m "integration: multi-step walk over SSE; intervalMs on the wire"
```

---

## Task 5: Client — `pixelToHex` + round-trip test

**Files:**
- Modify: `client/src/render/hex.ts`
- Test: `client/e2e/hex.spec.ts` (a pure Playwright test — no `page`)

**Interfaces:**
- Consumes: existing `hexToPixel`, `HEX_SIZE`, `Point`.
- Produces: `export function pixelToHex(point: Point): Hex` — inverse of `hexToPixel` with cube rounding.

- [ ] **Step 1: Write the failing round-trip test**

Create `client/e2e/hex.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

import { hexToPixel, pixelToHex } from "../src/render/hex";

// hex.ts is pure math (only a type import), so this runs in-process with no
// browser — a unit test wearing a Playwright hat.
test("pixelToHex inverts hexToPixel for a spread of hexes", () => {
  for (let q = -6; q <= 6; q++) {
    for (let r = -6; r <= 6; r++) {
      const round = pixelToHex(hexToPixel({ q, r }));
      expect(round).toEqual({ q, r });
    }
  }
});

test("pixelToHex snaps a near-center point to the right hex", () => {
  const center = hexToPixel({ q: 2, r: -1 });
  // Nudge a few pixels off-center; still inside the hex.
  expect(pixelToHex({ x: center.x + 3, y: center.y - 3 })).toEqual({ q: 2, r: -1 });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd client && npx playwright test hex.spec.ts`
Expected: FAIL — `pixelToHex` is not exported.

- [ ] **Step 3: Implement `pixelToHex`**

In `client/src/render/hex.ts`, append:

```ts
/**
 * The hex containing a world-pixel point — the inverse of hexToPixel for the
 * flat-top layout, via fractional axial → cube rounding (Red Blob Games).
 * Used to turn a click into a destination hex; the server owns reachability.
 */
export function pixelToHex(point: Point): Hex {
  const qf = ((2 / 3) * point.x) / HEX_SIZE;
  const rf = (-point.x / 3 + (Math.sqrt(3) / 3) * point.y) / HEX_SIZE;

  return cubeRound(qf, rf);
}

/** Rounds fractional axial coordinates to the nearest hex (cube rounding). */
function cubeRound(qf: number, rf: number): Hex {
  const sf = -qf - rf;
  let q = Math.round(qf);
  let r = Math.round(rf);
  const s = Math.round(sf);

  const dq = Math.abs(q - qf);
  const dr = Math.abs(r - rf);
  const ds = Math.abs(s - sf);

  if (dq > dr && dq > ds) {
    q = -r - s;
  } else if (dr > ds) {
    r = -q - s;
  }

  return { q, r };
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd client && npx playwright test hex.spec.ts`
Expected: PASS (both tests).

- [ ] **Step 5: Typecheck**

Run: `cd client && npm run check`
Expected: no output, exit 0.

- [ ] **Step 6: Commit**

```bash
git add client/src/render/hex.ts client/e2e/hex.spec.ts
git commit -m "client: pixelToHex for click-picking, with round-trip test"
```

---

## Task 6: Client — per-entity sprites + playback tween

**Files:**
- Modify: `client/src/render/entities.ts`

**Interfaces:**
- Consumes: `hexToPixel`, `HEX_SIZE`, `Point`; `pixi.js` `Ticker`; `protocol.Entity`.
- Produces: `EntityLayer` constructed with a `Ticker`; `update(entities, myEntityID, playbackMs)` tweens each entity from its rendered position to the new hex over `playbackMs`.

- [ ] **Step 1: Rewrite `EntityLayer` with per-entity tweening**

Replace the whole body of `client/src/render/entities.ts`:

```ts
import { Container, Graphics, Text, type Ticker } from "pixi.js";

import type { Entity } from "../protocol.gen";
import { hexToPixel, HEX_SIZE, type Point } from "./hex";

const OTHER_COLOR = 0xc8b458;
const ME_COLOR = 0x8fd0ff;
const BADGE_STYLE = { fontFamily: "Courier New", fontSize: 13, fill: 0xe8f0e8 } as const;

interface Sprite {
  gfx: Graphics;
  badge: Text;
  from: Point;
  to: Point;
  current: Point;
  elapsed: number;
  duration: number;
  mine: boolean;
  count: number;
}

/**
 * The entity layer: one persistent Graphics per hex-stack, tweened between
 * turns. On each turn bundle we set every stack's tween target to its new
 * pixel position; the ticker interpolates over the playback window so moves
 * glide instead of snapping. The server snapshot is authoritative — a short or
 * dropped tween just means the sprite is already where the next bundle puts it.
 */
export class EntityLayer {
  readonly container = new Container();
  // Keyed by hex string "q,r": the entities standing there render as one
  // stack (top marker + count badge), matching the STACK_CAP rendering rule.
  private stacks = new Map<string, Sprite>();

  constructor(ticker: Ticker) {
    ticker.add(this.tick);
  }

  update(entities: Entity[], myEntityID: number, playbackMs: number): void {
    const byHex = new Map<string, Entity[]>();
    for (const e of entities) {
      const key = `${e.hex.q},${e.hex.r}`;
      byHex.set(key, [...(byHex.get(key) ?? []), e]);
    }

    // Retire stacks that no longer exist.
    for (const [key, sprite] of this.stacks) {
      if (!byHex.has(key)) {
        sprite.gfx.destroy();
        sprite.badge.destroy();
        this.stacks.delete(key);
      }
    }

    for (const [key, stack] of byHex) {
      const top = stack[0];
      if (top === undefined) {
        continue;
      }

      const to = hexToPixel(top.hex);
      const mine = stack.some((e) => e.id === myEntityID);
      let sprite = this.stacks.get(key);

      if (sprite === undefined) {
        const gfx = new Graphics();
        const badge = new Text({ text: "", style: BADGE_STYLE });
        this.container.addChild(gfx, badge);
        // New stack: appear in place (no tween).
        sprite = { gfx, badge, from: to, to, current: to, elapsed: 0, duration: 0, mine, count: stack.length };
        this.stacks.set(key, sprite);
      } else {
        sprite.from = sprite.current;
        sprite.to = to;
        sprite.elapsed = 0;
        sprite.duration = playbackMs;
        sprite.mine = mine;
        sprite.count = stack.length;
      }

      this.draw(sprite);
    }
  }

  private tick = (ticker: Ticker): void => {
    for (const sprite of this.stacks.values()) {
      if (sprite.current.x === sprite.to.x && sprite.current.y === sprite.to.y) {
        continue;
      }
      sprite.elapsed += ticker.deltaMS;
      const f = sprite.duration > 0 ? Math.min(1, sprite.elapsed / sprite.duration) : 1;
      sprite.current = {
        x: sprite.from.x + (sprite.to.x - sprite.from.x) * f,
        y: sprite.from.y + (sprite.to.y - sprite.from.y) * f,
      };
      this.draw(sprite);
    }
  };

  private draw(sprite: Sprite): void {
    const { x, y } = sprite.current;
    sprite.gfx
      .clear()
      .circle(x, y, HEX_SIZE * 0.45)
      .fill(sprite.mine ? ME_COLOR : OTHER_COLOR)
      .stroke({ width: 2, color: 0x0b0f0b });

    if (sprite.count > 1) {
      sprite.badge.text = `×${sprite.count}`;
      sprite.badge.position.set(x + HEX_SIZE * 0.3, y - HEX_SIZE * 0.9);
      sprite.badge.visible = true;
    } else {
      sprite.badge.visible = false;
    }
  }
}
```

- [ ] **Step 2: Typecheck (will fail — `main.ts` still calls the old signature)**

Run: `cd client && npm run check`
Expected: FAIL — `EntityLayer` constructor now needs a `Ticker`, and `update` needs `playbackMs`. This is wired up in Task 7; the failure is expected here.

> Do not commit yet — Task 7 restores a green typecheck. (This task's deliverable is verified together with Task 7.)

---

## Task 7: Client — click-to-move, unified keyboard, phase clock, `window.game`

**Files:**
- Modify: `client/src/main.ts`
- Modify: `client/src/input/keys.ts` (no logic change; confirm it still fits)

**Interfaces:**
- Consumes: `EntityLayer(ticker)` + `update(entities, myEntityID, playbackMs)` (Task 6); `pixelToHex` (Task 5); `submitIntent` (existing); `TurnSeconds`, `PlaybackSeconds` (protocol.gen).
- Produces: extended `GameDebug` — `intervalMs`, `phase`, `phaseRemainingMs`, `destination`, `tapHex(q, r)`.

- [ ] **Step 1: Extend `GameDebug` and wire everything in `main.ts`**

Replace `client/src/main.ts` with:

```ts
// Keeps Pixi off `new Function`, so the server's strict CSP (no unsafe-eval)
// holds. Must load before any other pixi.js import.
import "pixi.js/unsafe-eval";

import { Application, Container } from "pixi.js";

import { bindMovementKeys } from "./input/keys";
import { connectEvents } from "./net/events";
import { fetchMap } from "./net/map";
import { join, submitIntent } from "./net/session";
import type { Hex, TurnEvent } from "./protocol.gen";
import { PlaybackSeconds, TurnSeconds } from "./protocol.gen";
import { EntityLayer } from "./render/entities";
import { neighbor, pixelToHex } from "./render/hex";
import { buildMapLayer } from "./render/map";
import { TurnTimer } from "./ui/timer";

// window.game is the debug/testing surface: Playwright (and a curious human in
// devtools) reads live state through it. Testability is a design rule — every
// feature keeps this in sync. See §6 of the plan.
export interface GameDebug {
  turn: number;
  connected: boolean;
  /** Number of map tiles rendered; 0 until the map layer is on stage. */
  tiles: number;
  /** Entity count from the latest turn bundle. */
  entities: number;
  /** This client's entity, server-authoritative position. Null until joined. */
  me: { id: number; hex: Hex } | null;
  /** Runtime turn interval from the latest bundle, in ms. */
  intervalMs: number;
  /** Current turn phase: animating the last result, or awaiting input. */
  phase: "playback" | "input";
  /** Milliseconds left in the current phase. */
  phaseRemainingMs: number;
  /** The hex this client last asked to walk to; null once reached. */
  destination: Hex | null;
  /** Submit a destination as if the hex were clicked (drives e2e). */
  tapHex: (q: number, r: number) => void;
}

declare global {
  interface Window {
    game: GameDebug;
  }
}

function mustGet(id: string): HTMLElement {
  const el = document.getElementById(id);
  if (el === null) {
    throw new Error(`required element #${id} missing from index.html`);
  }

  return el;
}

const turnEl = mustGet("turn");
const statusEl = mustGet("status");

window.game = {
  turn: -1,
  connected: false,
  tiles: 0,
  entities: 0,
  me: null,
  intervalMs: 0,
  phase: "input",
  phaseRemainingMs: 0,
  destination: null,
  tapHex: () => {},
};

async function start(): Promise<void> {
  const app = new Application();
  await app.init({ background: "#0b0f0b", resizeTo: window, antialias: true });
  document.body.appendChild(app.canvas);

  const world = new Container();
  app.stage.addChild(world);

  const center = (): void => {
    world.position.set(app.screen.width / 2, app.screen.height / 2);
  };
  center();
  app.renderer.on("resize", center);

  const map = await fetchMap();
  world.addChild(buildMapLayer(map));
  window.game.tiles = map.tiles.length;

  const entityLayer = new EntityLayer(app.ticker);
  world.addChild(entityLayer.container);

  const timer = new TurnTimer(app.ticker, (phase, remainingMs) => {
    window.game.phase = phase;
    window.game.phaseRemainingMs = remainingMs;
  });

  const me = await join();
  window.game.me = { id: me.entityId, hex: me.hex };
  const identity = { entityId: me.entityId, token: me.token };

  // walkTo submits a destination and records it for the HUD/tests. The world's
  // answer (movement) only ever arrives via turn bundles.
  const walkTo = (target: Hex): void => {
    window.game.destination = target;
    void submitIntent(identity, target);
  };

  window.game.tapHex = (q, r): void => walkTo({ q, r });

  connectEvents({
    onTurn: (event: TurnEvent): void => {
      window.game.turn = event.turn;
      window.game.entities = event.entities.length;
      window.game.intervalMs = event.intervalMs;
      turnEl.textContent = String(event.turn);

      const playbackMs = event.intervalMs * (PlaybackSeconds / TurnSeconds);

      const mine = event.entities.find((e) => e.id === me.entityId);
      if (mine !== undefined && window.game.me !== null) {
        window.game.me.hex = mine.hex;
        // Arrived at the destination → clear it.
        if (
          window.game.destination !== null &&
          mine.hex.q === window.game.destination.q &&
          mine.hex.r === window.game.destination.r
        ) {
          window.game.destination = null;
        }
      }

      entityLayer.update(event.entities, me.entityId, playbackMs);
      timer.onTurn(event.intervalMs, playbackMs);
    },
    onConnectionChange: (connected: boolean): void => {
      window.game.connected = connected;
      statusEl.dataset["connected"] = String(connected);
      statusEl.textContent = connected ? "connected" : "reconnecting…";
    },
  });

  // Keyboard: a step is a one-hex destination — same code path as a click.
  bindMovementKeys({
    onStep: (dir): void => {
      const from = window.game.me?.hex;
      if (from === undefined) {
        return;
      }
      walkTo(neighbor(from, dir));
    },
  });

  // Click-to-move: canvas point → world point (undo the centering translate) →
  // hex → destination.
  app.canvas.addEventListener("pointerdown", (ev: PointerEvent): void => {
    const rect = app.canvas.getBoundingClientRect();
    const worldX = ev.clientX - rect.left - world.position.x;
    const worldY = ev.clientY - rect.top - world.position.y;
    walkTo(pixelToHex({ x: worldX, y: worldY }));
  });
}

start().catch((err: unknown) => {
  statusEl.textContent = `failed to start: ${String(err)}`;
});
```

- [ ] **Step 2: Typecheck (still fails — `TurnTimer` does not exist yet)**

Run: `cd client && npm run check`
Expected: FAIL — cannot find module `./ui/timer`. Fixed in Task 8.

> Task 8 completes the compile; this task and Tasks 6/8 verify and commit together at Task 8 Step 5.

---

## Task 8: Client — turn timer (DOM bar + phase clock)

**Files:**
- Create: `client/src/ui/timer.ts`
- Modify: `client/index.html`

**Interfaces:**
- Consumes: `pixi.js` `Ticker`.
- Produces: `class TurnTimer` — `new TurnTimer(ticker, onPhase)` and `onTurn(intervalMs, playbackMs)`. Drives `#turn-timer-fill` width and reports phase via the `onPhase(phase, remainingMs)` callback.

- [ ] **Step 1: Add the timer bar to `index.html`**

In `client/index.html`, add CSS inside `<style>` (after the `#status` rules):

```css
      #turn-timer {
        margin-top: 0.4rem;
        width: 8rem;
        height: 0.4rem;
        background: #1a241a;
        border: 1px solid var(--dim);
      }
      #turn-timer-fill {
        height: 100%;
        width: 0%;
        background: #7cb342;
      }
      #turn-timer[data-phase="playback"] #turn-timer-fill {
        background: var(--dim);
      }
```

And add the element inside `<header id="hud">`, after the `#status` div:

```html
      <div id="turn-timer" data-phase="input"><div id="turn-timer-fill"></div></div>
```

- [ ] **Step 2: Implement `TurnTimer`**

Create `client/src/ui/timer.ts`:

```ts
import type { Ticker } from "pixi.js";

type Phase = "playback" | "input";

/**
 * The turn timer: a DOM countdown bar (HTML-over-canvas, per plan §6). On each
 * turn bundle it restarts a local phase clock — a playback phase while the
 * result animates, then a draining input-window bar — re-synced every turn so
 * it can never drift from the server. Milestone 6 adds the combat-bubble
 * "waiting for: …" state; this is the single auto-advance state.
 */
export class TurnTimer {
  private readonly fill: HTMLElement;
  private readonly bar: HTMLElement;
  private elapsed = 0;
  private intervalMs = 0;
  private playbackMs = 0;

  constructor(
    ticker: Ticker,
    private readonly onPhase: (phase: Phase, remainingMs: number) => void,
  ) {
    this.bar = this.mustGet("turn-timer");
    this.fill = this.mustGet("turn-timer-fill");
    ticker.add(this.tick);
  }

  onTurn(intervalMs: number, playbackMs: number): void {
    this.intervalMs = intervalMs;
    this.playbackMs = playbackMs;
    this.elapsed = 0;
  }

  private tick = (ticker: Ticker): void => {
    if (this.intervalMs === 0) {
      return;
    }
    this.elapsed = Math.min(this.intervalMs, this.elapsed + ticker.deltaMS);

    const inPlayback = this.elapsed < this.playbackMs;
    const phase: Phase = inPlayback ? "playback" : "input";
    this.bar.dataset["phase"] = phase;

    if (inPlayback) {
      // Bar fills up while the move animates.
      const f = this.playbackMs > 0 ? this.elapsed / this.playbackMs : 1;
      this.fill.style.width = `${f * 100}%`;
      this.onPhase("playback", this.playbackMs - this.elapsed);
    } else {
      // Bar drains over the input window.
      const inputMs = this.intervalMs - this.playbackMs;
      const left = this.intervalMs - this.elapsed;
      const f = inputMs > 0 ? left / inputMs : 0;
      this.fill.style.width = `${f * 100}%`;
      this.onPhase("input", left);
    }
  };

  private mustGet(id: string): HTMLElement {
    const el = document.getElementById(id);
    if (el === null) {
      throw new Error(`required element #${id} missing from index.html`);
    }

    return el;
  }
}
```

- [ ] **Step 3: Typecheck the whole client (Tasks 6+7+8 now cohere)**

Run: `cd client && npm run check`
Expected: no output, exit 0.

- [ ] **Step 4: Build the client**

Run: `cd client && npm run build`
Expected: Vite build succeeds, writes `../internal/web/dist/...`.

- [ ] **Step 5: Commit Tasks 6–8 together (they share one compile unit)**

```bash
git add client/src/render/entities.ts client/src/main.ts client/src/ui/timer.ts client/index.html
git commit -m "client: playback tweens, click-to-move, unified keyboard, turn timer"
```

---

## Task 9: e2e — click-to-walk arrival + timer bar

**Files:**
- Create: `client/e2e/walk.spec.ts`

**Interfaces:**
- Consumes: `window.game` (`tapHex`, `me`, `destination`, `phase`); `#turn-timer` DOM. `TURN_INTERVAL=250ms` (playwright.config.ts).

- [ ] **Step 1: Write the e2e tests**

Create `client/e2e/walk.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

test("clicking (tapHex) a distant hex walks my entity there over turns", async ({ page }) => {
  await page.goto("/");

  await expect
    .poll(() => page.evaluate(() => window.game.me !== null && window.game.connected))
    .toBe(true);

  const start = await page.evaluate(() => window.game.me!.hex);

  // Pick a reachable destination two hexes north-ish of spawn. The server
  // rejects unwalkable/unreachable targets (no queue), so we try a couple of
  // candidates and keep whichever the server accepts (destination stays set
  // until arrival; a rejected target still sets destination but never clears
  // by movement — so assert on actual movement below).
  const dest = { q: start.q, r: start.r - 2 };
  await page.evaluate((d) => window.game.tapHex(d.q, d.r), dest);

  // The server-authoritative position reaches the destination over several
  // 250ms turns.
  await expect
    .poll(
      () =>
        page.evaluate(
          (d) => {
            const hex = window.game.me!.hex;

            return hex.q === d.q && hex.r === d.r;
          },
          dest,
        ),
      { timeout: 10_000 },
    )
    .toBe(true);

  // Arrival clears the pending destination.
  await expect.poll(() => page.evaluate(() => window.game.destination)).toBeNull();

  // It actually moved (guards against dest == start).
  const end = await page.evaluate(() => window.game.me!.hex);
  expect(end).not.toEqual(start);
});

test("the turn timer bar exists and animates across a turn", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("#turn-timer")).toBeVisible();

  await expect.poll(() => page.evaluate(() => window.game.intervalMs)).toBeGreaterThan(0);

  // The phase cycles through playback and input over successive turns.
  const sawPlayback = await page.evaluate(async () => {
    for (let i = 0; i < 200; i++) {
      if (window.game.phase === "playback") {
        return true;
      }
      await new Promise((res) => setTimeout(res, 10));
    }

    return false;
  });
  expect(sawPlayback).toBe(true);
});
```

> Note on the destination pick: if `{q, r-2}` is water/rock on the static map and the walk stalls, change `dest` to another offset the map allows (e.g. `{q: start.q + 2, r: start.r - 1}`) — spawn is near the grassy origin, so a distance-2 grass hex exists; pick one the server accepts. Keep the destination exactly two steps away so the multi-turn walk is exercised.

- [ ] **Step 2: Build the e2e binary and run the suite**

Run: `make e2e`
Expected: all specs PASS — `walk.spec.ts` (both tests), plus the unchanged `move.spec.ts`, `turn.spec.ts`, and the pure `hex.spec.ts`.

> `make e2e` rebuilds the client bundle, embeds it, builds the Go binary, and runs Playwright against it at `TURN_INTERVAL=250ms`. If `walk.spec.ts` stalls at the destination, adjust `dest` per the Step 1 note and re-run.

- [ ] **Step 3: Commit**

```bash
git add client/e2e/walk.spec.ts
git commit -m "e2e: click-to-walk arrival and turn-timer animation"
```

---

## Task 10: Milestone wrap — full gate + STATUS.md

**Files:**
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Run the full pre-commit gate**

Run: `make check`
Expected: lint 0 issues, protocol no-drift, `tsc` clean, Go unit + integration PASS, client + server build. All green.

- [ ] **Step 2: Run the e2e suite**

Run: `make e2e`
Expected: all Playwright specs PASS.

- [ ] **Step 3: Update `docs/STATUS.md`**

In `docs/STATUS.md`: add milestone 4 to the done table (with its commit range once known), update "What works right now" to mention playback tweens, click-to-move, the turn timer, and `intervalMs`; change the "Next" section to **milestone 5 — multiplayer polish + reconnect/`Last-Event-ID` replay proof + first conflict-resolution tests**. Move the now-resolved M4 items out of "Known placeholders", and note the still-open ones that this milestone deliberately deferred:

- Server-side input-window enforcement (acceptance stays permissive).
- Re-pathing around a route blocked mid-walk (a blocked step just waits).
- Multi-hex-per-turn travel out of danger.
- The combat-bubble "waiting for: …" timer state (milestone 6).

Set `Last updated` to the session date.

- [ ] **Step 4: Commit**

```bash
git add docs/STATUS.md
git commit -m "docs: milestone 4 done — playback & feel; next is milestone 5"
```

---

## Self-Review

**Spec coverage:**
- Client-derived phase timing + `intervalMs` per bundle → Tasks 1, 7 (phase clock), 8 (timer). ✔
- Permissive intent acceptance (no server cutoff) → unchanged server behavior; explicitly deferred in Task 10. ✔
- `IntentRequest.Target` destination semantics → Task 1. ✔
- BFS pathfinding in `internal/game` → Task 2. ✔
- Entity path queue, `SubmitIntent` destination, `ErrNoPath`, `resolveTurn`, `Snapshot.IntervalMs`, API mapping → Task 3. ✔
- Integration walk + `intervalMs` on wire + 422 → Task 4 (walk + interval); unwalkable-422 covered by existing `TestIntentRejectsBadToken` pattern and Task 3's `ErrNotWalkable`/`ErrNoPath` mapping. ✔
- `pixelToHex` + round-trip test → Task 5. ✔
- Per-entity playback tween → Task 6. ✔
- Click-to-move + unified keyboard + `window.game` (`intervalMs`, `phase`, `phaseRemainingMs`, `destination`, `tapHex`) → Task 7. ✔
- Turn timer DOM bar → Task 8. ✔
- e2e click→walk→arrival + timer animates → Task 9. ✔
- Out-of-scope items recorded → Task 10. ✔

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to Task N" — each code step carries full code. The only conditional guidance (e2e `dest` pick in Task 9) gives a concrete fallback value and the reason. ✔

**Type consistency:**
- `Pathfind(from, to, walkable) []protocol.Hex` — defined Task 2, consumed Task 3. ✔
- `EntityLayer(ticker)` + `update(entities, myEntityID, playbackMs)` — defined Task 6, consumed Task 7. ✔
- `TurnTimer(ticker, onPhase)` + `onTurn(intervalMs, playbackMs)` — defined Task 8, consumed Task 7. ✔
- `TurnEvent.IntervalMs`/`intervalMs` — Task 1, consumed Tasks 3, 4, 7. ✔
- `GameDebug` fields (`intervalMs`, `phase`, `phaseRemainingMs`, `destination`, `tapHex`) — Task 7, consumed Task 9. ✔
- `ErrNoPath` added / `ErrNotAdjacent` removed consistently across Task 3 (world.go) and Task 3 Step 7 (api.go). ✔

**Cross-task compile note:** Tasks 6–8 intentionally leave the client typecheck red between them (documented in each) and turn green at Task 8 Step 3, committing together at Task 8 Step 5. This keeps each *server* task independently green while treating the interlocking client change as one reviewable unit.
