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
	WorkerLeaseDuration time.Duration
	WorkerMaxRetries    int
	Provider            string
	OpenAIAPIKey        string
	DefaultModel        string
	MaxOutputTokens     int64
	ModelTimeout        time.Duration
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
	workerLeaseDuration, err := durationEnv("RELAY_WORKER_LEASE", 45*time.Second)
	if err != nil {
		return Config{}, err
	}
	workerMaxRetries, err := int64Env("RELAY_MAX_RETRIES", 3)
	if err != nil {
		return Config{}, err
	}
	maxOutputTokens, err := int64Env("RELAY_MAX_OUTPUT_TOKENS", 2048)
	if err != nil {
		return Config{}, err
	}
	modelTimeout, err := durationEnv("RELAY_MODEL_TIMEOUT", 90*time.Second)
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
		WorkerLeaseDuration: workerLeaseDuration,
		WorkerMaxRetries:    int(workerMaxRetries),
		Provider:            stringEnv("RELAY_PROVIDER", "mock"),
		DefaultModel:        stringEnv("RELAY_DEFAULT_MODEL", "gpt-5-mini"),
		MaxOutputTokens:     maxOutputTokens,
		ModelTimeout:        modelTimeout,
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
	if cfg.WorkerLeaseDuration < 15*time.Second || cfg.WorkerLeaseDuration > 10*time.Minute {
		return Config{}, errors.New("RELAY_WORKER_LEASE must be between 15s and 10m")
	}
	if cfg.WorkerMaxRetries < 0 || cfg.WorkerMaxRetries > 100 {
		return Config{}, errors.New("RELAY_MAX_RETRIES must be between 0 and 100")
	}
	if cfg.Provider != "mock" && cfg.Provider != "openai" {
		return Config{}, errors.New("RELAY_PROVIDER must be mock or openai")
	}
	if cfg.MaxOutputTokens < 1 || cfg.MaxOutputTokens > 100000 {
		return Config{}, errors.New("RELAY_MAX_OUTPUT_TOKENS must be between 1 and 100000")
	}
	if cfg.ModelTimeout < time.Second || cfg.ModelTimeout > 10*time.Minute {
		return Config{}, errors.New("RELAY_MODEL_TIMEOUT must be between 1s and 10m")
	}
	if cfg.Provider == "openai" {
		apiKey, err := loadOpenAIAPIKey(cfg.Environment)
		if err != nil {
			return Config{}, err
		}
		cfg.OpenAIAPIKey = apiKey
	}
	if cfg.Environment != "development" {
		return Config{}, errors.New("only development authentication is implemented; refusing to start a non-development environment")
	}
	return cfg, nil
}

func loadOpenAIAPIKey(environment string) (string, error) {
	if key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY")); key != "" {
		return key, nil
	}
	path := strings.TrimSpace(os.Getenv("OPENAI_API_KEY_FILE"))
	if path == "" && environment == "development" {
		path = "OPENAI_API_KEY.txt"
	}
	if path == "" {
		return "", errors.New("OPENAI_API_KEY or OPENAI_API_KEY_FILE is required for the OpenAI provider")
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read OpenAI API key file: %w", err)
	}
	key := strings.TrimSpace(string(contents))
	if key == "" || strings.ContainsAny(key, "\r\n") {
		return "", errors.New("OpenAI API key file must contain exactly one non-empty line")
	}
	return key, nil
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
