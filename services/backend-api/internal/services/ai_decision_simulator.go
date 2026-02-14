package services

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/pkg/indicators"
	"github.com/shopspring/decimal"
)

type SimulationConfig struct {
	Symbol         string            `json:"symbol"`
	Exchange       string            `json:"exchange"`
	Timeframe      string            `json:"timeframe"`
	StartTime      time.Time         `json:"start_time"`
	EndTime        time.Time         `json:"end_time"`
	Speed          float64           `json:"speed"`
	InitialCapital decimal.Decimal   `json:"initial_capital"`
	UseGoFlux      bool              `json:"use_goflux"`
	OnDecision     DecisionHandler   `json:"-"`
	OnError        ErrorHandler      `json:"-"`
	OnComplete     SimulationHandler `json:"-"`
}

type DecisionHandler func(ctx context.Context, decision *SimulatedDecision) error
type SimulationHandler func(summary *SimulationSummary)

type SimulatedDecision struct {
	ID            string                     `json:"id"`
	Timestamp     time.Time                  `json:"timestamp"`
	CandleIndex   int                        `json:"candle_index"`
	TotalCandles  int                        `json:"total_candles"`
	Price         decimal.Decimal            `json:"price"`
	Action        TradingAction              `json:"action"`
	Side          PositionSide               `json:"side"`
	SizePercent   float64                    `json:"size_percent"`
	Confidence    float64                    `json:"confidence"`
	Reasoning     string                     `json:"reasoning"`
	RiskScore     float64                    `json:"risk_score"`
	Indicators    map[string]decimal.Decimal `json:"indicators,omitempty"`
	ShouldExecute bool                       `json:"should_execute"`
}

type SimulationSummary struct {
	Symbol            string          `json:"symbol"`
	Exchange          string          `json:"exchange"`
	Timeframe         string          `json:"timeframe"`
	StartTime         time.Time       `json:"start_time"`
	EndTime           time.Time       `json:"end_time"`
	TotalCandles      int             `json:"total_candles"`
	TotalDecisions    int             `json:"total_decisions"`
	DecisionsByAction map[string]int  `json:"decisions_by_action"`
	Executions        int             `json:"executions"`
	Skips             int             `json:"skips"`
	InitialCapital    decimal.Decimal `json:"initial_capital"`
	FinalCapital      decimal.Decimal `json:"final_capital"`
	TotalReturn       decimal.Decimal `json:"total_return"`
	ReturnPercent     decimal.Decimal `json:"return_percent"`
	Duration          time.Duration   `json:"duration"`
	Status            ReplayStatus    `json:"status"`
}

type AIDecisionSimulator struct {
	db        *database.PostgresDB
	ccxtSvc   ccxt.CCXTService
	config    SimulationConfig
	replay    *OHLCVReplayEngine
	trader    *TraderAgent
	provider  indicators.IndicatorProvider
	decisions []SimulatedDecision
	summary   *SimulationSummary
	mu        sync.RWMutex
	status    ReplayStatus
	startTime time.Time
}

func NewAIDecisionSimulator(
	db *database.PostgresDB,
	ccxtSvc ccxt.CCXTService,
	config SimulationConfig,
) *AIDecisionSimulator {
	replay := NewOHLCVReplayEngine(db, ccxtSvc)

	var provider indicators.IndicatorProvider
	if config.UseGoFlux {
		provider = indicators.NewGoFluxAdapter()
	} else {
		provider = indicators.NewTalibAdapter()
	}

	trader := NewTraderAgent(DefaultTraderAgentConfig())

	return &AIDecisionSimulator{
		db:       db,
		ccxtSvc:  ccxtSvc,
		config:   config,
		replay:   replay,
		trader:   trader,
		provider: provider,
		status:   ReplayStatusIdle,
	}
}

