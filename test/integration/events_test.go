package integration_test

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/starquake/medium-rogue/internal/protocol"
)

// sseFrame is one parsed server-sent event.
type sseFrame struct {
	id    string
	event string
	data  string
}

// readFrames reads SSE frames (skipping comment-only frames) until count
// frames arrive or the deadline hits.
func readFrames(t *testing.T, r *bufio.Reader, count int, deadline time.Duration) []sseFrame {
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
	case <-time.After(deadline):
		t.Fatalf("read %d/%d SSE frames before deadline", len(frames), count)
	}

	return frames
}

func TestEventsStreamsTurns(t *testing.T) {
	t.Parallel()

	ts := startServer(t, 20*time.Millisecond, time.Hour)

	resp := get(t, ts, "/api/events")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", got)
	}

	// First frame is the immediate snapshot (turn may be 0), then live turns.
	frames := readFrames(t, bufio.NewReader(resp.Body), 3, 5*time.Second)

	prev := int64(-1)

	for _, f := range frames {
		if f.event != protocol.EventTurn {
			t.Fatalf("event = %q, want %q", f.event, protocol.EventTurn)
		}

		var payload protocol.TurnEvent
		if err := json.Unmarshal([]byte(f.data), &payload); err != nil {
			t.Fatalf("unmarshal %q: %v", f.data, err)
		}

		id, err := strconv.ParseInt(f.id, 10, 64)
		if err != nil {
			t.Fatalf("frame id %q is not a number: %v", f.id, err)
		}

		if id != payload.Turn {
			t.Fatalf("frame id %d != payload turn %d (id must be the turn for Last-Event-ID resume)", id, payload.Turn)
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
