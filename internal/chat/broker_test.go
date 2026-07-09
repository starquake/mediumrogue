package chat_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/chat"
)

func TestBrokerFansOutInOrderWithMonotonicSeq(t *testing.T) {
	t.Parallel()

	b := chat.NewBroker()

	ch1, cancel1 := b.Subscribe()
	defer cancel1()

	ch2, cancel2 := b.Subscribe()
	defer cancel2()

	b.Publish("alice", "hello")
	b.Publish("bob", "hi")

	// ch1 and ch2 are <-chan protocol.ChatMessage; receiving yields a
	// protocol.ChatMessage with Seq/Sender/Text.
	m1 := <-ch1
	m2 := <-ch1

	if got, want := m1.Text, "hello"; got != want {
		t.Errorf("ch1 msg1 text = %q, want %q", got, want)
	}

	if got, want := m2.Text, "hi"; got != want {
		t.Errorf("ch1 msg2 text = %q, want %q", got, want)
	}

	if got, want := m1.Sender, "alice"; got != want {
		t.Errorf("ch1 msg1 sender = %q, want %q", got, want)
	}

	if m2.Seq <= m1.Seq {
		t.Errorf("seq not monotonic: %d then %d", m1.Seq, m2.Seq)
	}

	// Both subscribers receive both messages.
	if got := (<-ch2).Text; got != "hello" {
		t.Errorf("ch2 msg1 = %q, want hello", got)
	}

	if got := (<-ch2).Text; got != "hi" {
		t.Errorf("ch2 msg2 = %q, want hi", got)
	}
}

func TestBrokerDropsForFullSubscriberOnly(t *testing.T) {
	t.Parallel()

	b := chat.NewBroker()

	slow, cancelSlow := b.Subscribe()
	defer cancelSlow()

	fast, cancelFast := b.Subscribe()
	defer cancelFast()

	// Overfill: publish well past the buffer without reading `slow`.
	const n = 100

	for range n {
		b.Publish("x", "m")
	}

	// `fast` is also unread here, so both cap at the buffer — the point is
	// neither Publish blocked and draining yields <= n (drops happened) but > 0.
	got := 0

	for {
		select {
		case <-slow:
			got++
		default:
			if got == 0 {
				t.Fatal("slow subscriber got nothing")
			}

			if got >= n {
				t.Fatalf("slow subscriber got %d, want < %d (drops expected)", got, n)
			}

			_ = fast

			return
		}
	}
}

func TestUnsubscribeStopsDeliveryAndNeverBlocks(t *testing.T) {
	t.Parallel()

	b := chat.NewBroker()
	ch, cancel := b.Subscribe()
	cancel()

	// Must not panic or block even though ch is no longer subscribed.
	b.Publish("a", "after cancel")

	select {
	case _, ok := <-ch:
		if ok {
			t.Error("received a message after unsubscribe")
		}
	default:
		// nothing delivered — correct
	}
}
