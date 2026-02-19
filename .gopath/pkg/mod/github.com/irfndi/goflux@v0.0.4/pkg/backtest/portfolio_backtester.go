package backtest

import (
	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
	"github.com/irfndi/goflux/pkg/trading"
)

// MultiAssetBacktester runs backtests across multiple assets simultaneously
type MultiAssetBacktester struct {
	assets    map[string]*series.TimeSeries
	strategy  trading.Strategy
	analyzers *AnalyzerRegistry
}

func NewMultiAssetBacktester(strategy trading.Strategy) *MultiAssetBacktester {
	return &MultiAssetBacktester{
		assets:    make(map[string]*series.TimeSeries),
		strategy:  strategy,
		analyzers: NewAnalyzerRegistry(),
	}
}

func (m *MultiAssetBacktester) AddAsset(symbol string, s *series.TimeSeries) {
	m.assets[symbol] = s
}

// Run performs a backtest across all assets.
// This is a simplified version where each asset is tested independently for now.
// A true portfolio backtester would handle rebalancing and correlation.
func (m *MultiAssetBacktester) Run(config BacktestConfig) map[string]BacktestResult {
	results := make(map[string]BacktestResult)

	for symbol, s := range m.assets {
		bt := NewBacktester(s, m.strategy)
		// Inherit analyzers
		for _, a := range m.analyzers.analyzers {
			bt.AddAnalyzer(a)
		}
		results[symbol] = bt.Run(config)
	}

	return results
}

// PortfolioResult combines results from multiple assets
type PortfolioResult struct {
	AssetResults map[string]BacktestResult
	TotalEquity  decimal.Decimal
	// ... more portfolio-level metrics
}
