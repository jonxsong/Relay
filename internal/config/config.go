package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Environment         string
	HTTPAddr            string
	DatabaseURL         string
	NATSURL             string
	LogLevel            string
	DevUserEmail        string
	MaxRequestBytes     int64
	ShutdownTimeout     time.Duration
	GlobalConcurrency   int32
	RunConcurrency      int32
	ControlPollInterval time.Duration
}

func Load() (Config, error) {
	maxRequestBytes, err := int64Env("RELAY_MAX_REQUEST_BYTES", 1<<20)
	if err != nil {
		return Config{}, err
	}
	shutdownTimeout, err := durationEnv("RELAY_SHUTDOWN_TIMEOUT", 15*time.Second)
	if err != nil {
		return Config{}, err
	}
	globalConcurrency, err := int64Env("RELAY_GLOBAL_CONCURRENCY", 100)
	if err != nil {
		return Config{}, err
	}
	runConcurrency, err := int64Env("RELAY_RUN_CONCURRENCY", 10)
	if err != nil {
		return Config{}, err
	}
	controlPollInterval, err := durationEnv("RELAY_CONTROL_POLL_INTERVAL", 250*time.Millisecond)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Environment:         stringEnv("RELAY_ENV", "development"),
		HTTPAddr:            stringEnv("RELAY_HTTP_ADDR", ":8080"),
		DatabaseURL:         strings.TrimSpace(os.Getenv("DATABASE_URL")),
		NATSURL:             stringEnv("NATS_URL", "nats://localhost:4222"),
		LogLevel:            stringEnv("RELAY_LOG_LEVEL", "info"),
		DevUserEmail:        stringEnv("RELAY_DEV_USER_EMAIL", "developer@relay.local"),
		MaxRequestBytes:     maxRequestBytes,
		ShutdownTimeout:     shutdownTimeout,
		GlobalConcurrency:   int32(globalConcurrency),
		RunConcurrency:      int32(runConcurrency),
		ControlPollInterval: controlPollInterval,
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.MaxRequestBytes < 1024 || cfg.MaxRequestBytes > 16<<20 {
		return Config{}, errors.New("RELAY_MAX_REQUEST_BYTES must be between 1024 and 16777216")
	}
	if cfg.ShutdownTimeout <= 0 || cfg.ShutdownTimeout > time.Minute {
		return Config{}, errors.New("RELAY_SHUTDOWN_TIMEOUT must be positive and at most 1m")
	}
	if cfg.GlobalConcurrency < 1 || cfg.GlobalConcurrency > 10000 {
		return Config{}, errors.New("RELAY_GLOBAL_CONCURRENCY must be between 1 and 10000")
	}
	if cfg.RunConcurrency < 1 || cfg.RunConcurrency > cfg.GlobalConcurrency {
		return Config{}, errors.New("RELAY_RUN_CONCURRENCY must be between 1 and RELAY_GLOBAL_CONCURRENCY")
	}
	if cfg.ControlPollInterval < 10*time.Millisecond || cfg.ControlPollInterval > 10*time.Second {
		return Config{}, errors.New("RELAY_CONTROL_POLL_INTERVAL must be between 10ms and 10s")
	}
	if cfg.Environment != "development" {
		return Config{}, errors.New("only development authentication is implemented; refusing to start a non-development environment")
	}
	return cfg, nil
}

func stringEnv(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func int64Env(name string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be a duration: %w", name, err)
	}
	return parsed, nil
}
