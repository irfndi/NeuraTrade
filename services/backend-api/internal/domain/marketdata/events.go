package marketdata

import (
	"time"

	"github.com/shopspring/decimal"
)

// Events emitted by the MarketDataCollectorActor.

// MarketTickEvent is published when a new tick is collected.
type MarketTickEvent struct {
	TraceID     string
	Exchange    string
	Symbol      string
	Bid         decimal.Decimal
	Ask         decimal.Decimal
	Last        decimal.Decimal
	Volume      decimal.Decimal
	Timestamp   time.Time
	CollectedAt time.Time
}

// MarketCandleEvent is published when OHLCV data is collected.
type MarketCandleEvent struct {
	TraceID     string
	Exchange    string
	Symbol      string
	Timeframe   string
	Open        decimal.Decimal
	High        decimal.Decimal
	Low         decimal.Decimal
	Close       decimal.Decimal
	Volume      decimal.Decimal
	Timestamp   time.Time
	CollectedAt time.Time
}

// OrderBookMetricsEvent is published when order book metrics are collected.
//
// Unit model (canonical, do not change without coordinating with consumers):
//   - BidAskSpread:    percentage (%). 0.05 == 0.05%, not 5%.
//   - Imbalance1Pct:   ratio in [-1, 1] (despite the "Pct" suffix in the
//                     name; the value is the raw (bid-ask)/(bid+ask)
//                     imbalance, not a percentage).
//   - LiquidityScore:  base-asset notional size, raw units. Number of BTC
//                     for BTC/USDT pairs, number of ETH for ETH/USDT, etc.
//   - MidPrice, BestBid, BestAsk: absolute price in quote currency.
type OrderBookMetricsEvent struct {
	TraceID        string
	Exchange       string
	Symbol         string
	BidAskSpread   decimal.Decimal
	MidPrice       decimal.Decimal
	BestBid        decimal.Decimal
	BestAsk        decimal.Decimal
	Imbalance1Pct  decimal.Decimal
	LiquidityScore decimal.Decimal
	Timestamp      time.Time
	CollectedAt    time.Time
}

// FundingRateEvent is published when funding rate data is collected.
type FundingRateEvent struct {
	TraceID         string
	Exchange        string
	Symbol          string
	Rate            decimal.Decimal
	NextFundingTime time.Time
	Timestamp       time.Time
	CollectedAt     time.Time
}

// CollectorStatusEvent is published when collector status changes.
type CollectorStatusEvent struct {
	TraceID    string
	ExchangeID string
	Status     string // "started", "stopped", "paused", "resumed", "degraded", "recovered"
	Reason     string
	Timestamp  time.Time
}

// CollectorErrorEvent is published when collection errors occur.
type CollectorErrorEvent struct {
	TraceID    string
	ExchangeID string
	Symbol     string
	Error      string
	ErrorCount int
	Timestamp  time.Time
}

// SymbolsUpdatedEvent is published when symbol list is updated.
type SymbolsUpdatedEvent struct {
	TraceID        string
	ExchangeID     string
	AddedSymbols   []string
	RemovedSymbols []string
	TotalSymbols   int
	Timestamp      time.Time
}
