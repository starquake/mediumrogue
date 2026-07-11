package app

// export_test.go exposes the unexported snapshot load/save wiring
// (loadSnapshot, saveSnapshot, runSnapshotSaver) to app_test.go, mirroring
// internal/game/export_test.go's pattern: thin wrappers here, the actual
// TestXxx functions in the external app_test package.

import (
	"context"
	"log/slog"
	"time"

	"github.com/starquake/mediumrogue/internal/game"
)

// LoadSnapshotForTest exposes loadSnapshot.
func LoadSnapshotForTest(logger *slog.Logger, path string, world *game.World) bool {
	return loadSnapshot(logger, path, world)
}

// SaveSnapshotForTest exposes saveSnapshot.
func SaveSnapshotForTest(logger *slog.Logger, path string, world *game.World) {
	saveSnapshot(logger, path, world)
}

// RunSnapshotSaverForTest exposes runSnapshotSaver.
func RunSnapshotSaverForTest(
	ctx context.Context, logger *slog.Logger, path string, interval time.Duration, world *game.World,
) {
	runSnapshotSaver(ctx, logger, path, interval, world)
}
