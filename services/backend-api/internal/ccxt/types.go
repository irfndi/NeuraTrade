package ccxt

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/cache"
	"github.com/irfndi/neuratrade/internal/config"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

// Service provides high-level CCXT operations using native Go implementation.
type Service struct {
	nativeClient       *NativeCCXTService
	supportedExchanges map[string]ExchangeInfo
	blacklistCache     cache.BlacklistCache
	mu                 sync.RWMutex
	lastUpdate         time.Time
	logger             *zaplogrus.Logger
}

// NewService creates a new CCXT service instance.
//
// Parameters:
//
//	cfg: CCXT configuration.
//	logger: Logger instance.
//	blacklistCache: Blacklist cache.
//
// Returns:
//
//	*Service: Initialized service.
func NewService(cfg *config.CCXTConfig, logger *zaplogrus.Logger, blacklistCache cache.BlacklistCache) *Service {
	// Use native CCXT implementation (direct exchange API calls)
	nativeClient := NewNativeCCXTService(time.Duration(cfg.Timeout)*time.Second, 3)
	
	s := &Service{
		nativeClient:       nativeClient,
		supportedExchanges: make(map[string]ExchangeInfo),
		blacklistCache:     blacklistCache,
		logger:             logger,
	}

	return s
}

// Initialize initializes the service by fetching supported exchanges and loading blacklist.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	error: Error if initialization fails.
func (s *Service) Initialize(ctx context.Context) error {
	// Initialize native client
	if err := s.nativeClient.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize native CCXT: %w", err)
	}
	
	// Get exchanges from native client
	for _, exchangeID := range s.nativeClient.GetSupportedExchanges() {
		if info, ok := s.nativeClient.GetExchangeInfo(exchangeID); ok {
			s.supportedExchanges[exchangeID] = info
		}
	}

	s.lastUpdate = time.Now()
	s.logger.Info("Initialized CCXT service", "count", len(s.supportedExchanges))

	// Load existing blacklist from database if blacklist cache is available
	if s.blacklistCache != nil {
		if err := s.blacklistCache.LoadFromDatabase(ctx); err != nil {
			s.logger.WithError(err).Warn("Failed to load blacklist from database")
			// Don't fail initialization if blacklist loading fails
		} else {
			s.logger.Info("Successfully loaded blacklist from database")
		}
	}

	return nil
}

// IsHealthy checks if the CCXT service is healthy.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	bool: True if healthy.
func (s *Service) IsHealthy(ctx context.Context) bool {
	return s.nativeClient.IsHealthy(ctx)
}

// GetSupportedExchanges returns a list of supported exchange IDs.
//
// Returns:
//
//	[]string: List of exchange IDs.
func (s *Service) GetSupportedExchanges() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	exchanges := make([]string, 0, len(s.supportedExchanges))
	for id := range s.supportedExchanges {
		exchanges = append(exchanges, id)
	}
	return exchanges
}

// GetExchangeInfo returns information about a specific exchange.
//
// Parameters:
//
//	exchangeID: Exchange identifier.
//
// Returns:
//
//	ExchangeInfo: Exchange information.
//	bool: True if found.
func (s *Service) GetExchangeInfo(exchangeID string) (ExchangeInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, exists := s.supportedExchanges[exchangeID]
	return info, exists
}

// FetchMarketData fetches market data for multiple exchanges and symbols.
//
// Parameters:
//
//	ctx: Context.
//	exchanges: List of exchanges.
//	symbols: List of symbols.
//
// Returns:
//
//	[]MarketPriceInterface: List of market data.
//	error: Error if fetch fails.
func (s *Service) FetchMarketData(ctx context.Context, exchanges []string, symbols []string) ([]MarketPriceInterface, error) {
	// Use native client for direct exchange API calls
	return s.nativeClient.FetchMarketData(ctx, exchanges, symbols)
}

