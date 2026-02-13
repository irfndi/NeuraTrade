package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/api/handlers/testmocks"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestMarketHandler_GetTicker(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}

	// Create handler with nil database (we'll mock the CCXT service)
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	// Setup route
	router.GET("/ticker/:exchange/:symbol", handler.GetTicker)

	// Mock expectations
	expectedTicker := &models.MarketPrice{
		ExchangeName: "binance",
		Symbol:       "BTCUSDT",
		Price:        decimal.NewFromFloat(50000.0),
		Volume:       decimal.NewFromFloat(1000.0),
	}
	// Mock IsHealthy to return true so the service tries to fetch from CCXT
	mockCCXT.On("IsHealthy", mock.Anything).Return(true)
	mockCCXT.On("FetchSingleTicker", mock.Anything, "binance", "BTCUSDT").Return(expectedTicker, nil)

	// Create request
	req, _ := http.NewRequest("GET", "/ticker/binance/BTCUSDT", nil)
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Assertions
	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "binance", response["exchange"])
	assert.Equal(t, "BTCUSDT", response["symbol"])
	assert.Equal(t, "50000", response["price"])

	// Verify mock expectations
	mockCCXT.AssertExpectations(t)
}

func TestMarketHandler_GetWorkerStatus(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}

	// For testing worker status, we'll pass nil for the collector service
	// In a real scenario, this would be properly initialized
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	// Setup route
	router.GET("/workers/status", handler.GetWorkerStatus)

	// Create request
	req, _ := http.NewRequest("GET", "/workers/status", nil)
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Assertions - Since collector service is nil, we expect an error response
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "error")
}

