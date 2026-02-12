package models

import (
	"time"

	"github.com/shopspring/decimal"
)

// PriceRange represents a price range from multiple exchanges.
// It tracks the minimum, maximum, and average price observed across different markets.
type PriceRange struct {
	Min decimal.Decimal `json:"min"`
	Max decimal.Decimal `json:"max"`
	Avg decimal.Decimal `json:"avg"`
}

// ProfitRange represents profit range based on best/worst price combinations.
// It includes percentage and absolute amount ranges for potential profit.
type ProfitRange struct {
	MinPercentage decimal.Decimal `json:"min_percentage"`
	MaxPercentage decimal.Decimal `json:"max_percentage"`
	MinAmount     decimal.Decimal `json:"min_amount"`
	MaxAmount     decimal.Decimal `json:"max_amount"`
	BaseAmount    decimal.Decimal `json:"base_amount"` // Amount used for calculation (e.g., $20,000)
}

// ArbitrageOpportunity represents a detected arbitrage opportunity between two exchanges.
// It contains details about the prices, exchanges involved, potential profit, and validity.
type ArbitrageOpportunity struct {
	ID               string          `json:"id" db:"id"`
	TradingPairID    int             `json:"trading_pair_id" db:"trading_pair_id"`
	BuyExchangeID    int             `json:"buy_exchange_id" db:"buy_exchange_id"`
	SellExchangeID   int             `json:"sell_exchange_id" db:"sell_exchange_id"`
	BuyPrice         decimal.Decimal `json:"buy_price" db:"buy_price"`
	SellPrice        decimal.Decimal `json:"sell_price" db:"sell_price"`
	ProfitPercentage decimal.Decimal `json:"profit_percentage" db:"profit_percentage"`
	DetectedAt       time.Time       `json:"detected_at" db:"detected_at"`
	ExpiresAt        time.Time       `json:"expires_at" db:"expires_at"`
	TradingPair      *TradingPair    `json:"trading_pair,omitempty"`
	BuyExchange      *Exchange       `json:"buy_exchange,omitempty"`
	SellExchange     *Exchange       `json:"sell_exchange,omitempty"`

	// Enhanced fields for price ranges and multiple exchanges
	BuyPriceRange   *PriceRange     `json:"buy_price_range,omitempty"`
	SellPriceRange  *PriceRange     `json:"sell_price_range,omitempty"`
	ProfitRange     *ProfitRange    `json:"profit_range,omitempty"`
	BuyExchanges    []string        `json:"buy_exchanges,omitempty"`
	SellExchanges   []string        `json:"sell_exchanges,omitempty"`
	MinVolume       decimal.Decimal `json:"min_volume,omitempty"`
	EstimatedVolume decimal.Decimal `json:"estimated_volume,omitempty"`
}

// ArbitrageOpportunityRequest represents request parameters for filtering arbitrage opportunities.
type ArbitrageOpportunityRequest struct {
	MinProfit decimal.Decimal `json:"min_profit" form:"min_profit"`
	Symbol    string          `json:"symbol" form:"symbol"`
	Limit     int             `json:"limit" form:"limit"`
}

// ArbitrageOpportunityResponse represents arbitrage opportunity data formatted for API responses.
type ArbitrageOpportunityResponse struct {
	ID               string          `json:"id"`
	Symbol           string          `json:"symbol"`
	BuyExchange      string          `json:"buy_exchange"`
	SellExchange     string          `json:"sell_exchange"`
	BuyPrice         decimal.Decimal `json:"buy_price"`
	SellPrice        decimal.Decimal `json:"sell_price"`
	ProfitPercentage decimal.Decimal `json:"profit_percentage"`
	DetectedAt       time.Time       `json:"detected_at"`
	ExpiresAt        time.Time       `json:"expires_at"`

	// Enhanced fields for price ranges and multiple exchanges
	BuyPriceRange   *PriceRange     `json:"buy_price_range,omitempty"`
	SellPriceRange  *PriceRange     `json:"sell_price_range,omitempty"`
	ProfitRange     *ProfitRange    `json:"profit_range,omitempty"`
	BuyExchanges    []string        `json:"buy_exchanges,omitempty"`
	SellExchanges   []string        `json:"sell_exchanges,omitempty"`
	MinVolume       decimal.Decimal `json:"min_volume,omitempty"`
	ValidForMinutes int             `json:"valid_for_minutes,omitempty"`
}

// GetSymbol returns the symbol associated with the opportunity.
func (ao *ArbitrageOpportunityResponse) GetSymbol() string {
	return ao.Symbol
}

// GetBuyExchange returns the name of the exchange to buy from.
func (ao *ArbitrageOpportunityResponse) GetBuyExchange() string {
	return ao.BuyExchange
}

// GetSellExchange returns the name of the exchange to sell on.
func (ao *ArbitrageOpportunityResponse) GetSellExchange() string {
	return ao.SellExchange
}

// GetBuyPrice returns the buy price.
func (ao *ArbitrageOpportunityResponse) GetBuyPrice() decimal.Decimal {
	return ao.BuyPrice
}

// GetSellPrice returns the sell price.
func (ao *ArbitrageOpportunityResponse) GetSellPrice() decimal.Decimal {
	return ao.SellPrice
}

// GetProfitPercentage returns the calculated profit percentage.
func (ao *ArbitrageOpportunityResponse) GetProfitPercentage() decimal.Decimal {
	return ao.ProfitPercentage
}

// GetDetectedAt returns the timestamp when the opportunity was detected.
func (ao *ArbitrageOpportunityResponse) GetDetectedAt() time.Time {
	return ao.DetectedAt
}

// GetExpiresAt returns the timestamp when the opportunity expires.
func (ao *ArbitrageOpportunityResponse) GetExpiresAt() time.Time {
	return ao.ExpiresAt
}

// ArbitrageOpportunitiesResponse represents a paginated or listed response of arbitrage opportunities.
type ArbitrageOpportunitiesResponse struct {
	Opportunities []ArbitrageOpportunityResponse `json:"opportunities"`
	Count         int                            `json:"count"`
	Timestamp     time.Time                      `json:"timestamp"`
}

// MultiLegOpportunity represents a detected arbitrage opportunity across 3 or more legs (e.g., Triangular Arbitrage).
type MultiLegOpportunity struct {
	ID               string          `json:"id" db:"id"`
	ExchangeID       int             `json:"exchange_id" db:"exchange_id"`
	ExchangeName     string          `json:"exchange_name" db:"exchange_name"`
	Legs             []ArbitrageLeg  `json:"legs"`
	TotalProfit      decimal.Decimal `json:"total_profit" db:"total_profit"`
	ProfitPercentage decimal.Decimal `json:"profit_percentage" db:"profit_percentage"`
	DetectedAt       time.Time       `json:"detected_at" db:"detected_at"`
	ExpiresAt        time.Time       `json:"expires_at" db:"expires_at"`
}

// ArbitrageLeg represents a single step in a multi-leg trade.
type ArbitrageLeg struct {
	Symbol   string          `json:"symbol"`
	Side     string          `json:"side"` // "buy" or "sell"
	Price    decimal.Decimal `json:"price"`
	Volume   decimal.Decimal `json:"volume"`
	QuoteFee decimal.Decimal `json:"quote_fee"`
}

// MultiLegOpportunitiesResponse is the API response for multiple multi-leg opportunities.
type MultiLegOpportunitiesResponse struct {
	Opportunities []MultiLegOpportunity `json:"opportunities"`
	Count         int                   `json:"count"`
	Timestamp     time.Time             `json:"timestamp"`
}
