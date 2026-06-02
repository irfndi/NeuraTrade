package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	TradingOrdersPlacedTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "trading_orders_placed_total",
			Help: "Total number of trading orders placed, labeled by exchange, symbol, side, and status.",
		},
		[]string{"exchange", "symbol", "side", "status"},
	)

	TradingOrdersPlacementDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "trading_orders_placement_duration_seconds",
			Help:    "Duration of order placement operations in seconds, labeled by exchange.",
			Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		},
		[]string{"exchange"},
	)

	TradingPositionPnL = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "trading_position_pnl_dollars",
			Help: "Realized profit and loss in dollars, labeled by symbol and exchange.",
		},
		[]string{"symbol", "exchange"},
	)

	AIScalpingDecisionDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "ai_scalping_decision_duration_seconds",
			Help:    "Duration of AI scalping decisions in seconds, labeled by outcome.",
			Buckets: []float64{0.25, 0.5, 1, 2.5, 5, 10, 15, 30, 60, 120},
		},
		[]string{"outcome"},
	)

	RiskKillSwitchEngaged = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "risk_kill_switch_engaged",
		Help: "Whether the risk kill switch is currently engaged (1) or disengaged (0).",
	})

	RedisPingDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "redis_ping_duration_seconds",
		Help:    "Duration of Redis ping operations in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	})

	DatabasePingDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "database_ping_duration_seconds",
		Help:    "Duration of database ping operations in seconds.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	})

	LLMNonJSONPaperTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "neuratrade_llm_nonjson_paper_total",
			Help: "Total number of non-JSON LLM responses handled in paper mode, labeled by provider.",
		},
		[]string{"provider"},
	)
)

func init() {
	_ = prometheus.Register(TradingOrdersPlacedTotal)
	_ = prometheus.Register(TradingOrdersPlacementDuration)
	_ = prometheus.Register(TradingPositionPnL)
	_ = prometheus.Register(AIScalpingDecisionDuration)
	_ = prometheus.Register(RiskKillSwitchEngaged)
	_ = prometheus.Register(RedisPingDuration)
	_ = prometheus.Register(DatabasePingDuration)
	_ = prometheus.Register(LLMNonJSONPaperTotal)
	_ = prometheus.Register(collectors.NewGoCollector())
	_ = prometheus.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
}

func MetricsHandler() http.Handler {
	return promhttp.Handler()
}
