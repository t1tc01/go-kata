package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	// Setup structured logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	fmt.Println("=== Kata 03: The Graceful Shutdown Server ===")
	fmt.Println()

	// Create server configuration
	config := Config{
		Port:            "8080",
		WorkerPoolSize:  5,
		RequestTimeout:  10 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		Logger:          logger,
	}

	// Create server instance
	server := NewServer(config)

	// Create root context for server lifecycle
	rootCtx := context.Background()

	// Start the server
	if err := server.Start(rootCtx); err != nil {
		logger.Error("failed to start server", "error", err)
		os.Exit(1)
	}

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	sig := <-sigCh
	logger.Info("received shutdown signal", "signal", sig.String())

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()

	// Stop the server gracefully
	if err := server.Stop(shutdownCtx); err != nil {
		logger.Error("shutdown error", "error", err)
		os.Exit(1)
	}

	logger.Info("server stopped successfully")
}






