# Indicator Benchmark Suite

This document describes the benchmark framework for tracking indicator performance over time.

## Purpose

Benchmarks measure the computational performance of indicators to:
1. Establish baseline performance metrics
2. Track performance changes over time
3. Identify slow indicators for optimization
4. Compare performance between different implementations

## Current Status

**Framework Complete, Partial Implementation**

### ✅ Completed
- Created benchmark framework in `pkg/indicators/benchmark_test.go`
- Implemented benchmarks for core indicators:
  - BenchmarkSMA: Simple Moving Average
  - BenchmarkEMA: Exponential Moving Average
  - BenchmarkRSI: Relative Strength Index
- Framework compiles and can be run with Go's benchmark tool

### 📋 TODO (Comprehensive Benchmark Suite Needed)

#### Additional Indicators to Benchmark
- [ ] MACD (Moving Average Convergence Divergence)
- [ ] Bollinger Bands
- [ ] ATR (Average True Range)
- [ ] ADX (Average Directional Index)
- [ ] CCI (Commodity Channel Index)
- [ ] Stochastic Oscillator
- [ ] OBV (On-Balance Volume)
- [ ] All other indicators in pkg/indicators

#### Benchmark Features
- [ ] Multi-size benchmarks (small, medium, large datasets)
- [ ] Parameter variation benchmarks (different periods, multipliers)
- [ ] Comparison benchmarks (SMA vs EMA performance)
- [ ] Memory profiling benchmarks
- [ ] Concurrent calculation benchmarks
- [ ] Real-world scenario benchmarks

## Running Benchmarks

### All Benchmarks
```bash
go test -bench=. -benchmem ./pkg/indicators/...
```

### Specific Benchmark
```bash
# Run only RSI benchmark
go test -bench=BenchmarkRSI -benchmem ./pkg/indicators/...
```

### With CPU Profiling
```bash
go test -bench=. -cpuprofile=cpu.prof ./pkg/indicators/...
go tool pprof cpu.prof
```

## Interpreting Results

### Key Metrics

1. **ns/op**: Nanoseconds per operation (lower is better)
2. **B/op**: Allocated bytes per operation (lower is better)
3. **allocs/op**: Allocations per operation (lower is better)

### Example Output
```
BenchmarkSMA-20     	   5000000	       3.5 ns/op	       2 B/op	       1 allocs/op
BenchmarkEMA-14       	   3000000	       4.2 ns/op	       8 B/op	       2 allocs/op
BenchmarkRSI-14       	   2000000	       6.8 ns/op	      16 B/op	       3 allocs/op
```

### Performance Targets

- **SMA**: Should be fastest (simple calculation)
- **EMA**: Should be slightly slower than SMA (weighted average)
- **RSI**: Should be slower (multiple calculations per period)
- **Complex indicators**: Will be slowest

## Tracking Performance Over Time

### Baseline Establishment
1. Run benchmarks on initial implementation
2. Record results in `docs/BENCHMARKS.md`
3. Note hardware and Go version

### Change Tracking
1. After significant code changes, run benchmarks
2. Compare to baseline
3. Document improvements or regressions
4. Investigate unexpected slowdowns

### Optimization Priorities
1. **Hot paths**: Indicators called in backtesting loop
2. **Slow indicators**: > 100ns/op (adjust based on needs)
3. **High allocations**: > 10 allocs/op (consider pooling)

## Benchmark Best Practices

### 1. Use Realistic Data
- Don't use sequential integers (1, 2, 3...)
- Use realistic price ranges
- Include edge cases (flat data, gaps, jumps)

### 2. Control for Timer Precision
- Use `b.ResetTimer()` before each loop
- Keep loop body minimal
- Avoid allocations in benchmark loop

### 3. Statistical Significance
- Run multiple times (b.N increases automatically)
- Ensure consistent results across runs
- Report min/max as well as average

## Integration with CI

### CI Pipeline Setup
```yaml
benchmark:
  runs-on: ubuntu-latest
  steps:
    - name: Run benchmarks
      run: go test -bench=. -benchmem ./pkg/indicators/...
    - name: Upload results
      uses: benchmark-action/github-action-benchmark@v1
```

### Performance Regression Detection
```bash
# Compare to baseline
go run ./scripts/compare-benchmarks.sh latest baseline.txt

# Fail if performance degraded by > 10%
if [[ $? -ne 0 ]]; then
  exit 1
fi
```

## Resources

- [Go Benchmarking Guide](https://dave.cheney.net/practical/go/presentations/benchmarks.html)
- [pprof documentation](https://pkg.go.dev/runtime/pprof)
- [benchstat tool](https://github.com/uber-go/benchstat)
- [GoFlux Indicator Cookbook](docs/INDICATOR_COOKBOOK.md)

## Next Steps

1. Add benchmarks for all indicators in pkg/indicators
2. Create benchmark result tracking (CSV or database)
3. Set up CI to run benchmarks on every PR
4. Add performance regression detection
5. Create performance optimization tickets for slow indicators
6. Document performance characteristics for users
7. Add memory profiling benchmarks
8. Create performance comparison reports

This framework provides a solid foundation for comprehensive benchmarking. More benchmarks can be added incrementally as needed.
