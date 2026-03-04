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
const minimumInterval = time.Second

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
	ExchangeID  string
	Enabled     bool
	Paused      bool
	Symbols     []string
	Interval    time.Duration
	TimerStop   chan struct{}
	TickerReset chan struct{}
	mu          sync.RWMutex
}

type Config struct {
	DefaultInterval time.Duration
	LogLevel        string
}

func DefaultConfig() Config {
	return Config{
		DefaultInterval: 5 * time.Minute,
		LogLevel:        "info",
	}
}

func NewCollectorActor(db database.DBPool, exchange ports.ExchangeRegistry, eventBus *eventbus.Bus, config Config) *CollectorActor {
	defaultInterval := config.DefaultInterval
	if defaultInterval <= 0 {
		defaultInterval = DefaultConfig().DefaultInterval
	}
	if defaultInterval < minimumInterval {
		defaultInterval = minimumInterval
	}

	logLevel := config.LogLevel
	if logLevel == "" {
		logLevel = DefaultConfig().LogLevel
	}

	logger := zaplogrus.New()
	logger.SetLevel(logging.ParseLogrusLevel(logLevel))

	return &CollectorActor{
		logger:          logger,
		db:              db,
		exchange:        exchange,
		eventBus:        eventBus,
		exchangeStates:  make(map[string]*ExchangeState),
		defaultInterval: defaultInterval,
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
	case marketdata.UpdateSymbolsCommand:
		return a.handleUpdateSymbols(ctx, traceID, msg)
	case marketdata.SetIntervalCommand:
		return a.handleSetInterval(ctx, traceID, msg)
	case marketdata.FetchNowCommand:
		return a.handleFetchNow(ctx, traceID, msg)
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

	interval := msg.Interval
	if interval <= 0 {
		interval = a.defaultInterval
	}
	if interval < minimumInterval {
		interval = minimumInterval
	}

	state, exists := a.exchangeStates[msg.ExchangeID]
	if exists {
		state.mu.Lock()
		if state.Enabled {
			state.mu.Unlock()
			a.mu.Unlock()
			return nil
		}

		state.Enabled = true
		state.Paused = false
		state.Symbols = append([]string(nil), msg.Symbols...)
		state.Interval = interval
		state.TimerStop = make(chan struct{})
		state.TickerReset = make(chan struct{}, 1)
		state.mu.Unlock()
	} else {
		state = &ExchangeState{
			ExchangeID:  msg.ExchangeID,
			Enabled:     true,
			Symbols:     append([]string(nil), msg.Symbols...),
			Interval:    interval,
			TimerStop:   make(chan struct{}),
			TickerReset: make(chan struct{}, 1),
		}
		a.exchangeStates[msg.ExchangeID] = state
	}
	a.mu.Unlock()

	go a.runCollectionLoop(context.WithoutCancel(ctx), state)
	if err := a.publishEvent(ctx, "collector.status", "started", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "started", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish start event")
	}

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
	if !state.Enabled {
		state.mu.Unlock()
		return nil
	}
	if state.TimerStop != nil {
		select {
		case <-state.TimerStop:
		default:
			close(state.TimerStop)
		}
	}
	state.Enabled = false
	state.Paused = false
	state.mu.Unlock()

	if err := a.publishEvent(ctx, "collector.status", "stopped", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "stopped", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish stop event")
	}

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

	if err := a.publishEvent(ctx, "collector.status", "paused", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "paused", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish pause event")
	}

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
	if !state.Enabled {
		state.mu.Unlock()
		return fmt.Errorf("exchange %s is stopped, cannot resume", msg.ExchangeID)
	}
	state.Paused = false
	state.mu.Unlock()

	if err := a.publishEvent(ctx, "collector.status", "resumed", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "resumed", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish resume event")
	}

	return nil
}

func (a *CollectorActor) handleUpdateSymbols(ctx context.Context, traceID string, msg marketdata.UpdateSymbolsCommand) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("exchange %s not found", msg.ExchangeID)
	}

	state.mu.Lock()
	state.Symbols = append([]string(nil), msg.Symbols...)
	state.mu.Unlock()

	if err := a.publishEvent(ctx, "collector.symbols_updated", "symbols_updated", marketdata.SymbolsUpdatedEvent{
		TraceID:      traceID,
		ExchangeID:   msg.ExchangeID,
		AddedSymbols: msg.Symbols,
		TotalSymbols: len(msg.Symbols),
		Timestamp:    time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish symbols update event")
	}

	return nil
}

