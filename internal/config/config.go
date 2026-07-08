// Package config loads server configuration from the environment, with
// development-friendly defaults so `go run ./cmd/rogue` works with zero setup.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/starquake/mediumrogue/internal/protocol"
)

// errNonPositiveDuration rejects zero and negative duration overrides — a
// zero turn interval would spin the clock, a negative one panics the ticker.
var errNonPositiveDuration = errors.New("duration must be positive")

// errNegativeInt rejects negative int overrides — a negative monster count
// has no meaning.
var errNegativeInt = errors.New("value must not be negative")

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
	// MonsterCount is how many monsters to spawn at startup, from
	// MONSTER_COUNT. Defaults to 0 (no monsters) so existing deployments and
	// tests are unaffected until milestone 6.2 turns them on.
	MonsterCount int
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

	if err := overrideInt(&cfg.MonsterCount, "MONSTER_COUNT"); err != nil {
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

func overrideInt(dst *int, key string) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}

	if n < 0 {
		return fmt.Errorf("%s = %d: %w", key, n, errNegativeInt)
	}

	*dst = n

	return nil
}
