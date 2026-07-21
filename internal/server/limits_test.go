package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/server"
)

func TestPerKeyLimiterEnforcesInterval(t *testing.T) {
	t.Parallel()

	l := server.NewPerKeyLimiterForTest(time.Second)
	now := time.Now()

	if got, want := l.AllowAtForTest("alice", now), true; got != want {
		t.Errorf("first allow = %v, want %v", got, want)
	}

	if got, want := l.AllowAtForTest("alice", now.Add(500*time.Millisecond)), false; got != want {
		t.Errorf("allow inside the interval = %v, want %v", got, want)
	}

	// A different key is an independent budget.
	if got, want := l.AllowAtForTest("bob", now.Add(500*time.Millisecond)), true; got != want {
		t.Errorf("other key allow = %v, want %v", got, want)
	}

	if got, want := l.AllowAtForTest("alice", now.Add(time.Second)), true; got != want {
		t.Errorf("allow after the interval = %v, want %v", got, want)
	}
}

func TestPerKeyLimiterZeroIntervalDisables(t *testing.T) {
	t.Parallel()

	l := server.NewPerKeyLimiterForTest(0)
	now := time.Now()

	for i := range 3 {
		if got, want := l.AllowAtForTest("alice", now), true; got != want {
			t.Errorf("allow #%d with zero interval = %v, want %v", i, got, want)
		}
	}
}

// TestPerKeyLimiterPrunesExpiredEntries pins the memory bound: once the map
// crosses the prune trigger, entries whose interval has elapsed (deletable
// without changing any verdict) are swept, so the limiter tracks currently
// throttling keys, not every token that ever chatted.
func TestPerKeyLimiterPrunesExpiredEntries(t *testing.T) {
	t.Parallel()

	l := server.NewPerKeyLimiterForTest(time.Second)
	now := time.Now()

	for i := range 64 {
		l.AllowAtForTest(fmt.Sprintf("old-%d", i), now)
	}

	// All 64 have expired by now+1s; the 65th insert crosses the trigger and
	// sweeps them, leaving just the fresh key.
	l.AllowAtForTest("fresh", now.Add(time.Second))

	if got, want := l.LenForTest(), 1; got != want {
		t.Errorf("entries after prune = %d, want %d", got, want)
	}
}

func TestTokenBucketBurstsThenThrottles(t *testing.T) {
	t.Parallel()

	b := server.NewTokenBucketForTest(time.Second, 3)
	now := time.Now()

	for i := range 3 {
		if got, want := b.TakeAtForTest(now), true; got != want {
			t.Errorf("burst take #%d = %v, want %v", i, got, want)
		}
	}

	if got, want := b.TakeAtForTest(now), false; got != want {
		t.Errorf("take on empty bucket = %v, want %v", got, want)
	}

	// One interval refills exactly one slot — no faster refill by polling.
	if got, want := b.TakeAtForTest(now.Add(time.Second)), true; got != want {
		t.Errorf("take after one interval = %v, want %v", got, want)
	}

	if got, want := b.TakeAtForTest(now.Add(1500*time.Millisecond)), false; got != want {
		t.Errorf("second take inside the same interval = %v, want %v", got, want)
	}

	// A long idle stretch refills to burst, never beyond.
	long := now.Add(time.Hour)
	for i := range 3 {
		if got, want := b.TakeAtForTest(long), true; got != want {
			t.Errorf("post-idle take #%d = %v, want %v", i, got, want)
		}
	}

	if got, want := b.TakeAtForTest(long), false; got != want {
		t.Errorf("post-idle take past burst = %v, want %v", got, want)
	}
}

func TestTokenBucketZeroIntervalDisables(t *testing.T) {
	t.Parallel()

	b := server.NewTokenBucketForTest(0, 1)
	now := time.Now()

	for i := range 5 {
		if got, want := b.TakeAtForTest(now), true; got != want {
			t.Errorf("take #%d with zero interval = %v, want %v", i, got, want)
		}
	}
}

func TestStreamGateCapsAndReleases(t *testing.T) {
	t.Parallel()

	g := server.NewStreamGateForTest(2)

	if got, want := g.AcquireForTest(), true; got != want {
		t.Errorf("acquire #1 = %v, want %v", got, want)
	}

	if got, want := g.AcquireForTest(), true; got != want {
		t.Errorf("acquire #2 = %v, want %v", got, want)
	}

	if got, want := g.AcquireForTest(), false; got != want {
		t.Errorf("acquire over cap = %v, want %v", got, want)
	}

	g.ReleaseForTest()

	if got, want := g.AcquireForTest(), true; got != want {
		t.Errorf("acquire after release = %v, want %v", got, want)
	}
}

