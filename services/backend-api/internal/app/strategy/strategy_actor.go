package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/domain/marketdata"
	"github.com/irfndi/neuratrade/internal/domain/signals"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
	scalping "github.com/irfndi/neuratrade/internal/services/scalping"
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
	ComposeSignal(ctx context.Context, ohlcv scalping.OHLCVData, obMetrics scalping.OrderBookMetrics) (*scalping.ScalpingSignal, error)
}

// IngestMarketTickMessage is forwarded from market.tick events to strategy logic.
type IngestMarketTickMessage struct {
	Tick marketdata.MarketTickEvent
}

// MessageType identifies IngestMarketTickMessage for actor routing.
func (*IngestMarketTickMessage) MessageType() string { return "strategy.ingest_market_tick" }

type IngestCandleMessage struct {
	Candle marketdata.MarketCandleEvent
}

func (*IngestCandleMessage) MessageType() string { return "strategy.ingest_candle" }

type IngestOrderBookMetricsMessage struct {
	Metrics marketdata.OrderBookMetricsEvent
}

func (*IngestOrderBookMetricsMessage) MessageType() string {
	return "strategy.ingest_orderbook_metrics"
}

// StrategyActor consumes market ticks and emits SignalProposed events.
type StrategyActor struct {
	id               string
	strategyID       string
	windowSize       int
	engine           *signals.Engine
	eventBus         *eventbus.Bus
	windows          map[string][]signals.Tick
	candleBuffer     map[string][]scalping.OHLCVCandle
	obMetricsCache   map[string]scalping.OrderBookMetrics
	lastScalpingKeys map[string]string
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
		id:               cfg.ActorID,
		strategyID:       cfg.StrategyID,
		windowSize:       cfg.WindowSize,
		engine:           signals.NewEngine(cfg.Signal),
		eventBus:         bus,
		windows:          make(map[string][]signals.Tick),
		candleBuffer:     make(map[string][]scalping.OHLCVCandle),
		obMetricsCache:   make(map[string]scalping.OrderBookMetrics),
		lastScalpingKeys: make(map[string]string),
		logger:           logger,
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
	case *IngestCandleMessage:
		if msg != nil {
			s.handleCandle(msg.Candle)
		}
		return nil
	case *IngestOrderBookMetricsMessage:
		if msg != nil {
			s.handleOBMetrics(msg.Metrics)
		}
		return nil
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

	if s.scalpingComposer != nil {
		s.tryComposeScalpingSignal(ctx, env.TraceID, tick)
	}

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

	return nil
}

func (s *StrategyActor) tryComposeScalpingSignal(ctx context.Context, traceID string, tick marketdata.MarketTickEvent) {
	if s.eventBus == nil {
		return
	}

	key := tick.Exchange + ":" + tick.Symbol
	candles := s.candleBuffer[key]
	if len(candles) == 0 {
		return
	}

	ohlcv := scalping.OHLCVData{
		Exchange: tick.Exchange,
		Symbol:   tick.Symbol,
		Candles:  candles,
	}

	obMetrics := s.obMetricsCache[key]

	result, err := s.scalpingComposer.ComposeSignal(ctx, ohlcv, obMetrics)
	if err != nil {
		s.logger.Warn("scalping signal composition failed",
			"exchange", tick.Exchange,
			"symbol", tick.Symbol,
			"error", err,
		)
		return
	}
	if result == nil {
		return
	}
	fingerprint := scalpingSignalFingerprint(result)
	if fingerprint != "" && s.lastScalpingKeys[key] == fingerprint {
		s.logger.Debug("skipping duplicate scalping signal",
			"signal_id", result.ID,
			"exchange", tick.Exchange,
			"symbol", tick.Symbol)
		return
	}

	compNames := make([]string, len(result.Components))
	for i, component := range result.Components {
		compNames[i] = component.Name
	}

	var qualityScore decimal.Decimal
	if result.Quality != nil {
		qualityScore = result.Quality.OverallScore
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
		Direction:    string(result.Direction),
		Confidence:   result.Confidence,
		Components:   compNames,
		QualityScore: qualityScore,
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
		return
	}
	if fingerprint != "" {
		s.lastScalpingKeys[key] = fingerprint
	}
}

