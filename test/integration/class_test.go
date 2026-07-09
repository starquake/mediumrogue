package integration_test

import (
	"bufio"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestJoinPerClassMaxHP joins one of each class and reads their MaxHP (and
// Class) back off a real turn bundle, proving the per-class stats in
// internal/game/class.go actually reach the wire: fighter is tankiest, rogue
// and mage are squishier, matching the protocol constants exactly (not just
// "different").
func TestJoinPerClassMaxHP(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour) // frozen clock: no monsters, no movement needed

	fighter := joinClass(t, ts, "", protocol.ClassFighter)
	rogue := joinClass(t, ts, "", protocol.ClassRogue)
	mage := joinClass(t, ts, "", protocol.ClassMage)

	events := get(t, ts, "/api/events")
	frames := readFrames(t, bufio.NewReader(events.Body), 1) // immediate snapshot, post-joins

	var bundle protocol.TurnEvent
	if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
		t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
	}

	entityOf := func(id int64) protocol.Entity {
		for _, e := range bundle.Entities {
			if e.ID == id {
				return e
			}
		}

		t.Fatalf("entity %d missing from turn bundle", id)

		return protocol.Entity{}
	}

	fighterE, rogueE, mageE := entityOf(fighter.EntityID), entityOf(rogue.EntityID), entityOf(mage.EntityID)

	for _, tc := range []struct {
		name      string
		e         protocol.Entity
		wantClass string
		wantMaxHP int
	}{
		{"fighter", fighterE, protocol.ClassFighter, protocol.FighterMaxHP},
		{"rogue", rogueE, protocol.ClassRogue, protocol.RogueMaxHP},
		{"mage", mageE, protocol.ClassMage, protocol.MageMaxHP},
	} {
		if got, want := tc.e.Class, tc.wantClass; got != want {
			t.Errorf("%s: Class on wire = %q, want %q", tc.name, got, want)
		}

		if got, want := tc.e.MaxHP, tc.wantMaxHP; got != want {
			t.Errorf("%s: MaxHP on wire = %d, want %d", tc.name, got, want)
		}

		if got, want := tc.e.HP, tc.wantMaxHP; got != want {
			t.Errorf("%s: spawn HP = %d, want full MaxHP %d", tc.name, got, want)
		}
	}

	if got, want := fighterE.MaxHP, rogueE.MaxHP; got <= want {
		t.Errorf("fighter MaxHP %d should exceed rogue MaxHP %d", got, want)
	}

	if got, want := fighterE.MaxHP, mageE.MaxHP; got <= want {
		t.Errorf("fighter MaxHP %d should exceed mage MaxHP %d", got, want)
	}
}

// TestFighterAttackIntentRejected proves the class-gated ranged rule end to
// end: a fighter has no ranged weapon, so an "attack" intent must be rejected
// (422) rather than silently doing nothing or moving.
func TestFighterAttackIntentRejected(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)
	me := joinClass(t, ts, "", protocol.ClassFighter)

	resp := postAttackIntent(t, ts, me, protocol.Hex{Q: me.Hex.Q + 1, R: me.Hex.R})
	if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
		t.Fatalf("fighter attack-intent status = %d, want %d", got, want)
	}
}

// postAttackIntent posts a ranged "attack" intent and returns the raw
// response. Unlike postIntent (which hard-asserts 202 because a move onto a
// walkable, reachable hex the test itself chose should never fail), an attack
// can legitimately be rejected — out of range, or a fighter with no ranged
// weapon — so callers decide how to react to the status.
func postAttackIntent(t *testing.T, ts *httptest.Server, me protocol.JoinResponse, target protocol.Hex) *http.Response {
	t.Helper()

	intent := protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentAttack, Target: target,
	}

	return postJSON(t, ts, "/api/intent", intent)
}

// fireBowOrChase submits a rogue's turn action: fire the bow (an "attack"
// intent) at target if it is within BowRange, else move toward it. Returns
// whether the shot was accepted (fired in range this turn); a rejection
// increments *consecutiveRejected and, past 40 in a row, fails the test loudly
// with the rejection body — a real regression (e.g. the range check flipped)
// should not silently exhaust the whole deadline before failing.
func fireBowOrChase(
	t *testing.T, ts *httptest.Server, me protocol.JoinResponse,
	myHex, target protocol.Hex, consecutiveRejected *int,
) bool {
	t.Helper()

	if hexDistance(myHex, target) > protocol.BowRange {
		postIntent(t, ts, me, target)

		return false
	}

	resp := postAttackIntent(t, ts, me, target)
	if resp.StatusCode == http.StatusAccepted {
		*consecutiveRejected = 0

		return true
	}

	var body protocol.ErrorResponse

	_ = json.NewDecoder(resp.Body).Decode(&body)

	*consecutiveRejected++
	if *consecutiveRejected > 40 {
		t.Fatalf(
			"rogue attack intent rejected repeatedly (status %d, %q); range check: dist=%d, BowRange=%d",
			resp.StatusCode, body.Error, hexDistance(myHex, target), protocol.BowRange,
		)
	}

	return false
}

