package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to convert mock opportunity to model type
func convertMockToModelOpportunity(mock *MockFuturesArbitrageOpportunity) *models.FuturesArbitrageOpportunity {
	return &models.FuturesArbitrageOpportunity{
		ID:                        mock.ID,
		Symbol:                    mock.Symbol,
		BaseCurrency:              mock.BaseCurrency,
		QuoteCurrency:             mock.QuoteCurrency,
		LongExchange:              mock.LongExchange,
		ShortExchange:             mock.ShortExchange,
		LongExchangeID:            int(mock.LongExchangeID),
		ShortExchangeID:           int(mock.ShortExchangeID),
		LongFundingRate:           mock.LongFundingRate,
		ShortFundingRate:          mock.ShortFundingRate,
		NetFundingRate:            mock.NetFundingRate,
		FundingInterval:           mock.FundingInterval,
		LongMarkPrice:             mock.LongMarkPrice,
		ShortMarkPrice:            mock.ShortMarkPrice,
		PriceDifference:           mock.PriceDifference,
		PriceDifferencePercentage: mock.PriceDifferencePercentage,
		HourlyRate:                mock.HourlyRate,
		DailyRate:                 mock.DailyRate,
		APY:                       mock.APY,
		EstimatedProfit8h:         mock.EstimatedProfit8h,
		EstimatedProfitDaily:      mock.EstimatedProfitDaily,
		EstimatedProfitWeekly:     mock.EstimatedProfitWeekly,
		EstimatedProfitMonthly:    mock.EstimatedProfitMonthly,
		RiskScore:                 mock.RiskScore,
		VolatilityScore:           mock.VolatilityScore,
		LiquidityScore:            mock.LiquidityScore,
		RecommendedPositionSize:   mock.RecommendedPositionSize,
		MaxLeverage:               mock.MaxLeverage,
		RecommendedLeverage:       mock.RecommendedLeverage,
		StopLossPercentage:        mock.StopLossPercentage,
		MinPositionSize:           mock.MinPositionSize,
		MaxPositionSize:           mock.MaxPositionSize,
		OptimalPositionSize:       mock.OptimalPositionSize,
		DetectedAt:                mock.DetectedAt,
		ExpiresAt:                 mock.ExpiresAt,
		NextFundingTime:           mock.NextFundingTime,
		TimeToNextFunding:         mock.TimeToNextFunding,
		IsActive:                  mock.IsActive,
		MarketTrend:               mock.MarketTrend,
		Volume24h:                 mock.Volume24h,
		OpenInterest:              mock.OpenInterest,
	}
}

// Helper function to convert mock risk metrics to model type
func convertMockToModelRiskMetrics(mock *MockFuturesArbitrageRiskMetrics) *models.FuturesArbitrageRiskMetrics {
	return &models.FuturesArbitrageRiskMetrics{
		PriceCorrelation:      mock.PriceCorrelation,
		PriceVolatility:       mock.PriceVolatility,
		MaxDrawdown:           mock.MaxDrawdown,
		FundingRateVolatility: mock.FundingRateVolatility,
		FundingRateStability:  mock.FundingRateStability,
		BidAskSpread:          mock.BidAskSpread,
		MarketDepth:           mock.MarketDepth,
		SlippageRisk:          mock.SlippageRisk,
		ExchangeReliability:   mock.ExchangeReliability,
		CounterpartyRisk:      mock.CounterpartyRisk,
		OverallRiskScore:      mock.OverallRiskScore,
		RiskCategory:          mock.RiskCategory,
		Recommendation:        mock.Recommendation,
	}
}

// Helper function to convert mock input to model type
func convertMockToModelInput(mock MockFuturesArbitrageCalculationInput) models.FuturesArbitrageCalculationInput {
	return models.FuturesArbitrageCalculationInput{
		Symbol:             mock.Symbol,
		LongExchange:       mock.LongExchange,
		ShortExchange:      mock.ShortExchange,
		LongFundingRate:    mock.LongFundingRate,
		ShortFundingRate:   mock.ShortFundingRate,
		LongMarkPrice:      mock.LongMarkPrice,
		ShortMarkPrice:     mock.ShortMarkPrice,
		BaseAmount:         mock.BaseAmount,
		UserRiskTolerance:  mock.UserRiskTolerance,
		MaxLeverageAllowed: mock.MaxLeverageAllowed,
		AvailableCapital:   mock.AvailableCapital,
		FundingInterval:    mock.FundingInterval,
	}
}

// Helper function to convert mock request to model type
func convertMockToModelRequest(mock MockFuturesArbitrageRequest) models.FuturesArbitrageRequest {
	return models.FuturesArbitrageRequest{
		Symbols:               mock.Symbols,
		Exchanges:             mock.Exchanges,
		MinAPY:                mock.MinAPY,
		MaxRiskScore:          mock.MaxRiskScore,
		RiskTolerance:         mock.RiskTolerance,
		AvailableCapital:      mock.AvailableCapital,
		MaxLeverage:           mock.MaxLeverage,
		TimeHorizon:           mock.TimeHorizon,
		IncludeRiskMetrics:    mock.IncludeRiskMetrics,
		IncludePositionSizing: mock.IncludePositionSizing,
		Limit:                 mock.Limit,
		Page:                  mock.Page,
	}
}

// Helper function to convert mock opportunities to model type
func convertMockToModelOpportunities(mocks []MockFuturesArbitrageOpportunity) []models.FuturesArbitrageOpportunity {
	result := make([]models.FuturesArbitrageOpportunity, len(mocks))
	for i, mock := range mocks {
		result[i] = *convertMockToModelOpportunity(&mock)
	}
	return result
}

