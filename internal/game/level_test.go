//nolint:testpackage // white-box: needs unexported levelFor/xpFloorFor; see rules_test.go's file doc.
package game

import "testing"

func TestLevelForQuadraticCurve(t *testing.T) {
	t.Parallel()

	cases := []struct {
		xp   int
		want int
	}{
		{0, 1}, {99, 1}, {100, 2}, {250, 2}, {399, 2},
		{400, 3}, {899, 3}, {900, 4}, {1600, 5}, {3600, 7}, {8100, 10},
	}
	for _, c := range cases {
		if got, want := levelFor(c.xp), c.want; got != want {
			t.Errorf("levelFor(%d) = %d, want %d", c.xp, got, want)
		}
	}
}

func TestXPFloorForInvertsLevelFor(t *testing.T) {
	t.Parallel()

	for level := 1; level <= 20; level++ {
		floor := xpFloorFor(level)

		if got, want := levelFor(floor), level; got != want {
			t.Errorf("levelFor(xpFloorFor(%d)) = %d, want %d", level, got, want)
		}

		if level > 1 {
			if got, want := levelFor(floor-1), level-1; got != want {
				t.Errorf("levelFor(xpFloorFor(%d)-1) = %d, want %d", level, got, want)
			}
		}
	}
}
