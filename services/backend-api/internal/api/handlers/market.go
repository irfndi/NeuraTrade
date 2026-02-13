package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
)

// MarketHandler handles market data API endpoints.
type MarketHandler struct {
	db               DBQuerier
	ccxtService      ccxt.CCXTService
	collectorService *services.CollectorService
	redis            *database.RedisClient
	cacheAnalytics   *services.CacheAnalyticsService
}

// CacheStats tracks cache hit/miss statistics (deprecated - use CacheAnalyticsService).
type CacheStats struct {
	Hits   int64 `json:"hits"`
	Misses int64 `json:"misses"`
}

// NewMarketHandler creates a new instance of MarketHandler.
//
// Parameters:
//
//	db: Database connection.
//	ccxtService: CCXT service.
//	collectorService: Collector service.
//	redis: Redis client.
//	cacheAnalytics: Cache analytics service.
//
// Returns:
//
//	*MarketHandler: Initialized handler.
func NewMarketHandler(db DBQuerier, ccxtService ccxt.CCXTService, collectorService *services.CollectorService, redis *database.RedisClient, cacheAnalytics *services.CacheAnalyticsService) *MarketHandler {
	return &MarketHandler{
		db:               db,
		ccxtService:      ccxtService,
		collectorService: collectorService,
		redis:            redis,
		cacheAnalytics:   cacheAnalytics,
	}
}

// MarketPricesResponse represents the response for market prices.
type MarketPricesResponse struct {
	Data      []MarketPriceData `json:"data"`
	Total     int               `json:"total"`
	Page      int               `json:"page"`
	Limit     int               `json:"limit"`
	Timestamp time.Time         `json:"timestamp"`
}

// MarketPriceData represents a single market price record.
type MarketPriceData struct {
	Exchange    string          `json:"exchange"`
	Symbol      string          `json:"symbol"`
	Price       decimal.Decimal `json:"price"`
	Volume      decimal.Decimal `json:"volume"`
	Timestamp   time.Time       `json:"timestamp"`
	LastUpdated time.Time       `json:"last_updated"`
}

// TickerResponse represents the response for a single ticker.
type TickerResponse struct {
	Exchange  string          `json:"exchange"`
	Symbol    string          `json:"symbol"`
	Price     decimal.Decimal `json:"price"`
	Volume    decimal.Decimal `json:"volume"`
	Timestamp time.Time       `json:"timestamp"`
}

// BulkTickerResponse represents bulk ticker data for an exchange.
type BulkTickerResponse struct {
	Exchange  string           `json:"exchange"`
	Tickers   []TickerResponse `json:"tickers"`
	Timestamp time.Time        `json:"timestamp"`
	Cached    bool             `json:"cached"`
}

// OrderBookResponse represents order book data.
type OrderBookResponse struct {
	Exchange  string      `json:"exchange"`
	Symbol    string      `json:"symbol"`
	Bids      [][]float64 `json:"bids"`
	Asks      [][]float64 `json:"asks"`
	Timestamp time.Time   `json:"timestamp"`
	Cached    bool        `json:"cached"`
}

// CacheMarketPrices caches market prices data in Redis with 10-second TTL.
func (h *MarketHandler) CacheMarketPrices(ctx context.Context, cacheKey string, data MarketPricesResponse) {
	if h.redis == nil {
		return
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal market prices for caching: %v", err)
		return
	}

	ttl := 10 * time.Second
	if err := h.redis.Set(ctx, cacheKey, string(dataJSON), ttl); err != nil {
		log.Printf("Failed to cache market prices: %v", err)
	}
}

