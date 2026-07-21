package game_test

import (
	"errors"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// throwables_test.go (#271, slice 5): the throwable flask and the scroll of
// recall — the targeted-consumable action path, end to end through the real
// resolution pipeline.

const (
	flaskOfFireID    = "flask-of-fire"
	scrollOfRecallID = "scroll-of-recall"
	burningEffectID  = "burning"
)

// The Flask of Alchemist's Fire's authored payload (content.go); duplicated
// here so a tuning change to the flask fails these pins loudly and on purpose.
const (
	flaskRange      = 4
	flaskDamage     = 6
	flaskBurnMag    = -3
	flaskBurnTurns  = 3
	defaultThrowSep = 3 // in range (<= flaskRange), leaves room for a controlled sight line
)

// throwIntent builds a "throw" IntentRequest: ItemID names the flask, Target
// the aim hex.
func throwIntent(id int64, token string, itemID int64, target protocol.Hex) protocol.IntentRequest {
	return protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentThrow, ItemID: itemID, Target: target,
	}
}

// recallIntent builds a "recall" IntentRequest: ItemID names the scroll; no
// target (recall teleports to a server-chosen safe hex).
func recallIntent(id int64, token string, itemID int64) protocol.IntentRequest {
	return protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentRecall, ItemID: itemID,
	}
}

// backpackCount sums the units of defID across an entity's backpack.
func backpackCount(w *game.World, id int64, defID string) int {
	total := 0

	for _, e := range w.BackpackForTest(id) {
		if e.DefID == defID {
			total += e.Count
		}
	}

	return total
}

// TestThrowFlaskBlastsAndBurns is the whole point: a thrown flask deals its
// fire damage to a monster on the aim hex NOW and leaves a burning DoT that
// first bites on the NEXT turn's tick — and the flask is spent.
func TestThrowFlaskBlastsAndBurns(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: defaultThrowSep, R: 0}
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0}, monsterHex)

	id, token := w.PlaceEntityForTest(origin)
	flask := w.GrantItemForTest(id, flaskOfFireID)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(throwIntent(id, token, flask, monsterHex)); err != nil {
		t.Fatalf("SubmitIntent(throw): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	// Immediate blast: 6 fire on a 10-HP wolf, no fire modifier → flat.
	if got, want := w.HPForTest(monsterID), protocol.MonsterMaxHP-flaskDamage; got != want {
		t.Errorf("monster HP after blast = %d, want %d", got, want)
	}

	// The burning DoT is applied (buffered, then applied after the tick), so it
	// is present with its full duration but has NOT drained yet this turn.
	mag, turns, ok := w.EffectForTest(monsterID, burningEffectID)
	if !ok {
		t.Fatalf("monster has no burning effect after the blast")
	}

	if mag != flaskBurnMag || turns != flaskBurnTurns {
		t.Errorf("burning = (mag %d, turns %d), want (%d, %d)", mag, turns, flaskBurnMag, flaskBurnTurns)
	}

	// The flask is consumed.
	if got := backpackCount(w, id, flaskOfFireID); got != 0 {
		t.Errorf("flask count after throw = %d, want 0 (consumed)", got)
	}

	// Next turn the DoT bites: -3 more, duration ticks down.
	w.ResolveCombatOnlyForTest()

	if got, want := w.HPForTest(monsterID), protocol.MonsterMaxHP-flaskDamage+flaskBurnMag; got != want {
		t.Errorf("monster HP after one burn tick = %d, want %d", got, want)
	}

	if _, turns, _ := w.EffectForTest(monsterID, burningEffectID); turns != flaskBurnTurns-1 {
		t.Errorf("burning turns after one tick = %d, want %d", turns, flaskBurnTurns-1)
	}
}

// TestThrowFlaskHitsEveryEnemyInBlast: AoE always hits (the ARPG identity) —
// two monsters within the flask's radius of the aim hex both take the damage,
// no to-hit roll.
func TestThrowFlaskHitsEveryEnemyInBlast(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	aim := protocol.Hex{Q: defaultThrowSep, R: 0}
	adjacent := protocol.Hex{Q: defaultThrowSep, R: 1} // distance 1 from aim, within the flask's blast radius
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0}, aim, adjacent)

	id, token := w.PlaceEntityForTest(origin)
	flask := w.GrantItemForTest(id, flaskOfFireID)
	centerID := w.PlaceMonsterForTest(aim)
	splashID := w.PlaceMonsterForTest(adjacent)

	if err := w.SubmitIntent(throwIntent(id, token, flask, aim)); err != nil {
		t.Fatalf("SubmitIntent(throw): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	for _, m := range []int64{centerID, splashID} {
		if got, want := w.HPForTest(m), protocol.MonsterMaxHP-flaskDamage; got != want {
			t.Errorf("blast victim %d HP = %d, want %d (AoE always hits)", m, got, want)
		}
	}
}

