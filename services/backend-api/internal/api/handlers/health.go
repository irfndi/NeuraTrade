package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfndi/neuratrade/internal/services"
)

// DatabaseHealthChecker interface for database health checks.
type DatabaseHealthChecker interface {
	// HealthCheck verifies the database connection.
	HealthCheck(ctx context.Context) error
}

// RedisHealthChecker interface for redis health checks.
type RedisHealthChecker interface {
	// HealthCheck verifies the Redis connection.
	HealthCheck(ctx context.Context) error
}

// HealthHandler manages health check endpoints.
type HealthHandler struct {
	db             DatabaseHealthChecker
	redis          RedisHealthChecker
	ccxtURL        string
	cacheAnalytics CacheAnalyticsInterface
}

// HealthResponse represents the health status response.
type HealthResponse struct {
	// Status is the overall system status ("healthy", "degraded", "unhealthy").
	Status string `json:"status"`
	// Timestamp is the check time.
	Timestamp time.Time `json:"timestamp"`
	// Services contains status of individual services.
	Services map[string]string `json:"services"`
	// Version is the application version.
	Version string `json:"version"`
	// Uptime is the service uptime.
	Uptime string `json:"uptime"`
	// CacheMetrics contains detailed cache metrics if available.
	CacheMetrics *services.CacheMetrics `json:"cache_metrics,omitempty"`
	// CacheStats contains cache statistics if available.
	CacheStats map[string]services.CacheStats `json:"cache_stats,omitempty"`
}

// ServiceStatus represents the status of a single service.
type ServiceStatus struct {
	// Status indicates if the service is operational.
	Status string `json:"status"`
	// Message provides details if the service is unhealthy.
	Message string `json:"message,omitempty"`
}

// NewHealthHandler creates a new instance of HealthHandler.
//
// Parameters:
//
//	db: Database checker.
//	redis: Redis checker.
//	ccxtURL: URL of the CCXT service.
//	cacheAnalytics: Cache analytics service.
//
// Returns:
//
//	*HealthHandler: Initialized handler.
func NewHealthHandler(db DatabaseHealthChecker, redis RedisHealthChecker, ccxtURL string, cacheAnalytics CacheAnalyticsInterface) *HealthHandler {
	return &HealthHandler{
		db:             db,
		redis:          redis,
		ccxtURL:        ccxtURL,
		cacheAnalytics: cacheAnalytics,
	}
}

