# M6 warmup — Heartbeat as an observable SSE event: Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or executing-plans. Steps use `- [ ]` checkboxes.

**Goal:** Promote the SSE keep-alive from a comment to a named `heartbeat` event with a guaranteed always-on cadence, so the client's liveness watchdog survives a frozen combat clock (milestone 6.4).

**Architecture:** Server emits `event: heartbeat` (no id) on a fixed `HeartbeatInterval` regardless of turns; client resets its watchdog on it and counts it on `window.game`.

## Global Constraints
- `internal/protocol` is the single source of truth; after editing it run `make protocol` and stage `client/src/protocol.gen.ts`.
- `window.game` stays in sync (interface + initializer + assignment).
- A heartbeat frame carries **no `id:`** — it must not advance `Last-Event-ID`.
- Go may not be on PATH; use `make` / `PATH=$PATH:/usr/local/go/bin go ...`. Run `make lint` before committing Go.
- Full gate: `make check` + `make e2e` green.
- Delivery: one PR off `main` (branch `m6-heartbeat-event`).

---

## Task 1: Protocol + server — named, always-on heartbeat + integration test

**Files:** modify `internal/protocol/protocol.go`, `internal/server/events.go`, `test/integration/events_test.go`; regenerate `client/src/protocol.gen.ts`.

- [ ] **Step 1: Add the protocol constant.** In `internal/protocol/protocol.go`, in the SSE-event-names const block (next to `EventTurn`):

```go
	// EventHeartbeat is a keep-alive frame on the events stream. It carries no
	// id (it is not a turn and must not advance Last-Event-ID) and fires on a
	// fixed HeartbeatInterval so the client's liveness watchdog stays fed even
	// when a frozen combat clock stops turn frames.
	EventHeartbeat = "heartbeat"
```

Then run `make protocol` and confirm `client/src/protocol.gen.ts` gains `export const EventHeartbeat = "heartbeat";`.

- [ ] **Step 2: Rewrite the integration test (RED).** In `test/integration/events_test.go`, replace `TestEventsHeartbeat` with:

```go
func TestEventsHeartbeatIsNamedEvent(t *testing.T) {
	t.Parallel()

	// Frozen turn clock, fast heartbeat: the stream must carry a named
	// `heartbeat` event (not a comment) so EventSource can observe it, and it
	// must NOT carry an id (heartbeats do not advance Last-Event-ID).
	ts := startServer(t, time.Hour, 20*time.Millisecond)

	resp := get(t, ts, "/api/events")
	reader := bufio.NewReader(resp.Body)
	deadline := time.After(5 * time.Second)
	got := make(chan bool, 1)

	go func() {
		sawID := false
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			line = strings.TrimRight(line, "\n")
			switch {
			case strings.HasPrefix(line, "id: "):
				sawID = true
			case line == "event: "+protocol.EventHeartbeat:
				got <- !sawID // true only if no id preceded this heartbeat
				return
			case line == "":
				sawID = false // frame boundary resets id tracking
			}
		}
	}()

	select {
	case noID := <-got:
		if !noID {
			t.Fatal("heartbeat event carried an id; it must not advance Last-Event-ID")
		}
	case <-deadline:
		t.Fatal("no named heartbeat event arrived on the stream")
	}
}
```

Run: `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ -run TestEventsHeartbeatIsNamedEvent -v` → FAIL (server still writes a `: hb` comment, no `event: heartbeat`).

- [ ] **Step 3: Change the server (GREEN).** In `internal/server/events.go`:
  - Update the handler's doc comment: the keep-alive is now a named `heartbeat` event on a fixed cadence.
  - Remove `heartbeat.Reset(deps.HeartbeatInterval)` from the `case <-ticks:` branch (always-on cadence).
  - Replace the `case <-heartbeat.C:` body:

```go
			case <-heartbeat.C:
				// Named keep-alive: fires on a fixed cadence regardless of turns,
				// so the client's liveness watchdog stays fed even when a frozen
				// combat clock stops turn frames. No id: — a heartbeat is not a
				// turn and must not advance Last-Event-ID.
				if _, err := fmt.Fprintf(w, "event: %s\ndata: {}\n\n", protocol.EventHeartbeat); err != nil {
					return
				}

				flusher.Flush()
```

Run: `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ -run TestEventsHeartbeatIsNamedEvent -v` → PASS.

