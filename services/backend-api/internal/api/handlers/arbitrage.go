package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// ArbitrageHandler handles arbitrage-related API endpoints.
type ArbitrageHandler struct {
	db                  DBQuerier
	ccxtService         ccxt.CCXTService
	notificationService *services.NotificationService
	redisClient         *redis.Client
}

// ArbitrageOpportunity represents a detected arbitrage opportunity.
type ArbitrageOpportunity struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// BuyExchange is the exchange name to buy from.
	BuyExchange string `json:"buy_exchange"`
	// SellExchange is the exchange name to sell to.
	SellExchange string `json:"sell_exchange"`
	// BuyPrice is the price to buy.
	BuyPrice float64 `json:"buy_price"`
	// SellPrice is the price to sell.
	SellPrice float64 `json:"sell_price"`
	// ProfitPercent is the profit percentage.
	ProfitPercent float64 `json:"profit_percent"`
	// ProfitAmount is the estimated profit amount.
	ProfitAmount float64 `json:"profit_amount"`
	// Volume is the volume available for the trade.
	Volume float64 `json:"volume"`
	// Timestamp is the detection time.
	Timestamp time.Time `json:"timestamp"`
	// OpportunityType classifies the opportunity (e.g., "arbitrage", "technical").
	OpportunityType string `json:"opportunity_type"` // "arbitrage", "technical", "ai_generated"
}

// ArbitrageResponse is the response structure for arbitrage opportunities endpoint.
type ArbitrageResponse struct {
	// Opportunities is the list of opportunities found.
	Opportunities []ArbitrageOpportunity `json:"opportunities"`
	// Count is the number of opportunities.
	Count int `json:"count"`
	// Timestamp is the response generation time.
	Timestamp time.Time `json:"timestamp"`
}

// ArbitrageHistoryItem represents a historical arbitrage record.
type ArbitrageHistoryItem struct {
	// ID is the unique identifier.
	ID int `json:"id"`
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// BuyExchange is the buying exchange.
	BuyExchange string `json:"buy_exchange"`
	// SellExchange is the selling exchange.
	SellExchange string `json:"sell_exchange"`
	// BuyPrice is the historical buy price.
	BuyPrice float64 `json:"buy_price"`
	// SellPrice is the historical sell price.
	SellPrice float64 `json:"sell_price"`
	// ProfitPercent is the historical profit percentage.
	ProfitPercent float64 `json:"profit_percent"`
	// DetectedAt is when the opportunity was recorded.
	DetectedAt time.Time `json:"detected_at"`
}

// ArbitrageHistoryResponse is the response structure for arbitrage history endpoint.
type ArbitrageHistoryResponse struct {
	// History is the list of historical items.
	History []ArbitrageHistoryItem `json:"history"`
	// Count is the number of items in this page.
	Count int `json:"count"`
	// Page is the current page number.
	Page int `json:"page"`
	// Limit is the items per page limit.
	Limit int `json:"limit"`
}

// ArbitrageStats represents arbitrage statistics.
type ArbitrageStats struct {
	// TotalOpportunities is the total count of opportunities in the time window.
	TotalOpportunities int64 `json:"total_opportunities"`
	// AvgProfitPercent is the average profit percentage across all opportunities.
	AvgProfitPercent decimal.Decimal `json:"avg_profit_percent"`
	// MaxProfitPercent is the maximum profit percentage found.
	MaxProfitPercent decimal.Decimal `json:"max_profit_percent"`
	// MinProfitPercent is the minimum profit percentage found.
	MinProfitPercent decimal.Decimal `json:"min_profit_percent"`
	// TotalVolume is the total volume across all opportunities.
	TotalVolume decimal.Decimal `json:"total_volume"`
	// TopSymbols is the top traded symbols.
	TopSymbols []SymbolStats `json:"top_symbols"`
	// TopPairs is the top exchange pairs.
	TopPairs []PairStats `json:"top_pairs"`
	// TimeWindow is the time window for the stats.
	TimeWindow string `json:"time_window"`
}

// SymbolStats represents statistics for a symbol.
type SymbolStats struct {
	// Symbol is the trading pair.
	Symbol string `json:"symbol"`
	// Count is the number of opportunities for this symbol.
	Count int64 `json:"count"`
	// AvgProfit is the average profit for this symbol.
	AvgProfit decimal.Decimal `json:"avg_profit"`
}

// PairStats represents statistics for an exchange pair.
type PairStats struct {
	// BuyExchange is the buying exchange.
	BuyExchange string `json:"buy_exchange"`
	// SellExchange is the selling exchange.
	SellExchange string `json:"sell_exchange"`
	// Count is the number of opportunities for this pair.
	Count int64 `json:"count"`
	// AvgProfit is the average profit for this pair.
	AvgProfit decimal.Decimal `json:"avg_profit"`
}