// TestRogueBowKillsAtRange exercises the rogue's ranged bow attack over real
// HTTP/SSE: a joined rogue targets whichever monster is nearest (recomputed
// every bundle, since monsters hunt back and a fixed target can go stale) and,
// once within BowRange, fires an "attack" intent instead of moving.
//
// Because an attack intent clears the rogue's route (queueAttackLocked), the
// rogue never bumps while it is firing — so any monster HP drop observed on a
// turn where the rogue fired is attributable solely to the ranged shot
// (resolveBowLocked), proving the bow path lands over real HTTP, not just a
// disguised bump.
//
// The monster is seeded two hexes from the origin (where the rogue spawns) —
// already inside BowRange, so the rogue fires from range on its first bubble
// lock-in rather than after a long crypto-random chase gated on the background
// tick loop. That makes the ranged kill deterministic and robust even under a
// CPU-starved runner (#22). The test is not parallel so its tick loop is not
// starved by sibling servers.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestRogueBowKillsAtRange(t *testing.T) {
	ts := startServerWithMonstersAt(t, 15*time.Millisecond, protocol.Hex{Q: 0, R: -2})

	me := joinClass(t, ts, "", protocol.ClassRogue)

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	firstFrame := readFrames(t, reader, 1)

	var first protocol.TurnEvent
	if err := json.Unmarshal([]byte(firstFrame[0].data), &first); err != nil {
		t.Fatalf("unmarshal bundle %q: %v", firstFrame[0].data, err)
	}

	startHP := make(map[int64]int)

	for _, e := range first.Entities {
		if e.Kind == protocol.EntityMonster {
			startHP[e.ID] = e.HP
		}
	}

	if len(startHP) == 0 {
		t.Fatal("no monsters present in first bundle")
	}

	var (
		monsterDamaged      bool
		everFiredInRange    bool
		consecutiveRejected int
	)

	deadline := time.Now().Add(10 * time.Second)

	for time.Now().Before(deadline) {
		frames := readFrames(t, reader, 1)

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		myHex := hexOf(bundle, me.EntityID)
		if myHex == (protocol.Hex{Q: -999, R: -999}) {
			t.Fatal("joined rogue missing from turn bundle")
		}

		if target, found := nearestMonster(bundle, myHex); found {
			if fireBowOrChase(t, ts, me, myHex, target, &consecutiveRejected) {
				everFiredInRange = true
			}
		}

		seenNow := make(map[int64]bool, len(startHP))

		for _, e := range bundle.Entities {
			if e.Kind != protocol.EntityMonster {
				continue
			}

			seenNow[e.ID] = true

			if start, tracked := startHP[e.ID]; tracked && e.HP < start {
				monsterDamaged = true
			}
		}

		for id := range startHP {
			if !seenNow[id] {
				monsterDamaged = true // killed and removed from the snapshot
			}
		}

		if monsterDamaged && everFiredInRange {
			return // ranged bow damage proven end to end
		}
	}

	t.Fatalf(
		"rogue ranged combat trend not observed before deadline: monsterDamaged=%v everFiredInRange=%v",
		monsterDamaged, everFiredInRange,
	)
}

// bestAoETarget picks whichever candidate hex — the caster's own hex, or one
// of the given hostile hexes — currently has the most hostiles within
// MageAoERadius, maximizing the mage's chance of a same-turn multi-hit given
// the CURRENT board (recomputed every bundle, since monsters keep moving).
// hostiles must be non-empty; the caster's own hex always counts itself as a
// covering candidate (distance 0), and every hostile hex trivially covers
// itself, so the returned count is always >= 1.
func bestAoETarget(mine protocol.Hex, hostiles []protocol.Hex) (protocol.Hex, int) {
	countWithin := func(h protocol.Hex) int {
		n := 0

		for _, m := range hostiles {
			if hexDistance(h, m) <= protocol.MageAoERadius {
				n++
			}
		}

		return n
	}

	best, bestCount := mine, countWithin(mine)

	for _, h := range hostiles {
		if c := countWithin(h); c > bestCount {
			best, bestCount = h, c
		}
	}

	return best, bestCount
}

// fireAoEOrChase submits a mage's turn action: fire the AoE (an "attack"
// intent) at whichever hex currently covers the most hostiles (bestAoETarget)
// if that hex is within MageRange, else chase the nearest monster into range.
// A rejection increments *consecutiveRejected and, past 40 in a row, fails the
// test loudly instead of silently exhausting the whole deadline.
func fireAoEOrChase(
	t *testing.T, ts *httptest.Server, me protocol.JoinResponse,
	bundle protocol.TurnEvent, myHex protocol.Hex, monsterHexes []protocol.Hex,
	consecutiveRejected *int,
) {
	t.Helper()

	candidate, _ := bestAoETarget(myHex, monsterHexes)

	if hexDistance(myHex, candidate) > protocol.MageRange {
		if target, found := nearestMonster(bundle, myHex); found {
			postIntent(t, ts, me, target)
		}

		return
	}

	resp := postAttackIntent(t, ts, me, candidate)
	if resp.StatusCode == http.StatusAccepted {
		*consecutiveRejected = 0

		return
	}

	*consecutiveRejected++
	if *consecutiveRejected > 40 {
		t.Fatalf("mage attack intent rejected repeatedly (status %d)", resp.StatusCode)
	}
}

