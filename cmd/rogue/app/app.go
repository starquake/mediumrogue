// Package app owns process lifecycle: flag parsing, config, wiring, the HTTP
// listener, and graceful shutdown. main() stays a one-liner so the whole run
// path is testable, following topbanana's cmd/server/app pattern.
package app

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/starquake/mediumrogue/internal/chat"
	"github.com/starquake/mediumrogue/internal/config"
	"github.com/starquake/mediumrogue/internal/game"
	"github.com/starquake/mediumrogue/internal/hub"
	"github.com/starquake/mediumrogue/internal/server"
)

// shutdownGrace is how long in-flight requests (including open SSE streams,
// which close on context cancel) get to drain after SIGTERM/SIGINT.
const shutdownGrace = 5 * time.Second

// readHeaderTimeout bounds how long a client may take to send request
// headers; the anti-slowloris backstop.
const readHeaderTimeout = 10 * time.Second

// Exit codes: exitErr for runtime failures, exitUsage for bad flags.
const (
	exitOK    = 0
	exitErr   = 1
	exitUsage = 2
)

// Run is the whole program. args is os.Args[1:]; stderr receives logs.
// It returns the process exit code.
func Run(ctx context.Context, args []string, stderr io.Writer) int {
	logger := slog.New(slog.NewTextHandler(stderr, nil))

	flags := flag.NewFlagSet("rogue", flag.ContinueOnError)
	flags.SetOutput(stderr)

	check := flags.Bool("check", false, "validate config and wiring, then exit (smoke test)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}

	cfg, err := config.Load()
	if err != nil {
		logger.Error("load config", "err", err)

		return exitErr
	}

	ticks := hub.New()
	world := game.NewWorld(
		cfg.TurnInterval, cfg.CombatPatience, cfg.BubblePoll, cfg.DisconnectGrace,
		cfg.WorldSeed, cfg.WorldRadius, ticks,
	)
	world.SetLogger(logger)

	// A snapshot restore already brings back the persisted monster
	// population (a restart must not respawn a healed, repositioned
	// population mid-expedition) — only spawn a fresh one when persistence
	// is disabled or nothing was actually restored (first boot, missing
	// file, or a version/seed/radius mismatch).
	if !loadSnapshot(logger, cfg.SnapshotPath, world) {
		world.SpawnMonsters(cfg.MonsterCount)
	}

	chatBroker := chat.NewBroker()

	world.SetAnnounce(func(sender, text string) { chatBroker.Publish(sender, text) })

	handler := server.New(server.Deps{
		Logger:            logger,
		World:             world,
		Ticks:             ticks,
		Chat:              chatBroker,
		HeartbeatInterval: cfg.HeartbeatInterval,
		ChatMinInterval:   cfg.ChatMinInterval,
		JoinMinInterval:   cfg.JoinMinInterval,
		SSEMaxStreams:     cfg.SSEMaxStreams,
	})

	if *check {
		_, _ = fmt.Fprintln(stderr, "ok: config loaded, handler wired")

		return exitOK
	}

	if err := serve(ctx, logger, cfg, world, handler); err != nil {
		logger.Error("server", "err", err)

		return exitErr
	}

	return exitOK
}

// serve runs the clock and the HTTP listener until ctx is canceled (SIGINT/
// SIGTERM), then drains in-flight requests for up to shutdownGrace.
func serve(
	ctx context.Context,
	logger *slog.Logger,
	cfg *config.Config,
	world *game.World,
	handler http.Handler,
) error {
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go world.Run(ctx)

	saverDone := startSnapshotSaver(ctx, logger, cfg, world)

	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		// SSE streams are long-lived by design: no WriteTimeout. Instead,
		// BaseContext ties every request (and thus every open stream) to ctx,
		// so SIGTERM cancels them and Shutdown can drain promptly.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)

	go func() {
		logger.Info("listening", "addr", cfg.Addr, "turn_interval", cfg.TurnInterval.String())

		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down", "grace", shutdownGrace.String())

	// WithoutCancel: ctx is already canceled at this point (that is why we
	// are shutting down); the drain deadline must outlive it.
	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		// Open SSE streams only end when their request context dies, so hit
		// the deadline, close them hard, and report a clean exit anyway.
		if closeErr := httpServer.Close(); closeErr != nil {
			return fmt.Errorf("close after drained shutdown deadline: %w", closeErr)
		}
	}

	// One final write after the drain, so shutdown persists whatever state
	// in-flight requests left behind — not just the last periodic tick. Wait
	// for the periodic saver to exit first (ctx is already canceled, so it
	// returns promptly): a periodic save mid-flight at this instant would
	// otherwise rename its slightly-staler snapshot OVER this final one.
	if saverDone != nil {
		<-saverDone
		saveSnapshot(logger, cfg.SnapshotPath, world)
	}

	return nil
}

// startSnapshotSaver launches the periodic snapshot saver goroutine and
// returns a channel that closes when it exits, or nil when persistence is
// disabled. serve joins this channel before its final shutdown save, so an
// in-flight periodic marshal+rename can never land AFTER (and clobber) the
// shutdown snapshot with slightly staler state.
func startSnapshotSaver(
	ctx context.Context, logger *slog.Logger, cfg *config.Config, world *game.World,
) chan struct{} {
	if cfg.SnapshotPath == "" {
		return nil
	}

	done := make(chan struct{})

	go func() {
		defer close(done)

		runSnapshotSaver(ctx, logger, cfg.SnapshotPath, cfg.SnapshotInterval, world)
	}()

	return done
}

// loadSnapshot loads world's state from path if persistence is enabled
// (path != "") and a snapshot file exists there. It must be called before
// world.Run starts the control loop — restoring into a live world would race
// turn resolution. Reports whether a snapshot was actually applied, so the
// caller can skip the fresh-world SpawnMonsters call it would otherwise make
// (a restore already brings back the persisted monster population).
//
// Any failure — persistence disabled, no file yet (first boot), an unreadable
// file, or a version/seed/radius mismatch — logs and returns false: the
// caller continues with the fresh world already under construction. Never a
// migration, never a crash over a stale or foreign snapshot file.
func loadSnapshot(logger *slog.Logger, path string, world *game.World) bool {
	if path == "" {
		return false
	}

	//nolint:gosec // path is SNAPSHOT_PATH, an operator-supplied deploy-time config value, not user input.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logger.Info("snapshot: no existing file, starting fresh", "path", path)
		} else {
			logger.Error("snapshot: read", "path", path, "err", err)
		}

		return false
	}

	if err := world.RestoreState(data); err != nil {
		// NEVER destroy a rejected snapshot: left at the live path, the fresh
		// world's periodic saver would overwrite the only copy of everyone's
		// characters within one SNAPSHOT_INTERVAL — so a mere WORLD_SEED typo
		// in the deployment env would permanently erase the world. Move it
		// aside; recovering it is a manual `mv` back (plus fixing the config).
		rejected := fmt.Sprintf("%s.rejected-%d", path, time.Now().Unix())
		if mvErr := os.Rename(path, rejected); mvErr != nil {
			logger.Error("snapshot: REJECTED and could not be moved aside — the periodic saver may overwrite it",
				"path", path, "err", err, "renameErr", mvErr)
		} else {
			logger.Error("snapshot: REJECTED — starting fresh; original preserved",
				"path", path, "preserved", rejected, "err", err)
		}

		return false
	}

	logger.Info("snapshot: restored", "path", path)

	return true
}

