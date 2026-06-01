package marketdata

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/database"
	domainmarketdata "github.com/irfndi/neuratrade/internal/domain/marketdata"
	"github.com/irfndi/neuratrade/internal/logging"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

const CollectorActorID = "marketdata-collector"
const minimumInterval = time.Second

type CollectorActor struct {
	logger           *zaplogrus.Logger
	db               database.DBPool
	exchange         ports.ExchangeRegistry
	eventBus         *eventbus.Bus
	exchangeStates   map[string]*ExchangeState
	lastOHLCVCollect map[string]time.Time
	lastOBCollect    map[string]time.Time
	defaultInterval  time.Duration
	ohlcvInterval    time.Duration
	obInterval       time.Duration
	mu               sync.RWMutex
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
		logger:           logger,
		db:               db,
		exchange:         exchange,
		eventBus:         eventBus,
		exchangeStates:   make(map[string]*ExchangeState),
		lastOHLCVCollect: make(map[string]time.Time),
		lastOBCollect:    make(map[string]time.Time),
		defaultInterval:  defaultInterval,
		ohlcvInterval:    30 * time.Second,
		obInterval:       15 * time.Second,
	}
}

func (a *CollectorActor) ID() string { return CollectorActorID }

func (a *CollectorActor) Receive(ctx context.Context, env actor.Envelope) error {
	traceID := env.TraceID
	if traceID == "" {
		traceID = uuid.New().String()
	}
	switch msg := env.Message.(type) {
	case domainmarketdata.StartExchangeCommand:
		return a.handleStartExchange(ctx, traceID, msg)
	case domainmarketdata.StopExchangeCommand:
		return a.handleStopExchange(ctx, traceID, msg)
	case domainmarketdata.PauseExchangeCommand:
		return a.handlePauseExchange(ctx, traceID, msg)
	case domainmarketdata.ResumeExchangeCommand:
		return a.handleResumeExchange(ctx, traceID, msg)
	case domainmarketdata.UpdateSymbolsCommand:
		return a.handleUpdateSymbols(ctx, traceID, msg)
	case domainmarketdata.SetIntervalCommand:
		return a.handleSetInterval(ctx, traceID, msg)
	case domainmarketdata.FetchNowCommand:
		return a.handleFetchNow(ctx, traceID, msg)
	case domainmarketdata.HealthCheckCommand:
		return a.handleHealthCheck(ctx, traceID, msg, env)
	case domainmarketdata.GetStatsCommand:
		return a.handleGetStats(ctx, traceID, msg, env)
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}
}

func (a *CollectorActor) handleStartExchange(ctx context.Context, traceID string, msg domainmarketdata.StartExchangeCommand) error {
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
	if err := a.publishEvent(ctx, "collector.status", "started", domainmarketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "started", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish start event")
	}

	return nil
}

func (a *CollectorActor) handleStopExchange(ctx context.Context, traceID string, msg domainmarketdata.StopExchangeCommand) error {
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

	if err := a.publishEvent(ctx, "collector.status", "stopped", domainmarketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "stopped", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish stop event")
	}

	return nil
}

func (a *CollectorActor) handlePauseExchange(ctx context.Context, traceID string, msg domainmarketdata.PauseExchangeCommand) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("exchange %s not found", msg.ExchangeID)
	}

	state.mu.Lock()
	state.Paused = true
	state.mu.Unlock()

	if err := a.publishEvent(ctx, "collector.status", "paused", domainmarketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "paused", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish pause event")
	}

	return nil
}

func (a *CollectorActor) handleResumeExchange(ctx context.Context, traceID string, msg domainmarketdata.ResumeExchangeCommand) error {
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

	if err := a.publishEvent(ctx, "collector.status", "resumed", domainmarketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "resumed", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish resume event")
	}

	return nil
}

