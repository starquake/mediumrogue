package game_test

import (
	"testing"
	"time"

	"github.com/starquake/medium-rogue/internal/game"
	"github.com/starquake/medium-rogue/internal/hub"
)

func TestClockAdvancesAndPublishes(t *testing.T) {
	t.Parallel()

	ticks := hub.New()
	clock := game.NewClock(5*time.Millisecond, ticks)

	ch, unsubscribe := ticks.Subscribe()
	defer unsubscribe()

	ctx := t.Context()
	go clock.Run(ctx)

	deadline := time.After(2 * time.Second)
	// Wait for two ticks so we observe an actual advance, not just the first fire.
	for range 2 {
		select {
		case <-ch:
		case <-deadline:
			t.Fatal("clock did not tick before the deadline")
		}
	}

	if got := clock.Turn(); got < 2 {
		t.Fatalf("Turn() = %d, want >= 2 after two observed ticks", got)
	}
}

func TestClockStartsAtZero(t *testing.T) {
	t.Parallel()

	clock := game.NewClock(time.Hour, hub.New())
	if got := clock.Turn(); got != 0 {
		t.Fatalf("Turn() = %d before Run, want 0", got)
	}
}