// HealthCheck performs a comprehensive system health check.
// It verifies connectivity to database, Redis, and CCXT service.
//
// Parameters:
//
//	w: HTTP response writer.
//	r: HTTP request.
func (h *HealthHandler) HealthCheck(w http.ResponseWriter, r *http.Request) {
	// Create context with timeout for health checks
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	span := sentry.StartSpan(ctx, "health_check")
	defer span.Finish()
	// Update context to include the span for downstream calls
	ctx = span.Context()

	span.SetTag("http.method", r.Method)
	span.SetTag("http.url", r.URL.String())
	span.SetTag("handler.name", "HealthCheck")

	servicesStatus := make(map[string]string)

	// Check database
	if h.db != nil {
		if err := h.db.HealthCheck(ctx); err != nil {
			servicesStatus["database"] = "unhealthy: " + err.Error()
			span.SetTag("database.status", "unhealthy")
			sentry.CaptureException(err)
		} else {
			servicesStatus["database"] = "healthy"
			span.SetTag("database.status", "healthy")
		}
	} else {
		servicesStatus["database"] = "unhealthy: not configured"
		span.SetTag("database.status", "not_configured")
	}

	// Check Redis
	if h.redis != nil {
		if err := h.redis.HealthCheck(ctx); err != nil {
			servicesStatus["redis"] = "unhealthy: " + err.Error()
			span.SetTag("redis.status", "unhealthy")
			sentry.CaptureException(err)
		} else {
			servicesStatus["redis"] = "healthy"
			span.SetTag("redis.status", "healthy")
		}
	} else {
		servicesStatus["redis"] = "unhealthy: not configured"
		span.SetTag("redis.status", "not_configured")
	}

	// Check CCXT Service
	var ccxtStatus string
	if err := h.checkCCXTService(); err != nil {
		ccxtStatus = "unhealthy: " + err.Error()
		span.SetTag("ccxt.status", "unhealthy")
		sentry.CaptureException(err)
	} else {
		ccxtStatus = "healthy"
		span.SetTag("ccxt.status", "healthy")
	}
	servicesStatus["ccxt"] = ccxtStatus

	// Check Telegram bot configuration - support both TELEGRAM_BOT_TOKEN and TELEGRAM_TOKEN
	telegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if telegramToken == "" {
		telegramToken = os.Getenv("TELEGRAM_TOKEN")
	}
	if telegramToken == "" {
		servicesStatus["telegram"] = "unhealthy: TELEGRAM_BOT_TOKEN not set"
		span.SetTag("telegram.status", "not_configured")
	} else {
		servicesStatus["telegram"] = "healthy"
		span.SetTag("telegram.status", "healthy")
	}

	// Determine overall status
	// Critical services map - services that should cause 503 if unhealthy
	// This centralizes the definition for maintainability
	criticalServices := map[string]bool{"database": true}
	criticalUnhealthy := false
	status := "healthy"
	for serviceName, s := range servicesStatus {
		if s != "healthy" && s != "not configured" {
			status = "degraded"
			// Check if the unhealthy service is critical
			if criticalServices[serviceName] {
				criticalUnhealthy = true
			}
		}
	}
	span.SetTag("overall.status", status)

	var cacheMetrics *services.CacheMetrics
	var cacheStats map[string]services.CacheStats

	// Add cache metrics if cache analytics service is available
	if h.cacheAnalytics != nil {
		if metrics, err := h.cacheAnalytics.GetMetrics(ctx); err == nil {
			cacheMetrics = metrics
		}
		cacheStats = h.cacheAnalytics.GetAllStats()
	}

	response := HealthResponse{
		Status:       status,
		Timestamp:    time.Now(),
		Services:     servicesStatus,
		Version:      os.Getenv("APP_VERSION"),
		Uptime:       time.Since(startTime).String(),
		CacheMetrics: cacheMetrics,
		CacheStats:   cacheStats,
	}

	w.Header().Set("Content-Type", "application/json")
	// Only return 503 if critical services (database) are unhealthy
	// This allows the service to remain available for degraded operation
	// when non-critical services (CCXT, Telegram) are temporarily unavailable
	if criticalUnhealthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		span.Status = sentry.SpanStatusUnavailable
	} else {
		w.WriteHeader(http.StatusOK)
		span.Status = sentry.SpanStatusOK
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// CCXTHealthResponse represents the detailed health response from CCXT service.
type CCXTHealthResponse struct {
	Status               string `json:"status"`
	Timestamp            string `json:"timestamp"`
	Service              string `json:"service"`
	Version              string `json:"version"`
	ExchangesCount       int    `json:"exchanges_count"`
	ExchangeConnectivity string `json:"exchange_connectivity"`
}

func (h *HealthHandler) checkCCXTService() error {
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	resp, err := client.Get(h.ccxtURL + "/health")
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("CCXT service returned status: %d", resp.StatusCode)
	}

	// Parse the response to check detailed health
	var healthResp CCXTHealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		// A 200 OK with an unparseable body is a contract violation and should be an error.
		// This indicates either a response format change or a CCXT service issue.
		return fmt.Errorf("failed to parse CCXT health response: %w", err)
	}

	// Verify CCXT service has active exchanges (only if the field was present and parsed)
	// If exchanges_count is 0 but status is "healthy", it could be a startup condition
	if healthResp.ExchangesCount == 0 && healthResp.Status != "" && healthResp.Status != "healthy" {
		return fmt.Errorf("CCXT service has no active exchanges")
	}

	return nil
}

// Global start time for uptime calculation
var startTime = time.Now()

