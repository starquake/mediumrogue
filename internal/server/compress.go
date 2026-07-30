package server

import (
	"compress/gzip"
	"io"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
)

// compressibleTypes are the response media types worth gzipping (#288). It is
// an allowlist rather than a "compress everything except images" denylist so a
// future binary route can never silently pay the CPU to re-compress bytes that
// are already compressed.
//
// text/event-stream earns its place for the reason this middleware exists: a
// turn bundle is ~1000 near-identical entity records, which gzip collapses
// ~22x. It is also the one type whose flush semantics matter — see
// gzipResponseWriter.Flush.
//
//nolint:gochecknoglobals // fixed media-type allowlist, effectively const.
var compressibleTypes = []string{
	"application/javascript",
	"application/json",
	"image/svg+xml",
	"text/css",
	"text/event-stream",
	"text/html",
	"text/javascript",
	"text/plain",
}

// gzipPool recycles gzip writers. The SSE stream compresses one bundle per
// client per turn forever, so allocating a writer (and its window) per
// response would be steady, pointless churn.
//
//nolint:gochecknoglobals // writer pool, the standard sync.Pool idiom.
var gzipPool = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

// compressResponses gzips responses for clients that accept it.
//
// Measured at the #286 big-world config (WORLD_RADIUS=120, MONSTER_COUNT=1000):
// GET /api/map is 1,885,659 B and compresses to 118,713 B (16x); one SSE turn
// bundle is 230,937 B and compresses to ~10,520 B (22x). At the production 4s
// cadence that is 462 kbit/s per client before, ~21 kbit/s after.
//
// There is deliberately NO minimum-size threshold. Buffering the first writes
// to decide "is this body big enough to be worth compressing" is exactly the
// machinery that risks stalling the SSE stream, and the saving on a ~200-byte
// join response is nil. Media type alone decides.
func compressResponses(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Announced on every response, compressed or not, so a shared cache
		// can never serve a gzipped body to a client that cannot read it.
		w.Header().Add("Vary", "Accept-Encoding")

		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)

			return
		}

		gw := &gzipResponseWriter{ResponseWriter: w}
		defer gw.close()

		next.ServeHTTP(gw, r)
	})
}

// gzipResponseWriter compresses the body on its way out. Whether to compress
// at all is decided at WriteHeader time, because that is the first moment the
// handler's Content-Type is known.
type gzipResponseWriter struct {
	http.ResponseWriter

	// gz is nil when this response is passing through uncompressed.
	gz          *gzip.Writer
	wroteHeader bool
}

func (w *gzipResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}

	w.wroteHeader = true

	if w.shouldCompress(status) {
		h := w.Header()
		h.Set("Content-Encoding", "gzip")
		// Content-Length describes the identity body; the compressed body is a
		// different and (streaming) unknown length, so the response goes out
		// chunked instead. Leaving it would truncate the body at the client.
		h.Del("Content-Length")

		// A pool holding anything else would nil-deref on the next line, which
		// is a worse failure than simply making a writer.
		gz, ok := gzipPool.Get().(*gzip.Writer)
		if !ok {
			gz = gzip.NewWriter(w.ResponseWriter)
		}

		gz.Reset(w.ResponseWriter)
		w.gz = gz
	}

	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	if w.gz != nil {
		n, err := w.gz.Write(b)
		if err != nil {
			return n, err //nolint:wrapcheck // ResponseWriter.Write returns the raw write error.
		}

		return n, nil
	}

	return w.ResponseWriter.Write(b) //nolint:wrapcheck // ditto: this IS the ResponseWriter.
}

// Flush makes everything written so far readable by the client.
//
// Getting this wrong is how gzip freezes the game. handleEvents writes a turn
// bundle and calls Flush; if the bundle is still sitting in the compressor's
// window, the client sees nothing until a later write happens to overflow it.
// gz.Flush emits a zlib sync marker, which ends the current deflate block and
// makes everything written so far decodable on its own — only then is flushing
// the socket underneath meaningful.
//
// handleEvents also asserts w.(http.Flusher) and 500s without it, so a wrapper
// that dropped this method would fail the stream loudly rather than stall it
// silently.
func (w *gzipResponseWriter) Flush() {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	if w.gz != nil {
		_ = w.gz.Flush()
	}

	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// close finishes the gzip stream and returns the writer to the pool. Always
// deferred by compressResponses: without the Close, the final deflate block
// and the gzip trailer are never written and the client sees a truncated body.
func (w *gzipResponseWriter) close() {
	if w.gz == nil {
		return
	}

	_ = w.gz.Close()
	gzipPool.Put(w.gz)
	w.gz = nil
}

// shouldCompress reports whether this response should be gzipped: a status
// that carries a body, a body not already coded by the handler, and a media
// type on the allowlist.
func (w *gzipResponseWriter) shouldCompress(status int) bool {
	if status < http.StatusOK || status == http.StatusNoContent || status == http.StatusNotModified {
		return false
	}

	h := w.Header()
	if h.Get("Content-Encoding") != "" {
		return false
	}

	return compressibleType(h.Get("Content-Type"))
}

// compressibleType matches a Content-Type header against the allowlist,
// ignoring parameters ("application/json; charset=utf-8").
func compressibleType(contentType string) bool {
	media, _, _ := strings.Cut(contentType, ";")

	return slices.Contains(compressibleTypes, strings.ToLower(strings.TrimSpace(media)))
}

// acceptsGzip reports whether r's Accept-Encoding invites a gzip body. An
// explicit "gzip;q=0" is honoured as the refusal it is (RFC 9110 §12.5.3);
// an absent header means no compression, which keeps curl and the Go
// integration tests on identity bodies unless they ask otherwise.
func acceptsGzip(r *http.Request) bool {
	for coding := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(coding, ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}

		return !qualityZero(params)
	}

	return false
}

// qualityZero reports whether an Accept-Encoding parameter list rejects the
// coding outright with q=0.
func qualityZero(params string) bool {
	for p := range strings.SplitSeq(params, ";") {
		key, value, ok := strings.Cut(p, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(key), "q") {
			continue
		}

		q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)

		return err == nil && q == 0
	}

	return false
}
