# Milestone 5 — Multiplayer & Reconnect Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove the game is genuinely multiplayer and survives a dropped connection — two clients in one world resolving intents simultaneously and seeing each other, a client reconnecting and resyncing to the latest turn, and a first test locking the observable conflict-resolution behaviour (friendly stacking + `StackCap`).

**Architecture:** Resync-to-latest (no replay buffer): full-snapshot bundles + a coalescing hub mean a reconnecting client only needs the current snapshot, which the SSE handler already sends. The only server change is honouring `Last-Event-ID` as a watermark so a reconnect doesn't re-paint an already-seen turn. Everything else is verification.

**Tech Stack:** Go (server, `internal/server`/`internal/game`), TypeScript + PixiJS (client), Playwright (e2e, incl. multi-context + `setOffline`).

## Global Constraints

- **Resync-to-latest** — no replay buffer, no resync endpoint. This supersedes the early replay-buffer language in `docs/roguelike-mp-plan.md` §4.
- **Conflict resolution stays the ascending-entity-ID placeholder** — tests assert only observable, milestone-6-stable outcomes (stacking works; `StackCap` respected). Do NOT lock the exact overflow tie-break winner and do NOT implement phased/seeded resolution (milestone 6).
- **`window.game` is a design-mandated test surface** — additions keep the interface and initializer in sync (`GameDebug` in `client/src/main.ts`).
- **Tests at the right layer** — Go unit next to code (`internal/game`, black-box `package game_test`), real-HTTP tests in `test/integration`, browser tests in `client/e2e`.
- **Go may not be on PATH** — prefer `make`; for one package `PATH=$PATH:/usr/local/go/bin go test ...`. **Run `make lint` and fix any `wsl_v5` whitespace findings before committing Go changes** (per-task gate, not just the final `make check`).
- **A malformed `Last-Event-ID` must never break the stream** — default to `-1` (fresh-connect behaviour).
- **Full gate before the milestone is done:** `make check` and `make e2e` both green.
- **Delivery:** all work lands on branch `milestone-5-multiplayer-reconnect` and ships as one PR (do not push to `main`).

---

## File Structure

**Server (Go):**
- `internal/server/events.go` — modify: seed `lastSent` from `Last-Event-ID` via a new `parseLastEventID` helper.
- `test/integration/events_test.go` — modify: add `getWithLastEventID` + `readFrameWithin` helpers and the `Last-Event-ID` watermark tests.
- `test/integration/multiplayer_test.go` — create: simultaneous-resolution-over-SSE test + its small helpers.
- `internal/game/export_test.go` — modify: add `PlaceEntityForTest`.
- `internal/game/conflict_test.go` — create: friendly-stacking + `StackCap`-overflow tests.

**Client (TS):**
- `client/src/main.ts` — modify: add `positions` to `GameDebug`, populate from each bundle.
- `client/e2e/multiplayer.spec.ts` — create: multi-client visibility + pulled-plug reconnect.

**Docs:**
- `docs/roguelike-mp-plan.md` — modify: annotate §4 (replay buffer superseded) and tick the §9 decision.
- `docs/STATUS.md` — modify: milestone 5 done; next → milestone 6.

---

## Task 1: Server — honour `Last-Event-ID` (watermark) + integration tests

**Files:**
- Modify: `internal/server/events.go`
- Modify: `test/integration/events_test.go`

**Interfaces:**
- Produces: `handleEvents` seeds its `lastSent` watermark from the `Last-Event-ID` header (default `-1`); helper `parseLastEventID(r *http.Request) int64`.

- [ ] **Step 1: Write the failing integration tests**

Add to `test/integration/events_test.go` (the imports `bufio`, `net/http`, `net/http/httptest`, `strings`, `testing`, `time` are already present):

