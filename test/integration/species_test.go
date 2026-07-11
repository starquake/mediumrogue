package integration_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// joinSpecies is join/joinClass plus an explicit species (protocol.SpeciesHuman
// /Elf/Dwarf), always as a new Fighter (empty token — no caller here needs a
// reclaim; class is irrelevant to every test in this file, which is only about
// the species passive). join and joinClass always join Human — the default
// every test that doesn't care about species uses; species-specific behavior
// needs this instead.
func joinSpecies(t *testing.T, ts *httptest.Server, species string) protocol.JoinResponse {
	t.Helper()

	resp := postJSON(t, ts, "/api/join",
		protocol.JoinRequest{Name: testerName, Class: protocol.ClassFighter, Species: species})
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("join status = %d, want 200", got)
	}

	var joined protocol.JoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&joined); err != nil {
		t.Fatalf("decode join response: %v", err)
	}

	return joined
}

// TestSpeciesOnWire joins one of each species and reads Species back off a
// real turn bundle, the species analogue of TestJoinPerClassMaxHP — proving
// the wire field (internal/game -> protocol.Entity.Species) round-trips
// exactly, not just "something non-empty".
func TestSpeciesOnWire(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour) // frozen clock: no monsters, no movement needed

	human := joinSpecies(t, ts, protocol.SpeciesHuman)
	elf := joinSpecies(t, ts, protocol.SpeciesElf)
	dwarf := joinSpecies(t, ts, protocol.SpeciesDwarf)

	events := get(t, ts, "/api/events")
	bundle := decodeBundle(t, bufio.NewReader(events.Body))

	for _, tc := range []struct {
		name        string
		id          int64
		wantSpecies string
	}{
		{"human", human.EntityID, protocol.SpeciesHuman},
		{"elf", elf.EntityID, protocol.SpeciesElf},
		{"dwarf", dwarf.EntityID, protocol.SpeciesDwarf},
	} {
		e, ok := entityOf(bundle, tc.id)
		if !ok {
			t.Fatalf("%s entity %d missing from turn bundle", tc.name, tc.id)
		}

		if got, want := e.Species, tc.wantSpecies; got != want {
			t.Errorf("%s: Species on wire = %q, want %q", tc.name, got, want)
		}
	}
}

// TestHumanEarnsMoreXPThanDwarfOverHTTP exercises milestone 6b.3's headline
// behavior over real HTTP/SSE: a human and a dwarf jointly fight the same
// monster inside one shared combat bubble. Milestone 6b.1 pays every surviving
// bubble member the FULL MonsterXP on a kill (no last-hit competition), so the
// only difference between the two players' final XP is the species passive —
// the human ends up with exactly MonsterXP*(100+HumanXPBonusPercent)/100 (1.5x
// at +50%) while the dwarf gets the flat MonsterXP. This is the HTTP-level
// analogue of internal/game's TestHumanKillXPBonus, proving the passive lands
// on the wire and not just at the unit level.
//
// The monster is seeded one hex from the origin (where both players spawn), so
// the shared kill lands within a handful of bubble-turns resolved on the
// players' own lock-ins (both submit an intent every round, which is what
// actually advances an action-gated bubble turn) rather than a long
// crypto-random chase gated on the background tick loop — deterministic and
// robust even under a CPU-starved runner (#22). The test is not parallel so its
// tick loop is not starved by sibling servers.
//
//nolint:paralleltest // serial by design (#22): tick loop must not be CPU-starved by parallel siblings.
func TestHumanEarnsMoreXPThanDwarfOverHTTP(t *testing.T) {
	ts := startServerWithMonstersAt(t, protocol.Hex{Q: 1, R: 0})

	human := joinSpecies(t, ts, protocol.SpeciesHuman)
	dwarf := joinSpecies(t, ts, protocol.SpeciesDwarf)

	events := get(t, ts, "/api/events")
	reader := bufio.NewReader(events.Body)

	wantHumanXP := protocol.MonsterXP * (100 + protocol.HumanXPBonusPercent) / 100
	wantDwarfXP := protocol.MonsterXP

	deadline := time.Now().Add(10 * time.Second)

	var lastHumanXP, lastDwarfXP int

	for time.Now().Before(deadline) {
		bundle := decodeBundle(t, reader)

		humanEntity, ok := entityOf(bundle, human.EntityID)
		if !ok {
			t.Fatal("joined human missing from turn bundle")
		}

		dwarfEntity, ok := entityOf(bundle, dwarf.EntityID)
		if !ok {
			t.Fatal("joined dwarf missing from turn bundle")
		}

		lastHumanXP, lastDwarfXP = humanEntity.XP, dwarfEntity.XP

		// The shared kill credits every bubble member atomically in one
		// resolution (resolveBubbleTurnLocked), so the first bundle showing
		// ANY award should already show BOTH, at their exact final values.
		if humanEntity.XP > 0 || dwarfEntity.XP > 0 {
			if got, want := humanEntity.XP, wantHumanXP; got != want {
				t.Fatalf("human XP = %d, want %d (MonsterXP +%d%%)", got, want, protocol.HumanXPBonusPercent)
			}

			if got, want := dwarfEntity.XP, wantDwarfXP; got != want {
				t.Fatalf("dwarf XP = %d, want the flat %d", got, want)
			}

			return // both species-scaled kill rewards landed over the wire
		}

		// Both players chase/attack the same (only) monster every round; a
		// bubble's action-gated turn only advances once every member locks in
		// an intent, so both must submit each round for the fight to progress.
		if target, found := nearestMonster(bundle, humanEntity.Hex); found {
			postIntent(t, ts, human, target)
			postIntent(t, ts, dwarf, target)
		}
	}

	t.Fatalf("shared kill never landed before deadline: last human xp=%d dwarf xp=%d", lastHumanXP, lastDwarfXP)
}

// TestJoinRejectsInvalidSpeciesOverHTTP closes the 6b.3 species validation
// coverage gap at the wire level: POST /api/join with an empty or unknown
// species (a valid class) must be rejected 422 — never silently defaulted or
// accepted as a new player. internal/game already unit-tests class rejection
// (TestJoinRejectsInvalidClass) but nothing exercised species rejection, or
// either at the HTTP layer, until now.
func TestJoinRejectsInvalidSpeciesOverHTTP(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	for _, tc := range []struct {
		name    string
		species string
	}{
		{"empty", ""},
		{"unknown", "gnome"},
	} {
		resp := postJSON(t, ts, "/api/join",
			protocol.JoinRequest{Name: testerName, Class: protocol.ClassFighter, Species: tc.species})
		if got, want := resp.StatusCode, http.StatusUnprocessableEntity; got != want {
			t.Errorf("join(species=%q) status = %d, want %d (case %s)", tc.species, got, want, tc.name)
		}
	}
}