// TestThrowFlaskNoFriendlyFire: a throw never harms the thrower or an ally
// standing in the blast — resolveAoELocked is faction-aware.
func TestThrowFlaskNoFriendlyFire(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	aim := protocol.Hex{Q: defaultThrowSep, R: 0}
	allyHex := protocol.Hex{Q: defaultThrowSep, R: 1} // in the blast radius, but friendly
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0}, aim, allyHex)

	id, token := w.PlaceEntityForTest(origin)
	flask := w.GrantItemForTest(id, flaskOfFireID)
	allyID, _ := w.PlaceEntityForTest(allyHex)
	monsterID := w.PlaceMonsterForTest(aim)

	allyBefore := w.HPForTest(allyID)

	if err := w.SubmitIntent(throwIntent(id, token, flask, aim)); err != nil {
		t.Fatalf("SubmitIntent(throw): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	if got, want := w.HPForTest(allyID), allyBefore; got != want {
		t.Errorf("ally HP after nearby blast = %d, want %d (no friendly fire)", got, want)
	}

	if got := w.HPForTest(monsterID); got >= protocol.MonsterMaxHP {
		t.Errorf("monster HP = %d, want < %d (it still took the blast)", got, protocol.MonsterMaxHP)
	}
}

// TestThrowIsCanceledByALaterMove: a throw is the turn's action, but a later
// intent in the same window replaces it — and since the flask is consumed only
// at resolution, the cancelled throw keeps the flask.
func TestThrowIsCanceledByALaterMove(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: defaultThrowSep, R: 0}
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0}, monsterHex)

	id, token := w.PlaceEntityForTest(origin)
	flask := w.GrantItemForTest(id, flaskOfFireID)
	monsterID := w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(throwIntent(id, token, flask, monsterHex)); err != nil {
		t.Fatalf("SubmitIntent(throw): %v", err)
	}

	// A move submitted after the throw wins — the throw never fires.
	if err := w.SubmitIntent(protocol.IntentRequest{
		EntityID: id, Token: token, Kind: protocol.IntentMove, Target: protocol.Hex{Q: 0, R: 1},
	}); err != nil {
		t.Fatalf("SubmitIntent(move): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	if got := w.HPForTest(monsterID); got != protocol.MonsterMaxHP {
		t.Errorf("monster HP = %d, want %d (the throw was cancelled)", got, protocol.MonsterMaxHP)
	}

	if got := backpackCount(w, id, flaskOfFireID); got != 1 {
		t.Errorf("flask count = %d, want 1 (a cancelled throw keeps the flask)", got)
	}
}

// TestThrowNotConsumedAtSubmit: the flask is spent at RESOLUTION, not at
// submit — a submitted-but-unresolved throw still shows the flask in hand.
func TestThrowNotConsumedAtSubmit(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	monsterHex := protocol.Hex{Q: defaultThrowSep, R: 0}
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, protocol.Hex{Q: 2, R: 0}, monsterHex)

	id, token := w.PlaceEntityForTest(origin)
	flask := w.GrantItemForTest(id, flaskOfFireID)
	w.PlaceMonsterForTest(monsterHex)

	if err := w.SubmitIntent(throwIntent(id, token, flask, monsterHex)); err != nil {
		t.Fatalf("SubmitIntent(throw): %v", err)
	}

	if got := backpackCount(w, id, flaskOfFireID); got != 1 {
		t.Errorf("flask count after submit (pre-resolve) = %d, want 1", got)
	}
}

// TestThrowOutOfRangeRejected: beyond the flask's throw range, the intent is
// a 422 (ErrOutOfRange), not a 500.
func TestThrowOutOfRangeRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	farHex := protocol.Hex{Q: flaskRange + 1, R: 0}

	line := []protocol.Hex{origin}
	for q := 1; q <= flaskRange+1; q++ {
		line = append(line, protocol.Hex{Q: q, R: 0})
	}

	clearLine(w, line...)

	id, token := w.PlaceEntityForTest(origin)
	flask := w.GrantItemForTest(id, flaskOfFireID)

	if got, want := w.SubmitIntent(throwIntent(id, token, flask, farHex)), game.ErrOutOfRange; !errors.Is(got, want) {
		t.Errorf("out-of-range throw err = %v, want %v", got, want)
	}
}