// runSnapshotSaver writes world to path every interval until ctx is
// canceled, then returns — the periodic half of persistence; serve makes the
// final, post-drain write itself (see saveSnapshot's call site above). Run
// in a goroutine.
func runSnapshotSaver(
	ctx context.Context, logger *slog.Logger, path string, interval time.Duration, world *game.World,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			saveSnapshot(logger, path, world)
		case <-ctx.Done():
			return
		}
	}
}

// saveSnapshot marshals world and atomically writes it to path: a temp file
// in the same directory, then os.Rename over the final name, so a
// concurrent reader (or a crash mid-write) never observes a partial
// snapshot. Logs and returns on any error — a save failure (e.g. a full
// disk) must never crash the game loop.
func saveSnapshot(logger *slog.Logger, path string, world *game.World) {
	data, err := world.MarshalState()
	if err != nil {
		logger.Error("snapshot: marshal", "err", err)

		return
	}

	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, ".snapshot-*.tmp")
	if err != nil {
		logger.Error("snapshot: create temp file", "dir", dir, "err", err)

		return
	}

	tmpPath := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		logger.Error("snapshot: write", "path", tmpPath, "err", err)

		_ = tmp.Close()
		_ = os.Remove(tmpPath)

		return
	}

	// fsync before the rename: os.Rename is atomic against a process crash,
	// not against power loss — without the flush, the rename can land while
	// the data hasn't, leaving a garbage file at the live path and costing
	// the whole world.
	if err := tmp.Sync(); err != nil {
		logger.Error("snapshot: sync temp file", "path", tmpPath, "err", err)

		_ = tmp.Close()
		_ = os.Remove(tmpPath)

		return
	}

	if err := tmp.Close(); err != nil {
		logger.Error("snapshot: close temp file", "path", tmpPath, "err", err)
		_ = os.Remove(tmpPath)

		return
	}

	if err := os.Rename(tmpPath, path); err != nil {
		logger.Error("snapshot: rename", "from", tmpPath, "to", path, "err", err)
		_ = os.Remove(tmpPath)

		return
	}

	logger.Info("snapshot: saved", "path", path)
}
