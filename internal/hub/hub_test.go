package hub_test

import (
	"testing"
	"time"

	"github.com/starquake/medium-rogue/internal/hub"
)

func TestPublishReachesSubscriber(t *testing.T) {
	t.Parallel()

	h := hub.New()

	ch, unsubscribe := h.Subscribe()
	defer unsubscribe()

	h.Publish()

	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("expected a tick after Publish, got none")
	}
}

func TestPublishCoalesces(t *testing.T) {
	t.Parallel()

	h := hub.New()

	ch, unsubscribe := h.Subscribe()
	defer unsubscribe()

	// Two publishes with no read in between must coalesce into one tick.
	h.Publish()
	h.Publish()

	<-ch

	select {
	case <-ch:
		t.Fatal("expected coalescing: second unread tick should have been dropped")
	default:
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	t.Parallel()

	h := hub.New()
	ch, unsubscribe := h.Subscribe()
	unsubscribe()

	h.Publish()

	select {
	case <-ch:
		t.Fatal("expected no tick after unsubscribe")
	default:
	}
}

func TestUnsubscribeIsIdempotent(t *testing.T) {
	t.Parallel()

	h := hub.New()
	_, unsubscribe := h.Subscribe()
	unsubscribe()
	unsubscribe() // must not panic or corrupt the map

	h.Publish() // must not panic with an empty subscriber set
}

func TestPublishReachesAllSubscribers(t *testing.T) {
	t.Parallel()

	h := hub.New()

	first, unsubFirst := h.Subscribe()
	defer unsubFirst()

	second, unsubSecond := h.Subscribe()
	defer unsubSecond()

	h.Publish()

	for name, ch := range map[string]<-chan struct{}{"first": first, "second": second} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("subscriber %q did not receive the tick", name)
		}
	}
}
