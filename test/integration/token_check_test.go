package integration_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// TestTokenCheck drives the start-screen probe (#311) over real HTTP: the
// client asks whether its stored token still reclaims a character BEFORE it
// offers the welcome-back card, so a world that was reset out from under it
// shows the creation form on the first click instead of the second.
func TestTokenCheck(t *testing.T) {
	t.Parallel()

	ts := startServer(t, time.Minute, time.Minute)

	joined := joinWith(t, ts, protocol.JoinRequest{
		Name: testerName, Class: protocol.ClassFighter, Species: protocol.SpeciesHuman,
	})

	for _, tc := range []struct {
		name  string
		token string
		want  bool
	}{
		{name: "live token", token: joined.Token, want: true},
		{name: "unknown token", token: "not-a-real-token", want: false},
		{name: "empty token", token: "", want: false},
	} {
		resp := postJSON(t, ts, "/api/token-check", protocol.TokenCheckRequest{Token: tc.token})
		if got, want := resp.StatusCode, http.StatusOK; got != want {
			t.Fatalf("%s: token-check status = %d, want %d", tc.name, got, want)
		}

		var checked protocol.TokenCheckResponse

		err := json.NewDecoder(resp.Body).Decode(&checked)
		_ = resp.Body.Close()

		if err != nil {
			t.Fatalf("%s: decode token-check response: %v", tc.name, err)
		}

		if got, want := checked.Known, tc.want; got != want {
			t.Errorf("%s: known = %v, want %v", tc.name, got, want)
		}
	}
}
