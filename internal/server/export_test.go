package server

import "time"

// RequireSameOriginPostsForTest re-exports the cross-origin POST guard
// (middleware.go) so the external test package can drive it directly with a
// stub next handler, without booting the whole handler tree.
var RequireSameOriginPostsForTest = requireSameOriginPosts

// IntentErrorStatusForTest re-exports handleIntent's error→status mapping
// (api.go) so a test can drive every game sentinel through it — Deps.World is
// a concrete *game.World, so no stub can make SubmitIntent return an
// arbitrary error. Returns the status and whether the error was recognized.
var IntentErrorStatusForTest = intentErrorStatus

// Limiter re-exports (#199): the constructors plus explicit-clock wrappers,
// so the external test package can drive rate windows deterministically
// without sleeping.
var (
	NewPerKeyLimiterForTest    = newPerKeyLimiter
	NewTokenBucketForTest      = newTokenBucket
	NewStreamGateForTest       = newStreamGate
	NewPerKeyStreamGateForTest = newPerKeyStreamGate
	RetryAfterSecondsForTest   = retryAfterSeconds
	// ClientIPForTest re-exports clientIP (events.go) so the XFF/RemoteAddr
	// derivation can be table-tested without booting a stream.
	ClientIPForTest = clientIP
)

// AllowAtForTest exposes perKeyLimiter.allowAt.
func (l *perKeyLimiter) AllowAtForTest(key string, now time.Time) bool { return l.allowAt(key, now) }

// LenForTest exposes the perKeyLimiter map size, for the prune test.
func (l *perKeyLimiter) LenForTest() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return len(l.last)
}

// TakeAtForTest exposes tokenBucket.takeAt.
func (b *tokenBucket) TakeAtForTest(now time.Time) bool { return b.takeAt(now) }

// AcquireForTest exposes streamGate.acquire.
func (g *streamGate) AcquireForTest() bool { return g.acquire() }

// ReleaseForTest exposes streamGate.release.
func (g *streamGate) ReleaseForTest() { g.release() }

// AcquireForTest exposes perKeyStreamGate.acquire.
func (g *perKeyStreamGate) AcquireForTest(key string) bool { return g.acquire(key) }

// ReleaseForTest exposes perKeyStreamGate.release.
func (g *perKeyStreamGate) ReleaseForTest(key string) { g.release(key) }
