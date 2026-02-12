package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockCacheAnalyticsService is a mock implementation of CacheAnalyticsService
type MockCacheAnalyticsService struct {
	mock.Mock
}

// NewMockCacheAnalyticsService creates a new mock service
func NewMockCacheAnalyticsService() *MockCacheAnalyticsService {
	return &MockCacheAnalyticsService{
		Mock: mock.Mock{},
	}
}

func (m *MockCacheAnalyticsService) GetStats(category string) services.CacheStats {
	args := m.Called(category)
	return args.Get(0).(services.CacheStats)
}

func (m *MockCacheAnalyticsService) GetAllStats() map[string]services.CacheStats {
	args := m.Called()
	return args.Get(0).(map[string]services.CacheStats)
}

func (m *MockCacheAnalyticsService) GetMetrics(ctx context.Context) (*services.CacheMetrics, error) {
	args := m.Called(ctx)
	return args.Get(0).(*services.CacheMetrics), args.Error(1)
}

func (m *MockCacheAnalyticsService) ResetStats() {
	m.Called()
}

func (m *MockCacheAnalyticsService) RecordHit(category string) {
	m.Called(category)
}

func (m *MockCacheAnalyticsService) RecordMiss(category string) {
	m.Called(category)
}

func TestNewCacheHandler(t *testing.T) {
	mockService := NewMockCacheAnalyticsService()
	handler := NewCacheHandler(mockService)

	assert.NotNil(t, handler)
}

func TestCacheHandler_GetCacheStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockService := NewMockCacheAnalyticsService()
	handler := NewCacheHandler(mockService)

	// Mock the GetAllStats method
	expectedStats := map[string]services.CacheStats{
		"market_data": {
			Hits:        100,
			Misses:      20,
			HitRate:     0.83,
			TotalOps:    120,
			LastUpdated: time.Now(),
		},
	}
	mockService.On("GetAllStats").Return(expectedStats)

	// Create request
	req, _ := http.NewRequest("GET", "/api/cache/stats", nil)
	w := httptest.NewRecorder()

	// Setup Gin
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.GET("/api/cache/stats", handler.GetCacheStats)

	// Perform request
	router.ServeHTTP(w, req)

	// Assert response
	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.True(t, response["success"].(bool))

	// Assert data structure
	data := response["data"].(map[string]interface{})
	marketData := data["market_data"].(map[string]interface{})
	assert.Equal(t, float64(100), marketData["hits"])
	assert.Equal(t, float64(20), marketData["misses"])
	assert.Equal(t, 0.83, marketData["hit_rate"])
	assert.Equal(t, float64(120), marketData["total_ops"])

	mockService.AssertExpectations(t)
}

func TestCacheHandler_GetCacheStatsByCategory(t *testing.T) {
	tests := []struct {
		name         string
		category     string
		expectError  bool
		expectedCode int
	}{
		{
			name:         "valid category",
			category:     "market_data",
			expectError:  false,
			expectedCode: http.StatusOK,
		},
		{
			name:         "empty category",
			category:     "",
			expectError:  true,
			expectedCode: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := NewMockCacheAnalyticsService()
			handler := NewCacheHandler(mockService)

			if !tt.expectError {
				expectedStats := services.CacheStats{
					Hits:        50,
					Misses:      10,
					TotalOps:    60,
					HitRate:     0.833,
					LastUpdated: time.Now(),
				}
				mockService.On("GetStats", tt.category).Return(expectedStats)
			}

			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request, _ = http.NewRequest("GET", "/cache/stats/"+tt.category, nil)
			c.Params = []gin.Param{{Key: "category", Value: tt.category}}

			handler.GetCacheStatsByCategory(c)

			assert.Equal(t, tt.expectedCode, w.Code)

			if !tt.expectError {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.True(t, response["success"].(bool))

				// Assert data structure
				data := response["data"].(map[string]interface{})
				assert.Equal(t, float64(50), data["hits"])
				assert.Equal(t, float64(10), data["misses"])
				assert.Equal(t, float64(60), data["total_ops"])

				mockService.AssertExpectations(t)
			}
		})
	}
}

func TestCacheHandler_GetCacheMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockService := NewMockCacheAnalyticsService()
	handler := NewCacheHandler(mockService)

	expectedMetrics := &services.CacheMetrics{
		Overall: services.CacheStats{
			Hits:        150,
			Misses:      30,
			TotalOps:    180,
			HitRate:     0.833,
			LastUpdated: time.Now(),
		},
		ByCategory: map[string]services.CacheStats{
			"symbols": {
				Hits:        100,
				Misses:      20,
				TotalOps:    120,
				HitRate:     0.833,
				LastUpdated: time.Now(),
			},
		},
		RedisInfo:        map[string]string{"version": "6.2.0"},
		MemoryUsage:      1024,
		ConnectedClients: 5,
		KeyCount:         100,
	}

	mockService.On("GetMetrics", mock.Anything).Return(expectedMetrics, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("GET", "/cache/metrics", nil)

	handler.GetCacheMetrics(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.True(t, response["success"].(bool))

	// Assert data structure
	data := response["data"].(map[string]interface{})
	overall := data["overall"].(map[string]interface{})
	assert.Equal(t, float64(150), overall["hits"])
	assert.Equal(t, float64(1024), data["memory_usage_bytes"])

	mockService.AssertExpectations(t)
}

func TestCacheHandler_ResetCacheStats(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockService := NewMockCacheAnalyticsService()
	handler := NewCacheHandler(mockService)

	mockService.On("ResetStats").Return(nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/cache/reset", nil)

	handler.ResetCacheStats(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.True(t, response["success"].(bool))

	mockService.AssertExpectations(t)
}

func TestCacheHandler_RecordCacheHit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockService := NewMockCacheAnalyticsService()
	handler := NewCacheHandler(mockService)

	mockService.On("RecordHit", "symbols").Return()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/cache/hit?category=symbols", nil)

	handler.RecordCacheHit(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.True(t, response["success"].(bool))

	mockService.AssertExpectations(t)
}

func TestCacheHandler_RecordCacheMiss(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockService := NewMockCacheAnalyticsService()
	handler := NewCacheHandler(mockService)

	mockService.On("RecordMiss", "symbols").Return()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request, _ = http.NewRequest("POST", "/cache/miss?category=symbols", nil)

	handler.RecordCacheMiss(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.True(t, response["success"].(bool))

	mockService.AssertExpectations(t)
}
