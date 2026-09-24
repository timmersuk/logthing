package config

import (
	"path/filepath"
	"testing"
	"time"
)

func TestIncidentDefaults(t *testing.T) {
	t.Setenv("LOGTHING_USERNAME", "admin")
	t.Setenv("LOGTHING_PASSWORD", "secret")
	t.Setenv("LOGTHING_DATA_DIR", filepath.Join("custom", "messages"))
	cfg, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WANDownAfter != time.Minute || cfg.WANRecoveredAfter != time.Minute {
		t.Fatalf("durations = %v/%v", cfg.WANDownAfter, cfg.WANRecoveredAfter)
	}
	if cfg.StateDir != filepath.Join("custom", "state") || cfg.WANInterface != "wan" || cfg.Notifier != "none" {
		t.Fatalf("incident defaults = %#v", cfg)
	}
}

func TestDiscordRequiresWebhook(t *testing.T) {
	t.Setenv("LOGTHING_USERNAME", "admin")
	t.Setenv("LOGTHING_PASSWORD", "secret")
	t.Setenv("LOGTHING_NOTIFIER", "discord")
	t.Setenv("LOGTHING_DISCORD_WEBHOOK_URL", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected missing webhook error")
	}
}

func TestWANRecoveryDurationMustBePositive(t *testing.T) {
	t.Setenv("LOGTHING_USERNAME", "admin")
	t.Setenv("LOGTHING_PASSWORD", "secret")
	t.Setenv("LOGTHING_WAN_RECOVERED_AFTER", "0s")
	if _, err := FromEnv(); err == nil {
		t.Fatal("expected invalid duration error")
	}
}
