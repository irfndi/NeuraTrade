package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfandi/celebrum-ai-go/internal/api/handlers/testmocks"
	"github.com/irfandi/celebrum-ai-go/internal/ccxt"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockRedisClient implements RedisInterface for testing
type MockRedisClient struct {
	mock.Mock
}

func (m *MockRedisClient) Get(ctx context.Context, key string) *redis.StringCmd {
	args := m.Called(ctx, key)
	return args.Get(0).(*redis.StringCmd)
}

func (m *MockRedisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	args := m.Called(ctx, key, value, expiration)
	return args.Get(0).(*redis.StatusCmd)
}

func (m *MockRedisClient) Del(ctx context.Context, keys ...string) *redis.IntCmd {
	args := m.Called(ctx, keys)
	if args.Get(0) == nil {
		return redis.NewIntResult(0, nil)
	}
	return args.Get(0).(*redis.IntCmd)
}

func TestNewExchangeHandler(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	mockCollector := &testmocks.MockCollectorService{}
	mockRedis := &MockRedisClient{}

	handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.ccxtService)
}

func TestExchangeHandler_GetExchangeConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockCCXT := &testmocks.MockCCXTService{}
	mockCollector := &testmocks.MockCollectorService{}
	mockRedis := &MockRedisClient{}

	handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

	// Mock Redis cache miss (returns error)
	redisGetCmd := redis.NewStringResult("", redis.Nil)
	mockRedis.On("Get", mock.Anything, "exchange:config").Return(redisGetCmd)

	// Mock the CCXT service response
	expectedConfig := &ccxt.ExchangeConfigResponse{
		Config: ccxt.ExchangeConfig{
			BlacklistedExchanges: []string{},
			PriorityExchanges:    []string{"binance", "coinbase"},
			ExchangeConfigs:      map[string]interface{}{},
		},
		ActiveExchanges:    []string{"binance", "coinbase"},
		AvailableExchanges: []string{"binance", "coinbase", "kraken"},
		Timestamp:          "2023-01-01T00:00:00Z",
	}
	mockCCXT.On("GetExchangeConfig", mock.Anything).Return(expectedConfig, nil)

	// Mock Redis set operation
	redisSetCmd := redis.NewStatusResult("OK", nil)
	mockRedis.On("Set", mock.Anything, "exchange:config", mock.Anything, time.Hour).Return(redisSetCmd)

	// Create test request
	req, _ := http.NewRequest("GET", "/api/v1/exchange/config", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = req

	// Call the handler
	handler.GetExchangeConfig(c)

	// Assert response
	assert.Equal(t, http.StatusOK, w.Code)

	var response ccxt.ExchangeConfigResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, *expectedConfig, response)

	// Verify mocks
	mockCCXT.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
}

func TestExchangeHandler_AddExchangeToBlacklist(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}
	mockRedis := &MockRedisClient{}
	mockCollector := &testmocks.MockCollectorService{}

	// Setup mock expectations
	mockResponse := &ccxt.ExchangeManagementResponse{
		Message:   "Exchange added to blacklist successfully",
		Timestamp: "2023-01-01T00:00:00Z",
	}
	mockCCXT.On("AddExchangeToBlacklist", mock.Anything, "binance").Return(mockResponse, nil)
	mockCollector.On("RestartWorker", "binance").Return(nil)
	mockRedis.On("Del", mock.Anything, []string{"exchange:config", "exchange:supported"}).Return(nil)

	handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

	// Setup route
	router.POST("/exchanges/blacklist/:exchange", handler.AddExchangeToBlacklist)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/exchanges/blacklist/binance", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockCCXT.AssertExpectations(t)
	mockCollector.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
}

func TestExchangeHandler_RemoveExchangeFromBlacklist(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}
	mockRedis := &MockRedisClient{}
	mockCollector := &testmocks.MockCollectorService{}

	// Setup mock expectations
	mockResponse := &ccxt.ExchangeManagementResponse{
		Message:   "Exchange removed from blacklist successfully",
		Timestamp: "2023-01-01T00:00:00Z",
	}
	mockCCXT.On("RemoveExchangeFromBlacklist", mock.Anything, "binance").Return(mockResponse, nil)
	mockCollector.On("RestartWorker", "binance").Return(nil)
	mockRedis.On("Del", mock.Anything, []string{"exchange:config", "exchange:supported"}).Return(nil)

	handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

	// Setup route
	router.DELETE("/exchanges/blacklist/:exchange", handler.RemoveExchangeFromBlacklist)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("DELETE", "/exchanges/blacklist/binance", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockCCXT.AssertExpectations(t)
	mockCollector.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
}

func TestExchangeHandler_RefreshExchanges(t *testing.T) {
	mockCCXT := &testmocks.MockCCXTService{}
	mockCollector := &testmocks.MockCollectorService{}
	mockRedis := &MockRedisClient{}

	handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

	mockResponse := &ccxt.ExchangeManagementResponse{
		Message:   "Exchanges refreshed successfully",
		Timestamp: "2023-01-01T00:00:00Z",
	}
	mockCCXT.On("RefreshExchanges", mock.Anything).Return(mockResponse, nil)

	// Mock collector service methods
	mockCollector.On("Stop").Return()
	mockCollector.On("Start").Return(nil)
	mockRedis.On("Del", mock.Anything, []string{"exchange:config", "exchange:supported"}).Return(nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/exchange/refresh", nil)

	handler.RefreshExchanges(c)

	assert.Equal(t, http.StatusOK, w.Code)
	mockCCXT.AssertExpectations(t)
	mockCollector.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
}

func TestExchangeHandler_AddExchange(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}
	mockRedis := &MockRedisClient{}
	mockCollector := &testmocks.MockCollectorService{}

	// Setup mock expectations
	mockResponse := &ccxt.ExchangeManagementResponse{
		Message:   "Exchange added successfully",
		Timestamp: "2023-01-01T00:00:00Z",
	}
	mockCCXT.On("AddExchange", mock.Anything, "binance").Return(mockResponse, nil)
	mockCollector.On("Stop").Return()
	mockCollector.On("Start").Return(nil)
	mockRedis.On("Del", mock.Anything, []string{"exchange:config", "exchange:supported"}).Return(nil)

	handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

	// Setup route
	router.POST("/exchanges/add/:exchange", handler.AddExchange)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/exchanges/add/binance", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	mockCCXT.AssertExpectations(t)
	mockCollector.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
}

