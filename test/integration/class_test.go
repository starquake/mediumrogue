package integration_test

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// equippedRangedStats returns the range/AoE radius of entity id's equipped
// ranged/magic-tagged weapon, read straight off a real turn bundle
// (Entity.Items). Used to drive the bow/AoE tests' fire-or-chase decisions
// without mirroring internal/game/content.go's shortbow/ember-focus numbers
// as local literals — the wire is now the single source of truth, also
// pinned directly against the registry by internal/game's items_test.go.
func equippedRangedStats(bundle protocol.TurnEvent, id int64) (int, int, bool) {
	e, found := entityOf(bundle, id)
	if !found {
		return 0, 0, false
	}

	// Every weapon shares ItemTypeWeapon since the gear keystone (#55/#56);
	// Tags names which attacks fire it — a ranged or magic tag means this
	// held weapon fires the ranged/AoE attack path.
	for _, it := range e.Items {
		if !it.Equipped || it.Type != protocol.ItemTypeWeapon {
			continue
		}

		for _, tag := range it.Tags {
			if tag == protocol.WeaponTagRanged || tag == protocol.WeaponTagMagic {
				return it.RangeHex, it.AoERadius, true
			}
		}
	}

	return 0, 0, false
}

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
// intent) at target if it is within range, else move toward it. Returns
// whether the shot was accepted (fired in range this turn); a rejection
// increments *consecutiveRejected and, past 40 in a row, fails the test loudly
// with the rejection body — a real regression (e.g. the range check flipped)
// should not silently exhaust the whole deadline before failing.
func fireBowOrChase(
	t *testing.T, ts *httptest.Server, me protocol.JoinResponse,
	myHex, target protocol.Hex, bowRange int, consecutiveRejected *int,
) bool {
	t.Helper()

	if hexDistance(myHex, target) > bowRange {
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
			"rogue attack intent rejected repeatedly (status %d, %q); range check: dist=%d, range=%d",
			resp.StatusCode, body.Error, hexDistance(myHex, target), bowRange,
		)
	}

	return false
}

