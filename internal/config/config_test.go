package config_test

import (
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
}

func TestLoadOverrides(t *testing.T) {
	t.Setenv("LISTEN_ADDR", ":9999")
	t.Setenv("TURN_INTERVAL", "250ms")
	t.Setenv("HEARTBEAT_INTERVAL", "1s")

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
