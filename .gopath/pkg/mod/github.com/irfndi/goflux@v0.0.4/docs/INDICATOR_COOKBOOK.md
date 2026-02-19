# Indicator Cookbook

This cookbook provides practical examples and patterns for using goflux indicators in trading strategies.

## Table of Contents

1. [Trend Indicators](#trend-indicators)
2. [Momentum Indicators](#momentum-indicators)
3. [Volatility Indicators](#volatility-indicators)
4. [Volume Indicators](#volume-indicators)
5. [Indicator Combinations](#indicator-combinations)
6. [Strategy Patterns](#strategy-patterns)
7. [Best Practices](#best-practices)

## Trend Indicators

### Simple Moving Average (SMA)

**Use Case**: Smooth price data to identify trends

**Example**:
```go
import (
	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
)

func ExampleSMA() {
	// Create time series with price data
	ts := series.NewTimeSeries()
	// ... add candles ...

	// Create 20-period SMA
	sma := indicators.NewSMAIndicator(ts, 20)

	// Get SMA at specific index
	value := sma.Calculate(len(ts.Candles) - 1)

	// Use in strategy
	if value.GT(sma.Calculate(len(ts.Candles) - 2)) {
		// Price is trending up
	}
}
```

**Period Selection**:
- **Short-term (5-10)**: Fast response, more noise
- **Medium-term (20-50)**: Balance of speed and smoothness
- **Long-term (100+)**: Very smooth, slower response

### Exponential Moving Average (EMA)

**Use Case**: Weighted moving average giving more importance to recent prices

**Example**:
```go
// Create 14-period EMA
ema := indicators.NewEMAIndicator(sma, 14) // Can use SMA as underlying

value := ema.Calculate(len(ts.Candles) - 1)
```

**Advantages over SMA**:
- Reacts faster to price changes
- Better for short-term signals
- More commonly used in strategies

**Combination Pattern**: SMA/EMA Crossover
```go
// Create both SMAs and EMAs
sma20 := indicators.NewSMAIndicator(ts, 20)
sma50 := indicators.NewSMAIndicator(ts, 50)
ema9 := indicators.NewEMAIndicator(sma20, 9)
ema21 := indicators.NewEMAIndicator(sma20, 21)

// Use builder to create combined indicator
indicator := indicators.NewIndicatorBuilder(ts).
	SMA(20).
	SMA(50).
	Build()
```

## Momentum Indicators

### RSI (Relative Strength Index)

**Use Case**: Identify overbought/oversold conditions

**Example**:
```go
// Create 14-period RSI
rsi := indicators.NewRSIIndicator(ts, 14)

value := rsi.Calculate(len(ts.Candles) - 1)

// Typical usage
if value.LT(decimal.New(30)) {
	// Oversold - potential buy signal
} else if value.GT(decimal.New(70)) {
	// Overbought - potential sell signal
}
```

**Signal Levels**:
- **< 30**: Oversold (buy opportunity)
- **30-70**: Neutral
- **> 70**: Overbought (sell opportunity)

### MACD (Moving Average Convergence Divergence)

**Use Case**: Trend-following momentum indicator

**Example**:
```go
// Create MACD (12, 26, 9)
macd := indicators.NewMACDIndicator(ts, 12, 26, 9)

// Get MACD line, Signal line, and Histogram
macdLine := macd.Calculate(len(ts.Candles) - 1)
signalLine := macd.Calculate(len(ts.Candles) - 1)
histogram := macdLine.Sub(signalLine)

// Signal: MACD line crosses above signal line
if macdLine.GT(signalLine) && previousMacdLine.LTE(previousSignalLine) {
	// Bullish crossover - buy signal
}
```

**Interpretation**:
- **MACD > Signal**: Bullish momentum
- **MACD < Signal**: Bearish momentum
- **Histogram growing**: Increasing momentum
- **Histogram shrinking**: Decreasing momentum

## Volatility Indicators

### ATR (Average True Range)

**Use Case**: Measure volatility and set stop-loss levels

**Example**:
```go
// Create 14-period ATR
atr := indicators.NewATRIndicator(ts, 14)

value := atr.Calculate(len(ts.Candles) - 1)

// Use for stop-loss (2x ATR)
stopLoss := currentPrice.Sub(value.Mul(decimal.New(2)))
```

**ATR Periods**:
- **14 days**: Standard for daily trading
- **20 days**: Less sensitive
- **5 days**: More sensitive to recent volatility

### Bollinger Bands

**Use Case**: Measure volatility and identify overbought/oversold

**Example**:
```go
// Create Bollinger Bands (20 period, 2 std dev)
bb := indicators.NewBollingerBandsIndicator(ts, 20, 2)

middle := bb.Calculate(len(ts.Candles) - 1)  // Middle band (SMA)
upper := bb.CalculateUpper(len(ts.Candles) - 1)   // Upper band
lower := bb.CalculateLower(len(ts.Candles) - 1)   // Lower band

// Squeeze detection (volatility is low)
currentWidth := upper.Sub(lower)
if currentWidth.LT(averageWidth.Mul(decimal.New(0.5))) {
	// Bollinger squeeze - potential breakout coming
}

// Price at upper band - sell signal
if currentPrice.GTE(upper) {
	// Sell
}
// Price at lower band - buy signal
if currentPrice.LTE(lower) {
	// Buy
}
```

## Volume Indicators

### Volume Moving Average

**Use Case**: Confirm trends with volume

**Example**:
```go
// Create volume SMA
volSMA := indicators.NewSMAIndicator(volumeSeries, 20)

currentVol := volSMA.Calculate(len(volumeSeries.Candles) - 1)
averageVol := volSMA.Calculate(len(volumeSeries.Candles) - 20)

// Volume confirmation
if priceUp && currentVol.GT(averageVol) {
	// Strong buy signal
}
```

## Indicator Combinations

### Triple Screen Strategy

**Pattern**: Use multiple indicators to filter trades

```go
// Create indicators
priceTs := series.NewTimeSeries()
VolumeTs := series.NewTimeSeries()
// ... add data ...

sma20 := indicators.NewSMAIndicator(priceTs, 20)
rsi := indicators.NewRSIIndicator(priceTs, 14)
volSMA := indicators.NewSMAIndicator(volumeTs, 20)

// Entry rules: Price > SMA, RSI < 70, Volume > Average
entryRule := trading.And(
	trading.OverIndicatorRule{First: price, Second: sma20},
	trading.UnderIndicatorRule{First: rsi, Second: decimal.New(70)},
	trading.OverIndicatorRule{First: volume, Second: volSMA},
)

// Exit rule: Price < SMA or RSI > 70
exitRule := trading.Or(
	trading.UnderIndicatorRule{First: price, Second: sma20},
	trading.OverIndicatorRule{First: rsi, Second: decimal.New(70)},
)
```

### ADX + DMI Strategy

**Pattern**: Use ADX to identify trending markets before using other indicators

```go
// Create ADX
adx := indicators.NewADXIndicator(ts, 14)

adxValue := adx.Calculate(len(ts.Candles) - 1)

// Only trade when ADX > 25 (strong trend)
if adxValue.GT(decimal.New(25)) {
	// Apply your other strategies
	diPlus := adx.CalculateDIPlus(len(ts.Candles) - 1)
	diMinus := adx.CalculateDIMinus(len(ts.Candles) - 1)

	if diPlus.GT(diMinus) {
		// Uptrend - use buy signals
	} else {
		// Downtrend - use sell signals
	}
}
```

## Strategy Patterns

### Moving Average Crossover

**Concept**: Buy when fast MA crosses above slow MA

```go
fastMA := indicators.NewSMAIndicator(ts, 10)
slowMA := indicators.NewSMAIndicator(ts, 50)

crossoverRule := trading.CrossUpIndicatorRule{
	First:  fastMA,
	Second: slowMA,
}

// Backtest this rule
backtester.AddEntryRule(crossoverRule)
backtester.AddExitRule(trading.CrossDownIndicatorRule{
	First:  fastMA,
	Second: slowMA,
})
```

### RSI Divergence

**Concept**: Price makes new high but RSI doesn't confirm (bearish divergence)

```go
rsi := indicators.NewRSIIndicator(ts, 14)

// Track previous RSI values
prevRSI := rsi.Calculate(len(ts.Candles) - 2)

// Divergence detection
if price.HigherThan(previousPrice) && rsi.LowerThan(prevRSI) {
	// Bearish divergence - potential sell
}
```

### Bollinger Band Breakout

**Concept**: Trade breakouts from Bollinger squeeze

```go
bb := indicators.NewBollingerBandsIndicator(ts, 20, 2)

// Calculate bandwidth
upper := bb.CalculateUpper(len(ts.Candles) - 1)
lower := bb.CalculateLower(len(ts.Candles) - 1)
bandwidth := upper.Sub(lower)

// Breakout when price moves outside bands
breakoutRule := trading.Or(
	trading.OverIndicatorRule{First: price, Second: upper},
	trading.UnderIndicatorRule{First: price, Second: lower},
)
```

## Best Practices

### 1. Indicator Selection

- **Match timeframe to strategy**: Don't use hourly indicators for daily trading
- **Combine indicators**: Use multiple indicators for confirmation
- **Avoid overfitting**: Too many parameters lead to poor out-of-sample performance
- **Backtest thoroughly**: Test on different market conditions

### 2. Signal Generation

- **Define clear rules**: When to enter, when to exit
- **Use trailing stops**: Protect profits with trailing stop-loss
- **Consider transaction costs**: Account for spread and commissions
- **Risk management**: Never risk more than 1-2% per trade

### 3. Indicator Parameters

- **Default to common values**: SMA 20, RSI 14, ATR 14
- **Optimize parameters**: Walk-forward optimization
- **Avoid look-ahead bias**: Don't use future data in current calculation
- **Use appropriate periods**: Shorter for day trading, longer for swing trading

### 4. Performance Considerations

- **Use cached indicators**: Avoid recalculating on every bar
- **Pre-calculate values**: For static indicators like SMA
- **Minimize object creation**: In hot loops and backtesting
- **Batch database writes**: When using database storage

## Common Pitfalls

### 1. Look-ahead Bias

**Problem**: Using future data in current calculation
**Solution**: Ensure indicator only uses historical data up to current index

### 2. Curve Fitting

**Problem**: Optimizing too much on historical data
**Solution**: Use walk-forward analysis and out-of-sample testing

### 3. Insufficient Data

**Problem**: Using indicators before they have enough data
**Solution**: Check index and return appropriate default (ZERO) when not ready

### 4. Parameter Mismatch

**Problem**: Different periods for related indicators
**Solution**: Document and standardize periods in strategy

## Resources

- [GoFlux API Docs](https://pkg.go.dev/github.com/irfndi/goflux)
- [Architecture](ARCHITECTURE.md)
- [Contributing Guide](CONTRIBUTING.md)
- [Technical Analysis Resources](./RESOURCES.md) - to be created