```go
// getWithLastEventID issues a GET with the SSE reconnection header set, the way
// the browser's EventSource does automatically on reconnect.
func getWithLastEventID(t *testing.T, ts *httptest.Server, path, lastEventID string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}

	req.Header.Set("Last-Event-ID", lastEventID)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	return resp
}

// readFrameWithin reads one SSE data frame, returning ok=false if none arrives
// before timeout — used to assert a frame is deliberately withheld.
func readFrameWithin(t *testing.T, r *bufio.Reader, timeout time.Duration) (sseFrame, bool) {
	t.Helper()

	type result struct {
		frame sseFrame
		ok    bool
	}

	ch := make(chan result, 1)

	go func() {
		var cur sseFrame

		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ch <- result{}

				return
			}

			line = strings.TrimRight(line, "\n")
			switch {
			case line == "":
				if cur.event != "" || cur.data != "" {
					ch <- result{cur, true}

					return
				}

				cur = sseFrame{}
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	select {
	case res := <-ch:
		return res.frame, res.ok
	case <-time.After(timeout):
		return sseFrame{}, false
	}
}

// TestLastEventIDWithholdsAlreadySeenTurn: with a frozen clock the world stays
// at turn 0, so a client reconnecting with Last-Event-ID: 0 must NOT be
// re-sent turn 0 — it already has it.
func TestLastEventIDWithholdsAlreadySeenTurn(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour) // frozen clock + heartbeat

	fresh := get(t, ts, "/api/events")
	frames := readFrames(t, bufio.NewReader(fresh.Body), 1, 5*time.Second)
	if frames[0].id != "0" {
		t.Fatalf("first frame id = %q, want 0", frames[0].id)
	}

	reconnect := getWithLastEventID(t, ts, "/api/events", "0")
	if _, ok := readFrameWithin(t, bufio.NewReader(reconnect.Body), 300*time.Millisecond); ok {
		t.Fatal("server re-sent an already-seen turn to a reconnecting client")
	}
}

// TestLastEventIDSendsCurrentWhenMismatched: a Last-Event-ID that does not match
// the current turn (ahead, as after a server restart, or garbage) still yields
// the current snapshot — the watermark must not over-withhold.
func TestLastEventIDSendsCurrentWhenMismatched(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	for _, id := range []string{"999", "not-a-number"} {
		resp := getWithLastEventID(t, ts, "/api/events", id)

		frame, ok := readFrameWithin(t, bufio.NewReader(resp.Body), 5*time.Second)
		if !ok {
			t.Fatalf("Last-Event-ID %q: no snapshot delivered", id)
		}

		if frame.id != "0" {
			t.Fatalf("Last-Event-ID %q: frame id = %q, want current turn 0", id, frame.id)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify the withhold test fails**

Run: `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ -run 'TestLastEventID' -v`
Expected: `TestLastEventIDWithholdsAlreadySeenTurn` FAILS ("server re-sent an already-seen turn") — the current handler always seeds `lastSent = -1`. `TestLastEventIDSendsCurrentWhenMismatched` passes (current behaviour already sends).

- [ ] **Step 3: Implement the watermark in `events.go`**

Add `"strconv"` to the imports. Replace the immediate-send seed:

```go
		ticks, unsubscribe := deps.Ticks.Subscribe()
		defer unsubscribe()

		// Seed the watermark from Last-Event-ID so a reconnecting client is not
		// re-sent a turn it already has. A fresh client (no header) or a
		// malformed value defaults to -1 → current snapshot sent immediately.
		lastSent := parseLastEventID(r)
		lastSent = writeTurn(w, deps, flusher, lastSent)
