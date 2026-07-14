package integration_test

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
	"github.com/starquake/mediumrogue/internal/server"
)

// decodeTurnFrame reads SSE frames until a turn frame arrives, skipping any
// other named event. Gear pickups announce over the same chat broker as
// quests (pickupLocked's w.announce call, world.go), so a chat frame can ride
// this stream exactly like the quest announcement quest_kill_test.go's local
// decodeTurn was written to skip; decodeBundle (bubble_test.go) would
// mis-decode a chat frame as an empty TurnEvent. Package-level (unlike that
// local closure) since every test in this file needs it.
func decodeTurnFrame(t *testing.T, r *bufio.Reader) protocol.TurnEvent {
	t.Helper()

	for {
		frames := readFrames(t, r, 1)
		if frames[0].event != protocol.EventTurn {
			continue
		}

		var bundle protocol.TurnEvent
		if err := json.Unmarshal([]byte(frames[0].data), &bundle); err != nil {
			t.Fatalf("unmarshal bundle %q: %v", frames[0].data, err)
		}

		return bundle
	}
}

// TestEquipOverHTTP proves the equip intent's toggle semantics (item 2) over
// real HTTP/SSE: join, grab an owned item id straight off the first turn
// bundle (every class starts pre-equipped with its defaults —
// grantDefaultsLocked), equip it AGAIN — which now unequips it, since an
// equip intent naming an already-equipped item toggles it off — then equip
// it a third time to prove the round trip back to equipped:true. Both swaps
// apply synchronously outside a bubble (queueEquipLocked), so each is
// visible on the very next turn bundle.
func TestEquipOverHTTP(t *testing.T) {
	t.Parallel()

	ts := startServer(t, 20*time.Millisecond, time.Hour)

	me := joinClass(t, ts, "", protocol.ClassRogue)

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	first := decodeTurnFrame(t, reader)

	myEntity, ok := entityOf(first, me.EntityID)
	if !ok {
		t.Fatal("joined rogue missing from first turn bundle")
	}

	if len(myEntity.Items) == 0 {
		t.Fatal("rogue joined with no items — class defaults not granted")
	}

	item := myEntity.Items[0]
	if !item.Equipped {
		t.Fatalf("item %d (%s) not equipped by default — class defaults are granted pre-equipped", item.ID, item.DefID)
	}

	intent := protocol.IntentRequest{
		EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentEquip, ItemID: item.ID,
	}

	resp := postJSON(t, ts, "/api/intent", intent)
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("equip intent status = %d, want %d", got, want)
	}

	next := decodeTurnFrame(t, reader)

	if got := equippedFlag(t, next, me.EntityID, item.ID); got {
		t.Fatalf("item %d still shows equipped:true after toggling an already-equipped item off", item.ID)
	}

	// Toggle it back on: the round trip.
	resp = postJSON(t, ts, "/api/intent", intent)
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("equip intent (toggle on) status = %d, want %d", got, want)
	}

	final := decodeTurnFrame(t, reader)

	if got := equippedFlag(t, final, me.EntityID, item.ID); !got {
		t.Fatalf("item %d still shows equipped:false after toggling it back on", item.ID)
	}
}

// equippedFlag returns itemID's Equipped flag for entityID in bundle,
// failing the test if either the entity or the item is missing from the
// wire.
func equippedFlag(t *testing.T, bundle protocol.TurnEvent, entityID, itemID int64) bool {
	t.Helper()

	e, ok := entityOf(bundle, entityID)
	if !ok {
		t.Fatalf("entity %d missing from turn bundle", entityID)
	}

	for _, it := range e.Items {
		if it.ID == itemID {
			return it.Equipped
		}
	}

	t.Fatalf("item %d vanished from the wire", itemID)

	return false
}

// TestEquipValidation proves the two equip-intent rejections left reachable
// without a drop: an itemId the player doesn't own (ErrItemNotOwned), and an
// intent Kind the server doesn't recognize at all (ErrInvalidIntentKind) —
// class gates are gone entirely (gear keystone, #56), so there is no
// wrong-class rejection to prove here anymore.
func TestEquipValidation(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)
	me := joinClass(t, ts, "", protocol.ClassFighter)

	t.Run("unowned item id", func(t *testing.T) {
		t.Parallel()

		intent := protocol.IntentRequest{
			EntityID: me.EntityID, Token: me.Token, Kind: protocol.IntentEquip, ItemID: 999_999,
		}

		resp := postJSON(t, ts, "/api/intent", intent)
		if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
			t.Fatalf("equip unowned-item status = %d, want %d", got, want)
		}
	})

	t.Run("unknown intent kind", func(t *testing.T) {
		t.Parallel()

		intent := protocol.IntentRequest{
			EntityID: me.EntityID, Token: me.Token, Kind: "teleport", ItemID: 0,
		}

		resp := postJSON(t, ts, "/api/intent", intent)
		if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
			t.Fatalf("unknown-kind intent status = %d, want %d", got, want)
		}
	})
}

