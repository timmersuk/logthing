package config

import (
	"errors"
	"os"
	"strings"

	sysloglistener "github.com/timmersuk/logthing/internal/syslog"
)

type Config struct {
	HTTPAddr         string
	SyslogUDPAddr    string
	SyslogTCPAddr    string
	SyslogFormat     string
	TestEventNetwork string
	TestEventTarget  string
	DataDir          string
	Username         string
	Password         string
}

func FromEnv() (Config, error) {
	syslogUDPAddr := envDefault("LOGTHING_SYSLOG_UDP_ADDR", ":5514")
	syslogTCPAddr := envDefault("LOGTHING_SYSLOG_TCP_ADDR", ":5514")
	defaultTestNetwork, defaultTestTarget := sysloglistener.DefaultTestEventDestination(syslogUDPAddr, syslogTCPAddr)

	cfg := Config{
		HTTPAddr:         envDefault("LOGTHING_HTTP_ADDR", ":8080"),
		SyslogUDPAddr:    syslogUDPAddr,
		SyslogTCPAddr:    syslogTCPAddr,
		SyslogFormat:     envDefault("LOGTHING_SYSLOG_FORMAT", "automatic"),
		TestEventNetwork: envDefault("LOGTHING_TEST_EVENT_NETWORK", defaultTestNetwork),
		TestEventTarget:  envDefault("LOGTHING_TEST_EVENT_TARGET", defaultTestTarget),
		DataDir:          envDefault("LOGTHING_DATA_DIR", "data/messages"),
		Username:         strings.TrimSpace(os.Getenv("LOGTHING_USERNAME")),
		Password:         os.Getenv("LOGTHING_PASSWORD"),
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

	return cfg, nil
}

func envDefault(name, fallback string) string {
	value, ok := os.LookupEnv(name)
	if !ok {
		return fallback
	}
	return strings.TrimSpace(value)
}
