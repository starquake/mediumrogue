// Package hub provides a process-local pub/sub fan-out with coalescing
// semantics, following the pattern proven in topbanana's leaderboard hub.
//
// Coalescing is intentional: each subscriber channel is buffered to one slot,
// so a slow subscriber drops intermediate ticks and just sees "the world
// moved" on its next receive. Subscribers must treat every tick as a "fetch
// the latest state" signal, never as a per-event delta.
package hub

import "sync"

// Hub fans out ticks to in-process subscribers. Safe for concurrent use. A
// slow reader never blocks Publish.
type Hub struct {
	mu   sync.Mutex
	subs map[chan struct{}]struct{}
}

// New returns a fresh Hub with no subscribers.
func New() *Hub {
	return &Hub{subs: make(map[chan struct{}]struct{})}
}

// Subscribe registers a receiver and returns its channel plus an unsubscribe
// func. The caller MUST invoke unsubscribe when done (typically via defer);
// failing to do so pins the channel in the subscriber map for the process
// lifetime.
//
// The channel has capacity 1. If a Publish lands while a previous tick is
// still unread, the new tick is dropped — see the package doc on coalescing.
func (h *Hub) Subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)

	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once

	unsubscribe := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
		})
	}

	return ch, unsubscribe
}

// Publish wakes every subscriber that is ready to receive. Subscribers with
// an unread tick already buffered are skipped (coalesced).
func (h *Hub) Publish() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