// Mock models to avoid circular dependency
type MockFuturesArbitrageOpportunity struct {
	ID                        string          `json:"id"`
	Symbol                    string          `json:"symbol"`
	BaseCurrency              string          `json:"base_currency"`
	QuoteCurrency             string          `json:"quote_currency"`
	LongExchange              string          `json:"long_exchange"`
	ShortExchange             string          `json:"short_exchange"`
	LongExchangeID            int64           `json:"long_exchange_id"`
	ShortExchangeID           int64           `json:"short_exchange_id"`
	LongFundingRate           decimal.Decimal `json:"long_funding_rate"`
	ShortFundingRate          decimal.Decimal `json:"short_funding_rate"`
	NetFundingRate            decimal.Decimal `json:"net_funding_rate"`
	FundingInterval           int             `json:"funding_interval"`
	LongMarkPrice             decimal.Decimal `json:"long_mark_price"`
	ShortMarkPrice            decimal.Decimal `json:"short_mark_price"`
	PriceDifference           decimal.Decimal `json:"price_difference"`
	PriceDifferencePercentage decimal.Decimal `json:"price_difference_percentage"`
	HourlyRate                decimal.Decimal `json:"hourly_rate"`
	DailyRate                 decimal.Decimal `json:"daily_rate"`
	APY                       decimal.Decimal `json:"apy"`
	EstimatedProfit8h         decimal.Decimal `json:"estimated_profit_8h"`
	EstimatedProfitDaily      decimal.Decimal `json:"estimated_profit_daily"`
	EstimatedProfitWeekly     decimal.Decimal `json:"estimated_profit_weekly"`
	EstimatedProfitMonthly    decimal.Decimal `json:"estimated_profit_monthly"`
	RiskScore                 decimal.Decimal `json:"risk_score"`
	VolatilityScore           decimal.Decimal `json:"volatility_score"`
	LiquidityScore            decimal.Decimal `json:"liquidity_score"`
	RecommendedPositionSize   decimal.Decimal `json:"recommended_position_size"`
	MaxLeverage               decimal.Decimal `json:"max_leverage"`
	RecommendedLeverage       decimal.Decimal `json:"recommended_leverage"`
	StopLossPercentage        decimal.Decimal `json:"stop_loss_percentage"`
	MinPositionSize           decimal.Decimal `json:"min_position_size"`
	MaxPositionSize           decimal.Decimal `json:"max_position_size"`
	OptimalPositionSize       decimal.Decimal `json:"optimal_position_size"`
	DetectedAt                time.Time       `json:"detected_at"`
	ExpiresAt                 time.Time       `json:"expires_at"`
	NextFundingTime           time.Time       `json:"next_funding_time"`
	TimeToNextFunding         int             `json:"time_to_next_funding"`
	IsActive                  bool            `json:"is_active"`
	MarketTrend               string          `json:"market_trend"`
	Volume24h                 decimal.Decimal `json:"volume_24h"`
	OpenInterest              decimal.Decimal `json:"open_interest"`
}

type MockFuturesArbitrageRequest struct {
	Symbols               []string        `json:"symbols,omitempty"`
	Exchanges             []string        `json:"exchanges,omitempty"`
	MinAPY                decimal.Decimal `json:"min_apy,omitempty"`
	MaxRiskScore          decimal.Decimal `json:"max_risk_score,omitempty"`
	RiskTolerance         string          `json:"risk_tolerance,omitempty"`
	AvailableCapital      decimal.Decimal `json:"available_capital,omitempty"`
	MaxLeverage           decimal.Decimal `json:"max_leverage,omitempty"`
	TimeHorizon           string          `json:"time_horizon,omitempty"`
	IncludeRiskMetrics    bool            `json:"include_risk_metrics,omitempty"`
	IncludePositionSizing bool            `json:"include_position_sizing,omitempty"`
	Limit                 int             `json:"limit,omitempty"`
	Page                  int             `json:"page,omitempty"`
}

type MockFuturesArbitrageRiskMetrics struct {
	PriceCorrelation      decimal.Decimal `json:"price_correlation"`
	PriceVolatility       decimal.Decimal `json:"price_volatility"`
	MaxDrawdown           decimal.Decimal `json:"max_drawdown"`
	FundingRateVolatility decimal.Decimal `json:"funding_rate_volatility"`
	FundingRateStability  decimal.Decimal `json:"funding_rate_stability"`
	BidAskSpread          decimal.Decimal `json:"bid_ask_spread"`
	MarketDepth           decimal.Decimal `json:"market_depth"`
	SlippageRisk          decimal.Decimal `json:"slippage_risk"`
	ExchangeReliability   decimal.Decimal `json:"exchange_reliability"`
	CounterpartyRisk      decimal.Decimal `json:"counterparty_risk"`
	OverallRiskScore      decimal.Decimal `json:"overall_risk_score"`
	RiskCategory          string          `json:"risk_category"`
	Recommendation        string          `json:"recommendation"`
}

type MockFuturesMarketSummary struct {
	TotalOpportunities  int             `json:"total_opportunities"`
	ActiveOpportunities int             `json:"active_opportunities"`
	AverageAPY          decimal.Decimal `json:"average_apy"`
	HighestAPY          decimal.Decimal `json:"highest_apy"`
	AverageRiskScore    decimal.Decimal `json:"average_risk_score"`
	MarketVolatility    decimal.Decimal `json:"market_volatility"`
	FundingRateTrend    string          `json:"funding_rate_trend"`
	RecommendedStrategy string          `json:"recommended_strategy"`
	MarketCondition     string          `json:"market_condition"`
}

type MockFuturesArbitrageCalculationInput struct {
	Symbol             string          `json:"symbol"`
	LongExchange       string          `json:"long_exchange"`
	ShortExchange      string          `json:"short_exchange"`
	LongFundingRate    decimal.Decimal `json:"long_funding_rate"`
	ShortFundingRate   decimal.Decimal `json:"short_funding_rate"`
	LongMarkPrice      decimal.Decimal `json:"long_mark_price"`
	ShortMarkPrice     decimal.Decimal `json:"short_mark_price"`
	BaseAmount         decimal.Decimal `json:"base_amount"`
	UserRiskTolerance  string          `json:"user_risk_tolerance"`
	MaxLeverageAllowed decimal.Decimal `json:"max_leverage_allowed"`
	AvailableCapital   decimal.Decimal `json:"available_capital"`
	FundingInterval    int             `json:"funding_interval"`
}

type MockFuturesPositionSizing struct {
	RecommendedSize decimal.Decimal `json:"recommended_size"`
	MaxSize         decimal.Decimal `json:"max_size"`
	RiskAmount      decimal.Decimal `json:"risk_amount"`
	LeverageUsed    decimal.Decimal `json:"leverage_used"`
	ExpectedReturn  decimal.Decimal `json:"expected_return"`
	PotentialLoss   decimal.Decimal `json:"potential_loss"`
	RiskRewardRatio decimal.Decimal `json:"risk_reward_ratio"`
	ConfidenceLevel decimal.Decimal `json:"confidence_level"`
}

type MockFuturesArbitrageResponse struct {
	Success        bool                             `json:"success"`
	Opportunity    *MockFuturesArbitrageOpportunity `json:"opportunity"`
	RiskMetrics    *MockFuturesArbitrageRiskMetrics `json:"risk_metrics"`
	PositionSizing MockFuturesPositionSizing        `json:"position_sizing"`
	Message        string                           `json:"message"`
	Timestamp      time.Time                        `json:"timestamp"`
}

func setupTestHandler(t *testing.T) (*FuturesArbitrageHandler, pgxmock.PgxPoolIface) {
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	// Use the NewFuturesArbitrageHandlerWithQuerier constructor
	handler := NewFuturesArbitrageHandlerWithQuerier(mock)
	return handler, mock
}

