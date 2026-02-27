# IMPLEMENTATION PLAN: SQLite-First + Redis-Ready Architecture

## Current State Analysis

### 1. SQLite-First Migration Infrastructure (NEURA-2RT - DONE)
**Status**: Partially Complete ✅

**Completed**:
- ✅ `internal/database/sqlite.go` with extension support
- ✅ `database/sqlite-migrate.sh` migration runner
- ✅ `database/sqlite_migrations/` directory with 2 migrations
- ✅ SQLite config bindings: `SQLITE_DB_PATH`, `SQLITE_VEC_EXTENSION_PATH`
- ✅ SQLite runtime guard in `main.go` (lines 82-92)

**Next Steps**:
1. Create idempotent `001_initial_schema.sql` optimized for SQLite
2. Document sqlite-vec usage patterns for future semantic memory features
3. Add test coverage for SQLite migrations

### 2. Database Abstraction Layer (CRITICAL BLOCKER)
**Status**: Incomplete ⚠️

**Problem**:
- Services directly use `db.Pool` of type `*pgxpool.Pool`
- `DBPool` interface in `interfaces.go` is Postgres-specific (uses `pgx.Rows`, `pgx.Row`, `pgconn.CommandTag`)
- `SQLiteDB` struct uses `*sql.DB` which doesn't implement `DBPool` interface
- **Consequence**: Cannot switch to SQLite runtime without breaking all services

**Affected Services** (47 files total):
- `collector.go` - 15+ direct `db.Pool` calls
- `arbitrage_service.go` - 10+ direct `db.Pool` calls
- `futures_arbitrage_service.go` - 8+ direct `db.Pool` calls
- `cache_warming.go` - 4+ direct `db.Pool` calls

### 3. Redis Pub/Sub, Caching, Locks (NEURA-5DG)
**Status**: Partially Complete ⚠️

**Completed**:
- ✅ `internal/database/redis.go` with retry mechanism
- ✅ `RedisClient` wrapper with basic methods (Set, Get, Delete, Exists)
- ✅ ErrorRecoveryManager integration in `main.go`

**Missing**:
- ❌ Pub/sub channel implementation
- ❌ Distributed lock patterns (Redlock/RedisLock)
- ❌ Redis connection persistence in services
- ❌ Pub/sub subscriber registration

---

## Implementation Roadmap

### PHASE 1: Driver-Agnostic DB Interface (Priority: CRITICAL)

**Goal**: Create unified interface that PostgreSQL and SQLite can both implement.

#### Step 1.1: Refactor `internal/services/interfaces.go`
```go
// Change from pgx-specific to driver-agnostic
type DBPool interface {
    Query(ctx context.Context, sql string, args ...any) (*sql.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) *sql.Row
    Exec(ctx context.Context, sql string, args ...any) (sql.Result, error)
    Begin(ctx context.Context) (*sql.Tx, error)
    Close()
}

// Add Transaction support
type Transaction interface {
    Query(ctx context.Context, sql string, args ...any) (*sql.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) *sql.Row
    Exec(ctx context.Context, sql string, args ...any) (sql.Result, error)
    Commit() error
    Rollback() error
}
```

#### Step 1.2: Implement DBPool for PostgreSQL
- Refactor `internal/database/postgres.go` to implement new interface
- Keep existing `PostgresDB` struct but expose `Pool interface`
- Add helper method `BeginTx` for transactions

#### Step 1.3: Implement DBPool for SQLite
- Refactor `internal/database/sqlite.go` to implement new interface
- Use `*sql.DB` for Query/QueryRow/Exec
- Add `BeginTx` method for transactions
- Maintain extension loading logic

#### Step 1.4: Migrate Services to Interface
**Target Services**:
1. `collector.go` - Replace 15+ `db.Pool` calls with interface
2. `arbitrage_service.go` - Replace 10+ `db.Pool` calls with interface
3. `futures_arbitrage_service.go` - Replace 8+ `db.Pool` calls with interface
4. `cache_warming.go` - Replace 4+ `db.Pool` calls with interface

**Migration Pattern**:
```go
// Before
rows, err := s.db.Pool.Query(ctx, query, args...)

// After
rows, err := s.db.Query(ctx, query, args...)
```