```

And add the helper (below `handleEvents`, above `writeTurn`):

```go
// parseLastEventID reads the SSE reconnection header the browser's EventSource
// sends automatically: the turn number the client last saw, used as the initial
// "already sent" watermark. Missing or malformed values yield -1, which makes
// the handler behave like a fresh connection (send the current snapshot).
func parseLastEventID(r *http.Request) int64 {
	id, err := strconv.ParseInt(r.Header.Get("Last-Event-ID"), 10, 64)
	if err != nil {
		return -1
	}

	return id
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ -run 'TestLastEventID' -v`
Expected: both PASS.

- [ ] **Step 5: Run the whole integration suite + lint**

Run: `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ && make lint`
Expected: all integration tests pass; lint 0 issues (fix any `wsl_v5` whitespace in the new code first).

- [ ] **Step 6: Commit**

```bash
git add internal/server/events.go test/integration/events_test.go
git commit -m "server: honor Last-Event-ID as a resync watermark (resync-to-latest)"
```

---

## Task 2: Integration — simultaneous multiplayer resolution over SSE

**Files:**
- Create: `test/integration/multiplayer_test.go`

**Interfaces:**
- Consumes: existing `startServer`, `join`, `postJSON`, `get`, `readFrames`, `neighborsOf` helpers; `protocol` types.

- [ ] **Step 1: Write the test**

Create `test/integration/multiplayer_test.go`:

```go
package integration_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestSimultaneousResolutionMovesBothEntities: two joined clients each submit a
// step, and one turn bundle carries BOTH entities at their new positions —
// proving intents from multiple clients resolve together in a single turn.
func TestSimultaneousResolutionMovesBothEntities(t *testing.T) {
	t.Parallel()

	ts := startServer(t, 20*time.Millisecond, time.Hour)

	a := join(t, ts, "")
	b := join(t, ts, "")
	if a.EntityID == b.EntityID {
		t.Fatal("two joins must yield distinct entities")
	}

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

	targetA := walkableNeighborOf(t, a.Hex, walkable)
	targetB := walkableNeighborOf(t, b.Hex, walkable)

	postIntent(t, ts, a, targetA)
	postIntent(t, ts, b, targetB)

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)
	deadline := time.Now().Add(5 * time.Second)

	for time.Now().Before(deadline) {
		frames := readFrames(t, reader, 1, 5*time.Second)

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		if hexOf(bundle, a.EntityID) == targetA && hexOf(bundle, b.EntityID) == targetB {
			return // both moved in one bundle
		}
	}

	t.Fatal("both entities never reached their targets in one bundle")
}

func postIntent(t *testing.T, ts *httptest.Server, me protocol.JoinResponse, target protocol.Hex) {
	t.Helper()

	intent := protocol.IntentRequest{EntityID: me.EntityID, Token: me.Token, Target: target}
	if resp := postJSON(t, ts, "/api/intent", intent); resp.StatusCode != http.StatusAccepted {
		t.Fatalf("intent status = %d, want 202", resp.StatusCode)
	}
}

func hexOf(bundle protocol.TurnEvent, id int64) protocol.Hex {
	for _, e := range bundle.Entities {
		if e.ID == id {
			return e.Hex
		}
	}

	return protocol.Hex{Q: -999, R: -999}
}

func walkableNeighborOf(t *testing.T, from protocol.Hex, walkable map[protocol.Hex]bool) protocol.Hex {
	t.Helper()

	for _, n := range neighborsOf(from) {
		if walkable[n] {
			return n
		}
	}

	t.Fatalf("no walkable neighbor of %v", from)

	return protocol.Hex{}
}
```

Note: `httptest` must be imported (used in `postIntent`). Add `"net/http/httptest"` to the import block.

- [ ] **Step 2: Run it**

Run: `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ -run TestSimultaneousResolutionMovesBothEntities -v`
Expected: PASS (the behaviour already exists; this locks the multiplayer wire contract).

- [ ] **Step 3: Full integration suite + lint**

Run: `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ && make lint`
Expected: all pass; lint 0 issues.

- [ ] **Step 4: Commit**

```bash
git add test/integration/multiplayer_test.go
git commit -m "integration: two clients' intents resolve in one turn bundle"
```

---

## Task 3: Game — conflict-resolution tests + `PlaceEntityForTest` bridge

**Files:**
- Modify: `internal/game/export_test.go`
- Create: `internal/game/conflict_test.go`

**Interfaces:**
- Produces: `(*World).PlaceEntityForTest(hex protocol.Hex) (int64, string)` — injects an entity at a hex, returns its id + bearer token.

- [ ] **Step 1: Add the `PlaceEntityForTest` bridge**

Replace `internal/game/export_test.go` with:

```go
package game

