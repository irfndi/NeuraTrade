package ccxt

import (
	"context"
	"time"

	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

// MarketPriceInterface defines the interface for market price data.
type MarketPriceInterface interface {
	// GetPrice retrieves the asset price.
	GetPrice() float64
	// GetVolume retrieves the asset volume.
	GetVolume() float64
	// GetTimestamp retrieves the recording time.
	GetTimestamp() time.Time
	// GetExchangeName retrieves the exchange name.
	GetExchangeName() string
	// GetSymbol retrieves the trading pair symbol.
	GetSymbol() string
	// GetBid retrieves the best bid price.
	GetBid() float64
	// GetAsk retrieves the best ask price.
	GetAsk() float64
}

// ArbitrageOpportunityInterface defines the interface for arbitrage opportunity data.
type ArbitrageOpportunityInterface interface {
	// GetSymbol retrieves the trading pair.
	GetSymbol() string
	// GetBuyExchange retrieves the buying exchange.
	GetBuyExchange() string
	// GetSellExchange retrieves the selling exchange.
	GetSellExchange() string
	// GetBuyPrice retrieves the buy price.
	GetBuyPrice() decimal.Decimal
	// GetSellPrice retrieves the sell price.
	GetSellPrice() decimal.Decimal
	// GetProfitPercentage retrieves the profit percentage.
	GetProfitPercentage() decimal.Decimal
	// GetDetectedAt retrieves the detection time.
	GetDetectedAt() time.Time
	// GetExpiresAt retrieves the expiration time.
	GetExpiresAt() time.Time
}

// CCXTService defines the interface for CCXT operations.
type CCXTService interface {
	// Service lifecycle

	// Initialize prepares the service for use.
	Initialize(ctx context.Context) error
	// IsHealthy checks if the service is operational.
	IsHealthy(ctx context.Context) bool
	// Close terminates the service.
	Close() error
	// GetServiceURL returns the base URL of the underlying service.
	GetServiceURL() string

	// Exchange information

	// GetSupportedExchanges returns a list of supported exchange IDs.
	GetSupportedExchanges() []string
	// GetExchangeInfo retrieves detailed info for a specific exchange.
	GetExchangeInfo(exchangeID string) (ExchangeInfo, bool)

	// Exchange management

	// GetExchangeConfig retrieves the dynamic exchange configuration.
	GetExchangeConfig(ctx context.Context) (*ExchangeConfigResponse, error)
	// AddExchangeToBlacklist adds an exchange to the runtime blacklist.
	AddExchangeToBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
	// RemoveExchangeFromBlacklist removes an exchange from the runtime blacklist.
	RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
	// RefreshExchanges reloads exchange configurations.
	RefreshExchanges(ctx context.Context) (*ExchangeManagementResponse, error)
	// AddExchange dynamically adds a new exchange to the system.
	AddExchange(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)

	// Market data operations

	// FetchMarketData retrieves ticker data for multiple exchanges and symbols.
	FetchMarketData(ctx context.Context, exchanges []string, symbols []string) ([]MarketPriceInterface, error)
	// FetchSingleTicker retrieves a single ticker.
	FetchSingleTicker(ctx context.Context, exchange, symbol string) (MarketPriceInterface, error)
	// FetchOrderBook retrieves order book depth data.
	FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error)
	// FetchOHLCV retrieves candlestick data.
	FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error)
	// FetchTrades retrieves recent trade history.
	FetchTrades(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error)
	// FetchMarkets retrieves all tradable markets on an exchange.
	FetchMarkets(ctx context.Context, exchange string) (*MarketsResponse, error)

	// Funding rate operations

	// FetchFundingRate retrieves the current funding rate for a symbol.
	FetchFundingRate(ctx context.Context, exchange, symbol string) (*FundingRate, error)
	// FetchFundingRates retrieves funding rates for multiple symbols.
	FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error)
	// FetchAllFundingRates retrieves all funding rates for an exchange.
	FetchAllFundingRates(ctx context.Context, exchange string) ([]FundingRate, error)

	// Arbitrage operations

	// CalculateArbitrageOpportunities finds price discrepancies between exchanges.
	CalculateArbitrageOpportunities(ctx context.Context, exchanges []string, symbols []string, minProfitPercent decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error)
	// CalculateFundingRateArbitrage finds funding rate arbitrage opportunities.
	CalculateFundingRateArbitrage(ctx context.Context, symbols []string, exchanges []string, minProfit float64) ([]FundingArbitrageOpportunity, error)
}

// CCXTClient defines the interface for low-level CCXT HTTP operations.
type CCXTClient interface {
	// Health and status

	// HealthCheck verifies connectivity to the CCXT microservice.
	HealthCheck(ctx context.Context) (*HealthResponse, error)

	// Exchange operations

	// GetExchanges retrieves the list of supported exchanges from the microservice.
	GetExchanges(ctx context.Context) (*ExchangesResponse, error)

	// Exchange management operations

	// GetExchangeConfig retrieves exchange configuration.
	GetExchangeConfig(ctx context.Context) (*ExchangeConfigResponse, error)
	// AddExchangeToBlacklist blacklists an exchange.
	AddExchangeToBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
	// RemoveExchangeFromBlacklist unblacklists an exchange.
	RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)
	// RefreshExchanges refreshes exchange list.
	RefreshExchanges(ctx context.Context) (*ExchangeManagementResponse, error)
	// AddExchange adds an exchange.
	AddExchange(ctx context.Context, exchange string) (*ExchangeManagementResponse, error)

	// Market data operations

	// GetTicker gets a single ticker.
	GetTicker(ctx context.Context, exchange, symbol string) (*TickerResponse, error)
	// GetTickers gets multiple tickers.
	GetTickers(ctx context.Context, req *TickersRequest) (*TickersResponse, error)
	// GetOrderBook gets order book.
	GetOrderBook(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error)
	// GetTrades gets trades.
	GetTrades(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error)
	// GetOHLCV gets candlesticks.
	GetOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error)
	// GetMarkets gets markets.
	GetMarkets(ctx context.Context, exchange string) (*MarketsResponse, error)

	// Funding rate operations

	// GetFundingRate gets a funding rate.
	GetFundingRate(ctx context.Context, exchange, symbol string) (*FundingRate, error)
	// GetFundingRates gets multiple funding rates.
	GetFundingRates(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error)
	// GetAllFundingRates gets all funding rates.
	GetAllFundingRates(ctx context.Context, exchange string) ([]FundingRate, error)

	// Lifecycle

	// Close cleans up resources.
	Close() error

	// Base URL for service identification

	// BaseURL returns the service URL.
	BaseURL() string
}

// Ensure our implementations satisfy the interfaces
var (
	_ CCXTService = (*Service)(nil)
	_ CCXTClient  = (*Client)(nil)
)
