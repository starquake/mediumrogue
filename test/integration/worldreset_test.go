package integration_test

// worldreset_test.go: item 4, playtest feedback batch 3 — the world-reset
// signal. TurnEvent.WorldID lets a client tell a genuine world reset (a
// restart with no matching snapshot — a fresh WorldID) from an ordinary
// restore (the persisted WorldID survives, because a restored world IS the
// same world). Proven here over the same real-HTTP/snapshot-file path
// persistence_test.go uses for milestone 10a.

import (
	"bufio"
	"os"
	"path/filepath"
	"testing"
)

// TestFreshBootsMintDistinctWorldIDs: two independent servers with no
// snapshot involved (two separate `go run` boots, or a restart with
// SNAPSHOT_PATH unset) are different worlds — their first turn bundles
// carry different, non-empty WorldIDs.
func TestFreshBootsMintDistinctWorldIDs(t *testing.T) {
	t.Parallel()

	pwA := newPersistWorld(t)
	tsA := pwA.serve(t)
	readerA := bufio.NewReader(get(t, tsA, "/api/events").Body)
	firstA := decodeTurnFrame(t, readerA)

	pwB := newPersistWorld(t)
	tsB := pwB.serve(t)
	readerB := bufio.NewReader(get(t, tsB, "/api/events").Body)
	firstB := decodeTurnFrame(t, readerB)

	if firstA.WorldID == "" || firstB.WorldID == "" {
		t.Fatalf("empty WorldID: A=%q B=%q", firstA.WorldID, firstB.WorldID)
	}

	if firstA.WorldID == firstB.WorldID {
		t.Fatalf("two independently booted servers minted the same WorldID %q", firstA.WorldID)
	}
}

// TestRestoreKeepsSameWorldID: server A snapshots to a file; server B
// restores from it. Server B's WorldID must equal server A's — a restored
// world is the SAME world, not a reset, so a client that already saw A's
// WorldID must never treat reconnecting to (the restored) B as a reset.
//
//nolint:paralleltest // serial by design (#22, matches TestDropPickupLoop): tick loop must not be CPU-starved.
func TestRestoreKeepsSameWorldID(t *testing.T) {
	pwA := newPersistWorld(t)
	tsA := pwA.serve(t)
	readerA := bufio.NewReader(get(t, tsA, "/api/events").Body)
	firstA := decodeTurnFrame(t, readerA)

	if firstA.WorldID == "" {
		t.Fatal("server A's first bundle has an empty WorldID")
	}

	data, err := pwA.world.MarshalState()
	if err != nil {
		t.Fatalf("MarshalState: %v", err)
	}

	path := filepath.Join(t.TempDir(), "snapshot.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write snapshot file: %v", err)
	}

	//nolint:gosec // path is a t.TempDir() file this test wrote itself, not user input.
	loaded, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot file: %v", err)
	}

	pwB := newPersistWorld(t)
	if err := pwB.world.RestoreState(loaded); err != nil {
		t.Fatalf("RestoreState: %v", err)
	}

	tsB := pwB.serve(t)
	readerB := bufio.NewReader(get(t, tsB, "/api/events").Body)
	firstB := decodeTurnFrame(t, readerB)

	if got, want := firstB.WorldID, firstA.WorldID; got != want {
		t.Errorf("restored server's WorldID = %q, want %q (server A's, unchanged)", got, want)
	}
}