import (
	"fmt"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// ResolveTurnForTest drives one turn resolution synchronously, so tests can
// step the world without running the ticker goroutine.
func (w *World) ResolveTurnForTest() {
	w.resolveTurn()
}

// PlaceEntityForTest injects an entity at a specific hex and returns its id and
// bearer token, so conflict tests can build exact board states instead of
// depending on spawn geometry.
func (w *World) PlaceEntityForTest(hex protocol.Hex) (int64, string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.nextID++
	token := fmt.Sprintf("test-token-%d", w.nextID)
	e := &entity{id: w.nextID, hex: hex, token: token}
	w.entities[e.id] = e
	w.byToken[token] = e

	return e.id, token
}
```

- [ ] **Step 2: Write the conflict tests**

Create `internal/game/conflict_test.go`:

```go
package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestFriendlyStackingConverges: two entities on neighbouring hexes both step
// onto one shared hex in a single turn and stack — friendly stacking under the
// StackCap, resolved simultaneously.
func TestFriendlyStackingConverges(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	idA, tokA := w.PlaceEntityForTest(ns[0])
	idB, tokB := w.PlaceEntityForTest(ns[1])

	mustSubmit(t, w, idA, tokA, center)
	mustSubmit(t, w, idB, tokB, center)

	w.ResolveTurnForTest()

	snap := w.Snapshot()
	if got := hexOfSnap(snap, idA); got != center {
		t.Errorf("A at %v, want center %v", got, center)
	}

	if got := hexOfSnap(snap, idB); got != center {
		t.Errorf("B at %v, want center %v", got, center)
	}

	if n := countAt(snap, center); n != 2 {
		t.Errorf("center occupancy = %d, want 2", n)
	}
}

// TestStackCapBlocksOverflow: a hex already full at StackCap does not admit one
// more mover — the overflow entity stays put and the hex still holds exactly
// StackCap. Asserts the invariant, not which entity won a tie-break.
func TestStackCapBlocksOverflow(t *testing.T) {
	t.Parallel()

	w := newWorld()

	center := protocol.Hex{Q: 0, R: 0}
	if !isWalkable(w, center) {
		t.Skip("origin is not walkable on this map")
	}

	ns := game.HexNeighbors(center)

	for range protocol.StackCap {
		w.PlaceEntityForTest(center)
	}

	sixth, tok := w.PlaceEntityForTest(ns[0])
	mustSubmit(t, w, sixth, tok, center)

	w.ResolveTurnForTest()

	snap := w.Snapshot()
	if got := hexOfSnap(snap, sixth); got == center {
		t.Errorf("overflow entity entered a full hex; want it blocked at %v", ns[0])
	}

	if n := countAt(snap, center); n != protocol.StackCap {
		t.Errorf("center occupancy = %d, want StackCap %d", n, protocol.StackCap)
	}
}

func mustSubmit(t *testing.T, w *game.World, id int64, token string, target protocol.Hex) {
	t.Helper()

	if err := w.SubmitIntent(protocol.IntentRequest{EntityID: id, Token: token, Target: target}); err != nil {
		t.Fatalf("SubmitIntent(%d -> %v): %v", id, target, err)
	}
}

func hexOfSnap(snap protocol.TurnEvent, id int64) protocol.Hex {
	for _, e := range snap.Entities {
		if e.ID == id {
			return e.Hex
		}
	}

	return protocol.Hex{Q: -999, R: -999}
}

func countAt(snap protocol.TurnEvent, hex protocol.Hex) int {
	n := 0

	for _, e := range snap.Entities {
		if e.Hex == hex {
			n++
		}
	}

	return n
}
```

Note: `newWorld` and `isWalkable` already exist in `world_test.go` (same `game_test` package). `for range protocol.StackCap` is the Go 1.22+ integer-range form (module is `go 1.26`).

- [ ] **Step 3: Run the conflict tests to verify they pass**

Run: `PATH=$PATH:/usr/local/go/bin go test ./internal/game/ -run 'TestFriendlyStacking|TestStackCapBlocksOverflow' -v`
Expected: both PASS.

- [ ] **Step 4: Full game package + lint**

Run: `PATH=$PATH:/usr/local/go/bin go test ./internal/game/ && make lint`
Expected: all game tests pass; lint 0 issues.

- [ ] **Step 5: Commit**

```bash
git add internal/game/export_test.go internal/game/conflict_test.go
git commit -m "game: first conflict-resolution tests — friendly stacking, StackCap overflow"
```

---

## Task 4: Client — positions + SSE liveness watchdog + multi-client & reconnect e2e

**Files:**
- Modify: `client/src/main.ts`
- Modify: `client/src/net/events.ts` (SSE liveness watchdog — added 2026-07-08)
- Create: `client/e2e/multiplayer.spec.ts`

**Interfaces:**
- Consumes: existing `window.game` fields (`me`, `entities`, `turn`, `connected`, `tapHex`).
- Produces: `GameDebug.positions: { id: number; hex: Hex }[]`; `connectEvents` returns a teardown `() => void` (was `EventSource`; `main.ts` ignores the return either way).

> **Scope note (2026-07-08):** the reconnect e2e as first written could not sever an open SSE stream via `context.setOffline` in the sandbox, so `window.game.connected` never flipped. Resolution (design decision C): add a real **SSE liveness watchdog** to `events.ts` — no data within a turn-scaled window ⇒ report disconnected + reconnect. That both fixes the test and closes a genuine half-open-connection gap. See the spec's "SSE liveness watchdog" section, including the milestone-6 caveat (turn-based liveness must become heartbeat-based once combat bubbles freeze the clock).

- [ ] **Step 1: Add `positions` to `GameDebug`**

In `client/src/main.ts`, add to the `GameDebug` interface (after `entities`):

```ts
  /** Every entity in the latest bundle, for cross-client observation in tests. */
  positions: { id: number; hex: Hex }[];
```

Add to the `window.game = { ... }` initializer (after `entities: 0,`):

```ts
  positions: [],
```

And populate it in the `onTurn` handler, alongside the existing `window.game.entities` assignment:

```ts
      window.game.entities = event.entities.length;
      window.game.positions = event.entities.map((e) => ({ id: e.id, hex: e.hex }));
```

- [ ] **Step 2: Implement the SSE liveness watchdog**

Replace `client/src/net/events.ts` with:

```ts
// SSE world stream with a liveness watchdog. EventSource auto-reconnects on
// errors it *detects*, but a silently half-open connection (network away, socket
// not reset) can leave the stream stalled without ever firing `error` — the
// client would keep believing it is connected. The watchdog treats "no data
// within a turn-scaled window" as a dead stream: it reports disconnected and
// opens a fresh EventSource, which resyncs to the latest snapshot on reconnect.
import { EventTurn, type TurnEvent } from "../protocol.gen";

export interface EventsCallbacks {
  onTurn: (turn: TurnEvent) => void;
  onConnectionChange: (connected: boolean) => void;
}

// Liveness window: a stream is dead after this long with no data. Turn-scaled so
// a slow production cadence (5s turns → 20s window) is never mistaken for a
// drop; floored for the pre-first-bundle window.
const LIVENESS_FLOOR_MS = 3_000;
const LIVENESS_TURNS = 4;

function livenessWindow(intervalMs: number): number {
  return Math.max(LIVENESS_FLOOR_MS, intervalMs * LIVENESS_TURNS);
}

/**
 * Opens the world stream. Returns a teardown that stops the watchdog and closes
 * the stream. EventSource handles Last-Event-ID on its own auto-retry; a
 * watchdog-driven reconnect is a fresh connection that resyncs to latest.
 */
export function connectEvents(callbacks: EventsCallbacks): () => void {
  let source: EventSource;
  let watchdog: ReturnType<typeof setTimeout> | undefined;
  let windowMs = LIVENESS_FLOOR_MS;
  let torndown = false;

  const arm = (): void => {
    if (watchdog !== undefined) {
      clearTimeout(watchdog);
    }

    watchdog = setTimeout(() => {
      // No data in the window: the stream is dead even if EventSource has not
      // noticed. Report disconnected and reconnect from scratch.
      callbacks.onConnectionChange(false);
      source.close();
      if (!torndown) {
        connect();
      }
    }, windowMs);
  };

  const connect = (): void => {
    source = new EventSource("/api/events");

    source.addEventListener(EventTurn, (event: MessageEvent<string>) => {
      const turn = JSON.parse(event.data) as TurnEvent;
      windowMs = livenessWindow(turn.intervalMs);
      callbacks.onConnectionChange(true);
      arm();
      callbacks.onTurn(turn);
    });

    source.addEventListener("open", () => {
      callbacks.onConnectionChange(true);
      arm();
    });

    // EventSource retries on its own after an error it detects; just report the
    // state. The watchdog covers the errors it never detects.
    source.addEventListener("error", () => callbacks.onConnectionChange(false));

    arm();
  };

  connect();

  return (): void => {
    torndown = true;
    if (watchdog !== undefined) {
      clearTimeout(watchdog);
    }

    source.close();
  };
}
```

- [ ] **Step 2b: Typecheck**

Run: `cd client && npm run check`
Expected: clean (tsc --noEmit). `main.ts` ignores `connectEvents`'s return value, so the changed return type is fine.

- [ ] **Step 3: Write the multiplayer e2e**

Create `client/e2e/multiplayer.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

// The e2e server is shared across the whole suite and entities never despawn,
// so assert >= 2 and track specific entity ids rather than an exact count.
test("two clients share one world and see each other move", async ({ browser }) => {
  const ctxA = await browser.newContext();
  const ctxB = await browser.newContext();
  const a = await ctxA.newPage();
  const b = await ctxB.newPage();

  await a.goto("/");
  await b.goto("/");

  await expect.poll(() => a.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();
  await expect.poll(() => b.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();

  const idA = await a.evaluate(() => window.game.me!.id);
  const idB = await b.evaluate(() => window.game.me!.id);
  expect(idA).not.toBe(idB); // distinct identities, separate localStorage

  await expect.poll(() => a.evaluate(() => window.game.entities)).toBeGreaterThanOrEqual(2);
  await expect.poll(() => b.evaluate(() => window.game.entities)).toBeGreaterThanOrEqual(2);

  // A walks one step (whichever direction is walkable from its spawn).
  const startA = await a.evaluate(() => window.game.me!.hex);
  for (const key of ["KeyW", "KeyE", "KeyD", "KeyS", "KeyA", "KeyQ"]) {
    await a.keyboard.press(key);
  }
  await expect
    .poll(
      () => a.evaluate((s) => { const h = window.game.me!.hex; return h.q !== s.q || h.r !== s.r; }, startA),
      { timeout: 10_000 },
    )
    .toBe(true);
  const movedA = await a.evaluate(() => window.game.me!.hex);

  // B observes A's entity at the moved hex — two clients, one shared world.
  await expect
    .poll(
      () =>
        b.evaluate(
          (args) => {
            const p = window.game.positions.find((x) => x.id === args.id);

            return p ? p.hex.q === args.hex.q && p.hex.r === args.hex.r : false;
          },
          { id: idA, hex: movedA },
        ),
      { timeout: 10_000 },
    )
    .toBe(true);

  await ctxA.close();
  await ctxB.close();
});

test("a client reconnects and resyncs after its stream drops", async ({ page, context }) => {
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.connected)).toBe(true);
  await expect.poll(() => page.evaluate(() => window.game.me?.id ?? null)).not.toBeNull();

  const turnBefore = await page.evaluate(() => window.game.turn);

  // Pull the plug: the SSE stream drops and EventSource reports disconnected.
  await context.setOffline(true);
  await expect.poll(() => page.evaluate(() => window.game.connected), { timeout: 15_000 }).toBe(false);

  // Restore: EventSource reconnects (sending Last-Event-ID) and resyncs.
  await context.setOffline(false);
  await expect.poll(() => page.evaluate(() => window.game.connected), { timeout: 15_000 }).toBe(true);

  // The turn counter resumes advancing past where it stalled.
  await expect
    .poll(() => page.evaluate(() => window.game.turn), { timeout: 15_000 })
    .toBeGreaterThan(turnBefore);
});
```

- [ ] **Step 4: Run the full e2e suite**

Run: `make e2e`
Expected: all specs PASS — the two new `multiplayer.spec.ts` tests plus the unchanged `hex`/`turn`/`move`/`walk` specs.

> If the reconnect test flakes on the offline→online transition, the timeouts are already generous (15s) and poll-based; re-run once. If multi-client `keyboard.press` doesn't move A (spawn hemmed in — rare), that mirrors `move.spec`'s existing approach and should resolve on retry.

- [ ] **Step 5: Commit**

```bash
git add client/src/main.ts client/src/net/events.ts client/e2e/multiplayer.spec.ts
git commit -m "client: entity positions + SSE liveness watchdog; e2e multi-client + reconnect"
```

---

## Task 5: Docs + full gate

**Files:**
- Modify: `docs/roguelike-mp-plan.md`
- Modify: `docs/STATUS.md`

- [ ] **Step 1: Annotate the plan's §4**

In `docs/roguelike-mp-plan.md` §4, add a note (near the "replay buffer" / "full-state resync endpoint" bullet) recording the milestone-5 decision, e.g.:

> **Milestone 5 update:** with full-snapshot turn bundles and a coalescing hub, a reconnecting client only needs the current snapshot, so the replay buffer and separate resync endpoint above were **not** built — reconnect is resync-to-latest, and `Last-Event-ID` is honoured only as a watermark to avoid re-painting an already-seen turn.

And in §9, tick the reconnect/resync decision as resolved (resync-to-latest).

- [ ] **Step 2: Update `docs/STATUS.md`**

- Add milestone 5 to the done table (commit column: `milestone-5-multiplayer-reconnect (branch)` — final SHA unknown pre-merge).
- Update "What works right now" to mention: multiple clients in one world with simultaneous resolution; reconnect/resync via `Last-Event-ID` (resync-to-latest); `window.game.positions`.
- Remove the "`Last-Event-ID` replay not implemented" known-placeholder (now resolved); note resync-to-latest as the chosen model.
- Change "Next" to **milestone 6 — combat, time bubbles, phased resolution & death** (bump-to-attack, deterministic phased resolution with seeded tie-break on overflow, local time bubbles, death → fall-back-to-level-start).
- Set `Last updated` to the session date.

- [ ] **Step 3: Run the full gate**

Run: `make check`
Expected: lint 0, protocol no-drift, tsc clean, Go unit + integration pass, builds succeed.

- [ ] **Step 4: Run the e2e suite**

Run: `make e2e`
Expected: all specs PASS.

- [ ] **Step 5: Commit**

```bash
git add docs/roguelike-mp-plan.md docs/STATUS.md
git commit -m "docs: milestone 5 done — multiplayer & reconnect; next is milestone 6"
```

---

## Self-Review

**Spec coverage:**
- Resync-to-latest + honour `Last-Event-ID` → Task 1 (server + tests). ✔
- Client `positions` field → Task 4. ✔
- Unit conflict tests (stacking, overflow, no tie-break lock) + `PlaceEntityForTest` → Task 3. ✔
- Integration `Last-Event-ID` resume (withhold + mismatch/fresh) → Task 1. ✔
- Integration simultaneous resolution → Task 2. ✔
- e2e multi-client visibility + pulled-plug reconnect → Task 4. ✔
- Docs: supersede §4, resolve STATUS placeholder, next=M6 → Task 5. ✔
- Out-of-scope (phased/seeded resolution, combat, replay buffer) → not implemented; noted in Global Constraints + Task 3. ✔

**Placeholder scan:** No "TBD"/"handle edge cases"/"similar to Task N" — each code step carries full code; the only conditional note (reconnect e2e flake) gives concrete mitigation. ✔

**Type consistency:**
- `PlaceEntityForTest(hex) (int64, string)` — defined Task 3, used in Task 3 tests. ✔
- `parseLastEventID(r) int64` — defined + used Task 1. ✔
- Integration helpers `getWithLastEventID`, `readFrameWithin` (Task 1) and `postIntent`/`hexOf`/`walkableNeighborOf` (Task 2) live in distinct files, no name clash with existing `get`/`readFrames`/`neighborsOf`. ✔
- `GameDebug.positions: { id: number; hex: Hex }[]` — defined Task 4, read by `multiplayer.spec.ts` (Task 4). ✔
- Frozen-clock tests rely on `startServer(t, time.Hour, time.Hour)` keeping the world at turn 0 — consistent with `NewWorld` initializing `turn = 0` and `Run` first ticking after the interval. ✔

**Shared-server caveat surfaced:** the multi-client e2e asserts `entities >= 2` and tracks specific ids because the e2e server accumulates entities across the suite (no despawn) — documented in Task 4. ✔
