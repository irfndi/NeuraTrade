# GoFlux Architecture

This document describes the high-level architecture of the goflux trading analysis library.

## Overview

GoFlux is a Go-based technical analysis library for trading and backtesting. It provides indicators, strategies, backtesting capabilities, and analysis tools for algorithmic trading.

## Core Components

### 1. Package Structure

```
goflux/
├── pkg/                    # Core library packages
│   ├── analysis/          # Analysis tools and metrics
│   ├── backtest/          # Backtesting engine
│   ├── candlesticks/      # Candlestick pattern detection
│   ├── decimal/           # High-precision decimal arithmetic
│   ├── indicators/        # Technical analysis indicators
│   ├── math/              # Mathematical utilities
│   ├── metrics/           # Performance and risk metrics
│   ├── series/            # Time series data management
│   ├── trading/           # Trading execution and position management
│   ├── database/          # Database integration (scaffold)
│   └── reftest/          # Reference validation tests
├── example/               # Example applications
└── doc/                   # Documentation (to be created)
```

### 2. Data Flow

```
┌─────────────────────────────────────────────────────────────┐
│                     Market Data Input                     │
│                     (CSV, API, Database)                  │
└─────────────────────────┬───────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                   Time Series Layer                     │
│              - Candle storage                         │
│              - Time indexing                          │
│              - Data validation                        │
└─────────────────────────┬───────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                  Indicator Layer                        │
│         - SMA, EMA, RSI, MACD, etc.            │
│         - Modular and composable                     │
│         - Cached calculations                        │
└─────────────────────────┬───────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                  Strategy Layer                         │
│        - Rule-based logic                            │
│        - Signal generation                          │
│        - Position sizing                           │
│        - Risk management                          │
└─────────────────────────┬───────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                Backtesting Layer                       │
│       - Order execution                            │
│       - P&L tracking                              │
│       - Trade management                          │
│       - Performance analysis                      │
└─────────────────────────┬───────────────────────────────┘
                      │
                      ▼
┌─────────────────────────────────────────────────────────────┐
│                  Analysis Layer                        │
│        - Risk metrics                               │
│        - Performance statistics                     │
│        - Benchmarking                              │
└─────────────────────────────────────────────────────────────┘
```

## Key Design Decisions

### 1. Decimal Precision
- **Decision**: Use custom Decimal type instead of float64
- **Rationale**: Avoid floating point arithmetic errors in financial calculations
- **Implementation**: Wraps `big.Float` from Go's math package
- **Trade-off**: Slightly more verbose syntax, but correct financial math

### 2. Time Series Representation
- **Decision**: Separate Candle struct from TimeSeries
- **Rationale**: Clear separation of data points from collection logic
- **Benefits**: Easier to iterate, validate, and transform time series

### 3. Indicator Interface
- **Decision**: All indicators implement common `Indicator` interface
- **Rationale**: Polymorphic composition and strategy building
- **Pattern**: `Calculate(index int) decimal.Decimal`

### 4. Strategy Pattern
- **Decision**: Rule-based strategy system with composable rules
- **Rationale**: Flexible and declarative strategy definition
- **Implementation**: Rules can be combined with `And()`, `Or()`, `Not()`

### 5. Backtesting Architecture
- **Decision**: Event-driven backtesting with step-by-step execution
- **Rationale**: More accurate simulation than vectorized approaches
- **Benefits**: Can track intraday changes, support stop-losses

## Component Details

### Decimal Package
- High-precision arithmetic for financial calculations
- Operations: Add, Sub, Mul, Div, Abs, Round, etc.
- Thread-safe (immutable values)
- Comprehensive test coverage

### Indicators Package
- Modular indicator implementations
- Types: Trend, Momentum, Volatility, Volume
- Built-in caching for performance
- Extensible through Indicator interface

### Trading Package
- Order management (limit, market, stop orders)
- Position tracking (long, short, profit/loss)
- Position sizing strategies (fixed, fractional, Kelly, volatility-based)
- Risk management rules (max loss, drawdown, consecutive losses)

### Backtest Package
- Backtester: Orchestrates strategy execution
- Analyzers: Extract performance metrics
- Portfolio support for multi-asset backtesting
- Trade record for audit trail

### Series Package
- TimeSeries: Core data structure
- Operations: Add, Get, Last, Slice
- Support for OHLCV candles
- Resampling capabilities (daily, weekly, monthly)
- Heikin Ashi transformation

## Performance Considerations

### 1. Indicator Caching
- Indicators cache results to avoid recalculation
- `CachedIndicator` wrapper provides transparent caching
- Trade-off: Memory usage vs. CPU time

### 2. Memory Management
- Use pointers for large structs
- Avoid unnecessary allocations in hot paths
- Pool objects where appropriate

### 3. Concurrent Access
- Use mutexes for shared state
- Design for read-heavy workloads
- Consider channel-based event processing

## Extensibility

### Adding New Indicators
1. Create new struct implementing `Indicator` interface
2. Implement `Calculate(index int) decimal.Decimal` method
3. Add tests in `*_test.go` file
4. Update documentation with examples

### Adding New Strategies
1. Create rules using existing indicators
2. Combine rules with `And()`, `Or()`, `Not()`
3. Register strategy in strategy_registry
4. Add backtest to validate

### Adding New Analyzers
1. Implement `Analyzer` interface
2. Register in backtest analyzer registry
3. Add unit tests

## Database Integration (Planned)

- Generic `Storage` interface for multiple database backends
- InfluxDB support (time-optimized)
- TimescaleDB support (SQL-compatible)
- Schema: Symbol, timestamp, OHLCV data

## Testing Strategy

### Unit Tests
- Each package has `*_test.go` files
- High coverage target: 90%+ (currently 82.5%)
- Mock data fixtures for testing

### Integration Tests
- Backtesting with realistic data
- Strategy validation
- Database integration (when implemented)

### Reference Tests
- TA-Lib parity validation (framework created)
- Cross-library compatibility checks

## Contributing

### Getting Started
1. Review this architecture document
2. Explore codebase by following data flow
3. Start with small contributions (bug fixes, docs)
4. Gradually move to larger features

### Code Style
- Follow Go conventions (gofmt, golint)
- Add comments for exported functions
- Write tests for new code
- Keep functions focused and single-responsibility

### Guidelines
- Maintain backward compatibility
- Add deprecation notices for breaking changes
- Document public API thoroughly
- Performance test critical paths

## Future Improvements

### Short Term
- [ ] Complete database integration implementation
- [ ] Add more indicator examples
- [ ] Improve test coverage to 90%+
- [ ] Add benchmarks for performance tracking

### Medium Term
- [ ] Add streaming indicator calculations
- [ ] Support for multiple timeframes
- [ ] Add real-time data ingestion
- [ ] Optimize memory usage in backtesting

### Long Term
- [ ] Distributed backtesting
- [ ] Machine learning strategy support
- [ ] Multi-asset portfolio optimization
- [ ] Cloud deployment support
