// Package chat provides the global chat channel: a non-coalescing fan-out
// broker (unlike internal/hub, every message is delivered, best-effort) and
// the server-side "/" command registry.
package chat

import (
	"sync"
	"sync/atomic"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// chatBufferN is each subscriber's buffer. A subscriber that falls this far
// behind drops messages (ephemeral: no history, no delivery guarantee) rather
// than blocking Publish.
const chatBufferN = 32

// Broker fans chat messages out to in-process subscribers (the SSE handlers).
// Safe for concurrent use. A slow reader never blocks Publish.
type Broker struct {
	mu   sync.Mutex
	subs map[chan protocol.ChatMessage]struct{}
	seq  atomic.Int64
}

// NewBroker returns a broker with no subscribers.
func NewBroker() *Broker {
	return &Broker{subs: make(map[chan protocol.ChatMessage]struct{})}
}

// Subscribe registers a receiver and returns its channel plus an unsubscribe
// func the caller MUST invoke when done (typically via defer).
func (b *Broker) Subscribe() (<-chan protocol.ChatMessage, func()) {
	ch := make(chan protocol.ChatMessage, chatBufferN)

	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()

	var once sync.Once

	unsubscribe := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, ch)
			b.mu.Unlock()
		})
	}

	return ch, unsubscribe
}

// Publish stamps a monotonic Seq and delivers the message to every current
// subscriber, skipping any whose buffer is full (best-effort).
func (b *Broker) Publish(sender, text string) {
	msg := protocol.ChatMessage{
		Seq:    b.seq.Add(1),
		Sender: sender,
		Text:   text,
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	for ch := range b.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}
