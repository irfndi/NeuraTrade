package services

import (
	"fmt"
	"sync"
	"time"
)

// PerformanceMetric represents a single metric data point
type PerformanceMetric struct {
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Unit      string            `json:"unit"`
	Timestamp time.Time         `json:"timestamp"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// MetricSnapshot represents a collection of metrics at a point in time
type MetricSnapshot struct {
	ID        string              `json:"id"`
	Timestamp time.Time           `json:"timestamp"`
	Metrics   []PerformanceMetric `json:"metrics"`
	Metadata  map[string]string   `json:"metadata,omitempty"`
}

// PerformanceMetricsCollector collects and aggregates performance metrics
type PerformanceMetricsCollector struct {
	metrics  map[string][]PerformanceMetric
	mu       sync.RWMutex
	interval time.Duration
	handlers []MetricsHandler
}

// MetricsHandler processes collected metrics
type MetricsHandler interface {
	Handle(snapshot *MetricSnapshot) error
}

// NewPerformanceMetricsCollector creates a new metrics collector
func NewPerformanceMetricsCollector(interval time.Duration) *PerformanceMetricsCollector {
	return &PerformanceMetricsCollector{
		metrics:  make(map[string][]PerformanceMetric),
		interval: interval,
		handlers: make([]MetricsHandler, 0),
	}
}

// AddHandler registers a metrics handler
func (c *PerformanceMetricsCollector) AddHandler(handler MetricsHandler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlers = append(c.handlers, handler)
}

// Record records a single metric
func (c *PerformanceMetricsCollector) Record(name string, value float64, unit string, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	metric := PerformanceMetric{
		Name:      name,
		Value:     value,
		Unit:      unit,
		Timestamp: time.Now(),
		Labels:    labels,
	}

	c.metrics[name] = append(c.metrics[name], metric)

	// Keep only last 1000 points per metric to prevent memory bloat
	if len(c.metrics[name]) > 1000 {
		c.metrics[name] = c.metrics[name][len(c.metrics[name])-1000:]
	}
}

// Collect creates a snapshot of current metrics
func (c *PerformanceMetricsCollector) Collect() *MetricSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()

	snapshot := &MetricSnapshot{
		ID:        fmt.Sprintf("snapshot_%d", time.Now().UnixNano()),
		Timestamp: time.Now(),
		Metrics:   make([]PerformanceMetric, 0),
		Metadata: map[string]string{
			"collector_version": "1.0",
		},
	}

	for _, metrics := range c.metrics {
		if len(metrics) > 0 {
			snapshot.Metrics = append(snapshot.Metrics, metrics[len(metrics)-1])
		}
	}

	return snapshot
}

// Emit sends collected metrics to all handlers
func (c *PerformanceMetricsCollector) Emit() error {
	snapshot := c.Collect()

	c.mu.RLock()
	handlers := make([]MetricsHandler, len(c.handlers))
	copy(handlers, c.handlers)
	c.mu.RUnlock()

	var errs []error
	for _, handler := range handlers {
		if err := handler.Handle(snapshot); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors emitting metrics: %v", errs)
	}

	return nil
}

// GetMetricHistory returns historical values for a metric
func (c *PerformanceMetricsCollector) GetMetricHistory(name string, limit int) []PerformanceMetric {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics, exists := c.metrics[name]
	if !exists {
		return nil
	}

	if limit <= 0 || limit > len(metrics) {
		limit = len(metrics)
	}

	return metrics[len(metrics)-limit:]
}

// GetAverage returns the average value for a metric over time
func (c *PerformanceMetricsCollector) GetAverage(name string) float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	metrics, exists := c.metrics[name]
	if !exists || len(metrics) == 0 {
		return 0
	}

	sum := 0.0
	for _, m := range metrics {
		sum += m.Value
	}

	return sum / float64(len(metrics))
}

// Reset clears all metrics
func (c *PerformanceMetricsCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = make(map[string][]PerformanceMetric)
}

// ConsoleMetricsHandler outputs metrics to console
type ConsoleMetricsHandler struct{}

func (h *ConsoleMetricsHandler) Handle(snapshot *MetricSnapshot) error {
	fmt.Printf("[METRICS] %s - %d metrics\n", snapshot.Timestamp.Format(time.RFC3339), len(snapshot.Metrics))
	for _, m := range snapshot.Metrics {
		fmt.Printf("  %s: %.2f %s\n", m.Name, m.Value, m.Unit)
	}
	return nil
}

// LogMetricsHandler outputs metrics via structured logging
type LogMetricsHandler struct {
	logger interface {
		Info(args ...interface{})
	}
}

func NewLogMetricsHandler(logger interface{ Info(args ...interface{}) }) *LogMetricsHandler {
	return &LogMetricsHandler{logger: logger}
}

func (h *LogMetricsHandler) Handle(snapshot *MetricSnapshot) error {
	for _, m := range snapshot.Metrics {
		h.logger.Info(fmt.Sprintf("metric: %s=%.2f%s", m.Name, m.Value, m.Unit))
	}
	return nil
}

// BacktestMetrics represents aggregated backtest performance metrics
type BacktestMetrics struct {
	TotalTrades   int     `json:"total_trades"`
	WinningTrades int     `json:"winning_trades"`
	LosingTrades  int     `json:"losing_trades"`
	WinRate       float64 `json:"win_rate"`
	AverageReturn float64 `json:"average_return"`
	MaxDrawdown   float64 `json:"max_drawdown"`
	SharpeRatio   float64 `json:"sharpe_ratio"`
	TotalReturn   float64 `json:"total_return"`
	Volatility    float64 `json:"volatility"`
	ProfitFactor  float64 `json:"profit_factor"`
	Expectancy    float64 `json:"expectancy"`
	CalmarRatio   float64 `json:"calmar_ratio"`
	SortinoRatio  float64 `json:"sortino_ratio"`
}

// CalculateBacktestMetrics calculates comprehensive backtest metrics
func CalculateBacktestMetrics(returns []float64) *BacktestMetrics {
	if len(returns) == 0 {
		return &BacktestMetrics{}
	}

	m := &BacktestMetrics{
		TotalTrades: len(returns),
	}

	var sum, positiveSum, negativeSum float64
	maxReturn := returns[0]
	minReturn := returns[0]

	for _, r := range returns {
		sum += r
		if r > 0 {
			m.WinningTrades++
			positiveSum += r
		} else if r < 0 {
			m.LosingTrades++
			negativeSum += -r
		}
		if r > maxReturn {
			maxReturn = r
		}
		if r < minReturn {
			minReturn = r
		}
	}

	m.AverageReturn = sum / float64(len(returns))
	m.WinRate = float64(m.WinningTrades) / float64(len(returns)) * 100
	m.MaxDrawdown = minReturn
	m.TotalReturn = sum

	if negativeSum > 0 {
		m.ProfitFactor = positiveSum / negativeSum
	}

	if m.TotalTrades > 0 {
		m.Expectancy = m.AverageReturn
	}

	// Calculate volatility (standard deviation)
	var varianceSum float64
	for _, r := range returns {
		diff := r - m.AverageReturn
		varianceSum += diff * diff
	}
	m.Volatility = varianceSum / float64(len(returns))

	// Sharpe ratio (simplified, assuming risk-free rate of 0)
	if m.Volatility > 0 {
		m.SharpeRatio = m.AverageReturn / m.Volatility
	}

	return m
}
