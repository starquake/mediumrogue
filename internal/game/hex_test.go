package game_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/protocol"
)

func TestHexDistance(t *testing.T) {
	t.Parallel()

	origin := protocol.Hex{Q: 0, R: 0}

	cases := []struct {
		name string
		a, b protocol.Hex
		want int
	}{
		{"same hex", origin, origin, 0},
		{"neighbor", origin, protocol.Hex{Q: 1, R: 0}, 1},
		{"diagonal-ish", origin, protocol.Hex{Q: 2, R: -1}, 2},
		{"q and r cancel into s", origin, protocol.Hex{Q: 3, R: -3}, 3},
		{"symmetric", protocol.Hex{Q: -2, R: 5}, protocol.Hex{Q: 4, R: -1}, 6},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got, want := game.HexDistance(tc.a, tc.b), tc.want; got != want {
				t.Fatalf("HexDistance(%v, %v) = %d, want %d", tc.a, tc.b, got, want)
			}

			if got, want := game.HexDistance(tc.b, tc.a), tc.want; got != want {
				t.Fatalf("HexDistance(%v, %v) = %d, want %d (must be symmetric)", tc.b, tc.a, got, want)
			}
		})
	}
}

func TestHexNeighborsAreAllAtDistanceOne(t *testing.T) {
	t.Parallel()

	h := protocol.Hex{Q: 3, R: -7}

	seen := make(map[protocol.Hex]bool)

	for _, n := range game.HexNeighbors(h) {
		if got, want := game.HexDistance(h, n), 1; got != want {
			t.Errorf("neighbor %v at distance %d, want 1", n, got)
		}

		if seen[n] {
			t.Errorf("duplicate neighbor %v", n)
		}

		seen[n] = true
	}
}
