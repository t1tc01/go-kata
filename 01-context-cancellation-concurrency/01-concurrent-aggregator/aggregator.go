package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/sync/errgroup"
)

// UserAggregator aggregates user data from multiple services
type UserAggregator struct {
	timeout time.Duration
	logger  *slog.Logger
	profile *ProfileService
	order   *OrderService
}

// Option configures UserAggregator
type Option func(*UserAggregator)

// WithTimeout sets the timeout for aggregation operations
func WithTimeout(d time.Duration) Option {
	return func(a *UserAggregator) {
		a.timeout = d
	}
}

// WithLogger sets a custom logger
func WithLogger(logger *slog.Logger) Option {
	return func(a *UserAggregator) {
		a.logger = logger
	}
}

// New creates a new UserAggregator with the provided options
func New(opts ...Option) *UserAggregator {
	agg := &UserAggregator{
		timeout: 5 * time.Second, // default timeout
		logger:  slog.Default(),
		profile: NewProfileService(),
		order:   NewOrderService(),
	}

	for _, opt := range opts {
		opt(agg)
	}

	return agg
}

// Aggregate fetches data from Profile and Order services concurrently
// Returns combined result or error if any service fails or timeout occurs
func (a *UserAggregator) Aggregate(ctx context.Context, id int) (string, error) {
	// Create context with timeout
	ctx, cancel := context.WithTimeout(ctx, a.timeout)
	defer cancel()

	// Create errgroup with context for automatic cancellation
	g, gCtx := errgroup.WithContext(ctx)

	var profileResult string
	var orderResult string

	// Fetch profile data concurrently
	g.Go(func() error {
		a.logger.Info("fetching profile", "user_id", id)
		result, err := a.profile.Fetch(gCtx, id)
		if err != nil {
			a.logger.Error("profile fetch failed", "error", err, "user_id", id)
			return fmt.Errorf("profile service: %w", err)
		}
		profileResult = result
		a.logger.Info("profile fetched successfully", "user_id", id)
		return nil
	})

	// Fetch order data concurrently
	g.Go(func() error {
		a.logger.Info("fetching orders", "user_id", id)
		result, err := a.order.Fetch(gCtx, id)
		if err != nil {
			a.logger.Error("order fetch failed", "error", err, "user_id", id)
			return fmt.Errorf("order service: %w", err)
		}
		orderResult = result
		a.logger.Info("orders fetched successfully", "user_id", id)
		return nil
	})

	// Wait for all goroutines to complete or fail
	if err := g.Wait(); err != nil {
		return "", err
	}

	// Combine results
	result := fmt.Sprintf("User: %s | %s", profileResult, orderResult)
	a.logger.Info("aggregation completed", "user_id", id, "result", result)
	return result, nil
}
