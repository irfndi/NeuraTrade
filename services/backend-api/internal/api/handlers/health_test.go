package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/irfndi/neuratrade/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// DatabaseInterface for mocking database operations
type DatabaseInterface interface {
	HealthCheck(ctx context.Context) error
}

// RedisHealthInterface for mocking Redis health operations
type RedisHealthInterface interface {
	HealthCheck(ctx context.Context) error
}

// MockDatabase mocks the database interface
type MockDatabase struct {
	mock.Mock
}

func (m *MockDatabase) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// MockRedisHealthClient is a mock implementation of RedisHealthInterface
type MockRedisHealthClient struct {
	mock.Mock
}

func (m *MockRedisHealthClient) HealthCheck(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func TestNewHealthHandler(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRedis := &MockRedisHealthClient{}
	mockCacheAnalytics := NewMockCacheAnalyticsService()

	handler := NewHealthHandler(mockDB, mockRedis, "http://localhost:8080", mockCacheAnalytics, nil)

	assert.NotNil(t, handler)
	assert.Equal(t, mockDB, handler.db)
	assert.Equal(t, mockRedis, handler.redis)
	assert.Equal(t, "http://localhost:8080", handler.ccxtURL)
	assert.Equal(t, mockCacheAnalytics, handler.cacheAnalytics)
}

func TestHealthHandler_HealthCheck(t *testing.T) {
	// Set up a mock HTTP server for CCXT service with exchanges_count field
	mockCCXTServer := newTestServerOrSkip(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":50,"exchange_connectivity":"configured"}`))
		}
	}))
	if mockCCXTServer == nil {
		return
	}
	defer mockCCXTServer.Close()

	// Set environment variable for Telegram bot token
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	tests := []struct {
		name           string
		dbError        error
		redisError     error
		expectedStatus int
		expectedHealth string
	}{
		{
			name:           "all services healthy",
			dbError:        nil,
			redisError:     nil,
			expectedStatus: http.StatusOK,
			expectedHealth: "healthy",
		},
		{
			name:           "database error - critical, returns 503",
			dbError:        assert.AnError,
			redisError:     nil,
			expectedStatus: http.StatusServiceUnavailable,
			expectedHealth: "degraded",
		},
		{
			name:           "redis error - non-critical, returns 200",
			dbError:        nil,
			redisError:     assert.AnError,
			expectedStatus: http.StatusOK,
			expectedHealth: "degraded",
		},
		{
			name:           "both db and redis error - critical, returns 503",
			dbError:        assert.AnError,
			redisError:     assert.AnError,
			expectedStatus: http.StatusServiceUnavailable,
			expectedHealth: "degraded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := &MockDatabase{}
			mockRedis := &MockRedisHealthClient{}
			mockCacheAnalytics := NewMockCacheAnalyticsService()

			mockDB.On("HealthCheck", mock.Anything).Return(tt.dbError)
			mockRedis.On("HealthCheck", mock.Anything).Return(tt.redisError)
			mockCacheAnalytics.On("GetMetrics", mock.Anything).Return(&services.CacheMetrics{}, nil)
			mockCacheAnalytics.On("GetAllStats").Return(map[string]services.CacheStats{})

			handler := NewHealthHandler(mockDB, mockRedis, mockCCXTServer.URL, mockCacheAnalytics, nil)

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/health", nil)

			handler.HealthCheck(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Contains(t, response, "status")
			assert.Contains(t, response, "services")
			assert.Contains(t, response, "timestamp")

			// Verify the health status matches expected
			status, ok := response["status"].(string)
			assert.True(t, ok, "status should be a string")
			assert.Equal(t, tt.expectedHealth, status, "health status should match expected")

			mockDB.AssertExpectations(t)
			mockRedis.AssertExpectations(t)
			mockCacheAnalytics.AssertExpectations(t)
		})
	}
}

