package database

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRedisClient(t *testing.T) (*RedisClient, *miniredis.Miniredis) {
	t.Helper()

	server, err := miniredis.Run()
	require.NoError(t, err)

	rdb := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = rdb.Close()
		server.Close()
	})

	return &RedisClient{Client: rdb}, server
}

func TestRedisClient_PublishSubscribe(t *testing.T) {
	client, _ := newTestRedisClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pubsub, err := client.Subscribe(ctx, "market:btc")
	require.NoError(t, err)
	defer func() { _ = pubsub.Close() }()

	err = client.Publish(ctx, "market:btc", "tick-1")
	require.NoError(t, err)

	select {
	case msg := <-pubsub.Channel():
		require.NotNil(t, msg)
		assert.Equal(t, "market:btc", msg.Channel)
		assert.Equal(t, "tick-1", msg.Payload)
	case <-ctx.Done():
		t.Fatal("did not receive published message")
	}
}

func TestRedisClient_AcquireReleaseLock(t *testing.T) {
	client, _ := newTestRedisClient(t)
	ctx := context.Background()

	token1, acquired, err := client.AcquireLock(ctx, "lock:position:btc", 5*time.Second)
	require.NoError(t, err)
	require.True(t, acquired)
	require.NotEmpty(t, token1)

	token2, acquired, err := client.AcquireLock(ctx, "lock:position:btc", 5*time.Second)
	require.NoError(t, err)
	require.False(t, acquired)
	require.Empty(t, token2)

	released, err := client.ReleaseLock(ctx, "lock:position:btc", "wrong-token")
	require.NoError(t, err)
	assert.False(t, released)

	released, err = client.ReleaseLock(ctx, "lock:position:btc", token1)
	require.NoError(t, err)
	assert.True(t, released)

	_, acquired, err = client.AcquireLock(ctx, "lock:position:btc", 5*time.Second)
	require.NoError(t, err)
	assert.True(t, acquired)
}

func TestRedisClient_ValidationErrors(t *testing.T) {
	client, _ := newTestRedisClient(t)
	ctx := context.Background()

	err := client.Publish(ctx, "", "value")
	assert.Error(t, err)

	_, err = client.Subscribe(ctx)
	assert.Error(t, err)

	_, _, err = client.AcquireLock(ctx, "", time.Second)
	assert.Error(t, err)

	_, _, err = client.AcquireLock(ctx, "lock:key", 0)
	assert.Error(t, err)

	_, err = client.ReleaseLock(ctx, "", "token")
	assert.Error(t, err)

	_, err = client.ReleaseLock(ctx, "lock:key", "")
	assert.Error(t, err)
}
