package chat

import (
	"errors"
	"fmt"
	"strings"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// ErrUnknownCommand and ErrEmptyCommand are the command-parse failures; the
// HTTP layer maps both to 422.
var (
	ErrUnknownCommand = errors.New("unknown command")
	ErrEmptyCommand   = errors.New("empty command")
)

// Sender is the authoritative context a command runs with — resolved
// server-side from the caller's token, so e.g. /here can't be spoofed.
type Sender struct {
	Name string
	Hex  protocol.Hex
}

// cmdFunc turns a command's args + sender into the text to broadcast.
type cmdFunc func(s Sender, args []string) (string, error)

// commands is the "/" registry. 8.2/8.3 add /invite, /quest, … here.
//
//nolint:gochecknoglobals // a fixed command table, effectively const.
var commands = map[string]cmdFunc{
	"here": cmdHere,
}

// RunCommand parses "/verb args…" and returns the text to broadcast (as a
// normal ChatMessage from the sender). Unknown/empty verbs are errors (422),
// not broadcasts.
func RunCommand(input string, s Sender) (string, error) {
	fields := strings.Fields(strings.TrimPrefix(input, "/"))
	if len(fields) == 0 {
		return "", ErrEmptyCommand
	}

	fn, ok := commands[strings.ToLower(fields[0])]
	if !ok {
		return "", fmt.Errorf("%w: /%s", ErrUnknownCommand, fields[0])
	}

	return fn(s, fields[1:])
}

// cmdHere shares the sender's authoritative position.
func cmdHere(s Sender, _ []string) (string, error) {
	return fmt.Sprintf("📍 at (%d, %d)", s.Hex.Q, s.Hex.R), nil
}
