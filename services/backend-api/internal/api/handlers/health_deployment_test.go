package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/irfndi/neuratrade/internal/services"
)

func TestHealthHandler_CurlHealthcheckCompatibility(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	// Start a mock CCXT service that returns valid JSON health response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":5}`))
	}))
	defer ts.Close()

	mockDB := &MockDatabase{}
	mockRedis := &MockRedisHealthClient{}
	mockCacheAnalytics := NewMockCacheAnalyticsService()

	mockDB.On("HealthCheck", mock.Anything).Return(nil)
	mockRedis.On("HealthCheck", mock.Anything).Return(nil)
	mockCacheAnalytics.On("GetMetrics", mock.Anything).Return(&services.CacheMetrics{}, nil)
	mockCacheAnalytics.On("GetAllStats").Return(map[string]services.CacheStats{})

	handler := NewHealthHandler(mockDB, mockRedis, ts.URL, mockCacheAnalytics)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)

	handler.HealthCheck(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "healthy")
}

func TestHealthHandler_CCXTServiceConnectivity(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	tests := []struct {
		name            string
		setupServer     func() *httptest.Server
		useBadURL       bool
		expectUnhealthy bool
	}{
		{
			name: "ccxt_reachable",
			setupServer: func() *httptest.Server {
				return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":5}`))
				}))
			},
			useBadURL:       false,
			expectUnhealthy: false,
		},
		{
			name:            "ccxt_unreachable",
			setupServer:     nil,
			useBadURL:       true,
			expectUnhealthy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var url string
			if tt.setupServer != nil {
				ts := tt.setupServer()
				defer ts.Close()
				url = ts.URL
			}
			if tt.useBadURL {
				url = "http://localhost:9999" // intentionally unreachable
			}

			mockDB := &MockDatabase{}
			mockRedis := &MockRedisHealthClient{}
			mockCacheAnalytics := NewMockCacheAnalyticsService()

			mockDB.On("HealthCheck", mock.Anything).Return(nil)
			mockRedis.On("HealthCheck", mock.Anything).Return(nil)
			mockCacheAnalytics.On("GetMetrics", mock.Anything).Return(&services.CacheMetrics{}, nil)
			mockCacheAnalytics.On("GetAllStats").Return(map[string]services.CacheStats{})

			handler := NewHealthHandler(mockDB, mockRedis, url, mockCacheAnalytics)

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/health", nil)

			handler.HealthCheck(w, req)

			if tt.expectUnhealthy {
				assert.Contains(t, w.Body.String(), "unhealthy")
			} else {
				assert.NotContains(t, w.Body.String(), "unhealthy")
			}

			mockDB.AssertExpectations(t)
			mockRedis.AssertExpectations(t)
			mockCacheAnalytics.AssertExpectations(t)
		})
	}
}

func TestHealthHandler_MissingTelegramToken(t *testing.T) {
	// Start a mock CCXT service that returns valid JSON health response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":5}`))
	}))
	defer ts.Close()

	tests := []struct {
		name           string
		telegramToken  string
		expectedStatus int
	}{
		{
			name:           "telegram_token_missing",
			telegramToken:  "",
			expectedStatus: http.StatusOK, // Telegram is non-critical, so degraded returns 200
		},
		{
			name:           "telegram_token_present",
			telegramToken:  "test-token",
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDB := &MockDatabase{}
			mockRedis := &MockRedisHealthClient{}
			mockCacheAnalytics := NewMockCacheAnalyticsService()

			mockDB.On("HealthCheck", mock.Anything).Return(nil)
			mockRedis.On("HealthCheck", mock.Anything).Return(nil)
			mockCacheAnalytics.On("GetMetrics", mock.Anything).Return(&services.CacheMetrics{}, nil)
			mockCacheAnalytics.On("GetAllStats").Return(map[string]services.CacheStats{})

			t.Setenv("TELEGRAM_BOT_TOKEN", tt.telegramToken)

			handler := NewHealthHandler(mockDB, mockRedis, ts.URL, mockCacheAnalytics)

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/health", nil)

			handler.HealthCheck(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			mockDB.AssertExpectations(t)
			mockRedis.AssertExpectations(t)
			mockCacheAnalytics.AssertExpectations(t)
		})
	}
}