// TestThrowThroughRockRejected: the throw obeys the same line-of-sight gate a
// ranged attack does (#195) — a wall between shields the target.
func TestThrowThroughRockRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	between := protocol.Hex{Q: 2, R: 0}
	aim := protocol.Hex{Q: flaskRange, R: 0}
	clearLine(w, origin, protocol.Hex{Q: 1, R: 0}, between, protocol.Hex{Q: 3, R: 0}, aim)

	id, token := w.PlaceEntityForTest(origin)
	flask := w.GrantItemForTest(id, flaskOfFireID)

	w.SetTerrainForTest(between, protocol.TerrainRock)

	if got, want := w.SubmitIntent(throwIntent(id, token, flask, aim)), game.ErrNoLineOfSight; !errors.Is(got, want) {
		t.Fatalf("through-rock throw err = %v, want %v", got, want)
	}

	// Clear the only blocker and the identical throw is accepted.
	w.SetTerrainForTest(between, protocol.TerrainGrass)

	if err := w.SubmitIntent(throwIntent(id, token, flask, aim)); err != nil {
		t.Errorf("throw with a clear line err = %v, want nil", err)
	}
}

// TestThrowNonThrowableRejected: drinking is not throwing — a plain potion
// cannot be thrown (ErrNotThrowable).
func TestThrowNonThrowableRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)
	potion := w.GrantItemForTest(id, "healing-potion")

	if got, want := w.SubmitIntent(throwIntent(id, token, potion, protocol.Hex{Q: 1, R: 0})),
		game.ErrNotThrowable; !errors.Is(got, want) {
		t.Errorf("throw of a non-throwable err = %v, want %v", got, want)
	}
}

// TestThrowUnownedItemRejected: throwing an item you do not own is
// ErrItemNotOwned.
func TestThrowUnownedItemRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	origin := protocol.Hex{Q: 0, R: 0}
	id, token := w.PlaceEntityForTest(origin)

	if got, want := w.SubmitIntent(throwIntent(id, token, 999999, protocol.Hex{Q: 1, R: 0})),
		game.ErrItemNotOwned; !errors.Is(got, want) {
		t.Errorf("throw of an unowned item err = %v, want %v", got, want)
	}
}

// TestRecallTeleportsToSanctuaryAndSpendsTheScroll: a recall teleports the
// user from the field back into the shared sanctuary and consumes the scroll.
func TestRecallTeleportsToSanctuaryAndSpendsTheScroll(t *testing.T) {
	t.Parallel()

	w := newWorld()
	w.SetSeedForTest(1)

	origin := protocol.Hex{Q: 0, R: 0}
	fieldHex := protocol.Hex{Q: 9, R: 0} // well beyond SanctuaryRadius (5)

	id, token := w.PlaceEntityForTest(fieldHex)
	scroll := w.GrantItemForTest(id, scrollOfRecallID)

	if err := w.SubmitIntent(recallIntent(id, token, scroll)); err != nil {
		t.Fatalf("SubmitIntent(recall): %v", err)
	}

	w.ResolveCombatOnlyForTest()

	dest := hexOfSnap(w.Snapshot(), id)
	if dest == fieldHex {
		t.Fatalf("player did not move on recall (still at %v)", dest)
	}

	if got := game.HexDistance(origin, dest); got > protocol.SanctuaryRadius {
		t.Errorf("recall landed at %v, distance %d from origin, want <= %d (the sanctuary)",
			dest, got, protocol.SanctuaryRadius)
	}

	if got := backpackCount(w, id, scrollOfRecallID); got != 0 {
		t.Errorf("scroll count after recall = %d, want 0 (consumed)", got)
	}
}

// TestStarterConsumablesGrantedAtJoin: the config-gated starter kit (#271)
// lands each configured consumable in a new player's backpack, so the client
// feature is deterministically usable in e2e without a monster drop.
func TestStarterConsumablesGrantedAtJoin(t *testing.T) {
	t.Parallel()

	w := game.NewWorld(game.WorldConfig{
		Interval:           time.Hour,
		CombatPatience:     time.Second,
		BubblePoll:         time.Millisecond,
		DisconnectGrace:    time.Minute,
		WorldSeed:          0xC0FFEE,
		Radius:             12,
		Ticks:              hub.New(),
		StarterConsumables: []string{flaskOfFireID, scrollOfRecallID},
	})

	resp, err := w.Join("", "kitted", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	if got := backpackCount(w, resp.EntityID, flaskOfFireID); got != 1 {
		t.Errorf("flask count at join = %d, want 1", got)
	}

	if got := backpackCount(w, resp.EntityID, scrollOfRecallID); got != 1 {
		t.Errorf("scroll count at join = %d, want 1", got)
	}
}

// TestRecallNonRecallItemRejected: only a recall scroll recalls
// (ErrNotRecallable).
func TestRecallNonRecallItemRejected(t *testing.T) {
	t.Parallel()

	w := newWorld()

	id, token := w.PlaceEntityForTest(protocol.Hex{Q: 0, R: 0})
	flask := w.GrantItemForTest(id, flaskOfFireID)

	if got, want := w.SubmitIntent(recallIntent(id, token, flask)), game.ErrNotRecallable; !errors.Is(got, want) {
		t.Errorf("recall of a non-recall item err = %v, want %v", got, want)
	}
}
