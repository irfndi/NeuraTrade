// Package marketdata contains pure domain logic for market data collection.
// This package has no external dependencies beyond standard library and decimal.
package marketdata

import (
	"time"

	"github.com/shopspring/decimal"
)

// Tick represents a single price tick from an exchange.
type Tick struct {
	Exchange  string
	Symbol    string
	Bid       decimal.Decimal
	Ask       decimal.Decimal
	Last      decimal.Decimal
	Volume    decimal.Decimal
	Timestamp time.Time
}

// Candle represents an OHLCV candle.
type Candle struct {
	Exchange  string
	Symbol    string
	Timeframe string
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
	Timestamp time.Time
}

// OrderBook represents the order book state.
type OrderBook struct {
	Exchange  string
	Symbol    string
	Bids      []PriceLevel
	Asks      []PriceLevel
	Timestamp time.Time
}

// PriceLevel represents a single price level in an order book.
type PriceLevel struct {
	Price  decimal.Decimal
	Amount decimal.Decimal
}

// FundingRate represents the funding rate for a perpetual contract.
type FundingRate struct {
	Exchange        string
	Symbol          string
	Rate            decimal.Decimal
	NextFundingTime time.Time
	Timestamp       time.Time
}

// SymbolInfo contains metadata about a trading symbol.
type SymbolInfo struct {
	Exchange      string
	Symbol        string
	BaseCurrency  string
	QuoteCurrency string
	Active        bool
	Spot          bool
	Futures       bool
	MinAmount     decimal.Decimal
	MaxAmount     decimal.Decimal
	MinCost       decimal.Decimal
	MaxCost       decimal.Decimal
	TickSize      decimal.Decimal
	LotSize       decimal.Decimal
	CreatedAt     time.Time
	LastUpdated   time.Time
}

// ExchangeStatus represents the operational status of an exchange.
type ExchangeStatus struct {
	ExchangeID string
	Enabled    bool
	Ready      bool
	ErrorCount int
	LastError  string
	LastCheck  time.Time
}

// CollectionStats tracks collection performance metrics.
type CollectionStats struct {
	ExchangeID      string
	SymbolsCount    int
	LastCollection  time.Time
	CollectionCount int64
	ErrorCount      int64
	AvgLatencyMs    float64
}
