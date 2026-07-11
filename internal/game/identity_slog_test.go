package game_test

// identity_slog_test.go: item 7, playtest feedback batch 3 — the identity
// audit log. The structured slog convention from batch 2's combat log,
// extended to identity lifecycle events (join-new/join-reclaim/join-restore/
// join-rejected/sweep-archive/snapshot-restore) so the next cross-machine
// "players swapped" report gets diagnosed from server logs. This test pins
// the load-bearing safety property: a logged token is TRUNCATED to a fixed
// prefix, never the full bearer secret.

import (
	"bytes"
	"log/slog"
	"testing"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// identityEventsOfKind filters slogEvents (combat_slog_test.go) down to
// msg=="identity" lines with the given event attribute.
func identityEventsOfKind(events []map[string]any, kind string) []map[string]any {
	var out []map[string]any

	for _, e := range events {
		if e["msg"] == "identity" && e["event"] == kind {
			out = append(out, e)
		}
	}

	return out
}

// TestIdentitySlogJoinNewTruncatesToken: a brand-new join emits a "join-new"
// identity event carrying id/name/class and a token PREFIX of exactly 8
// characters — matching the real token's first 8 chars and never the full
// secret (a full token in a log file would be a character-theft vector).
func TestIdentitySlogJoinNewTruncatesToken(t *testing.T) {
	t.Parallel()

	w := newWorld()

	var buf bytes.Buffer

	w.SetLogger(slog.New(slog.NewJSONHandler(&buf, nil)))

	resp, err := w.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman)
	if err != nil {
		t.Fatalf("Join: %v", err)
	}

	joins := identityEventsOfKind(slogEvents(t, &buf), "join-new")
	if len(joins) != 1 {
		t.Fatalf("join-new events = %d, want 1", len(joins))
	}

	ev := joins[0]

	if got, want := ev["id"], float64(resp.EntityID); got != want {
		t.Errorf("join-new id = %v, want %v", got, want)
	}

	if got, want := ev["name"], "tester"; got != want {
		t.Errorf("join-new name = %v, want %q", got, want)
	}

	if got, want := ev["class"], protocol.ClassFighter; got != want {
		t.Errorf("join-new class = %v, want %q", got, want)
	}

	prefix, ok := ev["token_prefix"].(string)
	if !ok {
		t.Fatalf("join-new token_prefix = %v (%T), want a string", ev["token_prefix"], ev["token_prefix"])
	}

	// Pin the prefix length: 8 chars, matching the token's own head, and
	// strictly shorter than the full token — the secret never lands whole.
	if got, want := len(prefix), 8; got != want {
		t.Errorf("token_prefix length = %d, want %d", got, want)
	}

	if got, want := prefix, resp.Token[:8]; got != want {
		t.Errorf("token_prefix = %q, want %q (the token's first 8 chars)", got, want)
	}

	if got, want := len(prefix) < len(resp.Token), true; got != want {
		t.Errorf("token_prefix is not shorter than the full token (%d vs %d chars)", len(prefix), len(resp.Token))
	}
}
