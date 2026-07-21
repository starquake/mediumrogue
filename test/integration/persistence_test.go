package integration_test

// persistence_test.go: milestone 10a Task 4 — the world survives a restart
// and a character survives a sweep, proven over real HTTP against two
// independent server instances sharing one snapshot file. The harness
// constructs the *game.World directly (as every other file in this package
// does), so persistence itself is driven exactly the way cmd/rogue/app
// drives it: World.MarshalState/RestoreState plus a plain file on disk —
// see persistWorld below.

import (
	"bufio"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// persistSeed and persistRadius are shared by every world this file builds:
// RestoreState refuses a snapshot whose worldSeed/worldRadius don't match
// the target world, so server A and server B must agree on both — exactly
// the constraint a real deploy's WORLD_SEED/WORLD_RADIUS env vars satisfy by
// staying fixed across a restart.
const (
	persistSeed   = 0xC0FFEE
	persistRadius = 12
)

// persistWorld bundles a *game.World with its ticks hub, held without starting
// the control loop yet — so a caller can place monsters or call RestoreState
// (both of which require a not-yet-running world) before calling serve.
type persistWorld struct {
	world *game.World
	ticks *hub.Hub
}

// newPersistWorld builds (but does not start) a world for a restart test. The
// announcing chat broker is wired by serve (via serveWorld) right before
// Run — SetAnnounce's before-Run contract still holds, and nothing between
// build and serve announces (monster placement and RestoreState do not).
func newPersistWorld(t *testing.T) *persistWorld {
	t.Helper()

	ticks := hub.New()
	world := game.NewWorld(game.WorldConfig{
		Interval:        15 * time.Millisecond,
		CombatPatience:  time.Minute,
		BubblePoll:      5 * time.Millisecond,
		DisconnectGrace: testDisconnectGrace,
		WorldSeed:       persistSeed,
		Radius:          persistRadius,
		Ticks:           ticks,
	})

	return &persistWorld{world: world, ticks: ticks}
}

// serve starts pw's control loop and an httptest.Server in front of it —
// the point of no return for placing monsters or restoring a snapshot.
func (pw *persistWorld) serve(t *testing.T) *httptest.Server {
	t.Helper()

	return serveWorld(t, pw.world, pw.ticks, server.Deps{HeartbeatInterval: time.Hour})
}

// monsterVital is the (kind, HP) pair a restart must reproduce exactly for
// every surviving monster — the design's "must not respawn a healed,
// repositioned monster population" guarantee, checked on HP/kind (not exact
// hex: the restored world's control loop may have already ticked a step or
// two of AI movement by the time the test reads server B's first bundle).
type monsterVital struct {
	kind string
	hp   int
}

func monsterVitalsOf(bundle protocol.TurnEvent) map[int64]monsterVital {
	out := make(map[int64]monsterVital)

	for _, e := range bundle.Entities {
		if e.Kind == protocol.EntityMonster {
			out[e.ID] = monsterVital{kind: e.MonsterKind, hp: e.HP}
		}
	}

	return out
}

// TestWorldSurvivesRestartCharacterSurvivesSweep is milestone 10a's
// end-to-end proof over real HTTP: server A accumulates real state (a
// player earns XP and picks up a dropped item farming one-on-one wolf
// fights — adapted from gear_test.go's TestDropPickupLoop technique — and
// takes a reach quest so its board holder reference has something to
// preserve), snapshots to a plain file, and server B restores from that
// file. A token rejoin on server B reclaims the SAME character (identity,
// XP, gear, unchanged) — RestoreState brings the player back as a
// live-but-disconnected entity, so Join takes its ordinary live-token
// reclaim path, not the archive-restore path (that lifecycle is
// archive_test.go's job, at the unit level). Monsters, ground items, and the
// quest board match what server A had at snapshot time, and a brand-new
// token still joins normally on server B.
//
//nolint:paralleltest // serial by design (#22, matches TestDropPickupLoop): tick loop must not be CPU-starved.
func TestWorldSurvivesRestartCharacterSurvivesSweep(t *testing.T) {
	pwA := newPersistWorld(t)
	tsA := pwA.serve(t)

	me := join(t, tsA, "")

	readerA := bufio.NewReader(get(t, tsA, "/api/events").Body)
	first := decodeTurnFrame(t, readerA)

	// Take a REACH quest specifically: the fight below must not touch its
	// progress or holder, so its state is a clean pin for "the quest
	// board's holder references a persisted entity id" (the design's own
	// phrasing for what must survive a restart).
	reachQuest := closestReachQuest(t, first, me.Hex)

	resp := postJSON(t, tsA, "/api/chat",
		protocol.ChatRequest{Token: me.Token, Text: fmt.Sprintf("/quest %d", reachQuest.ID)})
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("/quest status = %d, want %d", got, want)
	}

	// Farm one-on-one wolf fights (a fresh wolf placed adjacent every retry,
	// never more than one alive at once) for a kill (XP) and a drop (ground
	// item), then walk onto the drop to pick it up. One-on-one, not a
	// pre-seeded ring: a level-1 Fighter (30 HP, iron sword) beats a lone
	// wolf (10 HP, 3 dmg) outright, so there is no risk of the death/respawn
	// XP floor (resolveDeathsLocked) wiping the very XP this test asserts
	// on — see TestDropPickupLoop (gear_test.go) for the ring-farm variant
	// this technique is adapted from, and its determinism note on why the
	// drop roll itself can't be pinned directly.
	preSnap := farmKillAndPickup(t, tsA, pwA.world, readerA, me)

	preMe, ok := entityOf(preSnap, me.EntityID)
	if !ok {
		t.Fatalf("joined player %d missing from pre-snapshot bundle", me.EntityID)
	}

	if preMe.XP == 0 {
		t.Fatal("test setup: player earned no XP farming for a kill")
	}

	preMonsters := monsterVitalsOf(preSnap)

	data, err := pwA.world.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write snapshot file: %v", err)
	}

	// "server B from the file": a fresh world, restored before its control
	// loop starts (RestoreState's contract) and given NO monsters of its
	// own — the restore already brings back the persisted population,
	// mirroring cmd/rogue/app's loadSnapshot-skips-SpawnMonsters wiring.
	//nolint:gosec // path is a t.TempDir() file this test wrote itself, not user input.
	loaded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot file: %v", err)
	}

	pwB := newPersistWorld(t)
	if err := pwB.world.RestoreState(loaded); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	tsB := pwB.serve(t)

	back := join(t, tsB, me.Token)
	if got, want := back.EntityID, me.EntityID; got != want {
		t.Errorf("rejoined EntityID = %d, want %d (same restored entity, live reclaim not a new join)", got, want)
	}

	postSnap := decodeTurnFrame(t, bufio.NewReader(get(t, tsB, "/api/events").Body))

	postMe, ok := entityOf(postSnap, back.EntityID)
	if !ok {
		t.Fatalf("rejoined player %d missing from server B's first bundle", back.EntityID)
	}

	if got, want := postMe.Name, preMe.Name; got != want {
		t.Errorf("restored Name = %q, want %q", got, want)
	}

	if got, want := postMe.Class, preMe.Class; got != want {
		t.Errorf("restored Class = %q, want %q", got, want)
	}

	if got, want := postMe.Species, preMe.Species; got != want {
		t.Errorf("restored Species = %q, want %q", got, want)
	}

	if got, want := postMe.XP, preMe.XP; got != want {
		t.Errorf("restored XP = %d, want %d", got, want)
	}

	if got, want := len(postMe.Items), len(preMe.Items); got != want {
		t.Errorf("restored item count = %d, want %d", got, want)
	}

	for _, want := range preMe.Items {
		found := false

		for _, got := range postMe.Items {
			if got.ID == want.ID && got.DefID == want.DefID && got.Equipped == want.Equipped {
				found = true
			}
		}

		if !found {
			t.Errorf("restored items missing pre-snapshot item %d (%s)", want.ID, want.DefID)
		}
	}

	postMonsters := monsterVitalsOf(postSnap)
	if got, want := len(postMonsters), len(preMonsters); got != want {
		t.Errorf("restored monster count = %d, want %d", got, want)
	}

	for id, want := range preMonsters {
		got, ok := postMonsters[id]
		if !ok {
			t.Errorf("restored monsters missing pre-snapshot monster %d (%s, %d hp)", id, want.kind, want.hp)

			continue
		}

		if got != want {
			t.Errorf("restored monster %d = %+v, want %+v (not healed/respawned)", id, got, want)
		}
	}

	if got, want := len(postSnap.GroundItems), len(preSnap.GroundItems); got != want {
		t.Errorf("restored ground item count = %d, want %d", got, want)
	}

	postQuest, ok := questByID(postSnap, reachQuest.ID)
	if !ok {
		t.Fatalf("restored quest %d missing from server B's first bundle", reachQuest.ID)
	}

	if got, want := postQuest.State, protocol.QuestTaken; got != want {
		t.Errorf("restored quest %d state = %q, want %q", reachQuest.ID, got, want)
	}

	if got, want := postQuest.HolderEntityID, back.EntityID; got != want {
		t.Errorf("restored quest %d HolderEntityID = %d, want %d (references the persisted entity id)",
			reachQuest.ID, got, want)
	}

	// A brand-new (never-seen) token still joins normally on server B.
	other := join(t, tsB, "")
	if got, notWant := other.EntityID, back.EntityID; got == notWant {
		t.Errorf("fresh join EntityID = %d, want a different id than the restored character's %d", got, notWant)
	}
}