// NewArbitrageHandler creates a new instance of ArbitrageHandler.
//
// Parameters:
//
//	db: Database connection.
//	ccxtService: CCXT service.
//	notificationService: Notification service.
//	redisClient: Redis client.
//
// Returns:
//
//	*ArbitrageHandler: Initialized handler.
func NewArbitrageHandler(db DBQuerier, ccxtService ccxt.CCXTService, notificationService *services.NotificationService, redisClient *redis.Client) *ArbitrageHandler {
	return &ArbitrageHandler{
		db:                  db,
		ccxtService:         ccxtService,
		notificationService: notificationService,
		redisClient:         redisClient,
	}
}

// GetArbitrageOpportunities finds and returns real-time arbitrage opportunities.
// It supports filtering by profit threshold, symbol, and limit.
//
// Parameters:
//
//	c: Gin context.
func (h *ArbitrageHandler) GetArbitrageOpportunities(c *gin.Context) {
	// Parse query parameters
	minProfitStr := c.DefaultQuery("min_profit", "0.5") // Default 0.5% minimum profit
	maxResults := c.DefaultQuery("limit", "50")
	symbolFilter := c.Query("symbol")

	minProfit, err := strconv.ParseFloat(minProfitStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid min_profit parameter"})
		return
	}

	limit, err := strconv.Atoi(maxResults)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit parameter"})
		return
	}

	log.Printf("Finding arbitrage opportunities with min_profit=%.2f, limit=%d, symbol=%s", minProfit, limit, symbolFilter)

	// Get recent market data from database
	opportunities, err := h.FindArbitrageOpportunities(c.Request.Context(), minProfit, limit, symbolFilter)
	if err != nil {
		log.Printf("Error finding arbitrage opportunities: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to find arbitrage opportunities"})
		return
	}

	log.Printf("Found %d arbitrage opportunities", len(opportunities))

	// Send notifications for high-profit opportunities
	go h.sendArbitrageNotifications(opportunities)

	response := ArbitrageResponse{
		Opportunities: opportunities,
		Count:         len(opportunities),
		Timestamp:     time.Now(),
	}

	c.JSON(http.StatusOK, response)
}

// sendArbitrageNotifications sends notifications for arbitrage opportunities
func (h *ArbitrageHandler) sendArbitrageNotifications(opportunities []ArbitrageOpportunity) {
	if h.notificationService == nil {
		return
	}

	// Filter for high-profit opportunities (> 1%)
	var notifiableOpportunities []services.ArbitrageOpportunity
	for _, opp := range opportunities {
		if opp.ProfitPercent > 1.0 {
			notifiableOpportunities = append(notifiableOpportunities, services.ArbitrageOpportunity{
				Symbol:          opp.Symbol,
				BuyExchange:     opp.BuyExchange,
				SellExchange:    opp.SellExchange,
				BuyPrice:        opp.BuyPrice,
				SellPrice:       opp.SellPrice,
				ProfitPercent:   opp.ProfitPercent,
				ProfitAmount:    opp.ProfitAmount,
				Volume:          opp.Volume,
				Timestamp:       opp.Timestamp,
				OpportunityType: opp.OpportunityType,
			})
		}
	}

	if len(notifiableOpportunities) > 0 {
		ctx := context.Background()
		if err := h.notificationService.NotifyArbitrageOpportunities(ctx, notifiableOpportunities); err != nil {
			log.Printf("Failed to send arbitrage notifications: %v", err)
		}
	}
}

// GetArbitrageHistory returns historical arbitrage opportunities with pagination.
//
// Parameters:
//
//	c: Gin context.
func (h *ArbitrageHandler) GetArbitrageHistory(c *gin.Context) {
	// Parse pagination parameters
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "20")
	symbolFilter := c.Query("symbol")

	page, err := strconv.Atoi(pageStr)
	if err != nil || page < 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid page parameter"})
		return
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit parameter (1-100)"})
		return
	}

	offset := (page - 1) * limit

	// Query historical data from database
	history, err := h.getArbitrageHistory(c.Request.Context(), limit, offset, symbolFilter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get arbitrage history"})
		return
	}

	response := ArbitrageHistoryResponse{
		History: history,
		Count:   len(history),
		Page:    page,
		Limit:   limit,
	}

	c.JSON(http.StatusOK, response)
}

