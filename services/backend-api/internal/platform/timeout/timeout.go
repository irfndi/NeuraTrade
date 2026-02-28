// Package timeout provides standardized timeout utilities for all IO operations.
// Every external call (exchange, database, redis, telegram) should use these utilities.
package timeout

import (
	"context"
	"time"
)

// Preset timeout values for different operation types.
const (
	// Fast operations: local cache, in-memory lookups
	Fast = 100 * time.Millisecond

	// Normal operations: database queries, local processing
	Normal = 5 * time.Second

	// Slow operations: external API calls, network requests
	Slow = 30 * time.Second

	// Extended operations: bulk processing, large data transfers
	Extended = 5 * time.Minute
)

// Config holds timeout configuration for a service.
type Config struct {
	// Database is the timeout for database operations.
	Database time.Duration

	// Redis is the timeout for Redis operations.
	Redis time.Duration

	// Exchange is the timeout for exchange API calls.
	Exchange time.Duration

	// Telegram is the timeout for Telegram API calls.
	Telegram time.Duration

	// Plugin is the timeout for plugin execution.
	Plugin time.Duration

	// Default is the fallback timeout.
	Default time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Database: Normal,
		Redis:    Fast,
		Exchange: Slow,
		Telegram: Normal,
		Plugin:   Normal,
		Default:  Normal,
	}
}

// Context creates a context with the specified timeout.
// If parent already has a deadline sooner than timeout, the parent's deadline is preserved.
func Context(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, timeout)
}

// ContextWithDefault creates a context with the default timeout.
func ContextWithDefault(parent context.Context) (context.Context, context.CancelFunc) {
	return Context(parent, Normal)
}

// Do executes a function with a timeout.
// Returns context.DeadlineExceeded if the timeout is exceeded.
func Do(parent context.Context, timeout time.Duration, fn func(ctx context.Context) error) error {
	ctx, cancel := Context(parent, timeout)
	defer cancel()
	return fn(ctx)
}

// DoWithResult executes a function with a timeout and returns a result.
func DoWithResult[T any](parent context.Context, timeout time.Duration, fn func(ctx context.Context) (T, error)) (T, error) {
	ctx, cancel := Context(parent, timeout)
	defer cancel()
	return fn(ctx)
}

// WithDatabase creates a context with the database timeout.
func (c Config) WithDatabase(parent context.Context) (context.Context, context.CancelFunc) {
	return Context(parent, c.Database)
}

// WithRedis creates a context with the Redis timeout.
func (c Config) WithRedis(parent context.Context) (context.Context, context.CancelFunc) {
	return Context(parent, c.Redis)
}

// WithExchange creates a context with the exchange timeout.
func (c Config) WithExchange(parent context.Context) (context.Context, context.CancelFunc) {
	return Context(parent, c.Exchange)
}

// WithTelegram creates a context with the Telegram timeout.
func (c Config) WithTelegram(parent context.Context) (context.Context, context.CancelFunc) {
	return Context(parent, c.Telegram)
}

// WithPlugin creates a context with the plugin timeout.
func (c Config) WithPlugin(parent context.Context) (context.Context, context.CancelFunc) {
	return Context(parent, c.Plugin)
}

// WithDefault creates a context with the default timeout.
func (c Config) WithDefault(parent context.Context) (context.Context, context.CancelFunc) {
	return Context(parent, c.Default)
}
