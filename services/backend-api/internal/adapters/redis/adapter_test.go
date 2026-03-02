package redis

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/irfndi/neuratrade/internal/database"
	redisv9 "github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestAdapter(t *testing.T) (*Adapter, *miniredis.Miniredis) {
	t.Helper()

	server, err := miniredis.Run()
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "operation not permitted") {
			t.Skip("miniredis cannot bind in this environment")
		}
		require.NoError(t, err)
	}

	client := redisv9.NewClient(&redisv9.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = client.Close()
		server.Close()
	})

	return NewAdapter(&database.RedisClient{Client: client}), server
}

func TestAdapter_NilClientErrors(t *testing.T) {
	adapter := NewAdapter(nil)
	ctx := context.Background()

	_, err := adapter.Get(ctx, "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	err = adapter.Set(ctx, "key", []byte("value"), time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	err = adapter.Delete(ctx, "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	_, err = adapter.Exists(ctx, "key")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")

	err = adapter.Health(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "redis client is nil")
}

func TestAdapter_Smoke(t *testing.T) {
	adapter, _ := newTestAdapter(t)
	ctx := context.Background()

	err := adapter.Set(ctx, "cache:key", []byte("hello"), time.Minute)
	require.NoError(t, err)

	value, err := adapter.Get(ctx, "cache:key")
	require.NoError(t, err)
	assert.Equal(t, []byte("hello"), value)

	exists, err := adapter.Exists(ctx, "cache:key")
	require.NoError(t, err)
	assert.True(t, exists)

	err = adapter.Delete(ctx, "cache:key")
	require.NoError(t, err)

	exists, err = adapter.Exists(ctx, "cache:key")
	require.NoError(t, err)
	assert.False(t, exists)

	err = adapter.Health(ctx)
	require.NoError(t, err)
}
