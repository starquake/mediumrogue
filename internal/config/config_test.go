package config_test

import (
	"errors"
	"testing"
	"time"

	"github.com/starquake/mediumrogue/internal/config"
)

//nolint:paralleltest // sibling tests mutate the process env via t.Setenv; parallel would race them.
func TestLoadDefaults(t *testing.T) {
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got, want := cfg.Addr, ":8080"; got != want {
		t.Errorf("Addr = %q, want :8080", got)
	}

	if got, want := cfg.TurnInterval, 5*time.Second; got != want {
		t.Errorf("TurnInterval = %s, want 5s", got)
	}

	if got, want := cfg.HeartbeatInterval, 15*time.Second; got != want {
		t.Errorf("HeartbeatInterval = %s, want 15s", got)
	}

	if got, want := cfg.MonsterCount, 0; got != want {
		t.Errorf("MonsterCount = %d, want 0", got)
	}

	if got, want := cfg.CombatPatience, 60*time.Second; got != want {
		t.Errorf("CombatPatience = %s, want 60s", got)
	}

	if got, want := cfg.BubblePoll, 100*time.Millisecond; got != want {
		t.Errorf("BubblePoll = %s, want 100ms", got)
	}

	if got, want := cfg.DisconnectGrace, 20*time.Second; got != want {
		t.Errorf("DisconnectGrace = %s, want 20s", got)
	}

	if got, want := cfg.SnapshotPath, ""; got != want {
		t.Errorf("SnapshotPath = %q, want %q (disabled by default)", got, want)
	}

	if got, want := cfg.SnapshotInterval, 60*time.Second; got != want {
		t.Errorf("SnapshotInterval = %s, want 60s", got)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9999")
	t.Setenv("TURN_INTERVAL", "250ms")
	t.Setenv("HEARTBEAT_INTERVAL", "1s")
	t.Setenv("MONSTER_COUNT", "7")
	t.Setenv("COMBAT_PATIENCE", "30s")
	t.Setenv("BUBBLE_POLL", "50ms")
	t.Setenv("DISCONNECT_GRACE", "10s")
	t.Setenv("SNAPSHOT_PATH", "/tmp/rogue-snapshot.json")
	t.Setenv("SNAPSHOT_INTERVAL", "30s")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got, want := cfg.Addr, ":9999"; got != want {
		t.Errorf("Addr = %q, want :9999", got)
	}

	if got, want := cfg.TurnInterval, 250*time.Millisecond; got != want {
		t.Errorf("TurnInterval = %s, want 250ms", got)
	}

	if got, want := cfg.HeartbeatInterval, time.Second; got != want {
		t.Errorf("HeartbeatInterval = %s, want 1s", got)
	}

	if got, want := cfg.MonsterCount, 7; got != want {
		t.Errorf("MonsterCount = %d, want 7", got)
	}

	if got, want := cfg.CombatPatience, 30*time.Second; got != want {
		t.Errorf("CombatPatience = %s, want 30s", got)
	}

	if got, want := cfg.BubblePoll, 50*time.Millisecond; got != want {
		t.Errorf("BubblePoll = %s, want 50ms", got)
	}

	if got, want := cfg.DisconnectGrace, 10*time.Second; got != want {
		t.Errorf("DisconnectGrace = %s, want 10s", got)
	}

	if got, want := cfg.SnapshotPath, "/tmp/rogue-snapshot.json"; got != want {
		t.Errorf("SnapshotPath = %q, want %q", got, want)
	}

	if got, want := cfg.SnapshotInterval, 30*time.Second; got != want {
		t.Errorf("SnapshotInterval = %s, want 30s", got)
	}
}

func TestLoadRejectsNonPositiveSnapshotInterval(t *testing.T) {
	t.Setenv("SNAPSHOT_INTERVAL", "0s")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a zero SNAPSHOT_INTERVAL")
	}
}

func TestLoadRejectsBadDuration(t *testing.T) {
	t.Setenv("TURN_INTERVAL", "not-a-duration")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted an invalid TURN_INTERVAL")
	}
}

func TestLoadRejectsNonPositiveDuration(t *testing.T) {
	t.Setenv("TURN_INTERVAL", "-5s")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a negative TURN_INTERVAL")
	}
}

func TestLoadRejectsNegativeMonsterCount(t *testing.T) {
	t.Setenv("MONSTER_COUNT", "-1")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a negative MONSTER_COUNT")
	}
}

func TestLoadRejectsNonNumericMonsterCount(t *testing.T) {
	t.Setenv("MONSTER_COUNT", "not-a-number")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a non-numeric MONSTER_COUNT")
	}
}

func TestLoadRejectsNonPositiveCombatPatience(t *testing.T) {
	t.Setenv("COMBAT_PATIENCE", "0s")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a zero COMBAT_PATIENCE")
	}
}

func TestLoadRejectsNonPositiveBubblePoll(t *testing.T) {
	t.Setenv("BUBBLE_POLL", "-1ms")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a negative BUBBLE_POLL")
	}
}

func TestLoadRejectsNonPositiveDisconnectGrace(t *testing.T) {
	t.Setenv("DISCONNECT_GRACE", "0s")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a zero DISCONNECT_GRACE")
	}
}

func TestLoadRejectsBubblePollNotBelowInterval(t *testing.T) {
	t.Setenv("TURN_INTERVAL", "100ms")
	t.Setenv("BUBBLE_POLL", "100ms")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted BUBBLE_POLL equal to TURN_INTERVAL")
	}
}

func TestLoadWorldDefaults(t *testing.T) {
	t.Setenv("WORLD_SEED", "")
	t.Setenv("WORLD_RADIUS", "")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.WorldSeed, uint64(0xC0FFEE); got != want {
		t.Errorf("WorldSeed = %#x, want %#x", got, want)
	}

	if got, want := cfg.WorldRadius, 24; got != want {
		t.Errorf("WorldRadius = %d, want %d", got, want)
	}
}

func TestLoadWorldOverrides(t *testing.T) {
	t.Setenv("WORLD_SEED", "0x2A")
	t.Setenv("WORLD_RADIUS", "10")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got, want := cfg.WorldSeed, uint64(42); got != want {
		t.Errorf("WorldSeed = %d, want %d", got, want)
	}

	if got, want := cfg.WorldRadius, 10; got != want {
		t.Errorf("WorldRadius = %d, want %d", got, want)
	}
}

func TestLoadRejectsNonPositiveRadius(t *testing.T) {
	t.Setenv("WORLD_RADIUS", "0")

	_, err := config.Load()
	if got, want := err, config.ErrNonPositiveRadius; !errors.Is(got, want) {
		t.Errorf("err = %v, want %v", got, want)
	}
}
