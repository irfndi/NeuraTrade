# Database Integration

This package provides storage interfaces and implementations for time series data.

## Overview

The `database` package provides:
- A generic `Storage` interface for time series data operations
- `InfluxDBStorage` implementation (scaffold)
- `TimescaleDBStorage` implementation (scaffold)

## Status

**Current State: Initial scaffolding - ready for full implementation**

### Completed
- ✅ Created Storage interface with CRUD operations
- ✅ Created InfluxDBStorage struct and skeleton methods
- ✅ Created TimescaleDBStorage struct and skeleton methods
- ✅ Added basic tests for storage creation

### TODO (Requires Full Implementation)

#### InfluxDB Integration
- [ ] Add InfluxDB Go client dependency (`influxdb-client-go`)
- [ ] Implement `StoreCandle()` with actual InfluxDB write
- [ ] Implement `StoreCandles()` with batch writes
- [ ] Implement `GetCandles()` with Flux query
- [ ] Implement `GetLatestCandles()` with sorting
- [ ] Implement `DeleteSymbol()` with delete query
- [ ] Implement `Close()` with proper connection cleanup
- [ ] Add connection pooling and retry logic
- [ ] Add unit tests with testcontainers

#### TimescaleDB Integration
- [ ] Add PostgreSQL/TimescaleDB Go client dependency (`pgx` or `lib/pq`)
- [ ] Create SQL schema/migrations for time series tables
- [ ] Implement `StoreCandle()` with SQL INSERT
- [ ] Implement `StoreCandles()` with batch COPY
- [ ] Implement `GetCandles()` with time range query
- [ ] Implement `GetLatestCandles()` with LIMIT/OFFSET
- [ ] Implement `DeleteSymbol()` with SQL DELETE
- [ ] Implement `Close()` with connection cleanup
- [ ] Add connection pooling (pgxpool)
- [ ] Add unit tests with testcontainers

#### Integration Tests
- [ ] Set up Docker Compose for local testing
- [ ] Add integration tests for InfluxDB
- [ ] Add integration tests for TimescaleDB
- [ ] Add performance benchmarks for both databases

#### Documentation
- [ ] Add README with setup instructions
- [ ] Add examples for common use cases
- [ ] Document DSN/connection string formats
- [ ] Add troubleshooting guide

## Usage Examples

```go
// InfluxDB
storage, err := database.NewInfluxDBStorage("http://localhost:8086", "trading")
if err != nil {
    log.Fatal(err)
}
defer storage.Close()

// TimescaleDB
storage, err := database.NewTimescaleDBStorage("postgres://user:pass@localhost/trading")
if err != nil {
    log.Fatal(err)
}
defer storage.Close()

// Store candle
candle := &database.Candle{
    Symbol:    "BTC",
    Timestamp: time.Now().UnixNano(),
    Open:       100.0,
    High:       105.0,
    Low:        95.0,
    Close:      102.0,
    Volume:     1000.0,
}
err = storage.StoreCandle("BTC", candle)
```

## Dependencies Required

### InfluxDB
```go
import "github.com/influxdata/influxdb-client-go/v2"
```

### TimescaleDB
```go
import "github.com/jackc/pgx/v5"
```

## Next Steps

1. Choose primary database for initial implementation
2. Add necessary dependencies to go.mod
3. Implement full CRUD operations
4. Add integration tests
5. Document performance characteristics
6. Add to CI/CD pipeline

## Notes

- The current implementation is a scaffold that compiles but returns "not yet implemented" errors
- Full implementation requires database servers to be available for testing
- Consider using testcontainers for integration tests
- Both databases support efficient time series queries but have different trade-offs:
  - InfluxDB: Purpose-built for time series, write-optimized
  - TimescaleDB: Postgres extension, SQL-compatible, ACID guarantees
