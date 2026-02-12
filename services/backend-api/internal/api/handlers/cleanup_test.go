package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockCleanupService mocks the CleanupService
type MockCleanupService struct {
	mock.Mock
}

// NewMockCleanupService creates a new mock cleanup service
func NewMockCleanupService() *MockCleanupService {
	return &MockCleanupService{}
}

func (m *MockCleanupService) GetDataStats(ctx context.Context) (map[string]int64, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(map[string]int64), args.Error(1)
}

func (m *MockCleanupService) RunCleanup(config config.CleanupConfig) error {
	args := m.Called(config)
	return args.Error(0)
}

func TestNewCleanupHandler(t *testing.T) {
	mockService := NewMockCleanupService()
	handler := NewCleanupHandler(mockService)

	assert.NotNil(t, handler)
	assert.Equal(t, mockService, handler.cleanupService)
}

func TestCleanupHandler_GetDataStats(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		stats          map[string]int64
		statsError     error
		expectedStatus int
		expectError    bool
	}{
		{
			name: "Success",
			stats: map[string]int64{
				"market_data_count":                     1000,
				"funding_rates_count":                   500,
				"arbitrage_opportunities_count":         200,
				"funding_arbitrage_opportunities_count": 100,
			},
			statsError:     nil,
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Service error",
			stats:          nil,
			statsError:     assert.AnError,
			expectedStatus: http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := NewMockCleanupService()
			handler := NewCleanupHandler(mockService)

			mockService.On("GetDataStats", mock.Anything).Return(tt.stats, tt.statsError)

			// Create request
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest("GET", "/api/data/stats", nil)
			c.Request = req

			// Execute
			handler.GetDataStats(c)

			// Assert
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectError {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.NotEmpty(t, response["error"])
			} else {
				var response DataStatsResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Equal(t, int64(1000), response.MarketDataCount)
				assert.Equal(t, int64(500), response.FundingRatesCount)
				assert.Equal(t, int64(200), response.ArbitrageOpportunitiesCount)
				assert.Equal(t, int64(100), response.FundingArbitrageOpportunitiesCount)
				assert.Equal(t, int64(1800), response.TotalRecords) // Sum of all counts
			}

			mockService.AssertExpectations(t)
		})
	}
}

func TestCleanupHandler_TriggerCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name              string
		queryParams       string
		cleanupError      error
		statsAfterCleanup map[string]int64
		statsError        error
		expectedStatus    int
		expectError       bool
	}{
		{
			name:         "Success with default parameters",
			queryParams:  "",
			cleanupError: nil,
			statsAfterCleanup: map[string]int64{
				"market_data_count":                     800,
				"funding_rates_count":                   400,
				"arbitrage_opportunities_count":         150,
				"funding_arbitrage_opportunities_count": 80,
			},
			statsError:     nil,
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name:         "Success with custom parameters",
			queryParams:  "?market_data_hours=48&funding_rate_hours=36&arbitrage_hours=96",
			cleanupError: nil,
			statsAfterCleanup: map[string]int64{
				"market_data_count":                     900,
				"funding_rates_count":                   450,
				"arbitrage_opportunities_count":         180,
				"funding_arbitrage_opportunities_count": 90,
			},
			statsError:     nil,
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name:           "Cleanup service error",
			queryParams:    "",
			cleanupError:   assert.AnError,
			expectedStatus: http.StatusInternalServerError,
			expectError:    true,
		},
		{
			name:         "Cleanup success but stats error",
			queryParams:  "",
			cleanupError: nil,
			statsAfterCleanup: map[string]int64{
				"market_data_count":                     800,
				"funding_rates_count":                   400,
				"arbitrage_opportunities_count":         150,
				"funding_arbitrage_opportunities_count": 80,
			},
			statsError:     assert.AnError,
			expectedStatus: http.StatusInternalServerError,
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := NewMockCleanupService()
			handler := NewCleanupHandler(mockService)

			// Setup expectations
			if tt.cleanupError == nil {
				mockService.On("RunCleanup", mock.AnythingOfType("config.CleanupConfig")).Return(tt.cleanupError)
				mockService.On("GetDataStats", mock.Anything).Return(tt.statsAfterCleanup, tt.statsError)
			} else {
				mockService.On("RunCleanup", mock.AnythingOfType("config.CleanupConfig")).Return(tt.cleanupError)
			}

			// Create request
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest("POST", "/api/data/cleanup"+tt.queryParams, nil)
			c.Request = req

			// Execute
			handler.TriggerCleanup(c)

			// Assert
			assert.Equal(t, tt.expectedStatus, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)

			if tt.expectError {
				assert.NotEmpty(t, response["error"])
			} else {
				assert.Equal(t, "Cleanup completed successfully", response["message"])
				assert.NotNil(t, response["stats"])
			}

			mockService.AssertExpectations(t)
		})
	}
}