func TestCalculateFuturesArbitrage(t *testing.T) {
	handler, mockDB := setupTestHandler(t)
	defer mockDB.Close()

	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		requestBody    MockFuturesArbitrageCalculationInput
		expectedStatus int
		setupMock      func(pgxmock.PgxPoolIface)
	}{
		{
			name: "successful calculation",
			requestBody: MockFuturesArbitrageCalculationInput{
				Symbol:             "BTC/USDT",
				LongExchange:       "binance",
				ShortExchange:      "okx",
				LongFundingRate:    decimal.NewFromFloat(0.01),
				ShortFundingRate:   decimal.NewFromFloat(-0.005),
				LongMarkPrice:      decimal.NewFromFloat(50000),
				ShortMarkPrice:     decimal.NewFromFloat(50100),
				BaseAmount:         decimal.NewFromFloat(1.0),
				FundingInterval:    8,
				AvailableCapital:   decimal.NewFromFloat(10000),
				UserRiskTolerance:  "medium",
				MaxLeverageAllowed: decimal.NewFromFloat(10),
			},
			expectedStatus: http.StatusOK,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// No database operations expected since storeFuturesOpportunity is a stub
			},
		},
		{
			name: "invalid request body",
			requestBody: MockFuturesArbitrageCalculationInput{
				// Missing required fields
				Symbol: "",
			},
			expectedStatus: http.StatusBadRequest,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// No database interaction expected for invalid input
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mockDB)

			// Create request body
			body, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			// Create HTTP request
			req := httptest.NewRequest(http.MethodPost, "/api/futures-arbitrage/calculate", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			// Create response recorder
			w := httptest.NewRecorder()

			// Create Gin context
			c, _ := gin.CreateTestContext(w)
			c.Request = req

			// Call handler
			handler.CalculateFuturesArbitrage(c)

			// Check response
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response struct {
					Opportunity *MockFuturesArbitrageOpportunity `json:"opportunity"`
					RiskMetrics *MockFuturesArbitrageRiskMetrics `json:"risk_metrics"`
					Timestamp   time.Time                        `json:"timestamp"`
				}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Equal(t, tt.requestBody.Symbol, response.Opportunity.Symbol)
				assert.Equal(t, tt.requestBody.LongExchange, response.Opportunity.LongExchange)
				assert.Equal(t, tt.requestBody.ShortExchange, response.Opportunity.ShortExchange)
			}

			// Ensure all expectations were met
			assert.NoError(t, mockDB.ExpectationsWereMet())
		})
	}
}

