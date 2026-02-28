package marketdata

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/domain/marketdata"
	"github.com/irfndi/neuratrade/internal/logging"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
)

const CollectorActorID = "marketdata-collector"

type CollectorActor struct {
	logger          *zaplogrus.Logger
	db              database.DBPool
	exchange        ports.ExchangeRegistry
	eventBus        *eventbus.Bus
	exchangeStates  map[string]*ExchangeState
	defaultInterval time.Duration
	mu              sync.RWMutex
}

type ExchangeState struct {
	ExchangeID string
	Enabled    bool
	Paused     bool
	Symbols    []string
	Interval   time.Duration
	TimerStop  chan struct{}
	mu         sync.RWMutex
}

type Config struct {
	DefaultInterval time.Duration
}

func DefaultConfig() Config {
	return Config{DefaultInterval: 5 * time.Minute}
}

func NewCollectorActor(db database.DBPool, exchange ports.ExchangeRegistry, eventBus *eventbus.Bus, config Config) *CollectorActor {
	logger := zaplogrus.New()
	logger.SetLevel(logging.ParseLogrusLevel("info"))
	return &CollectorActor{
		logger:          logger,
		db:              db,
		exchange:        exchange,
		eventBus:        eventBus,
		exchangeStates:  make(map[string]*ExchangeState),
		defaultInterval: config.DefaultInterval,
	}
}

func (a *CollectorActor) ID() string { return CollectorActorID }

func (a *CollectorActor) Receive(ctx context.Context, env actor.Envelope) error {
	traceID := env.TraceID
	if traceID == "" {
		traceID = uuid.New().String()
	}
	switch msg := env.Message.(type) {
	case marketdata.StartExchangeCommand:
		return a.handleStartExchange(ctx, traceID, msg)
	case marketdata.StopExchangeCommand:
		return a.handleStopExchange(ctx, traceID, msg)
	case marketdata.PauseExchangeCommand:
		return a.handlePauseExchange(ctx, traceID, msg)
	case marketdata.ResumeExchangeCommand:
		return a.handleResumeExchange(ctx, traceID, msg)
	case marketdata.HealthCheckCommand:
		return a.handleHealthCheck(ctx, traceID, msg, env)
	case marketdata.GetStatsCommand:
		return a.handleGetStats(ctx, traceID, msg, env)
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
}

func (a *CollectorActor) handleStartExchange(ctx context.Context, traceID string, msg marketdata.StartExchangeCommand) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, exists := a.exchangeStates[msg.ExchangeID]; exists {
		return nil
	}
	state := &ExchangeState{
		ExchangeID: msg.ExchangeID,
		Enabled:    true,
		Symbols:    msg.Symbols,
		Interval:   msg.Interval,
		TimerStop:  make(chan struct{}),
	}
	if state.Interval == 0 {
		state.Interval = a.defaultInterval
	}
	a.exchangeStates[msg.ExchangeID] = state
	go a.runCollectionLoop(ctx, state)
	a.publishEvent(ctx, "collector.status", "started", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "started", Timestamp: time.Now(),
	})
	return nil
}

func (a *CollectorActor) handleStopExchange(ctx context.Context, traceID string, msg marketdata.StopExchangeCommand) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("exchange %s not found", msg.ExchangeID)
	}
	state.mu.Lock()
	close(state.TimerStop)
	state.Enabled = false
	state.mu.Unlock()
	a.publishEvent(ctx, "collector.status", "stopped", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "stopped", Timestamp: time.Now(),
	})
	return nil
}

func (a *CollectorActor) handlePauseExchange(ctx context.Context, traceID string, msg marketdata.PauseExchangeCommand) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("exchange %s not found", msg.ExchangeID)
	}
	state.mu.Lock()
	state.Paused = true
	state.mu.Unlock()
	a.publishEvent(ctx, "collector.status", "paused", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "paused", Timestamp: time.Now(),
	})
	return nil
}

func (a *CollectorActor) handleResumeExchange(ctx context.Context, traceID string, msg marketdata.ResumeExchangeCommand) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("exchange %s not found", msg.ExchangeID)
	}
	state.mu.Lock()
	state.Paused = false
	state.mu.Unlock()
	go a.runCollectionLoop(ctx, state)
	a.publishEvent(ctx, "collector.status", "resumed", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "resumed", Timestamp: time.Now(),
	})
	return nil
}

func (a *CollectorActor) handleHealthCheck(ctx context.Context, traceID string, msg marketdata.HealthCheckCommand, env actor.Envelope) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		if env.Reply != nil {
			env.Reply <- marketdata.HealthCheckResponse{ExchangeID: msg.ExchangeID, Healthy: false}
		}
		return nil
	}
	state.mu.RLock()
	healthy := state.Enabled && !state.Paused
	ready := state.Enabled && len(state.Symbols) > 0
	state.mu.RUnlock()
	if env.Reply != nil {
		env.Reply <- marketdata.HealthCheckResponse{
			ExchangeID: msg.ExchangeID,
			Healthy:    healthy,
			Ready:      ready,
		}
	}
	return nil
}

func (a *CollectorActor) handleGetStats(ctx context.Context, traceID string, msg marketdata.GetStatsCommand, env actor.Envelope) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		if env.Reply != nil {
			env.Reply <- marketdata.GetStatsResponse{ExchangeID: msg.ExchangeID}
		}
		return nil
	}
	state.mu.RLock()
	symbolsCount := len(state.Symbols)
	state.mu.RUnlock()
	if env.Reply != nil {
		env.Reply <- marketdata.GetStatsResponse{
			ExchangeID:   msg.ExchangeID,
			SymbolsCount: symbolsCount,
		}
	}
	return nil
}

func (a *CollectorActor) runCollectionLoop(ctx context.Context, state *ExchangeState) {
	ticker := time.NewTicker(state.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-state.TimerStop:
			return
		case <-ticker.C:
			state.mu.RLock()
			enabled := state.Enabled
			paused := state.Paused
			exchangeID := state.ExchangeID
			symbols := state.Symbols
			state.mu.RUnlock()
			if enabled && !paused {
				a.collectFromExchange(ctx, exchangeID, symbols)
			}
		}
	}
}

func (a *CollectorActor) collectFromExchange(ctx context.Context, exchangeID string, symbols []string) {
	gw, err := a.exchange.GetMarketDataGateway(exchangeID)
	if err != nil || gw == nil {
		return
	}
	for _, symbol := range symbols {
		tick, err := gw.FetchTick(ctx, exchangeID, symbol)
		if err != nil {
			continue
		}
		_ = a.eventBus.PublishSync(ctx, eventbus.Event{
			Topic: "market.tick",
			Type:  "tick",
			Payload: marketdata.MarketTickEvent{
				TraceID:   uuid.New().String(),
				Exchange:  exchangeID,
				Symbol:    symbol,
				Bid:       tick.Bid,
				Ask:       tick.Ask,
				Last:      tick.Last,
				Volume:    tick.Volume,
				Timestamp: tick.Timestamp,
			},
			Source:    CollectorActorID,
			Timestamp: time.Now(),
		})
	}
}

func (a *CollectorActor) publishEvent(ctx context.Context, topic, eventType string, payload interface{}) {
	_ = a.eventBus.PublishSync(ctx, eventbus.Event{
		Topic:     topic,
		Type:      eventType,
		Payload:   payload,
		Source:    CollectorActorID,
		Timestamp: time.Now(),
	})
}
