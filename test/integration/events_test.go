package integration_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// sseFrame is one parsed server-sent event.
type sseFrame struct {
	id    string
	event string
	data  string
}

const frameReadTimeout = 5 * time.Second

// readFrames reads SSE frames (skipping comment-only frames) until count
// frames arrive or the deadline hits.
func readFrames(t *testing.T, r *bufio.Reader, count int) []sseFrame {
	t.Helper()

	frames := make([]sseFrame, 0, count)

	done := make(chan struct{})
	go func() {
		defer close(done)

		var cur sseFrame

		for len(frames) < count {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}

			line = strings.TrimRight(line, "\n")
			switch {
			case line == "":
				if cur.event != "" || cur.data != "" {
					frames = append(frames, cur)
				}

				cur = sseFrame{}
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	select {
	case <-done:
	case <-time.After(frameReadTimeout):
		t.Fatalf("read %d/%d SSE frames before deadline", len(frames), count)
	}

	return frames
}

func TestEventsStreamsTurns(t *testing.T) {
	t.Parallel()

	ts := startServer(t, 20*time.Millisecond, time.Hour)

	resp := get(t, ts, "/api/events")
	if got, want := resp.StatusCode, http.StatusOK; got != want {
		t.Fatalf("status = %d, want 200", got)
	}

	if got, want := resp.Header.Get("Content-Type"), "text/event-stream"; got != want {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	// First frame is the immediate snapshot (turn may be 0), then live turns.
	frames := readFrames(t, bufio.NewReader(resp.Body), 3)

	prev := int64(-1)

	for _, f := range frames {
		if got, want := f.event, protocol.EventTurn; got != want {
			t.Fatalf("event = %q, want %q", got, want)
		}

		var payload protocol.TurnEvent
		if err := json.Unmarshal([]byte(f.data), &payload); err != nil {
			t.Fatalf("unmarshal %q: %v", f.data, err)
		}

		id, err := strconv.ParseInt(f.id, 10, 64)
		if err != nil {
			t.Fatalf("frame id %q is not a number: %v", f.id, err)
		}

		if got, want := id, payload.Turn; got != want {
			t.Fatalf("frame id %d != payload turn %d (id must be the turn for Last-Event-ID resume)", got, want)
		}

		if payload.Turn <= prev {
			t.Fatalf("turns not increasing: %d after %d", payload.Turn, prev)
		}

		prev = payload.Turn
	}
}

func TestEventsHeartbeat(t *testing.T) {
	t.Parallel()

	// Frozen clock, fast heartbeat: the stream must still carry comment
	// frames so proxies keep the connection alive.
	ts := startServer(t, time.Hour, 20*time.Millisecond)

	resp := get(t, ts, "/api/events")

	reader := bufio.NewReader(resp.Body)
	deadline := time.After(5 * time.Second)
	got := make(chan string, 1)

	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			if strings.HasPrefix(line, ":") {
				got <- line

				return
			}
		}
	}()

	select {
	case <-got:
	case <-deadline:
		t.Fatal("no heartbeat comment frame arrived on an idle stream")
	}
}

// getWithLastEventID issues a GET with the SSE reconnection header set, the way
// the browser's EventSource does automatically on reconnect.
func getWithLastEventID(t *testing.T, ts *httptest.Server, path, lastEventID string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("build request for %s: %v", path, err)
	}

	req.Header.Set("Last-Event-ID", lastEventID)

	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}

	t.Cleanup(func() { _ = resp.Body.Close() })

	return resp
}

// readFrameWithin reads one SSE data frame, returning ok=false if none arrives
// before timeout — used to assert a frame is deliberately withheld.
func readFrameWithin(t *testing.T, r *bufio.Reader, timeout time.Duration) (sseFrame, bool) {
	t.Helper()

	type result struct {
		frame sseFrame
		ok    bool
	}

	ch := make(chan result, 1)

	go func() {
		var cur sseFrame

		for {
			line, err := r.ReadString('\n')
			if err != nil {
				ch <- result{}

				return
			}

			line = strings.TrimRight(line, "\n")
			switch {
			case line == "":
				if cur.event != "" || cur.data != "" {
					ch <- result{cur, true}

					return
				}

				cur = sseFrame{}
			case strings.HasPrefix(line, "id: "):
				cur.id = strings.TrimPrefix(line, "id: ")
			case strings.HasPrefix(line, "event: "):
				cur.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()

	select {
	case res := <-ch:
		return res.frame, res.ok
	case <-time.After(timeout):
		return sseFrame{}, false
	}
}

// TestLastEventIDWithholdsAlreadySeenTurn: with a frozen clock the world stays
// at turn 0, so a client reconnecting with Last-Event-ID: 0 must NOT be
// re-sent turn 0 — it already has it.
func TestLastEventIDWithholdsAlreadySeenTurn(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour) // frozen clock + heartbeat

	fresh := get(t, ts, "/api/events")
	frames := readFrames(t, bufio.NewReader(fresh.Body), 1)

	if got, want := frames[0].id, "0"; got != want {
		t.Fatalf("first frame id = %q, want 0", got)
	}

	reconnect := getWithLastEventID(t, ts, "/api/events", "0")
	if _, ok := readFrameWithin(t, bufio.NewReader(reconnect.Body), 300*time.Millisecond); ok {
		t.Fatal("server re-sent an already-seen turn to a reconnecting client")
	}
}

// TestLastEventIDSendsCurrentWhenMismatched: a Last-Event-ID that does not match
// the current turn (ahead, as after a server restart, or garbage) still yields
// the current snapshot — the watermark must not over-withhold.
func TestLastEventIDSendsCurrentWhenMismatched(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Hour, time.Hour)

	for _, id := range []string{"999", "not-a-number"} {
		resp := getWithLastEventID(t, ts, "/api/events", id)

		frame, ok := readFrameWithin(t, bufio.NewReader(resp.Body), 5*time.Second)
		if !ok {
			t.Fatalf("Last-Event-ID %q: no snapshot delivered", id)
		}

		if got, want := frame.id, "0"; got != want {
			t.Fatalf("Last-Event-ID %q: frame id = %q, want current turn 0", id, got)
		}
	}
}
