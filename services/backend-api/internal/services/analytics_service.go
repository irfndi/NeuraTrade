package services

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
	"github.com/jackc/pgx/v5"
)

// AnalyticsQuerier defines the database operations needed for analytics.
type AnalyticsQuerier interface {
	Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error)
}

// AnalyticsService provides correlation, regime detection, and forecasting utilities.
type AnalyticsService struct {
	db     AnalyticsQuerier
	config config.AnalyticsConfig
}

// NewAnalyticsService creates a new analytics service.
func NewAnalyticsService(db *database.PostgresDB, cfg config.AnalyticsConfig) *AnalyticsService {
	var querier AnalyticsQuerier
	if db != nil {
		querier = db.Pool
	}

	return &AnalyticsService{
		db:     querier,
		config: cfg,
	}
}

// NewAnalyticsServiceWithQuerier creates a new analytics service with a custom querier (for tests).
func NewAnalyticsServiceWithQuerier(db AnalyticsQuerier, cfg config.AnalyticsConfig) *AnalyticsService {
	return &AnalyticsService{
		db:     db,
		config: cfg,
	}
}

// CalculateCorrelationMatrix builds a correlation matrix for the given symbols.
func (s *AnalyticsService) CalculateCorrelationMatrix(ctx context.Context, exchange string, symbols []string, limit int) (*models.CorrelationMatrix, error) {
	spanCtx, span := observability.StartSpan(ctx, observability.SpanOpTechnicalAnalys, "AnalyticsService.CalculateCorrelationMatrix")
	defer observability.FinishSpan(span, nil)

	if s.db == nil {
		err := fmt.Errorf("analytics database is not available")
		observability.CaptureException(spanCtx, err)
		return nil, err
	}
	if !s.config.EnableCorrelation {
		return nil, fmt.Errorf("correlation analysis disabled")
	}
	if len(symbols) < 2 {
		return nil, fmt.Errorf("at least two symbols are required")
	}

	if limit <= 0 {
		limit = s.config.CorrelationWindow
	}

	series := make(map[string][]float64)
	minLen := math.MaxInt
	for _, symbol := range symbols {
		prices, _, err := s.getPriceSeries(spanCtx, exchange, symbol, limit)
		if err != nil {
			observability.CaptureException(spanCtx, err)
			return nil, err
		}
		if len(prices) == 0 {
			err := fmt.Errorf("no data for %s", symbol)
			observability.CaptureException(spanCtx, err)
			return nil, err
		}
		series[symbol] = prices
		if len(prices) < minLen {
			minLen = len(prices)
		}
	}

	if minLen < s.config.CorrelationMinPoints {
		return nil, fmt.Errorf("insufficient points for correlation")
	}

	// Create a copy of symbols for sorting (avoid modifying input slice)
	orderedSymbols := make([]string, len(symbols))
	copy(orderedSymbols, symbols)
	sort.Strings(orderedSymbols)

	matrix := make([][]float64, len(orderedSymbols))
	for i := range orderedSymbols {
		matrix[i] = make([]float64, len(orderedSymbols))
		for j := range orderedSymbols {
			if i == j {
				matrix[i][j] = 1
				continue
			}
			x := series[orderedSymbols[i]]
			y := series[orderedSymbols[j]]
			length := minLen
			x = x[len(x)-length:]
			y = y[len(y)-length:]
			xReturns := logReturns(x)
			yReturns := logReturns(y)
			minReturns := len(xReturns)
			if len(yReturns) < minReturns {
				minReturns = len(yReturns)
			}
			if minReturns < s.config.CorrelationMinPoints-1 {
				matrix[i][j] = 0
				continue
			}
			matrix[i][j] = calculateCorrelation(xReturns[len(xReturns)-minReturns:], yReturns[len(yReturns)-minReturns:])
		}
	}

	return &models.CorrelationMatrix{
		Exchange:    exchange,
		Symbols:     orderedSymbols,
		Matrix:      matrix,
		WindowSize:  minLen,
		GeneratedAt: time.Now(),
	}, nil
}