func TestCleanupHandler_TriggerCleanup_ParameterParsing(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name                     string
		queryParams              string
		expectedMarketDataHours  int
		expectedFundingRateHours int
		expectedArbitrageHours   int
	}{
		{
			name:                     "Default values",
			queryParams:              "",
			expectedMarketDataHours:  24,
			expectedFundingRateHours: 24,
			expectedArbitrageHours:   72,
		},
		{
			name:                     "Custom valid values",
			queryParams:              "?market_data_hours=48&funding_rate_hours=36&arbitrage_hours=96",
			expectedMarketDataHours:  48,
			expectedFundingRateHours: 36,
			expectedArbitrageHours:   96,
		},
		{
			name:                     "Invalid values should use defaults",
			queryParams:              "?market_data_hours=invalid&funding_rate_hours=-1&arbitrage_hours=0",
			expectedMarketDataHours:  24,
			expectedFundingRateHours: 24,
			expectedArbitrageHours:   72,
		},
		{
			name:                     "Partial custom values",
			queryParams:              "?market_data_hours=12",
			expectedMarketDataHours:  12,
			expectedFundingRateHours: 24,
			expectedArbitrageHours:   72,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockService := NewMockCleanupService()
			handler := NewCleanupHandler(mockService)

			// Setup expectations - we'll verify the config passed to RunCleanup
			mockService.On("RunCleanup", mock.MatchedBy(func(config config.CleanupConfig) bool {
				return config.MarketData.RetentionHours == tt.expectedMarketDataHours &&
					config.FundingRates.RetentionHours == tt.expectedFundingRateHours &&
					config.ArbitrageOpportunities.RetentionHours == tt.expectedArbitrageHours
			})).Return(nil)

			mockService.On("GetDataStats", mock.Anything).Return(map[string]int64{
				"market_data_count":                     100,
				"funding_rates_count":                   50,
				"arbitrage_opportunities_count":         25,
				"funding_arbitrage_opportunities_count": 10,
			}, nil)

			// Create request
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req := httptest.NewRequest("POST", "/api/data/cleanup"+tt.queryParams, nil)
			c.Request = req

			// Execute
			handler.TriggerCleanup(c)

			// Assert
			assert.Equal(t, http.StatusOK, w.Code)

			mockService.AssertExpectations(t)
		})
	}
}

func TestParseIntParam(t *testing.T) {
	tests := []struct {
		name        string
		param       string
		expected    int
		expectError bool
	}{
		{
			name:        "Valid positive integer",
			param:       "42",
			expected:    42,
			expectError: false,
		},
		{
			name:        "Valid zero",
			param:       "0",
			expected:    0,
			expectError: false,
		},
		{
			name:        "Valid negative integer",
			param:       "-10",
			expected:    -10,
			expectError: false,
		},
		{
			name:        "Invalid string",
			param:       "invalid",
			expected:    0,
			expectError: true,
		},
		{
			name:        "Empty string",
			param:       "",
			expected:    0,
			expectError: true,
		},
		{
			name:        "Float string",
			param:       "3.14",
			expected:    0,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseIntParam(tt.param)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}
