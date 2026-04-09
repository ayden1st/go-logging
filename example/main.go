package main

import (
	"os"

	logging "github.com/ayden1st/go-logging"
)

func main() {
	// initialize logging
	cfg := logging.DefaultConfig()
	cfg.Level = logging.ParseLevel("debug")
	cfg.Service = "myapp"
	cfg.Env = "dev"
	cfg.Version = "v0.1.0"
	cfg.JSON = true
	cfg.Output = os.Stdout

	logging.Init(cfg)
	defer logging.Get().Close()

	logg := logging.Get()

	logg.Info("Service started", "pid", os.Getpid())
	logg.Debug("This is a debug message")

	// Context
	reqLog := logg.With("request_id", "abc123", "method", "GET")
	reqLog.Info("Handling request")

	// Group
	dbLog := logg.WithGroup("database").With("table", "users")
	dbLog.Warn("Query took too long", "duration_ms", 150)

	// Error
	dbLog.Error("Failed to insert", "err", "duplicate key")

	// Fatal
	// logg.Fatal("Critical failure")
}