// DetectMarketRegime determines the current regime for a symbol.
func (s *AnalyticsService) DetectMarketRegime(ctx context.Context, exchange string, symbol string, limit int) (*models.MarketRegime, error) {
	spanCtx, span := observability.StartSpan(ctx, observability.SpanOpTechnicalAnalys, "AnalyticsService.DetectMarketRegime")
	defer observability.FinishSpan(span, nil)

	if s.db == nil {
		err := fmt.Errorf("analytics database is not available")
		observability.CaptureException(spanCtx, err)
		return nil, err
	}
	if !s.config.EnableRegimeDetection {
		return nil, fmt.Errorf("regime detection disabled")
	}
	if limit <= 0 {
		limit = s.config.RegimeLongWindow
	}

	prices, _, err := s.getPriceSeries(spanCtx, exchange, symbol, limit)
	if err != nil {
		observability.CaptureException(spanCtx, err)
		return nil, err
	}
	if len(prices) < s.config.RegimeLongWindow {
		return nil, fmt.Errorf("insufficient data for regime detection")
	}

	shortWindow := s.config.RegimeShortWindow
	longWindow := s.config.RegimeLongWindow
	if shortWindow <= 1 {
		shortWindow = 10
	}
	if longWindow <= shortWindow {
		longWindow = shortWindow * 3
	}

	shortSlice := prices[len(prices)-shortWindow:]
	longSlice := prices[len(prices)-longWindow:]

	shortMA := calculateMeanFloat64(shortSlice)
	longMA := calculateMeanFloat64(longSlice)
	trendStrength := 0.0
	if longMA != 0 {
		trendStrength = math.Abs(shortMA-longMA) / longMA
	}

	trend := "sideways"
	trendThreshold := 0.002
	if shortMA > longMA*(1+trendThreshold) {
		trend = "bullish"
	} else if shortMA < longMA*(1-trendThreshold) {
		trend = "bearish"
	}

	returns := logReturns(prices)
	volatility := calculateStdDev(returns)
	volatilityScore := math.Min(1, volatility/s.config.VolatilityHighThreshold)
	volatilityLabel := "normal"
	if volatility >= s.config.VolatilityHighThreshold {
		volatilityLabel = "high"
	} else if volatility <= s.config.VolatilityLowThreshold {
		volatilityLabel = "low"
	}

	confidence := math.Min(1, trendStrength*3+volatilityScore/2)

	return &models.MarketRegime{
		Symbol:          symbol,
		Exchange:        exchange,
		Trend:           trend,
		Volatility:      volatilityLabel,
		TrendStrength:   trendStrength,
		VolatilityScore: volatilityScore,
		Confidence:      confidence,
		WindowSize:      len(prices),
		GeneratedAt:     time.Now(),
	}, nil
}