// GetCachedMarketPrices retrieves cached market prices from Redis.
func (h *MarketHandler) GetCachedMarketPrices(ctx context.Context, cacheKey string) (*MarketPricesResponse, bool) {
	if h.redis == nil {
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("market_data")
		}
		return nil, false
	}

	cachedData, err := h.redis.Get(ctx, cacheKey)
	if err != nil {
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("market_data")
		}
		return nil, false
	}

	var data MarketPricesResponse
	if err := json.Unmarshal([]byte(cachedData), &data); err != nil {
		log.Printf("Failed to unmarshal cached market prices: %v", err)
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("market_data")
		}
		return nil, false
	}

	if h.cacheAnalytics != nil {
		h.cacheAnalytics.RecordHit("market_data")
	}
	log.Printf("Cache hit for market prices: %s", cacheKey)
	return &data, true
}

// CacheTicker caches single ticker data in Redis with 10-second TTL.
func (h *MarketHandler) CacheTicker(ctx context.Context, cacheKey string, data TickerResponse) {
	if h.redis == nil {
		return
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal ticker for caching: %v", err)
		return
	}

	ttl := 10 * time.Second
	if err := h.redis.Set(ctx, cacheKey, string(dataJSON), ttl); err != nil {
		log.Printf("Failed to cache ticker: %v", err)
	}
}

// GetCachedTicker retrieves cached ticker from Redis.
func (h *MarketHandler) GetCachedTicker(ctx context.Context, cacheKey string) (*TickerResponse, bool) {
	if h.redis == nil {
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("tickers")
		}
		return nil, false
	}

	cachedData, err := h.redis.Get(ctx, cacheKey)
	if err != nil {
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("tickers")
		}
		return nil, false
	}

	var data TickerResponse
	if err := json.Unmarshal([]byte(cachedData), &data); err != nil {
		log.Printf("Failed to unmarshal cached ticker: %v", err)
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("tickers")
		}
		return nil, false
	}

	if h.cacheAnalytics != nil {
		h.cacheAnalytics.RecordHit("tickers")
	}
	log.Printf("Cache hit for ticker: %s", cacheKey)
	return &data, true
}

// CacheBulkTickers caches bulk ticker data in Redis with 10-second TTL.
func (h *MarketHandler) CacheBulkTickers(ctx context.Context, cacheKey string, data BulkTickerResponse) {
	if h.redis == nil {
		return
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal bulk tickers for caching: %v", err)
		return
	}

	ttl := 10 * time.Second
	if err := h.redis.Set(ctx, cacheKey, string(dataJSON), ttl); err != nil {
		log.Printf("Failed to cache bulk tickers: %v", err)
	}
}

// GetCachedBulkTickers retrieves cached bulk ticker data from Redis.
func (h *MarketHandler) GetCachedBulkTickers(ctx context.Context, cacheKey string) (*BulkTickerResponse, bool) {
	if h.redis == nil {
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("bulk_tickers")
		}
		return nil, false
	}

	cachedData, err := h.redis.Get(ctx, cacheKey)
	if err != nil {
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("bulk_tickers")
		}
		return nil, false
	}

	var data BulkTickerResponse
	if err := json.Unmarshal([]byte(cachedData), &data); err != nil {
		log.Printf("Failed to unmarshal cached bulk tickers: %v", err)
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("bulk_tickers")
		}
		return nil, false
	}

	data.Cached = true
	if h.cacheAnalytics != nil {
		h.cacheAnalytics.RecordHit("bulk_tickers")
	}
	log.Printf("Cache hit for bulk tickers: %s", cacheKey)
	return &data, true
}

// CacheOrderBook caches order book data in Redis with 5-second TTL.
func (h *MarketHandler) CacheOrderBook(ctx context.Context, cacheKey string, data OrderBookResponse) {
	if h.redis == nil {
		return
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		log.Printf("Failed to marshal order book for caching: %v", err)
		return
	}

	ttl := 5 * time.Second // Shorter TTL for order books as they change frequently
	if err := h.redis.Set(ctx, cacheKey, string(dataJSON), ttl); err != nil {
		log.Printf("Failed to cache order book: %v", err)
	}
}