// TestRogueBowKillsAtRange exercises the rogue's ranged bow attack over real
// HTTP/SSE: a joined rogue targets whichever monster is nearest (recomputed
// every bundle, since monsters hunt back and a fixed target can go stale) and,
// once within range, fires an "attack" intent instead of moving.
//
// Because an attack intent clears the rogue's route (queueAttackLocked), the
// rogue never bumps while it is firing — so any monster HP drop observed on a
// turn where the rogue fired is attributable solely to the ranged shot
// (resolveBowLocked), proving the bow path lands over real HTTP, not just a
// disguised bump.
//
// The monster is seeded two hexes from the origin (where the rogue spawns) —
// already inside range, so the rogue fires from range on its first bubble
// lock-in rather than after a long crypto-random chase gated on the background
// tick loop. That makes the ranged kill deterministic and robust even under a
// CPU-starved runner (#22). The test is not parallel so its tick loop is not
// starved by sibling servers.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestRogueBowKillsAtRange(t *testing.T) {
	ts := startServerWithMonstersAt(t, protocol.Hex{Q: 0, R: -2})

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

	bowRange, _, ok := equippedRangedStats(first, me.EntityID)
	if !ok {
		t.Fatal("joined rogue has no equipped ranged item in first turn bundle")
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
			if fireBowOrChase(t, ts, me, myHex, target, bowRange, &consecutiveRejected) {
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
// of the given hostile hexes — currently has the most hostiles within the AoE
// radius, maximizing the mage's chance of a same-turn multi-hit given
// the CURRENT board (recomputed every bundle, since monsters keep moving).
// hostiles must be non-empty; the caster's own hex always counts itself as a
// covering candidate (distance 0), and every hostile hex trivially covers
// itself, so the returned count is always >= 1.
func bestAoETarget(mine protocol.Hex, hostiles []protocol.Hex, aoeRadius int) (protocol.Hex, int) {
	countWithin := func(h protocol.Hex) int {
		n := 0

		for _, m := range hostiles {
			if hexDistance(h, m) <= aoeRadius {
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
// if that hex is within range, else chase the nearest monster into range.
// A rejection increments *consecutiveRejected and, past 40 in a row, fails the
// test loudly instead of silently exhausting the whole deadline.
func fireAoEOrChase(
	t *testing.T, ts *httptest.Server, me protocol.JoinResponse,
	bundle protocol.TurnEvent, myHex protocol.Hex, monsterHexes []protocol.Hex,
	mageRange, aoeRadius int, consecutiveRejected *int,
) {
	t.Helper()

	candidate, _ := bestAoETarget(myHex, monsterHexes, aoeRadius)

	if hexDistance(myHex, candidate) > mageRange {
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

// rotateHex60CW rotates h 60° clockwise around the origin (Red Blob Games'
// cube-coordinate rotation formula: (x,y,z) -> (-z,-x,-y), with y implicit
// as -x-z on the q/r axial pair) — used by placeMonsterClusterNear to try a
// fixed two-hex cluster shape in all six directions around a spawn hex,
// since any one direction might land on water/rock.
func rotateHex60CW(h protocol.Hex) protocol.Hex {
	x, z := h.Q, h.R
	y := -x - z

	return protocol.Hex{Q: -z, R: -y}
}

// placeMonsterClusterNear seeds two monsters on adjacent hexes two steps out
// from center — the fixed shape TestMageAoEDamagesMonsters needs — trying
// all six rotations around center until one lands both monsters on walkable,
// unfull hexes (SpawnMonsterAt refuses water/rock/StackCap). Reports whether
// any rotation succeeded.
func placeMonsterClusterNear(world *game.World, center protocol.Hex) bool {
	offset1 := protocol.Hex{Q: 0, R: -2}
	offset2 := protocol.Hex{Q: 1, R: -2}

	for range 6 {
		h1 := protocol.Hex{Q: center.Q + offset1.Q, R: center.R + offset1.R}
		h2 := protocol.Hex{Q: center.Q + offset2.Q, R: center.R + offset2.R}

		if world.SpawnMonsterAt(h1) && world.SpawnMonsterAt(h2) {
			return true
		}

		offset1, offset2 = rotateHex60CW(offset1), rotateHex60CW(offset2)
	}

	return false
}

// TestMageAoEDamagesMonsters exercises the mage's ranged AoE attack over real
// HTTP/SSE: a joined mage fires at whichever hex currently covers the most
// hostiles within AoE radius (bestAoETarget, recomputed every bundle),
// falling back to chasing the nearest monster into range otherwise.
//
// It requires TWO OR MORE monsters to take damage in the SAME turn (a bump or
// a bow always resolves to exactly one victim, so a same-turn multi-drop is
// only possible via the AoE) — not just the single-hit trend, matching the
// unit-level "2 monsters in radius, both take damage, no friendly fire" case
// pinned deterministically in internal/game's ranged_test.go.
//
// Two monsters are seeded on adjacent hexes two steps from the mage's spawn
// hex. The spawn hex is random since #36 (no longer always the origin), so
// this test joins FIRST over HTTP to learn where it actually landed, then
// seeds the cluster relative to THAT hex via a direct world reference. They
// beeline for the mage as one cluster (bubble.go: every monster within
// CombatRadius joins one bubble), so on an early bubble-turn they sit within
// a single AoE-radius footprint the mage can reach — the mage fires at the
// hex covering the most hostiles (bestAoETarget, recomputed every bundle) and
// drops both at once. Seeding a known cluster next to the spawn makes the
// same-turn multi-hit deterministic and robust even under a CPU-starved
// runner (#22), rather than depending on where crypto/rand scattered 16
// monsters. The test is not parallel so its tick loop is not starved by
// sibling servers.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestMageAoEDamagesMonsters(t *testing.T) {
	ticks := hub.New()
	world := game.NewWorld(10*time.Millisecond, time.Minute, 5*time.Millisecond, testDisconnectGrace, 0xC0FFEE, 12, ticks)

	chatBroker := newAnnouncingChatBroker(world)
	go world.Run(t.Context())

	handler := server.New(server.Deps{
		Logger: slog.New(slog.DiscardHandler), World: world, Ticks: ticks, Chat: chatBroker,
		HeartbeatInterval: time.Hour,
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	me := joinClass(t, ts, "", protocol.ClassMage)

	if !placeMonsterClusterNear(world, me.Hex) {
		t.Fatalf("SpawnMonsterAt refused every rotation of the cluster near the mage's spawn %v", me.Hex)
	}

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

	mageRange, aoeRadius, ok := equippedRangedStats(first, me.EntityID)
	if !ok {
		t.Fatal("joined mage has no equipped ranged item in first turn bundle")
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
			fireAoEOrChase(t, ts, me, bundle, myHex, monsterHexes, mageRange, aoeRadius, &consecutiveRejected)
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