func TestMarketHandler_GetTicker_MissingParams(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	// Setup route
	router.GET("/ticker/:exchange/:symbol", handler.GetTicker)

	// Test missing exchange
	req, _ := http.NewRequest("GET", "/ticker//:symbol", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return 400 for missing exchange
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMarketHandler_GetOrderBook_FetchError(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	// Setup route
	router.GET("/orderbook/:exchange/:symbol", handler.GetOrderBook)

	// Mock CCXT service as healthy but FetchOrderBook returns error
	mockCCXT.On("IsHealthy", mock.Anything).Return(true)
	mockCCXT.On("FetchOrderBook", mock.Anything, "binance", "BTCUSDT", 20).Return(nil, assert.AnError)

	// Create request
	req, _ := http.NewRequest("GET", "/orderbook/binance/BTCUSDT", nil)
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Should return 500 for internal server error
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Verify mock expectations
	mockCCXT.AssertExpectations(t)
}

func TestMarketHandler_GetOrderBook(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	// Setup route
	router.GET("/orderbook/:exchange/:symbol", handler.GetOrderBook)

	// Mock expectations
	expectedOrderBook := &ccxt.OrderBookResponse{
		Exchange: "binance",
		Symbol:   "BTCUSDT",
		OrderBook: ccxt.OrderBook{
			Symbol:    "BTCUSDT",
			Bids:      []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(49999), Amount: decimal.NewFromFloat(1.0)}},
			Asks:      []ccxt.OrderBookEntry{{Price: decimal.NewFromFloat(50001), Amount: decimal.NewFromFloat(1.0)}},
			Timestamp: time.Now(),
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	mockCCXT.On("IsHealthy", mock.Anything).Return(true)
	mockCCXT.On("FetchOrderBook", mock.Anything, "binance", "BTCUSDT", 20).Return(expectedOrderBook, nil)

	// Create request
	req, _ := http.NewRequest("GET", "/orderbook/binance/BTCUSDT", nil)
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Assertions
	assert.Equal(t, http.StatusOK, w.Code)

	var response ccxt.OrderBookResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "binance", response.Exchange)
	assert.Equal(t, "BTCUSDT", response.Symbol)

	// Verify mock expectations
	mockCCXT.AssertExpectations(t)
}

func TestMarketHandler_GetOrderBook_MissingParams(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	// Setup route
	router.GET("/orderbook/:exchange/:symbol", handler.GetOrderBook)

	// Test missing exchange
	req, _ := http.NewRequest("GET", "/orderbook//:symbol", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should return 400 for missing exchange
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMarketHandler_GetOrderBook_ServiceUnavailable(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	// Setup route
	router.GET("/orderbook/:exchange/:symbol", handler.GetOrderBook)

	// Mock CCXT service as unhealthy
	mockCCXT.On("IsHealthy", mock.Anything).Return(false)

	// Create request
	req, _ := http.NewRequest("GET", "/orderbook/binance/BTCUSDT", nil)
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Should return 503 for service unavailable
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	// Verify mock expectations
	mockCCXT.AssertExpectations(t)
}

func TestMarketHandler_GetOrderBookMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	router.GET("/orderbook/:exchange/:symbol/metrics", handler.GetOrderBookMetrics)

	expectedMetrics := &ccxt.OrderBookMetrics{
		Exchange:       "binance",
		Symbol:         "BTCUSDT",
		BidAskSpread:   decimal.NewFromFloat(0.04),
		MidPrice:       decimal.NewFromFloat(50000),
		BestBid:        decimal.NewFromFloat(49999),
		BestAsk:        decimal.NewFromFloat(50001),
		BidDepth1Pct:   decimal.NewFromFloat(100000),
		AskDepth1Pct:   decimal.NewFromFloat(80000),
		Imbalance1Pct:  decimal.NewFromFloat(0.11),
		Imbalance2Pct:  decimal.NewFromFloat(0.09),
		LiquidityScore: decimal.NewFromFloat(85),
	}

	mockCCXT.On("IsHealthy", mock.Anything).Return(true)
	mockCCXT.On("CalculateOrderBookMetrics", mock.Anything, "binance", "BTCUSDT", 20).Return(expectedMetrics, nil)

	req, _ := http.NewRequest("GET", "/orderbook/binance/BTCUSDT/metrics", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response ccxt.OrderBookMetrics
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "binance", response.Exchange)
	assert.Equal(t, "BTCUSDT", response.Symbol)
	assert.Equal(t, decimal.NewFromFloat(0.11), response.Imbalance1Pct)

	mockCCXT.AssertExpectations(t)
}

func TestMarketHandler_GetOrderBookMetrics_FetchError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	router.GET("/orderbook/:exchange/:symbol/metrics", handler.GetOrderBookMetrics)

	mockCCXT.On("IsHealthy", mock.Anything).Return(true)
	mockCCXT.On("CalculateOrderBookMetrics", mock.Anything, "binance", "BTCUSDT", 20).Return(nil, assert.AnError)

	req, _ := http.NewRequest("GET", "/orderbook/binance/BTCUSDT/metrics", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	mockCCXT.AssertExpectations(t)
}

func TestMarketHandler_GetOrderBookMetrics_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	router.GET("/orderbook/:exchange/:symbol/metrics", handler.GetOrderBookMetrics)

	req, _ := http.NewRequest("GET", "/orderbook//BTCUSDT/metrics", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMarketHandler_GetOrderBookMetrics_ServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	router.GET("/orderbook/:exchange/:symbol/metrics", handler.GetOrderBookMetrics)

	mockCCXT.On("IsHealthy", mock.Anything).Return(false)

	req, _ := http.NewRequest("GET", "/orderbook/binance/BTCUSDT/metrics", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)

	mockCCXT.AssertExpectations(t)
}

// Test struct validation
func TestMarketPricesResponse_Struct(t *testing.T) {
	response := MarketPricesResponse{
		Data:      []MarketPriceData{},
		Total:     100,
		Page:      1,
		Limit:     50,
		Timestamp: time.Now(),
	}

	assert.NotNil(t, response)
	assert.Equal(t, 100, response.Total)
	assert.Equal(t, 1, response.Page)
	assert.Equal(t, 50, response.Limit)
}

func TestMarketPriceData_Struct(t *testing.T) {
	data := MarketPriceData{
		Exchange:    "binance",
		Symbol:      "BTCUSDT",
		Price:       decimal.NewFromFloat(50000.0),
		Volume:      decimal.NewFromFloat(1000.0),
		Timestamp:   time.Now(),
		LastUpdated: time.Now(),
	}

	assert.NotNil(t, data)
	assert.Equal(t, "binance", data.Exchange)
	assert.Equal(t, "BTCUSDT", data.Symbol)
	assert.True(t, data.Price.Equal(decimal.NewFromFloat(50000.0)))
}

func TestTickerResponse_Struct(t *testing.T) {
	response := TickerResponse{
		Exchange:  "binance",
		Symbol:    "BTCUSDT",
		Price:     decimal.NewFromFloat(50000.0),
		Volume:    decimal.NewFromFloat(1000.0),
		Timestamp: time.Now(),
	}

	assert.NotNil(t, response)
	assert.Equal(t, "binance", response.Exchange)
	assert.Equal(t, "BTCUSDT", response.Symbol)
	assert.True(t, response.Price.Equal(decimal.NewFromFloat(50000.0)))
}

func TestNewMarketHandler(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	assert.NotNil(t, handler)
	assert.Equal(t, mockCCXT, handler.ccxtService)
	assert.Nil(t, handler.db)
	assert.Nil(t, handler.collectorService)
}

func TestGetTicker(t *testing.T) {
	t.Run("successful ticker fetch", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		expectedPrice := &models.MarketPrice{
			Symbol:       "BTCUSDT",
			ExchangeName: "binance",
			Price:        decimal.NewFromFloat(50000.0),
			Volume:       decimal.NewFromFloat(1000.0),
			Timestamp:    time.Now(),
		}
		mockCCXT.On("IsHealthy", mock.Anything).Return(true)
		mockCCXT.On("FetchSingleTicker", mock.Anything, "binance", "BTCUSDT").Return(expectedPrice, nil)

		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Setup router with URL parameters
		router := gin.New()
		router.GET("/ticker/:exchange/:symbol", handler.GetTicker)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/ticker/binance/BTCUSDT", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockCCXT.AssertExpectations(t)

		// Verify response body
		var response TickerResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "binance", response.Exchange)
		assert.Equal(t, "BTCUSDT", response.Symbol)
		assert.True(t, response.Price.Equal(decimal.NewFromFloat(50000.0)))
	})

	t.Run("service error", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		mockCCXT.On("IsHealthy", mock.Anything).Return(true)
		mockCCXT.On("FetchSingleTicker", mock.Anything, "binance", "BTCUSDT").Return(nil, assert.AnError)

		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Setup router with URL parameters
		router := gin.New()
		router.GET("/ticker/:exchange/:symbol", handler.GetTicker)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/ticker/binance/BTCUSDT", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		mockCCXT.AssertExpectations(t)
	})
}

