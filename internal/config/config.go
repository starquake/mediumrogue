// Package config loads server configuration from the environment, with
// development-friendly defaults so `go run ./cmd/rogue` works with zero setup.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/starquake/medium-rogue/internal/protocol"
)

// errNonPositiveDuration rejects zero and negative duration overrides — a
// zero turn interval would spin the clock, a negative one panics the ticker.
var errNonPositiveDuration = errors.New("duration must be positive")

// defaultHeartbeatInterval is the idle-stream comment-frame cadence; well
// under common proxy idle timeouts (60s) with margin.
const defaultHeartbeatInterval = 15 * time.Second

// Config is the fully resolved server configuration.
type Config struct {
	// Addr is the listen address, from LISTEN_ADDR.
	Addr string
	// TurnInterval is the world-turn period, from TURN_INTERVAL. Production
	// uses the protocol default; tests and the e2e suite shrink it so a
	// browser test observes several turns in milliseconds.
	TurnInterval time.Duration
	// HeartbeatInterval is how often the SSE handler emits a comment frame on
	// an otherwise idle stream, from HEARTBEAT_INTERVAL. Keeps proxies from
	// reaping the connection; shrunk in tests.
	HeartbeatInterval time.Duration
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:              envOr("LISTEN_ADDR", ":8080"),
		TurnInterval:      protocol.TurnSeconds * time.Second,
		HeartbeatInterval: defaultHeartbeatInterval,
	}

	if err := overrideDuration(&cfg.TurnInterval, "TURN_INTERVAL"); err != nil {
		return nil, err
	}

	if err := overrideDuration(&cfg.HeartbeatInterval, "HEARTBEAT_INTERVAL"); err != nil {
		return nil, err
	}

	return cfg, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}

func overrideDuration(dst *time.Duration, key string) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}

	d, err := time.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}

	if d <= 0 {
		return fmt.Errorf("%s = %s: %w", key, d, errNonPositiveDuration)
	}

	*dst = d

	return nil
}
