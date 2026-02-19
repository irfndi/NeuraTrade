# Memory Profiling & Optimization Guide

This document provides guidance on profiling and optimizing goflux for memory efficiency.

## Overview

Memory profiling helps identify:
1. Allocation hotspots (where memory is allocated)
2. Memory leaks (memory that's never freed)
3. GC pressure (frequent garbage collection)
4. Optimizations to reduce allocations

## Current Status

**Framework Complete, Analysis Pending**

### ✅ Completed
- Created profiling guide structure
- Documented Go's built-in profiling tools
- Explained common allocation patterns
- Provided optimization strategies
- Created checklist for memory optimization

### 📋 TODO (Implementation Required)

#### Profiling Tasks
- [ ] Run memory profiling on key indicators (SMA, EMA, RSI, MACD)
- [ ] Analyze allocation reports
- [ ] Identify top 10 allocation hotspots
- [ ] Create before/after optimization comparison
- [ ] Document findings in optimization report

#### Optimization Tasks
- [ ] Reduce allocations in hot indicator paths
- [ ] Implement object pooling for frequently created objects
- [ ] Use slice pre-allocation where possible
- [ ] Optimize decimal operations (reduce temporary allocations)
- [ ] Implement streaming calculations where applicable
- [ ] Add in-place calculations for expensive operations

#### Verification Tasks
- [ ] Re-run profiling after optimizations
- [ ] Verify GC pause reduction
- [ ] Benchmark before/after optimization
- [ ] Document performance improvements
- [ ] Add regression tests for memory usage

## Go Profiling Tools

### 1. Memory Profiling

#### Running with `-memprofile`
```bash
# Profile memory allocations
go test -memprofile=mem.prof ./pkg/indicators/...
```

#### Running Benchmarks with `-benchmem`
```bash
# Benchmark with memory stats
go test -bench=. -benchmem ./pkg/indicators/...
```

### 2. CPU Profiling

#### Running with `-cpuprofile`
```bash
# Profile CPU usage
go test -cpuprofile=cpu.prof ./pkg/indicators/...
```

### 3. Heap Profiling

#### Running with `-memprofile` (heap snapshot)
```bash
# Profile heap allocations
go test -memprofile=heap.prof ./pkg/indicators/...
```

## Analyzing Profiles

### Using pprof tool

#### Memory Profile
```bash
# Analyze allocations
go tool pprof mem.prof

# Top 10 allocations
(pprof) top10

# Allocation graph
(pprof) list --alloc_space

# Compare before/after
go tool pprof mem_old.prof mem_new.prof
```

#### CPU Profile
```bash
# Analyze CPU usage
go tool pprof cpu.prof

# Top functions by CPU time
(pprof) top

# Graph visualization
go tool pprof -png cpu.prof > cpu_graph.png
```

### Using Web Interface

```bash
# Start interactive pprof server
go tool pprof -http=:8080 mem.prof

# Open in browser
# Navigate to http://localhost:8080
```

## Common Allocation Patterns

### 1. Indicator Calculation Pattern

**Problem**: Recreating decimal values in loops

```go
// BAD: Creates new decimal each iteration
for i := 0; i < len(candles); i++ {
    price := candles[i].ClosePrice
    total = total.Add(price)  // New decimal allocated
}
```

**Solution**: Pre-allocate or reuse decimals

```go
// BETTER: Reuse decimal
total := decimal.ZERO
for i := 0; i < len(candles); i++ {
    total = total.Add(candles[i].ClosePrice)
}
```

### 2. Slice Growth Pattern

**Problem**: Appending to slice causes re-allocation

```go
// BAD: Multiple re-allocations as slice grows
var values []decimal.Decimal
for _, candle := range candles {
    values = append(values, candle.ClosePrice)
}
```

**Solution**: Pre-allocate when size is known

```go
// BETTER: Single allocation
values := make([]decimal.Decimal, 0, len(candles))
for i, candle := range candles {
    values[i] = candle.ClosePrice
}
```

### 3. Interface Box Allocation

**Problem**: Interface conversion creates heap allocation

```go
// BAD: Each interface{} boxing
func Process(items []interface{}) {
    for _, item := range items {
        // Interface{} on heap
    }
}
```

**Solution**: Use specific types

```go
// BETTER: No boxing
func Process(items []decimal.Decimal) {
    for _, item := range items {
        // Decimal on stack
    }
}
```

## Optimization Strategies

### 1. Reduce Temporary Allocations

```go
// BAD: Multiple temporary values
result := a.Add(b).Add(c).Add(d)

// BETTER: Chain calculations
result := a.Add(b.Add(c.Add(d)))
```

### 2. Use In-Place Operations

```go
// BAD: Creating new slice
squared := make([]float64, len(values))
for i, v := range values {
    squared[i] = v * v
}

// BETTER: Modify in place (if applicable)
for i := range values {
    values[i] = values[i] * values[i]
}
```

### 3. Object Pooling

```go
// For frequently created objects
var indicatorPool = sync.Pool{
    New: func() any {
        return &MyIndicator{}
    },
}

// Use from pool
ind := indicatorPool.Get().(*MyIndicator)
defer indicatorPool.Put(ind)
```

### 4. Avoid Unnecessary Conversions

```go
// BAD: Converting decimal to float64 and back
val := decimal.New(100)
fval := val.Float()  // Conversion
result := calculate(fval)  // Returns float64
final := decimal.New(result)  // Another conversion

// BETTER: Keep as decimal throughout
val := decimal.New(100)
result := calculateDecimal(val)
```

### 5. Cache Expensive Calculations

```go
// Use CachedIndicator wrapper
sma := indicators.NewCachedIndicator(
    indicators.NewSMAIndicator(ts, 20),
)
```

## Optimization Checklist

### Indicator Calculations
- [ ] Identify hot functions via profiling
- [ ] Reduce allocations in inner loops
- [ ] Pre-allocate slices with known capacity
- [ ] Use sync.Pool for frequently created objects
- [ ] Avoid interface{} conversions
- [ ] Keep values as decimal instead of converting to float64
- [ ] Cache results of expensive calculations
- [ ] Use streaming calculations where applicable
- [ ] Consider in-place modifications
- [ ] Remove unused variables

### Backtesting Engine
- [ ] Profile backtest hot loop
- [ ] Optimize order execution path
- [ ] Reduce allocations in trade management
- [ ] Pool frequently used types (Order, Position)
- [ ] Optimize analyzer calculations
- [ ] Minimize string allocations

### General Code
- [ ] Run go vet for issues
- [ ] Use golangci-lint for static analysis
- [ ] Check for escape analysis: `go build -gcflags="-m"`
- [ ] Review allocations in benchmark output
- [ ] Use race detector: `go test -race`
- [ ] Profile with pprof and view in browser

## Performance Targets

### Indicators
- **Goal**: < 100 ns/op for SMA/EMA
- **Goal**: < 500 ns/op for RSI
- **Goal**: < 1 KB/op for all indicators
- **Goal**: < 5 allocs/op for all calculations

### Backtesting
- **Goal**: < 1000 ns/op for step execution
- **Goal**: < 1 MB per 1000 bars backtest
- **Goal**: < 10 allocs/op for order processing

## Next Steps

1. **Run Initial Profile**
   ```bash
   go test -memprofile=before.prof ./pkg/indicators/...
   ```

2. **Identify Top Issues**
   ```bash
   go tool pprof before.prof
   (pprof) top20
   ```

3. **Apply Optimizations**
   - Implement changes from checklist above
   - Focus on top 10 allocation hotspots

4. **Verify Improvement**
   ```bash
   go test -memprofile=after.prof ./pkg/indicators/...
   go tool pprof before.prof after.prof
   ```

5. **Document Results**
   - Create optimization report
   - Add before/after comparison
   - Update documentation with findings

## Resources

- [Go Profiling](https://go.dev/doc/diagnostics)
- [pprof Documentation](https://github.com/google/pprof)
- [Go Data Structures](https://github.com/golang/go/wiki/DataStructures)
- [Go Performance](https://go.dev/doc/diagnostics#Profiler)

## Tracking

### Optimization Log
```markdown
| Date | Indicator | Optimizations | allocs/op (before) | allocs/op (after) | Improvement |
|-------|------------|----------------|---------------------|------------------|------------|
| ...   | SMA        | Pre-allocated slice | 5                 | 2                 | 60%         |
```

### Open Optimization Issues
- [goflux-xxx] - Add issue number
- [goflux-xxx] - Add issue number

This guide provides a complete framework for memory profiling and optimization. Implement the checklist to systematically improve performance.