func scalpingSignalFingerprint(signal *scalping.ScalpingSignal) string {
	if signal == nil {
		return ""
	}

	var b strings.Builder
	b.WriteString(signal.Exchange)
	b.WriteByte('|')
	b.WriteString(signal.Symbol)
	b.WriteByte('|')
	b.WriteString(string(signal.Direction))
	b.WriteByte('|')
	b.WriteString(signal.Confidence.String())
	b.WriteByte('|')
	b.WriteString(signal.StopLoss.String())
	b.WriteByte('|')
	b.WriteString(signal.TakeProfit.String())

	if signal.Quality != nil {
		b.WriteString("|q=")
		b.WriteString(signal.Quality.OverallScore.String())
		b.WriteByte('/')
		b.WriteString(signal.Quality.DataFreshness.String())
		b.WriteByte('/')
		b.WriteString(signal.Quality.LiquidityScore.String())
		b.WriteByte('/')
		if signal.Quality.VolatilityOK {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}

	for _, component := range signal.Components {
		b.WriteString("|c=")
		b.WriteString(component.Name)
		b.WriteByte('/')
		b.WriteString(component.Value.String())
		b.WriteByte('/')
		b.WriteString(string(component.Signal))
		b.WriteByte('/')
		b.WriteString(string(component.Strength))
		b.WriteByte('/')
		b.WriteString(component.Weight.String())
	}

	return b.String()
}

func (s *StrategyActor) handleCandle(candle marketdata.MarketCandleEvent) {
	key := candle.Exchange + ":" + candle.Symbol
	sc := scalping.OHLCVCandle{
		Timestamp: candle.Timestamp,
		Open:      candle.Open,
		High:      candle.High,
		Low:       candle.Low,
		Close:     candle.Close,
		Volume:    candle.Volume,
	}
	buf := append(s.candleBuffer[key], sc)
	if len(buf) > 60 {
		buf = buf[len(buf)-60:]
	}
	s.candleBuffer[key] = buf
}

func (s *StrategyActor) handleOBMetrics(metrics marketdata.OrderBookMetricsEvent) {
	key := metrics.Exchange + ":" + metrics.Symbol
	s.obMetricsCache[key] = &orderBookMetricsAdapter{event: metrics}
}

type orderBookMetricsAdapter struct {
	event marketdata.OrderBookMetricsEvent
}

func (a *orderBookMetricsAdapter) GetSpreadPct() decimal.Decimal      { return a.event.BidAskSpread }
func (a *orderBookMetricsAdapter) GetImbalance1Pct() decimal.Decimal  { return a.event.Imbalance1Pct }
func (a *orderBookMetricsAdapter) GetMidPrice() decimal.Decimal       { return a.event.MidPrice }
func (a *orderBookMetricsAdapter) GetBestBid() decimal.Decimal        { return a.event.BestBid }
func (a *orderBookMetricsAdapter) GetBestAsk() decimal.Decimal        { return a.event.BestAsk }
func (a *orderBookMetricsAdapter) GetLiquidityScore() decimal.Decimal { return a.event.LiquidityScore }

// GetBidDepth1Pct and GetAskDepth1Pct return zero until OrderBookMetricsEvent
// is extended with BidDepthUSD/AskDepthUSD fields.
func (a *orderBookMetricsAdapter) GetBidDepth1Pct() decimal.Decimal { return decimal.Zero }
func (a *orderBookMetricsAdapter) GetAskDepth1Pct() decimal.Decimal { return decimal.Zero }

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

func SubscribeCandleEvents(ctx context.Context, bus *eventbus.Bus, ref *actor.Ref) (*eventbus.Subscription, error) {
	if bus == nil {
		return nil, ErrNilEventBus
	}
	if ref == nil {
		return nil, ErrNilActorRef
	}

	handler := func(ctx context.Context, event eventbus.Event) error {
		candle, ok := payloadToMarketCandle(event.Payload)
		if !ok {
			return nil
		}

		env := actor.Envelope{
			Message: &IngestCandleMessage{Candle: candle},
			TraceID: event.TraceID,
		}
		if err := ref.SendEnvelope(ctx, env); err != nil {
			return fmt.Errorf("send candle envelope: %w", err)
		}
		return nil
	}

	sub, err := bus.Subscribe(ctx, ports.EventTypeMarketCandle, handler)
	if err != nil {
		return nil, fmt.Errorf("subscribe to %s: %w", ports.EventTypeMarketCandle, err)
	}
	return sub, nil
}

func SubscribeOrderBookMetricsEvents(ctx context.Context, bus *eventbus.Bus, ref *actor.Ref) (*eventbus.Subscription, error) {
	if bus == nil {
		return nil, ErrNilEventBus
	}
	if ref == nil {
		return nil, ErrNilActorRef
	}

	handler := func(ctx context.Context, event eventbus.Event) error {
		metrics, ok := payloadToOrderBookMetrics(event.Payload)
		if !ok {
			return nil
		}

		env := actor.Envelope{
			Message: &IngestOrderBookMetricsMessage{Metrics: metrics},
			TraceID: event.TraceID,
		}
		if err := ref.SendEnvelope(ctx, env); err != nil {
			return fmt.Errorf("send orderbook metrics envelope: %w", err)
		}
		return nil
	}

	sub, err := bus.Subscribe(ctx, ports.EventTypeOrderBook, handler)
	if err != nil {
		return nil, fmt.Errorf("subscribe to %s: %w", ports.EventTypeOrderBook, err)
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

func payloadToMarketCandle(payload any) (marketdata.MarketCandleEvent, bool) {
	switch candle := payload.(type) {
	case marketdata.MarketCandleEvent:
		return candle, true
	case *marketdata.MarketCandleEvent:
		if candle == nil {
			return marketdata.MarketCandleEvent{}, false
		}
		return *candle, true
	default:
		return marketdata.MarketCandleEvent{}, false
	}
}

func payloadToOrderBookMetrics(payload any) (marketdata.OrderBookMetricsEvent, bool) {
	switch metrics := payload.(type) {
	case marketdata.OrderBookMetricsEvent:
		return metrics, true
	case *marketdata.OrderBookMetricsEvent:
		if metrics == nil {
			return marketdata.OrderBookMetricsEvent{}, false
		}
		return *metrics, true
	default:
		return marketdata.OrderBookMetricsEvent{}, false
	}
}
