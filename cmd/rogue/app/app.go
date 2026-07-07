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
	"os/signal"
	"syscall"
	"time"

	"github.com/starquake/medium-rogue/internal/config"
	"github.com/starquake/medium-rogue/internal/game"
	"github.com/starquake/medium-rogue/internal/hub"
	"github.com/starquake/medium-rogue/internal/server"
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
	world := game.NewWorld(cfg.TurnInterval, ticks)
	handler := server.New(server.Deps{
		Logger:            logger,
		World:             world,
		Ticks:             ticks,
		HeartbeatInterval: cfg.HeartbeatInterval,
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

	return nil
}
