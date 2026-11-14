package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jonxsong/Relay/internal/config"
	"github.com/jonxsong/Relay/internal/dispatcher"
	"github.com/jonxsong/Relay/internal/store"
	"github.com/jonxsong/Relay/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
)

func main() {
	if err := run(); err != nil && !errors.Is(err, context.Canceled) {
		slog.Error("control plane stopped", "error", err)
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
	shutdownTracing, err := telemetry.ConfigureTracing(ctx, "relay-controlplane", cfg.Environment)
	if err != nil {
		return err
	}
	defer func() {
		shutdownContext, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if shutdownErr := shutdownTracing(shutdownContext); shutdownErr != nil {
			slog.Warn("flush traces", "error", shutdownErr)
		}
	}()
	startupContext, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStartup()

	database, err := store.Open(startupContext, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	broker, err := dispatcher.Open(startupContext, cfg.NATSURL, "relay-controlplane")
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := broker.Close(); closeErr != nil {
			slog.Warn("close NATS", "error", closeErr)
		}
	}()

	ticker := time.NewTicker(cfg.ControlPollInterval)
	defer ticker.Stop()
	slog.Info("control plane started", "poll_interval", cfg.ControlPollInterval)
	for {
		if err := runPass(ctx, database, broker, cfg.GlobalConcurrency, cfg.RunConcurrency, cfg.WorkerMaxRetries); err != nil {
			slog.Error("control plane pass failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func runPass(ctx context.Context, database *store.Store, broker *dispatcher.NATS, globalConcurrency, runConcurrency int32, maxRetries int) error {
	ctx, span := otel.Tracer("github.com/jonxsong/Relay/controlplane").Start(ctx, "controlplane.pass")
	defer span.End()
	recovered, err := database.RecoverExpiredAttempts(ctx, 100, maxRetries)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "recover expired attempts")
		return err
	}
	span.SetAttributes(attribute.Int("relay.attempts.recovered", recovered))
	if recovered > 0 {
		slog.Info("expired attempts recovered", "count", recovered)
	}
	for scheduledCount := 0; scheduledCount < 32; scheduledCount++ {
		scheduled, found, err := database.ScheduleOne(ctx, globalConcurrency, runConcurrency)
		if err != nil {
			return err
		}
		if !found {
			break
		}
		slog.Info("node attempt scheduled", "run_id", scheduled.RunID, "node_key", scheduled.NodeKey, "attempt_id", scheduled.AttemptID)
	}
	published, err := database.DispatchOutbox(ctx, 100, broker.Publish)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "dispatch outbox")
		return err
	}
	span.SetAttributes(attribute.Int("relay.outbox.published", published))
	if published > 0 {
		slog.Info("outbox dispatched", "published", published)
	}
	return nil
}
