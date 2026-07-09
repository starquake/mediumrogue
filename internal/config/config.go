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

// errPollNotBelowInterval rejects a bubble poll that isn't strictly shorter
// than the turn interval — the poll drives the control loop, so a poll at or
// above the interval starves the world domain of prompt ticks.
var errPollNotBelowInterval = errors.New("BUBBLE_POLL must be shorter than TURN_INTERVAL")

// defaultHeartbeatInterval is the idle-stream comment-frame cadence; well
// under common proxy idle timeouts (60s) with margin.
const defaultHeartbeatInterval = 15 * time.Second

// Bubble-clock defaults. Patience matches the plan's ~60s AFK fallback (§5);
// the poll cadence is fine-grained enough that lock-in feels instant yet coarse
// enough not to spin.
const (
	defaultCombatPatience = 60 * time.Second
	defaultBubblePoll     = 100 * time.Millisecond
)

// defaultDisconnectGrace is how long a disconnected player's entity lingers
// before the world sweeps it — long enough to survive a brief reconnect, short
// enough that a rage-quit doesn't leave a ghost blocking a hex for a whole
// combat.
const defaultDisconnectGrace = 20 * time.Second

// World-generation defaults. A fixed seed regenerates the same world on every
// restart (so planned homes stay put and tests reproduce); radius 24 ≈ 1,801
// tiles — a roomy shared world for ~15 players.
const (
	defaultWorldSeed   = 0xC0FFEE
	defaultWorldRadius = 24
)

// ErrNonPositiveRadius rejects a world radius below 1 — a zero/negative world
// has no interior to spawn in.
var ErrNonPositiveRadius = errors.New("WORLD_RADIUS must be positive")

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
	// CombatPatience is the AFK fallback: how long a combat time bubble waits
	// for a straggler before resolving anyway, from COMBAT_PATIENCE.
	CombatPatience time.Duration
	// BubblePoll is the control-loop poll cadence — how often the world checks
	// for elapsed turns and lock-ins, from BUBBLE_POLL. Must be shorter than
	// TurnInterval.
	BubblePoll time.Duration
	// DisconnectGrace is how long a disconnected player's entity lingers before
	// the world sweeps it, from DISCONNECT_GRACE.
	DisconnectGrace time.Duration
	// WorldSeed seeds procedural map generation, from WORLD_SEED (accepts
	// 0x… hex). A fixed default gives the same world across restarts.
	WorldSeed uint64
	// WorldRadius is the generated world's hex radius, from WORLD_RADIUS.
	WorldRadius int
}

// Load reads configuration from the environment.
func Load() (*Config, error) {
	cfg := &Config{
		Addr:              envOr("LISTEN_ADDR", ":8080"),
		TurnInterval:      protocol.TurnSeconds * time.Second,
		HeartbeatInterval: defaultHeartbeatInterval,
		CombatPatience:    defaultCombatPatience,
		BubblePoll:        defaultBubblePoll,
		DisconnectGrace:   defaultDisconnectGrace,
		WorldSeed:         defaultWorldSeed,
		WorldRadius:       defaultWorldRadius,
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

	if err := overrideDuration(&cfg.CombatPatience, "COMBAT_PATIENCE"); err != nil {
		return nil, err
	}

	if err := overrideDuration(&cfg.BubblePoll, "BUBBLE_POLL"); err != nil {
		return nil, err
	}

	if err := overrideDuration(&cfg.DisconnectGrace, "DISCONNECT_GRACE"); err != nil {
		return nil, err
	}

	if err := overrideUint64(&cfg.WorldSeed, "WORLD_SEED"); err != nil {
		return nil, err
	}

	if err := overrideInt(&cfg.WorldRadius, "WORLD_RADIUS"); err != nil {
		return nil, err
	}

	if cfg.WorldRadius < 1 {
		return nil, fmt.Errorf("WORLD_RADIUS = %d: %w", cfg.WorldRadius, ErrNonPositiveRadius)
	}

	if cfg.BubblePoll >= cfg.TurnInterval {
		return nil, fmt.Errorf("BUBBLE_POLL = %s, TURN_INTERVAL = %s: %w",
			cfg.BubblePoll, cfg.TurnInterval, errPollNotBelowInterval)
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

func overrideUint64(dst *uint64, key string) error {
	v := os.Getenv(key)
	if v == "" {
		return nil
	}

	// base 0 → accept decimal and 0x… hex seeds.
	n, err := strconv.ParseUint(v, 0, 64)
	if err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}

	*dst = n

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
