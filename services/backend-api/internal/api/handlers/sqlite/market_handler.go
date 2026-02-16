package sqlite

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

// MarketHandler handles market data API endpoints for SQLite mode.
// It proxies requests to the CCXT service for market data.
type MarketHandler struct {
	db             *database.SQLiteDB
	ccxtServiceURL string
	httpClient     *http.Client
}

// NewMarketHandler creates a new SQLite market handler.
//
// Parameters:
//   - db: SQLite database connection (can be nil for HTTP-only mode)
//   - ccxtServiceURL: The URL of the CCXT service (e.g., "http://localhost:3001")
//
// Returns:
//   - *MarketHandler: The initialized handler.
func NewMarketHandler(db *database.SQLiteDB, ccxtServiceURL string) *MarketHandler {
	// Ensure URL doesn't have trailing slash
	url := strings.TrimSuffix(ccxtServiceURL, "/")

	return &MarketHandler{
		db:             db,
		ccxtServiceURL: url,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ================== Request/Response Structs ==================

// MarketPriceData represents a single market price record.
type MarketPriceData struct {
	Exchange    string          `json:"exchange"`
	Symbol      string          `json:"symbol"`
	Price       decimal.Decimal `json:"price"`
	Volume      decimal.Decimal `json:"volume"`
	Timestamp   time.Time       `json:"timestamp"`
	LastUpdated time.Time       `json:"last_updated"`
}

// MarketPricesResponse represents the response for market prices.
type MarketPricesResponse struct {
	Data      []MarketPriceData `json:"data"`
	Total     int               `json:"total"`
	Page      int               `json:"page"`
	Limit     int               `json:"limit"`
	Timestamp time.Time         `json:"timestamp"`
}

// TickerResponse represents the response for a single ticker.
type TickerResponse struct {
	Exchange  string          `json:"exchange"`
	Symbol    string          `json:"symbol"`
	Price     decimal.Decimal `json:"price"`
	Volume    decimal.Decimal `json:"volume"`
	High      decimal.Decimal `json:"high,omitempty"`
	Low       decimal.Decimal `json:"low,omitempty"`
	Bid       decimal.Decimal `json:"bid,omitempty"`
	Ask       decimal.Decimal `json:"ask,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// BulkTickerResponse represents bulk ticker data for an exchange.
type BulkTickerResponse struct {
	Exchange  string           `json:"exchange"`
	Tickers   []TickerResponse `json:"tickers"`
	Count     int              `json:"count"`
	Timestamp time.Time        `json:"timestamp"`
}

// OrderBookEntry represents a single order book entry.
type OrderBookEntry struct {
	Price  decimal.Decimal `json:"price"`
	Amount decimal.Decimal `json:"amount"`
}

// OrderBookResponse represents order book data.
type OrderBookResponse struct {
	Exchange  string           `json:"exchange"`
	Symbol    string           `json:"symbol"`
	Bids      []OrderBookEntry `json:"bids"`
	Asks      []OrderBookEntry `json:"asks"`
	Timestamp time.Time        `json:"timestamp"`
}

// OrderBookResponseAPI represents the API response format for order book.
type OrderBookResponseAPI struct {
	Exchange  string      `json:"exchange"`
	Symbol    string      `json:"symbol"`
	Bids      [][]float64 `json:"bids"`
	Asks      [][]float64 `json:"asks"`
	Timestamp time.Time   `json:"timestamp"`
}

// WorkerStatus represents the status of a single worker.
type WorkerStatus struct {
	Exchange   string    `json:"exchange"`
	Status     string    `json:"status"`
	LastUpdate time.Time `json:"last_update"`
	LastError  string    `json:"last_error,omitempty"`
	Tickers    int       `json:"tickers"`
}

// WorkerStatusResponse represents the response for worker status.
type WorkerStatusResponse struct {
	Workers   []WorkerStatus `json:"workers"`
	Count     int            `json:"count"`
	Timestamp time.Time      `json:"timestamp"`
	Healthy   bool           `json:"healthy"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// ================== HTTP Client Helpers ==================

// makeCCXTRequest makes an HTTP request to the CCXT service.
func (h *MarketHandler) makeCCXTRequest(ctx context.Context, method, path string, body interface{}, result interface{}) error {
	reqURL := h.ccxtServiceURL + path

	var reqBody io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonData)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, reqBody)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "NeuraTrade-SQLite/1.0")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to make request to CCXT service: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode >= 400 {
		var errorResp ErrorResponse
		errorMsg := string(respBody)
		if jsonErr := json.Unmarshal(respBody, &errorResp); jsonErr == nil {
			errorMsg = errorResp.Error
			if errorResp.Message != "" {
				errorMsg = fmt.Sprintf("%s: %s", errorResp.Error, errorResp.Message)
			}
		}
		return fmt.Errorf("CCXT service error (%d): %s", resp.StatusCode, errorMsg)
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	return nil
}

// checkCCXTHealth checks if the CCXT service is healthy.
func (h *MarketHandler) checkCCXTHealth(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", h.ccxtServiceURL+"/health", nil)
	if err != nil {
		return false
	}

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	return resp.StatusCode == http.StatusOK
}

// ================== Handler Methods ==================

// GetMarketPrices retrieves current market prices from CCXT service.
// This endpoint fetches live data from configured exchanges.
//
// @Summary Get market prices
// @Description Get current market prices from CCXT service
// @Tags market
// @Produce json
// @Param exchange query string false "Filter by exchange"
// @Param symbol query string false "Filter by symbol"
// @Param page query int false "Page number" default(1)
// @Param limit query int false "Items per page" default(50)
// @Success 200 {object} MarketPricesResponse
// @Failure 503 {object} ErrorResponse
// @Router /api/v1/market/prices [get]
func (h *MarketHandler) GetMarketPrices(c *gin.Context) {
	ctx := c.Request.Context()

	// Check CCXT service health first
	if !h.checkCCXTHealth(ctx) {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Service unavailable",
			Message: "CCXT service is not available",
		})
		return
	}

	// Parse query parameters
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))

	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 50
	}

	// Build request path
	path := "/api/tickers"
	if exchange != "" {
		path = fmt.Sprintf("/api/tickers?exchange=%s", exchange)
		if symbol != "" {
			path += fmt.Sprintf("&symbol=%s", symbol)
		}
	}

	// Make request to CCXT service
	var ccxtResponse struct {
		Tickers []struct {
			Exchange string `json:"exchange"`
			Symbol   string `json:"symbol"`
			Last     string `json:"last"`
			Volume   string `json:"baseVolume"`
		} `json:"tickers"`
	}

	if err := h.makeCCXTRequest(ctx, "GET", path, nil, &ccxtResponse); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to fetch market prices",
			Message: err.Error(),
		})
		return
	}

	// Convert to response format
	data := make([]MarketPriceData, 0, len(ccxtResponse.Tickers))
	for _, t := range ccxtResponse.Tickers {
		price, _ := decimal.NewFromString(t.Last)
		volume, _ := decimal.NewFromString(t.Volume)

		data = append(data, MarketPriceData{
			Exchange:    t.Exchange,
			Symbol:      t.Symbol,
			Price:       price,
			Volume:      volume,
			Timestamp:   time.Now(),
			LastUpdated: time.Now(),
		})
	}

	// Apply pagination (simple slice operation for now)
	total := len(data)
	start := (page - 1) * limit
	end := start + limit
	if start > total {
		data = []MarketPriceData{}
	} else if end > total {
		data = data[start:]
	} else {
		data = data[start:end]
	}

	c.JSON(http.StatusOK, MarketPricesResponse{
		Data:      data,
		Total:     total,
		Page:      page,
		Limit:     limit,
		Timestamp: time.Now(),
	})
}