- [ ] **Step 4: Full integration suite + lint.** `PATH=$PATH:/usr/local/go/bin go test ./test/integration/ && make lint` → all pass, 0 issues. (All other integration tests freeze the heartbeat with `time.Hour`, so the always-on change doesn't interleave heartbeat frames into their `readFrames` reads.)

- [ ] **Step 5: Commit.**
```bash
git add internal/protocol/protocol.go client/src/protocol.gen.ts internal/server/events.go test/integration/events_test.go
git commit -m "server: heartbeat is a named always-on SSE event (survives a frozen clock)"
```

---

## Task 2: Client — observe heartbeats + reset watchdog + e2e

**Files:** modify `client/src/net/events.ts`, `client/src/main.ts`, `client/playwright.config.ts`; create `client/e2e/heartbeat.spec.ts`.

- [ ] **Step 1: events.ts — heartbeat listener + callback.** Add `onHeartbeat` (optional) to `EventsCallbacks`:

```ts
export interface EventsCallbacks {
  onTurn: (turn: TurnEvent) => void;
  onConnectionChange: (connected: boolean) => void;
  onHeartbeat?: () => void;
}
```

Import the constant: `import { EventHeartbeat, EventTurn, type TurnEvent } from "../protocol.gen";`

Inside `connect()`, add a listener (a heartbeat is proof of liveness — report connected and re-arm the watchdog, same as a turn minus the payload):

```ts
    source.addEventListener(EventHeartbeat, () => {
      callbacks.onConnectionChange(true);
      arm();
      callbacks.onHeartbeat?.();
    });
```

- [ ] **Step 2: main.ts — expose the counter.** Add to the `GameDebug` interface (after `intervalMs` or near the connection fields):

```ts
  /** Count of named heartbeat frames received — proves the keep-alive is observable. */
  heartbeats: number;
```

Add `heartbeats: 0,` to the `window.game = { ... }` initializer, and pass the callback in the `connectEvents({ ... })` call:

```ts
    onHeartbeat: (): void => {
      window.game.heartbeats += 1;
    },
```

- [ ] **Step 3: playwright.config.ts — fast heartbeat.** Add to `webServer.env` (next to `TURN_INTERVAL`):

```ts
      HEARTBEAT_INTERVAL: "500ms",
```

- [ ] **Step 4: Typecheck.** `cd client && npm run check` → clean.

- [ ] **Step 5: e2e test.** Create `client/e2e/heartbeat.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

import type { GameDebug } from "../src/main";

declare global {
  interface Window {
    game: GameDebug;
  }
}

test("the client receives named heartbeat events while turns also flow", async ({ page }) => {
  await page.goto("/");

  await expect.poll(() => page.evaluate(() => window.game.connected)).toBe(true);

  // Named heartbeats (HEARTBEAT_INTERVAL=500ms) are observable by the client.
  await expect
    .poll(() => page.evaluate(() => window.game.heartbeats), { timeout: 10_000 })
    .toBeGreaterThan(0);

  // Turns still advance and the connection stays up alongside the heartbeats.
  const turn = await page.evaluate(() => window.game.turn);
  await expect.poll(() => page.evaluate(() => window.game.turn), { timeout: 10_000 }).toBeGreaterThan(turn);
  expect(await page.evaluate(() => window.game.connected)).toBe(true);
});
```

- [ ] **Step 6: Full e2e suite.** `make e2e` → all specs pass (the new `heartbeat.spec.ts` plus the unchanged suite).

- [ ] **Step 7: Commit.**
```bash
git add client/src/net/events.ts client/src/main.ts client/playwright.config.ts client/e2e/heartbeat.spec.ts
git commit -m "client: observe named heartbeats, reset watchdog on them; e2e"
```

---

## Task 3: Docs + gate

**Files:** modify `docs/STATUS.md`.

- [ ] **Step 1: Update STATUS.md.** Remove/resolve the "Watchdog liveness is turn-based, not heartbeat-based" known-placeholder — the client now resets its watchdog on named heartbeats and the server emits them always-on. Note the residual: the full "watchdog survives a frozen clock" scenario is only end-to-end testable once combat bubbles (6.4) can freeze the clock. Note this warmup is the first landed piece of milestone 6; next is 6.1 phased resolution. Set `Last updated`.

- [ ] **Step 2: Full gate.** `make check` (green) and `make e2e` (green).

- [ ] **Step 3: Commit.**
```bash
git add docs/STATUS.md
git commit -m "docs: heartbeat-as-event landed (m6 warmup); next is 6.1 phased resolution"
```

## Self-Review
- Protocol `EventHeartbeat` → Task 1; consumed by server (Task 1) and client (Task 2). ✔
- No-id heartbeat asserted → Task 1 integration test. ✔
- Always-on cadence (drop `heartbeat.Reset`) → Task 1. ✔
- Client watchdog reset + `window.game.heartbeats` + e2e config → Task 2. ✔
- STATUS debt resolved, freeze-survival deferred to 6.4 → Task 3. ✔
- No placeholders; each code step carries full code. ✔
