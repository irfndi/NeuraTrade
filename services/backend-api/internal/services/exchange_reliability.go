package services

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/irfndi/neuratrade/internal/telemetry"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// ExchangeReliabilityTracker tracks exchange API performance for risk scoring.
type ExchangeReliabilityTracker struct {
	db          *database.PostgresDB
	redisClient *redis.Client

	// In-memory counters for real-time tracking
	counters map[string]*exchangeCounters
	mu       sync.RWMutex

	// Configuration
	windowSize time.Duration // Size of the sliding window for metrics
}

// exchangeCounters holds real-time counters for an exchange.
type exchangeCounters struct {
	successCount24h  int64
	failureCount24h  int64
	totalLatency24h  int64 // Cumulative latency in ms
	requestCount24h  int64
	lastFailure      *time.Time
	lastSuccess      *time.Time
	consecutiveFails int
	mu               sync.Mutex
}

// APICallResult represents the result of an API call for tracking.
type APICallResult struct {
	Exchange  string
	Endpoint  string
	Success   bool
	LatencyMs int64
	ErrorMsg  string
	Timestamp time.Time
}

// NewExchangeReliabilityTracker creates a new exchange reliability tracker.
func NewExchangeReliabilityTracker(
	db *database.PostgresDB,
	redisClient *redis.Client,
) *ExchangeReliabilityTracker {
	tracker := &ExchangeReliabilityTracker{
		db:          db,
		redisClient: redisClient,
		counters:    make(map[string]*exchangeCounters),
		windowSize:  24 * time.Hour,
	}

	// Initialize counters for common exchanges
	exchanges := []string{"binance", "bybit", "okx", "bitget", "kucoin", "gate"}
	for _, ex := range exchanges {
		tracker.counters[ex] = &exchangeCounters{}
	}

	return tracker
}

// RecordAPICall records an API call result for reliability tracking.
func (t *ExchangeReliabilityTracker) RecordAPICall(result APICallResult) error {
	ctx := context.Background()
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpMarketData, "ExchangeReliabilityTracker.RecordAPICall", map[string]string{
		"exchange":   result.Exchange,
		"endpoint":   result.Endpoint,
		"success":    fmt.Sprintf("%t", result.Success),
		"latency_ms": fmt.Sprintf("%d", result.LatencyMs),
	})
	defer observability.FinishSpan(span, nil)

	t.mu.Lock()
	counters, exists := t.counters[result.Exchange]
	if !exists {
		counters = &exchangeCounters{}
		t.counters[result.Exchange] = counters
	}
	t.mu.Unlock()

	counters.mu.Lock()
	defer counters.mu.Unlock()

	counters.requestCount24h++
	counters.totalLatency24h += result.LatencyMs

	if result.Success {
		counters.successCount24h++
		counters.lastSuccess = &result.Timestamp
		counters.consecutiveFails = 0
	} else {
		counters.failureCount24h++
		counters.lastFailure = &result.Timestamp
		counters.consecutiveFails++
		observability.AddBreadcrumb(spanCtx, "exchange_reliability", fmt.Sprintf("API call to %s failed: %s", result.Exchange, result.ErrorMsg), sentry.LevelWarning)
	}

	// Store in Redis for persistence (optional)
	if t.redisClient != nil {
		go t.persistMetrics(result)
	}

	return nil
}

// persistMetrics stores metrics in Redis for persistence across restarts.
func (t *ExchangeReliabilityTracker) persistMetrics(result APICallResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	key := fmt.Sprintf("exchange_metrics:%s:%s", result.Exchange, time.Now().Format("2006-01-02-15"))

	data, _ := json.Marshal(result)
	t.redisClient.LPush(ctx, key, data)
	t.redisClient.Expire(ctx, key, 25*time.Hour) // Keep slightly longer than window
}

// GetReliabilityScore returns the reliability score for an exchange (0-20 scale).
// This integrates with the existing risk scoring system where exchange risk is 0-20 points.
func (t *ExchangeReliabilityTracker) GetReliabilityScore(exchange string) (decimal.Decimal, error) {
	metrics := t.GetReliabilityMetrics(exchange)
	if metrics == nil {
		// Return default score if no data
		return decimal.NewFromInt(10), nil
	}

	return metrics.RiskScore, nil
}

// GetReliabilityMetrics returns comprehensive reliability metrics for an exchange.
func (t *ExchangeReliabilityTracker) GetReliabilityMetrics(exchange string) *models.ExchangeReliabilityMetrics {
	t.mu.RLock()
	counters, exists := t.counters[exchange]
	t.mu.RUnlock()

	if !exists {
		return nil
	}

	counters.mu.Lock()
	defer counters.mu.Unlock()

	metrics := &models.ExchangeReliabilityMetrics{
		Exchange:        exchange,
		FailureCount24h: int(counters.failureCount24h),
		LastUpdated:     time.Now(),
	}

	// Copy last failure time
	if counters.lastFailure != nil {
		lastFailure := *counters.lastFailure
		metrics.LastFailure = &lastFailure
	}

	// Calculate uptime percentage
	totalRequests := counters.successCount24h + counters.failureCount24h
	if totalRequests > 0 {
		uptimeFloat := float64(counters.successCount24h) / float64(totalRequests) * 100
		metrics.UptimePercent24h = decimal.NewFromFloat(uptimeFloat)
	} else {
		metrics.UptimePercent24h = decimal.NewFromInt(100) // Assume 100% if no data
	}

	// Calculate average latency
	if counters.requestCount24h > 0 {
		metrics.AvgLatencyMs = counters.totalLatency24h / counters.requestCount24h
	}

	// Calculate risk score (0-20 scale to match existing risk model)
	metrics.RiskScore = t.calculateRiskScore(metrics, counters.consecutiveFails)

	return metrics
}