// persistFarmTries is how many one-on-one wolf fights farmKillAndPickup will
// spend hunting a drop. DropChancePercent = 30, so each independent kill is
// a 30% trial; with this many tries the chance of every single one whiffing
// — 0.7^40 — is under 0.0001%, comfortably below normal CI flake budgets
// (mirrors gear_test.go's dropFarmMonsterCount, at a smaller per-fight risk
// footprint — see farmKillAndPickup's doc comment).
const persistFarmTries = 40

// farmKillAndPickup places a single wolf adjacent to me, melee-fights it to
// death (a level-1 Fighter's 30 HP / iron-sword-4-dmg outright beats a lone
// wolf's 10 HP / 3 dmg — no death/respawn risk, unlike a pre-seeded ring of
// many at once), and repeats with a fresh wolf if that one didn't drop loot,
// up to persistFarmTries times. Once a drop lands it walks onto the hex to
// pick it up, and returns the turn bundle from that moment — the
// pre-snapshot state the caller compares against server B's restore.
// world is the SAME *game.World behind ts (the harness constructs it
// directly), so this can place monsters mid-run exactly like
// monster_test.go's SpawnMonsterAt calls after join.
func farmKillAndPickup(
	t *testing.T, ts *httptest.Server, world *game.World, reader *bufio.Reader, me protocol.JoinResponse,
) protocol.TurnEvent {
	t.Helper()

	var dropped protocol.GroundItemView

	myHex := me.Hex

	for try := 0; try < persistFarmTries && dropped.ID == 0; try++ {
		spawnAdjacentWolf(t, world, myHex)

		fightDeadline := time.Now().Add(5 * time.Second)

		for {
			if time.Now().After(fightDeadline) {
				t.Fatalf("wolf fight %d/%d did not resolve before its deadline", try+1, persistFarmTries)
			}

			bundle := decodeTurnFrame(t, reader)

			if len(bundle.GroundItems) > 0 {
				dropped = bundle.GroundItems[0]

				break
			}

			hex := hexOf(bundle, me.EntityID)
			if hex == (protocol.Hex{Q: -999, R: -999}) {
				t.Fatal("joined player missing from turn bundle")
			}

			myHex = hex

			id, target, found := nearestMonsterID(bundle, myHex)
			if !found {
				break // this wolf died with no drop; the outer loop places another
			}

			if hexDistance(myHex, target) == 1 {
				postEntityAttackIntent(t, ts, me, id)
			} else {
				postIntent(t, ts, me, target)
			}
		}
	}

	if dropped.ID == 0 {
		t.Fatalf("no wolf dropped loot in %d one-on-one fights — statistically shouldn't happen "+
			"(see persistFarmTries's doc comment)", persistFarmTries)
	}

	pickupDeadline := time.Now().Add(10 * time.Second)

	for {
		if time.Now().After(pickupDeadline) {
			t.Fatalf("player never picked up item %d (dropped at %v) before the pickup deadline", dropped.ID, dropped.Hex)
		}

		bundle := decodeTurnFrame(t, reader)

		ent, ok := entityOf(bundle, me.EntityID)
		if !ok {
			t.Fatal("joined player missing from turn bundle")
		}

		for _, it := range ent.Items {
			if it.ID == dropped.ID {
				return bundle // picked up
			}
		}

		// Walk to the drop, then claim it with an explicit pickup intent
		// (walk-over auto-pickup is gone — the inventory-slots milestone);
		// resubmitting every bundle keeps feeding bubble lock-ins, mirroring
		// TestDropPickupLoop (gear_test.go).
		if ent.Hex == dropped.Hex {
			postPickupIntent(t, ts, me, dropped.ID)
		} else {
			postIntent(t, ts, me, dropped.Hex)
		}
	}
}

// spawnAdjacentWolf places one wolf on a walkable neighbor of near (trying
// every direction in order), so farmKillAndPickup's next fight starts in
// melee range immediately instead of needing a chase. Fails the test if
// every neighbor is refused (water/rock/StackCap) — near comes from a live
// turn bundle, so this should only happen on a pathologically cramped map.
func spawnAdjacentWolf(t *testing.T, world *game.World, near protocol.Hex) {
	t.Helper()

	if !slices.ContainsFunc(neighborsOf(near), world.SpawnMonsterAt) {
		t.Fatalf("SpawnMonsterAt refused every neighbor of %v — no room to place the next wolf", near)
	}
}
