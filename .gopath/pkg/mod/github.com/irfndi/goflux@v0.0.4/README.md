# GoFlux

[![Go Reference](https://pkg.go.dev/badge/github.com/irfndi/goflux/pkg.svg)](https://pkg.go.dev/github.com/irfndi/goflux/pkg)
[![Go Report Card](https://goreportcard.com/badge/github.com/irfndi/goflux)](https://goreportcard.com/report/github.com/irfndi/goflux)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

**GoFlux** is a modern technical analysis library for Go, forked from [techan](https://github.com/sdcoffey/techan) by sdcoffey. This project aims to revitalize and expand the library with modern Go best practices, comprehensive testing, and additional technical analysis indicators.

## Features 

- 35+ technical analysis indicators (trend, momentum, volume, moving averages)
- Performance metrics (Sharpe, Sortino, Calmar, CAGR, drawdown, and more)
- Candlestick pattern detection (20+ patterns)
- Rule-based strategy engine (AND/OR/NOT, trailing stops, time-based exits)
- Backtesting engine with trade & equity analytics
- Time series utilities (resampling, Heikin Ashi, Renko)

### Installation

```sh
$ go get github.com/irfndi/goflux@latest
```

### Quickstart

```go
package main

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/irfndi/goflux/pkg"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	series := goflux.NewTimeSeries()

	// fetch this from your preferred exchange
	dataset := [][]string{
		// Timestamp, Open, Close, High, Low, volume
		{"1234567", "1", "2", "3", "5", "6"},
	}

	for _, datum := range dataset {
		start, _ := strconv.ParseInt(datum[0], 10, 64)
		period := goflux.NewTimePeriod(time.Unix(start, 0), time.Hour*24)

		candle := goflux.NewCandle(period)
		open, err := goflux.NewDecimalFromStringWithError(datum[1])
		if err != nil {
			return err
		}
		closePrice, err := goflux.NewDecimalFromStringWithError(datum[2])
		if err != nil {
			return err
		}
		maxPrice, err := goflux.NewDecimalFromStringWithError(datum[3])
		if err != nil {
			return err
		}
		minPrice, err := goflux.NewDecimalFromStringWithError(datum[4])
		if err != nil {
			return err
		}
		volume, err := goflux.NewDecimalFromStringWithError(datum[5])
		if err != nil {
			return err
		}

		candle.OpenPrice = open
		candle.ClosePrice = closePrice
		candle.MaxPrice = maxPrice
		candle.MinPrice = minPrice
		candle.Volume = volume

		series.AddCandle(candle)
	}

	closePrices := goflux.NewClosePriceIndicator(series)
	movingAverage := goflux.NewEMAIndicator(closePrices, 10)

	fmt.Println(movingAverage.Calculate(0).FormattedString(2))
	return nil
}
```

### Creating trading strategies

```go
indicator := goflux.NewClosePriceIndicator(series)

// record trades on this object
record := goflux.NewTradingRecord()

entryConstant := goflux.NewConstantIndicator(30)
exitConstant := goflux.NewConstantIndicator(10)

// Is satisfied when the price ema moves above 30 and the current position is new
entryRule := goflux.And(
	goflux.NewCrossUpIndicatorRule(entryConstant, indicator),
	goflux.PositionNewRule{})
	
// Is satisfied when the price ema moves below 10 and the current position is open
exitRule := goflux.And(
	goflux.NewCrossDownIndicatorRule(indicator, exitConstant),
	goflux.PositionOpenRule{})

strategy := goflux.RuleStrategy{
	UnstablePeriod: 10, // Index at or below which ShouldEnter and ShouldExit return false
	EntryRule:      entryRule,
	ExitRule:       exitRule,
}

strategy.ShouldEnter(0, record) // returns false
```

## Roadmap

See [BEADS.md](BEADS.md) for a detailed roadmap of planned improvements including:

- Modern project structure
- Additional indicators and utilities
- Expanded trading and risk-management rules
- Comprehensive testing suite
- Improved documentation
- CI/CD pipeline with GitHub Actions

## Migration from Techan

GoFlux maintains backward compatibility with the original techan API. Simply update your imports:

```go
// Old
import "github.com/sdcoffey/techan"

// New
import "github.com/irfndi/goflux/pkg"
```

The package name is `goflux`, so you can use it directly:

```go
import "github.com/irfndi/goflux/pkg"

// Usage
series := goflux.NewTimeSeries()
```

## Contributing

Contributions are welcome! Please see [BEADS.md](BEADS.md) for planned improvements and areas where help is needed.

To contribute:

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Submit a pull request

## Acknowledgments

GoFlux builds on the pioneering work of the technical analysis community:

- **[techan](https://github.com/sdcoffey/techan)** (sdcoffey) – the original Go technical analysis library and direct foundation of this project.
- **[ta4j](https://github.com/ta4j/ta4j)** – a mature Java TA framework whose strategy composition and indicator design patterns heavily influenced GoFlux's architecture.
- **[TA‑Lib](https://www.ta-lib.org/)** – the long-standing C/C++ technical analysis library (200+ indicators and candlestick patterns) that serves as a reference standard for indicator behavior and naming.
- **[Pandas TA (Python)](https://github.com/freqtrade/pandas-ta)** – for ideas around indicator catalogs, composition patterns, and declarative strategy definitions.
- **[YATA (Rust)](https://github.com/yata-rs/yata)** – for performance-oriented designs and trait-based indicator patterns.
- **[Backtrader](https://www.backtrader.com/)** – the Python backtesting framework that inspired GoFlux's analyzer and observer patterns.
- **[VectorBT](https://vectorbt.dev/)** – for vectorized operations and portfolio simulation approaches.

GoFlux aims to bring these proven ideas into a modern, idiomatic Go library focused on concurrent, cloud-native trading systems.

## License

GoFlux is released under the MIT license. See [LICENSE](./LICENSE) for details.
