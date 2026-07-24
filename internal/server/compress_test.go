package server_test

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/server"
)

// jsonHandler writes body as an application/json response — the shape of
// every API route, and the type the middleware is meant to compress.
func jsonHandler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})
}

// gunzip decodes a complete gzip body, failing the test if it is not one.
func gunzip(t *testing.T, b []byte) string {
	t.Helper()

	zr, err := gzip.NewReader(strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer func() { _ = zr.Close() }()

	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}

	return string(out)
}

func TestCompressResponsesGzipsJSON(t *testing.T) {
	t.Parallel()

	// A body long enough that gzip beats it comfortably — the real payloads
	// (the map, a turn bundle) are hugely repetitive, so this mirrors them.
	body := strings.Repeat(`{"hp":6,"maxHp":6,"inCombat":false},`, 200)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/map", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	server.CompressResponsesForTest(jsonHandler(body)).ServeHTTP(rec, req)

	resp := rec.Result()
	defer func() { _ = resp.Body.Close() }()

	if got, want := resp.Header.Get("Content-Encoding"), "gzip"; got != want {
		t.Errorf("Content-Encoding = %q, want %q", got, want)
	}

	if got, want := resp.Header.Get("Vary"), "Accept-Encoding"; !strings.Contains(got, want) {
		t.Errorf("Vary = %q, should contain %q", got, want)
	}

	if got, want := gunzip(t, rec.Body.Bytes()), body; got != want {
		t.Errorf("decompressed body = %q, want %q", got[:min(len(got), 60)], want[:min(len(want), 60)])
	}

	if got, want := rec.Body.Len(), len(body); got >= want {
		t.Errorf("compressed size = %d, want < uncompressed %d", got, want)
	}
}

func TestCompressResponsesSkipsClientsThatDoNotAcceptGzip(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("a", 4096)

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/map", nil)

	server.CompressResponsesForTest(jsonHandler(body)).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty for a non-gzip client", got)
	}

	if got, want := rec.Body.String(), body; got != want {
		t.Errorf("body length = %d, want %d (identity)", len(got), len(want))
	}

	// Vary is announced even when the response is not compressed, so a shared
	// cache cannot hand this identity body to a gzip client (or vice versa).
	if got, want := rec.Header().Get("Vary"), "Accept-Encoding"; !strings.Contains(got, want) {
		t.Errorf("Vary = %q, should contain %q", got, want)
	}
}

// TestCompressResponsesHonoursQZero: "gzip;q=0" is the only way a client can
// explicitly refuse the coding (RFC 9110 §12.5.3).
func TestCompressResponsesHonoursQZero(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/map", nil)
	req.Header.Set("Accept-Encoding", "gzip;q=0")

	server.CompressResponsesForTest(jsonHandler(strings.Repeat("a", 4096))).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty when the client sent gzip;q=0", got)
	}
}

// TestCompressResponsesSkipsUncompressibleTypes: the allowlist keeps the CPU
// away from bytes that are already compressed (sprite sheets, fonts).
func TestCompressResponsesSkipsUncompressibleTypes(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, strings.Repeat("a", 4096))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/sprite.png", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	server.CompressResponsesForTest(handler).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Errorf("Content-Encoding = %q, want empty for image/png", got)
	}
}

// TestCompressResponsesDropsContentLength: the header describes the identity
// body, so leaving it on a gzipped response would truncate it at the client.
func TestCompressResponsesDropsContentLength(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", "4096")
		_, _ = io.WriteString(w, strings.Repeat("a", 4096))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/map", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	server.CompressResponsesForTest(handler).ServeHTTP(rec, req)

	if got := rec.Header().Get("Content-Length"); got != "" {
		t.Errorf("Content-Length = %q, want it dropped on a compressed response", got)
	}
}

// TestCompressResponsesNeverDoubleEncodes: a handler that already produced a
// coded body owns its Content-Encoding.
func TestCompressResponsesNeverDoubleEncodes(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "br")
		_, _ = io.WriteString(w, strings.Repeat("a", 4096))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/map", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	server.CompressResponsesForTest(handler).ServeHTTP(rec, req)

	if got, want := rec.Header().Get("Content-Encoding"), "br"; got != want {
		t.Errorf("Content-Encoding = %q, want %q untouched", got, want)
	}
}

// TestCompressResponsesFlushIsReadableImmediately is THE regression test for
// this middleware: it pins that a flushed SSE frame is decodable by the client
// before the handler returns.
//
// Without a gz.Flush() (a zlib sync marker) inside Flush, the frame stays in
// the compressor's window and the client sees nothing until a later write
// overflows it — i.e. the game visibly freezes while every unit test that only
// checks the FINAL body still passes. Reading the bytes mid-handler is the
// only way to catch that.
func TestCompressResponsesFlushIsReadableImmediately(t *testing.T) {
	t.Parallel()

	const frame = "id: 1\nevent: turn\ndata: {\"turn\":1}\n\n"

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/events", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	var midStream []byte

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		flusher, ok := w.(http.Flusher)
		if !ok {
			// handleEvents makes exactly this assertion (events.go) and 500s
			// when it fails, so losing Flush would break SSE outright.
			t.Error("compressed ResponseWriter does not implement http.Flusher")

			return
		}

		_, _ = io.WriteString(w, frame)

		flusher.Flush()

		// Snapshot what the client could read at this instant — the handler
		// has NOT returned, so nothing has closed the gzip stream.
		midStream = append(midStream, rec.Body.Bytes()...)

		// Block-forever behaviour of a real stream, minus the blocking.
	})

	server.CompressResponsesForTest(handler).ServeHTTP(rec, req)

	zr, err := gzip.NewReader(strings.NewReader(string(midStream)))
	if err != nil {
		t.Fatalf("mid-stream bytes are not a readable gzip stream: %v", err)
	}
	defer func() { _ = zr.Close() }()

	// ReadFull, not ReadAll: the stream is deliberately unterminated here, so
	// ReadAll would report the missing trailer instead of the flushed frame.
	buf := make([]byte, len(frame))
	if _, err := io.ReadFull(zr, buf); err != nil {
		t.Fatalf("flushed frame not readable mid-stream: %v", err)
	}

	if got, want := string(buf), frame; got != want {
		t.Errorf("flushed frame = %q, want %q", got, want)
	}
}
