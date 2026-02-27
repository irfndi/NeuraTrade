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

// Set stores a value in the cache.
func (a *Adapter) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if a.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return a.client.Set(ctx, key, value, ttl)
}

// Delete deletes a value from the cache.
func (a *Adapter) Delete(ctx context.Context, key string) error {
	if a.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return a.client.Delete(ctx, key)
}

// Exists checks if a key exists.
func (a *Adapter) Exists(ctx context.Context, key string) (bool, error) {
	if a.client == nil {
		return false, fmt.Errorf("redis client is nil")
	}
	count, err := a.client.Exists(ctx, key)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Health checks if the cache is healthy.
func (a *Adapter) Health(ctx context.Context) error {
	if a.client == nil {
		return fmt.Errorf("redis client is nil")
	}
	return a.client.HealthCheck(ctx)
}

// Compile-time interface check
var _ ports.CacheStore = (*Adapter)(nil)
