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
	"log/slog"
	"net/http"
	neturl "net/url"
	"strings"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// Reconnect backoff (#430). A dropped SSE stream is an ORDINARY event, not a
// failure: every `deploy:dev` push restarts development and drops every
// connected stream, which happens several times a day while a PR is in flight.
// Before this, that ended a playtest party silently — the process exited 0 and
// nothing on screen said the bots were gone.
const (
	reconnectInitialDelay = time.Second
	reconnectMaxDelay     = 30 * time.Second
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

// Event is one thing the world told us: a resolved turn, or a chat line.
//
// Both arrive on the same SSE stream and a bot needs both — a party INVITE is
// announced only as chat (party.go), with no field on the bundle to read — so
// splitting them into two subscriptions would mean two streams and two
// presences for one player.
type Event struct {
	// Turn is set for a resolved world turn; Chat is nil.
	Turn *protocol.TurnEvent
	// Chat is set for a chat line; Turn is nil.
	Chat *protocol.ChatMessage
}

// Turns streams resolved turn bundles until ctx is cancelled or the stream
// drops. Heartbeat frames are consumed and not surfaced: they exist to keep the
// connection warm, and a bot has nothing to do with one.
//
// The channel is closed when the stream ends, so a caller ranging over it
// learns about a disconnect by the range finishing.
//
// Chat is dropped here. Use Events for a bot that needs it.
func (c *Client) Turns(ctx context.Context) (<-chan protocol.TurnEvent, error) {
	//nolint:bodyclose // closed by the streaming goroutine below.
	resp, err := c.openStream(ctx)
	if err != nil {
		return nil, err
	}

	out := make(chan protocol.TurnEvent)

	go func() {
		defer close(out)
		defer func() { _ = resp.Body.Close() }()

		scanFrames(bufio.NewReader(resp.Body), func(ev Event) bool {
			if ev.Turn == nil {
				return true // chat: not this subscription's business
			}

			select {
			case out <- *ev.Turn:
				return true
			case <-ctx.Done():
				return false
			}
		})
	}()

	return out, nil
}

// Events is Turns plus chat, for a bot that must react to what players say —
// accepting a party invite, above all.
func (c *Client) Events(ctx context.Context) (<-chan Event, error) {
	// The FIRST open still fails loudly, and that asymmetry is deliberate: a
	// typo'd URL or a refused token should stop a bot at startup rather than
	// leave it retrying forever against an address that will never answer.
	// Only a stream that once worked and then dropped is reconnected.
	//nolint:bodyclose // closed by the streaming goroutine below.
	resp, err := c.openStream(ctx)
	if err != nil {
		return nil, err
	}

	out := make(chan Event)

	go c.streamWithReconnect(ctx, resp, out)

	return out, nil
}

// Say sends a chat line — including the slash verbs, which is how a bot
// accepts a party invite.
func (c *Client) Say(ctx context.Context, text string) error {
	resp, err := c.post(ctx, "/api/chat", protocol.ChatRequest{Token: c.identity.Token, Text: text})
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("%w: chat: status %d", ErrIntentRejected, resp.StatusCode)
	}

	return nil
}

// openStream opens the SSE connection.
//
// The token goes on the query string, exactly as the browser does it
// (client/src/net/events.ts). It does two things, and BOTH were missing while
// this connected anonymously: it selects the VIEWER, so own-only fields
// (cooldowns, energy, skills) are populated rather than zero — a bot reading a
// zeroed HealthPotionReadyIn quaffs on cooldown forever — and it registers the
// stream for presence, so the bot is a connected player rather than one
// silently running down its disconnect grace.
func (c *Client) openStream(ctx context.Context) (*http.Response, error) {
	url := c.baseURL + "/api/events"
	if c.identity.Token != "" {
		url += "?token=" + neturl.QueryEscape(c.identity.Token)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("botclient: events request: %w", err)
	}

	//nolint:bodyclose // closed by the streaming goroutine in the caller, which bodyclose cannot follow.
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("botclient: open event stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()

		return nil, fmt.Errorf("%w: status %d", ErrStream, resp.StatusCode)
	}

	return resp, nil
}

// scanFrames parses the SSE stream and hands each turn or chat frame to onEvent,
// stopping when it returns false or the stream ends.
//
// SSE framing: `id:`/`event:`/`data:` lines, a blank line ending the frame.
// Only the event and data matter here — the id is the turn number, which the
// bundle already carries.
// dispatchFrame decodes one completed SSE frame and delivers it, reporting
// whether scanning should continue. A frame that fails to decode is skipped
// rather than ending the stream: one malformed line must not kill a bot.
func dispatchFrame(event, data string, onEvent func(Event) bool) bool {
	if data == "" {
		return true
	}

	switch event {
	case protocol.EventTurn:
		var bundle protocol.TurnEvent
		if json.Unmarshal([]byte(data), &bundle) != nil {
			return true
		}

		return onEvent(Event{Turn: &bundle})
	case protocol.EventChat:
		var msg protocol.ChatMessage
		if json.Unmarshal([]byte(data), &msg) != nil {
			return true
		}

		return onEvent(Event{Chat: &msg})
	default:
		return true // heartbeat and anything new: nothing for a bot to do
	}
}

func scanFrames(r *bufio.Reader, onEvent func(Event) bool) {
	var event, data string

	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}

		switch line = strings.TrimRight(line, "\n"); {
		case line == "":
			if !dispatchFrame(event, data, onEvent) {
				return
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

// streamWithReconnect consumes one stream after another on a single channel,
// reconnecting across drops until ctx is cancelled (#430). The consumer sees
// one uninterrupted channel and needs no reconnect logic of its own — which is
// why this lives here rather than in cmd/bot: every caller would otherwise
// write the same loop, and the one that forgot would be the one that died
// overnight.
//
// The channel closes on CANCELLATION ONLY. That is the contract cmd/bot reads
// to tell Ctrl-C from a drop, and it is now true rather than approximately
// true.
func (c *Client) streamWithReconnect(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer close(out)

	delay := reconnectInitialDelay

	for {
		consumeStream(ctx, resp, out)

		if ctx.Err() != nil {
			return
		}

		slog.Info("botclient: event stream dropped, reconnecting",
			"entity", c.identity.EntityID, "in", delay)

		if !sleepCtx(ctx, delay) {
			return
		}

		next, err := c.openStream(ctx) //nolint:bodyclose // closed by consumeStream.
		if err != nil {
			// Still down. Keep the backoff growing and try again — a bot has
			// no reason to give up while its process lives, and a server that
			// is mid-restart is exactly the case this exists for.
			slog.Warn("botclient: reconnect failed", "entity", c.identity.EntityID, "err", err)

			delay = min(delay*2, reconnectMaxDelay)

			continue
		}

		slog.Info("botclient: event stream reconnected", "entity", c.identity.EntityID)

		resp = next
		delay = reconnectInitialDelay
	}
}

// consumeStream reads one response to exhaustion, forwarding frames to out.
func consumeStream(ctx context.Context, resp *http.Response, out chan<- Event) {
	defer func() { _ = resp.Body.Close() }()

	scanFrames(bufio.NewReader(resp.Body), func(ev Event) bool {
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	})
}

// sleepCtx waits for d, reporting false if ctx was cancelled first — so a
// Ctrl-C during a backoff stops the bot immediately rather than after the full
// delay.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()

	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
