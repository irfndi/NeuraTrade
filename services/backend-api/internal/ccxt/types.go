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

// Service provides high-level CCXT operations.
type Service struct {
	client             CCXTClient
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
	s := &Service{
		client:             NewClient(cfg),
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
	exchangesResp, err := s.client.GetExchanges(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch supported exchanges: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Clear existing exchanges
	s.supportedExchanges = make(map[string]ExchangeInfo)

	// Store exchanges with their info
	for _, exchange := range exchangesResp.Exchanges {
		s.supportedExchanges[exchange.ID] = ExchangeInfo{
			ID:   exchange.ID,
			Name: exchange.Name,
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
	_, err := s.client.HealthCheck(ctx)
	return err == nil
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
	if len(exchanges) == 0 || len(symbols) == 0 {
		return nil, fmt.Errorf("exchanges and symbols cannot be empty")
	}

	// Use the bulk tickers endpoint for efficiency
	req := &TickersRequest{
		Symbols:   symbols,
		Exchanges: exchanges,
	}

	resp, err := s.client.GetTickers(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tickers: %w", err)
	}

	// CRITICAL FIX: Validate response is not empty
	// This helps detect API format mismatches or CCXT service issues
	if len(resp.Tickers) == 0 {
		s.logger.Warn("Empty tickers response from CCXT service",
			"exchanges", exchanges,
			"symbols_count", len(symbols))
	}

	marketData := make([]MarketPriceInterface, 0, len(resp.Tickers))
	for _, tickerData := range resp.Tickers {
		md := &models.MarketPrice{
			ExchangeName: tickerData.Exchange,
			Symbol:       tickerData.Ticker.Symbol,
			Bid:          tickerData.Ticker.Bid,
			BidVolume:    decimal.Zero, // CCXT doesn't provide bid volume in ticker, would need order book
			Ask:          tickerData.Ticker.Ask,
			AskVolume:    decimal.Zero, // CCXT doesn't provide ask volume in ticker, would need order book
			Price:        tickerData.Ticker.Last,
			Volume:       tickerData.Ticker.Volume,
			Timestamp:    tickerData.Ticker.Timestamp.Time(),
		}
		marketData = append(marketData, md)
	}

	return marketData, nil
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
	resp, err := s.client.GetTicker(ctx, exchange, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch ticker for %s:%s: %w", exchange, symbol, err)
	}

	return &models.MarketPrice{
		ExchangeName: resp.Exchange,
		Symbol:       resp.Ticker.Symbol,
		Price:        resp.Ticker.Last,
		Volume:       resp.Ticker.Volume,
		Timestamp:    resp.Ticker.Timestamp.Time(),
	}, nil
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
	resp, err := s.client.GetOrderBook(ctx, exchange, symbol, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch order book for %s on %s: %w", symbol, exchange, err)
	}
	return resp, nil
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
	resp, err := s.client.GetOHLCV(ctx, exchange, symbol, timeframe, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OHLCV for %s on %s: %w", symbol, exchange, err)
	}
	return resp, nil
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
	resp, err := s.client.GetTrades(ctx, exchange, symbol, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch trades for %s on %s: %w", symbol, exchange, err)
	}
	return resp, nil
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
	resp, err := s.client.GetMarkets(ctx, exchange)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch markets for %s: %w", exchange, err)
	}
	return resp, nil
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
	if len(exchanges) == 0 || len(symbols) == 0 {
		return nil, fmt.Errorf("exchanges and symbols cannot be empty")
	}

	// Fetch detailed ticker data with bid/ask prices
	req := &TickersRequest{
		Symbols:   symbols,
		Exchanges: exchanges,
	}

	resp, err := s.client.GetTickers(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tickers for arbitrage calculation: %w", err)
	}

	// Group ticker data by symbol
	symbolData := make(map[string][]TickerData)
	for _, tickerData := range resp.Tickers {
		symbol := tickerData.Ticker.Symbol
		symbolData[symbol] = append(symbolData[symbol], tickerData)
	}

	var opportunities []models.ArbitrageOpportunityResponse

	// Find arbitrage opportunities for each symbol
	for symbol, data := range symbolData {
		if len(data) < 2 {
			continue // Need at least 2 exchanges for arbitrage
		}

		// Find the lowest ask and highest bid
		var lowestAsk, highestBid TickerData
		lowestAskPrice := decimal.NewFromFloat(1e10) // Large initial value
		highestBidPrice := decimal.Zero

		for _, td := range data {
			if td.Ticker.Ask.GreaterThan(decimal.Zero) && td.Ticker.Ask.LessThan(lowestAskPrice) {
				lowestAsk = td
				lowestAskPrice = td.Ticker.Ask
			}
			if td.Ticker.Bid.GreaterThan(highestBidPrice) {
				highestBid = td
				highestBidPrice = td.Ticker.Bid
			}
		}

		// Skip if we don't have valid bid/ask data or same exchange
		if lowestAsk.Exchange == "" || highestBid.Exchange == "" ||
			lowestAsk.Exchange == highestBid.Exchange {
			continue
		}

		// Calculate profit percentage
		if lowestAskPrice.GreaterThan(decimal.Zero) && highestBidPrice.GreaterThan(lowestAskPrice) {
			profitAmount := highestBidPrice.Sub(lowestAskPrice)
			profitPercent := profitAmount.Div(lowestAskPrice).Mul(decimal.NewFromInt(100))

			if profitPercent.GreaterThanOrEqual(minProfitPercent) {
				opportunity := models.ArbitrageOpportunityResponse{
					Symbol:           symbol,
					BuyExchange:      lowestAsk.Exchange,
					SellExchange:     highestBid.Exchange,
					BuyPrice:         lowestAskPrice,
					SellPrice:        highestBidPrice,
					ProfitPercentage: profitPercent,
					DetectedAt:       time.Now(),
					ExpiresAt:        time.Now().Add(5 * time.Minute), // 5-minute window
				}
				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities, nil
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
	return s.client.GetFundingRate(ctx, exchange, symbol)
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
	return s.client.GetFundingRates(ctx, exchange, symbols)
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
	return s.client.GetAllFundingRates(ctx, exchange)
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
	return s.client.Close()
}

// GetServiceURL returns the CCXT service URL for health checks.
//
// Returns:
//
//	string: The service URL.
func (s *Service) GetServiceURL() string {
	if s.client != nil {
		return s.client.BaseURL()
	}
	return ""
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
	return s.client.GetExchangeConfig(ctx)
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

	// Then call the TypeScript service to update the runtime blacklist
	return s.client.AddExchangeToBlacklist(ctx, exchange)
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

	// Then call the TypeScript service to update the runtime blacklist
	return s.client.RemoveExchangeFromBlacklist(ctx, exchange)
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
	return s.client.RefreshExchanges(ctx)
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
	return s.client.AddExchange(ctx, exchange)
}
