package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sysloglistener "github.com/timmersuk/logthing/internal/syslog"
)

type Config struct {
	HTTPAddr          string
	SyslogUDPAddr     string
	SyslogTCPAddr     string
	SyslogFormat      string
	TestEventNetwork  string
	TestEventTarget   string
	DataDir           string
	Username          string
	Password          string
	StateDir          string
	WANInterface      string
	WANDownAfter      time.Duration
	WANRecoveredAfter time.Duration
	Notifier          string
	DiscordWebhookURL string
	PublicURL         string
}

func FromEnv() (Config, error) {
	syslogUDPAddr := envDefault("LOGTHING_SYSLOG_UDP_ADDR", ":5514")
	syslogTCPAddr := envDefault("LOGTHING_SYSLOG_TCP_ADDR", ":5514")
	defaultTestNetwork, defaultTestTarget := sysloglistener.DefaultTestEventDestination(syslogUDPAddr, syslogTCPAddr)

	dataDir := envDefault("LOGTHING_DATA_DIR", "data/messages")
	stateDir := envDefault("LOGTHING_STATE_DIR", filepath.Join(filepath.Dir(dataDir), "state"))
	downAfter, err := durationDefault("LOGTHING_WAN_DOWN_AFTER", time.Minute)
	if err != nil {
		return Config{}, err
	}
	recoveredAfter, err := durationDefault("LOGTHING_WAN_RECOVERED_AFTER", time.Minute)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		HTTPAddr:          envDefault("LOGTHING_HTTP_ADDR", ":8080"),
		SyslogUDPAddr:     syslogUDPAddr,
		SyslogTCPAddr:     syslogTCPAddr,
		SyslogFormat:      envDefault("LOGTHING_SYSLOG_FORMAT", "automatic"),
		TestEventNetwork:  envDefault("LOGTHING_TEST_EVENT_NETWORK", defaultTestNetwork),
		TestEventTarget:   envDefault("LOGTHING_TEST_EVENT_TARGET", defaultTestTarget),
		DataDir:           dataDir,
		Username:          strings.TrimSpace(os.Getenv("LOGTHING_USERNAME")),
		Password:          os.Getenv("LOGTHING_PASSWORD"),
		StateDir:          stateDir,
		WANInterface:      envDefault("LOGTHING_WAN_INTERFACE", "wan"),
		WANDownAfter:      downAfter,
		WANRecoveredAfter: recoveredAfter,
		Notifier:          strings.ToLower(envDefault("LOGTHING_NOTIFIER", "none")),
		DiscordWebhookURL: strings.TrimSpace(os.Getenv("LOGTHING_DISCORD_WEBHOOK_URL")),
		PublicURL:         strings.TrimRight(strings.TrimSpace(os.Getenv("LOGTHING_PUBLIC_URL")), "/"),
	}

	if cfg.Username == "" {
		return Config{}, errors.New("LOGTHING_USERNAME is required")
	}
	if cfg.Password == "" {
		return Config{}, errors.New("LOGTHING_PASSWORD is required")
	}
	if cfg.SyslogUDPAddr == "" && cfg.SyslogTCPAddr == "" {
		return Config{}, errors.New("at least one of LOGTHING_SYSLOG_UDP_ADDR or LOGTHING_SYSLOG_TCP_ADDR must be set")
	}
	if cfg.StateDir == "" {
		return Config{}, errors.New("LOGTHING_STATE_DIR is required")
	}
	switch cfg.Notifier {
	case "none":
	case "discord":
		if cfg.DiscordWebhookURL == "" {
			return Config{}, errors.New("LOGTHING_DISCORD_WEBHOOK_URL is required when LOGTHING_NOTIFIER=discord")
		}
	default:
		return Config{}, fmt.Errorf("unsupported LOGTHING_NOTIFIER %q", cfg.Notifier)
	}

	return cfg, nil
}

func durationDefault(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", name)
	}
	return duration, nil
}

func envDefault(name, fallback string) string {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(value)
}