// dropFarmMonsterCount is how many monsters TestDropPickupLoop pre-seeds
// around spawn. DropChancePercent = 30, so each independent kill is a 30%
// trial; with this many tries the chance of every single one whiffing —
// 0.7^24 — is under 0.02%, comfortably below normal CI flake budgets.
const dropFarmMonsterCount = 24

// startGearServerWithMonsterRing is startServerWithMonstersAt (testmain_test.go)
// but tolerant of unwalkable candidates: it places up to want monsters, trying
// candidates in order and skipping any SpawnMonsterAt refuses (water/rock or,
// this deep into a ring, occasionally StackCap), rather than hard-failing on
// the first miss the way startServerWithMonstersAt does for its short,
// hand-picked hex lists. All placement happens before world.Run starts and
// before any player joins — the same "startup, before any player" contract
// SpawnMonsterAt documents — so there is no risk of a monster the world just
// spawned pathing onto a hex an *already-bubbled* player occupies (byHex for
// the WORLD domain doesn't include a bubbled entity, so a same-domain
// occupancy check can't see it — SpawnMonsterAt calling this mid-run, after
// players have already joined and bubbled, produced exactly that stall
// chasing the fix for this test down).
func startGearServerWithMonsterRing(
	t *testing.T, turnInterval time.Duration, want int, candidates []protocol.Hex,
) *httptest.Server {
	t.Helper()

	ticks := hub.New()

	world := game.NewWorld(turnInterval, time.Minute, 5*time.Millisecond, testDisconnectGrace, 0xC0FFEE, 12, ticks)

	placed := 0
	for _, h := range candidates {
		if placed >= want {
			break
		}

		if world.SpawnMonsterAt(h) {
			placed++
		}
	}

	if placed < want {
		t.Fatalf("placed %d/%d monsters from %d candidates — ring too small or too much unwalkable terrain near spawn",
			placed, want, len(candidates))
	}

	chatBroker := newAnnouncingChatBroker(world)
	go world.Run(t.Context())

	handler := server.New(server.Deps{
		Logger: slog.New(slog.DiscardHandler), World: world, Ticks: ticks, Chat: chatBroker,
		HeartbeatInterval: time.Hour,
	})

	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)

	return ts
}

// spawnRingCandidates returns every hex within maxRing steps of origin,
// nearest ring first (origin's own 6 neighbors, then the 12 two away, then
// the 18 three away, ...) — more candidates than dropFarmMonsterCount needs,
// so a handful landing on water/rock (SpawnMonsterAt refuses those) still
// leaves enough to reach the target count.
func spawnRingCandidates(origin protocol.Hex, maxRing int) []protocol.Hex {
	seen := map[protocol.Hex]bool{origin: true}
	frontier := []protocol.Hex{origin}

	// Ring i (1-indexed) has 6*i hexes, so maxRing rings hold
	// 6*(1+2+...+maxRing) = 3*maxRing*(maxRing+1) total.
	out := make([]protocol.Hex, 0, 3*maxRing*(maxRing+1))

	for range maxRing {
		var next []protocol.Hex

		for _, h := range frontier {
			for _, n := range neighborsOf(h) {
				if !seen[n] {
					seen[n] = true

					next = append(next, n)
				}
			}
		}

		out = append(out, next...)
		frontier = next
	}

	return out
}

