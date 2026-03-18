package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/irfndi/neuratrade/internal/domain/marketdata"
	"github.com/irfndi/neuratrade/internal/domain/signals"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
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

type ScalpingSignalComposer interface {
	ComposeSignal(ctx context.Context, ohlcv ScalpingOHLCVData, obMetrics ScalpingOrderBookMetrics) (*ScalpingSignalResult, error)
}

type ScalpingOHLCVData struct {
	Exchange  string
	Symbol    string
	Timeframe string
	Candles   []ScalpingCandle
}

type ScalpingCandle struct {
	Timestamp time.Time
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
}

type ScalpingOrderBookMetrics struct {
	SpreadPct      decimal.Decimal
	Imbalance1Pct  decimal.Decimal
	MidPrice       decimal.Decimal
	BestBid        decimal.Decimal
	BestAsk        decimal.Decimal
	LiquidityScore decimal.Decimal
}

type ScalpingSignalResult struct {
	ID         string
	Direction  string
	Confidence decimal.Decimal
	Components []string
	Quality    decimal.Decimal
}

// IngestMarketTickMessage is forwarded from market.tick events to strategy logic.
type IngestMarketTickMessage struct {
	Tick marketdata.MarketTickEvent
}

// MessageType identifies IngestMarketTickMessage for actor routing.
func (*IngestMarketTickMessage) MessageType() string { return "strategy.ingest_market_tick" }

// StrategyActor consumes market ticks and emits SignalProposed events.
type StrategyActor struct {
	id               string
	strategyID       string
	windowSize       int
	engine           *signals.Engine
	eventBus         *eventbus.Bus
	windows          map[string][]signals.Tick
	logger           *slog.Logger
	scalpingComposer ScalpingSignalComposer
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

func (s *StrategyActor) SetScalpingComposer(composer ScalpingSignalComposer) {
	s.scalpingComposer = composer
}

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
		window = append(make([]signals.Tick, 0, s.windowSize), window[len(window)-s.windowSize:]...)
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

	if err := s.eventBus.PublishSync(ctx, event); err != nil {
		return fmt.Errorf("publish SignalProposed event: %w", err)
	}

	if s.scalpingComposer != nil {
		s.tryComposeScalpingSignal(ctx, env.TraceID, tick)
	}

	return nil
}

// SubscribeMarketTicks forwards market.tick events from event bus into the strategy actor mailbox.
func (s *StrategyActor) tryComposeScalpingSignal(ctx context.Context, traceID string, tick marketdata.MarketTickEvent) {
	ohlcv := ScalpingOHLCVData{
		Exchange: tick.Exchange,
		Symbol:   tick.Symbol,
		Candles: []ScalpingCandle{
			{
				Timestamp: tick.Timestamp,
				Open:      tick.Last,
				High:      tick.Last,
				Low:       tick.Last,
				Close:     tick.Last,
				Volume:    tick.Volume,
			},
		},
	}

	spread := tick.Ask.Sub(tick.Bid)
	midPrice := tick.Bid.Add(tick.Ask).Div(decimal.NewFromInt(2))
	var spreadPct decimal.Decimal
	if !midPrice.IsZero() {
		spreadPct = spread.Div(midPrice).Mul(decimal.NewFromInt(100))
	}

	obMetrics := ScalpingOrderBookMetrics{
		SpreadPct: spreadPct,
		MidPrice:  midPrice,
		BestBid:   tick.Bid,
		BestAsk:   tick.Ask,
	}

	result, err := s.scalpingComposer.ComposeSignal(ctx, ohlcv, obMetrics)
	if err != nil || result == nil {
		return
	}

	payload := signals.ScalpingSignalProposedEvent{
		BaseEvent: ports.BaseEvent{
			Type:       ports.EventTypeScalpingSignalProposed,
			Aggregate:  s.strategyID,
			OccurredAt: time.Now().Unix(),
		},
		SignalID:     result.ID,
		Exchange:     tick.Exchange,
		Symbol:       tick.Symbol,
		Direction:    result.Direction,
		Confidence:   result.Confidence,
		Components:   result.Components,
		QualityScore: result.Quality,
	}

	event := eventbus.NewEvent(ports.EventTypeScalpingSignalProposed, ports.EventTypeScalpingSignalProposed, payload).
		WithSource(s.id)
	if traceID != "" {
		event = event.WithTraceID(traceID)
	}

	s.logger.Debug("proposed scalping signal",
		"signal_id", payload.SignalID,
		"symbol", payload.Symbol,
		"direction", payload.Direction,
		"confidence", payload.Confidence.String())

	if err := s.eventBus.PublishSync(ctx, event); err != nil {
		s.logger.Warn("failed to publish scalping signal", "error", err)
	}
}

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

		env := actor.Envelope{
			Message: &IngestMarketTickMessage{Tick: tick},
			TraceID: event.TraceID,
		}
		if err := ref.SendEnvelope(ctx, env); err != nil {
			return fmt.Errorf("send envelope for market tick trace=%s: %w", event.TraceID, err)
		}
		return nil
	}

	sub, err := bus.Subscribe(ctx, ports.EventTypeMarketTick, handler)
	if err != nil {
		return nil, fmt.Errorf("subscribe to %s: %w", ports.EventTypeMarketTick, err)
	}
	return sub, nil
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
