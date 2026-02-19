# Reference Validation Tests

This package provides reference validation tests to ensure goflux indicators produce results compatible with TA-Lib.

## Purpose

TA-Lib is the de-facto standard for technical analysis. This package validates that goflux indicators produce similar results to TA-Lib's reference implementations.

## Current Status

**Framework Complete, Partial Implementation**

### ✅ Completed
- Created reference test data structure
- Implemented validation framework with tolerance support
- Added reference test cases for basic scenarios
- Created SMA reference validation tests
- Created EMA reference validation tests
- Created RSI reference validation tests
- Tests compile and run successfully

### 📋 TODO (More Reference Data Needed)

#### Additional Test Cases
- [ ] Add test cases from TA-Lib documentation/examples
- [ ] Add edge cases (flat data, single value, extreme values)
- [ ] Add test cases for:
  - [ ] MACD (12, 26, 9)
  - [ ] Bollinger Bands (20, 2)
  - [ ] Stochastic Oscillator (14, 3, 3)
  - [ ] ATR (14)
  - [ ] ADX (14)
  - [ ] CCI (20)
  - [ ] OBV
  - [ ] Volume indicators

#### Reference Data Sources
Research and extract from:
- [ ] TA-Lib function documentation
- [ ] TA-Lib test files (e.g., `ta_func_tests.c`)
- [ ] Technical analysis textbooks (Kaufman, Ehlers)
- [ ] Academic papers on indicator formulas

#### Tolerance Calibration
- [ ] Determine appropriate tolerances for each indicator
- [ ] Document precision differences between implementations
- [ ] Account for floating point arithmetic differences

## Usage

Running reference validation tests:

```bash
go test ./pkg/reftest/... -v
```

## Test Data Format

Reference test cases follow this format:

```go
ReferenceTestCase{
    Name:       "Descriptive name",
    Data:       []float64{...}, // OHLC close prices
    ExpectedSMA:  105.0,
    ExpectedEMA:  106.36,
    ExpectedRSI:  80.0,
}
```

## Known Differences

Different implementations may produce slightly different results due to:

1. **Initialization Methods**: TA-Lib often uses SMA to initialize EMA
2. **Rounding**: Different precision handling
3. **First Values**: Treatment of insufficient data
4. **Formula Variations**: Minor formula differences (e.g., Wilder's vs original RSI)

Tolerance values should account for these expected differences.

## Adding New Reference Tests

1. Find reference values from TA-Lib docs or examples
2. Create test case in `reference_test.go`
3. Add validation test in corresponding file (e.g., `sma_ema_rsi_test.go`)
4. Document tolerance and expected differences

## References

- [TA-Lib Documentation](https://ta-lib.github.io/ta-lib/)
- [TA-Lib GitHub](https://github.com/TA-Lib/ta-lib)
- Technical Analysis from A to Z - Steven B. Achelis
- Quantitative Technical Analysis - Clifford J. Sherry