// FetchSingleTicker fetches ticker data for a single exchange and symbol.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//	symbol: Trading pair symbol.
//
// Returns:
//
//	MarketPriceInterface: Ticker data.
//	error: Error if fetch fails.
func (s *Service) FetchSingleTicker(ctx context.Context, exchange, symbol string) (MarketPriceInterface, error) {
	// Use native client for direct exchange API calls
	return s.nativeClient.FetchSingleTicker(ctx, exchange, symbol)
}

// FetchOrderBook fetches order book data for a specific exchange and symbol.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//	symbol: Trading pair symbol.
//	limit: Depth limit.
//
// Returns:
//
//	*OrderBookResponse: Order book data.
//	error: Error if fetch fails.
func (s *Service) FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (*OrderBookResponse, error) {
	return &OrderBookResponse{}, nil // TODO: Implement in native client
}

func (s *Service) CalculateOrderBookMetrics(ctx context.Context, exchange, symbol string, limit int) (*OrderBookMetrics, error) {
	return &OrderBookMetrics{}, nil // TODO: Implement
}

// FetchOHLCV fetches OHLCV data for technical analysis.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//	symbol: Trading pair symbol.
//	timeframe: Candle timeframe.
//	limit: Number of candles.
//
// Returns:
//
//	*OHLCVResponse: OHLCV data.
//	error: Error if fetch fails.
func (s *Service) FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, limit int) (*OHLCVResponse, error) {
	return &OHLCVResponse{}, nil // TODO: Implement
}

// FetchTrades fetches recent trades for a specific exchange and symbol.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//	symbol: Trading pair symbol.
//	limit: Number of trades.
//
// Returns:
//
//	*TradesResponse: Trade history.
//	error: Error if fetch fails.
func (s *Service) FetchTrades(ctx context.Context, exchange, symbol string, limit int) (*TradesResponse, error) {
	return &TradesResponse{}, nil // TODO: Implement
}

// FetchMarkets fetches all available trading pairs for an exchange.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//
// Returns:
//
//	*MarketsResponse: List of markets.
//	error: Error if fetch fails.
func (s *Service) FetchMarkets(ctx context.Context, exchange string) (*MarketsResponse, error) {
	// Use native client for direct exchange API calls
	return s.nativeClient.FetchMarkets(ctx, exchange)
}

// CalculateArbitrageOpportunities identifies arbitrage opportunities from market data.
// This function takes ticker data with bid/ask prices to find arbitrage opportunities.
//
// Parameters:
//
//	ctx: Context.
//	exchanges: List of exchanges to consider.
//	symbols: List of symbols to check.
//	minProfitPercent: Minimum profit threshold.
//
// Returns:
//
//	[]models.ArbitrageOpportunityResponse: List of opportunities.
//	error: Error if calculation fails.
func (s *Service) CalculateArbitrageOpportunities(ctx context.Context, exchanges []string, symbols []string, minProfitPercent decimal.Decimal) ([]models.ArbitrageOpportunityResponse, error) {
	// Use native client for market data
	marketData, err := s.nativeClient.FetchMarketData(ctx, exchanges, symbols)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch market data: %w", err)
	}
	// TODO: Implement arbitrage calculation with native market data
	_ = marketData
	return []models.ArbitrageOpportunityResponse{}, nil
}

// FetchFundingRate fetches funding rate for a specific symbol on an exchange.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//	symbol: Trading pair symbol.
//
// Returns:
//
//	*FundingRate: Funding rate data.
//	error: Error if fetch fails.
func (s *Service) FetchFundingRate(ctx context.Context, exchange, symbol string) (*FundingRate, error) {
	return &FundingRate{}, nil // TODO: Implement
}

// FetchFundingRates fetches funding rates for multiple symbols on an exchange.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//	symbols: List of symbols.
//
// Returns:
//
//	[]FundingRate: List of funding rates.
//	error: Error if fetch fails.
func (s *Service) FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error) {
	return []FundingRate{}, nil // TODO: Implement
}