func TestMarketHandler_GetBulkTickers(t *testing.T) {
	t.Run("successful bulk tickers fetch", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		expectedData := []models.MarketPrice{
			{
				Symbol:       "BTCUSDT",
				ExchangeName: "binance",
				Price:        decimal.NewFromFloat(50000.0),
				Volume:       decimal.NewFromFloat(1000.0),
				Timestamp:    time.Now(),
			},
		}
		mockCCXT.On("IsHealthy", mock.Anything).Return(true)
		mockCCXT.On("FetchMarketData", mock.Anything, []string{"binance"}, []string{}).Return(expectedData, nil)

		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		router := gin.New()
		router.GET("/bulk-tickers/:exchange", handler.GetBulkTickers)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/bulk-tickers/binance", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		mockCCXT.AssertExpectations(t)
	})

	t.Run("missing exchange parameter", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		router := gin.New()
		router.GET("/bulk-tickers/:exchange", handler.GetBulkTickers)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/bulk-tickers", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("service unavailable", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		mockCCXT.On("IsHealthy", mock.Anything).Return(false)

		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		router := gin.New()
		router.GET("/bulk-tickers/:exchange", handler.GetBulkTickers)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/bulk-tickers/binance", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
		mockCCXT.AssertExpectations(t)
	})

	t.Run("fetch error", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		mockCCXT.On("IsHealthy", mock.Anything).Return(true)
		mockCCXT.On("FetchMarketData", mock.Anything, []string{"binance"}, []string{}).Return([]models.MarketPrice{}, assert.AnError)

		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		router := gin.New()
		router.GET("/bulk-tickers/:exchange", handler.GetBulkTickers)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/bulk-tickers/binance", nil)
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockCCXT.AssertExpectations(t)
	})
}

