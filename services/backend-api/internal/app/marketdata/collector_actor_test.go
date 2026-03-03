package marketdata

import (
	"context"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/domain/marketdata"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
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
	a.mu.RLock()
	state, exists := a.exchangeStates["binance"]
	a.mu.RUnlock()
	assert.True(t, exists)
	assert.NotNil(t, state)
	if state == nil {
		return
	}

	state.mu.RLock()
	assert.True(t, state.Enabled)
	originalTimerStop := state.TimerStop
	state.mu.RUnlock()
	assert.NotNil(t, originalTimerStop)

	// Stop exchange
	stopCmd := marketdata.StopExchangeCommand{
		ExchangeID: "binance",
	}
	err = a.Receive(ctx, actor.Envelope{Message: stopCmd})
	assert.NoError(t, err)
	state.mu.RLock()
	assert.False(t, state.Enabled)
	state.mu.RUnlock()

	// Stop again should be idempotent and must not panic.
	err = a.Receive(ctx, actor.Envelope{Message: stopCmd})
	assert.NoError(t, err)

	// Start again should recreate loop channels for a clean restart.
	err = a.Receive(ctx, actor.Envelope{Message: startCmd})
	assert.NoError(t, err)
	state.mu.RLock()
	assert.True(t, state.Enabled)
	assert.NotEqual(t, originalTimerStop, state.TimerStop)
	state.mu.RUnlock()
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
	a.mu.RLock()
	state := a.exchangeStates["binance"]
	a.mu.RUnlock()
	assert.NotNil(t, state)
	if state == nil {
		return
	}

	state.mu.RLock()
	originalTimerStop := state.TimerStop
	state.mu.RUnlock()

	// Pause exchange
	pauseCmd := marketdata.PauseExchangeCommand{
		ExchangeID: "binance",
	}
	err = a.Receive(ctx, actor.Envelope{Message: pauseCmd})
	assert.NoError(t, err)
	state.mu.RLock()
	assert.True(t, state.Paused)
	state.mu.RUnlock()

	// Resume exchange
	resumeCmd := marketdata.ResumeExchangeCommand{
		ExchangeID: "binance",
	}
	err = a.Receive(ctx, actor.Envelope{Message: resumeCmd})
	assert.NoError(t, err)
	state.mu.RLock()
	assert.False(t, state.Paused)
	assert.Equal(t, originalTimerStop, state.TimerStop)
	state.mu.RUnlock()
}
