package chat_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/chat"
	"github.com/starquake/mediumrogue/internal/protocol"
)

func TestRunCommandHereFormatsLocation(t *testing.T) {
	t.Parallel()

	s := chat.Sender{Name: "alice", Hex: protocol.Hex{Q: 5, R: -3}}

	out, err := chat.RunCommand("/here", s)
	if err != nil {
		t.Fatalf("RunCommand(/here) error = %v", err)
	}

	if got, want := out, "(5, -3)"; !strings.Contains(got, want) {
		t.Errorf("out = %q, should contain %q", got, want)
	}
}

func TestRunCommandUnknownVerb(t *testing.T) {
	t.Parallel()

	_, err := chat.RunCommand("/bogus stuff", chat.Sender{Name: "x"})
	if got, want := err, chat.ErrUnknownCommand; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}

func TestRunCommandEmpty(t *testing.T) {
	t.Parallel()

	_, err := chat.RunCommand("/", chat.Sender{Name: "x"})
	if got, want := err, chat.ErrEmptyCommand; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}