func TestGetFuturesArbitrageOpportunities(t *testing.T) {
	handler, mockDB := setupTestHandler(t)
	defer mockDB.Close()

	gin.SetMode(gin.TestMode)

	// Mock data
	mockOpportunities := []MockFuturesArbitrageOpportunity{
		{
			ID:               "1",
			Symbol:           "BTC/USDT",
			BaseCurrency:     "BTC",
			QuoteCurrency:    "USDT",
			LongExchange:     "binance",
			ShortExchange:    "okx",
			LongFundingRate:  decimal.NewFromFloat(0.01),
			ShortFundingRate: decimal.NewFromFloat(-0.005),
			NetFundingRate:   decimal.NewFromFloat(0.015),
			APY:              decimal.NewFromFloat(131.4),
			HourlyRate:       decimal.NewFromFloat(0.015),
			DailyRate:        decimal.NewFromFloat(0.36),
			IsActive:         true,
			DetectedAt:       time.Now(),
			ExpiresAt:        time.Now().Add(time.Hour * 8),
		},
	}

	tests := []struct {
		name           string
		queryParams    string
		expectedStatus int
		setupMock      func(pgxmock.PgxPoolIface)
	}{
		{
			name:           "successful retrieval",
			queryParams:    "",
			expectedStatus: http.StatusOK,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "symbol", "base_currency", "quote_currency",
					"long_exchange", "short_exchange", "long_exchange_id", "short_exchange_id",
					"long_funding_rate", "short_funding_rate", "net_funding_rate", "funding_interval",
					"long_mark_price", "short_mark_price", "price_difference", "price_difference_percentage",
					"hourly_rate", "daily_rate", "apy",
					"estimated_profit_8h", "estimated_profit_daily", "estimated_profit_weekly", "estimated_profit_monthly",
					"risk_score", "volatility_score", "liquidity_score",
					"recommended_position_size", "max_leverage", "recommended_leverage", "stop_loss_percentage",
					"min_position_size", "max_position_size", "optimal_position_size",
					"detected_at", "expires_at", "next_funding_time", "time_to_next_funding", "is_active",
					"market_trend", "volume_24h", "open_interest",
				}).AddRow(
					mockOpportunities[0].ID,
					mockOpportunities[0].Symbol,
					mockOpportunities[0].BaseCurrency,
					mockOpportunities[0].QuoteCurrency,
					mockOpportunities[0].LongExchange,
					mockOpportunities[0].ShortExchange,
					1, 2, // Mock exchange IDs
					mockOpportunities[0].LongFundingRate,
					mockOpportunities[0].ShortFundingRate,
					mockOpportunities[0].NetFundingRate,
					8,                                                        // funding_interval
					decimal.NewFromFloat(50000), decimal.NewFromFloat(50100), // mark prices
					decimal.NewFromFloat(100), decimal.NewFromFloat(0.2), // price difference
					mockOpportunities[0].HourlyRate,
					mockOpportunities[0].DailyRate,
					mockOpportunities[0].APY,
					decimal.NewFromFloat(10), decimal.NewFromFloat(30), decimal.NewFromFloat(210), decimal.NewFromFloat(900), // estimated profits
					decimal.NewFromFloat(0.3), decimal.NewFromFloat(0.2), decimal.NewFromFloat(0.8), // risk scores
					decimal.NewFromFloat(1000), decimal.NewFromFloat(10), decimal.NewFromFloat(5), decimal.NewFromFloat(2), // position sizing
					decimal.NewFromFloat(100), decimal.NewFromFloat(5000), decimal.NewFromFloat(1000), // position limits
					mockOpportunities[0].DetectedAt,
					mockOpportunities[0].ExpiresAt,
					time.Now().Add(time.Hour), time.Duration(3600000000000), // next funding time
					mockOpportunities[0].IsActive,
					"bullish", decimal.NewFromFloat(1000000), decimal.NewFromFloat(50000000), // market data
				)

				mock.ExpectQuery("SELECT (.+) FROM futures_arbitrage_opportunities WHERE is_active = true AND expires_at > NOW\\(\\) ORDER BY apy DESC, risk_score ASC LIMIT \\$1 OFFSET \\$2").WithArgs(50, 0).WillReturnRows(rows)
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM futures_arbitrage_opportunities WHERE is_active = true AND expires_at > NOW\\(\\)").WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
			},
		},
		{
			name:           "with symbol filter",
			queryParams:    "?symbols=BTC/USDT",
			expectedStatus: http.StatusOK,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "symbol", "base_currency", "quote_currency",
					"long_exchange", "short_exchange", "long_exchange_id", "short_exchange_id",
					"long_funding_rate", "short_funding_rate", "net_funding_rate", "funding_interval",
					"long_mark_price", "short_mark_price", "price_difference", "price_difference_percentage",
					"hourly_rate", "daily_rate", "apy",
					"estimated_profit_8h", "estimated_profit_daily", "estimated_profit_weekly", "estimated_profit_monthly",
					"risk_score", "volatility_score", "liquidity_score",
					"recommended_position_size", "max_leverage", "recommended_leverage", "stop_loss_percentage",
					"min_position_size", "max_position_size", "optimal_position_size",
					"detected_at", "expires_at", "next_funding_time", "time_to_next_funding", "is_active",
					"market_trend", "volume_24h", "open_interest",
				})

				mock.ExpectQuery("SELECT (.+) FROM futures_arbitrage_opportunities WHERE is_active = true AND expires_at > NOW\\(\\) AND symbol = ANY\\(\\$1\\) ORDER BY apy DESC, risk_score ASC LIMIT \\$2 OFFSET \\$3").WithArgs([]string{"BTC/USDT"}, 50, 0).WillReturnRows(rows)
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM futures_arbitrage_opportunities WHERE is_active = true AND expires_at > NOW\\(\\)").WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))
			},
		},
		{
			name:           "database error handling",
			queryParams:    "",
			expectedStatus: http.StatusInternalServerError,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// Simulate database error
				mock.ExpectQuery("SELECT (.+) FROM futures_arbitrage_opportunities WHERE is_active = true AND expires_at > NOW\\(\\) ORDER BY apy DESC, risk_score ASC LIMIT \\$1 OFFSET \\$2").
					WithArgs(50, 0).
					WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
		{
			name:           "with strategies inclusion",
			queryParams:    "?include_strategies=true",
			expectedStatus: http.StatusOK,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// Mock market data query for strategies calculation
				marketRows := pgxmock.NewRows([]string{
					"id", "symbol", "base_currency", "quote_currency",
					"long_exchange", "short_exchange", "long_exchange_id", "short_exchange_id",
					"long_funding_rate", "short_funding_rate", "net_funding_rate", "funding_interval",
					"long_mark_price", "short_mark_price", "price_difference", "price_difference_percentage",
					"hourly_rate", "daily_rate", "apy",
					"estimated_profit_8h", "estimated_profit_daily", "estimated_profit_weekly", "estimated_profit_monthly",
					"risk_score", "volatility_score", "liquidity_score",
					"recommended_position_size", "max_leverage", "recommended_leverage", "stop_loss_percentage",
					"min_position_size", "max_position_size", "optimal_position_size",
					"detected_at", "expires_at", "next_funding_time", "time_to_next_funding", "is_active",
					"market_trend", "volume_24h", "open_interest",
				}).AddRow(
					1, "BTC/USDT", "BTC", "USDT",
					"Binance", "Bybit", 1, 2,
					0.0001, -0.0002, -0.0003, 8,
					50000.0, 50010.0, 10.0, 0.02,
					0.0000125, 0.0003, 10.95,
					90.0, 2160.0, 15120.0, 64800.0,
					65.0, 45.0, 85.0,
					1000.0, 20.0, 5.0, 2.0,
					100.0, 10000.0, 1000.0,
					time.Now(), time.Now().Add(24*time.Hour), time.Now().Add(2*time.Hour), 2*time.Hour, true,
					"bullish", decimal.NewFromFloat(1000000), decimal.NewFromFloat(50000000),
				)

				mock.ExpectQuery("SELECT (.+) FROM futures_arbitrage_opportunities WHERE is_active = true AND expires_at > NOW\\(\\) ORDER BY apy DESC, risk_score ASC LIMIT \\$1 OFFSET \\$2").
					WithArgs(50, 0).
					WillReturnRows(marketRows)
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM futures_arbitrage_opportunities WHERE is_active = true AND expires_at > NOW\\(\\)").
					WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mockDB)

			// Create HTTP request
			req := httptest.NewRequest(http.MethodGet, "/api/futures-arbitrage/opportunities"+tt.queryParams, nil)

			// Create response recorder
			w := httptest.NewRecorder()

			// Create Gin context
			c, _ := gin.CreateTestContext(w)
			c.Request = req

			// Set query parameters if any
			if tt.queryParams != "" {
				c.Request.URL.RawQuery = tt.queryParams[1:] // Remove the '?' prefix
			}

			// Call handler
			handler.GetFuturesArbitrageOpportunities(c)

			// Check response
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response MockFuturesArbitrageResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				// Response should be a FuturesArbitrageResponse
				assert.IsType(t, MockFuturesArbitrageResponse{}, response)
			}

			// Ensure all expectations were met
			assert.NoError(t, mockDB.ExpectationsWereMet())
		})
	}
}

func TestNewFuturesArbitrageHandler(t *testing.T) {
	// Note: This function requires a real pgxpool.Pool, not a mock
	// Since we can't easily create a real pgxpool.Pool in unit tests,
	// we'll skip this test and rely on integration tests for coverage

	t.Skip("NewFuturesArbitrageHandler requires real pgxpool.Pool, test in integration suite")
}

func TestNewFuturesArbitrageHandlerWithQuerier(t *testing.T) {
	// Test the constructor with custom querier
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	handler := NewFuturesArbitrageHandlerWithQuerier(mock)

	// Verify handler is properly initialized
	assert.NotNil(t, handler)
	assert.NotNil(t, handler.db)
	assert.NotNil(t, handler.calculator)
	assert.NotNil(t, handler.metrics)
}