// GetCachedOrderBook retrieves cached order book from Redis.
func (h *MarketHandler) GetCachedOrderBook(ctx context.Context, cacheKey string) (*OrderBookResponse, bool) {
	if h.redis == nil {
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("order_books")
		}
		return nil, false
	}

	cachedData, err := h.redis.Get(ctx, cacheKey)
	if err != nil {
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("order_books")
		}
		return nil, false
	}

	var data OrderBookResponse
	if err := json.Unmarshal([]byte(cachedData), &data); err != nil {
		log.Printf("Failed to unmarshal cached order book: %v", err)
		if h.cacheAnalytics != nil {
			h.cacheAnalytics.RecordMiss("order_books")
		}
		return nil, false
	}

	data.Cached = true
	if h.cacheAnalytics != nil {
		h.cacheAnalytics.RecordHit("order_books")
	}
	log.Printf("Cache hit for order book: %s", cacheKey)
	return &data, true
}

// GetMarketPrices retrieves market prices with pagination and filtering.
func (h *MarketHandler) GetMarketPrices(c *gin.Context) {
	// Parse query parameters
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	// Create cache key based on query parameters
	cacheKey := fmt.Sprintf("market_prices:%s:%s:%d:%d", exchange, symbol, page, limit)

	// Try to get cached data first
	if cachedData, found := h.GetCachedMarketPrices(c.Request.Context(), cacheKey); found {
		c.JSON(http.StatusOK, cachedData)
		return
	}

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}

	offset := (page - 1) * limit

	// Build SQL query
	sqlQuery := `
		SELECT e.name as exchange, tp.symbol, md.last_price, md.volume_24h, md.timestamp, md.created_at
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
	`
	args := []interface{}{}
	whereClauses := []string{}

	// Apply filters
	if exchange != "" {
		whereClauses = append(whereClauses, "e.name = $"+strconv.Itoa(len(args)+1))
		args = append(args, exchange)
	}
	if symbol != "" {
		whereClauses = append(whereClauses, "tp.symbol = $"+strconv.Itoa(len(args)+1))
		args = append(args, symbol)
	}

	if len(whereClauses) > 0 {
		sqlQuery += " WHERE " + whereClauses[0]
		for i := 1; i < len(whereClauses); i++ {
			sqlQuery += " AND " + whereClauses[i]
		}
	}

	// Get total count
	countQuery := "SELECT COUNT(*) FROM (" + sqlQuery + ") as count_query"
	var total int64
	if err := h.db.QueryRow(context.Background(), countQuery, args...).Scan(&total); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to count market data"})
		return
	}

	// Add ordering and pagination
	sqlQuery += " ORDER BY md.created_at DESC LIMIT $" + strconv.Itoa(len(args)+1) + " OFFSET $" + strconv.Itoa(len(args)+2) // SAFE: using parameterized query
	args = append(args, limit, offset)

	// Execute query
	rows, err := h.db.Query(context.Background(), sqlQuery, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve market data"})
		return
	}
	defer rows.Close()

	var results []struct {
		Exchange  string          `json:"exchange"`
		Symbol    string          `json:"symbol"`
		Price     decimal.Decimal `json:"price"`
		Volume    decimal.Decimal `json:"volume"`
		Timestamp time.Time       `json:"timestamp"`
		CreatedAt time.Time       `json:"created_at"`
	}

	for rows.Next() {
		var result struct {
			Exchange  string          `json:"exchange"`
			Symbol    string          `json:"symbol"`
			Price     decimal.Decimal `json:"price"`
			Volume    decimal.Decimal `json:"volume"`
			Timestamp time.Time       `json:"timestamp"`
			CreatedAt time.Time       `json:"created_at"`
		}
		if err := rows.Scan(&result.Exchange, &result.Symbol, &result.Price, &result.Volume, &result.Timestamp, &result.CreatedAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to scan market data"})
			return
		}
		results = append(results, result)
	}

	// Convert to response format
	data := make([]MarketPriceData, len(results))
	for i, result := range results {
		data[i] = MarketPriceData{
			Exchange:    result.Exchange,
			Symbol:      result.Symbol,
			Price:       result.Price,
			Volume:      result.Volume,
			Timestamp:   result.Timestamp,
			LastUpdated: result.CreatedAt,
		}
	}

	response := MarketPricesResponse{
		Data:      data,
		Total:     int(total),
		Page:      page,
		Limit:     limit,
		Timestamp: time.Now(),
	}

	// Cache the response for future requests
	h.CacheMarketPrices(c.Request.Context(), cacheKey, response)

	c.JSON(http.StatusOK, response)
}

