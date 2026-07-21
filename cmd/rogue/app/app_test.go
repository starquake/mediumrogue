// Package app_test pins the app-level snapshot load/save wiring in
// isolation via export_test.go's wrappers: default-off, atomic writes, and
// the periodic saver actually ticking. Full restart-survival over HTTP is
// covered by test/integration/persistence_test.go.
package app_test

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/cmd/rogue/app"
	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/protocol"
)

// newAppTestWorld builds a small world for the snapshot-plumbing tests —
// its exact seed/radius/timings do not matter, only that it is fresh.
func newAppTestWorld() *game.World {
	return newAppTestWorldRadius(8)
}

// newAppTestWorldRadius is newAppTestWorld with a caller-chosen radius, so
// the rejected-snapshot test can build a snapshot that genuinely mismatches
// the loading world's configuration.
func newAppTestWorldRadius(radius int) *game.World {
	return game.NewWorld(game.WorldConfig{
		Interval:        time.Second,
		CombatPatience:  time.Minute,
		BubblePoll:      5 * time.Millisecond,
		DisconnectGrace: time.Hour,
		WorldSeed:       0xC0FFEE,
		Radius:          radius,
		Ticks:           hub.New(),
	})
}

func TestLoadSnapshotDisabledIsNoOp(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	world := newAppTestWorld()

	if got, want := app.LoadSnapshotForTest(logger, "", world), false; got != want {
		t.Errorf("loadSnapshot(path=\"\") = %v, want %v", got, want)
	}
}

func TestLoadSnapshotMissingFileReturnsFalse(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	world := newAppTestWorld()
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	if got, want := app.LoadSnapshotForTest(logger, path, world), false; got != want {
		t.Errorf("loadSnapshot(missing file) = %v, want %v", got, want)
	}
}

// TestSaveSnapshotIsAtomicAndLoadable: saveSnapshot writes exactly the final
// file (no leftover .tmp), and a fresh, same-seed/radius world can load it
// back via loadSnapshot.
func TestSaveSnapshotIsAtomicAndLoadable(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	original := newAppTestWorld()
	if _, err := original.Join("", "tester", protocol.ClassFighter, protocol.SpeciesHuman); err != nil {
		t.Fatalf("Join: %v", err)
	}

	app.SaveSnapshotForTest(logger, path, original)

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	if got, want := len(entries), 1; got != want {
		t.Fatalf("files in snapshot dir after save = %d, want %d (no leftover temp file): %v", got, want, entries)
	}

	if got, want := entries[0].Name(), "snapshot.json"; got != want {
		t.Errorf("file in snapshot dir = %q, want %q", got, want)
	}

	restored := newAppTestWorld()
	if got, want := app.LoadSnapshotForTest(logger, path, restored), true; got != want {
		t.Fatalf("loadSnapshot after save = %v, want %v", got, want)
	}
}

// TestLoadSnapshotPreservesRejectedFile: a snapshot the world refuses
// (here: a radius mismatch — e.g. a WORLD_RADIUS/WORLD_SEED typo in the
// deployment env) must never be destroyed. loadSnapshot moves it aside to
// `<path>.rejected-<unix-ts>` with its bytes intact, so the fresh world's
// periodic saver can't overwrite the only copy of everyone's characters
// within one SNAPSHOT_INTERVAL; the live path is then free for fresh saves.
func TestLoadSnapshotPreservesRejectedFile(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	dir := t.TempDir()
	path := filepath.Join(dir, "snapshot.json")

	// A real snapshot, but from a differently-configured world (radius 9 vs
	// the loading world's 8) — RestoreState must reject it.
	other := newAppTestWorldRadius(9)
	app.SaveSnapshotForTest(logger, path, other)

	//nolint:gosec // path is a t.TempDir() file this test wrote itself, not user input.
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read mismatched snapshot fixture: %v", err)
	}

	world := newAppTestWorld()
	if got, want := app.LoadSnapshotForTest(logger, path, world), false; got != want {
		t.Fatalf("loadSnapshot(mismatched snapshot) = %v, want %v", got, want)
	}

	if _, err := os.Stat(path); err == nil {
		t.Errorf("rejected snapshot still at the live path %s, want moved aside", path)
	}

	entries, err := filepath.Glob(path + ".rejected-*")
	if err != nil {
		t.Fatalf("glob rejected files: %v", err)
	}

	if got, want := len(entries), 1; got != want {
		t.Fatalf("rejected-aside files = %d, want %d", got, want)
	}

	preserved, err := os.ReadFile(entries[0])
	if err != nil {
		t.Fatalf("read preserved rejected snapshot: %v", err)
	}

	if got, want := string(preserved), string(original); got != want {
		t.Errorf("preserved rejected snapshot bytes differ from the original")
	}

	// The live path is free again: a fresh save lands there without touching
	// the preserved copy.
	app.SaveSnapshotForTest(logger, path, world)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("fresh save did not land at the live path after rejection: %v", err)
	}

	if got, want := app.LoadSnapshotForTest(logger, path, newAppTestWorld()), true; got != want {
		t.Errorf("loadSnapshot(fresh save after rejection) = %v, want %v", got, want)
	}
}

// TestSaveSnapshotErrorsLogAndContinue: a save to a directory that does not
// exist fails to write to disk but must not panic — save errors log and
// continue, never crash the game loop.
func TestSaveSnapshotErrorsLogAndContinue(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	world := newAppTestWorld()
	path := filepath.Join(t.TempDir(), "no-such-dir", "snapshot.json")

	app.SaveSnapshotForTest(logger, path, world) // must not panic

	if _, err := os.Stat(path); err == nil {
		t.Errorf("snapshot file exists at %s despite an unwritable directory", path)
	}
}

// TestRunSnapshotSaverTicksThenStops: the periodic saver writes at least once
// within a couple of intervals and exits cleanly when ctx is canceled.
func TestRunSnapshotSaverTicksThenStops(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.DiscardHandler)
	path := filepath.Join(t.TempDir(), "snapshot.json")
	world := newAppTestWorld()

	ctx, cancel := context.WithCancel(t.Context())

	const interval = 5 * time.Millisecond

	done := make(chan struct{})

	go func() {
		app.RunSnapshotSaverForTest(ctx, logger, path, interval, world)
		close(done)
	}()

	deadline := time.Now().Add(2 * time.Second)

	for {
		if _, err := os.Stat(path); err == nil {
			break
		}

		if time.Now().After(deadline) {
			cancel()
			t.Fatalf("runSnapshotSaver did not write %s within the deadline", path)
		}

		time.Sleep(interval)
	}

	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runSnapshotSaver did not return after ctx cancel")
	}
}