// ForecastSeries produces a short-term forecast for price or funding rates.
func (s *AnalyticsService) ForecastSeries(ctx context.Context, exchange string, symbol string, seriesType string, horizon int, limit int) (*models.ForecastResult, error) {
	spanCtx, span := observability.StartSpan(ctx, observability.SpanOpTechnicalAnalys, "AnalyticsService.ForecastSeries")
	defer observability.FinishSpan(span, nil)

	if s.db == nil {
		err := fmt.Errorf("analytics database is not available")
		observability.CaptureException(spanCtx, err)
		return nil, err
	}
	if !s.config.EnableForecasting {
		return nil, fmt.Errorf("forecasting disabled")
	}
	if horizon <= 0 {
		horizon = s.config.ForecastHorizon
	}
	if limit <= 0 {
		limit = s.config.ForecastLookback
	}

	var series []float64
	var timestamps []time.Time
	var err error
	if seriesType == "funding" {
		series, timestamps, err = s.getFundingRateSeries(spanCtx, exchange, symbol, limit)
	} else {
		seriesType = "price"
		series, timestamps, err = s.getPriceSeries(spanCtx, exchange, symbol, limit)
	}
	if err != nil {
		observability.CaptureException(spanCtx, err)
		return nil, err
	}
	if len(series) < 5 {
		return nil, fmt.Errorf("insufficient data for forecasting")
	}

	series = series[len(series)-limit:]
	lastValue := series[len(series)-1]
	model := "AR(1)+GARCH(1,1)"

	var forecastPoints []models.ForecastPoint
	if seriesType == "price" {
		returns := logReturns(series)
		if len(returns) < 2 {
			return nil, fmt.Errorf("insufficient returns for forecasting")
		}
		phi, c := fitAR1(returns)
		variances := garch11Forecast(returns, horizon, 1e-6, 0.1, 0.8)
		lastReturn := returns[len(returns)-1]
		lastTimestamp := timestamps[len(timestamps)-1]
		for i := 0; i < horizon; i++ {
			nextReturn := c + phi*lastReturn
			lastValue = lastValue * math.Exp(nextReturn)
			lastReturn = nextReturn
			lastTimestamp = lastTimestamp.Add(8 * time.Hour)
			forecastPoints = append(forecastPoints, models.ForecastPoint{
				Timestamp: lastTimestamp,
				Value:     lastValue,
				Variance:  variances[i],
			})
		}
	} else {
		phi, c := fitAR1(series)
		returnChanges := make([]float64, 0, len(series)-1)
		for i := 1; i < len(series); i++ {
			returnChanges = append(returnChanges, series[i]-series[i-1])
		}
		variances := garch11Forecast(returnChanges, horizon, 1e-6, 0.1, 0.8)
		lastValue = series[len(series)-1]
		lastTimestamp := timestamps[len(timestamps)-1]
		for i := 0; i < horizon; i++ {
			nextValue := c + phi*lastValue
			lastValue = nextValue
			lastTimestamp = lastTimestamp.Add(8 * time.Hour)
			forecastPoints = append(forecastPoints, models.ForecastPoint{
				Timestamp: lastTimestamp,
				Value:     lastValue,
				Variance:  variances[i],
			})
		}
	}

	return &models.ForecastResult{
		Symbol:       symbol,
		Exchange:     exchange,
		Model:        model,
		SeriesType:   seriesType,
		Horizon:      horizon,
		LastObserved: series[len(series)-1],
		Points:       forecastPoints,
		GeneratedAt:  time.Now(),
	}, nil
}

func (s *AnalyticsService) getPriceSeries(ctx context.Context, exchange string, symbol string, limit int) ([]float64, []time.Time, error) {
	query := `
		SELECT md.last_price, md.timestamp
		FROM market_data md
		JOIN trading_pairs tp ON md.trading_pair_id = tp.id
		JOIN exchanges e ON md.exchange_id = e.id
		WHERE tp.symbol = $1 AND e.name = $2 AND md.last_price > 0
		ORDER BY md.timestamp DESC
		LIMIT $3
	`

	rows, err := s.db.Query(ctx, query, symbol, exchange, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var values []float64
	var timestamps []time.Time
	for rows.Next() {
		var price float64
		var ts time.Time
		if err := rows.Scan(&price, &ts); err != nil {
			return nil, nil, err
		}
		values = append(values, price)
		timestamps = append(timestamps, ts)
	}
	if rows.Err() != nil {
		return nil, nil, rows.Err()
	}

	// Reverse to ascending time order
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
		timestamps[i], timestamps[j] = timestamps[j], timestamps[i]
	}

	return values, timestamps, nil
}

func (s *AnalyticsService) getFundingRateSeries(ctx context.Context, exchange string, symbol string, limit int) ([]float64, []time.Time, error) {
	query := `
		SELECT funding_rate, funding_time
		FROM funding_rate_history
		WHERE symbol = $1 AND exchange = $2
		ORDER BY funding_time DESC
		LIMIT $3
	`

	rows, err := s.db.Query(ctx, query, symbol, exchange, limit)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	var values []float64
	var timestamps []time.Time
	for rows.Next() {
		var rate float64
		var ts time.Time
		if err := rows.Scan(&rate, &ts); err != nil {
			return nil, nil, err
		}
		values = append(values, rate)
		timestamps = append(timestamps, ts)
	}
	if rows.Err() != nil {
		return nil, nil, rows.Err()
	}

	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
		timestamps[i], timestamps[j] = timestamps[j], timestamps[i]
	}

	return values, timestamps, nil
}