func (a *CollectorActor) handleUpdateSymbols(ctx context.Context, traceID string, msg domainmarketdata.UpdateSymbolsCommand) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		return fmt.Errorf("exchange %s not found", msg.ExchangeID)
	}

	state.mu.Lock()
	state.Symbols = append([]string(nil), msg.Symbols...)
	state.mu.Unlock()

	if err := a.publishEvent(ctx, "collector.symbols_updated", "symbols_updated", domainmarketdata.SymbolsUpdatedEvent{
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

func (a *CollectorActor) handleSetInterval(ctx context.Context, traceID string, msg domainmarketdata.SetIntervalCommand) error {
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

	if err := a.publishEvent(ctx, "collector.interval_updated", "interval_updated", domainmarketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "interval_updated", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish interval update event")
	}

	return nil
}

func (a *CollectorActor) handleFetchNow(ctx context.Context, traceID string, msg domainmarketdata.FetchNowCommand) error {
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
	if err := a.publishEvent(ctx, "collector.fetch_now", "fetch_now", domainmarketdata.CollectorStatusEvent{
		TraceID: traceID, ExchangeID: msg.ExchangeID, Status: "fetch_now", Timestamp: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warn("failed to publish fetch-now event")
	}

	return nil
}

func (a *CollectorActor) handleHealthCheck(ctx context.Context, traceID string, msg domainmarketdata.HealthCheckCommand, env actor.Envelope) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		if env.Reply != nil {
			env.Reply <- domainmarketdata.HealthCheckResponse{ExchangeID: msg.ExchangeID, Healthy: false}
		}
		return nil
	}
	state.mu.RLock()
	healthy := state.Enabled && !state.Paused
	ready := state.Enabled && len(state.Symbols) > 0
	state.mu.RUnlock()
	if env.Reply != nil {
		env.Reply <- domainmarketdata.HealthCheckResponse{
			ExchangeID: msg.ExchangeID,
			Healthy:    healthy,
			Ready:      ready,
		}
	}
	return nil
}

func (a *CollectorActor) handleGetStats(ctx context.Context, traceID string, msg domainmarketdata.GetStatsCommand, env actor.Envelope) error {
	a.mu.RLock()
	state, exists := a.exchangeStates[msg.ExchangeID]
	a.mu.RUnlock()
	if !exists {
		if env.Reply != nil {
			env.Reply <- domainmarketdata.GetStatsResponse{ExchangeID: msg.ExchangeID}
		}
		return nil
	}
	state.mu.RLock()
	symbolsCount := len(state.Symbols)
	state.mu.RUnlock()
	if env.Reply != nil {
		env.Reply <- domainmarketdata.GetStatsResponse{
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
			Payload: domainmarketdata.MarketTickEvent{
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

		key := exchangeID + ":" + symbol
		now := time.Now()
		if a.shouldCollect(&a.lastOHLCVCollect, key, now, a.ohlcvInterval) {
			a.collectOHLCV(ctx, gw, exchangeID, symbol)
		}
		if a.shouldCollect(&a.lastOBCollect, key, now, a.obInterval) {
			a.collectOrderBook(ctx, gw, exchangeID, symbol)
		}
	}
}

func (a *CollectorActor) shouldCollect(lastCollected *map[string]time.Time, key string, now time.Time, interval time.Duration) bool {
	if interval <= 0 {
		return true
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if previous, ok := (*lastCollected)[key]; ok && now.Sub(previous) < interval {
		return false
	}
	(*lastCollected)[key] = now
	return true
}

func (a *CollectorActor) collectOHLCV(ctx context.Context, gw ports.MarketDataGateway, exchangeID, symbol string) {
	candles, err := gw.FetchOHLCV(ctx, exchangeID, symbol, "1m", time.Time{}, 60)
	if err != nil {
		a.logger.WithError(fmt.Errorf("fetch ohlcv for %s:%s: %w", exchangeID, symbol, err)).
			Warn("collector fetch ohlcv failed")
		return
	}
	if len(candles) == 0 {
		return
	}
	latest := candles[len(candles)-1]
	if err := a.publishEvent(ctx, "market.candle", "market.candle", domainmarketdata.MarketCandleEvent{
		TraceID:     uuid.New().String(),
		Exchange:    exchangeID,
		Symbol:      symbol,
		Timeframe:   "1m",
		Open:        latest.Open,
		High:        latest.High,
		Low:         latest.Low,
		Close:       latest.Close,
		Volume:      latest.Volume,
		Timestamp:   latest.Timestamp,
		CollectedAt: time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warnf("failed to publish market candle for %s:%s", exchangeID, symbol)
	}
}

func (a *CollectorActor) collectOrderBook(ctx context.Context, gw ports.MarketDataGateway, exchangeID, symbol string) {
	ob, err := gw.FetchOrderBook(ctx, exchangeID, symbol, 10)
	if err != nil {
		a.logger.WithError(fmt.Errorf("fetch orderbook for %s:%s: %w", exchangeID, symbol, err)).
			Warn("collector fetch orderbook failed")
		return
	}
	if len(ob.Bids) == 0 || len(ob.Asks) == 0 {
		return
	}
	bestBid := ob.Bids[0].Price
	bestAsk := ob.Asks[0].Price
	spread := bestAsk.Sub(bestBid)
	midPrice := bestBid.Add(bestAsk).Div(decimal.NewFromInt(2))

	// 1% USD depth filter: only count levels within 1% of mid price.
	// The client.CalculateOrderBookMetrics helper applies the same filter;
	// without it the imbalance/liquidity scores diverge from what an
	// operator sees in the UI, which made the backtest look much
	// shallower than reality.
	thresholdPct := decimal.NewFromFloat(0.01)
	var thresholdAbs decimal.Decimal
	if !midPrice.IsZero() {
		thresholdAbs = midPrice.Mul(thresholdPct)
	}
	bidDepth := decimal.Zero
	askDepth := decimal.Zero
	if !thresholdAbs.IsZero() {
		for _, lvl := range ob.Bids {
			if lvl.Price.GreaterThanOrEqual(midPrice.Sub(thresholdAbs)) {
				bidDepth = bidDepth.Add(lvl.Amount)
			}
		}
		for _, lvl := range ob.Asks {
			if lvl.Price.LessThanOrEqual(midPrice.Add(thresholdAbs)) {
				askDepth = askDepth.Add(lvl.Amount)
			}
		}
	} else {
		for _, lvl := range ob.Bids {
			bidDepth = bidDepth.Add(lvl.Amount)
		}
		for _, lvl := range ob.Asks {
			askDepth = askDepth.Add(lvl.Amount)
		}
	}
	totalDepth := bidDepth.Add(askDepth)

	var imbalance decimal.Decimal
	if !totalDepth.IsZero() {
		imbalance = bidDepth.Sub(askDepth).Div(totalDepth)
	}

	var spreadPct decimal.Decimal
	if !midPrice.IsZero() {
		spreadPct = spread.Div(midPrice).Mul(decimal.NewFromInt(100))
	}

	var liquidityScore decimal.Decimal
	if !midPrice.IsZero() {
		// USD-weighted depth: totalDepth is in base currency (e.g., BTC),
		// multiply by midPrice to get USD-denominated depth.
		// Formula mirrors client.CalculateOrderBookMetrics.calculateLiquidityScore:
		//   spreadScore(40%) + depthScore(50%) - imbalancePenalty(10%)
		totalDepthUSD := totalDepth.Mul(midPrice)

		// Spread score: 0.01% spread → 100, 1% spread → 0
		spreadScore := decimal.NewFromInt(100).Sub(spreadPct.Mul(decimal.NewFromInt(100)))
		if spreadScore.LessThan(decimal.Zero) {
			spreadScore = decimal.Zero
		}

		// Depth score: $1M depth → 100, $10k depth → 10
		depthScore := totalDepthUSD.Div(decimal.NewFromInt(10000)).Mul(decimal.NewFromInt(10))
		if depthScore.GreaterThan(decimal.NewFromInt(100)) {
			depthScore = decimal.NewFromInt(100)
		}

		imbalancePenalty := imbalance.Abs().Mul(decimal.NewFromInt(20))

		liquidityScore = spreadScore.Mul(decimal.NewFromFloat(0.4)).
			Add(depthScore.Mul(decimal.NewFromFloat(0.5))).
			Sub(imbalancePenalty.Mul(decimal.NewFromFloat(0.1)))
	}
	if liquidityScore.LessThan(decimal.Zero) {
		liquidityScore = decimal.Zero
	}
	if liquidityScore.GreaterThan(decimal.NewFromInt(100)) {
		liquidityScore = decimal.NewFromInt(100)
	}

	if err := a.publishEvent(ctx, "market.orderbook", "market.orderbook", domainmarketdata.OrderBookMetricsEvent{
		TraceID:        uuid.New().String(),
		Exchange:       exchangeID,
		Symbol:         symbol,
		BidAskSpread:   spreadPct,
		MidPrice:       midPrice,
		BestBid:        bestBid,
		BestAsk:        bestAsk,
		Imbalance1Pct:  imbalance,
		LiquidityScore: liquidityScore,
		Timestamp:      ob.Timestamp,
		CollectedAt:    time.Now(),
	}); err != nil {
		a.logger.WithError(err).Warnf("failed to publish orderbook metrics for %s:%s", exchangeID, symbol)
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
