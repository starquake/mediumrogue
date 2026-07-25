package game //nolint:testpackage // white-box: needs the unexported monster registry (monsterDefs, defAggroRadius).

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestInterestRadiusExceedsEveryAggroRadius pins #289's load-bearing
// invariant: a player must be able to SEE a monster before that monster can
// start hunting them. If the interest radius ever fell to or below some
// kind's aggro radius, that kind would begin a chase from outside the
// player's bundle and then "pop in" already aggressive — the same failure
// protocol.MonsterAggroRadius > protocol.CombatRadius guards against.
//
// Deliberately asserted against EVERY kind's effective radius rather than the
// protocol.MonsterAggroRadius default: kinds override it (the Dragon sits at
// 12), so a check against the default would pass while the real margin
// eroded. defAggroRadius is the one place the "0 means the default" rule
// lives, so this cannot drift from the runtime.
func TestInterestRadiusExceedsEveryAggroRadius(t *testing.T) {
	t.Parallel()

	for _, def := range monsterDefs {
		if got, want := protocol.InterestRadius, defAggroRadius(def); got <= want {
			t.Errorf("InterestRadius = %d, want > %q's effective aggro radius %d", got, def.id, want)
		}
	}
}
