package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/irfndi/neuratrade/internal/domain/marketdata"
	"github.com/irfndi/neuratrade/internal/domain/signals"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
)

const (
	// DefaultActorID is used when config does not provide an explicit actor id.
	DefaultActorID = "strategy-actor"
)

var (
	ErrNilEventBus = errors.New("strategy actor requires event bus")
	ErrNilActorRef = errors.New("strategy actor subscription requires actor reference")
)

// Config controls the strategy actor runtime behavior.
type Config struct {
	ActorID    string
	StrategyID string
	WindowSize int
	Signal     signals.Config
}

// DefaultConfig returns safe defaults for strategy signal generation.
func DefaultConfig() Config {
	return Config{
		ActorID:    DefaultActorID,
		StrategyID: "default-strategy",
		WindowSize: 16,
		Signal:     signals.DefaultConfig(),
	}
}

// IngestMarketTickMessage is forwarded from market.tick events to strategy logic.
type IngestMarketTickMessage struct {
	Tick marketdata.MarketTickEvent
}

// MessageType identifies IngestMarketTickMessage for actor routing.
func (*IngestMarketTickMessage) MessageType() string { return "strategy.ingest_market_tick" }

// StrategyActor consumes market ticks and emits SignalProposed events.
type StrategyActor struct {
	id         string
	strategyID string
	windowSize int
	engine     *signals.Engine
	eventBus   *eventbus.Bus
	windows    map[string][]signals.Tick
	logger     *slog.Logger
}

// NewStrategyActor creates a strategy actor with deterministic signal logic.
func NewStrategyActor(cfg Config, bus *eventbus.Bus, logger *slog.Logger) *StrategyActor {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.ActorID == "" {
		cfg.ActorID = DefaultActorID
	}
	if cfg.StrategyID == "" {
		cfg.StrategyID = DefaultConfig().StrategyID
	}
	if cfg.WindowSize < cfg.Signal.Lookback {
		cfg.WindowSize = cfg.Signal.Lookback
	}
	if cfg.WindowSize < 2 {
		cfg.WindowSize = DefaultConfig().WindowSize
	}

	return &StrategyActor{
		id:         cfg.ActorID,
		strategyID: cfg.StrategyID,
		windowSize: cfg.WindowSize,
		engine:     signals.NewEngine(cfg.Signal),
		eventBus:   bus,
		windows:    make(map[string][]signals.Tick),
		logger:     logger,
	}
}

// ID returns the actor identifier.
func (s *StrategyActor) ID() string { return s.id }

// Receive handles strategy actor messages.
func (s *StrategyActor) Receive(ctx context.Context, env actor.Envelope) error {
	switch msg := env.Message.(type) {
	case *IngestMarketTickMessage:
		if msg == nil {
			return nil
		}
		return s.handleTick(ctx, env, msg.Tick)
	default:
		return fmt.Errorf("strategy actor: unknown message type %T", msg)
	}
}

func (s *StrategyActor) handleTick(ctx context.Context, env actor.Envelope, tick marketdata.MarketTickEvent) error {
	if tick.Symbol == "" || !tick.Last.IsPositive() {
		return nil
	}

	snapshot := signals.Tick{
		Exchange:  tick.Exchange,
		Symbol:    tick.Symbol,
		Bid:       tick.Bid,
		Ask:       tick.Ask,
		Last:      tick.Last,
		Volume:    tick.Volume,
		Timestamp: tick.Timestamp,
	}

	window := append(s.windows[tick.Symbol], snapshot)
	if len(window) > s.windowSize {
		window = window[len(window)-s.windowSize:]
	}
	s.windows[tick.Symbol] = window

	signal, ok := s.engine.Evaluate(s.strategyID, window)
	if !ok {
		return nil
	}
	if s.eventBus == nil {
		return ErrNilEventBus
	}

	payload := signals.NewSignalProposedEvent(signal)
	event := eventbus.NewEvent(ports.EventTypeSignalProposed, ports.EventTypeSignalProposed, payload).
		WithSource(s.id)
	if env.TraceID != "" {
		event = event.WithTraceID(env.TraceID)
	}

	s.logger.Debug("proposed signal",
		"strategy_id", payload.StrategyID,
		"symbol", payload.Symbol,
		"side", payload.Side,
		"confidence", payload.Confidence.String())

	return s.eventBus.PublishSync(ctx, event)
}

// SubscribeMarketTicks forwards market.tick events from event bus into the strategy actor mailbox.
func SubscribeMarketTicks(ctx context.Context, bus *eventbus.Bus, ref *actor.Ref) (*eventbus.Subscription, error) {
	if bus == nil {
		return nil, ErrNilEventBus
	}
	if ref == nil {
		return nil, ErrNilActorRef
	}

	handler := func(ctx context.Context, event eventbus.Event) error {
		tick, ok := payloadToMarketTick(event.Payload)
		if !ok {
			return nil
		}

		return ref.SendEnvelope(ctx, actor.Envelope{
			Message: &IngestMarketTickMessage{Tick: tick},
			TraceID: event.TraceID,
		})
	}

	return bus.Subscribe(ctx, ports.EventTypeMarketTick, handler)
}

func payloadToMarketTick(payload any) (marketdata.MarketTickEvent, bool) {
	switch tick := payload.(type) {
	case marketdata.MarketTickEvent:
		return tick, true
	case *marketdata.MarketTickEvent:
		if tick == nil {
			return marketdata.MarketTickEvent{}, false
		}
		return *tick, true
	default:
		return marketdata.MarketTickEvent{}, false
	}
}
