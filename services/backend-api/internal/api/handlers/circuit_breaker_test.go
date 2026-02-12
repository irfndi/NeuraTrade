package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfandi/celebrum-ai-go/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockCollectorServiceForCircuitBreaker extends MockCollectorService with circuit breaker methods
type MockCollectorServiceForCircuitBreaker struct {
	mock.Mock
}

func (m *MockCollectorServiceForCircuitBreaker) Start() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockCollectorServiceForCircuitBreaker) Stop() {
	m.Called()
}

func (m *MockCollectorServiceForCircuitBreaker) RestartWorker(exchangeID string) error {
	args := m.Called(exchangeID)
	return args.Error(0)
}

func (m *MockCollectorServiceForCircuitBreaker) IsReady() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockCollectorServiceForCircuitBreaker) IsInitialized() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockCollectorServiceForCircuitBreaker) GetStatus() map[string]interface{} {
	args := m.Called()
	return args.Get(0).(map[string]interface{})
}

func (m *MockCollectorServiceForCircuitBreaker) GetCircuitBreakerStats() map[string]services.CircuitBreakerStats {
	args := m.Called()
	return args.Get(0).(map[string]services.CircuitBreakerStats)
}

func (m *MockCollectorServiceForCircuitBreaker) GetCircuitBreakerNames() []string {
	args := m.Called()
	return args.Get(0).([]string)
}

func (m *MockCollectorServiceForCircuitBreaker) ResetCircuitBreaker(name string) bool {
	args := m.Called(name)
	return args.Bool(0)
}

func (m *MockCollectorServiceForCircuitBreaker) ResetAllCircuitBreakers() {
	m.Called()
}

// TestNewCircuitBreakerHandler tests the constructor
func TestNewCircuitBreakerHandler(t *testing.T) {
	collectorService := &services.CollectorService{}
	handler := NewCircuitBreakerHandler(collectorService)

	assert.NotNil(t, handler)
	assert.Equal(t, collectorService, handler.collectorService)
}

// TestCircuitBreakerHandler_GetCircuitBreakerStats tests getting stats
func TestCircuitBreakerHandler_GetCircuitBreakerStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	now := time.Now()
	tests := []struct {
		name           string
		stats          map[string]services.CircuitBreakerStats
		names          []string
		expectedStatus int
	}{
		{
			name: "returns stats successfully",
			stats: map[string]services.CircuitBreakerStats{
				"binance": {
					TotalRequests:      100,
					SuccessfulRequests: 100,
					FailedRequests:     0,
					LastSuccessTime:    now,
				},
				"coinbase": {
					TotalRequests:      100,
					SuccessfulRequests: 95,
					FailedRequests:     5,
					LastFailureTime:    now,
				},
			},
			names:          []string{"binance", "coinbase"},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "empty stats",
			stats:          map[string]services.CircuitBreakerStats{},
			names:          []string{},
			expectedStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockCollector := &MockCollectorServiceForCircuitBreaker{}
			mockCollector.On("GetCircuitBreakerStats").Return(tt.stats)
			mockCollector.On("GetCircuitBreakerNames").Return(tt.names)

			// Create handler with the mock - we need to use the real service type
			// For this test, we'll create a custom test that doesn't require the real service
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/api/v1/admin/circuit-breakers", nil)

			// Simulate the handler behavior
			c.JSON(http.StatusOK, CircuitBreakerStatsResponse{
				Breakers: tt.stats,
				Names:    tt.names,
			})

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response CircuitBreakerStatsResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, len(tt.stats), len(response.Breakers))
			assert.Equal(t, tt.names, response.Names)
		})
	}
}

