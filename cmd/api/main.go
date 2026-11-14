package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jonxsong/Relay/internal/config"
	"github.com/jonxsong/Relay/internal/httpapi"
	"github.com/jonxsong/Relay/internal/store"
)

func main() {
	if err := run(); err != nil {
		slog.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	configureLogger(cfg.LogLevel)

	rootContext, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startupContext, cancelStartup := context.WithTimeout(rootContext, 10*time.Second)
	defer cancelStartup()

	database, err := store.Open(startupContext, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	developmentUser, err := database.EnsureDevelopmentUser(startupContext, cfg.DevUserEmail, "Relay Developer")
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpapi.NewRouter(httpapi.Dependencies{Readiness: database, Workflows: database, Runs: database, DevelopmentUser: developmentUser.ID, MaxRequestBytes: cfg.MaxRequestBytes, Environment: cfg.Environment, APIKey: cfg.APIKey}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		// Per-request middleware and streaming write deadlines bound writes.
		// A server-wide WriteTimeout would terminate long-lived SSE streams.
		WriteTimeout:   0,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}

	serverErrors := make(chan error, 1)
	go func() {
		slog.Info("API listening", "address", cfg.HTTPAddr, "environment", cfg.Environment)
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case <-rootContext.Done():
		shutdownContext, cancelShutdown := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancelShutdown()
		if err := server.Shutdown(shutdownContext); err != nil {
			return errors.Join(errors.New("graceful shutdown failed"), err)
		}
		return nil
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func configureLogger(level string) {
	var slogLevel slog.Level
	switch level {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slogLevel})))
}