// Note: GetCacheStats and ResetCacheStats tests have been removed
// as these methods are now handled by CacheHandler for centralized cache analytics

func TestMarketHandler_GetWorkerStatus_WithCollectorService(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}

	// Create handler without collector service
	handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

	// Setup route
	router.GET("/workers/status", handler.GetWorkerStatus)

	// Create request
	req, _ := http.NewRequest("GET", "/workers/status", nil)
	w := httptest.NewRecorder()

	// Execute request
	router.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Collector service is not available", response["error"])
}

func TestMarketHandler_GetMarketPrices(t *testing.T) {
	t.Run("nil database causes panic", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/market-prices", nil)

		// This test expects a panic due to nil database access
		assert.Panics(t, func() {
			handler.GetMarketPrices(c)
		})
	})

	t.Run("valid pagination parameters", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/market-prices?page=2&limit=10", nil)

		// This test expects a panic due to nil database access
		assert.Panics(t, func() {
			handler.GetMarketPrices(c)
		})
	})

	t.Run("invalid limit parameter", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/market-prices?limit=101", nil)

		// This test expects a panic due to nil database access
		assert.Panics(t, func() {
			handler.GetMarketPrices(c)
		})
	})

	t.Run("with exchange filter", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/market-prices?exchange=binance", nil)

		// This test expects a panic due to nil database access
		assert.Panics(t, func() {
			handler.GetMarketPrices(c)
		})
	})

	t.Run("with symbol filter", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		gin.SetMode(gin.TestMode)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/market-prices?symbol=BTCUSDT", nil)

		// This test expects a panic due to nil database access
		assert.Panics(t, func() {
			handler.GetMarketPrices(c)
		})
	})
}

func TestMarketHandler_CacheFunctionality(t *testing.T) {
	t.Run("cache market prices with nil redis", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Should not panic with nil redis
		response := MarketPricesResponse{
			Data:      []MarketPriceData{},
			Total:     0,
			Page:      1,
			Limit:     50,
			Timestamp: time.Now(),
		}
		handler.CacheMarketPrices(context.Background(), "test_key", response)
	})

	t.Run("get cached market prices with nil redis", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Should return false with nil redis
		data, found := handler.GetCachedMarketPrices(context.Background(), "test_key")
		assert.False(t, found)
		assert.Nil(t, data)
	})

	t.Run("cache ticker with nil redis", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Should not panic with nil redis
		response := TickerResponse{
			Exchange:  "binance",
			Symbol:    "BTCUSDT",
			Price:     decimal.NewFromFloat(50000.0),
			Volume:    decimal.NewFromFloat(1000.0),
			Timestamp: time.Now(),
		}
		handler.CacheTicker(context.Background(), "test_key", response)
	})

	t.Run("get cached ticker with nil redis", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Should return false with nil redis
		data, found := handler.GetCachedTicker(context.Background(), "test_key")
		assert.False(t, found)
		assert.Nil(t, data)
	})

	t.Run("cache bulk tickers with nil redis", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Should not panic with nil redis
		response := BulkTickerResponse{
			Exchange:  "binance",
			Tickers:   []TickerResponse{},
			Timestamp: time.Now(),
			Cached:    false,
		}
		handler.CacheBulkTickers(context.Background(), "test_key", response)
	})

	t.Run("get cached bulk tickers with nil redis", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Should return false with nil redis
		data, found := handler.GetCachedBulkTickers(context.Background(), "test_key")
		assert.False(t, found)
		assert.Nil(t, data)
	})

	t.Run("cache order book with nil redis", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Should not panic with nil redis
		response := OrderBookResponse{
			Exchange:  "binance",
			Symbol:    "BTCUSDT",
			Bids:      [][]float64{},
			Asks:      [][]float64{},
			Timestamp: time.Now(),
			Cached:    false,
		}
		handler.CacheOrderBook(context.Background(), "test_key", response)
	})

	t.Run("get cached order book with nil redis", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		handler := NewMarketHandler(nil, mockCCXT, nil, nil, nil)

		// Should return false with nil redis
		data, found := handler.GetCachedOrderBook(context.Background(), "test_key")
		assert.False(t, found)
		assert.Nil(t, data)
	})
}