// GetTicker retrieves ticker data for a specific exchange and symbol.
//
// @Summary Get ticker
// @Description Get ticker for specific exchange/symbol
// @Tags market
// @Produce json
// @Param exchange path string true "Exchange name"
// @Param symbol path string true "Trading symbol"
// @Success 200 {object} TickerResponse
// @Failure 400 {object} ErrorResponse
// @Failure 404 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /api/v1/market/ticker/{exchange}/{symbol} [get]
func (h *MarketHandler) GetTicker(c *gin.Context) {
	ctx := c.Request.Context()
	exchange := c.Param("exchange")
	symbol := c.Param("symbol")

	if exchange == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Exchange and symbol are required",
		})
		return
	}

	// Check CCXT service health
	if !h.checkCCXTHealth(ctx) {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Service unavailable",
			Message: "CCXT service is not available",
		})
		return
	}

	// Format symbol for URL (remove slashes for most exchanges)
	formattedSymbol := strings.ReplaceAll(symbol, "/", "")

	// Make request to CCXT service
	path := fmt.Sprintf("/api/ticker/%s/%s", exchange, formattedSymbol)

	var ccxtResponse struct {
		Exchange string `json:"exchange"`
		Symbol   string `json:"symbol"`
		Last     string `json:"last"`
		High     string `json:"high"`
		Low      string `json:"low"`
		Bid      string `json:"bid"`
		Ask      string `json:"ask"`
		Volume   string `json:"baseVolume"`
	}

	if err := h.makeCCXTRequest(ctx, "GET", path, nil, &ccxtResponse); err != nil {
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "Ticker not found",
			Message: err.Error(),
		})
		return
	}

	// Convert to response format
	price, _ := decimal.NewFromString(ccxtResponse.Last)
	volume, _ := decimal.NewFromString(ccxtResponse.Volume)
	high, _ := decimal.NewFromString(ccxtResponse.High)
	low, _ := decimal.NewFromString(ccxtResponse.Low)
	bid, _ := decimal.NewFromString(ccxtResponse.Bid)
	ask, _ := decimal.NewFromString(ccxtResponse.Ask)

	c.JSON(http.StatusOK, TickerResponse{
		Exchange:  ccxtResponse.Exchange,
		Symbol:    ccxtResponse.Symbol,
		Price:     price,
		Volume:    volume,
		High:      high,
		Low:       low,
		Bid:       bid,
		Ask:       ask,
		Timestamp: time.Now(),
	})
}