func TestGetFuturesArbitrageStrategy(t *testing.T) {
	handler, mockDB := setupTestHandler(t)
	defer mockDB.Close()

	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		strategyID     string
		expectedStatus int
		setupMock      func(pgxmock.PgxPoolIface)
	}{
		{
			name:           "strategy found",
			strategyID:     "test-strategy-123",
			expectedStatus: http.StatusOK,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// Mock database query for strategy
				rows := pgxmock.NewRows([]string{
					"id", "opportunity_id", "position_size", "leverage", "entry_price", "stop_loss",
					"take_profit", "risk_score", "expected_profit", "duration_hours", "created_at",
				}).AddRow(
					"test-strategy-123", "opp-123",
					decimal.NewFromFloat(1000), decimal.NewFromFloat(5),
					decimal.NewFromFloat(50000), decimal.NewFromFloat(49000),
					decimal.NewFromFloat(51000), decimal.NewFromFloat(0.3),
					decimal.NewFromFloat(500), 24, time.Now(),
				)
				mock.ExpectQuery("SELECT (.+) FROM futures_arbitrage_strategies WHERE id = \\$1").WithArgs("test-strategy-123").WillReturnRows(rows)
			},
		},
		{
			name:           "strategy not found",
			strategyID:     "non-existent-strategy",
			expectedStatus: http.StatusNotFound,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// Mock no rows found
				mock.ExpectQuery("SELECT (.+) FROM futures_arbitrage_strategies WHERE id = \\$1").WithArgs("non-existent-strategy").WillReturnRows(pgxmock.NewRows([]string{}))
			},
		},
		{
			name:           "database error",
			strategyID:     "error-strategy",
			expectedStatus: http.StatusInternalServerError,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// Mock database error (use a different error than pgx.ErrNoRows)
				mock.ExpectQuery("SELECT (.+) FROM futures_arbitrage_strategies WHERE id = \\$1").WithArgs("error-strategy").WillReturnError(fmt.Errorf("database connection failed"))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mockDB)

			// Create HTTP request
			req := httptest.NewRequest(http.MethodGet, "/api/futures-arbitrage/strategy/"+tt.strategyID, nil)

			// Create response recorder
			w := httptest.NewRecorder()

			// Create Gin context with parameters
			c, _ := gin.CreateTestContext(w)
			c.Request = req
			c.Params = gin.Params{gin.Param{Key: "id", Value: tt.strategyID}}

			// Call handler
			handler.GetFuturesArbitrageStrategy(c)

			// Check response
			assert.Equal(t, tt.expectedStatus, w.Code)

			// Ensure all expectations were met
			assert.NoError(t, mockDB.ExpectationsWereMet())
		})
	}
}

func TestGetPositionSizingRecommendation(t *testing.T) {
	handler, mockDB := setupTestHandler(t)
	defer mockDB.Close()

	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		requestBody    MockFuturesArbitrageCalculationInput
		expectedStatus int
		setupMock      func(pgxmock.PgxPoolIface)
	}{
		{
			name: "successful position sizing calculation",
			requestBody: MockFuturesArbitrageCalculationInput{
				Symbol:             "BTC/USDT",
				LongExchange:       "binance",
				ShortExchange:      "okx",
				LongMarkPrice:      decimal.NewFromFloat(50000),
				ShortMarkPrice:     decimal.NewFromFloat(50100),
				BaseAmount:         decimal.NewFromFloat(1.0),
				FundingInterval:    8,
				AvailableCapital:   decimal.NewFromFloat(10000),
				UserRiskTolerance:  "medium",
				MaxLeverageAllowed: decimal.NewFromFloat(10),
			},
			expectedStatus: http.StatusOK,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// No database operations expected for position sizing
			},
		},
		{
			name: "invalid request body",
			requestBody: MockFuturesArbitrageCalculationInput{
				// Missing required fields
				Symbol: "",
			},
			expectedStatus: http.StatusBadRequest,
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// No database interaction expected for invalid input
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setupMock(mockDB)

			// Create request body
			body, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			// Create HTTP request
			req := httptest.NewRequest(http.MethodPost, "/api/futures-arbitrage/position-sizing", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")

			// Create response recorder
			w := httptest.NewRecorder()

			// Create Gin context
			c, _ := gin.CreateTestContext(w)
			c.Request = req

			// Call handler
			handler.GetPositionSizingRecommendation(c)

			// Check response
			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.expectedStatus == http.StatusOK {
				var response struct {
					PositionSizing MockFuturesPositionSizing `json:"position_sizing"`
					RiskScore      decimal.Decimal           `json:"risk_score"`
					Timestamp      time.Time                 `json:"timestamp"`
				}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.NotZero(t, response.RiskScore)
				assert.NotZero(t, response.Timestamp)
			}

			// Ensure all expectations were met
			assert.NoError(t, mockDB.ExpectationsWereMet())
		})
	}
}

func TestParseArbitrageRequest(t *testing.T) {
	handler, mockDB := setupTestHandler(t)
	defer mockDB.Close()

	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		queryParams    string
		expectedResult MockFuturesArbitrageRequest
	}{
		{
			name:        "default values",
			queryParams: "",
			expectedResult: MockFuturesArbitrageRequest{
				Limit: 50,
				Page:  1,
			},
		},
		{
			name:        "with symbol filter",
			queryParams: "?symbols=BTC/USDT",
			expectedResult: MockFuturesArbitrageRequest{
				Limit:   50,
				Page:    1,
				Symbols: []string{"BTC/USDT"},
			},
		},
		{
			name:        "with exchange filter",
			queryParams: "?exchanges=binance,okx",
			expectedResult: MockFuturesArbitrageRequest{
				Limit:     50,
				Page:      1,
				Exchanges: []string{"binance,okx"},
			},
		},
		{
			name:        "with min APY",
			queryParams: "?min_apy=10.5",
			expectedResult: MockFuturesArbitrageRequest{
				Limit:  50,
				Page:   1,
				MinAPY: decimal.NewFromFloat(10.5),
			},
		},
		{
			name:        "with risk tolerance",
			queryParams: "?risk_tolerance=high",
			expectedResult: MockFuturesArbitrageRequest{
				Limit:         50,
				Page:          1,
				RiskTolerance: "high",
			},
		},
		{
			name:        "with pagination",
			queryParams: "?limit=100&page=2",
			expectedResult: MockFuturesArbitrageRequest{
				Limit: 100,
				Page:  2,
			},
		},
		{
			name:        "with risk metrics",
			queryParams: "?include_risk_metrics=true&include_position_sizing=true",
			expectedResult: MockFuturesArbitrageRequest{
				Limit:                 50,
				Page:                  1,
				IncludeRiskMetrics:    true,
				IncludePositionSizing: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create HTTP request
			req := httptest.NewRequest(http.MethodGet, "/api/futures-arbitrage/opportunities"+tt.queryParams, nil)

			// Create Gin context
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = req

			// Set query parameters if any
			if tt.queryParams != "" {
				c.Request.URL.RawQuery = tt.queryParams[1:] // Remove the '?' prefix
			}

			// Call the method under test
			result := handler.parseArbitrageRequest(c)

			// Verify the result
			assert.Equal(t, tt.expectedResult.Limit, result.Limit)
			assert.Equal(t, tt.expectedResult.Page, result.Page)

			if tt.expectedResult.Symbols != nil {
				assert.Equal(t, tt.expectedResult.Symbols, result.Symbols)
			}

			if tt.expectedResult.Exchanges != nil {
				assert.Equal(t, tt.expectedResult.Exchanges, result.Exchanges)
			}

			if !tt.expectedResult.MinAPY.IsZero() {
				assert.True(t, tt.expectedResult.MinAPY.Equal(result.MinAPY))
			}

			if tt.expectedResult.RiskTolerance != "" {
				assert.Equal(t, tt.expectedResult.RiskTolerance, result.RiskTolerance)
			}

			if tt.expectedResult.IncludeRiskMetrics {
				assert.True(t, result.IncludeRiskMetrics)
			}

			if tt.expectedResult.IncludePositionSizing {
				assert.True(t, result.IncludePositionSizing)
			}
		})
	}
}