// GetFundingRateArbitrage retrieves funding rate arbitrage opportunities.
// It calculates potential profit from funding rate differences across exchanges.
//
// Parameters:
//
//	c: Gin context.
func (h *ArbitrageHandler) GetFundingRateArbitrage(c *gin.Context) {
	minProfitStr := c.DefaultQuery("min_profit", "0.01") // Default 1% daily
	maxRiskStr := c.DefaultQuery("max_risk", "3.0")      // Default max risk 3.0
	limitStr := c.DefaultQuery("limit", "20")

	minProfit, err := strconv.ParseFloat(minProfitStr, 64)
	if err != nil || minProfit < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid min_profit parameter"})
		return
	}

	maxRisk, err := strconv.ParseFloat(maxRiskStr, 64)
	if err != nil || maxRisk < 1.0 || maxRisk > 5.0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid max_risk parameter (must be between 1.0 and 5.0)"})
		return
	}

	limit, err := strconv.Atoi(limitStr)
	if err != nil || limit < 1 || limit > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid limit parameter (must be between 1 and 100)"})
		return
	}

	// Get symbols and exchanges from query parameters
	symbols := c.QueryArray("symbols")
	exchanges := c.QueryArray("exchanges")

	// If no symbols specified, use default futures symbols
	if len(symbols) == 0 {
		symbols = []string{
			"BTC/USDT:USDT", "ETH/USDT:USDT", "BNB/USDT:USDT",
			"ADA/USDT:USDT", "SOL/USDT:USDT", "DOT/USDT:USDT",
			"MATIC/USDT:USDT", "AVAX/USDT:USDT", "LINK/USDT:USDT",
			"UNI/USDT:USDT", "XRP/USDT:USDT", "DOGE/USDT:USDT",
		}
	}

	// If no exchanges specified, use default futures exchanges
	if len(exchanges) == 0 {
		exchanges = []string{"binance", "bybit", "okx", "bitget"}
	}

	// Calculate funding rate arbitrage opportunities
	opportunities, err := h.ccxtService.CalculateFundingRateArbitrage(c.Request.Context(), symbols, exchanges, minProfit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate funding rate arbitrage"})
		return
	}

	// Filter by risk score and limit results
	var filteredOpportunities []ccxt.FundingArbitrageOpportunity
	for _, opp := range opportunities {
		if opp.RiskScore <= maxRisk {
			filteredOpportunities = append(filteredOpportunities, opp)
			if len(filteredOpportunities) >= limit {
				break
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"opportunities": filteredOpportunities,
		"count":         len(filteredOpportunities),
		"filters": gin.H{
			"min_profit": minProfit,
			"max_risk":   maxRisk,
			"symbols":    symbols,
			"exchanges":  exchanges,
		},
	})
}

// GetFundingRates retrieves current funding rates for specified exchanges and symbols.
// It supports caching to reduce API calls.
//
// Parameters:
//
//	c: Gin context.
func (h *ArbitrageHandler) GetFundingRates(c *gin.Context) {
	exchange := c.Param("exchange")
	if exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Exchange parameter is required"})
		return
	}

	// Get symbols from query parameters
	symbols := c.QueryArray("symbols")

	// Create cache key based on exchange and symbols
	var cacheKey string
	if len(symbols) == 0 {
		cacheKey = fmt.Sprintf("funding_rates:all:%s", exchange)
	} else {
		// Sort symbols for consistent cache key
		sortedSymbols := make([]string, len(symbols))
		copy(sortedSymbols, symbols)
		sort.Strings(sortedSymbols)
		cacheKey = fmt.Sprintf("funding_rates:%s:%s", exchange, strings.Join(sortedSymbols, ","))
	}

	// Check Redis cache first
	if h.redisClient != nil {
		if cachedData, err := h.redisClient.Get(c.Request.Context(), cacheKey).Result(); err == nil {
			var fundingRates []ccxt.FundingRate
			if json.Unmarshal([]byte(cachedData), &fundingRates) == nil {
				c.JSON(http.StatusOK, gin.H{
					"exchange":      exchange,
					"funding_rates": fundingRates,
					"count":         len(fundingRates),
					"cached":        true,
				})
				return
			}
		}
	}

	var fundingRates []ccxt.FundingRate
	var err error

	if len(symbols) == 0 {
		// Get all funding rates for the exchange
		fundingRates, err = h.ccxtService.FetchAllFundingRates(c.Request.Context(), exchange)
	} else {
		// Get funding rates for specific symbols
		fundingRates, err = h.ccxtService.FetchFundingRates(c.Request.Context(), exchange, symbols)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch funding rates"})
		return
	}

	// Cache the result with 1-minute TTL
	if h.redisClient != nil {
		if jsonData, err := json.Marshal(fundingRates); err == nil {
			h.redisClient.Set(c.Request.Context(), cacheKey, jsonData, time.Minute)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"exchange":      exchange,
		"funding_rates": fundingRates,
		"count":         len(fundingRates),
		"cached":        false,
	})
}