// GetBulkTickers retrieves all tickers for a specific exchange.
//
// @Summary Get bulk tickers
// @Description Get multiple tickers for an exchange
// @Tags market
// @Produce json
// @Param exchange path string true "Exchange name"
// @Success 200 {object} BulkTickerResponse
// @Failure 400 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /api/v1/market/bulk-tickers/{exchange} [get]
func (h *MarketHandler) GetBulkTickers(c *gin.Context) {
	ctx := c.Request.Context()
	exchange := c.Param("exchange")

	if exchange == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Exchange is required",
		})
		return
	}

	// Check CCXT service health
	if !h.checkCCXTHealth(ctx) {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Service unavailable",
			Message: "CCXT service is not available",
		})
		return
	}

	// Make request to CCXT service
	path := fmt.Sprintf("/api/tickers?exchange=%s", exchange)

	var ccxtResponse struct {
		Tickers []struct {
			Exchange string `json:"exchange"`
			Symbol   string `json:"symbol"`
			Last     string `json:"last"`
			Volume   string `json:"baseVolume"`
		} `json:"tickers"`
	}

	if err := h.makeCCXTRequest(ctx, "GET", path, nil, &ccxtResponse); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to fetch bulk tickers",
			Message: err.Error(),
		})
		return
	}

	// Convert to response format
	tickers := make([]TickerResponse, 0, len(ccxtResponse.Tickers))
	for _, t := range ccxtResponse.Tickers {
		price, _ := decimal.NewFromString(t.Last)
		volume, _ := decimal.NewFromString(t.Volume)

		tickers = append(tickers, TickerResponse{
			Exchange:  t.Exchange,
			Symbol:    t.Symbol,
			Price:     price,
			Volume:    volume,
			Timestamp: time.Now(),
		})
	}

	c.JSON(http.StatusOK, BulkTickerResponse{
		Exchange:  exchange,
		Tickers:   tickers,
		Count:     len(tickers),
		Timestamp: time.Now(),
	})
}