// TestDropPickupLoop drives the full milestone 6b.4 loot loop over real
// HTTP/SSE: kill monsters, see a drop land in TurnEvent.GroundItems, walk
// onto its hex, and see it land in the player's own Entity.Items — the
// pickup phase pinned at the unit level (drops_test.go) now proven over the
// real handler tree.
//
// Determinism note (see task-6-report.md for the full writeup): the task
// plan assumed the drop roll could be pinned the way worldgen/quests are —
// PCG(WORLD_SEED, turn). That doesn't hold here. NewWorld's worldSeed
// parameter feeds GenerateMap and generateQuests only; the *World.seed field
// dropLootLocked's rng actually uses is minted from crypto/rand inside
// NewWorld regardless of worldSeed (world.go), and the test-only override
// (SetSeedForTest) lives in export_test.go — a _test.go file in package
// game, invisible to this package (Go only compiles _test.go files into that
// package's own test binary, never into an importer). So there is no seed
// available to hunt from HTTP. Instead this test pre-seeds a whole ring of
// monsters (dropFarmMonsterCount) around spawn and farms kills (bump combat,
// like TestCombatOverHTTP) until one of them drops — DropChancePercent's 30%
// across that many independent tries makes a miss a sub-0.02% event, not a
// coin flip against the deadline (see dropFarmMonsterCount's doc comment). A
// player that dies mid-farm just respawns at full HP (resolveDeathsLocked)
// and the loop keeps going; nothing here depends on the player surviving any
// one fight. Since #36, spawnHexLocked picks randomly across the sanctuary
// rather than always the origin itself (and never lands on top of a
// living monster when avoidable, though a heavily monster-saturated area
// like this ring can still force that as a last resort) — occasionally
// co-locating the player with the very monster that goes on to drop the
// loot, so the player can already be standing on the drop's hex the instant
// it lands. See the pickup loop's postIntent-every-turn comment below for why
// that matters.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestDropPickupLoop(t *testing.T) {
	origin := protocol.Hex{Q: 0, R: 0} // the ring is seeded around the origin spawn area

	ts := startGearServerWithMonsterRing(t, 15*time.Millisecond, dropFarmMonsterCount, spawnRingCandidates(origin, 4))

	me := join(t, ts, "")

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	deadline := time.Now().Add(60 * time.Second)

	var dropped protocol.GroundItemView

	for dropped.ID == 0 && time.Now().Before(deadline) {
		bundle := decodeTurnFrame(t, reader)

		if len(bundle.GroundItems) > 0 {
			dropped = bundle.GroundItems[0]

			break
		}

		myHex := hexOf(bundle, me.EntityID)
		if myHex == (protocol.Hex{Q: -999, R: -999}) {
			t.Fatal("joined player missing from turn bundle")
		}

		target, found := nearestMonster(bundle, myHex)
		if !found {
			t.Fatal("every pre-seeded monster died with no drop — statistically shouldn't happen " +
				"(see dropFarmMonsterCount's doc comment); bump DropFarmMonsterCount or re-check DropChancePercent")
		}

		postIntent(t, ts, me, target)
	}

	if dropped.ID == 0 {
		t.Fatalf("no monster dropped loot before the %s deadline", 60*time.Second)
	}

	t.Logf("drop landed: item %d (%s) at %v", dropped.ID, dropped.DefID, dropped.Hex)

	// Walk onto the drop's hex, then claim it with an explicit PICKUP intent
	// (the inventory-slots milestone removed walk-over auto-pickup): outside a
	// bubble the pickup applies immediately; inside one it is the player's
	// action for that turn — either way the very submission doubles as the
	// bubble lock-in, so this loop never stalls an action-gated bubble.
	// Generous deadline as a safety margin against a CPU-starved runner (#22).
	pickupDeadline := time.Now().Add(30 * time.Second)

	for time.Now().Before(pickupDeadline) {
		bundle := decodeTurnFrame(t, reader)

		ent, ok := entityOf(bundle, me.EntityID)
		if !ok {
			t.Fatal("joined player missing from turn bundle")
		}

		for _, it := range ent.Items {
			if it.ID == dropped.ID {
				return // picked up — the full drop/pickup loop proven end to end
			}
		}

		// Submit every turn: a move toward the drop until this bundle shows
		// the player standing on its hex, then pickup intents (repeated —
		// the first may resolve on a later bubble turn, and a stale ground
		// id after someone took it would 422, which pickupOrMoveIntent
		// treats as fatal so a real bug still fails fast). Submitting every
		// bundle also keeps feeding lock-ins to any bubble the player is in
		// (other ring monsters still alive) so it never stalls on the AFK
		// patience timeout.
		if ent.Hex == dropped.Hex {
			postPickupIntent(t, ts, me, dropped.ID)
		} else {
			postIntent(t, ts, me, dropped.Hex)
		}
	}

	t.Fatalf("player never picked up item %d (dropped at %v) before the pickup deadline", dropped.ID, dropped.Hex)
}

// postPickupIntent submits an explicit pickup intent for a ground item id
// (the inventory-slots milestone's replacement for walk-over auto-pickup).
func postPickupIntent(t *testing.T, ts *httptest.Server, me protocol.JoinResponse, groundItemID int64) {
	t.Helper()

	intent := protocol.IntentRequest{
		Kind: protocol.IntentPickup, EntityID: me.EntityID, Token: me.Token, GroundItemID: groundItemID,
	}

	resp := postJSON(t, ts, "/api/intent", intent)
	if got, want := resp.StatusCode, http.StatusAccepted; got != want {
		t.Fatalf("pickup intent status = %d, want 202", got)
	}
}
