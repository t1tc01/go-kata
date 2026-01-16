package main

import (
	"context"
	"fmt"
	"time"
)

// ProfileService mocks a profile microservice
type ProfileService struct {
	delay   time.Duration
	willErr bool
}

// NewProfileService creates a new ProfileService
func NewProfileService() *ProfileService {
	return &ProfileService{
		delay:   100 * time.Millisecond,
		willErr: false,
	}
}

// WithDelay sets a custom delay for the profile service (for testing)
func (s *ProfileService) WithDelay(d time.Duration) *ProfileService {
	s.delay = d
	return s
}

// WithError makes the service return an error (for testing)
func (s *ProfileService) WithError() *ProfileService {
	s.willErr = true
	return s
}

// Fetch retrieves user profile data
func (s *ProfileService) Fetch(ctx context.Context, id int) (string, error) {
	if s.willErr {
		return "", fmt.Errorf("profile service unavailable")
	}

	// Simulate network delay
	select {
	case <-time.After(s.delay):
		return fmt.Sprintf("Name: Alice"), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// OrderService mocks an order microservice
type OrderService struct {
	delay   time.Duration
	willErr bool
}

// NewOrderService creates a new OrderService
func NewOrderService() *OrderService {
	return &OrderService{
		delay:   150 * time.Millisecond,
		willErr: false,
	}
}

// WithDelay sets a custom delay for the order service (for testing)
func (s *OrderService) WithDelay(d time.Duration) *OrderService {
	s.delay = d
	return s
}

// WithError makes the service return an error (for testing)
func (s *OrderService) WithError() *OrderService {
	s.willErr = true
	return s
}

// Fetch retrieves user order data
func (s *OrderService) Fetch(ctx context.Context, id int) (string, error) {
	if s.willErr {
		return "", fmt.Errorf("order service unavailable")
	}

	// Simulate network delay
	select {
	case <-time.After(s.delay):
		return fmt.Sprintf("Orders: 5"), nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
