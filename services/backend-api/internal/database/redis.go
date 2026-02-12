package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/config"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/redis/go-redis/v9"
)

var releaseLockScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// ErrorRecoveryManager interface for dependency injection.
// Allows injecting a mechanism to retry failed operations.
type ErrorRecoveryManager interface {
	// ExecuteWithRetry attempts to execute an operation with retry logic.
	ExecuteWithRetry(ctx context.Context, operationName string, operation func() error) error
}

// RedisClient wraps a Redis client with enhanced logging and error tracking.
type RedisClient struct {
	Client *redis.Client
	logger *zaplogrus.Logger
}

// NewRedisConnection creates a new Redis connection.
//
// Parameters:
//
//	cfg: Redis configuration.
//
// Returns:
//
//	*RedisClient: The initialized client.
//	error: Error if connection fails.
func NewRedisConnection(cfg config.RedisConfig) (*RedisClient, error) {
	return NewRedisConnectionWithRetry(cfg, nil)
}

// NewRedisConnectionWithRetry creates a new Redis connection with retry logic.
//
// Parameters:
//
//	cfg: Redis configuration.
//	errorRecoveryManager: Optional retry manager.
//
// Returns:
//
//	*RedisClient: The initialized client.
//	error: Error if connection fails.
func NewRedisConnectionWithRetry(cfg config.RedisConfig, errorRecoveryManager ErrorRecoveryManager) (*RedisClient, error) {
	logger := zaplogrus.New()

	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Add Sentry hook for error tracking
	rdb.AddHook(&RedisSentryHook{})

	// Test the connection with retry logic if available
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var connectionErr error
	if errorRecoveryManager != nil {
		// Use retry mechanism
		connectionErr = errorRecoveryManager.ExecuteWithRetry(ctx, "redis_operation", func() error {
			return rdb.Ping(ctx).Err()
		})
	} else {
		// Fallback to simple connection test
		connectionErr = rdb.Ping(ctx).Err()
	}

	if connectionErr != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", connectionErr)
	}

	logger.Info("Successfully connected to Redis")

	return &RedisClient{
		Client: rdb,
		logger: logger,
	}, nil
}

// Close closes the Redis connection.
func (r *RedisClient) Close() {
	if r.Client != nil {
		if err := r.Client.Close(); err != nil {
			// Log error but don't return it since this is a cleanup function
			if r.logger != nil {
				r.logger.WithError(err).Error("Error closing Redis client")
			} else {
				zaplogrus.Errorf("Error closing Redis client: %v", err)
			}
		}
		if r.logger != nil {
			r.logger.Info("Redis connection closed")
		} else {
			zaplogrus.Info("Redis connection closed")
		}
	}
}

// HealthCheck verifies the Redis connection.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	error: Error if ping fails.
func (r *RedisClient) HealthCheck(ctx context.Context) error {
	if r.Client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return r.Client.Ping(ctx).Err()
}

// Set stores a key-value pair with expiration.
//
// Parameters:
//
//	ctx: Context.
//	key: Key.
//	value: Value.
//	expiration: TTL.
//
// Returns:
//
//	error: Error if set fails.
func (r *RedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	if r.Client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return r.Client.Set(ctx, key, value, expiration).Err()
}

// Get retrieves a value by key.
//
// Parameters:
//
//	ctx: Context.
//	key: Key.
//
// Returns:
//
//	string: Value.
//	error: Error if get fails.
func (r *RedisClient) Get(ctx context.Context, key string) (string, error) {
	if r.Client == nil {
		return "", fmt.Errorf("redis client is nil")
	}
	return r.Client.Get(ctx, key).Result()
}

// Delete removes one or more keys.
//
// Parameters:
//
//	ctx: Context.
//	keys: Keys to remove.
//
// Returns:
//
//	error: Error if delete fails.
func (r *RedisClient) Delete(ctx context.Context, keys ...string) error {
	if r.Client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return r.Client.Del(ctx, keys...).Err()
}

// Exists checks if keys exist.
//
// Parameters:
//
//	ctx: Context.
//	keys: Keys to check.
//
// Returns:
//
//	int64: Number of existing keys.
//	error: Error if check fails.
func (r *RedisClient) Exists(ctx context.Context, keys ...string) (int64, error) {
	if r.Client == nil {
		return 0, fmt.Errorf("redis client is nil")
	}
	return r.Client.Exists(ctx, keys...).Result()
}

func (r *RedisClient) Publish(ctx context.Context, channel string, value interface{}) error {
	if r.Client == nil {
		return fmt.Errorf("redis client is nil")
	}
	if channel == "" {
		return fmt.Errorf("channel cannot be empty")
	}
	return r.Client.Publish(ctx, channel, value).Err()
}

func (r *RedisClient) Subscribe(ctx context.Context, channels ...string) (*redis.PubSub, error) {
	if r.Client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}
	if len(channels) == 0 {
		return nil, fmt.Errorf("at least one channel is required")
	}

	pubsub := r.Client.Subscribe(ctx, channels...)
	if err := pubsub.Ping(ctx); err != nil {
		_ = pubsub.Close()
		return nil, err
	}

	return pubsub, nil
}

func (r *RedisClient) AcquireLock(ctx context.Context, key string, expiration time.Duration) (string, bool, error) {
	if r.Client == nil {
		return "", false, fmt.Errorf("redis client is nil")
	}
	if key == "" {
		return "", false, fmt.Errorf("lock key cannot be empty")
	}
	if expiration <= 0 {
		return "", false, fmt.Errorf("lock expiration must be positive")
	}

	token := uuid.NewString()
	acquired, err := r.Client.SetNX(ctx, key, token, expiration).Result()
	if err != nil {
		return "", false, err
	}

	if !acquired {
		return "", false, nil
	}

	return token, true, nil
}

func (r *RedisClient) ReleaseLock(ctx context.Context, key, token string) (bool, error) {
	if r.Client == nil {
		return false, fmt.Errorf("redis client is nil")
	}
	if key == "" {
		return false, fmt.Errorf("lock key cannot be empty")
	}
	if token == "" {
		return false, fmt.Errorf("lock token cannot be empty")
	}

	deleted, err := releaseLockScript.Run(ctx, r.Client, []string{key}, token).Int64()
	if err != nil {
		return false, err
	}

	return deleted == 1, nil
}