### PHASE 2: SQLite Migration Optimization

#### Step 2.1: Review `001_initial_schema.sql`
- Ensure compatibility with SQLite-specific behaviors
- Add `IF NOT EXISTS` clauses
- Optimize index creation for SQLite

#### Step 2.2: Create `002_vector_memory.sql` (sqlite-vec ready)
```sql
-- Only if sqlite-vec extension is loaded
CREATE VIRTUAL TABLE IF NOT EXISTS semantic_memory USING vec0(0, embedding BLOB);

-- Helper table for metadata
CREATE TABLE IF NOT EXISTS semantic_memory_metadata (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    memory_id TEXT NOT NULL UNIQUE,
    content TEXT NOT NULL,
    embedding_id INTEGER NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (embedding_id) REFERENCES semantic_memory(id)
);
```

### PHASE 3: Redis Pub/Sub & Locks (NEURA-5DG)

#### Step 3.1: Create `internal/services/redis_pubsub.go`
```go
package services

import (
    "context"
    "encoding/json"
    "github.com/redis/go-redis/v9"
)

type RedisPubSub struct {
    client *redis.Client
    channels map[string]*redis.PubSub
    mu sync.RWMutex
}

func NewRedisPubSub(client *redis.Client) *RedisPubSub {
    return &RedisPubSub{
        client: client,
        channels: make(map[string]*redis.PubSub),
    }
}

// Publish sends a message to a channel
func (p *RedisPubSub) Publish(ctx context.Context, channel string, message interface{}) error {
    data, err := json.Marshal(message)
    if err != nil {
        return err
    }
    return p.client.Publish(ctx, channel, data).Err()
}

// Subscribe creates a subscription to a channel
func (p *RedisPubSub) Subscribe(ctx context.Context, channel string) (<-chan redis.Message, error) {
    p.mu.Lock()
    defer p.mu.Unlock()

    if sub, exists := p.channels[channel]; exists {
        return sub.Channel(), nil
    }

    sub := p.client.Subscribe(ctx, channel)
    p.channels[channel] = sub

    return sub.Channel(), nil
}
```

#### Step 3.2: Create `internal/services/redis_lock.go`
```go
package services

import (
    "context"
    "fmt"
    "time"
    "github.com/redis/go-redis/v9"
)

type RedisLock struct {
    client *redis.Client
    lockName string
    ttl time.Duration
}

func NewRedisLock(client *redis.Client, lockName string, ttl time.Duration) *RedisLock {
    return &RedisLock{
        client: client,
        lockName: lockName,
        ttl: ttl,
    }
}

// Acquire tries to acquire the lock with retry logic
func (l *RedisLock) Acquire(ctx context.Context) (bool, error) {
    for {
        locked, err := l.tryAcquire(ctx)
        if err != nil {
            return false, err
        }
        if locked {
            return true, nil
        }
        time.Sleep(100 * time.Millisecond)
    }
}

func (l *RedisLock) tryAcquire(ctx context.Context) (bool, error) {
    result, err := l.client.SetNX(ctx, l.lockName, "locked", l.ttl).Result()
    if err != nil {
        return false, err
    }
    return result, nil
}

// Release releases the lock
func (l *RedisLock) Release(ctx context.Context) error {
    return l.client.Del(ctx, l.lockName).Err()
}

// Renew extends the lock TTL
func (l *RedisLock) Renew(ctx context.Context) error {
    return l.client.Expire(ctx, l.lockName, l.ttl).Err()
}
```

