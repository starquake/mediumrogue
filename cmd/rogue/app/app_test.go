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
	return game.NewWorld(time.Second, time.Minute, 5*time.Millisecond, time.Hour, 0xC0FFEE, 8, hub.New())
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