func TestValidateCalculationInput(t *testing.T) {
	handler, mockDB := setupTestHandler(t)
	defer mockDB.Close()

	tests := []struct {
		name        string
		input       MockFuturesArbitrageCalculationInput
		expectError bool
	}{
		{
			name: "valid input",
			input: MockFuturesArbitrageCalculationInput{
				Symbol:          "BTC/USDT",
				LongExchange:    "binance",
				ShortExchange:   "okx",
				LongMarkPrice:   decimal.NewFromFloat(50000),
				ShortMarkPrice:  decimal.NewFromFloat(50100),
				BaseAmount:      decimal.NewFromFloat(1.0),
				FundingInterval: 8,
			},
			expectError: false,
		},
		{
			name: "missing symbol",
			input: MockFuturesArbitrageCalculationInput{
				Symbol:         "",
				LongExchange:   "binance",
				ShortExchange:  "okx",
				LongMarkPrice:  decimal.NewFromFloat(50000),
				ShortMarkPrice: decimal.NewFromFloat(50100),
				BaseAmount:     decimal.NewFromFloat(1.0),
			},
			expectError: true,
		},
		{
			name: "missing long exchange",
			input: MockFuturesArbitrageCalculationInput{
				Symbol:         "BTC/USDT",
				LongExchange:   "",
				ShortExchange:  "okx",
				LongMarkPrice:  decimal.NewFromFloat(50000),
				ShortMarkPrice: decimal.NewFromFloat(50100),
				BaseAmount:     decimal.NewFromFloat(1.0),
			},
			expectError: true,
		},
		{
			name: "missing short exchange",
			input: MockFuturesArbitrageCalculationInput{
				Symbol:         "BTC/USDT",
				LongExchange:   "binance",
				ShortExchange:  "",
				LongMarkPrice:  decimal.NewFromFloat(50000),
				ShortMarkPrice: decimal.NewFromFloat(50100),
				BaseAmount:     decimal.NewFromFloat(1.0),
			},
			expectError: true,
		},
		{
			name: "zero mark prices",
			input: MockFuturesArbitrageCalculationInput{
				Symbol:         "BTC/USDT",
				LongExchange:   "binance",
				ShortExchange:  "okx",
				LongMarkPrice:  decimal.Zero,
				ShortMarkPrice: decimal.Zero,
				BaseAmount:     decimal.NewFromFloat(1.0),
			},
			expectError: true,
		},
		{
			name: "zero base amount",
			input: MockFuturesArbitrageCalculationInput{
				Symbol:         "BTC/USDT",
				LongExchange:   "binance",
				ShortExchange:  "okx",
				LongMarkPrice:  decimal.NewFromFloat(50000),
				ShortMarkPrice: decimal.NewFromFloat(50100),
				BaseAmount:     decimal.Zero,
			},
			expectError: true,
		},
		{
			name: "invalid funding interval - should be defaulted",
			input: MockFuturesArbitrageCalculationInput{
				Symbol:          "BTC/USDT",
				LongExchange:    "binance",
				ShortExchange:   "okx",
				LongMarkPrice:   decimal.NewFromFloat(50000),
				ShortMarkPrice:  decimal.NewFromFloat(50100),
				BaseAmount:      decimal.NewFromFloat(1.0),
				FundingInterval: -5,
			},
			expectError: false, // Should default to 8
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := handler.validateCalculationInput(convertMockToModelInput(tt.input))

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCalculateMarketSummary(t *testing.T) {
	handler, mockDB := setupTestHandler(t)
	defer mockDB.Close()

	tests := []struct {
		name            string
		opportunities   []MockFuturesArbitrageOpportunity
		expectedSummary MockFuturesMarketSummary
	}{
		{
			name:          "no opportunities",
			opportunities: []MockFuturesArbitrageOpportunity{},
			expectedSummary: MockFuturesMarketSummary{
				TotalOpportunities: 0,
				MarketCondition:    "unfavorable",
				FundingRateTrend:   "stable",
			},
		},
		{
			name: "favorable market conditions",
			opportunities: []MockFuturesArbitrageOpportunity{
				{
					APY:             decimal.NewFromFloat(20),
					RiskScore:       decimal.NewFromFloat(30),
					VolatilityScore: decimal.NewFromFloat(25),
				},
				{
					APY:             decimal.NewFromFloat(25),
					RiskScore:       decimal.NewFromFloat(35),
					VolatilityScore: decimal.NewFromFloat(30),
				},
			},
			expectedSummary: MockFuturesMarketSummary{
				TotalOpportunities:  2,
				AverageAPY:          decimal.NewFromFloat(22.5),
				HighestAPY:          decimal.NewFromFloat(25),
				AverageRiskScore:    decimal.NewFromFloat(32.5),
				MarketVolatility:    decimal.NewFromFloat(27.5),
				FundingRateTrend:    "increasing",
				RecommendedStrategy: "aggressive",
				MarketCondition:     "favorable",
			},
		},
		{
			name: "unfavorable market conditions",
			opportunities: []MockFuturesArbitrageOpportunity{
				{
					APY:             decimal.NewFromFloat(3),
					RiskScore:       decimal.NewFromFloat(80),
					VolatilityScore: decimal.NewFromFloat(75),
				},
			},
			expectedSummary: MockFuturesMarketSummary{
				TotalOpportunities:  1,
				AverageAPY:          decimal.NewFromFloat(3),
				HighestAPY:          decimal.NewFromFloat(3),
				AverageRiskScore:    decimal.NewFromFloat(80),
				MarketVolatility:    decimal.NewFromFloat(75),
				FundingRateTrend:    "decreasing",
				RecommendedStrategy: "conservative",
				MarketCondition:     "unfavorable",
			},
		},
		{
			name: "neutral market conditions - moderate strategy",
			opportunities: []MockFuturesArbitrageOpportunity{
				{
					APY:             decimal.NewFromFloat(10),
					RiskScore:       decimal.NewFromFloat(60),
					VolatilityScore: decimal.NewFromFloat(50),
				},
			},
			expectedSummary: MockFuturesMarketSummary{
				TotalOpportunities:  1,
				AverageAPY:          decimal.NewFromFloat(10),
				HighestAPY:          decimal.NewFromFloat(10),
				AverageRiskScore:    decimal.NewFromFloat(60),
				MarketVolatility:    decimal.NewFromFloat(50),
				FundingRateTrend:    "stable",
				RecommendedStrategy: "moderate",
				MarketCondition:     "neutral",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.calculateMarketSummary(convertMockToModelOpportunities(tt.opportunities))

			assert.Equal(t, tt.expectedSummary.TotalOpportunities, result.TotalOpportunities)
			assert.Equal(t, tt.expectedSummary.MarketCondition, result.MarketCondition)
			assert.Equal(t, tt.expectedSummary.FundingRateTrend, result.FundingRateTrend)
			assert.Equal(t, tt.expectedSummary.RecommendedStrategy, result.RecommendedStrategy)

			if tt.expectedSummary.TotalOpportunities > 0 {
				assert.True(t, tt.expectedSummary.AverageAPY.Equal(result.AverageAPY))
				assert.True(t, tt.expectedSummary.HighestAPY.Equal(result.HighestAPY))
				assert.True(t, tt.expectedSummary.AverageRiskScore.Equal(result.AverageRiskScore))
				assert.True(t, tt.expectedSummary.MarketVolatility.Equal(result.MarketVolatility))
			}
		})
	}
}

func TestGenerateStrategies(t *testing.T) {
	// Test the generateStrategies function directly
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mock.Close()

	handler := NewFuturesArbitrageHandlerWithQuerier(mock)

	// Create test opportunities
	mockOpportunities := []MockFuturesArbitrageOpportunity{
		{
			ID:             "test-opportunity-1",
			Symbol:         "BTC/USDT",
			LongExchange:   "Binance",
			ShortExchange:  "Bybit",
			NetFundingRate: decimal.NewFromFloat(0.001),
			APY:            decimal.NewFromFloat(15.0),
			RiskScore:      decimal.NewFromFloat(3.5),
			IsActive:       true,
		},
	}

	// Convert mock opportunities to model opportunities
	opportunities := convertMockToModelOpportunities(mockOpportunities)

	// Create test request with position sizing enabled
	mockReq := MockFuturesArbitrageRequest{
		IncludePositionSizing: true,
		AvailableCapital:      decimal.NewFromFloat(10000),
		MaxLeverage:           decimal.NewFromFloat(5),
		RiskTolerance:         "medium",
	}

	// Convert mock request to model request
	req := convertMockToModelRequest(mockReq)

	// Call generateStrategies directly
	strategies, err := handler.generateStrategies(opportunities, req)

	// Verify results
	require.NoError(t, err)
	assert.NotNil(t, strategies)
	// The current implementation returns empty slice, so we expect empty
	assert.Empty(t, strategies)
}

func TestGetFuturesMarketSummary(t *testing.T) {
	tests := []struct {
		name       string
		setupMock  func(mock pgxmock.PgxPoolIface)
		expectCode int
	}{
		{
			name: "successful market summary",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				rows := pgxmock.NewRows([]string{
					"id", "symbol", "base_currency", "quote_currency",
					"long_exchange", "short_exchange", "long_exchange_id", "short_exchange_id",
					"long_funding_rate", "short_funding_rate", "net_funding_rate", "funding_interval",
					"long_mark_price", "short_mark_price", "price_difference", "price_difference_percentage",
					"hourly_rate", "daily_rate", "apy",
					"estimated_profit_8h", "estimated_profit_daily", "estimated_profit_weekly", "estimated_profit_monthly",
					"risk_score", "volatility_score", "liquidity_score",
					"recommended_position_size", "max_leverage", "recommended_leverage", "stop_loss_percentage",
					"min_position_size", "max_position_size", "optimal_position_size",
					"detected_at", "expires_at", "next_funding_time", "time_to_next_funding", "is_active",
					"market_trend", "volume_24h", "open_interest",
				}).AddRow(
					"test-id", "BTC/USDT", "BTC", "USDT",
					"binance", "okx", 1, 2,
					decimal.NewFromFloat(0.01), decimal.NewFromFloat(-0.005), decimal.NewFromFloat(0.015), 8,
					decimal.NewFromFloat(50000), decimal.NewFromFloat(49950), decimal.NewFromFloat(50), decimal.NewFromFloat(0.1),
					decimal.NewFromFloat(0.1875), decimal.NewFromFloat(4.5), decimal.NewFromFloat(54.75),
					decimal.NewFromFloat(100), decimal.NewFromFloat(1000), decimal.NewFromFloat(10000), decimal.NewFromFloat(100000),
					decimal.NewFromFloat(25), decimal.NewFromFloat(30), decimal.NewFromFloat(80),
					decimal.NewFromFloat(1000), decimal.NewFromFloat(10), decimal.NewFromFloat(5), decimal.NewFromFloat(2),
					decimal.NewFromFloat(100), decimal.NewFromFloat(10000), decimal.NewFromFloat(5000),
					time.Now(), time.Now().Add(time.Hour), time.Now().Add(time.Hour), time.Hour, true,
					"bullish", decimal.NewFromFloat(1000000), decimal.NewFromFloat(50000000),
				)
				mock.ExpectQuery("SELECT (.+) FROM futures_arbitrage_opportunities WHERE is_active = true AND expires_at > NOW\\(\\) ORDER BY apy DESC, risk_score ASC LIMIT \\$1 OFFSET \\$2").WithArgs(1000, 0).WillReturnRows(rows)
				mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM futures_arbitrage_opportunities WHERE is_active = true AND expires_at > NOW\\(\\)").WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))
			},
			expectCode: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			assert.NoError(t, err)
			defer mock.Close()

			handler := NewFuturesArbitrageHandlerWithQuerier(mock)

			tt.setupMock(mock)

			req, _ := http.NewRequest("GET", "/api/futures-arbitrage/market-summary", nil)
			w := httptest.NewRecorder()
			router := gin.New()
			router.GET("/api/futures-arbitrage/market-summary", handler.GetFuturesMarketSummary)
			router.ServeHTTP(w, req)

			if w.Code != tt.expectCode {
				t.Logf("Response body: %s", w.Body.String())
			}
			assert.Equal(t, tt.expectCode, w.Code)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestStoreFuturesOpportunity(t *testing.T) {
	testTime := time.Now()

	tests := []struct {
		name         string
		setupMock    func(mock pgxmock.PgxPoolIface)
		expectError  bool
		errorMessage string
	}{
		{
			name: "successful storage",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// Expect the INSERT query with all parameters
				mock.ExpectQuery(`INSERT INTO futures_arbitrage_opportunities`).
					WithArgs(
						"BTC/USDT", "BTC", "USDT",
						"Binance", "Bybit", 1, 2,
						decimal.NewFromFloat(0.01), decimal.NewFromFloat(-0.005), decimal.NewFromFloat(0.015), 8,
						decimal.NewFromFloat(50000), decimal.NewFromFloat(49950), decimal.NewFromFloat(50), decimal.NewFromFloat(0.1),
						decimal.NewFromFloat(0.1875), decimal.NewFromFloat(4.5), decimal.NewFromFloat(54.75),
						decimal.NewFromFloat(100), decimal.NewFromFloat(1000), decimal.NewFromFloat(10000), decimal.NewFromFloat(100000),
						decimal.NewFromFloat(25), decimal.NewFromFloat(30), decimal.NewFromFloat(80),
						decimal.NewFromFloat(1000), decimal.NewFromFloat(10), decimal.NewFromFloat(5), decimal.NewFromFloat(2),
						decimal.NewFromFloat(100), decimal.NewFromFloat(10000), decimal.NewFromFloat(5000),
						testTime, testTime.Add(time.Hour*24), testTime.Add(time.Hour*8), int(time.Hour*8)/int(time.Minute), true,
						"bullish", decimal.NewFromFloat(1000000), decimal.NewFromFloat(50000),
					).
					WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow("test-opportunity-id"))
			},
			expectError: false,
		},
		{
			name: "database insertion failure",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				// Expect the INSERT query but return an error
				mock.ExpectQuery(`INSERT INTO futures_arbitrage_opportunities`).
					WithArgs(
						"BTC/USDT", "BTC", "USDT",
						"Binance", "Bybit", 1, 2,
						decimal.NewFromFloat(0.01), decimal.NewFromFloat(-0.005), decimal.NewFromFloat(0.015), 8,
						decimal.NewFromFloat(50000), decimal.NewFromFloat(49950), decimal.NewFromFloat(50), decimal.NewFromFloat(0.1),
						decimal.NewFromFloat(0.1875), decimal.NewFromFloat(4.5), decimal.NewFromFloat(54.75),
						decimal.NewFromFloat(100), decimal.NewFromFloat(1000), decimal.NewFromFloat(10000), decimal.NewFromFloat(100000),
						decimal.NewFromFloat(25), decimal.NewFromFloat(30), decimal.NewFromFloat(80),
						decimal.NewFromFloat(1000), decimal.NewFromFloat(10), decimal.NewFromFloat(5), decimal.NewFromFloat(2),
						decimal.NewFromFloat(100), decimal.NewFromFloat(10000), decimal.NewFromFloat(5000),
						testTime, testTime.Add(time.Hour*24), testTime.Add(time.Hour*8), int(time.Hour*8)/int(time.Minute), true,
						"bullish", decimal.NewFromFloat(1000000), decimal.NewFromFloat(50000),
					).
					WillReturnError(sql.ErrConnDone)
			},
			expectError:  true,
			errorMessage: sql.ErrConnDone.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock, err := pgxmock.NewPool()
			assert.NoError(t, err)
			defer mock.Close()

			handler := NewFuturesArbitrageHandlerWithQuerier(mock)

			tt.setupMock(mock)

			// Create test opportunity
			mockOpportunity := &MockFuturesArbitrageOpportunity{
				Symbol:                    "BTC/USDT",
				BaseCurrency:              "BTC",
				QuoteCurrency:             "USDT",
				LongExchange:              "Binance",
				ShortExchange:             "Bybit",
				LongExchangeID:            int64(1),
				ShortExchangeID:           int64(2),
				LongFundingRate:           decimal.NewFromFloat(0.01),
				ShortFundingRate:          decimal.NewFromFloat(-0.005),
				NetFundingRate:            decimal.NewFromFloat(0.015),
				FundingInterval:           8,
				LongMarkPrice:             decimal.NewFromFloat(50000),
				ShortMarkPrice:            decimal.NewFromFloat(49950),
				PriceDifference:           decimal.NewFromFloat(50),
				PriceDifferencePercentage: decimal.NewFromFloat(0.1),
				HourlyRate:                decimal.NewFromFloat(0.1875),
				DailyRate:                 decimal.NewFromFloat(4.5),
				APY:                       decimal.NewFromFloat(54.75),
				EstimatedProfit8h:         decimal.NewFromFloat(100),
				EstimatedProfitDaily:      decimal.NewFromFloat(1000),
				EstimatedProfitWeekly:     decimal.NewFromFloat(10000),
				EstimatedProfitMonthly:    decimal.NewFromFloat(100000),
				RiskScore:                 decimal.NewFromFloat(25),
				VolatilityScore:           decimal.NewFromFloat(30),
				LiquidityScore:            decimal.NewFromFloat(80),
				RecommendedPositionSize:   decimal.NewFromFloat(1000),
				MaxLeverage:               decimal.NewFromFloat(10),
				RecommendedLeverage:       decimal.NewFromFloat(5),
				StopLossPercentage:        decimal.NewFromFloat(2),
				MinPositionSize:           decimal.NewFromFloat(100),
				MaxPositionSize:           decimal.NewFromFloat(10000),
				OptimalPositionSize:       decimal.NewFromFloat(5000),
				DetectedAt:                testTime,
				ExpiresAt:                 testTime.Add(time.Hour * 24),
				NextFundingTime:           testTime.Add(time.Hour * 8),
				TimeToNextFunding:         int(time.Hour*8) / int(time.Minute),
				IsActive:                  true,
				MarketTrend:               "bullish",
				Volume24h:                 decimal.NewFromFloat(1000000),
				OpenInterest:              decimal.NewFromFloat(50000),
			}

			// Create test risk metrics
			mockRiskMetrics := &MockFuturesArbitrageRiskMetrics{
				PriceCorrelation:      decimal.NewFromFloat(0.8),
				PriceVolatility:       decimal.NewFromFloat(0.2),
				MaxDrawdown:           decimal.NewFromFloat(0.1),
				FundingRateVolatility: decimal.NewFromFloat(0.15),
				FundingRateStability:  decimal.NewFromFloat(0.85),
				BidAskSpread:          decimal.NewFromFloat(0.001),
				MarketDepth:           decimal.NewFromFloat(1000000),
				SlippageRisk:          decimal.NewFromFloat(0.005),
				ExchangeReliability:   decimal.NewFromFloat(0.95),
				CounterpartyRisk:      decimal.NewFromFloat(0.1),
				OverallRiskScore:      decimal.NewFromFloat(25),
				RiskCategory:          "medium",
				Recommendation:        "proceed_with_caution",
			}

			// Convert mock types to model types
			opportunity := convertMockToModelOpportunity(mockOpportunity)
			riskMetrics := convertMockToModelRiskMetrics(mockRiskMetrics)

			// Call storeFuturesOpportunity
			err = handler.storeFuturesOpportunity(opportunity, riskMetrics)

			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMessage)
			} else {
				assert.NoError(t, err)
				// Verify that the opportunity ID was set
				assert.Equal(t, "test-opportunity-id", opportunity.ID)
			}

			assert.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
