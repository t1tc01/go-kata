package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"
)

func main() {
	// Setup structured logger
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	fmt.Println("=== Kata 01: The Fail-Fast Data Aggregator ===")
	fmt.Println()

	// Test 1: Happy path - both services succeed
	fmt.Println("Test 1: Happy Path")
	testHappyPath(logger)

	// Test 2: The "Slow Poke" - timeout scenario
	fmt.Println("\nTest	 2: The 'Slow Poke' (Timeout)")
	testSlowPoke(logger)

	// Test 3: The "Domino Effect" - one service fails immediately
	fmt.Println("\nTest 3: The 'Domino Effect' (Fail-Fast)")
	testDominoEffect(logger)
}

func testHappyPath(logger *slog.Logger) {
	agg := New(
		WithTimeout(2*time.Second),
		WithLogger(logger),
	)

	ctx := context.Background()
	result, err := agg.Aggregate(ctx, 1)
	if err != nil {
		fmt.Printf("❌ Failed: %v\n", err)
		return
	}

	fmt.Printf("✅ Success: %s\n", result)
}

func testSlowPoke(logger *slog.Logger) {
	agg := New(
		WithTimeout(1*time.Second),
		WithLogger(logger),
	)

	// Make profile service slow (2 seconds)
	agg.profile.WithDelay(2 * time.Second)

	start := time.Now()
	ctx := context.Background()
	_, err := agg.Aggregate(ctx, 2)
	duration := time.Since(start)

	if err == nil {
		fmt.Printf("❌ Failed: Expected timeout error, got nil\n")
		return
	}

	// Check if it failed fast (should be around 1s, not 2s)
	if duration > 1100*time.Millisecond {
		fmt.Printf("❌ Failed: Took %v, expected ~1s (context cancellation not working)\n", duration)
		return
	}

	fmt.Printf("✅ Success: Failed after %v with error: %v\n", duration, err)
}

func testDominoEffect(logger *slog.Logger) {
	agg := New(
		WithTimeout(10*time.Second),
		WithLogger(logger),
	)

	// Make profile service fail immediately
	agg.profile.WithError()
	// Make order service slow (10 seconds)
	agg.order.WithDelay(10 * time.Second)

	start := time.Now()
	ctx := context.Background()
	_, err := agg.Aggregate(ctx, 3)
	duration := time.Since(start)

	if err == nil {
		fmt.Printf("❌ Failed: Expected error, got nil\n")
		return
	}

	// Check if it failed fast (should be immediate, not 10s)
	if duration > 500*time.Millisecond {
		fmt.Printf("❌ Failed: Took %v, expected immediate failure (context cancellation not working)\n", duration)
		return
	}

	fmt.Printf("✅ Success: Failed immediately after %v with error: %v\n", duration, err)
}