// GetTicker retrieves the latest ticker data for a specific exchange and symbol.
func (h *MarketHandler) GetTicker(c *gin.Context) {
	exchange := c.Param("exchange")
	symbol := c.Param("symbol")

	if exchange == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Exchange and symbol are required"})
		return
	}

	// Create cache key for ticker
	cacheKey := fmt.Sprintf("ticker:%s:%s", exchange, symbol)

	// Try to get cached ticker first
	if cachedTicker, found := h.GetCachedTicker(c.Request.Context(), cacheKey); found {
		c.JSON(http.StatusOK, cachedTicker)
		return
	}

	// Try to get live data from CCXT service first
	if h.ccxtService.IsHealthy(c.Request.Context()) {
		ticker, err := h.ccxtService.FetchSingleTicker(c.Request.Context(), exchange, symbol)
		if err == nil && ticker != nil {
			response := TickerResponse{
				Exchange:  exchange,
				Symbol:    symbol,
				Price:     decimal.NewFromFloat(ticker.GetPrice()),
				Volume:    decimal.NewFromFloat(ticker.GetVolume()),
				Timestamp: ticker.GetTimestamp(),
			}
			// Cache the live ticker data
			h.CacheTicker(c.Request.Context(), cacheKey, response)
			c.JSON(http.StatusOK, response)
			return
		}
	}

	// Fallback to database data
	if h.db == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Ticker data not found"})
		return
	}

	var result struct {
		Exchange  string          `json:"exchange"`
		Symbol    string          `json:"symbol"`
		Price     decimal.Decimal `json:"price"`
		Volume    decimal.Decimal `json:"volume"`
		Timestamp time.Time       `json:"timestamp"`
	}

	sqlQuery := `
		SELECT e.name as exchange, tp.symbol, md.last_price, md.volume_24h, md.timestamp
		FROM market_data md
		JOIN exchanges e ON md.exchange_id = e.id
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		WHERE e.name = $1 AND tp.symbol = $2
		ORDER BY md.created_at DESC
		LIMIT 1
	`

	err := h.db.QueryRow(context.Background(), sqlQuery, exchange, symbol).Scan(
		&result.Exchange, &result.Symbol, &result.Price, &result.Volume, &result.Timestamp)

	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Ticker data not found"})
		return
	}

	response := TickerResponse{
		Exchange:  result.Exchange,
		Symbol:    result.Symbol,
		Price:     result.Price,
		Volume:    result.Volume,
		Timestamp: result.Timestamp,
	}

	// Cache the database ticker data
	h.CacheTicker(c.Request.Context(), cacheKey, response)

	c.JSON(http.StatusOK, response)
}