func (s *AIDecisionSimulator) Configure(config SimulationConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

func (s *AIDecisionSimulator) Load(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	replayConfig := ReplayConfig{
		Symbol:     s.config.Symbol,
		Exchange:   s.config.Exchange,
		Timeframe:  s.config.Timeframe,
		StartTime:  s.config.StartTime,
		EndTime:    s.config.EndTime,
		Speed:      s.config.Speed,
		OnCandle:   s.handleCandle,
		OnError:    s.handleError,
		OnComplete: s.handleComplete,
	}

	s.replay.Configure(replayConfig)

	if err := s.replay.Load(ctx); err != nil {
		s.status = ReplayStatusError
		return fmt.Errorf("failed to load replay: %w", err)
	}

	s.status = ReplayStatusIdle

	s.summary = &SimulationSummary{
		Symbol:            s.config.Symbol,
		Exchange:          s.config.Exchange,
		Timeframe:         s.config.Timeframe,
		StartTime:         s.config.StartTime,
		EndTime:           s.config.EndTime,
		InitialCapital:    s.config.InitialCapital,
		FinalCapital:      s.config.InitialCapital,
		DecisionsByAction: make(map[string]int),
	}

	return nil
}

func (s *AIDecisionSimulator) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.status == ReplayStatusPlaying {
		s.mu.Unlock()
		return fmt.Errorf("simulation already running")
	}
	if s.replay == nil || s.summary == nil {
		s.mu.Unlock()
		return fmt.Errorf("simulation not loaded, call Load() first")
	}
	s.status = ReplayStatusPlaying
	s.startTime = time.Now()
	s.mu.Unlock()

	if err := s.replay.Play(ctx); err != nil {
		s.mu.Lock()
		s.status = ReplayStatusError
		s.mu.Unlock()
		return err
	}

	return nil
}

func (s *AIDecisionSimulator) Pause() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status != ReplayStatusPlaying {
		return fmt.Errorf("cannot pause: current status is %s", s.status)
	}

	if err := s.replay.Pause(); err != nil {
		return err
	}

	s.status = ReplayStatusPaused
	return nil
}

func (s *AIDecisionSimulator) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.status != ReplayStatusPaused {
		return fmt.Errorf("cannot resume: current status is %s", s.status)
	}

	if err := s.replay.Resume(); err != nil {
		return err
	}

	s.status = ReplayStatusPlaying
	return nil
}

func (s *AIDecisionSimulator) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.replay.Stop(); err != nil {
		return err
	}

	s.status = ReplayStatusComplete
	return nil
}

func (s *AIDecisionSimulator) GetStatus() ReplayStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

func (s *AIDecisionSimulator) GetDecisions() []SimulatedDecision {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.decisions
}

func (s *AIDecisionSimulator) GetSummary() *SimulationSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.summary == nil {
		return &SimulationSummary{Status: s.status}
	}

	summary := *s.summary
	summary.TotalCandles = s.replay.GetSummary().TotalCandles
	summary.Duration = time.Since(s.startTime)
	summary.Status = s.status

	return &summary
}

func (s *AIDecisionSimulator) handleCandle(ctx context.Context, candle *ReplayCandle) error {
	marketCtx := s.buildMarketContext(candle)
	portfolioCtx := s.buildPortfolioContext()

	decision, err := s.trader.MakeDecision(ctx, marketCtx, portfolioCtx)
	if err != nil {
		return fmt.Errorf("failed to make decision: %w", err)
	}

	shouldExecute := s.trader.ShouldExecute(decision)

	simDecision := SimulatedDecision{
		ID:            decision.ID,
		Timestamp:     candle.Timestamp,
		CandleIndex:   candle.Index,
		TotalCandles:  candle.Total,
		Price:         candle.Close,
		Action:        decision.Action,
		Side:          decision.Side,
		SizePercent:   decision.SizePercent,
		Confidence:    decision.Confidence,
		Reasoning:     decision.Reasoning,
		RiskScore:     decision.RiskScore,
		ShouldExecute: shouldExecute,
	}

	simDecision.Indicators = s.calculateIndicators(candle)

	s.mu.Lock()
	s.decisions = append(s.decisions, simDecision)
	s.summary.TotalDecisions++
	s.summary.DecisionsByAction[string(decision.Action)]++
	if shouldExecute {
		s.summary.Executions++
		s.applyTrade(&simDecision)
	} else {
		s.summary.Skips++
	}
	s.mu.Unlock()

	if s.config.OnDecision != nil {
		return s.config.OnDecision(ctx, &simDecision)
	}

	return nil
}