// FetchAllFundingRates fetches all available funding rates for an exchange.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//
// Returns:
//
//	[]FundingRate: List of funding rates.
//	error: Error if fetch fails.
func (s *Service) FetchAllFundingRates(ctx context.Context, exchange string) ([]FundingRate, error) {
	return []FundingRate{}, nil // TODO: Implement
}

// CalculateFundingRateArbitrage finds funding rate arbitrage opportunities.
// It compares funding rates across exchanges for the same symbols.
//
// Parameters:
//
//	ctx: Context.
//	symbols: List of symbols.
//	exchanges: List of exchanges.
//	minProfit: Minimum profit threshold.
//
// Returns:
//
//	[]FundingArbitrageOpportunity: List of opportunities.
//	error: Error if calculation fails.
func (s *Service) CalculateFundingRateArbitrage(ctx context.Context, symbols []string, exchanges []string, minProfit float64) ([]FundingArbitrageOpportunity, error) {
	var opportunities []FundingArbitrageOpportunity

	// Fetch funding rates from all exchanges for all symbols
	fundingRateMap := make(map[string]map[string]*FundingRate) // exchange -> symbol -> funding rate

	for _, exchange := range exchanges {
		fundingRates, err := s.FetchFundingRates(ctx, exchange, symbols)
		if err != nil {
			continue // Skip this exchange if we can't get funding rates
		}

		if fundingRateMap[exchange] == nil {
			fundingRateMap[exchange] = make(map[string]*FundingRate)
		}

		for i := range fundingRates {
			fundingRateMap[exchange][fundingRates[i].Symbol] = &fundingRates[i]
		}
	}

	// Find arbitrage opportunities for each symbol
	for _, symbol := range symbols {
		// Get all exchanges that have this symbol
		var availableExchanges []string
		for _, exchange := range exchanges {
			if fundingRateMap[exchange] != nil && fundingRateMap[exchange][symbol] != nil {
				availableExchanges = append(availableExchanges, exchange)
			}
		}

		// Need at least 2 exchanges to find arbitrage
		if len(availableExchanges) < 2 {
			continue
		}

		// Compare funding rates between all exchange pairs
		for i := 0; i < len(availableExchanges); i++ {
			for j := i + 1; j < len(availableExchanges); j++ {
				exchange1 := availableExchanges[i]
				exchange2 := availableExchanges[j]

				fr1 := fundingRateMap[exchange1][symbol]
				fr2 := fundingRateMap[exchange2][symbol]

				// Calculate net funding rate (difference)
				netFundingRate := fr2.FundingRate - fr1.FundingRate
				absNetFundingRate := netFundingRate
				if absNetFundingRate < 0 {
					absNetFundingRate = -absNetFundingRate
				}

				// Calculate estimated profits
				estimatedProfit8h := absNetFundingRate * 100  // Convert to percentage
				estimatedProfitDaily := estimatedProfit8h * 3 // 3 funding periods per day

				// Check if profit meets minimum threshold
				if estimatedProfitDaily < minProfit {
					continue
				}

				// Determine which exchange to go long/short
				var longExchange, shortExchange string
				var longFundingRate, shortFundingRate float64
				var longMarkPrice, shortMarkPrice float64

				if fr1.FundingRate < fr2.FundingRate {
					// Go long on exchange1 (pay lower funding), short on exchange2 (receive higher funding)
					longExchange = exchange1
					shortExchange = exchange2
					longFundingRate = fr1.FundingRate
					shortFundingRate = fr2.FundingRate
					longMarkPrice = fr1.MarkPrice
					shortMarkPrice = fr2.MarkPrice
				} else {
					// Go long on exchange2 (pay lower funding), short on exchange1 (receive higher funding)
					longExchange = exchange2
					shortExchange = exchange1
					longFundingRate = fr2.FundingRate
					shortFundingRate = fr1.FundingRate
					longMarkPrice = fr2.MarkPrice
					shortMarkPrice = fr1.MarkPrice
				}

				// Calculate price difference
				priceDifference := shortMarkPrice - longMarkPrice
				priceDifferencePercentage := (priceDifference / longMarkPrice) * 100

				// Calculate risk score based on price difference
				riskScore := 1.0
				if priceDifferencePercentage < 0 {
					priceDifferencePercentage = -priceDifferencePercentage
				}
				if priceDifferencePercentage > 0.5 {
					riskScore = 2.0
				}
				if priceDifferencePercentage > 1.0 {
					riskScore = 3.0
				}
				if priceDifferencePercentage > 2.0 {
					riskScore = 4.0
				}
				if priceDifferencePercentage > 5.0 {
					riskScore = 5.0
				}

				opportunity := FundingArbitrageOpportunity{
					Symbol:                    symbol,
					LongExchange:              longExchange,
					ShortExchange:             shortExchange,
					LongFundingRate:           longFundingRate,
					ShortFundingRate:          shortFundingRate,
					NetFundingRate:            shortFundingRate - longFundingRate,
					EstimatedProfit8h:         estimatedProfit8h,
					EstimatedProfitDaily:      estimatedProfitDaily,
					EstimatedProfitPercentage: estimatedProfitDaily,
					LongMarkPrice:             longMarkPrice,
					ShortMarkPrice:            shortMarkPrice,
					PriceDifference:           priceDifference,
					PriceDifferencePercentage: priceDifferencePercentage,
					RiskScore:                 riskScore,
					Timestamp:                 UnixTimestamp(time.Now()),
				}

				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities, nil
}

// Close closes the CCXT service.
//
// Returns:
//
//	error: Error if closing fails.
func (s *Service) Close() error {
	return s.nativeClient.Close()
}

// GetServiceURL returns the CCXT service URL for health checks.
//
// Returns:
//
//	string: The service URL.
func (s *Service) GetServiceURL() string {
	return "native"
}

// GetExchangeConfig retrieves the current exchange configuration.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	*ExchangeConfigResponse: Exchange configuration.
//	error: Error if retrieval fails.
func (s *Service) GetExchangeConfig(ctx context.Context) (*ExchangeConfigResponse, error) {
	return &ExchangeConfigResponse{}, nil // TODO: Implement
}

// AddExchangeToBlacklist adds an exchange to the blacklist.
// It updates both the database cache and the runtime service.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//
// Returns:
//
//	*ExchangeManagementResponse: Response.
//	error: Error if operation fails.
func (s *Service) AddExchangeToBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	// Add to database-backed cache first (0 duration means no expiration)
	s.blacklistCache.Add(exchange, "Manual blacklist via API", 0)
	return &ExchangeManagementResponse{}, nil // TODO: Implement in native client
}

// RemoveExchangeFromBlacklist removes an exchange from the blacklist.
// It updates both the database cache and the runtime service.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//
// Returns:
//
//	*ExchangeManagementResponse: Response.
//	error: Error if operation fails.
func (s *Service) RemoveExchangeFromBlacklist(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	// Remove from database-backed cache first
	s.blacklistCache.Remove(exchange)
	return &ExchangeManagementResponse{}, nil // TODO: Implement in native client
}

// RefreshExchanges refreshes all non-blacklisted exchanges.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	*ExchangeManagementResponse: Response.
//	error: Error if operation fails.
func (s *Service) RefreshExchanges(ctx context.Context) (*ExchangeManagementResponse, error) {
	return &ExchangeManagementResponse{}, nil // TODO: Implement in native client
}

// AddExchange dynamically adds and initializes a new exchange.
//
// Parameters:
//
//	ctx: Context.
//	exchange: Exchange ID.
//
// Returns:
//
//	*ExchangeManagementResponse: Response.
//	error: Error if operation fails.
func (s *Service) AddExchange(ctx context.Context, exchange string) (*ExchangeManagementResponse, error) {
	return &ExchangeManagementResponse{}, nil // TODO: Implement in native client
}

func (s *Service) FetchBalance(ctx context.Context, exchange string) (*BalanceResponse, error) {
	return s.nativeClient.FetchBalance(ctx, exchange)
}
