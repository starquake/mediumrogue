// Package botclient speaks the player wire — HTTP up, SSE down — so a headless
// bot is indistinguishable from a browser to the server (#369).
//
// Deliberately over the wire rather than against *game.World in-process
// (decided 2026-08-05): a bot has to prove the real path and be able to stand
// in a world someone is actually playing in. The in-process route already
// exists and stays cmd/balance's job.
//
// This package is the plumbing only — join, read turns, submit intents. What a
// bot DOES with a turn belongs to its behaviour (#370).
package botclient

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/starquake/mediumrogue/internal/protocol"
)

var (
	// ErrJoinRejected is returned when the server refuses the join outright — a
	// bad name, an unknown class. Distinct from a transport failure so a caller
	// can tell "the server said no" from "the server is not there".
	ErrJoinRejected = errors.New("botclient: join rejected")
	// ErrIntentRejected is the world declining a well-formed intent (422) or the
	// request being malformed. The caller interprets it — a bot aiming at a hex
	// it cannot reach is normal, not a fault.
	ErrIntentRejected = errors.New("botclient: intent rejected")
	// ErrStream is the event stream failing to open or ending unexpectedly.
	ErrStream = errors.New("botclient: event stream")
)

// Client is one bot's connection to a world. Not safe for concurrent use: one
// bot, one goroutine, which is what the turn loop wants anyway.
type Client struct {
	baseURL  string
	http     *http.Client
	identity protocol.JoinResponse
}

// Identity is the joined character — entity id, token, and the hex it woke up
// on. The token is what makes a bot reclaimable across restarts, exactly as a
// browser's stored identity is.
func (c *Client) Identity() protocol.JoinResponse { return c.identity }

// Join registers with the world and returns a connected client. Passing a token
// in req reclaims that character instead of creating one; the server ignores
// name/class/species on a reclaim, so a restarting bot may send them anyway.
func Join(ctx context.Context, baseURL string, req protocol.JoinRequest) (*Client, error) {
	c := &Client{baseURL: strings.TrimRight(baseURL, "/"), http: &http.Client{}}

	resp, err := c.post(ctx, "/api/join", req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: status %d", ErrJoinRejected, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&c.identity); err != nil {
		return nil, fmt.Errorf("botclient: decode join response: %w", err)
	}

	return c, nil
}

// Submit sends one intent. The caller fills EntityID and Token from Identity;
// they are not injected here, because a bot testing a rejection may want to
// send the wrong ones deliberately.
func (c *Client) Submit(ctx context.Context, req protocol.IntentRequest) error {
	resp, err := c.post(ctx, "/api/intent", req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	// 202 is the accept. A 422 is the world saying no to a well-formed
	// request — a real answer, and the caller's to interpret, so it is
	// reported rather than swallowed.
	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("%w: %s: status %d", ErrIntentRejected, req.Kind, resp.StatusCode)
	}

	return nil
}

// Turns streams resolved turn bundles until ctx is cancelled or the stream
// drops. Heartbeat frames are consumed and not surfaced: they exist to keep the
// connection warm, and a bot has nothing to do with one.
//
// The channel is closed when the stream ends, so a caller ranging over it
// learns about a disconnect by the range finishing.
func (c *Client) Turns(ctx context.Context) (<-chan protocol.TurnEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/events", nil)
	if err != nil {
		return nil, fmt.Errorf("botclient: events request: %w", err)
	}

	//nolint:bodyclose // closed by the streaming goroutine below, which bodyclose cannot follow.
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("botclient: open event stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()

		return nil, fmt.Errorf("%w: status %d", ErrStream, resp.StatusCode)
	}

	out := make(chan protocol.TurnEvent)

	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()

		scanFrames(bufio.NewReader(resp.Body), func(bundle protocol.TurnEvent) bool {
			select {
			case out <- bundle:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}()

	return out, nil
}

// scanFrames parses the SSE stream and hands each TURN bundle to onTurn,
// stopping when it returns false or the stream ends.
//
// SSE framing: `id:`/`event:`/`data:` lines, a blank line ending the frame.
// Only the event and data matter here — the id is the turn number, which the
// bundle already carries.
func scanFrames(r *bufio.Reader, onTurn func(protocol.TurnEvent) bool) {
	var event, data string

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}

		switch line = strings.TrimRight(line, "\n"); {
		case line == "":
			if event == protocol.EventTurn && data != "" {
				var bundle protocol.TurnEvent
				if json.Unmarshal([]byte(data), &bundle) == nil && !onTurn(bundle) {
					return
				}
			}

			event, data = "", ""
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		default:
			// `id:` and any future field: the bundle carries the turn number
			// itself, so nothing else in the frame is needed here.
		}
	}
}

func (c *Client) post(ctx context.Context, path string, body any) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("botclient: encode %s: %w", path, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("botclient: %s request: %w", path, err)
	}

	req.Header.Set("Content-Type", "application/json")
	// Same-origin POSTs are enforced by requireSameOriginPosts; a bot is a
	// legitimate client, so it presents an Origin like a browser would.
	req.Header.Set("Origin", c.baseURL)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("botclient: %s: %w", path, err)
	}

	return resp, nil
}