func (a *CollectorActor) handleSetInterval(ctx context.Context, traceID string, msg marketdata.SetIntervalCommand) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("exchange %s not found", msg.ExchangeID)
	}

	interval := msg.Interval
	if interval <= 0 {
		interval = a.defaultInterval
	}
	if interval < minimumInterval {
		interval = minimumInterval
	}

	state.mu.Lock()
	state.Interval = interval
	resetCh := state.TickerReset
	state.mu.Unlock()
	if resetCh != nil {
		select {
		case resetCh <- struct{}{}:
		default:
		}
	}

	if err := a.publishEvent(ctx, "collector.interval_updated", "interval_updated", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "interval_updated", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish interval update event")
	}

	return nil
}

func (a *CollectorActor) handleFetchNow(ctx context.Context, traceID string, msg marketdata.FetchNowCommand) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("exchange %s not found", msg.ExchangeID)
	}

	state.mu.RLock()
	enabled := state.Enabled
	stateSymbols := append([]string(nil), state.Symbols...)
	state.mu.RUnlock()
	if !enabled {
		return fmt.Errorf("exchange %s is stopped", msg.ExchangeID)
	}

	symbols := msg.Symbols
	if len(symbols) == 0 {
		symbols = stateSymbols
	}
	if len(symbols) == 0 {
		return nil
	}

	a.collectFromExchange(ctx, msg.ExchangeID, symbols)
	if err := a.publishEvent(ctx, "collector.fetch_now", "fetch_now", marketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "fetch_now", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish fetch-now event")
	}

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
	ticker := time.NewTicker(a.normalizedInterval(state))
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-state.TimerStop:
			return
		case <-state.TickerReset:
			ticker.Stop()
			ticker = time.NewTicker(a.normalizedInterval(state))
		case <-ticker.C:
			state.mu.RLock()
			enabled := state.Enabled
			paused := state.Paused
			exchangeID := state.ExchangeID
			symbols := append([]string(nil), state.Symbols...)
			state.mu.RUnlock()
			if enabled && !paused {
				a.collectFromExchange(ctx, exchangeID, symbols)
			}
		}
	}
}

func (a *CollectorActor) normalizedInterval(state *ExchangeState) time.Duration {
	state.mu.RLock()
	interval := state.Interval
	state.mu.RUnlock()
	if interval <= 0 {
		interval = a.defaultInterval
	}
	if interval < minimumInterval {
		interval = minimumInterval
	}
	return interval
}

func (a *CollectorActor) collectFromExchange(ctx context.Context, exchangeID string, symbols []string) {
	gw, err := a.exchange.GetMarketDataGateway(exchangeID)
	if err != nil {
		a.logger.WithError(fmt.Errorf("get market data gateway for %s: %w", exchangeID, err)).
			Warn("collector gateway lookup failed")
		return
	}
	if gw == nil {
		a.logger.Warnf("collector gateway is nil for %s", exchangeID)
		return
	}
	if a.eventBus == nil {
		return
	}

	for _, symbol := range symbols {
		tick, err := gw.FetchTick(ctx, exchangeID, symbol)
		if err != nil {
			a.logger.WithError(fmt.Errorf("fetch tick for %s:%s: %w", exchangeID, symbol, err)).
				Warn("collector fetch tick failed")
			continue
		}
		if err := a.eventBus.PublishSync(ctx, eventbus.Event{
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
		}); err != nil {
			a.logger.WithError(err).Warnf("failed to publish market tick for %s:%s", exchangeID, symbol)
		}
	}
}

func (a *CollectorActor) publishEvent(ctx context.Context, topic, eventType string, payload interface{}) error {
	if a.eventBus == nil {
		return fmt.Errorf("publish %s: event bus is nil", eventType)
	}

	if err := a.eventBus.PublishSync(ctx, eventbus.Event{
		Topic:     topic,
		Type:      eventType,
		Payload:   payload,
		Source:    CollectorActorID,
		Timestamp: time.Now(),
	}); err != nil {
		return fmt.Errorf("publish %s: %w", eventType, err)
	}

	return nil
}