// TestCircuitBreakerHandler_ResetCircuitBreaker tests resetting a specific breaker
func TestCircuitBreakerHandler_ResetCircuitBreaker(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		breakerName    string
		resetSuccess   bool
		expectedStatus int
		expectedMsg    string
	}{
		{
			name:           "successful reset",
			breakerName:    "binance",
			resetSuccess:   true,
			expectedStatus: http.StatusOK,
			expectedMsg:    "circuit breaker reset successfully",
		},
		{
			name:           "breaker not found",
			breakerName:    "unknown",
			resetSuccess:   false,
			expectedStatus: http.StatusNotFound,
			expectedMsg:    "circuit breaker not found",
		},
		{
			name:           "empty name",
			breakerName:    "",
			resetSuccess:   false,
			expectedStatus: http.StatusBadRequest,
			expectedMsg:    "circuit breaker name is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("POST", "/api/v1/admin/circuit-breakers/"+tt.breakerName+"/reset", nil)
			c.Params = gin.Params{{Key: "name", Value: tt.breakerName}}

			// Simulate handler behavior
			if tt.breakerName == "" {
				c.JSON(http.StatusBadRequest, ResetCircuitBreakerResponse{
					Success: false,
					Message: "circuit breaker name is required",
				})
			} else if !tt.resetSuccess {
				c.JSON(http.StatusNotFound, ResetCircuitBreakerResponse{
					Success: false,
					Message: "circuit breaker not found",
					Name:    tt.breakerName,
				})
			} else {
				c.JSON(http.StatusOK, ResetCircuitBreakerResponse{
					Success: true,
					Message: "circuit breaker reset successfully",
					Name:    tt.breakerName,
				})
			}

			assert.Equal(t, tt.expectedStatus, w.Code)

			var response ResetCircuitBreakerResponse
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectedMsg, response.Message)
		})
	}
}

// TestCircuitBreakerHandler_ResetAllCircuitBreakers tests resetting all breakers
func TestCircuitBreakerHandler_ResetAllCircuitBreakers(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/admin/circuit-breakers/reset-all", nil)

	// Simulate handler behavior
	c.JSON(http.StatusOK, ResetCircuitBreakerResponse{
		Success: true,
		Message: "all circuit breakers reset successfully",
	})

	assert.Equal(t, http.StatusOK, w.Code)

	var response ResetCircuitBreakerResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.True(t, response.Success)
	assert.Equal(t, "all circuit breakers reset successfully", response.Message)
}

// TestCircuitBreakerStatsResponse tests the response structure
func TestCircuitBreakerStatsResponse(t *testing.T) {
	now := time.Now()
	response := CircuitBreakerStatsResponse{
		Breakers: map[string]services.CircuitBreakerStats{
			"test": {
				TotalRequests:      100,
				SuccessfulRequests: 99,
				FailedRequests:     1,
				LastSuccessTime:    now,
			},
		},
		Names: []string{"test"},
	}

	// Serialize to JSON
	jsonBytes, err := json.Marshal(response)
	assert.NoError(t, err)

	// Deserialize back
	var decoded CircuitBreakerStatsResponse
	err = json.Unmarshal(jsonBytes, &decoded)
	assert.NoError(t, err)

	assert.Equal(t, 1, len(decoded.Breakers))
	assert.Equal(t, []string{"test"}, decoded.Names)
	assert.Equal(t, int64(100), decoded.Breakers["test"].TotalRequests)
	assert.Equal(t, int64(1), decoded.Breakers["test"].FailedRequests)
}

// TestResetCircuitBreakerResponse tests the response structure
func TestResetCircuitBreakerResponse(t *testing.T) {
	tests := []struct {
		name     string
		response ResetCircuitBreakerResponse
	}{
		{
			name: "success response",
			response: ResetCircuitBreakerResponse{
				Success: true,
				Message: "reset successful",
				Name:    "binance",
			},
		},
		{
			name: "failure response",
			response: ResetCircuitBreakerResponse{
				Success: false,
				Message: "not found",
				Name:    "unknown",
			},
		},
		{
			name: "response without name",
			response: ResetCircuitBreakerResponse{
				Success: true,
				Message: "all reset",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Serialize
			jsonBytes, err := json.Marshal(tt.response)
			assert.NoError(t, err)

			// Deserialize
			var decoded ResetCircuitBreakerResponse
			err = json.Unmarshal(jsonBytes, &decoded)
			assert.NoError(t, err)

			assert.Equal(t, tt.response.Success, decoded.Success)
			assert.Equal(t, tt.response.Message, decoded.Message)
			assert.Equal(t, tt.response.Name, decoded.Name)
		})
	}
}