// GetOrderBook retrieves order book data for a specific exchange and symbol.
//
// @Summary Get order book
// @Description Get order book for exchange/symbol
// @Tags market
// @Produce json
// @Param exchange path string true "Exchange name"
// @Param symbol path string true "Trading symbol"
// @Param limit query int false "Number of levels" default(20)
// @Success 200 {object} OrderBookResponseAPI
// @Failure 400 {object} ErrorResponse
// @Failure 503 {object} ErrorResponse
// @Router /api/v1/market/orderbook/{exchange}/{symbol} [get]
func (h *MarketHandler) GetOrderBook(c *gin.Context) {
	ctx := c.Request.Context()
	exchange := c.Param("exchange")
	symbol := c.Param("symbol")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))

	if exchange == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error: "Exchange and symbol are required",
		})
		return
	}

	if limit < 1 || limit > 100 {
		limit = 20
	}

	// Check CCXT service health
	if !h.checkCCXTHealth(ctx) {
		c.JSON(http.StatusServiceUnavailable, ErrorResponse{
			Error:   "Service unavailable",
			Message: "CCXT service is not available",
		})
		return
	}

	// Format symbol for URL (remove slashes for most exchanges)
	formattedSymbol := strings.ReplaceAll(symbol, "/", "")

	// Make request to CCXT service
	path := fmt.Sprintf("/api/orderbook/%s/%s?limit=%d", exchange, formattedSymbol, limit)

	var ccxtResponse struct {
		Exchange string      `json:"exchange"`
		Symbol   string      `json:"symbol"`
		Bids     [][]float64 `json:"bids"`
		Asks     [][]float64 `json:"asks"`
	}

	if err := h.makeCCXTRequest(ctx, "GET", path, nil, &ccxtResponse); err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "Failed to fetch order book",
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, OrderBookResponseAPI{
		Exchange:  ccxtResponse.Exchange,
		Symbol:    ccxtResponse.Symbol,
		Bids:      ccxtResponse.Bids,
		Asks:      ccxtResponse.Asks,
		Timestamp: time.Now(),
	})
}

// GetWorkerStatus returns the status of market data workers.
// In SQLite mode, this returns mock data since there's no persistent worker.
//
// @Summary Get worker status
// @Description Get market data worker status
// @Tags market
// @Produce json
// @Success 200 {object} WorkerStatusResponse
// @Router /api/v1/market/workers/status [get]
func (h *MarketHandler) GetWorkerStatus(c *gin.Context) {
	// In SQLite mode, we don't have persistent workers
	// Return mock data indicating CCXT service connectivity

	ctx := c.Request.Context()
	isHealthy := h.checkCCXTHealth(ctx)

	status := "stopped"
	if isHealthy {
		status = "connected"
	}

	mockWorkers := []WorkerStatus{
		{
			Exchange:   "ccxt-service",
			Status:     status,
			LastUpdate: time.Now(),
			Tickers:    0,
		},
	}

	c.JSON(http.StatusOK, WorkerStatusResponse{
		Workers:   mockWorkers,
		Count:     len(mockWorkers),
		Timestamp: time.Now(),
		Healthy:   isHealthy,
	})
}

// ================== Additional Helper Methods ==================

// SetTimeout allows configuring the HTTP client timeout.
func (h *MarketHandler) SetTimeout(timeout time.Duration) {
	h.httpClient.Timeout = timeout
}

// GetServiceURL returns the configured CCXT service URL.
func (h *MarketHandler) GetServiceURL() string {
	return h.ccxtServiceURL
}