// TestMageAoEDamagesMonsters exercises the mage's ranged AoE attack over real
// HTTP/SSE: a joined mage fires at whichever hex currently covers the most
// hostiles within MageAoERadius (bestAoETarget, recomputed every bundle),
// falling back to chasing the nearest monster into range otherwise.
//
// It requires TWO OR MORE monsters to take damage in the SAME turn (a bump or
// a bow always resolves to exactly one victim, so a same-turn multi-drop is
// only possible via the AoE) — not just the single-hit trend, matching the
// unit-level "2 monsters in radius, both take damage, no friendly fire" case
// pinned deterministically in internal/game's ranged_test.go.
//
// Two monsters are seeded on adjacent hexes two steps from the origin (where
// the mage spawns). They beeline for the mage as one cluster (bubble.go: every
// monster within CombatRadius joins one bubble), so on an early bubble-turn
// they sit within a single MageAoERadius footprint the mage can reach — the
// mage fires at the hex covering the most hostiles (bestAoETarget, recomputed
// every bundle) and drops both at once. Seeding a known cluster next to the
// spawn makes the same-turn multi-hit deterministic and robust even under a
// CPU-starved runner (#22), rather than depending on where crypto/rand
// scattered 16 monsters. The test is not parallel so its tick loop is not
// starved by sibling servers.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestMageAoEDamagesMonsters(t *testing.T) {
	ts := startServerWithMonstersAt(
		t, 10*time.Millisecond, protocol.Hex{Q: 0, R: -2}, protocol.Hex{Q: 1, R: -2},
	)

	me := joinClass(t, ts, "", protocol.ClassMage)

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	firstFrame := readFrames(t, reader, 1)

	var first protocol.TurnEvent
	if err := json.Unmarshal([]byte(firstFrame[0].data), &first); err != nil {
		t.Fatalf("unmarshal bundle %q: %v", firstFrame[0].data, err)
	}

	startHP := make(map[int64]int)

	for _, e := range first.Entities {
		if e.Kind == protocol.EntityMonster {
			startHP[e.ID] = e.HP
		}
	}

	if len(startHP) == 0 {
		t.Fatal("no monsters present in first bundle")
	}

	prevHP := make(map[int64]int, len(startHP))
	maps.Copy(prevHP, startHP)

	var (
		monsterDamaged      bool
		maxSimultaneous     int
		consecutiveRejected int
	)

	deadline := time.Now().Add(15 * time.Second)

	for time.Now().Before(deadline) {
		frames := readFrames(t, reader, 1)

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		myHex := hexOf(bundle, me.EntityID)
		if myHex == (protocol.Hex{Q: -999, R: -999}) {
			t.Fatal("joined mage missing from turn bundle")
		}

		monsterHexes := make([]protocol.Hex, 0, len(bundle.Entities))

		for _, e := range bundle.Entities {
			if e.Kind == protocol.EntityMonster {
				monsterHexes = append(monsterHexes, e.Hex)
			}
		}

		if len(monsterHexes) > 0 {
			fireAoEOrChase(t, ts, me, bundle, myHex, monsterHexes, &consecutiveRejected)
		}

		seenNow := make(map[int64]bool, len(startHP))
		curHP := make(map[int64]int, len(bundle.Entities))

		for _, e := range bundle.Entities {
			if e.Kind != protocol.EntityMonster {
				continue
			}

			seenNow[e.ID] = true
			curHP[e.ID] = e.HP

			if start, tracked := startHP[e.ID]; tracked && e.HP < start {
				monsterDamaged = true
			}
		}

		for id := range startHP {
			if !seenNow[id] {
				monsterDamaged = true // killed and removed from the snapshot
			}
		}

		// Bonus signal: how many distinct monsters lost HP (or vanished)
		// between the previous and this bundle. Only the AoE can drop >=2 in
		// one turn (a bump or a bow always resolves to exactly one victim).
		hitThisBundle := 0

		for id, before := range prevHP {
			if after, stillHere := curHP[id]; !stillHere || after < before {
				hitThisBundle++
			}
		}

		if hitThisBundle > maxSimultaneous {
			maxSimultaneous = hitThisBundle
		}

		prevHP = curHP

		if monsterDamaged && maxSimultaneous >= 2 {
			t.Logf("mage AoE hit %d monsters in one turn", maxSimultaneous)

			return
		}
	}

	// Empirically (40 local runs at this monsterCount/deadline, see
	// task-7-report.md) >=2 monsters converge onto the mage's AoE footprint on
	// the same bubble-turn every time, comfortably inside the deadline — so
	// this is a hard requirement, not a soft trend. If this ever proves flaky
	// on a slower CI box, the fallback is to relax it to the monsterDamaged
	// (single-hit) trend only, matching TestRogueBowKillsAtRange's rigor.
	t.Fatalf(
		"mage AoE never hit >=2 monsters in one turn before deadline: monsterDamaged=%v maxSimultaneous=%d",
		monsterDamaged, maxSimultaneous,
	)
}
