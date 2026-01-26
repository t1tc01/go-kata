package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestServer_GracefulShutdown(t *testing.T) {
	// Setup logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError, // Reduce noise in tests
	}))

	config := Config{
		Port:            "8081",
		WorkerPoolSize:  3,
		RequestTimeout:  5 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		Logger:          logger,
	}

	server := NewServer(config)
	rootCtx := context.Background()

	// Start server
	if err := server.Start(rootCtx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Give server time to start
	time.Sleep(100 * time.Millisecond)

	// Send multiple requests
	var wg sync.WaitGroup
	requestCount := 10
	for i := 0; i < requestCount; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			resp, err := http.Get(fmt.Sprintf("http://localhost:%s/", config.Port))
			if err != nil {
				// Expected after shutdown starts
				return
			}
			resp.Body.Close()
		}(i)
	}

	// Immediately trigger shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()

	// Start shutdown in background
	var shutdownErr error
	var shutdownWg sync.WaitGroup
	shutdownWg.Add(1)
	go func() {
		defer shutdownWg.Done()
		shutdownErr = server.Stop(shutdownCtx)
	}()

	// Wait for requests to complete or be cancelled
	wg.Wait()

	// Wait for shutdown to complete
	shutdownWg.Wait()

	if shutdownErr != nil {
		t.Logf("shutdown completed with error (may be expected): %v", shutdownErr)
	}
}

func TestServer_ShutdownTimeout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	config := Config{
		Port:            "8082",
		WorkerPoolSize:  2,
		RequestTimeout:  5 * time.Second,
		ShutdownTimeout: 2 * time.Second, // Short timeout
		Logger:          logger,
	}

	server := NewServer(config)
	rootCtx := context.Background()

	if err := server.Start(rootCtx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	// Create a context with shorter timeout than shutdown timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	start := time.Now()
	err := server.Stop(shutdownCtx)
	duration := time.Since(start)

	if duration > 2*time.Second {
		t.Errorf("shutdown took %v, expected ~1s (timeout context)", duration)
	}

	if err == nil {
		t.Log("shutdown completed (may have timed out internally)")
	}
}

func TestServer_NoGoroutineLeak(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	// Get initial goroutine count
	runtime.GC()
	initialGoroutines := runtime.NumGoroutine()

	config := Config{
		Port:            "8083",
		WorkerPoolSize:  5,
		RequestTimeout:  5 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		Logger:          logger,
	}

	server := NewServer(config)
	rootCtx := context.Background()

	// Start server
	if err := server.Start(rootCtx); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	// Run for a short period
	time.Sleep(2 * time.Second)

	// Send some requests
	for i := 0; i < 5; i++ {
		go func() {
			http.Get(fmt.Sprintf("http://localhost:%s/", config.Port))
		}()
		time.Sleep(100 * time.Millisecond)
	}

	// Shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), config.ShutdownTimeout)
	defer cancel()

	if err := server.Stop(shutdownCtx); err != nil {
		t.Logf("shutdown error: %v", err)
	}

	// Wait a bit for cleanup
	time.Sleep(500 * time.Millisecond)
	runtime.GC()

	// Check goroutine count
	finalGoroutines := runtime.NumGoroutine()
	leaked := finalGoroutines - initialGoroutines

	if leaked > 2 { // Allow some margin for test framework
		t.Errorf("potential goroutine leak: started with %d, ended with %d (leaked: %d)",
			initialGoroutines, finalGoroutines, leaked)
	} else {
		t.Logf("no goroutine leak detected: started with %d, ended with %d",
			initialGoroutines, finalGoroutines)
	}
}

func TestWorkerPool_ChannelCoordination(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	wp := newWorkerPool(3, logger)
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup
	wg.Add(1)
	go wp.start(ctx, &wg)

	// Submit some requests
	for i := 0; i < 5; i++ {
		req := &request{
			w: nil, // Not used in this test - workers will handle nil gracefully
			r: &http.Request{},
		}
		if err := wp.submit(ctx, req); err != nil {
			t.Logf("submit error (may be expected): %v", err)
		}
		time.Sleep(10 * time.Millisecond) // Give workers time to process
	}

	// Cancel context to stop workers
	cancel()

	// Wait for workers to finish
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := wp.stop(shutdownCtx); err != nil {
		t.Errorf("worker pool stop error: %v", err)
	}

	wg.Wait()
}

func TestCacheWarmer_TickerCleanup(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelError,
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cw := newCacheWarmer(ctx, logger)

	var wg sync.WaitGroup
	wg.Add(1)
	go cw.start(&wg)

	// Let it run for a bit
	time.Sleep(100 * time.Millisecond)

	// Cancel context
	cancel()

	// Wait for cleanup
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success - ticker was cleaned up properly
	case <-time.After(2 * time.Second):
		t.Error("cache warmer did not clean up in time")
	}
}