// ReadinessCheck checks if the service is ready to accept traffic.
// This is typically used by load balancers or Kubernetes.
// It performs comprehensive checks on all critical dependencies.
//
// Parameters:
//
//	w: HTTP response writer.
//	r: HTTP request.
func (h *HealthHandler) ReadinessCheck(w http.ResponseWriter, r *http.Request) {
	span := sentry.StartSpan(r.Context(), "readiness_check")
	defer span.Finish()
	ctx := span.Context()

	span.SetTag("http.method", r.Method)
	span.SetTag("http.url", r.URL.String())
	span.SetTag("handler.name", "ReadinessCheck")

	// Comprehensive readiness checks for all services
	servicesStatus := make(map[string]string)
	allReady := true

	// Check database (critical)
	if h.db != nil {
		if err := h.db.HealthCheck(ctx); err == nil {
			servicesStatus["database"] = "ready"
			span.SetTag("database.readiness", "ready")
		} else {
			servicesStatus["database"] = "not ready"
			span.SetTag("database.readiness", "not_ready")
			sentry.CaptureException(err)
			allReady = false
		}
	} else {
		servicesStatus["database"] = "not configured"
		span.SetTag("database.readiness", "not_configured")
		allReady = false
	}

	// Check Redis (critical for caching)
	if h.redis != nil {
		if err := h.redis.HealthCheck(ctx); err == nil {
			servicesStatus["redis"] = "ready"
			span.SetTag("redis.readiness", "ready")
		} else {
			servicesStatus["redis"] = "not ready"
			span.SetTag("redis.readiness", "not_ready")
			sentry.CaptureException(err)
			allReady = false
		}
	} else {
		servicesStatus["redis"] = "not configured"
		span.SetTag("redis.readiness", "not_configured")
		allReady = false
	}

	// Check CCXT service (important for market data)
	// CCXT unavailability marks service as degraded, not unready
	if err := h.checkCCXTService(); err != nil {
		servicesStatus["ccxt"] = "degraded"
		span.SetTag("ccxt.readiness", "degraded")
		sentry.CaptureException(err)
	} else {
		servicesStatus["ccxt"] = "ready"
		span.SetTag("ccxt.readiness", "ready")
	}

	// Set appropriate status code
	if !allReady {
		span.Status = sentry.SpanStatusUnavailable
		w.WriteHeader(http.StatusServiceUnavailable)
	} else {
		span.Status = sentry.SpanStatusOK
		w.WriteHeader(http.StatusOK)
	}

	if err := json.NewEncoder(w).Encode(map[string]interface{}{
		"ready":    allReady,
		"services": servicesStatus,
	}); err != nil {
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// LivenessCheck checks if the service is alive.
// This is a lightweight check to confirm the process is running.
//
// Parameters:
//
//	w: HTTP response writer.
//	r: HTTP request.
func (h *HealthHandler) LivenessCheck(w http.ResponseWriter, r *http.Request) {
	span := sentry.StartSpan(r.Context(), "liveness_check")
	defer span.Finish()

	span.SetTag("http.method", r.Method)
	span.SetTag("http.url", r.URL.String())
	span.SetTag("handler.name", "LivenessCheck")

	// Simple liveness check - just ensure the app is responsive
	span.Status = sentry.SpanStatusOK
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(map[string]string{
		"status":    "alive",
		"timestamp": time.Now().Format(time.RFC3339),
	}); err != nil {
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
		http.Error(w, "Failed to encode response", http.StatusInternalServerError)
	}
}

// RiskMetricsResponse represents the risk metrics response.
type RiskMetricsResponse struct {
	// Status is the overall risk status.
	Status string `json:"status"`
	// Timestamp is the check time.
	Timestamp time.Time `json:"timestamp"`
	// Metrics contains detailed risk metrics.
	Metrics RiskMetrics `json:"metrics"`
}

// RiskMetrics contains detailed risk information.
type RiskMetrics struct {
	// SystemRisk is the overall system risk score (0-100).
	SystemRisk int `json:"system_risk"`
	// ExchangeRisk is the exchange reliability risk score (0-20).
	ExchangeRisk int `json:"exchange_risk"`
	// LiquidityRisk is the liquidity risk score (0-20).
	LiquidityRisk int `json:"liquidity_risk"`
	// VolatilityRisk is the market volatility risk score (0-20).
	VolatilityRisk int `json:"volatility_risk"`
	// OperationalRisk is the operational risk score (0-20).
	OperationalRisk int `json:"operational_risk"`
	// ActiveExchanges is the count of active exchanges.
	ActiveExchanges int `json:"active_exchanges"`
	// FailedExchanges is the count of failed exchanges.
	FailedExchanges int `json:"failed_exchanges"`
	// LastRiskUpdate is the timestamp of last risk calculation.
	LastRiskUpdate time.Time `json:"last_risk_update"`
}

// GetRiskMetrics returns current risk metrics for the system.
// Note: This is a placeholder implementation. Real risk metrics should be
// calculated from ExchangeReliabilityTracker and other services in a follow-up.
func (h *HealthHandler) GetRiskMetrics(w http.ResponseWriter, r *http.Request) {
	span := sentry.StartSpan(r.Context(), "risk_metrics")
	defer span.Finish()

	span.SetTag("http.method", r.Method)
	span.SetTag("http.url", r.URL.String())
	span.SetTag("handler.name", "GetRiskMetrics")

	metrics := RiskMetrics{
		SystemRisk:      15,
		ExchangeRisk:    5,
		LiquidityRisk:   3,
		VolatilityRisk:  5,
		OperationalRisk: 2,
		ActiveExchanges: 6,
		FailedExchanges: 0,
		LastRiskUpdate:  time.Now(),
	}

	span.Status = sentry.SpanStatusOK
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(RiskMetricsResponse{
		Status:    "healthy",
		Timestamp: time.Now(),
		Metrics:   metrics,
	}); err != nil {
		sentry.CaptureException(err)
		span.Status = sentry.SpanStatusInternalError
	}
}