func TestHealthHandler_ServiceUnhealthyStatus(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	// Start a mock CCXT service that returns valid JSON health response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":5}`))
	}))
	defer ts.Close()

	mockDB := &MockDatabase{}
	mockRedis := &MockRedisHealthClient{}
	mockCacheAnalytics := NewMockCacheAnalyticsService()

	mockDB.On("HealthCheck", mock.Anything).Return(assert.AnError)
	mockRedis.On("HealthCheck", mock.Anything).Return(assert.AnError)
	mockCacheAnalytics.On("GetMetrics", mock.Anything).Return(&services.CacheMetrics{}, nil)
	mockCacheAnalytics.On("GetAllStats").Return(map[string]services.CacheStats{})

	handler := NewHealthHandler(mockDB, mockRedis, ts.URL, mockCacheAnalytics)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)

	handler.HealthCheck(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "database")
	assert.Contains(t, body, "redis")

	mockDB.AssertExpectations(t)
	mockRedis.AssertExpectations(t)
	mockCacheAnalytics.AssertExpectations(t)
}

func TestHealthHandler_ReadinessWithDBError(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	// Start a mock CCXT service that returns valid JSON health response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":5}`))
	}))
	defer ts.Close()

	mockDB := &MockDatabase{}
	mockRedis := &MockRedisHealthClient{}
	mockCacheAnalytics := NewMockCacheAnalyticsService()

	mockDB.On("HealthCheck", mock.Anything).Return(assert.AnError)

	handler := NewHealthHandler(mockDB, mockRedis, ts.URL, mockCacheAnalytics)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/ready", nil)

	handler.ReadinessCheck(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "\"ready\":false")

	mockDB.AssertExpectations(t)
}

func TestHealthHandler_ContextTimeout(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	// Start a mock CCXT service that returns valid JSON health response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":5}`))
	}))
	defer ts.Close()

	mockDB := &MockDatabase{}
	mockRedis := &MockRedisHealthClient{}
	mockCacheAnalytics := NewMockCacheAnalyticsService()

	mockDB.On("HealthCheck", mock.Anything).Return(context.DeadlineExceeded)
	mockRedis.On("HealthCheck", mock.Anything).Return(nil)
	mockCacheAnalytics.On("GetMetrics", mock.Anything).Return(&services.CacheMetrics{}, nil)
	mockCacheAnalytics.On("GetAllStats").Return(map[string]services.CacheStats{})

	handler := NewHealthHandler(mockDB, mockRedis, ts.URL, mockCacheAnalytics)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)

	handler.HealthCheck(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHealthHandler_LivenessAlwaysHealthy(t *testing.T) {
	handler := &HealthHandler{}

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/live", nil)

	handler.LivenessCheck(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "alive")
}

func TestHealthHandler_AllServicesHealthy(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")

	// Start a mock CCXT service that returns valid JSON health response
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy","exchanges_count":5}`))
	}))
	defer ts.Close()

	mockDB := &MockDatabase{}
	mockRedis := &MockRedisHealthClient{}
	mockCacheAnalytics := NewMockCacheAnalyticsService()

	mockDB.On("HealthCheck", mock.Anything).Return(nil)
	mockRedis.On("HealthCheck", mock.Anything).Return(nil)
	mockCacheAnalytics.On("GetMetrics", mock.Anything).Return(&services.CacheMetrics{}, nil)
	mockCacheAnalytics.On("GetAllStats").Return(map[string]services.CacheStats{})

	handler := NewHealthHandler(mockDB, mockRedis, ts.URL, mockCacheAnalytics)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/health", nil)

	handler.HealthCheck(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "\"status\":\"healthy\"")
	assert.Contains(t, w.Body.String(), "\"database\":\"healthy\"")
	assert.Contains(t, w.Body.String(), "\"redis\":\"healthy\"")
}
