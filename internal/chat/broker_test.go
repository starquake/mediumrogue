package chat_test

import (
	"testing"

	"github.com/starquake/mediumrogue/internal/chat"
	"github.com/starquake/mediumrogue/internal/protocol"
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

// TestPublishToStampsRecipientAndStillFansOut pins the split of duties for the
// directed line (#385): the BROKER addresses, the STREAM filters.
//
// The fan-out half is the counter-intuitive one and the reason this test
// exists. It looks like a privacy bug — a private message handed to every
// subscriber — but the broker deals in bare channels and has no idea which one
// belongs to whom. Enforcement lives at the SSE handler, which already
// resolved its viewer. If someone "fixes" this by filtering here, they will
// have to teach the broker identity, and that second copy of which-stream-is-
// whose is what drifts out of sync with the real one.
func TestPublishToStampsRecipientAndStillFansOut(t *testing.T) {
	t.Parallel()

	b := chat.NewBroker()

	first, cancelFirst := b.Subscribe()
	defer cancelFirst()

	second, cancelSecond := b.Subscribe()
	defer cancelSecond()

	const recipient int64 = 42

	b.PublishTo(recipient, "system", "alice declined")

	for i, ch := range []<-chan protocol.ChatMessage{first, second} {
		select {
		case msg := <-ch:
			if got, want := msg.Recipient, recipient; got != want {
				t.Errorf("subscriber %d Recipient = %d, want %d", i, got, want)
			}

			if got, want := msg.Text, "alice declined"; got != want {
				t.Errorf("subscriber %d Text = %q, want %q", i, got, want)
			}
		default:
			t.Errorf("subscriber %d received nothing", i)
		}
	}
}

// TestPublishLeavesRecipientZero: an ordinary line is global, and stays global.
// Recipient 0 is what every pre-#385 line was, so the plain Publish path must
// not start addressing anything by accident.
func TestPublishLeavesRecipientZero(t *testing.T) {
	t.Parallel()

	b := chat.NewBroker()

	ch, cancel := b.Subscribe()
	defer cancel()

	b.Publish("alice", "hello")

	select {
	case msg := <-ch:
		if got, want := msg.Recipient, int64(0); got != want {
			t.Errorf("Recipient = %d, want %d (global)", got, want)
		}
	default:
		t.Fatal("received nothing")
	}
}