func TestExchangeHandler_GetSupportedExchanges(t *testing.T) {
	// Setup
	gin.SetMode(gin.TestMode)
	router := gin.New()

	// Create mocks
	mockCCXT := &testmocks.MockCCXTService{}
	mockRedis := &MockRedisClient{}
	mockCollector := &testmocks.MockCollectorService{}

	// Setup mock expectations
	mockRedis.On("Get", mock.Anything, "exchange:supported").Return(&redis.StringCmd{})
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance", "coinbase", "kraken"})
	mockRedis.On("Set", mock.Anything, "exchange:supported", mock.Anything, 30*time.Minute).Return(&redis.StatusCmd{})

	handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

	// Setup route
	router.GET("/exchanges/supported", handler.GetSupportedExchanges)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/exchanges/supported", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// Parse response
	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	// Convert the interface{} slice to []string for comparison
	exchanges, ok := response["exchanges"].([]interface{})
	assert.True(t, ok)
	expectedExchanges := []string{"binance", "coinbase", "kraken"}
	for i, exchange := range exchanges {
		assert.Equal(t, expectedExchanges[i], exchange.(string))
	}
	assert.Equal(t, float64(3), response["count"])

	mockCCXT.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
}

// Error scenario tests
func TestExchangeHandler_AddExchangeToBlacklist_ErrorScenarios(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		exchange       string
		expectedStatus int
		setupMocks     func(*testmocks.MockCCXTService, *testmocks.MockCollectorService)
	}{
		{
			name:           "missing exchange parameter",
			exchange:       "",
			expectedStatus: http.StatusBadRequest,
			setupMocks:     func(ccxt *testmocks.MockCCXTService, collector *testmocks.MockCollectorService) {},
		},
		{
			name:           "ccxt service error",
			exchange:       "binance",
			expectedStatus: http.StatusInternalServerError,
			setupMocks: func(ccxt *testmocks.MockCCXTService, collector *testmocks.MockCollectorService) {
				ccxt.On("AddExchangeToBlacklist", mock.Anything, "binance").Return(nil, assert.AnError)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCCXT := &testmocks.MockCCXTService{}
			mockRedis := &MockRedisClient{}
			mockCollector := &testmocks.MockCollectorService{}

			tt.setupMocks(mockCCXT, mockCollector)

			handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{{Key: "exchange", Value: tt.exchange}}
			c.Request, _ = http.NewRequest("POST", "/exchanges/blacklist/"+tt.exchange, nil)

			handler.AddExchangeToBlacklist(c)

			assert.Equal(t, tt.expectedStatus, w.Code)
			mockCCXT.AssertExpectations(t)
			mockCollector.AssertExpectations(t)
		})
	}
}

func TestExchangeHandler_GetWorkerStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("collector service unavailable", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		mockRedis := &MockRedisClient{}

		handler := NewExchangeHandler(mockCCXT, nil, mockRedis)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/workers/status", nil)

		handler.GetWorkerStatus(c)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("collector service available", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		mockRedis := &MockRedisClient{}
		mockCollector := &testmocks.MockCollectorService{}

		handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/workers/status", nil)

		handler.GetWorkerStatus(c)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestExchangeHandler_RestartWorker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("missing exchange parameter", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		mockRedis := &MockRedisClient{}
		mockCollector := &testmocks.MockCollectorService{}

		handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "exchange", Value: ""}}
		c.Request, _ = http.NewRequest("POST", "/workers/restart/", nil)

		handler.RestartWorker(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("collector service unavailable", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		mockRedis := &MockRedisClient{}

		handler := NewExchangeHandler(mockCCXT, nil, mockRedis)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "exchange", Value: "binance"}}
		c.Request, _ = http.NewRequest("POST", "/workers/restart/binance", nil)

		handler.RestartWorker(c)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("restart worker error", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		mockRedis := &MockRedisClient{}
		mockCollector := &testmocks.MockCollectorService{}

		mockCollector.On("RestartWorker", "binance").Return(assert.AnError)

		handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "exchange", Value: "binance"}}
		c.Request, _ = http.NewRequest("POST", "/workers/restart/binance", nil)

		handler.RestartWorker(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		mockCollector.AssertExpectations(t)
	})

	t.Run("successful restart", func(t *testing.T) {
		mockCCXT := &testmocks.MockCCXTService{}
		mockRedis := &MockRedisClient{}
		mockCollector := &testmocks.MockCollectorService{}

		mockCollector.On("RestartWorker", "binance").Return(nil)

		handler := NewExchangeHandler(mockCCXT, mockCollector, mockRedis)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Params = gin.Params{{Key: "exchange", Value: "binance"}}
		c.Request, _ = http.NewRequest("POST", "/workers/restart/binance", nil)

		handler.RestartWorker(c)

		assert.Equal(t, http.StatusOK, w.Code)
		mockCollector.AssertExpectations(t)
	})
}