// GetOrderBook retrieves order book data for a specific exchange and symbol.
func (h *MarketHandler) GetOrderBook(c *gin.Context) {
	exchange := c.Param("exchange")
	symbol := c.Param("symbol")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	// Bounds check for int32 conversion safety (prevents overflow)
	if limit < 0 {
		limit = 0
	} else if limit > math.MaxInt32 {
		limit = math.MaxInt32
	}

	if exchange == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Exchange and symbol are required"})
		return
	}

	if limit < 1 || limit > 100 {
		limit = 20
	}

	// Create cache key for order book
	cacheKey := fmt.Sprintf("orderbook:%s:%s:%d", exchange, symbol, limit)

	// Try to get cached order book first
	if cachedOrderBook, found := h.GetCachedOrderBook(c.Request.Context(), cacheKey); found {
		c.JSON(http.StatusOK, cachedOrderBook)
		return
	}

	// Check if CCXT service is available
	if !h.ccxtService.IsHealthy(c.Request.Context()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Market data service is currently unavailable"})
		return
	}

	// Fetch order book from CCXT service
	orderBook, err := h.ccxtService.FetchOrderBook(c.Request.Context(), exchange, symbol, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch order book data"})
		return
	}
	if orderBook == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Invalid order book data received"})
		return
	}

	// Convert to our response format and cache
	response := OrderBookResponse{
		Exchange:  exchange,
		Symbol:    symbol,
		Bids:      convertOrderBookEntries(orderBook.OrderBook.Bids),
		Asks:      convertOrderBookEntries(orderBook.OrderBook.Asks),
		Timestamp: time.Now(),
		Cached:    false,
	}

	// Cache the order book data
	h.CacheOrderBook(c.Request.Context(), cacheKey, response)

	c.JSON(http.StatusOK, response)
}

// GetBulkTickers retrieves all tickers for a specific exchange with caching.
func (h *MarketHandler) GetBulkTickers(c *gin.Context) {
	exchange := c.Param("exchange")

	if exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Exchange is required"})
		return
	}

	// Create cache key for bulk tickers
	cacheKey := fmt.Sprintf("bulk_tickers:%s", exchange)

	// Try to get cached bulk tickers first
	if cachedTickers, found := h.GetCachedBulkTickers(c.Request.Context(), cacheKey); found {
		c.JSON(http.StatusOK, cachedTickers)
		return
	}

	// Check if CCXT service is available
	if !h.ccxtService.IsHealthy(c.Request.Context()) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Market data service is currently unavailable"})
		return
	}

	// Fetch all market data for the exchange using bulk operation
	marketData, err := h.ccxtService.FetchMarketData(c.Request.Context(), []string{exchange}, []string{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch bulk ticker data"})
		return
	}

	// Convert to ticker responses
	tickers := make([]TickerResponse, 0, len(marketData))
	for _, ticker := range marketData {
		tickers = append(tickers, TickerResponse{
			Exchange:  ticker.GetExchangeName(),
			Symbol:    ticker.GetSymbol(),
			Price:     decimal.NewFromFloat(ticker.GetPrice()),
			Volume:    decimal.NewFromFloat(ticker.GetVolume()),
			Timestamp: ticker.GetTimestamp(),
		})
	}

	response := BulkTickerResponse{
		Exchange:  exchange,
		Tickers:   tickers,
		Timestamp: time.Now(),
		Cached:    false,
	}

	// Cache the bulk ticker data
	h.CacheBulkTickers(c.Request.Context(), cacheKey, response)

	c.JSON(http.StatusOK, response)
}

// convertOrderBookEntries converts ccxt.OrderBookEntry slice to [][]float64 format
func convertOrderBookEntries(entries []ccxt.OrderBookEntry) [][]float64 {
	result := make([][]float64, len(entries))
	for i, entry := range entries {
		price, _ := entry.Price.Float64()
		amount, _ := entry.Amount.Float64()
		result[i] = []float64{price, amount}
	}
	return result
}

// Note: GetCacheStats and ResetCacheStats methods have been moved to CacheHandler
// to centralize cache analytics functionality

// GetWorkerStatus returns the status of all collection workers.
func (h *MarketHandler) GetWorkerStatus(c *gin.Context) {
	if h.collectorService == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":     "Collector service is not available",
			"timestamp": time.Now(),
		})
		return
	}

	status := h.collectorService.GetWorkerStatus()
	c.JSON(http.StatusOK, gin.H{
		"workers":   status,
		"timestamp": time.Now(),
	})
}
