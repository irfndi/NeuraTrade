package handlers

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// Minimal interfaces for testing when pkg/ccxt is not available
type MarketPriceInterface interface {
	GetPrice() float64
	GetVolume() float64
	GetTimestamp() time.Time
	GetExchangeName() string
	GetSymbol() string
	GetBid() float64
	GetAsk() float64
}

type ArbitrageOpportunityInterface interface {
	GetSymbol() string
	GetBuyExchange() string
	GetSellExchange() string
	GetBuyPrice() decimal.Decimal
	GetSellPrice() decimal.Decimal
	GetProfitPercentage() decimal.Decimal
	GetDetectedAt() time.Time
	GetExpiresAt() time.Time
}

// Placeholder types to satisfy the interface
type ExchangeInfo struct{}
type OHLCVResponse struct{}
type TradesResponse struct{}
type MarketsResponse struct{}
type FundingRate struct{}
type FundingArbitrageOpportunity struct{}
type ExchangeConfigResponse struct{}
type ExchangeManagementResponse struct{}

// Minimal CCXTService interface for testing
type CCXTService interface {
	Initialize(ctx context.Context) error
	IsHealthy(ctx context.Context) bool
	Close() error
	GetServiceURL() string
	GetSupportedExchanges() []string
	GetExchangeInfo(exchangeID string) (ExchangeInfo, bool)
	FetchMarketData(ctx context.Context, exchanges []string, symbols []string) ([]MarketPriceInterface, error)
	FetchSingleTicker(ctx context.Context, exchange, symbol string) (MarketPriceInterface, error)
	FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error)
	FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error)
	FetchTrades(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error)
	FetchMarkets(ctx context.Context, exchange string) (*MarketsResponse, error)
	FetchFundingRate(ctx context.Context, exchange, symbol string) (*FundingRate, error)
	FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error)
	FetchAllFundingRates(ctx context.Context, exchange string) ([]FundingRate, error)
	CalculateArbitrageOpportunities(ctx context.Context, exchanges []string, symbols []string, minProfitPercent decimal.Decimal) ([]ArbitrageOpportunityInterface, error)
	CalculateFundingRateArbitrage(ctx context.Context, symbols []string, exchanges []string, minProfit float64) ([]FundingArbitrageOpportunity, error)
	GetExchangeConfig(ctx context.Context) (*ExchangeConfigResponse, error)
	AddExchangeToBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
	RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
	RefreshExchanges(ctx context.Context) (*ExchangeManagementResponse, error)
	AddExchange(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
}
