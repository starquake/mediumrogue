package game_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// wire_nil_test.go: NO SLICE ON THE WIRE IS EVER nil.
//
// A nil Go slice marshals to JSON `null`, but tygo generates the TypeScript
// type from the Go type — which cannot express nullability — so the client is
// told `T[]` and calls .map on null. It is a runtime crash the compiler is
// structurally unable to catch, because the generated type is a lie.
//
// This has now bitten three times: #167 (ItemView.tags, froze the client
// mid-session) and the BubbleView.waitingForIds crash on development
// 2026-07-19 — which fires whenever every player has locked in, i.e. the
// COMMON case. Each was fixed one field at a time; this walks the whole
// bundle instead, so field number four fails here rather than in a browser.

// TestNoNilSlicesAnywhereOnTheWire walks a real turn bundle and fails on any
// nil slice, at any depth.
func TestNoNilSlicesAnywhereOnTheWire(t *testing.T) {
	t.Parallel()

	w := newWorld()

	// A board with enough going on to populate the interesting branches:
	// a player with gear and skills, a monster, and a live combat bubble.
	resp, err := w.Join("", "wirecheck", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	w.SetHexForTest(resp.EntityID, protocol.Hex{Q: 0, R: 0})
	w.SetSkillStateForTest(resp.EntityID, []string{"survivalist"}, 2, 1)
	w.GrantItemForTest(resp.EntityID, "butchers-cleaver")

	adjacent := walkableHexAtDistance(t, w, protocol.Hex{Q: 0, R: 0}, 1, 1)
	monsterID := w.PlaceMonsterKindForTest(adjacent, "wolf")

	step(t, w)

	// SnapshotFor is the real per-viewer render — the same path the client
	// receives, including own-only fields.
	assertNoNilSlices(t, reflect.ValueOf(w.SnapshotFor(resp.Token)), "TurnEvent (nobody locked in)")

	_ = monsterID
}

// assertNoNilSlices walks v recursively and reports every nil slice it finds.
// Maps are walked too; a nil MAP is fine (it marshals to `null`, but no client
// code iterates one today) — slices are the crash surface.
func assertNoNilSlices(t *testing.T, v reflect.Value, path string) {
	t.Helper()

	//nolint:exhaustive // only the composite kinds can contain a slice; the
	// rest are scalars with nothing to walk (default case covers them).
	switch v.Kind() {
	case reflect.Slice:
		if v.IsNil() {
			t.Errorf("%s is a nil slice — it marshals to JSON null, and the generated "+
				"TypeScript type says it is an array, so the client will crash on .map", path)

			return
		}

		for i := range v.Len() {
			assertNoNilSlices(t, v.Index(i), path+"[]")
		}
	case reflect.Struct:
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}

			assertNoNilSlices(t, v.Field(i), path+"."+f.Name)
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			assertNoNilSlices(t, v.Elem(), path)
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			assertNoNilSlices(t, v.MapIndex(k), path+"["+strings.ToLower(k.String())+"]")
		}
	default:
	}
}
