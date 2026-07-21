package config_test

import (
	"errors"
	"slices"
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

	if got, want := cfg.TurnInterval, 4*time.Second; got != want {
		t.Errorf("TurnInterval = %s, want 4s", got)
	}

	if got, want := cfg.HeartbeatInterval, 15*time.Second; got != want {
		t.Errorf("HeartbeatInterval = %s, want 15s", got)
	}

	if got, want := cfg.MonsterCount, 0; got != want {
		t.Errorf("MonsterCount = %d, want 0", got)
	}

	if got, want := cfg.CombatPatience, 30*time.Second; got != want {
		t.Errorf("CombatPatience = %s, want 30s", got)
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

	if got, want := cfg.ChatMinInterval, time.Second; got != want {
		t.Errorf("ChatMinInterval = %s, want 1s", got)
	}

	if got, want := cfg.JoinMinInterval, time.Second; got != want {
		t.Errorf("JoinMinInterval = %s, want 1s", got)
	}

	if got, want := cfg.SSEMaxStreams, 256; got != want {
		t.Errorf("SSEMaxStreams = %d, want 256", got)
	}

	if got, want := cfg.TrustProxyIP, false; got != want {
		t.Errorf("TrustProxyIP = %v, want false", got)
	}

	// Per-IP cap ships disabled: fairness is harmful when players share one IP.
	if got, want := cfg.PerIPSSEStreams, 0; got != want {
		t.Errorf("PerIPSSEStreams = %d, want 0 (per-IP cap off by default)", got)
	}

	// No starter consumables by default — production and existing tests keep the
	// empty starting backpack (#271).
	if got := cfg.StarterConsumables; len(got) != 0 {
		t.Errorf("StarterConsumables = %v, want empty by default", got)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9999")
	t.Setenv("TURN_INTERVAL", "250ms")
	t.Setenv("HEARTBEAT_INTERVAL", "1s")
	t.Setenv("MONSTER_COUNT", "7")
	t.Setenv("COMBAT_PATIENCE", "45s")
	t.Setenv("BUBBLE_POLL", "50ms")
	t.Setenv("DISCONNECT_GRACE", "10s")
	t.Setenv("SNAPSHOT_PATH", "/tmp/rogue-snapshot.json")
	t.Setenv("SNAPSHOT_INTERVAL", "30s")
	t.Setenv("CHAT_MIN_INTERVAL", "2s")
	t.Setenv("JOIN_MIN_INTERVAL", "500ms")
	t.Setenv("SSE_MAX_STREAMS", "3")
	t.Setenv("TRUST_PROXY_IP", "true")
	t.Setenv("PER_IP_SSE_STREAMS", "9")
	t.Setenv("STARTER_CONSUMABLES", " flask-of-fire , scroll-of-recall ,")

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

	if got, want := cfg.CombatPatience, 45*time.Second; got != want {
		t.Errorf("CombatPatience = %s, want 45s", got)
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

	if got, want := cfg.ChatMinInterval, 2*time.Second; got != want {
		t.Errorf("ChatMinInterval = %s, want 2s", got)
	}

	if got, want := cfg.JoinMinInterval, 500*time.Millisecond; got != want {
		t.Errorf("JoinMinInterval = %s, want 500ms", got)
	}

	if got, want := cfg.SSEMaxStreams, 3; got != want {
		t.Errorf("SSEMaxStreams = %d, want 3", got)
	}

	if got, want := cfg.TrustProxyIP, true; got != want {
		t.Errorf("TrustProxyIP = %v, want true", got)
	}

	if got, want := cfg.PerIPSSEStreams, 9; got != want {
		t.Errorf("PerIPSSEStreams = %d, want 9", got)
	}

	// STARTER_CONSUMABLES parses to trimmed, non-empty ids (the trailing/empty
	// field and surrounding spaces are dropped) — #271.
	if got, want := cfg.StarterConsumables, []string{"flask-of-fire", "scroll-of-recall"}; !slices.Equal(got, want) {
		t.Errorf("StarterConsumables = %v, want %v", got, want)
	}
}

// TestLoadZeroDisablesLimits pins the limit knobs' off switch (#199): zero is
// a VALID setting meaning "no limit" — the convention tests and the e2e
// harness rely on — unlike the timing knobs, which reject zero.
func TestLoadZeroDisablesLimits(t *testing.T) {
	t.Setenv("CHAT_MIN_INTERVAL", "0s")
	t.Setenv("JOIN_MIN_INTERVAL", "0s")
	t.Setenv("SSE_MAX_STREAMS", "0")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got, want := cfg.ChatMinInterval, time.Duration(0); got != want {
		t.Errorf("ChatMinInterval = %s, want 0s", got)
	}

	if got, want := cfg.JoinMinInterval, time.Duration(0); got != want {
		t.Errorf("JoinMinInterval = %s, want 0s", got)
	}

	if got, want := cfg.SSEMaxStreams, 0; got != want {
		t.Errorf("SSEMaxStreams = %d, want 0", got)
	}
}

func TestLoadRejectsNegativeChatMinInterval(t *testing.T) {
	t.Setenv("CHAT_MIN_INTERVAL", "-1s")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a negative CHAT_MIN_INTERVAL")
	}
}

func TestLoadRejectsNegativeJoinMinInterval(t *testing.T) {
	t.Setenv("JOIN_MIN_INTERVAL", "-1s")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a negative JOIN_MIN_INTERVAL")
	}
}

func TestLoadRejectsNegativeSSEMaxStreams(t *testing.T) {
	t.Setenv("SSE_MAX_STREAMS", "-1")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a negative SSE_MAX_STREAMS")
	}
}

// TestLoadZeroDisablesPerIPSSEStreams: zero is a valid PER_IP_SSE_STREAMS
// meaning "no per-IP layer" (the global cap still applies) — the same off
// convention as the other limit knobs.
func TestLoadZeroDisablesPerIPSSEStreams(t *testing.T) {
	t.Setenv("PER_IP_SSE_STREAMS", "0")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if got, want := cfg.PerIPSSEStreams, 0; got != want {
		t.Errorf("PerIPSSEStreams = %d, want 0", got)
	}
}

func TestLoadRejectsNegativePerIPSSEStreams(t *testing.T) {
	t.Setenv("PER_IP_SSE_STREAMS", "-1")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a negative PER_IP_SSE_STREAMS")
	}
}

func TestLoadRejectsNonBoolTrustProxyIP(t *testing.T) {
	t.Setenv("TRUST_PROXY_IP", "maybe")

	if _, err := config.Load(); err == nil {
		t.Fatal("Load() accepted a non-bool TRUST_PROXY_IP")
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
