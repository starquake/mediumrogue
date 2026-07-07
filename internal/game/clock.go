// Package game holds the authoritative world simulation. For now that is
// only the world clock; turn resolution, hex math, and procgen land here as
// the milestones progress.
package game

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/starquake/medium-rogue/internal/hub"
)

// Clock advances the world-turn counter on a fixed cadence and publishes a
// tick on each advance. It is the out-of-combat metronome; combat time
// bubbles (later milestone) suspend affected entities from it rather than
// stopping the clock itself.
type Clock struct {
	interval time.Duration
	ticks    *hub.Hub
	turn     atomic.Int64
}

// NewClock returns a Clock that advances every interval and announces each
// turn on ticks.
func NewClock(interval time.Duration, ticks *hub.Hub) *Clock {
	return &Clock{interval: interval, ticks: ticks}
}

// Turn returns the current world-turn number. Zero means no turn has been
// resolved yet.
func (c *Clock) Turn() int64 {
	return c.turn.Load()
}

// Run advances the clock until ctx is canceled. It blocks; run it in a
// goroutine.
func (c *Clock) Run(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.turn.Add(1)
			c.ticks.Publish()
		}
	}
}
