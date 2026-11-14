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
	Environment     string
	HTTPAddr        string
	DatabaseURL     string
	LogLevel        string
	DevUserEmail    string
	MaxRequestBytes int64
	ShutdownTimeout time.Duration
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

	cfg := Config{
		Environment:     stringEnv("RELAY_ENV", "development"),
		HTTPAddr:        stringEnv("RELAY_HTTP_ADDR", ":8080"),
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		LogLevel:        stringEnv("RELAY_LOG_LEVEL", "info"),
		DevUserEmail:    stringEnv("RELAY_DEV_USER_EMAIL", "developer@relay.local"),
		MaxRequestBytes: maxRequestBytes,
		ShutdownTimeout: shutdownTimeout,
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
