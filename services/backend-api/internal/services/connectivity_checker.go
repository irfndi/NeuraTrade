package services

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type ProviderStatus string

const (
	ProviderStatusConnected    ProviderStatus = "connected"
	ProviderStatusDisconnected ProviderStatus = "disconnected"
	ProviderStatusDegraded     ProviderStatus = "degraded"
	ProviderStatusUnknown      ProviderStatus = "unknown"
)

type ProviderType string

const (
	ProviderExchange   ProviderType = "exchange"
	ProviderMarketData ProviderType = "market_data"
	ProviderAI         ProviderType = "ai"
	ProviderTelegram   ProviderType = "telegram"
	ProviderDatabase   ProviderType = "database"
	ProviderRedis      ProviderType = "redis"
)

type ProviderCheckResult struct {
	ProviderID   string            `json:"provider_id"`
	ProviderType ProviderType      `json:"provider_type"`
	Status       ProviderStatus    `json:"status"`
	Latency      time.Duration     `json:"latency"`
	LastChecked  time.Time         `json:"last_checked"`
	ErrorMessage string            `json:"error_message,omitempty"`
	Details      map[string]string `json:"details,omitempty"`
}

type ConnectivityCheckConfig struct {
	CheckInterval    time.Duration `json:"check_interval"`
	Timeout          time.Duration `json:"timeout"`
	MaxRetries       int           `json:"max_retries"`
	RetryDelay       time.Duration `json:"retry_delay"`
	FailureThreshold int           `json:"failure_threshold"`
}

func DefaultConnectivityCheckConfig() ConnectivityCheckConfig {
	return ConnectivityCheckConfig{
		CheckInterval:    30 * time.Second,
		Timeout:          5 * time.Second,
		MaxRetries:       3,
		RetryDelay:       1 * time.Second,
		FailureThreshold: 3,
	}
}

type ConnectivityChecker struct {
	config    ConnectivityCheckConfig
	providers map[string]*ProviderInfo
	results   map[string]*ProviderCheckResult
	metrics   ConnectivityMetrics
	mu        sync.RWMutex
}

type ProviderInfo struct {
	ID           string
	ProviderType ProviderType
	CheckFunc    func(ctx context.Context) (ProviderStatus, time.Duration, error)
	FailureCount int
}

type ConnectivityMetrics struct {
	mu               sync.RWMutex
	TotalChecks      int64             `json:"total_checks"`
	SuccessfulChecks int64             `json:"successful_checks"`
	FailedChecks     int64             `json:"failed_checks"`
	AvgLatency       time.Duration     `json:"avg_latency"`
	ChecksByProvider map[string]int64  `json:"checks_by_provider"`
	StatusByProvider map[string]string `json:"status_by_provider"`
}

func (m *ConnectivityMetrics) IncrementTotal() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalChecks++
}

func (m *ConnectivityMetrics) IncrementSuccess() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SuccessfulChecks++
}

func (m *ConnectivityMetrics) IncrementFailed() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.FailedChecks++
}

func (m *ConnectivityMetrics) UpdateLatency(latency time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := m.TotalChecks
	if total == 1 {
		m.AvgLatency = latency
	} else {
		currentNs := m.AvgLatency.Nanoseconds() * (total - 1)
		m.AvgLatency = time.Duration((currentNs + latency.Nanoseconds()) / total)
	}
}

func (m *ConnectivityMetrics) IncrementByProvider(providerID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ChecksByProvider == nil {
		m.ChecksByProvider = make(map[string]int64)
	}
	m.ChecksByProvider[providerID]++
}

func (m *ConnectivityMetrics) SetProviderStatus(providerID string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.StatusByProvider == nil {
		m.StatusByProvider = make(map[string]string)
	}
	m.StatusByProvider[providerID] = status
}

func (m *ConnectivityMetrics) GetMetrics() ConnectivityMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return ConnectivityMetrics{
		TotalChecks:      m.TotalChecks,
		SuccessfulChecks: m.SuccessfulChecks,
		FailedChecks:     m.FailedChecks,
		AvgLatency:       m.AvgLatency,
		ChecksByProvider: m.ChecksByProvider,
		StatusByProvider: m.StatusByProvider,
	}
}

func NewConnectivityChecker(config ConnectivityCheckConfig) *ConnectivityChecker {
	return &ConnectivityChecker{
		config:    config,
		providers: make(map[string]*ProviderInfo),
		results:   make(map[string]*ProviderCheckResult),
		metrics:   ConnectivityMetrics{ChecksByProvider: make(map[string]int64), StatusByProvider: make(map[string]string)},
	}
}

func (c *ConnectivityChecker) RegisterProvider(id string, providerType ProviderType, checkFunc func(ctx context.Context) (ProviderStatus, time.Duration, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.providers[id] = &ProviderInfo{
		ID:           id,
		ProviderType: providerType,
		CheckFunc:    checkFunc,
	}
}

func (c *ConnectivityChecker) UnregisterProvider(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.providers, id)
	delete(c.results, id)
}