// calculateRiskScore calculates the risk score based on reliability metrics.
func (t *ExchangeReliabilityTracker) calculateRiskScore(
	metrics *models.ExchangeReliabilityMetrics,
	consecutiveFails int,
) decimal.Decimal {
	// Base score starts at 5 (neutral)
	riskScore := decimal.NewFromInt(5)

	// Uptime factor (0-7 points)
	// 99%+ uptime = 0 points, 95% = 3.5 points, 90% = 7 points
	uptimeFloat, _ := metrics.UptimePercent24h.Float64()
	if uptimeFloat < 99 {
		uptimePenalty := (99 - uptimeFloat) * 0.7
		if uptimePenalty > 7 {
			uptimePenalty = 7
		}
		riskScore = riskScore.Add(decimal.NewFromFloat(uptimePenalty))
	}

	// Latency factor (0-5 points)
	// < 200ms = 0, 200-500ms = 1-2, 500-1000ms = 2-4, > 1000ms = 5
	if metrics.AvgLatencyMs > 200 {
		latencyPenalty := float64(metrics.AvgLatencyMs-200) / 200.0
		if latencyPenalty > 5 {
			latencyPenalty = 5
		}
		riskScore = riskScore.Add(decimal.NewFromFloat(latencyPenalty))
	}

	// Consecutive failures penalty (0-5 points)
	if consecutiveFails > 0 {
		failPenalty := consecutiveFails
		if failPenalty > 5 {
			failPenalty = 5
		}
		riskScore = riskScore.Add(decimal.NewFromInt(int64(failPenalty)))
	}

	// Recent failure penalty (0-3 points)
	if metrics.LastFailure != nil && time.Since(*metrics.LastFailure) < 5*time.Minute {
		riskScore = riskScore.Add(decimal.NewFromInt(3))
	}

	// Cap at 20
	if riskScore.GreaterThan(decimal.NewFromInt(20)) {
		riskScore = decimal.NewFromInt(20)
	}

	return riskScore
}

// GetAllReliabilityMetrics returns metrics for all tracked exchanges.
func (t *ExchangeReliabilityTracker) GetAllReliabilityMetrics() map[string]*models.ExchangeReliabilityMetrics {
	t.mu.RLock()
	exchanges := make([]string, 0, len(t.counters))
	for ex := range t.counters {
		exchanges = append(exchanges, ex)
	}
	t.mu.RUnlock()

	result := make(map[string]*models.ExchangeReliabilityMetrics)
	for _, ex := range exchanges {
		if metrics := t.GetReliabilityMetrics(ex); metrics != nil {
			result[ex] = metrics
		}
	}

	return result
}

// ResetCounters resets the counters for an exchange.
// Typically called at the start of a new 24-hour window.
func (t *ExchangeReliabilityTracker) ResetCounters(exchange string) {
	t.mu.Lock()
	if counters, exists := t.counters[exchange]; exists {
		counters.mu.Lock()
		counters.successCount24h = 0
		counters.failureCount24h = 0
		counters.totalLatency24h = 0
		counters.requestCount24h = 0
		counters.consecutiveFails = 0
		counters.mu.Unlock()
	}
	t.mu.Unlock()
}

// ResetAllCounters resets counters for all exchanges.
func (t *ExchangeReliabilityTracker) ResetAllCounters() {
	t.mu.RLock()
	exchanges := make([]string, 0, len(t.counters))
	for ex := range t.counters {
		exchanges = append(exchanges, ex)
	}
	t.mu.RUnlock()

	for _, ex := range exchanges {
		t.ResetCounters(ex)
	}
}

// StartPeriodicReset starts a goroutine that resets counters periodically.
func (t *ExchangeReliabilityTracker) StartPeriodicReset(ctx context.Context) {
	observability.AddBreadcrumb(ctx, "exchange_reliability", "Starting periodic counter reset (24h interval)", sentry.LevelInfo)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				observability.AddBreadcrumb(ctx, "exchange_reliability", "Periodic reset goroutine stopped", sentry.LevelInfo)
				return
			case <-ticker.C:
				t.ResetAllCounters()
				observability.AddBreadcrumb(ctx, "exchange_reliability", "Reset exchange reliability counters", sentry.LevelInfo)
				telemetry.Logger().Info("Reset exchange reliability counters")
			}
		}
	}()
}

// GetExchangeRiskForArbitrage calculates combined exchange risk for an arbitrage pair.
// Returns a score (0-20) that can be used directly in the risk calculation.
func (t *ExchangeReliabilityTracker) GetExchangeRiskForArbitrage(
	longExchange, shortExchange string,
) decimal.Decimal {
	longRisk, _ := t.GetReliabilityScore(longExchange)
	shortRisk, _ := t.GetReliabilityScore(shortExchange)

	// Use the higher risk (more conservative approach)
	if longRisk.GreaterThan(shortRisk) {
		return longRisk
	}
	return shortRisk
}

// RecordSuccess is a convenience method to record a successful API call.
func (t *ExchangeReliabilityTracker) RecordSuccess(exchange string, latencyMs int64) {
	_ = t.RecordAPICall(APICallResult{
		Exchange:  exchange,
		Success:   true,
		LatencyMs: latencyMs,
		Timestamp: time.Now(),
	})
}

// RecordFailure is a convenience method to record a failed API call.
func (t *ExchangeReliabilityTracker) RecordFailure(exchange string, latencyMs int64, errorMsg string) {
	_ = t.RecordAPICall(APICallResult{
		Exchange:  exchange,
		Success:   false,
		LatencyMs: latencyMs,
		ErrorMsg:  errorMsg,
		Timestamp: time.Now(),
	})
}
