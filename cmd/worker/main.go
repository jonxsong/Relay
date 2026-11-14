package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jonxsong/Relay/internal/ai"
	"github.com/jonxsong/Relay/internal/config"
	"github.com/jonxsong/Relay/internal/dispatcher"
	relaymcp "github.com/jonxsong/Relay/internal/mcp"
	"github.com/jonxsong/Relay/internal/store"
	"github.com/jonxsong/Relay/internal/worker"
)

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("worker stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupContext, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStartup()

	database, err := store.Open(startupContext, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown-host"
	}
	workerID := fmt.Sprintf("%s-%d", hostname, os.Getpid())
	localExecutor := worker.DeterministicMock{}
	var llmExecutor worker.Executor = localExecutor
	if cfg.Provider == "openai" {
		openAIExecutor, openAIErr := ai.NewOpenAI(cfg.OpenAIAPIKey, cfg.DefaultModel, cfg.MaxOutputTokens, cfg.ModelTimeout)
		if openAIErr != nil {
			return openAIErr
		}
		llmExecutor = openAIExecutor
	}
	var toolExecutor worker.Executor
	if cfg.MCPConfigFile != "" {
		connections, configErr := relaymcp.LoadConfig(cfg.MCPConfigFile)
		if configErr != nil {
			return configErr
		}
		configuredExecutor, configErr := relaymcp.NewExecutor(connections, nil)
		if configErr != nil {
			return configErr
		}
		toolExecutor = configuredExecutor
	}
	executor := worker.Router{LLM: llmExecutor, Local: localExecutor, Tool: toolExecutor}
	processor, err := worker.New(database, executor, workerID, cfg.WorkerLeaseDuration, cfg.WorkerMaxRetries)
	if err != nil {
		return err
	}
	broker, err := dispatcher.Open(startupContext, cfg.NATSURL, "relay-worker-"+workerID)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := broker.Close(); closeErr != nil {
			slog.Warn("close NATS", "error", closeErr)
		}
	}()
	slog.Info("worker started", "worker_id", workerID, "provider", cfg.Provider)
	return worker.Consume(ctx, broker, processor)
}
