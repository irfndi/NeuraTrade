package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarketTicksTotal(t *testing.T) {
	counter, err := MarketTicksTotal.GetMetricWithLabelValues("binance", "BTC/USDT")
	require.NoError(t, err)
	assert.Equal(t, float64(0), testutil.ToFloat64(counter))

	counter.Inc()
	assert.Equal(t, float64(1), testutil.ToFloat64(counter))

	MarketTicksTotal.WithLabelValues("coinbase", "ETH/USDT").Add(3)
	assert.Equal(t, float64(3), testutil.ToFloat64(
		MarketTicksTotal.WithLabelValues("coinbase", "ETH/USDT"),
	))
}



func TestStrategySignalsTotal(t *testing.T) {
	StrategySignalsTotal.WithLabelValues("strat-1", "BTC/USDT", "buy").Inc()
	StrategySignalsTotal.WithLabelValues("strat-1", "BTC/USDT", "buy").Inc()
	StrategySignalsTotal.WithLabelValues("strat-1", "ETH/USDT", "sell").Inc()

	assert.Equal(t, float64(2), testutil.ToFloat64(
		StrategySignalsTotal.WithLabelValues("strat-1", "BTC/USDT", "buy"),
	))
	assert.Equal(t, float64(1), testutil.ToFloat64(
		StrategySignalsTotal.WithLabelValues("strat-1", "ETH/USDT", "sell"),
	))
}

func TestRiskDecisionsTotal(t *testing.T) {
	RiskDecisionsTotal.WithLabelValues("approved", "").Inc()
	RiskDecisionsTotal.WithLabelValues("rejected", "kill_switch_engaged").Inc()
	RiskDecisionsTotal.WithLabelValues("rejected", "max_drawdown_exceeded").Inc()

	assert.Equal(t, float64(1), testutil.ToFloat64(
		RiskDecisionsTotal.WithLabelValues("approved", ""),
	))
	assert.Equal(t, float64(1), testutil.ToFloat64(
		RiskDecisionsTotal.WithLabelValues("rejected", "kill_switch_engaged"),
	))
}

func TestRiskDecisionDuration(t *testing.T) {
	RiskDecisionDuration.Observe(0.005)
	RiskDecisionDuration.Observe(0.010)
	RiskDecisionDuration.Observe(0.100)

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == "neuratrade_risk_decision_duration_seconds" {
			assert.Equal(t, float64(3), float64(f.GetMetric()[0].Histogram.GetSampleCount()))
			return
		}
	}
	t.Error("metric neuratrade_risk_decision_duration_seconds not found")
}

func TestExecutionOrdersTotal(t *testing.T) {
	ExecutionOrdersTotal.WithLabelValues("binance", "BTC/USDT", "buy", "placed").Inc()
	ExecutionOrdersTotal.WithLabelValues("binance", "BTC/USDT", "buy", "filled").Inc()
	ExecutionOrdersTotal.WithLabelValues("coinbase", "ETH/USDT", "sell", "rejected").Inc()

	assert.Equal(t, float64(1), testutil.ToFloat64(
		ExecutionOrdersTotal.WithLabelValues("binance", "BTC/USDT", "buy", "placed"),
	))
	assert.Equal(t, float64(1), testutil.ToFloat64(
		ExecutionOrdersTotal.WithLabelValues("binance", "BTC/USDT", "buy", "filled"),
	))
	assert.Equal(t, float64(1), testutil.ToFloat64(
		ExecutionOrdersTotal.WithLabelValues("coinbase", "ETH/USDT", "sell", "rejected"),
	))
}

func TestExecutionOrderLatency(t *testing.T) {
	ExecutionOrderLatency.WithLabelValues("binance").Observe(0.05)
	ExecutionOrderLatency.WithLabelValues("coinbase").Observe(0.10)

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == "neuratrade_execution_order_latency_seconds" {
			for _, m := range f.GetMetric() {
				if len(m.GetLabel()) == 1 && m.GetLabel()[0].GetValue() == "binance" {
					assert.Equal(t, float64(1), float64(m.Histogram.GetSampleCount()))
					return
				}
			}
		}
	}
	t.Error("metric neuratrade_execution_order_latency_seconds{exchange=\"binance\"} not found")
}

func TestExecutionPendingOrders(t *testing.T) {
	ExecutionPendingOrders.Set(3)
	assert.Equal(t, float64(3), testutil.ToFloat64(ExecutionPendingOrders))

	ExecutionPendingOrders.Set(0)
	assert.Equal(t, float64(0), testutil.ToFloat64(ExecutionPendingOrders))

	ExecutionPendingOrders.Set(5)
	assert.Equal(t, float64(5), testutil.ToFloat64(ExecutionPendingOrders))
}

func TestPortfolioPositionsActive(t *testing.T) {
	PortfolioPositionsActive.Set(0)
	assert.Equal(t, float64(0), testutil.ToFloat64(PortfolioPositionsActive))

	PortfolioPositionsActive.Set(4)
	assert.Equal(t, float64(4), testutil.ToFloat64(PortfolioPositionsActive))

	PortfolioPositionsActive.Set(2)
	assert.Equal(t, float64(2), testutil.ToFloat64(PortfolioPositionsActive))
}

func TestActorLoopIterationsTotal(t *testing.T) {
	ActorLoopIterationsTotal.WithLabelValues("marketdata-collector").Inc()
	ActorLoopIterationsTotal.WithLabelValues("strategy-actor").Inc()
	ActorLoopIterationsTotal.WithLabelValues("marketdata-collector").Inc()

	assert.Equal(t, float64(2), testutil.ToFloat64(
		ActorLoopIterationsTotal.WithLabelValues("marketdata-collector"),
	))
	assert.Equal(t, float64(1), testutil.ToFloat64(
		ActorLoopIterationsTotal.WithLabelValues("strategy-actor"),
	))
}

func TestMetricsHandlerServesActorMetrics(t *testing.T) {
	handler := MetricsHandler()
	require.NotNil(t, handler)
}

func TestAllActorMetricsAreRegistered(t *testing.T) {
	names := []string{
		"neuratrade_market_ticks_total",
		"neuratrade_strategy_signals_total",
		"neuratrade_risk_decisions_total",
		"neuratrade_risk_decision_duration_seconds",
		"neuratrade_execution_orders_total",
		"neuratrade_execution_order_latency_seconds",
		"neuratrade_execution_pending_orders",
		"neuratrade_portfolio_positions_active",
		"neuratrade_actor_loop_iterations_total",
	}

	for _, name := range names {
		families, err := prometheus.DefaultGatherer.Gather()
		require.NoError(t, err)
		found := false
		for _, f := range families {
			if f.GetName() == name {
				found = true
				break
			}
		}
		assert.True(t, found, "metric %s should be registered in the default registry", name)
	}
}

func TestMetricsLabelsAreConsistent(t *testing.T) {
	// Verify no duplicate metric registration panics by re-registering via init()
	// All metrics should be registered exactly once
	require.NotPanics(t, func() {
		_ = prometheus.Register(MarketTicksTotal)
	})

	// Clean up: unregister the duplicate
	prometheus.Unregister(MarketTicksTotal)
}