// TestHealthHandler_DegradedNonCriticalService tests that a non-critical CCXT service failure
// returns 200 OK with a "degraded" overall health status.
// Note: Redis failures are tested separately in the table-driven tests above (line 99-104).
func TestHealthHandler_DegradedNonCriticalService(t *testing.T) {
	// Set up a mock CCXT server that returns unhealthy
	mockCCXTServer := newTestServerOrSkip(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"unhealthy"}`))
	}))
	if mockCCXTServer == nil {
		return
	}
	defer mockCCXTServer.Close()

	// Set environment variable for Telegram bot token
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	mockDB := &MockDatabase{}
	mockRedis := &MockRedisHealthClient{}
	mockCacheAnalytics := NewMockCacheAnalyticsService()

	// Database and Redis are healthy
	mockDB.On("HealthCheck", mock.Anything).Return(nil)
	mockRedis.On("HealthCheck", mock.Anything).Return(nil)
	mockCacheAnalytics.On("GetMetrics", mock.Anything).Return(&services.CacheMetrics{}, nil)
	mockCacheAnalytics.On("GetAllStats").Return(map[string]services.CacheStats{})

	handler := NewHealthHandler(mockDB, mockRedis, mockCCXTServer.URL, mockCacheAnalytics, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)

	handler.HealthCheck(w, req)

	// Should return 200 OK even though CCXT is unhealthy (non-critical)
	assert.Equal(t, http.StatusOK, w.Code, "should return 200 when only non-critical services are unhealthy")

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	status, ok := response["status"].(string)
	assert.True(t, ok, "status should be a string")
	assert.Equal(t, "degraded", status, "status should be degraded when CCXT is unhealthy")

	services, ok := response["services"].(map[string]interface{})
	assert.True(t, ok, "services should be a map")
	assert.Contains(t, services["ccxt"].(string), "unhealthy", "ccxt should be marked as unhealthy")

	mockDB.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
	mockCacheAnalytics.AssertExpectations(t)
}

func TestHealthHandler_ReadinessCheck(t *testing.T) {
	tests := []struct {
		name           string
		dbError        error
		expectedStatus int
	}{
		{
			name:           "all services ready",
			dbError:        nil,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "database not ready",
			dbError:        assert.AnError,
			expectedStatus: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := &MockDatabase{}
			mockRedis := &MockRedisHealthClient{}
			mockCacheAnalytics := NewMockCacheAnalyticsService()

			mockDB.On("HealthCheck", mock.Anything).Return(tt.dbError)
			mockRedis.On("HealthCheck", mock.Anything).Return(nil)

			handler := NewHealthHandler(mockDB, mockRedis, "http://localhost:8080", mockCacheAnalytics, nil)

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/ready", nil)

			handler.ReadinessCheck(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Contains(t, response, "ready")
			assert.Contains(t, response, "services")

			mockDB.AssertExpectations(t)
		})
	}
}

func TestHealthHandler_LivenessCheck(t *testing.T) {
	mockDB := &MockDatabase{}
	mockRedis := &MockRedisHealthClient{}
	mockCacheAnalytics := NewMockCacheAnalyticsService()

	handler := NewHealthHandler(mockDB, mockRedis, "http://localhost:8080", mockCacheAnalytics, nil)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/live", nil)

	handler.LivenessCheck(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "status")
	assert.Contains(t, response, "timestamp")
}

func TestHealthHandler_CCXTServiceCheck(t *testing.T) {
	tests := []struct {
		name           string
		ccxtResponse   string
		ccxtStatusCode int
		expectError    bool
	}{
		{
			name:           "ccxt healthy with exchanges",
			ccxtResponse:   `{"status":"healthy","exchanges_count":50,"exchange_connectivity":"configured"}`,
			ccxtStatusCode: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "ccxt unhealthy no exchanges",
			ccxtResponse:   `{"status":"unhealthy","exchanges_count":0,"exchange_connectivity":"unknown"}`,
			ccxtStatusCode: http.StatusServiceUnavailable,
			expectError:    true,
		},
		{
			name:           "ccxt returns 500",
			ccxtResponse:   `{"error":"Internal Server Error"}`,
			ccxtStatusCode: http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCCXTServer := newTestServerOrSkip(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.ccxtStatusCode)
				_, _ = w.Write([]byte(tt.ccxtResponse))
			}))
			if mockCCXTServer == nil {
				return
			}
			defer mockCCXTServer.Close()

			mockDB := &MockDatabase{}
			mockRedis := &MockRedisHealthClient{}
			mockCacheAnalytics := NewMockCacheAnalyticsService()

			handler := NewHealthHandler(mockDB, mockRedis, mockCCXTServer.URL, mockCacheAnalytics, nil)
			err := handler.checkCCXTService()

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHealthHandler_CCXTServiceZeroExchanges(t *testing.T) {
	// Test behavior when CCXT service has zero exchanges
	// If status is "healthy", we don't fail even with 0 exchanges (backward-compatible, startup condition)
	t.Run("zero exchanges with healthy status - no error (startup condition)", func(t *testing.T) {
		mockCCXTServer := newTestServerOrSkip(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":0,"exchange_connectivity":"unknown"}`))
		}))
		if mockCCXTServer == nil {
			return
		}
		defer mockCCXTServer.Close()

		mockDB := &MockDatabase{}
		mockRedis := &MockRedisHealthClient{}
		mockCacheAnalytics := NewMockCacheAnalyticsService()

		handler := NewHealthHandler(mockDB, mockRedis, mockCCXTServer.URL, mockCacheAnalytics, nil)
		err := handler.checkCCXTService()

		// No error when status is healthy (backward-compatible)
		assert.NoError(t, err)
	})

	t.Run("zero exchanges with degraded status - error", func(t *testing.T) {
		mockCCXTServer := newTestServerOrSkip(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"degraded","exchanges_count":0,"exchange_connectivity":"failed"}`))
		}))
		if mockCCXTServer == nil {
			return
		}
		defer mockCCXTServer.Close()

		mockDB := &MockDatabase{}
		mockRedis := &MockRedisHealthClient{}
		mockCacheAnalytics := NewMockCacheAnalyticsService()

		handler := NewHealthHandler(mockDB, mockRedis, mockCCXTServer.URL, mockCacheAnalytics, nil)
		err := handler.checkCCXTService()

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no active exchanges")
	})
}

func TestHealthHandler_TelegramTokenDetection(t *testing.T) {
	// Test that Telegram health check supports both TELEGRAM_BOT_TOKEN and TELEGRAM_TOKEN env vars

	tests := []struct {
		name           string
		botToken       string
		token          string
		expectedStatus string
	}{
		{
			name:           "TELEGRAM_BOT_TOKEN set",
			botToken:       "test-bot-token",
			token:          "",
			expectedStatus: "healthy",
		},
		{
			name:           "TELEGRAM_TOKEN set (fallback)",
			botToken:       "",
			token:          "test-fallback-token",
			expectedStatus: "healthy",
		},
		{
			name:           "both tokens set - BOT_TOKEN takes precedence",
			botToken:       "primary-token",
			token:          "fallback-token",
			expectedStatus: "healthy",
		},
		{
			name:           "no token set",
			botToken:       "",
			token:          "",
			expectedStatus: "unhealthy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up mock CCXT server
			mockCCXTServer := newTestServerOrSkip(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":50,"exchange_connectivity":"configured"}`))
			}))
			if mockCCXTServer == nil {
				return
			}
			defer mockCCXTServer.Close()

			// Set environment variables for this test
			if tt.botToken != "" {
				t.Setenv("TELEGRAM_BOT_TOKEN", tt.botToken)
			}
			if tt.token != "" {
				t.Setenv("TELEGRAM_TOKEN", tt.token)
			}

			mockDB := &MockDatabase{}
			mockRedis := &MockRedisHealthClient{}
			mockCacheAnalytics := NewMockCacheAnalyticsService()

			mockDB.On("HealthCheck", mock.Anything).Return(nil)
			mockRedis.On("HealthCheck", mock.Anything).Return(nil)
			mockCacheAnalytics.On("GetMetrics", mock.Anything).Return(&services.CacheMetrics{}, nil)
			mockCacheAnalytics.On("GetAllStats").Return(map[string]services.CacheStats{})

			handler := NewHealthHandler(mockDB, mockRedis, mockCCXTServer.URL, mockCacheAnalytics, nil)

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/health", nil)

			handler.HealthCheck(w, req)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			services, ok := response["services"].(map[string]interface{})
			assert.True(t, ok, "services should be a map")

			telegramStatus, ok := services["telegram"].(string)
			assert.True(t, ok, "telegram status should be a string")

			if tt.expectedStatus == "healthy" {
				assert.Equal(t, "healthy", telegramStatus, "telegram should be healthy when token is set")
				assert.Equal(t, http.StatusOK, w.Code)
			} else {
				assert.Contains(t, telegramStatus, "unhealthy", "telegram should be unhealthy when no token is set")
			}

			mockDB.AssertExpectations(t)
			mockRedis.AssertExpectations(t)
			mockCacheAnalytics.AssertExpectations(t)
		})
	}
}

// newTestServerOrSkip starts an httptest.Server, skipping the test when binding is not permitted.
func newTestServerOrSkip(t *testing.T, h http.Handler) *httptest.Server {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			msg := fmt.Sprint(r)
			if strings.Contains(msg, "operation not permitted") {
				t.Skip("binding not permitted in this environment; skipping server-based test")
			}
			panic(r)
		}
	}()

	srv := httptest.NewServer(h)
	return srv
}
