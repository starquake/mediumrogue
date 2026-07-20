package server_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/server"
)

// api_test.go pins handleIntent's error→status mapping (#133). The bug it
// exists to prevent: ErrAttackTargetNotFound/ErrAttackTargetNotHostile were
// never added to the mapping when entity-targeted attacks landed, so an
// ordinary race — your victim dies between click and POST — surfaced as a
// 500 "internal error" for months, logged at Error level, and flaked the
// integration suite whenever a test's monster died a beat early.

// intentSentinelStatus is every game sentinel handleIntent can receive, with
// the status it must map to. TestEveryIntentSentinelIsMapped (below) fails if
// a sentinel exists in internal/game and appears in neither this table nor
// handledElsewhere — so adding an error forces a deliberate mapping choice
// rather than a silent 500.
var intentSentinelStatus = map[error]int{
	game.ErrUnauthorized:           http.StatusUnauthorized,
	game.ErrNotWalkable:            http.StatusUnprocessableEntity,
	game.ErrNoPath:                 http.StatusUnprocessableEntity,
	game.ErrNoRangedWeapon:         http.StatusUnprocessableEntity,
	game.ErrOutOfRange:             http.StatusUnprocessableEntity,
	game.ErrAttackTargetNotFound:   http.StatusUnprocessableEntity, // #133
	game.ErrAttackTargetNotHostile: http.StatusUnprocessableEntity, // #133
	game.ErrInvalidIntentKind:      http.StatusUnprocessableEntity,
	game.ErrItemNotOwned:           http.StatusUnprocessableEntity,
	game.ErrBackpackFull:           http.StatusUnprocessableEntity,
	game.ErrItemNotEquipped:        http.StatusUnprocessableEntity,
	game.ErrNotDrinkable:           http.StatusUnprocessableEntity,
	game.ErrNotEquippable:          http.StatusUnprocessableEntity,
	game.ErrNoSuchGroundItem:       http.StatusUnprocessableEntity,
	// Learn-skill rejections (#124): all well-formed requests the world says
	// no to, so 422 rather than 500.
	game.ErrNoSuchSkill:         http.StatusUnprocessableEntity,
	game.ErrSkillAlreadyLearned: http.StatusUnprocessableEntity,
	game.ErrSkillPrereqUnmet:    http.StatusUnprocessableEntity,
	game.ErrNoSkillPoints:       http.StatusUnprocessableEntity,
	game.ErrLearnInCombat:       http.StatusUnprocessableEntity,
	// Use-skill rejections (#161).
	game.ErrSkillNotActive:  http.StatusUnprocessableEntity,
	game.ErrSkillNotLearned: http.StatusUnprocessableEntity,
	game.ErrSkillOnCooldown: http.StatusUnprocessableEntity,
	game.ErrNoLineOfSight:   http.StatusUnprocessableEntity,
}

// handledElsewhere are the sentinels SubmitIntent can never return, each with
// the handler that owns it. Both of those handlers answer a non-500 for ANY
// error — Join's default is a deliberate 503, and the chat-command router
// answers 422 unconditionally — so an error reaching them cannot become an
// internal error the way an unmapped intent sentinel does.
//
// Listed by NAME because the completeness check reads names out of the
// source. This table is the "yes, this was considered" record: a new sentinel
// must land here or in intentSentinelStatus, deliberately, either way.
var handledElsewhere = map[string]string{
	// handleJoin (api.go) — its own switch; default 503.
	"ErrInvalidClass":    "Join rejects the class",
	"ErrInvalidSpecies":  "Join rejects the species",
	"ErrInvalidName":     "Join rejects the name",
	"ErrWorldFull":       "Join has no room — 503 by design",
	"ErrWorldAtCapacity": "Join past the player cap — 503 (#199)",
	// routeChatCommand (chat.go) — any error becomes 422 there.
	"ErrTargetNotFound":  "chat /invite names an absent player",
	"ErrInviteSelf":      "chat /invite yourself",
	"ErrInviteExpired":   "chat /accept (unreachable — see party.go)",
	"ErrAlreadyInParty":  "chat /invite, /accept",
	"ErrNotInParty":      "chat /leave",
	"ErrPartyNotJoined":  "chat party commands before joining",
	"ErrNoPendingInvite": "chat /accept",
	"ErrQuestNotFound":   "chat /quest",
	"ErrQuestTaken":      "chat /quest already taken",
	"ErrQuestSlotFull":   "chat /quest — party quest slot busy",
	"ErrQuestCompleted":  "chat /quest re-take",
	"ErrNoActiveQuest":   "chat /abandon",
}

// TestIntentErrorStatus drives every mapped sentinel through the real
// mapping. A regression that drops one (or mistypes its status) fails here
// with the sentinel named, not as a mystery 500 in an unrelated test.
func TestIntentErrorStatus(t *testing.T) {
	t.Parallel()

	for err, want := range intentSentinelStatus {
		if got, known := server.IntentErrorStatusForTest(err); !known || got != want {
			t.Errorf("intentErrorStatus(%v) = %d (known=%t), want %d (known=true)", err, got, known, want)
		}
	}
}

