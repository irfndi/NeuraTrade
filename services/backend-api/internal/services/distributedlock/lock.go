// Package distributedlock provides Redis-based distributed locking mechanisms
// for coordinating access to shared resources across multiple service instances.
package distributedlock

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Locker provides distributed locking capabilities using Redis.
type Locker struct {
	client *redis.Client
	mu     sync.RWMutex
	locks  map[string]*Lock
}

// Lock represents an acquired distributed lock.
type Lock struct {
	key       string
	token     string
	expiresAt time.Time
	renewalCh chan struct{}
	once      sync.Once
}

// LockOptions configures lock behavior.
type LockOptions struct {
	// TTL is the lock expiration time.
	TTL time.Duration
	// WaitTimeout is how long to wait for lock acquisition.
	WaitTimeout time.Duration
	// RetryInterval is the delay between retry attempts.
	RetryInterval time.Duration
	// AutoRenewal enables automatic lock renewal.
	AutoRenewal bool
	// RenewalInterval is how often to renew the lock.
	RenewalInterval time.Duration
}

// DefaultLockOptions returns sensible defaults.
func DefaultLockOptions() LockOptions {
	return LockOptions{
		TTL:             30 * time.Second,
		WaitTimeout:     0, // No wait by default
		RetryInterval:   100 * time.Millisecond,
		AutoRenewal:     false,
		RenewalInterval: 10 * time.Second,
	}
}

// releaseLockScript is a Lua script that safely releases a lock only if the token matches.
const releaseLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`

// extendLockScript is a Lua script that extends lock TTL only if the token matches.
const extendLockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`

// NewLocker creates a new distributed lock manager.
func NewLocker(client *redis.Client) *Locker {
	return &Locker{
		client: client,
		locks:  make(map[string]*Lock),
	}
}

// TryLock attempts to acquire a lock without waiting.
func (l *Locker) TryLock(ctx context.Context, key string, opts LockOptions) (*Lock, error) {
	if l.client == nil {
		return nil, fmt.Errorf("redis client is nil")
	}

	token := uuid.NewString()
	acquired, err := l.client.SetNX(ctx, key, token, opts.TTL).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !acquired {
		return nil, fmt.Errorf("lock already held")
	}

	lock := &Lock{
		key:       key,
		token:     token,
		expiresAt: time.Now().Add(opts.TTL),
		renewalCh: make(chan struct{}),
	}

	l.mu.Lock()
	l.locks[key] = lock
	l.mu.Unlock()

	if opts.AutoRenewal {
		go l.autoRenew(lock, opts)
	}

	return lock, nil
}

// Lock attempts to acquire a lock, waiting if necessary.
func (l *Locker) Lock(ctx context.Context, key string, opts LockOptions) (*Lock, error) {
	if opts.WaitTimeout == 0 {
		return l.TryLock(ctx, key, opts)
	}

	ctx, cancel := context.WithTimeout(ctx, opts.WaitTimeout)
	defer cancel()

	ticker := time.NewTicker(opts.RetryInterval)
	defer ticker.Stop()

	for {
		lock, err := l.TryLock(ctx, key, opts)
		if err == nil {
			return lock, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for lock: %w", ctx.Err())
		case <-ticker.C:
			continue
		}
	}
}

// Unlock releases a lock.
func (l *Locker) Unlock(ctx context.Context, lock *Lock) error {
	if lock == nil {
		return fmt.Errorf("lock is nil")
	}

	lock.once.Do(func() {
		close(lock.renewalCh)
	})

	l.mu.Lock()
	delete(l.locks, lock.key)
	l.mu.Unlock()

	result, err := l.client.Eval(ctx, releaseLockScript, []string{lock.key}, lock.token).Int64()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}

	if result == 0 {
		return fmt.Errorf("lock was not held or token mismatch")
	}

	return nil
}

// Extend extends the TTL of an acquired lock.
func (l *Locker) Extend(ctx context.Context, lock *Lock, ttl time.Duration) error {
	if lock == nil {
		return fmt.Errorf("lock is nil")
	}

	result, err := l.client.Eval(ctx, extendLockScript, []string{lock.key}, lock.token, ttl.Milliseconds()).Int64()
	if err != nil {
		return fmt.Errorf("failed to extend lock: %w", err)
	}

	if result == 0 {
		return fmt.Errorf("lock was not held or token mismatch")
	}

	lock.expiresAt = time.Now().Add(ttl)
	return nil
}

// IsLocked checks if a lock is currently held.
func (l *Locker) IsLocked(ctx context.Context, key string) (bool, error) {
	exists, err := l.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists == 1, nil
}

// ForceUnlock forcibly releases a lock regardless of ownership (use with caution).
func (l *Locker) ForceUnlock(ctx context.Context, key string) error {
	return l.client.Del(ctx, key).Err()
}

// GetLockInfo returns information about an acquired lock.
func (l *Locker) GetLockInfo(key string) (*Lock, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	lock, exists := l.locks[key]
	return lock, exists
}

// autoRenew automatically extends lock TTL at intervals.
func (l *Locker) autoRenew(lock *Lock, opts LockOptions) {
	ticker := time.NewTicker(opts.RenewalInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lock.renewalCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := l.Extend(ctx, lock, opts.TTL); err != nil {
				// Log error but don't stop renewal - lock may have been lost
				cancel()
				continue
			}
			cancel()
		}
	}
}

// Close cleans up all tracked locks.
func (l *Locker) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, lock := range l.locks {
		lock.once.Do(func() {
			close(lock.renewalCh)
		})
		_, _ = l.client.Eval(ctx, releaseLockScript, []string{lock.key}, lock.token).Result()
	}

	l.locks = make(map[string]*Lock)
	return nil
}

// GetToken returns the lock token.
func (l *Lock) GetToken() string {
	return l.token
}

// GetKey returns the lock key.
func (l *Lock) GetKey() string {
	return l.key
}

// GetExpiration returns when the lock expires.
func (l *Lock) GetExpiration() time.Time {
	return l.expiresAt
}
