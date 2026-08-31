package game //nolint:testpackage // white-box: pins the package-wide worldgen selection.

import (
	"os"
	"testing"
)

// TestMain pins this package's tests to the NOISE generator (#458 experiment
// branch).
//
// Almost nothing in here is a worldgen test: the suite is about leash
// behaviour, spawn fallback, bubble formation and combat geometry, and it only
// needs a world with walkable ground in most directions. The graph generator
// deliberately puts rock in most directions, which made three leash tests fail
// with "no walkable neighbor" — a failure about the fixture, not the subject.
//
// The graph generator is covered by worldgen_graph_test.go, which opts back in
// explicitly.
func TestMain(m *testing.M) {
	if err := os.Setenv("WORLDGEN", "noise"); err != nil {
		panic(err)
	}

	m.Run()
}
