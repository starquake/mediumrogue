package server

import (
	"strconv"
	"sync"
	"time"
)

// Server-hardening limiters (#199). All three are wire-abuse bounds, so they
// live in the HTTP layer, not in the game domain: a throttled request never
// reaches World. Each is constructed once at route wiring (its handler's
// constructor runs once in addRoutes) and shared by every request to that
// route. A zero interval/cap disables the limiter — the same convention the
// timing knobs use, so tests and the e2e harness can switch one off.

// pruneTrigger is the perKeyLimiter map size that triggers a sweep of expired
// entries. Entries older than the interval are re-allowable anyway, so
// deleting them never changes an Allow verdict — the sweep is pure memory
// hygiene, keeping the map bounded by *currently throttling* keys instead of
// every token that ever chatted.
const pruneTrigger = 64

// perKeyLimiter enforces a minimum interval between events per key (the chat
// limit's shape: one line per CHAT_MIN_INTERVAL per token).
type perKeyLimiter struct {
	interval time.Duration

	mu   sync.Mutex
	last map[string]time.Time
}

// newPerKeyLimiter returns a limiter allowing one event per interval per key.
// A zero interval means no limit.
func newPerKeyLimiter(interval time.Duration) *perKeyLimiter {
	return &perKeyLimiter{interval: interval, last: make(map[string]time.Time)}
}

// allow reports whether an event for key may proceed now, and records it if so.
func (l *perKeyLimiter) allow(key string) bool {
	return l.allowAt(key, time.Now())
}

// allowAt is allow with an explicit clock, for tests.
func (l *perKeyLimiter) allowAt(key string, now time.Time) bool {
	if l.interval <= 0 {
		return true
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	if last, ok := l.last[key]; ok && now.Sub(last) < l.interval {
		return false
	}

	if len(l.last) >= pruneTrigger {
		l.pruneLocked(now)
	}

	l.last[key] = now

	return true
}

// pruneLocked drops entries whose interval has fully elapsed. Callers hold l.mu.
func (l *perKeyLimiter) pruneLocked(now time.Time) {
	for key, last := range l.last {
		if now.Sub(last) >= l.interval {
			delete(l.last, key)
		}
	}
}

// tokenBucket is a global (not per-key) rate limiter with a burst: the join
// limit's shape. It starts full — a whole friend group (or a mass reconnect
// after a restart) bursts in at once — and refills one slot per interval, so
// sustained new-character minting is capped at 1/interval.
type tokenBucket struct {
	interval time.Duration
	burst    int

	mu         sync.Mutex
	tokens     int
	lastRefill time.Time
}

// newTokenBucket returns a bucket of burst slots refilling one per interval,
// starting full. A zero interval means no limit.
func newTokenBucket(interval time.Duration, burst int) *tokenBucket {
	return &tokenBucket{interval: interval, burst: burst, tokens: burst, lastRefill: time.Now()}
}

// take consumes a slot, reporting false when the bucket is empty.
func (b *tokenBucket) take() bool {
	return b.takeAt(time.Now())
}

// takeAt is take with an explicit clock, for tests.
func (b *tokenBucket) takeAt(now time.Time) bool {
	if b.interval <= 0 {
		return true
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	// Credit whole elapsed intervals, keeping the remainder on the clock so a
	// steady sub-interval caller can't refill faster by polling.
	if elapsed := now.Sub(b.lastRefill); elapsed >= b.interval {
		refilled := int(elapsed / b.interval)
		if b.tokens+refilled >= b.burst || refilled < 0 { // refilled < 0: overflow of a huge gap
			b.tokens = b.burst
			b.lastRefill = now
		} else {
			b.tokens += refilled
			b.lastRefill = b.lastRefill.Add(time.Duration(refilled) * b.interval)
		}
	}

	if b.tokens <= 0 {
		return false
	}

	b.tokens--

	return true
}

// streamGate caps concurrently held slots: the global SSE stream cap. A
// non-positive limit means no cap.
type streamGate struct {
	limit int

	mu sync.Mutex
	n  int
}

// newStreamGate returns a gate admitting at most limit concurrent holders.
func newStreamGate(limit int) *streamGate {
	return &streamGate{limit: limit}
}

// acquire claims a slot, reporting false when the gate is full. Every true
// return must be paired with a release.
func (g *streamGate) acquire() bool {
	if g.limit <= 0 {
		return true
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.n >= g.limit {
		return false
	}

	g.n++

	return true
}

// release frees a slot claimed by acquire.
func (g *streamGate) release() {
	if g.limit <= 0 {
		return
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	g.n--
}

// retryAfterSeconds renders a duration as a Retry-After header value: whole
// seconds, rounded up, never below 1 (a "0" would invite an instant retry
// storm — the opposite of a throttle's point).
func retryAfterSeconds(d time.Duration) string {
	secs := max(int64((d+time.Second-1)/time.Second), 1)

	return strconv.FormatInt(secs, 10)
}
