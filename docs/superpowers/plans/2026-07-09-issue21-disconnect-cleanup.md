# Issue #21 — Disconnect Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or executing-plans. Steps use `- [ ]` checkboxes.
> **DRAFT for review — do not build until approved.**

**Goal:** Remove a player's entity after its event stream has been gone for a grace period, so the world stops accumulating ghosts — without breaking the M5 reconnect model.

**Architecture:** The SSE stream carries the player's token (`/api/events?token=`). Each player entity tracks a live stream count + a `disconnectedAt`; the `Run`/`pollTick` control loop sweeps and removes players whose streams have been 0 for longer than `DisconnectGrace`. A reconnect (stream reopens with the same token) within the grace keeps the character.

## Global Constraints
- Grace-period removal (NOT immediate) so an `EventSource` blip/reconnect keeps the character. `DisconnectGrace` must exceed the reconnect + liveness-watchdog windows (default 20s ≫ `max(3s, 4×intervalMs)`).
- Only **player** entities are swept (monsters have no token/streams). Removal = delete from `entities` + `byToken`, then `recomputeBubblesLocked`.
- All presence mutations + the sweep hold `w.mu`; no new goroutine — the sweep rides the existing `pollTick`. Use the injectable clock (`w.now`) so tests drive it deterministically.
- Keep the per-spec e2e servers (6b.2) as-is (don't revert here). `got, want`; `make check` + `make e2e` green. One PR off `main` (branch `fix-21-disconnect-cleanup`).

## Task 1: Config — DisconnectGrace

**Files:** `internal/config/config.go`, `config_test.go`; `cmd/rogue/app/app.go`; `internal/game/world.go` (`NewWorld`).
- [ ] Add `Config.DisconnectGrace` (default **20s**) via `overrideDuration("DISCONNECT_GRACE")` (rejects ≤0, like the others). Config tests: default, override, non-positive rejected.
- [ ] Thread it into `NewWorld(interval, combatPatience, bubblePoll, disconnectGrace, ticks)` — add the param, store `w.disconnectGrace`. Update ALL callers (`app.go`, `internal/game` test helper `newWorld`, `test/integration` `startServer*`). (Follow the exact pattern used for `combatPatience`/`bubblePoll`.)
- [ ] Verify + commit.

## Task 2: Game — presence tracking + grace sweep

**Files:** `internal/game/world.go`; tests `internal/game/presence_test.go`.
- [ ] **entity fields**: add `streams int` and `disconnectedAt time.Time` (players only; monsters leave them zero — never swept).
- [ ] **Join**: a new player entity starts `streams = 0`, `disconnectedAt = w.now()` (starts the grace clock until its stream opens).
- [ ] **Presence API** (exported, take the token; hold `w.mu`):

```go
// StreamOpened marks that a live event stream opened for the entity with this
// token (a new connection or an EventSource reconnect). No-op for an unknown
// token (e.g. a stream opened before/without a join).
func (w *World) StreamOpened(token string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if e, ok := w.byToken[token]; ok {
		e.streams++
	}
}

// StreamClosed marks that an event stream for this token closed; when the last
// one closes it stamps disconnectedAt, starting the removal grace.
func (w *World) StreamClosed(token string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if e, ok := w.byToken[token]; ok && e.streams > 0 {
		e.streams--
		if e.streams == 0 {
			e.disconnectedAt = w.now()
		}
	}
}
```

- [ ] **Sweep**: `sweepDisconnectedLocked(now time.Time)` — collect player ids with `streams == 0 && now.Sub(disconnectedAt) > w.disconnectGrace`, `delete` each from `entities`/`byToken`, then `recomputeBubblesLocked()` if any were removed. Call it once per `pollTick` (at a safe point — before/after the resolution passes, consistent with the capture-then-recompute ordering; do not remove an entity a resolution this pass already captured — simplest: sweep at the START of `pollTick`, before capturing member sets). Publish on removal so clients despawn the entity.
- [ ] **Tests** (`presence_test.go`, injectable clock, tiny grace via `SetDisconnectGraceForTest`/config): open→count 1; close→count 0 + `disconnectedAt` set; sweep past grace removes the player; sweep within grace keeps it; reopen (reconnect) within grace → kept; two streams → kept until both close; a monster is never swept; removing a bubble member recomputes (the bubble dissolves if it was the only player). Add bridges `StreamOpenedForTest`/`ForceSweepForTest(now)` as needed.
- [ ] Verify + `make lint`; commit.

## Task 3: Server — identify the stream, hook open/close

**Files:** `internal/server/events.go`; a handler test.
- [ ] In `handleEvents`, read `token := r.URL.Query().Get("token")`. If non-empty, `deps.World.StreamOpened(token)` right after the stream is established, and `defer deps.World.StreamClosed(token)` so it fires on any return (incl. `r.Context().Done()`). (An empty token — a not-yet-joined client just watching — is a no-op.)
- [ ] Verify the existing SSE/reconnect integration tests still pass; commit.

## Task 4: Client — token on the stream + re-join if removed

**Files:** `client/src/net/events.ts` (+ its callers), `client/src/main.ts`.
- [ ] `connectEvents` opens `/api/events` **with the token** when available: pass the token into `connectEvents` (from the stored/joined identity) and build `new EventSource("/api/events?token=" + encodeURIComponent(token))`; a client with no token yet connects without it (watch-only until join). Ensure the reconnect path (auto-retry) also carries the token.
- [ ] **Re-join if gone**: if the client's own entity id is absent from turn bundles for a short spell (e.g. N consecutive bundles / a couple seconds — it was swept after a long disconnect), **re-join** with the stored token (unknown token → fresh entity, existing behaviour) and adopt the new identity/entity id. Keep it minimal and guarded (don't re-join spuriously on a single missed bundle).
- [ ] `npm run check`; `make e2e` (2×) green; commit.

## Task 5: Integration + e2e

**Files:** `test/integration/*`.
- [ ] **Integration**: with a tiny `DISCONNECT_GRACE`, a client that opens `/api/events?token=` then closes the stream has its entity **removed** from the bundle after the grace; open→close→reopen within the grace keeps it; a second client observes the removal. Robust; default `startServer` untouched (its grace can be long so existing tests are unaffected — check existing integration tests don't accidentally trip the sweep; set a comfortably long default grace in the harness).
- [ ] Confirm the full `make e2e` still passes (the per-spec servers keep it stable). Commit.

## Task 6: Docs + gate

**Files:** `docs/STATUS.md`, `docs/roguelike-mp-plan.md` (§9).
- [ ] STATUS: resolve the "Entities never leave the world" placeholder — a disconnected player's entity is removed after `DISCONNECT_GRACE`; reconnect within the grace keeps it. Note the §9 offline-policy is now decided (remove-after-grace). Mention the e2e per-spec-servers could be simplified as a follow-up now that accumulation is fixed at the root. Update `Last updated`.
- [ ] Mark the §9 "offline-character policy" open question decided in `docs/roguelike-mp-plan.md`.
- [ ] `make check` + `make e2e` green; commit. Reference: closes #21.

## Self-Review
- Identity on the SSE stream (`?token=`) → Task 3/4. ✔
- Grace-period removal (not immediate) preserves reconnect → Tasks 2/1. ✔
- Presence via stream count + `disconnectedAt`; sweep in `pollTick` → Task 2. ✔
- Client reconnect carries token + re-join-if-removed → Task 4. ✔
- Only players swept; bubble recompute on removal → Task 2. ✔
- §9 policy decided; STATUS placeholder resolved → Task 6. ✔
- Per-spec e2e servers kept (revert = follow-up) → Global Constraints + spec. ✔
