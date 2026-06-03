package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/domain/marketdata"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCollectorActor_ID(t *testing.T) {
	a := &CollectorActor{}
	assert.Equal(t, CollectorActorID, a.ID())
}

func TestCollectorActor_DefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 5*time.Minute, cfg.DefaultInterval)
}

func TestCollectorActor_Receive_HealthCheck(t *testing.T) {
	eventBus := eventbus.New(eventbus.DefaultConfig())
	a := NewCollectorActor(nil, nil, eventBus, DefaultConfig())

	ctx := context.Background()
	reply := make(chan interface{}, 1)

	cmd := marketdata.HealthCheckCommand{
		ExchangeID: "binance",
	}

	env := actor.Envelope{
		Message: cmd,
		Reply:   reply,
	}

	// Actor should return healthy=false for non-existent exchange
	err := a.Receive(ctx, env)
	assert.NoError(t, err)

	select {
	case resp := <-reply:
		healthResp, ok := resp.(marketdata.HealthCheckResponse)
		assert.True(t, ok)
		assert.False(t, healthResp.Healthy)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for health check response")
	}
}

func TestCollectorActor_Receive_StopExchange_NotFound(t *testing.T) {
	eventBus := eventbus.New(eventbus.DefaultConfig())
	a := NewCollectorActor(nil, nil, eventBus, DefaultConfig())

	ctx := context.Background()

	cmd := marketdata.StopExchangeCommand{
		ExchangeID: "nonexistent",
	}

	err := a.Receive(ctx, actor.Envelope{Message: cmd})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCollectorActor_Receive_GetStats(t *testing.T) {
	eventBus := eventbus.New(eventbus.DefaultConfig())
	a := NewCollectorActor(nil, nil, eventBus, DefaultConfig())

	ctx := context.Background()
	reply := make(chan interface{}, 1)

	cmd := marketdata.GetStatsCommand{
		ExchangeID: "binance",
	}

	env := actor.Envelope{
		Message: cmd,
		Reply:   reply,
	}

	err := a.Receive(ctx, env)
	assert.NoError(t, err)

	select {
	case resp := <-reply:
		statsResp, ok := resp.(marketdata.GetStatsResponse)
		assert.True(t, ok)
		assert.Equal(t, "binance", statsResp.ExchangeID)
		assert.Equal(t, 0, statsResp.SymbolsCount)
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for stats response")
	}
}

func TestCollectorActor_Receive_StartStopExchange(t *testing.T) {
	eventBus := eventbus.New(eventbus.DefaultConfig())
	a := NewCollectorActor(nil, nil, eventBus, DefaultConfig())

	ctx := context.Background()

	// Start exchange
	startCmd := marketdata.StartExchangeCommand{
		ExchangeID: "binance",
		Symbols:    []string{"BTC/USDT"},
		Interval:   1 * time.Minute,
	}
	err := a.Receive(ctx, actor.Envelope{Message: startCmd})
	assert.NoError(t, err)

	// Stop exchange
	stopCmd := marketdata.StopExchangeCommand{
		ExchangeID: "binance",
	}
	err = a.Receive(ctx, actor.Envelope{Message: stopCmd})
	assert.NoError(t, err)
}

func TestCollectorActor_Receive_PauseResumeExchange(t *testing.T) {
	eventBus := eventbus.New(eventbus.DefaultConfig())
	a := NewCollectorActor(nil, nil, eventBus, DefaultConfig())

	ctx := context.Background()

	// Start exchange
	startCmd := marketdata.StartExchangeCommand{
		ExchangeID: "binance",
		Symbols:    []string{"BTC/USDT"},
		Interval:   1 * time.Minute,
	}
	err := a.Receive(ctx, actor.Envelope{Message: startCmd})
	assert.NoError(t, err)

	// Pause exchange
	pauseCmd := marketdata.PauseExchangeCommand{
		ExchangeID: "binance",
	}
	err = a.Receive(ctx, actor.Envelope{Message: pauseCmd})
	assert.NoError(t, err)

	resumeCmd := marketdata.ResumeExchangeCommand{
		ExchangeID: "binance",
	}
	err = a.Receive(ctx, actor.Envelope{Message: resumeCmd})
	assert.NoError(t, err)
}

func TestComputeOrderBookMetrics_USDWeightedOnePct(t *testing.T) {
	bids := []ports.PriceLevel{
		{Price: decimal.NewFromInt(100), Amount: decimal.NewFromInt(5)},
		{Price: decimal.NewFromFloat(99.5), Amount: decimal.NewFromInt(2)},
		{Price: decimal.NewFromInt(90), Amount: decimal.NewFromInt(50)},
	}
	asks := []ports.PriceLevel{
		{Price: decimal.NewFromInt(100), Amount: decimal.NewFromInt(3)},
		{Price: decimal.NewFromFloat(100.5), Amount: decimal.NewFromInt(4)},
		{Price: decimal.NewFromInt(110), Amount: decimal.NewFromInt(50)},
	}
	bidDepth, askDepth, imbalance, spreadPct, liquidityScore := computeOrderBookMetrics(bids, asks)

	wantBid := decimal.NewFromInt(699)
	wantAsk := decimal.NewFromInt(702)
	assert.True(t, bidDepth.Equal(wantBid), "BidDepth1Pct: want %s, got %s", wantBid, bidDepth)
	assert.True(t, askDepth.Equal(wantAsk), "AskDepth1Pct: want %s, got %s", wantAsk, askDepth)

	wantImbalance, _ := decimal.NewFromString("-0.0021")
	gotRounded := imbalance.Round(4)
	assert.True(t, gotRounded.Equal(wantImbalance),
		"Imbalance1Pct: want %s, got %s (raw=%s)", wantImbalance, gotRounded, imbalance)

	assert.True(t, spreadPct.IsZero(), "BidAskSpread: want 0, got %s", spreadPct)

	assert.True(t, liquidityScore.GreaterThan(decimal.NewFromInt(40)),
		"LiquidityScore should be >40 with 0 spread, got %s", liquidityScore)
	assert.True(t, liquidityScore.LessThan(decimal.NewFromInt(41)),
		"LiquidityScore should be <41 with $1401 depth, got %s", liquidityScore)
}

func TestComputeOrderBookMetrics_EmptyBook(t *testing.T) {
	bidDepth, askDepth, imbalance, spreadPct, liquidityScore := computeOrderBookMetrics(nil, nil)
	assert.True(t, bidDepth.IsZero())
	assert.True(t, askDepth.IsZero())
	assert.True(t, imbalance.IsZero())
	assert.True(t, spreadPct.IsZero())
	assert.True(t, liquidityScore.IsZero())
}

func TestComputeOrderBookMetrics_AllLevelsOutsideOnePct(t *testing.T) {
	bids := []ports.PriceLevel{
		{Price: decimal.NewFromInt(80), Amount: decimal.NewFromInt(1)},
	}
	asks := []ports.PriceLevel{
		{Price: decimal.NewFromInt(120), Amount: decimal.NewFromInt(1)},
	}
	bidDepth, askDepth, imbalance, _, _ := computeOrderBookMetrics(bids, asks)
	assert.True(t, bidDepth.IsZero(), "all-out-of-range bids should yield 0 depth")
	assert.True(t, askDepth.IsZero(), "all-out-of-range asks should yield 0 depth")
	assert.True(t, imbalance.IsZero(), "zero depth means zero imbalance")
}