// TestIntentErrorStatusAcceptsNilAndRejectsUnknown pins the two ends: no
// error is 202, and an error the mapping does not know is reported unknown so
// handleIntent can log it and answer 500 — the behavior that is correct for a
// real server fault and wrong for a domain sentinel.
func TestIntentErrorStatusAcceptsNilAndRejectsUnknown(t *testing.T) {
	t.Parallel()

	if got, known := server.IntentErrorStatusForTest(nil); !known || got != http.StatusAccepted {
		t.Errorf("intentErrorStatus(nil) = %d (known=%t), want %d (known=true)", got, known, http.StatusAccepted)
	}

	if _, known := server.IntentErrorStatusForTest(unmappedTestError{}); known {
		t.Error("intentErrorStatus(unknown error) reported known=true, want false (so handleIntent answers 500)")
	}
}

type unmappedTestError struct{}

func (unmappedTestError) Error() string { return "not a domain sentinel" }

// TestEveryIntentSentinelIsMapped is the guard #133 asked for: it reads every
// exported Err* sentinel straight out of internal/game's source and fails on
// any that is neither mapped (intentSentinelStatus) nor explicitly handled
// elsewhere (handledElsewhere). Source-scanning rather than a hand-copied list is the
// point — Go cannot enumerate a package's vars at runtime, and a hand-copied
// list would drift exactly the way the mapping itself drifted.
func TestEveryIntentSentinelIsMapped(t *testing.T) {
	t.Parallel()

	mapped := make(map[string]bool, len(intentSentinelStatus))
	for err := range intentSentinelStatus {
		mapped[sentinelName(t, err)] = true
	}

	found := 0

	for _, name := range exportedSentinelNames(t, "../game") {
		found++

		if mapped[name] || handledElsewhere[name] != "" {
			continue
		}

		t.Errorf("game.%s is mapped nowhere: add it to handleIntent's mapping "+
			"(and intentSentinelStatus), or to handledElsewhere if SubmitIntent "+
			"can never return it. An unmapped intent sentinel becomes a 500 — see #133.", name)
	}

	// A scan that silently found nothing would pass forever.
	if found < len(intentSentinelStatus) {
		t.Fatalf("scanned internal/game and found %d exported sentinels, fewer than the %d "+
			"already mapped — the scan is broken, not the mapping", found, len(intentSentinelStatus))
	}
}

// sentinelName recovers a sentinel's variable name by matching its message
// against internal/game's source, so failures name game.ErrFoo rather than
// printing an opaque message.
func sentinelName(t *testing.T, target error) string {
	t.Helper()

	for name, msg := range sentinelMessages(t, "../game") {
		if msg == target.Error() {
			return name
		}
	}

	t.Fatalf("no exported sentinel in internal/game has message %q", target.Error())

	return ""
}

// exportedSentinelNames returns the names of every exported `ErrX =
// errors.New(...)` var declared in the package at dir.
func exportedSentinelNames(t *testing.T, dir string) []string {
	t.Helper()

	names := make([]string, 0, len(intentSentinelStatus))
	for name := range sentinelMessages(t, dir) {
		names = append(names, name)
	}

	return names
}

// sentinelMessages parses dir and returns exported-sentinel-name → message
// for every `ErrX = errors.New("...")` package-level var. Walks the .go files
// itself rather than using parser.ParseDir (deprecated in Go 1.25) — build
// tags don't matter here, since every sentinel is declared in an ordinary,
// unconditionally-built file.
func sentinelMessages(t *testing.T, dir string) map[string]string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	fset := token.NewFileSet()
	out := make(map[string]string)

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		for _, decl := range file.Decls {
			collectSentinels(decl, out)
		}
	}

	return out
}

// collectSentinels adds every exported `ErrX = errors.New("literal")` spec in
// decl to out. Split from sentinelMessages to keep the nesting readable.
func collectSentinels(decl ast.Decl, out map[string]string) {
	gen, ok := decl.(*ast.GenDecl)
	if !ok || gen.Tok != token.VAR {
		return
	}

	for _, spec := range gen.Specs {
		vs, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}

		for i, name := range vs.Names {
			if !name.IsExported() || i >= len(vs.Values) {
				continue
			}

			if msg, ok := errorsNewLiteral(vs.Values[i]); ok {
				out[name.Name] = msg
			}
		}
	}
}

// errorsNewLiteral reports the message of an `errors.New("literal")` call
// expression, or false for anything else.
func errorsNewLiteral(expr ast.Expr) (string, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok || len(call.Args) != 1 {
		return "", false
	}

	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "New" {
		return "", false
	}

	pkg, ok := sel.X.(*ast.Ident)
	if !ok || pkg.Name != "errors" {
		return "", false
	}

	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}

	return lit.Value[1 : len(lit.Value)-1], true // strip the quotes
}
