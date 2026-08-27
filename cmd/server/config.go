package main

import (
	"os"
	"strconv"
	"time"
)

type config struct {
	ListenAddress      string
	SQLitePath         string
	CheckpointInterval time.Duration
	LogLevel           string
}

func loadConfig() config {
	seconds, err := strconv.Atoi(environment("CHECKPOINT_INTERVAL_SECONDS", "60"))
	if err != nil || seconds <= 0 {
		seconds = 60
	}
	return config{
		ListenAddress:      environment("LISTEN_ADDRESS", ":8080"),
		SQLitePath:         environment("SQLITE_PATH", "concrete.db"),
		CheckpointInterval: time.Duration(seconds) * time.Second,
		LogLevel:           environment("LOG_LEVEL", "info"),
	}
}

func environment(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
