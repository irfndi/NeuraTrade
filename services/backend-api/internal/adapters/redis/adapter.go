// Package redis provides an adapter that wraps the existing Redis client
// to implement the ports.CacheStore interface.
package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/ports"
)

// Adapter wraps the existing Redis client to implement CacheStore.
type Adapter struct {
	client *database.RedisClient
}

// NewAdapter creates a new Redis cache adapter.
func NewAdapter(client *database.RedisClient) *Adapter {
	return &Adapter{client: client}
}

// Get fetches a value from the cache.
func (a *Adapter) Get(ctx context.Context, key string) ([]byte, error) {
	if a.client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}
	val, err := a.client.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("cache get failed: %w", err)
	}
	return []byte(val), nil
}

func (a *Adapter) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if a.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	if err := a.client.Set(ctx, key, value, ttl); err != nil {
		return fmt.Errorf("cache set failed: %w", err)
	}
	return nil
}

func (a *Adapter) Delete(ctx context.Context, key string) error {
	if a.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	if err := a.client.Delete(ctx, key); err != nil {
		return fmt.Errorf("cache delete failed: %w", err)
	}
	return nil
}

func (a *Adapter) Exists(ctx context.Context, key string) (bool, error) {
	if a.client == nil {
		return false, fmt.Errorf("redis client is nil")
	}
	count, err := a.client.Exists(ctx, key)
	if err != nil {
		return false, fmt.Errorf("cache exists check failed: %w", err)
	}
	return count > 0, nil
}

func (a *Adapter) Health(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	if err := a.client.HealthCheck(ctx); err != nil {
		return fmt.Errorf("cache health check failed: %w", err)
	}
	return nil
}

// Compile-time interface check
var _ ports.CacheStore = (*Adapter)(nil)