func (c *ConnectivityChecker) CheckProvider(ctx context.Context, providerID string) (*ProviderCheckResult, error) {
	c.mu.RLock()
	provider, ok := c.providers[providerID]
	c.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("provider not found: %s", providerID)
	}

	c.metrics.IncrementTotal()
	c.metrics.IncrementByProvider(providerID)

	checkCtx, cancel := context.WithTimeout(ctx, c.config.Timeout)
	defer cancel()

	status, latency, err := provider.CheckFunc(checkCtx)

	result := &ProviderCheckResult{
		ProviderID:   providerID,
		ProviderType: provider.ProviderType,
		Status:       status,
		Latency:      latency,
		LastChecked:  time.Now().UTC(),
		Details:      make(map[string]string),
	}

	if err != nil {
		result.ErrorMessage = err.Error()
		c.mu.Lock()
		provider.FailureCount++
		if provider.FailureCount >= c.config.FailureThreshold {
			result.Status = ProviderStatusDisconnected
		}
		c.mu.Unlock()
		c.metrics.IncrementFailed()
	} else {
		c.mu.Lock()
		provider.FailureCount = 0
		c.mu.Unlock()
		c.metrics.IncrementSuccess()
	}

	c.metrics.UpdateLatency(latency)
	c.metrics.SetProviderStatus(providerID, string(result.Status))

	c.mu.Lock()
	c.results[providerID] = result
	c.mu.Unlock()

	return result, nil
}

func (c *ConnectivityChecker) CheckAll(ctx context.Context) ([]*ProviderCheckResult, error) {
	c.mu.RLock()
	providerIDs := make([]string, 0, len(c.providers))
	for id := range c.providers {
		providerIDs = append(providerIDs, id)
	}
	c.mu.RUnlock()

	results := make([]*ProviderCheckResult, 0, len(providerIDs))
	for _, id := range providerIDs {
		result, err := c.CheckProvider(ctx, id)
		if err != nil {
			continue
		}
		results = append(results, result)
	}

	return results, nil
}

func (c *ConnectivityChecker) GetResult(providerID string) (*ProviderCheckResult, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.results[providerID]
	return result, ok
}

func (c *ConnectivityChecker) GetAllResults() map[string]*ProviderCheckResult {
	c.mu.RLock()
	defer c.mu.RUnlock()

	results := make(map[string]*ProviderCheckResult, len(c.results))
	for k, v := range c.results {
		results[k] = v
	}
	return results
}

func (c *ConnectivityChecker) GetMetrics() ConnectivityMetrics {
	return c.metrics.GetMetrics()
}

func (c *ConnectivityChecker) IsHealthy() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	for _, result := range c.results {
		if result.Status == ProviderStatusDisconnected {
			return false
		}
	}
	return true
}

func (c *ConnectivityChecker) GetUnhealthyProviders() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	unhealthy := make([]string, 0)
	for id, result := range c.results {
		if result.Status == ProviderStatusDisconnected || result.Status == ProviderStatusDegraded {
			unhealthy = append(unhealthy, id)
		}
	}
	return unhealthy
}

func (c *ConnectivityChecker) Summary() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	healthy := 0
	unhealthy := 0
	degraded := 0

	for _, result := range c.results {
		switch result.Status {
		case ProviderStatusConnected:
			healthy++
		case ProviderStatusDisconnected:
			unhealthy++
		case ProviderStatusDegraded:
			degraded++
		}
	}

	return map[string]interface{}{
		"total_providers": len(c.providers),
		"healthy":         healthy,
		"unhealthy":       unhealthy,
		"degraded":        degraded,
		"is_healthy":      c.IsHealthy(),
	}
}

func CreateDatabaseCheckFunc(db DBPool) func(ctx context.Context) (ProviderStatus, time.Duration, error) {
	return func(ctx context.Context) (ProviderStatus, time.Duration, error) {
		start := time.Now()
		err := db.QueryRow(ctx, "SELECT 1").Scan(new(int))
		latency := time.Since(start)

		if err != nil {
			return ProviderStatusDisconnected, latency, err
		}
		return ProviderStatusConnected, latency, nil
	}
}

func CreateRedisCheckFunc(redisClient interface {
	Ping(ctx context.Context) error
}) func(ctx context.Context) (ProviderStatus, time.Duration, error) {
	return func(ctx context.Context) (ProviderStatus, time.Duration, error) {
		start := time.Now()
		err := redisClient.Ping(ctx)
		latency := time.Since(start)

		if err != nil {
			return ProviderStatusDisconnected, latency, err
		}
		return ProviderStatusConnected, latency, nil
	}
}
