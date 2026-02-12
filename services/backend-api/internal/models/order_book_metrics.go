package models

import (
	"time"

	"github.com/shopspring/decimal"
)

// OrderBookMetrics contains calculated metrics derived from raw order book data.
// These metrics are used for liquidity assessment, slippage estimation, and risk scoring.
type OrderBookMetrics struct {
	Exchange     string          `json:"exchange"`
	Symbol       string          `json:"symbol"`
	BidAskSpread decimal.Decimal `json:"bid_ask_spread"` // Spread as percentage of mid price
	MidPrice     decimal.Decimal `json:"mid_price"`      // (best_bid + best_ask) / 2
	BestBid      decimal.Decimal `json:"best_bid"`
	BestAsk      decimal.Decimal `json:"best_ask"`

	// Depth metrics - total value within X% of mid price
	BidDepth1Pct decimal.Decimal `json:"bid_depth_1pct"` // Total bid value within 1% of mid
	AskDepth1Pct decimal.Decimal `json:"ask_depth_1pct"` // Total ask value within 1% of mid
	BidDepth2Pct decimal.Decimal `json:"bid_depth_2pct"` // Total bid value within 2% of mid
	AskDepth2Pct decimal.Decimal `json:"ask_depth_2pct"` // Total ask value within 2% of mid

	// Order book imbalance: (bid_depth - ask_depth) / (bid_depth + ask_depth)
	// Positive = more buying pressure, Negative = more selling pressure
	Imbalance1Pct decimal.Decimal `json:"imbalance_1pct"`
	Imbalance2Pct decimal.Decimal `json:"imbalance_2pct"`

	// Slippage estimates for common position sizes
	SlippageEstimates map[string]SlippageEstimate `json:"slippage_estimates"`

	// Quality score (0-100) based on spread, depth, and imbalance
	LiquidityScore decimal.Decimal `json:"liquidity_score"`

	// Raw data reference
	BidLevels int       `json:"bid_levels"` // Number of bid price levels
	AskLevels int       `json:"ask_levels"` // Number of ask price levels
	Timestamp time.Time `json:"timestamp"`
}

// SlippageEstimate represents estimated slippage for a given position size.
type SlippageEstimate struct {
	PositionSize     decimal.Decimal `json:"position_size"`      // USD value
	BuySlippage      decimal.Decimal `json:"buy_slippage"`       // Expected slippage % when buying
	SellSlippage     decimal.Decimal `json:"sell_slippage"`      // Expected slippage % when selling
	AvgBuyPrice      decimal.Decimal `json:"avg_buy_price"`      // Average fill price for buy
	AvgSellPrice     decimal.Decimal `json:"avg_sell_price"`     // Average fill price for sell
	IsFillable       bool            `json:"is_fillable"`        // Whether the size can be filled
	PartialFillDepth decimal.Decimal `json:"partial_fill_depth"` // Max fillable if not fully fillable
}

// OrderBookDepthLevel represents a single price level in the order book.
type OrderBookDepthLevel struct {
	Price    decimal.Decimal `json:"price"`
	Quantity decimal.Decimal `json:"quantity"`
	Value    decimal.Decimal `json:"value"` // price * quantity (USD value)
}

// ExchangeReliabilityMetrics tracks exchange performance for risk scoring.
type ExchangeReliabilityMetrics struct {
	Exchange         string          `json:"exchange"`
	UptimePercent24h decimal.Decimal `json:"uptime_percent_24h"`
	UptimePercent7d  decimal.Decimal `json:"uptime_percent_7d"`
	AvgLatencyMs     int64           `json:"avg_latency_ms"`
	FailureCount24h  int             `json:"failure_count_24h"`
	FailureCount7d   int             `json:"failure_count_7d"`
	LastFailure      *time.Time      `json:"last_failure,omitempty"`
	RiskScore        decimal.Decimal `json:"risk_score"` // 0-20 scale for integration with existing risk model
	LastUpdated      time.Time       `json:"last_updated"`
}

// FundingRateStats contains statistical analysis of funding rates.
type FundingRateStats struct {
	Symbol          string          `json:"symbol"`
	Exchange        string          `json:"exchange"`
	CurrentRate     decimal.Decimal `json:"current_rate"`
	AvgRate7d       decimal.Decimal `json:"avg_rate_7d"`
	AvgRate30d      decimal.Decimal `json:"avg_rate_30d"`
	StdDev7d        decimal.Decimal `json:"std_dev_7d"`
	StdDev30d       decimal.Decimal `json:"std_dev_30d"`
	MinRate7d       decimal.Decimal `json:"min_rate_7d"`
	MaxRate7d       decimal.Decimal `json:"max_rate_7d"`
	TrendDirection  string          `json:"trend_direction"` // "increasing", "decreasing", "stable"
	TrendStrength   decimal.Decimal `json:"trend_strength"`  // 0-1 scale
	VolatilityScore decimal.Decimal `json:"volatility_score"`
	StabilityScore  decimal.Decimal `json:"stability_score"`
	DataPoints      int             `json:"data_points"`
	LastUpdated     time.Time       `json:"last_updated"`
}

// CalculateSpread calculates the bid-ask spread as a percentage.
func (m *OrderBookMetrics) CalculateSpread() decimal.Decimal {
	if m.MidPrice.IsZero() {
		return decimal.Zero
	}
	spread := m.BestAsk.Sub(m.BestBid)
	return spread.Div(m.MidPrice).Mul(decimal.NewFromInt(100))
}

// IsLiquidEnough checks if the order book has sufficient liquidity for a given position size.
func (m *OrderBookMetrics) IsLiquidEnough(positionSize decimal.Decimal, maxSlippagePct decimal.Decimal) bool {
	positionKey := positionSize.StringFixed(0)
	if estimate, exists := m.SlippageEstimates[positionKey]; exists {
		avgSlippage := estimate.BuySlippage.Add(estimate.SellSlippage).Div(decimal.NewFromInt(2))
		return estimate.IsFillable && avgSlippage.LessThanOrEqual(maxSlippagePct)
	}
	// If no estimate exists for this size, check if we have depth
	totalDepth := m.BidDepth1Pct.Add(m.AskDepth1Pct)
	return totalDepth.GreaterThan(positionSize.Mul(decimal.NewFromInt(2)))
}

// GetImbalanceSignal returns a trading signal based on order book imbalance.
// Returns "bullish" if strong buy pressure, "bearish" if strong sell pressure, "neutral" otherwise.
func (m *OrderBookMetrics) GetImbalanceSignal() string {
	threshold := decimal.NewFromFloat(0.2) // 20% imbalance threshold
	if m.Imbalance1Pct.GreaterThan(threshold) {
		return "bullish"
	} else if m.Imbalance1Pct.LessThan(threshold.Neg()) {
		return "bearish"
	}
	return "neutral"
}
