// Package retry provides retry utilities with exponential backoff and jitter.
// All external calls should use these utilities for resilience.
package retry

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// Errors
var (
	ErrMaxRetries   = errors.New("max retries exceeded")
	ErrNonRetryable = errors.New("non-retryable error")
)

// IsRetryable determines if an error is retryable.
type IsRetryable func(err error) bool

// DefaultIsRetryable returns true for common retryable errors.
func DefaultIsRetryable(err error) bool {
	if err == nil {
		return false
	}
	// Timeout errors are retryable
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// Context cancelled is not retryable
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

// Config holds retry configuration.
type Config struct {
	// MaxAttempts is the maximum number of attempts (including initial).
	MaxAttempts int

	// InitialDelay is the delay before the first retry.
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries.
	MaxDelay time.Duration

	// Multiplier is the backoff multiplier.
	Multiplier float64

	// Jitter is the random jitter factor (0-1).
	Jitter float64

	// IsRetryable determines if an error is retryable.
	IsRetryable IsRetryable
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		MaxAttempts:  3,
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     30 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
		IsRetryable:  DefaultIsRetryable,
	}
}

// NewConfig creates a Config with custom values.
func NewConfig(maxAttempts int, initialDelay, maxDelay time.Duration) Config {
	return Config{
		MaxAttempts:  maxAttempts,
		InitialDelay: initialDelay,
		MaxDelay:     maxDelay,
		Multiplier:   2.0,
		Jitter:       0.1,
		IsRetryable:  DefaultIsRetryable,
	}
}

// Retryable wraps an error to indicate it's retryable.
type RetryableError struct {
	Err error
}

func (e *RetryableError) Error() string {
	return e.Err.Error()
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

// MarkRetryable marks an error as retryable.
func MarkRetryable(err error) error {
	return &RetryableError{Err: err}
}

// Do executes a function with retry logic.
func Do(ctx context.Context, config Config, fn func(ctx context.Context) error) error {
	var lastErr error

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}

		err := fn(ctx)
		if err == nil {
			return nil
		}

		// Check if error is retryable
		var retryableErr *RetryableError
		if errors.As(err, &retryableErr) {
			// Marked as retryable
		} else if !config.IsRetryable(err) {
			return err
		}

		lastErr = err

		// Calculate delay
		if attempt < config.MaxAttempts-1 {
			delay := calculateDelay(config, attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return errors.Join(ErrMaxRetries, lastErr)
}

// DoWithResult executes a function with retry logic and returns a result.
func DoWithResult[T any](ctx context.Context, config Config, fn func(ctx context.Context) (T, error)) (T, error) {
	var result T
	var lastErr error

	for attempt := 0; attempt < config.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return result, err
		}

		r, err := fn(ctx)
		if err == nil {
			return r, nil
		}

		var retryableErr *RetryableError
		if errors.As(err, &retryableErr) {
			// Marked as retryable
		} else if !config.IsRetryable(err) {
			return result, err
		}

		lastErr = err

		if attempt < config.MaxAttempts-1 {
			delay := calculateDelay(config, attempt)
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return result, errors.Join(ErrMaxRetries, lastErr)
}

func calculateDelay(config Config, attempt int) time.Duration {
	delay := float64(config.InitialDelay)
	for i := 0; i < attempt; i++ {
		delay *= config.Multiplier
	}
	if delay > float64(config.MaxDelay) {
		delay = float64(config.MaxDelay)
	}

	// Add jitter
	if config.Jitter > 0 {
		jitter := delay * config.Jitter * (2*rand.Float64() - 1) // -jitter to +jitter
		delay += jitter
	}

	return time.Duration(delay)
}

// Policy provides a convenient way to create retry policies.
type Policy struct {
	config Config
}

// NewPolicy creates a new retry policy.
func NewPolicy(config Config) *Policy {
	return &Policy{config: config}
}

// Do executes a function with this policy.
func (p *Policy) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return Do(ctx, p.config, fn)
}

// Config returns the underlying config for use with generic functions.
func (p *Policy) Config() Config {
	return p.config
}

// WithMaxAttempts returns a new policy with a different max attempts.
func (p *Policy) WithMaxAttempts(max int) *Policy {
	newConfig := p.config
	newConfig.MaxAttempts = max
	return NewPolicy(newConfig)
}

// WithDelay returns a new policy with different delay settings.
func (p *Policy) WithDelay(initial, max time.Duration) *Policy {
	newConfig := p.config
	newConfig.InitialDelay = initial
	newConfig.MaxDelay = max
	return NewPolicy(newConfig)
}

// WithIsRetryable returns a new policy with a custom retryable checker.
func (p *Policy) WithIsRetryable(fn IsRetryable) *Policy {
	newConfig := p.config
	newConfig.IsRetryable = fn
	return NewPolicy(newConfig)
}

// Quick is a policy for fast operations with quick retries.
var Quick = NewPolicy(Config{
	MaxAttempts:  3,
	InitialDelay: 50 * time.Millisecond,
	MaxDelay:     1 * time.Second,
	Multiplier:   2.0,
	Jitter:       0.1,
	IsRetryable:  DefaultIsRetryable,
})

// Standard is a policy for standard operations.
var Standard = NewPolicy(DefaultConfig())

// Slow is a policy for slow operations with longer delays.
var Slow = NewPolicy(Config{
	MaxAttempts:  5,
	InitialDelay: 1 * time.Second,
	MaxDelay:     60 * time.Second,
	Multiplier:   2.0,
	Jitter:       0.2,
	IsRetryable:  DefaultIsRetryable,
})