// FindArbitrageOpportunities analyzes market data to find arbitrage opportunities.
// It employs multiple strategies: price differences, technical analysis, volatility, and spreads.
//
// Parameters:
//
//	ctx: Context for cancellation.
//	minProfit: Minimum profit percentage to consider.
//	limit: Maximum number of results to return.
//	symbolFilter: Optional symbol to filter by.
//
// Returns:
//
//	[]ArbitrageOpportunity: List of found opportunities.
//	error: Error if analysis fails.
func (h *ArbitrageHandler) FindArbitrageOpportunities(ctx context.Context, minProfit float64, limit int, symbolFilter string) ([]ArbitrageOpportunity, error) {
	// Return empty slice if database is not available (for testing)
	if h.db == nil {
		return []ArbitrageOpportunity{}, nil
	}

	var opportunities []ArbitrageOpportunity

	// Strategy 1: Cross-exchange price differences
	crossExchangeOpps, err := h.findCrossExchangeOpportunities(ctx, minProfit, symbolFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to find cross-exchange opportunities: %w", err)
	}
	opportunities = append(opportunities, crossExchangeOpps...)

	// Strategy 2: Technical analysis based opportunities
	technicalOpps, err := h.findTechnicalAnalysisOpportunities(ctx, minProfit, symbolFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to find technical opportunities: %w", err)
	}
	opportunities = append(opportunities, technicalOpps...)

	// Strategy 3: Volatility and momentum based opportunities
	volatilityOpps, err := h.findVolatilityOpportunities(ctx, minProfit, symbolFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to find volatility opportunities: %w", err)
	}
	opportunities = append(opportunities, volatilityOpps...)

	// Strategy 4: Bid-Ask spread analysis
	spreadOpps, err := h.findSpreadOpportunities(ctx, minProfit, symbolFilter)
	if err != nil {
		return nil, fmt.Errorf("failed to find spread opportunities: %w", err)
	}
	opportunities = append(opportunities, spreadOpps...)

	// Sort by profit percentage (descending)
	sort.Slice(opportunities, func(i, j int) bool {
		return opportunities[i].ProfitPercent > opportunities[j].ProfitPercent
	})

	// Limit results
	if len(opportunities) > limit {
		opportunities = opportunities[:limit]
	}

	return opportunities, nil
}

// findCrossExchangeOpportunities finds arbitrage opportunities across different exchanges
func (h *ArbitrageHandler) findCrossExchangeOpportunities(ctx context.Context, minProfit float64, symbolFilter string) ([]ArbitrageOpportunity, error) {
	// Return empty slice if database is not available
	if h.db == nil {
		return []ArbitrageOpportunity{}, nil
	}

	var opportunities []ArbitrageOpportunity

	// Get recent market data from multiple exchanges
	query := `
		SELECT tp.symbol, e.name as exchange, md.last_price, md.volume_24h, md.timestamp
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		WHERE md.timestamp > NOW() - INTERVAL '5 minutes'
		  AND md.last_price > 0
	`
	args := []interface{}{}

	if symbolFilter != "" {
		query += " AND tp.symbol = $1"
		args = append(args, symbolFilter)
	}

	query += " ORDER BY md.timestamp DESC"

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query market data: %w", err)
	}
	defer rows.Close()

	// Group data by symbol and exchange
	marketData := make(map[string]map[string]struct {
		price     float64
		volume    float64
		timestamp time.Time
	})

	for rows.Next() {
		var symbol, exchange string
		var price, volume float64
		var timestamp time.Time

		if err := rows.Scan(&symbol, &exchange, &price, &volume, &timestamp); err != nil {
			continue
		}

		if marketData[symbol] == nil {
			marketData[symbol] = make(map[string]struct {
				price     float64
				volume    float64
				timestamp time.Time
			})
		}

		// Keep only the most recent data for each exchange
		if existing, exists := marketData[symbol][exchange]; !exists || timestamp.After(existing.timestamp) {
			marketData[symbol][exchange] = struct {
				price     float64
				volume    float64
				timestamp time.Time
			}{price, volume, timestamp}
		}
	}

	// Find cross-exchange arbitrage opportunities
	for symbol, exchanges := range marketData {
		if len(exchanges) >= 2 {
			opportunities = append(opportunities, h.findCrossExchangeArbitrage(symbol, exchanges, minProfit)...)
		}
	}

	return opportunities, nil
}