#### Step 3.3: Create `internal/services/redis_caching.go`
```go
package services

import (
    "context"
    "encoding/json"
    "time"
    "github.com/redis/go-redis/v9"
)

type RedisCache struct {
    client *redis.Client
    defaultTTL time.Duration
}

func NewRedisCache(client *redis.Client, defaultTTL time.Duration) *RedisCache {
    if defaultTTL == 0 {
        defaultTTL = 5 * time.Minute
    }
    return &RedisCache{
        client: client,
        defaultTTL: defaultTTL,
    }
}

// Get retrieves a value from cache
func (c *RedisCache) Get(ctx context.Context, key string, dest interface{}) error {
    data, err := c.client.Get(ctx, key).Bytes()
    if err != nil {
        return err
    }
    return json.Unmarshal(data, dest)
}

// Set stores a value in cache with optional TTL
func (c *RedisCache) Set(ctx context.Context, key string, value interface{}, ttl time.Duration) error {
    data, err := json.Marshal(value)
    if err != nil {
        return err
    }

    if ttl == 0 {
        ttl = c.defaultTTL
    }

    return c.client.Set(ctx, key, data, ttl).Err()
}

// Invalidate removes a key from cache
func (c *RedisCache) Invalidate(ctx context.Context, keys ...string) error {
    return c.client.Del(ctx, keys...).Err()
}
```

### PHASE 4: Service Integration

#### Step 4.1: Integrate Redis Pub/Sub in SignalProcessor
- Add `redisPubSub *RedisPubSub` field
- Subscribe to signal channels
- Publish signals to channels

#### Step 4.2: Integrate Redis Locks in ArbitrageService
- Use locks for concurrent opportunity processing
- Implement Redlock-like pattern for distributed safety

#### Step 4.3: Integrate Redis Cache in CollectorService
- Cache symbol lists and exchange metadata
- Cache frequently accessed market data

---

## Priority Order

1. **CRITICAL** (BLOCKS SQLite runtime):
   - Phase 1: Driver-agnostic DB interface
   - Implement DBPool for PostgreSQL and SQLite
   - Migrate CollectorService (highest impact)

2. **HIGH** (enables Redis features):
   - Phase 3.1: Redis Pub/Sub implementation
   - Phase 3.2: Redis Lock implementation

3. **MEDIUM** (optimization):
   - Phase 2: SQLite migration optimization
   - Phase 4: Service integration

---

## Files to Touch (Estimated)

**Phase 1**:
- `internal/services/interfaces.go` - Refactor interface
- `internal/database/postgres.go` - Implement new interface
- `internal/database/sqlite.go` - Implement new interface
- `internal/services/collector.go` - Migrate to interface
- `internal/services/arbitrage_service.go` - Migrate to interface
- `internal/services/futures_arbitrage_service.go` - Migrate to interface
- `internal/services/cache_warming.go` - Migrate to interface

**Phase 2**:
- `database/sqlite_migrations/001_initial_schema.sql` - Optimize
- `database/sqlite_migrations/002_vector_memory.sql` - Create

**Phase 3**:
- `internal/services/redis_pubsub.go` - New file
- `internal/services/redis_lock.go` - New file
- `internal/services/redis_caching.go` - New file

**Phase 4**:
- `internal/services/signal_processor.go` - Integrate pub/sub
- `internal/services/arbitrage_service.go` - Integrate locks
- `internal/services/collector.go` - Integrate cache

---

## Testing Strategy

### Unit Tests
```bash
# Test DB interface implementations
go test ./internal/database/... -run TestDBPool

# Test Redis pub/sub
go test ./internal/services/... -run TestRedisPubSub

# Test Redis locks
go test ./internal/services/... -run TestRedisLock
```

### Integration Tests
```bash
# Test SQLite migrations
./database/sqlite-migrate.sh status
./database/sqlite-migrate.sh run

# Test PostgreSQL migrations
./database/migrate.sh status

# Test full startup with SQLite
SQLITE_DB_PATH=/tmp/test.db DATABASE_DRIVER=sqlite go run cmd/server/main.go
```

---

## Success Criteria

✅ SQLite can be selected via `DATABASE_DRIVER=sqlite`
✅ All services use `DBPool` interface instead of `*pgxpool.Pool`
✅ Redis Pub/Sub channels work for signal distribution
✅ Redis Locks prevent concurrent arbitrage execution
✅ SQLite migrations run successfully
✅ Existing tests pass (coverage ≥80%)

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|------------|--------|------------|
| Service migration breaks existing tests | Medium | High | Migrate incrementally, run tests after each change |
| SQLite query performance issues | Low | Medium | Profile and optimize critical queries |
| Redis pub/sub message loss | Low | Medium | Use persistence and ack mechanisms |
| Extension loading failures | Low | Low | Graceful degradation if extension not available |