func TestMarketHandler_StructValidation(t *testing.T) {
	t.Run("CacheStats struct", func(t *testing.T) {
		stats := CacheStats{
			Hits:   100,
			Misses: 50,
		}

		assert.Equal(t, int64(100), stats.Hits)
		assert.Equal(t, int64(50), stats.Misses)
	})

	t.Run("OrderBookResponse struct", func(t *testing.T) {
		response := OrderBookResponse{
			Exchange:  "binance",
			Symbol:    "BTCUSDT",
			Bids:      [][]float64{{50000, 1.0}},
			Asks:      [][]float64{{50100, 1.0}},
			Timestamp: time.Now(),
			Cached:    false,
		}

		assert.Equal(t, "binance", response.Exchange)
		assert.Equal(t, "BTCUSDT", response.Symbol)
		assert.Len(t, response.Bids, 1)
		assert.Len(t, response.Asks, 1)
		assert.False(t, response.Cached)
	})

	t.Run("BulkTickerResponse struct", func(t *testing.T) {
		response := BulkTickerResponse{
			Exchange: "binance",
			Tickers: []TickerResponse{
				{
					Exchange:  "binance",
					Symbol:    "BTCUSDT",
					Price:     decimal.NewFromFloat(50000.0),
					Volume:    decimal.NewFromFloat(1000.0),
					Timestamp: time.Now(),
				},
			},
			Timestamp: time.Now(),
			Cached:    false,
		}

		assert.Equal(t, "binance", response.Exchange)
		assert.Len(t, response.Tickers, 1)
		assert.Equal(t, "BTCUSDT", response.Tickers[0].Symbol)
		assert.False(t, response.Cached)
	})
}

func TestConvertOrderBookEntries(t *testing.T) {
	t.Run("empty entries", func(t *testing.T) {
		entries := []ccxt.OrderBookEntry{}
		result := convertOrderBookEntries(entries)
		assert.Empty(t, result)
	})

	t.Run("single entry", func(t *testing.T) {
		entries := []ccxt.OrderBookEntry{
			{Price: decimal.NewFromFloat(50000.0), Amount: decimal.NewFromFloat(1.0)},
		}
		result := convertOrderBookEntries(entries)
		assert.Len(t, result, 1)
		assert.Equal(t, []float64{50000.0, 1.0}, result[0])
	})

	t.Run("multiple entries", func(t *testing.T) {
		entries := []ccxt.OrderBookEntry{
			{Price: decimal.NewFromFloat(50000.0), Amount: decimal.NewFromFloat(1.0)},
			{Price: decimal.NewFromFloat(50100.0), Amount: decimal.NewFromFloat(2.0)},
		}
		result := convertOrderBookEntries(entries)
		assert.Len(t, result, 2)
		assert.Equal(t, []float64{50000.0, 1.0}, result[0])
		assert.Equal(t, []float64{50100.0, 2.0}, result[1])
	})
}