// findCrossExchangeArbitrage finds arbitrage opportunities between different exchanges
func (h *ArbitrageHandler) findCrossExchangeArbitrage(symbol string, exchanges map[string]struct {
	price     float64
	volume    float64
	timestamp time.Time
}, minProfit float64) []ArbitrageOpportunity {
	var opportunities []ArbitrageOpportunity

	// Find lowest and highest prices across exchanges
	var lowestPrice, highestPrice struct {
		exchange  string
		price     float64
		volume    float64
		timestamp time.Time
	}

	for exchange, data := range exchanges {
		// Find lowest price (best buy opportunity)
		if lowestPrice.price == 0 || data.price < lowestPrice.price {
			lowestPrice.exchange = exchange
			lowestPrice.price = data.price
			lowestPrice.volume = data.volume
			lowestPrice.timestamp = data.timestamp
		}

		// Find highest price (best sell opportunity)
		if highestPrice.price == 0 || data.price > highestPrice.price {
			highestPrice.exchange = exchange
			highestPrice.price = data.price
			highestPrice.volume = data.volume
			highestPrice.timestamp = data.timestamp
		}
	}

	// Calculate profit opportunity using high-precision decimals
	if lowestPrice.price > 0 && highestPrice.price > lowestPrice.price && lowestPrice.exchange != highestPrice.exchange {
		buy := decimal.NewFromFloat(lowestPrice.price)
		sell := decimal.NewFromFloat(highestPrice.price)
		if buy.GreaterThan(decimal.Zero) && sell.GreaterThan(buy) {
			profitPercentDec := sell.Sub(buy).Div(buy).Mul(decimal.NewFromInt(100))
			minProfitDec := decimal.NewFromFloat(minProfit)
			if profitPercentDec.GreaterThanOrEqual(minProfitDec) {
				// Use minimum volume between exchanges
				minVol := math.Min(lowestPrice.volume, highestPrice.volume)
				volDec := decimal.NewFromFloat(minVol)
				profitAmountDec := sell.Sub(buy).Mul(volDec)

				buyF, _ := buy.Float64()
				sellF, _ := sell.Float64()
				profitPercentF, _ := profitPercentDec.Float64()
				profitAmountF, _ := profitAmountDec.Float64()

				opportunity := ArbitrageOpportunity{
					Symbol:          symbol,
					BuyExchange:     lowestPrice.exchange,
					SellExchange:    highestPrice.exchange,
					BuyPrice:        buyF,
					SellPrice:       sellF,
					ProfitPercent:   profitPercentF,
					ProfitAmount:    profitAmountF,
					Volume:          minVol,
					Timestamp:       time.Now(),
					OpportunityType: "arbitrage", // True cross-exchange arbitrage
				}

				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities
}

// findTechnicalAnalysisOpportunities finds opportunities based on technical indicators
func (h *ArbitrageHandler) findTechnicalAnalysisOpportunities(ctx context.Context, minProfit float64, symbolFilter string) ([]ArbitrageOpportunity, error) {
	// Return empty slice if database is not available
	if h.db == nil {
		return []ArbitrageOpportunity{}, nil
	}

	var opportunities []ArbitrageOpportunity

	// Get recent market data for technical analysis
	query := `
		SELECT tp.symbol, e.name as exchange, md.last_price, md.volume_24h, md.timestamp
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		WHERE md.timestamp > NOW() - INTERVAL '30 minutes'
		  AND md.last_price > 0
	`
	args := []interface{}{}

	if symbolFilter != "" {
		query += " AND tp.symbol = $1"
		args = append(args, symbolFilter)
	}

	query += " ORDER BY tp.symbol, e.name, md.timestamp DESC"

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query market data for technical analysis: %w", err)
	}
	defer rows.Close()

	// Group data by symbol and exchange
	marketData := make(map[string]map[string][]struct {
		price     float64
		volume    float64
		timestamp time.Time
	})

	for rows.Next() {
		var symbol, exchange string
		var price, volume float64
		var timestamp time.Time

		if err := rows.Scan(&symbol, &exchange, &price, &volume, &timestamp); err != nil {
			continue
		}

		if marketData[symbol] == nil {
			marketData[symbol] = make(map[string][]struct {
				price     float64
				volume    float64
				timestamp time.Time
			})
		}

		marketData[symbol][exchange] = append(marketData[symbol][exchange], struct {
			price     float64
			volume    float64
			timestamp time.Time
		}{price, volume, timestamp})
	}

	// Analyze each symbol for technical opportunities
	for symbol, exchanges := range marketData {
		for exchange, priceHistory := range exchanges {
			if len(priceHistory) < 5 {
				continue // Need at least 5 data points
			}

			// Calculate moving averages and volatility
			var prices []float64
			for _, data := range priceHistory {
				prices = append(prices, data.price)
			}

			// Simple moving average
			sma := h.calculateSMA(prices, 5)
			currentPrice := prices[0] // Most recent price

			// Check for oversold/overbought conditions using decimals
			smaDec := decimal.NewFromFloat(sma)
			curDec := decimal.NewFromFloat(currentPrice)
			if smaDec.IsZero() {
				continue
			}
			deviationPercentDec := curDec.Sub(smaDec).Div(smaDec).Mul(decimal.NewFromInt(100)).Abs()
			minProfitDec := decimal.NewFromFloat(minProfit)

			if deviationPercentDec.GreaterThanOrEqual(minProfitDec) {
				// Create technical opportunity
				var buyPriceDec, sellPriceDec decimal.Decimal
				var opportunityType string

				if curDec.LessThan(smaDec) {
					// Oversold - buy opportunity
					buyPriceDec = curDec
					sellPriceDec = smaDec
					opportunityType = "technical_oversold"
				} else {
					// Overbought - sell opportunity
					buyPriceDec = smaDec
					sellPriceDec = curDec
					opportunityType = "technical_overbought"
				}

				// Use a conservative fraction of volume
				volDec := decimal.NewFromFloat(priceHistory[0].volume).Mul(decimal.NewFromFloat(0.1))
				profitAmountDec := sellPriceDec.Sub(buyPriceDec).Abs().Mul(volDec)

				buyF, _ := buyPriceDec.Float64()
				sellF, _ := sellPriceDec.Float64()
				profitPercentF, _ := deviationPercentDec.Float64()
				profitAmountF, _ := profitAmountDec.Float64()
				volF, _ := volDec.Float64()

				opportunity := ArbitrageOpportunity{
					Symbol:          symbol,
					BuyExchange:     exchange,
					SellExchange:    exchange + " (technical)",
					BuyPrice:        buyF,
					SellPrice:       sellF,
					ProfitPercent:   profitPercentF,
					ProfitAmount:    profitAmountF,
					Volume:          volF,
					Timestamp:       time.Now(),
					OpportunityType: opportunityType,
				}
				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities, nil
}

// findVolatilityOpportunities finds opportunities based on volatility patterns
func (h *ArbitrageHandler) findVolatilityOpportunities(ctx context.Context, minProfit float64, symbolFilter string) ([]ArbitrageOpportunity, error) {
	// Return empty slice if database is not available
	if h.db == nil {
		return []ArbitrageOpportunity{}, nil
	}

	var opportunities []ArbitrageOpportunity

	// Get market data for volatility analysis
	query := `
		SELECT tp.symbol, e.name as exchange,
		       MIN(md.last_price) as min_price,
		       MAX(md.last_price) as max_price,
		       AVG(md.last_price) as avg_price,
		       AVG(md.volume_24h) as avg_volume
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		WHERE md.timestamp > NOW() - INTERVAL '1 hour'
		  AND md.last_price > 0
	`
	args := []interface{}{}

	if symbolFilter != "" {
		query += " AND tp.symbol = $1"
		args = append(args, symbolFilter)
	}

	query += " GROUP BY tp.symbol, e.name HAVING COUNT(*) >= 10"

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query volatility data: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var symbol, exchange string
		var minPrice, maxPrice, avgPrice, avgVolume float64

		if err := rows.Scan(&symbol, &exchange, &minPrice, &maxPrice, &avgPrice, &avgVolume); err != nil {
			continue
		}

		// Calculate volatility percentage using decimals
		minDec := decimal.NewFromFloat(minPrice)
		maxDec := decimal.NewFromFloat(maxPrice)
		avgDec := decimal.NewFromFloat(avgPrice)
		if avgDec.IsZero() || maxDec.LessThan(minDec) {
			continue
		}
		volatilityDec := maxDec.Sub(minDec).Div(avgDec).Mul(decimal.NewFromInt(100))
		minProfitDec := decimal.NewFromFloat(minProfit)

		if volatilityDec.GreaterThanOrEqual(minProfitDec) {
			volPortion := decimal.NewFromFloat(avgVolume).Mul(decimal.NewFromFloat(0.05))
			profitAmountDec := maxDec.Sub(minDec).Mul(volPortion)

			buyF, _ := minDec.Float64()
			sellF, _ := maxDec.Float64()
			profitPercentF, _ := volatilityDec.Float64()
			profitAmountF, _ := profitAmountDec.Float64()
			volF, _ := volPortion.Float64()

			opportunity := ArbitrageOpportunity{
				Symbol:          symbol,
				BuyExchange:     exchange,
				SellExchange:    exchange + " (volatility)",
				BuyPrice:        buyF,
				SellPrice:       sellF,
				ProfitPercent:   profitPercentF,
				ProfitAmount:    profitAmountF,
				Volume:          volF,
				Timestamp:       time.Now(),
				OpportunityType: "volatility",
			}
			opportunities = append(opportunities, opportunity)
		}
	}

	return opportunities, nil
}

// findSpreadOpportunities finds opportunities based on bid-ask spreads
func (h *ArbitrageHandler) findSpreadOpportunities(ctx context.Context, minProfit float64, symbolFilter string) ([]ArbitrageOpportunity, error) {
	// Return empty slice if database is not available
	if h.db == nil {
		return []ArbitrageOpportunity{}, nil
	}

	var opportunities []ArbitrageOpportunity

	// Get market data with bid/ask prices for spread analysis
	query := `
		SELECT tp.symbol, e.name as exchange, 
			md.bid, md.ask, md.last_price, md.volume_24h, md.timestamp
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		WHERE md.timestamp > NOW() - INTERVAL '5 minutes'
		  AND md.bid > 0 AND md.ask > 0
		  AND md.last_price > 0 AND md.volume_24h > 0
	`
	args := []interface{}{}

	if symbolFilter != "" {
		query += " AND tp.symbol = $1"
		args = append(args, symbolFilter)
	}

	query += " ORDER BY tp.symbol, md.timestamp DESC"

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query spread data: %w", err)
	}
	defer rows.Close()

	// Group data by symbol and exchange
	type ExchangeData struct {
		bid       float64
		ask       float64
		price     float64
		volume    float64
		timestamp time.Time
	}
	marketData := make(map[string]map[string]ExchangeData)

	for rows.Next() {
		var symbol, exchange string
		var bid, ask, price, volume float64
		var timestamp time.Time

		if err := rows.Scan(&symbol, &exchange, &bid, &ask, &price, &volume, &timestamp); err != nil {
			continue
		}

		if marketData[symbol] == nil {
			marketData[symbol] = make(map[string]ExchangeData)
		}

		// Keep only the most recent data for each exchange
		if existing, exists := marketData[symbol][exchange]; !exists || timestamp.After(existing.timestamp) {
			marketData[symbol][exchange] = ExchangeData{
				bid:       bid,
				ask:       ask,
				price:     price,
				volume:    volume,
				timestamp: timestamp,
			}
		}
	}

	// Find spread arbitrage opportunities
	// Buy at lowest ask, sell at highest bid
	for symbol, exchanges := range marketData {
		if len(exchanges) < 2 {
			continue
		}

		// Find exchange with lowest ask (best buy price)
		var lowestAsk struct {
			exchange string
			ask      float64
			volume   float64
		}

		// Find exchange with highest bid (best sell price)
		var highestBid struct {
			exchange string
			bid      float64
			volume   float64
		}

		for exchange, data := range exchanges {
			// Find lowest ask
			if lowestAsk.ask == 0 || data.ask < lowestAsk.ask {
				lowestAsk.exchange = exchange
				lowestAsk.ask = data.ask
				lowestAsk.volume = data.volume
			}

			// Find highest bid
			if highestBid.bid == 0 || data.bid > highestBid.bid {
				highestBid.exchange = exchange
				highestBid.bid = data.bid
				highestBid.volume = data.volume
			}
		}

		// Calculate spread arbitrage opportunity
		// Buy at lowestAsk, sell at highestBid
		if lowestAsk.exchange != "" && highestBid.exchange != "" && lowestAsk.exchange != highestBid.exchange {
			profitAmount := highestBid.bid - lowestAsk.ask
			profitPercent := (profitAmount / lowestAsk.ask) * 100

			// Only include if profit exceeds minimum threshold
			if profitPercent >= minProfit {
				minVol := min(lowestAsk.volume, highestBid.volume)

				opportunity := ArbitrageOpportunity{
					Symbol:          symbol,
					BuyExchange:     lowestAsk.exchange,
					SellExchange:    highestBid.exchange,
					BuyPrice:        lowestAsk.ask,  // Buy at ask price
					SellPrice:       highestBid.bid, // Sell at bid price
					ProfitPercent:   profitPercent,
					ProfitAmount:    profitAmount,
					Volume:          minVol,
					Timestamp:       time.Now(),
					OpportunityType: "spread", // Bid/ask spread arbitrage
				}

				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities, nil
}

// calculateSMA calculates Simple Moving Average
func (h *ArbitrageHandler) calculateSMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return 0
	}

	sum := 0.0
	for i := 0; i < period; i++ {
		sum += prices[i]
	}
	return sum / float64(period)
}

// getArbitrageHistory retrieves historical arbitrage data
func (h *ArbitrageHandler) getArbitrageHistory(ctx context.Context, limit, offset int, symbolFilter string) ([]ArbitrageHistoryItem, error) {
	// Return empty slice if database is not available
	if h.db == nil {
		return []ArbitrageHistoryItem{}, nil
	}

	// For now, we'll simulate historical data since we don't have a dedicated arbitrage_history table
	// In a real implementation, you would store detected opportunities in a separate table
	query := `
		SELECT
			ROW_NUMBER() OVER (ORDER BY md.timestamp DESC) as id,
			tp.symbol,
			e.name as buy_exchange,
			'simulated' as sell_exchange,
			md.last_price as buy_price,
			md.last_price as sell_price,
			0.0 as profit_percent,
			md.timestamp as detected_at
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		WHERE md.timestamp > NOW() - INTERVAL '24 hours'
		  AND md.last_price > 0
	`
	args := []interface{}{}

	if symbolFilter != "" {
		query += " AND tp.symbol = $1"
		args = append(args, symbolFilter)
	}

	query += " ORDER BY md.timestamp DESC LIMIT $" + strconv.Itoa(len(args)+1) + " OFFSET $" + strconv.Itoa(len(args)+2) // SAFE: using parameterized query
	args = append(args, limit, offset)

	rows, err := h.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query arbitrage history: %w", err)
	}
	defer rows.Close()

	var history []ArbitrageHistoryItem
	for rows.Next() {
		var item ArbitrageHistoryItem
		if err := rows.Scan(
			&item.ID,
			&item.Symbol,
			&item.BuyExchange,
			&item.SellExchange,
			&item.BuyPrice,
			&item.SellPrice,
			&item.ProfitPercent,
			&item.DetectedAt,
		); err != nil {
			continue
		}
		history = append(history, item)
	}

	return history, nil
}

// GetArbitrageStats returns arbitrage statistics for a given time window.
func (h *ArbitrageHandler) GetArbitrageStats(c *gin.Context) {
	timeWindow := c.DefaultQuery("window", "24h")

	var interval string
	switch timeWindow {
	case "1h":
		interval = "1 hour"
	case "6h":
		interval = "6 hours"
	case "24h":
		interval = "24 hours"
	case "7d":
		interval = "7 days"
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid time window (use: 1h, 6h, 24h, 7d)"})
		return
	}

	stats, err := h.calculateArbitrageStats(c.Request.Context(), interval)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to calculate arbitrage statistics"})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// calculateArbitrageStats calculates arbitrage statistics from the database.
func (h *ArbitrageHandler) calculateArbitrageStats(ctx context.Context, interval string) (*ArbitrageStats, error) {
	if h.db == nil {
		return &ArbitrageStats{
			TimeWindow: interval,
		}, nil
	}

	stats := &ArbitrageStats{
		TimeWindow: interval,
	}

	query := `
		SELECT
			COUNT(*) as total_opportunities,
			COALESCE(AVG(profit_percent), 0) as avg_profit,
			COALESCE(MAX(profit_percent), 0) as max_profit,
			COALESCE(MIN(profit_percent), 0) as min_profit,
			COALESCE(SUM(volume), 0) as total_volume
		FROM arbitrage_opportunities
		WHERE detected_at > NOW() - INTERVAL $1
	`

	var totalOpp int64
	var avgProfit, maxProfit, minProfit, totalVol float64
	err := h.db.QueryRow(ctx, query, interval).Scan(&totalOpp, &avgProfit, &maxProfit, &minProfit, &totalVol)
	if err != nil {
		return nil, fmt.Errorf("failed to query arbitrage stats: %w", err)
	}

	stats.TotalOpportunities = totalOpp
	stats.AvgProfitPercent = decimal.NewFromFloat(avgProfit)
	stats.MaxProfitPercent = decimal.NewFromFloat(maxProfit)
	stats.MinProfitPercent = decimal.NewFromFloat(minProfit)
	stats.TotalVolume = decimal.NewFromFloat(totalVol)

	symbolQuery := `
		SELECT symbol, COUNT(*) as count, COALESCE(AVG(profit_percent), 0) as avg_profit
		FROM arbitrage_opportunities
		WHERE detected_at > NOW() - INTERVAL $1
		GROUP BY symbol
		ORDER BY count DESC
		LIMIT 5
	`
	rows, err := h.db.Query(ctx, symbolQuery, interval)
	if err != nil {
		return stats, fmt.Errorf("failed to query symbol stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var sym SymbolStats
		var avgProf float64
		if err := rows.Scan(&sym.Symbol, &sym.Count, &avgProf); err == nil {
			sym.AvgProfit = decimal.NewFromFloat(avgProf)
			stats.TopSymbols = append(stats.TopSymbols, sym)
		}
	}

	pairQuery := `
		SELECT buy_exchange, sell_exchange, COUNT(*) as count, COALESCE(AVG(profit_percent), 0) as avg_profit
		FROM arbitrage_opportunities
		WHERE detected_at > NOW() - INTERVAL $1
		GROUP BY buy_exchange, sell_exchange
		ORDER BY count DESC
		LIMIT 5
	`
	rows, err = h.db.Query(ctx, pairQuery, interval)
	if err != nil {
		return stats, fmt.Errorf("failed to query pair stats: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pair PairStats
		var avgProf float64
		if err := rows.Scan(&pair.BuyExchange, &pair.SellExchange, &pair.Count, &avgProf); err == nil {
			pair.AvgProfit = decimal.NewFromFloat(avgProf)
			stats.TopPairs = append(stats.TopPairs, pair)
		}
	}

	return stats, nil
}