func TestStreamGateZeroDisables(t *testing.T) {
	t.Parallel()

	g := server.NewStreamGateForTest(0)

	for i := range 5 {
		if got, want := g.AcquireForTest(), true; got != want {
			t.Errorf("acquire #%d with zero cap = %v, want %v", i, got, want)
		}
	}
}

// TestPerKeyStreamGateCapsPerKey: the per-IP SSE cap admits up to the limit
// concurrent holders per key, rejects the next from that key, keeps a
// different key's budget independent, and frees a slot on release.
func TestPerKeyStreamGateCapsPerKey(t *testing.T) {
	t.Parallel()

	g := server.NewPerKeyStreamGateForTest(2)

	if got, want := g.AcquireForTest("1.1.1.1"), true; got != want {
		t.Errorf("acquire #1 for key = %v, want %v", got, want)
	}

	if got, want := g.AcquireForTest("1.1.1.1"), true; got != want {
		t.Errorf("acquire #2 for key = %v, want %v", got, want)
	}

	if got, want := g.AcquireForTest("1.1.1.1"), false; got != want {
		t.Errorf("acquire over per-key cap = %v, want %v", got, want)
	}

	// A different key is an independent budget.
	if got, want := g.AcquireForTest("2.2.2.2"), true; got != want {
		t.Errorf("acquire for other key = %v, want %v", got, want)
	}

	// Releasing one of the first key's slots lets it acquire again.
	g.ReleaseForTest("1.1.1.1")

	if got, want := g.AcquireForTest("1.1.1.1"), true; got != want {
		t.Errorf("acquire after release = %v, want %v", got, want)
	}
}

func TestPerKeyStreamGateZeroDisables(t *testing.T) {
	t.Parallel()

	g := server.NewPerKeyStreamGateForTest(0)

	for i := range 5 {
		if got, want := g.AcquireForTest("1.1.1.1"), true; got != want {
			t.Errorf("acquire #%d with zero cap = %v, want %v", i, got, want)
		}
	}
}

// TestClientIP pins the XFF derivation the per-IP cap keys on: the LAST
// X-Forwarded-For entry (the one the sole trusted proxy appended — earlier
// entries are client-spoofable), falling back to RemoteAddr's host when the
// header is absent or empty.
func TestClientIP(t *testing.T) {
	t.Parallel()

	const (
		client = "203.0.113.5" // the IP the trusted proxy records
		direct = "203.0.113.9" // the peer RemoteAddr sees when XFF is absent
	)

	tests := []struct {
		name       string
		xff        string
		remoteAddr string
		want       string
	}{
		{name: "single xff entry", xff: client, remoteAddr: "10.0.0.1:5000", want: client},
		{
			name:       "spoofed prefix, proxy-appended last wins",
			xff:        "1.2.3.4, " + client,
			remoteAddr: "10.0.0.1:5000",
			want:       client,
		},
		{name: "absent header falls back to RemoteAddr host", xff: "", remoteAddr: direct + ":5000", want: direct},
		{name: "empty entries fall back to RemoteAddr host", xff: " , ", remoteAddr: direct + ":5000", want: direct},
		{name: "RemoteAddr without port used verbatim", xff: "", remoteAddr: direct, want: direct},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", nil)
			req.RemoteAddr = tc.remoteAddr

			if tc.xff != "" {
				req.Header.Set("X-Forwarded-For", tc.xff)
			}

			if got, want := server.ClientIPForTest(req), tc.want; got != want {
				t.Errorf("clientIP = %q, want %q", got, want)
			}
		})
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		d    time.Duration
		want string
	}{
		{d: time.Second, want: "1"},
		{d: 1500 * time.Millisecond, want: "2"}, // rounds up
		{d: 100 * time.Millisecond, want: "1"},  // floor of 1: "0" invites an instant retry storm
		{d: 0, want: "1"},
		{d: 30 * time.Second, want: "30"},
	}

	for _, tc := range tests {
		if got, want := server.RetryAfterSecondsForTest(tc.d), tc.want; got != want {
			t.Errorf("retryAfterSeconds(%s) = %q, want %q", tc.d, got, want)
		}
	}
}
