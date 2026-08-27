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

	"concrete-specimen-chain-service/internal/calculation"
	"concrete-specimen-chain-service/internal/httpapi"
	"concrete-specimen-chain-service/internal/ingest"
	"concrete-specimen-chain-service/internal/storage"
)

func main() {
	configuration := loadConfig()
	startup, cancelStartup := context.WithTimeout(context.Background(), 15*time.Second)
	repository, err := storage.OpenSQLite(startup, configuration.SQLitePath, func() time.Time { return time.Now().UTC() })
	cancelStartup()
	if err != nil {
		slog.Error("sqlite startup failed", "error", err, "path", configuration.SQLitePath)
		os.Exit(1)
	}
	defer repository.Close()
	events := ingest.NewService(repository, calculation.ValidateEvent)
	server := &http.Server{
		Addr: configuration.ListenAddress, Handler: httpapi.New(events, repository),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		ticker := time.NewTicker(configuration.CheckpointInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				checkpointCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				_, checkpointErr := repository.CreateCheckpoint(checkpointCtx)
				cancel()
				if checkpointErr != nil {
					slog.Error("checkpoint failed", "error", checkpointErr)
				}
			}
		}
	}()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()

	slog.Info("concrete specimen service listening", "address", configuration.ListenAddress,
		"storage", "sqlite", "sqlite_path", configuration.SQLitePath,
		"checkpoint_interval", configuration.CheckpointInterval, "log_level", configuration.LogLevel)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}
