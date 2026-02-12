package distributedlock

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocker(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	locker := NewLocker(client)
	require.NotNil(t, locker)
	assert.NotNil(t, locker.client)
	assert.NotNil(t, locker.locks)
}

func TestLocker_TryLock(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	locker := NewLocker(client)
	ctx := t.Context()

	opts := DefaultLockOptions()
	opts.TTL = 1 * time.Second

	lock, err := locker.TryLock(ctx, "test-key", opts)
	require.NoError(t, err)
	require.NotNil(t, lock)
	assert.Equal(t, "test-key", lock.GetKey())
	assert.NotEmpty(t, lock.GetToken())

	_, err = locker.TryLock(ctx, "test-key", opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "lock already held")
}

func TestLocker_Unlock(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	locker := NewLocker(client)
	ctx := t.Context()

	opts := DefaultLockOptions()
	opts.TTL = 1 * time.Second

	lock, err := locker.TryLock(ctx, "test-key", opts)
	require.NoError(t, err)

	err = locker.Unlock(ctx, lock)
	assert.NoError(t, err)

	_, err = locker.TryLock(ctx, "test-key", opts)
	assert.NoError(t, err)
}

func TestLocker_IsLocked(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	locker := NewLocker(client)
	ctx := t.Context()

	locked, err := locker.IsLocked(ctx, "test-key")
	require.NoError(t, err)
	assert.False(t, locked)

	opts := DefaultLockOptions()
	opts.TTL = 1 * time.Second

	_, err = locker.TryLock(ctx, "test-key", opts)
	require.NoError(t, err)

	locked, err = locker.IsLocked(ctx, "test-key")
	require.NoError(t, err)
	assert.True(t, locked)
}

func TestLocker_Extend(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	locker := NewLocker(client)
	ctx := t.Context()

	opts := DefaultLockOptions()
	opts.TTL = 1 * time.Second

	lock, err := locker.TryLock(ctx, "test-key", opts)
	require.NoError(t, err)

	err = locker.Extend(ctx, lock, 5*time.Second)
	assert.NoError(t, err)
}

func TestLocker_Lock_WithTimeout(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	locker := NewLocker(client)
	ctx := t.Context()

	opts := DefaultLockOptions()
	opts.TTL = 1 * time.Second
	opts.WaitTimeout = 500 * time.Millisecond
	opts.RetryInterval = 50 * time.Millisecond

	lock1, err := locker.TryLock(ctx, "test-key", opts)
	require.NoError(t, err)

	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = locker.Unlock(ctx, lock1)
	}()

	lock2, err := locker.Lock(ctx, "test-key", opts)
	require.NoError(t, err)
	assert.NotNil(t, lock2)
}

func TestLocker_Lock_Timeout(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	locker := NewLocker(client)
	ctx := t.Context()

	opts := DefaultLockOptions()
	opts.TTL = 5 * time.Second
	opts.WaitTimeout = 100 * time.Millisecond
	opts.RetryInterval = 10 * time.Millisecond

	_, err := locker.TryLock(ctx, "test-key", opts)
	require.NoError(t, err)

	_, err = locker.Lock(ctx, "test-key", opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout")
}

func TestLocker_ForceUnlock(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	locker := NewLocker(client)
	ctx := t.Context()

	opts := DefaultLockOptions()
	opts.TTL = 5 * time.Second

	_, err := locker.TryLock(ctx, "test-key", opts)
	require.NoError(t, err)

	err = locker.ForceUnlock(ctx, "test-key")
	assert.NoError(t, err)

	locked, err := locker.IsLocked(ctx, "test-key")
	require.NoError(t, err)
	assert.False(t, locked)
}

func TestLocker_Close(t *testing.T) {
	s := miniredis.RunT(t)
	defer s.Close()

	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	locker := NewLocker(client)
	ctx := t.Context()

	opts := DefaultLockOptions()
	opts.TTL = 5 * time.Second

	lock, err := locker.TryLock(ctx, "test-key", opts)
	require.NoError(t, err)

	locker.locks["test-key"] = lock

	err = locker.Close()
	assert.NoError(t, err)

	locked, err := locker.IsLocked(ctx, "test-key")
	require.NoError(t, err)
	assert.False(t, locked)
}