func (s *AIDecisionSimulator) handleError(err error) {
	if s.config.OnError != nil {
		s.config.OnError(err)
	}
}

func (s *AIDecisionSimulator) handleComplete(replaySummary *ReplaySummary) {
	s.mu.Lock()
	s.summary.TotalCandles = replaySummary.TotalCandles
	s.summary.Status = ReplayStatusComplete
	s.status = ReplayStatusComplete
	s.mu.Unlock()

	if s.config.OnComplete != nil {
		s.config.OnComplete(s.GetSummary())
	}
}

func (s *AIDecisionSimulator) buildMarketContext(candle *ReplayCandle) MarketContext {
	return MarketContext{
		Symbol:       s.config.Symbol,
		CurrentPrice: candle.Close.InexactFloat64(),
		Volatility:   0.02,
		Trend:        "neutral",
		Liquidity:    1000000,
		FundingRate:  0.0001,
		OpenInterest: 50000000,
		Volume24h:    candle.Volume.InexactFloat64(),
		Signals:      []TradingSignal{},
	}
}

func (s *AIDecisionSimulator) buildPortfolioContext() PortfolioState {
	s.mu.RLock()
	capital := s.summary.FinalCapital
	s.mu.RUnlock()

	return PortfolioState{
		TotalValue:      capital.InexactFloat64(),
		AvailableCash:   capital.InexactFloat64(),
		OpenPositions:   0,
		UnrealizedPnL:   0,
		RealizedPnL:     0,
		MaxDrawdown:     0,
		CurrentDrawdown: 0,
	}
}

func (s *AIDecisionSimulator) calculateIndicators(candle *ReplayCandle) map[string]decimal.Decimal {
	indicators := make(map[string]decimal.Decimal)

	closePrices := []decimal.Decimal{candle.Close}
	rsi := s.provider.RSI(closePrices, 14)
	if len(rsi) > 0 {
		indicators["rsi"] = rsi[len(rsi)-1]
	}

	ema := s.provider.EMA(closePrices, 20)
	if len(ema) > 0 {
		indicators["ema20"] = ema[len(ema)-1]
	}

	return indicators
}

func (s *AIDecisionSimulator) applyTrade(decision *SimulatedDecision) {
	if decision.Action == ActionHold || decision.Action == ActionWait {
		return
	}

	tradeValue := s.summary.FinalCapital.Mul(decimal.NewFromFloat(decision.SizePercent))

	switch decision.Action {
	case ActionOpenLong, ActionAddToPos:
		s.summary.FinalCapital = s.summary.FinalCapital.Add(tradeValue.Mul(decimal.NewFromFloat(0.001)))
	case ActionOpenShort:
		s.summary.FinalCapital = s.summary.FinalCapital.Add(tradeValue.Mul(decimal.NewFromFloat(0.001)))
	case ActionCloseLong, ActionCloseShort, ActionReducePos:
		profitLoss := tradeValue.Mul(decimal.NewFromFloat(0.001))
		s.summary.FinalCapital = s.summary.FinalCapital.Add(profitLoss)
	}

	if !s.summary.InitialCapital.IsZero() {
		returnDiff := s.summary.FinalCapital.Sub(s.summary.InitialCapital)
		s.summary.TotalReturn = returnDiff
		s.summary.ReturnPercent = returnDiff.Div(s.summary.InitialCapital).Mul(decimal.NewFromInt(100))
	}
}
