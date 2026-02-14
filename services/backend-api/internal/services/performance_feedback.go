package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/observability"
	"golang.org/x/sync/errgroup"
)

type PerformanceFeedbackConfig struct {
	EnableDecisionJournal  bool `mapstructure:"enable_decision_journal"`
	EnableOutcomeTracking  bool `mapstructure:"enable_outcome_tracking"`
	EnablePatternDetection bool `mapstructure:"enable_pattern_detection"`
	EnableParameterTuning  bool `mapstructure:"enable_parameter_tuning"`
	MinSampleSizeForTuning int  `mapstructure:"min_sample_size_for_tuning"`
}

type PerformanceFeedbackService struct {
	db     *database.PostgresDB
	config PerformanceFeedbackConfig
}

func NewPerformanceFeedbackService(db *database.PostgresDB, config PerformanceFeedbackConfig) *PerformanceFeedbackService {
	return &PerformanceFeedbackService{
		db:     db,
		config: config,
	}
}

func (s *PerformanceFeedbackService) RecordDecision(ctx context.Context, decision *DecisionRecord) error {
	if !s.config.EnableDecisionJournal {
		return nil
	}
	spanCtx, span := observability.StartSpan(ctx, observability.SpanOpDBQuery, "PerformanceFeedback.RecordDecision")
	defer observability.FinishSpan(span, nil)

	record := models.DecisionJournal{
		ID:               generateID(),
		SessionID:        decision.SessionID,
		Symbol:           decision.Symbol,
		Exchange:         decision.Exchange,
		SkillID:          decision.SkillID,
		DecisionType:     decision.DecisionType,
		Action:           decision.Action,
		Side:             string(decision.Side),
		SizePercent:      decision.SizePercent,
		EntryPrice:       decision.EntryPrice,
		StopLoss:         decision.StopLoss,
		TakeProfit:       decision.TakeProfit,
		Confidence:       decision.Confidence,
		Reasoning:        decision.Reasoning,
		RegimeTrend:      decision.RegimeTrend,
		RegimeVolatility: decision.RegimeVolatility,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	if decision.MarketConditions != nil {
		record.MarketConditions, _ = json.Marshal(decision.MarketConditions)
	}
	if decision.SignalsUsed != nil {
		record.SignalsUsed, _ = json.Marshal(decision.SignalsUsed)
	}
	if decision.RiskAssessment != nil {
		record.RiskAssessment, _ = json.Marshal(decision.RiskAssessment)
	}

	query := `
		INSERT INTO decision_journal (
			id, session_id, symbol, exchange, skill_id, decision_type,
			action, side, size_percent, entry_price, stop_loss, take_profit,
			confidence, reasoning, regime_trend, regime_volatility,
			market_conditions, signals_used, risk_assessment, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
		)`
	_, err := s.db.Pool.Exec(spanCtx, query,
		record.ID, record.SessionID, record.Symbol, record.Exchange, record.SkillID,
		record.DecisionType, record.Action, record.Side, record.SizePercent,
		record.EntryPrice, record.StopLoss, record.TakeProfit, record.Confidence,
		record.Reasoning, record.RegimeTrend, record.RegimeVolatility,
		record.MarketConditions, record.SignalsUsed, record.RiskAssessment,
		record.CreatedAt, record.UpdatedAt,
	)
	return err
}

func (s *PerformanceFeedbackService) RecordOutcome(ctx context.Context, outcome *OutcomeRecord) error {
	if !s.config.EnableOutcomeTracking {
		return nil
	}
	spanCtx, span := observability.StartSpan(ctx, observability.SpanOpDBQuery, "PerformanceFeedback.RecordOutcome")
	defer observability.FinishSpan(span, nil)

	record := models.TradeOutcome{
		ID:                  generateID(),
		DecisionJournalID:   outcome.DecisionJournalID,
		Symbol:              outcome.Symbol,
		Exchange:            outcome.Exchange,
		SkillID:             outcome.SkillID,
		Side:                outcome.Side,
		EntryPrice:          outcome.EntryPrice,
		ExitPrice:           outcome.ExitPrice,
		Size:                outcome.Size,
		PnL:                 outcome.PnL,
		PnLPercent:          outcome.PnLPercent,
		Fees:                outcome.Fees,
		HoldDurationSeconds: outcome.HoldDurationSeconds,
		Outcome:             outcome.Outcome,
		ExitReason:          outcome.ExitReason,
		RegimeAtEntry:       outcome.RegimeAtEntry,
		RegimeAtExit:        outcome.RegimeAtExit,
		VolatilityAtEntry:   outcome.VolatilityAtEntry,
		VolatilityAtExit:    outcome.VolatilityAtExit,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}

	query := `
		INSERT INTO trade_outcomes (
			id, decision_journal_id, symbol, exchange, skill_id, side,
			entry_price, exit_price, size, pnl, pnl_percent, fees,
			hold_duration_seconds, outcome, exit_reason, regime_at_entry,
			regime_at_exit, volatility_at_entry, volatility_at_exit,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21
		)`
	_, err := s.db.Pool.Exec(spanCtx, query,
		record.ID, record.DecisionJournalID, record.Symbol, record.Exchange,
		record.SkillID, record.Side, record.EntryPrice, record.ExitPrice,
		record.Size, record.PnL, record.PnLPercent, record.Fees,
		record.HoldDurationSeconds, record.Outcome, record.ExitReason,
		record.RegimeAtEntry, record.RegimeAtExit, record.VolatilityAtEntry,
		record.VolatilityAtExit, record.CreatedAt, record.UpdatedAt,
	)
	if err != nil {
		return err
	}

	if s.config.EnablePatternDetection {
		if err := s.detectFailurePattern(spanCtx, &record); err != nil {
			observability.CaptureException(spanCtx, err)
		}
	}

	return nil
}

func (s *PerformanceFeedbackService) GetStrategyParameters(ctx context.Context, skillID, symbol, regime string) ([]models.StrategyParameter, error) {
	query := `
		SELECT id, skill_id, symbol, parameter_name, parameter_value,
		       min_value, max_value, tuning_reason, based_on_outcome_id,
		       regime_context, confidence, sample_size, is_active,
		       created_at, updated_at
		FROM strategy_parameters
		WHERE skill_id = $1 AND is_active = true
		  AND (symbol IS NULL OR symbol = $2)
		  AND (regime_context IS NULL OR regime_context = $3)
		ORDER BY confidence DESC, sample_size DESC
		LIMIT 20`
	rows, err := s.db.Pool.Query(ctx, query, skillID, symbol, regime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var params []models.StrategyParameter
	for rows.Next() {
		var p models.StrategyParameter
		if err := rows.Scan(&p.ID, &p.SkillID, &p.Symbol, &p.ParameterName,
			&p.ParameterValue, &p.MinValue, &p.MaxValue, &p.TuningReason,
			&p.BasedOnOutcomeID, &p.RegimeContext, &p.Confidence, &p.SampleSize,
			&p.IsActive, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		params = append(params, p)
	}
	return params, rows.Err()
}

func (s *PerformanceFeedbackService) detectFailurePattern(ctx context.Context, outcome *models.TradeOutcome) error {
	if outcome.Outcome != "loss" {
		return nil
	}

	patternType := "wrong_direction"
	var description string

	if outcome.ExitReason == "stop_loss" {
		patternType = "stop_loss_hit"
		description = fmt.Sprintf("Stop loss hit on %s %s", outcome.Symbol, outcome.Side)
	} else if outcome.RegimeAtEntry != outcome.RegimeAtExit {
		patternType = "regime_mismatch"
		description = fmt.Sprintf("Regime changed during trade: %s -> %s", outcome.RegimeAtEntry, outcome.RegimeAtExit)
	}

	query := `
		INSERT INTO failure_patterns (id, skill_id, pattern_type, description, occurrence_count, first_observed, last_observed, total_pnl_impact, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 1, $5, $5, $6, true, $5, $5)
		ON CONFLICT (skill_id, pattern_type) DO UPDATE SET
			occurrence_count = failure_patterns.occurrence_count + 1,
			last_observed = $5,
			total_pnl_impact = failure_patterns.total_pnl_impact + $6`

	_, err := s.db.Pool.Exec(ctx, query,
		generateID(),
		outcome.SkillID,
		patternType,
		description,
		time.Now(),
		outcome.PnL,
	)
	return err
}

func (s *PerformanceFeedbackService) GetFailurePatterns(ctx context.Context, skillID string) ([]models.FailurePattern, error) {
	query := `
		SELECT id, skill_id, pattern_type, description, occurrence_count,
		       first_observed, last_observed, total_pnl_impact, affected_symbols,
		       suggested_fix, enabled, created_at, updated_at
		FROM failure_patterns
		WHERE skill_id = $1 AND enabled = true
		ORDER BY occurrence_count DESC`

	rows, err := s.db.Pool.Query(ctx, query, skillID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var patterns []models.FailurePattern
	for rows.Next() {
		var p models.FailurePattern
		if err := rows.Scan(&p.ID, &p.SkillID, &p.PatternType, &p.Description,
			&p.OccurrenceCount, &p.FirstObserved, &p.LastObserved,
			&p.TotalPnLImpact, &p.AffectedSymbols, &p.SuggestedFix,
			&p.Enabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		patterns = append(patterns, p)
	}
	return patterns, rows.Err()
}

type DecisionRecord struct {
	SessionID        string
	Symbol           string
	Exchange         string
	SkillID          string
	DecisionType     string
	Action           string
	Side             PositionSide
	SizePercent      float64
	EntryPrice       float64
	StopLoss         float64
	TakeProfit       float64
	Confidence       float64
	Reasoning        string
	RegimeTrend      string
	RegimeVolatility string
	MarketConditions map[string]interface{}
	SignalsUsed      []interface{}
	RiskAssessment   map[string]interface{}
}

type OutcomeRecord struct {
	DecisionJournalID   string
	Symbol              string
	Exchange            string
	SkillID             string
	Side                string
	EntryPrice          float64
	ExitPrice           float64
	Size                float64
	PnL                 float64
	PnLPercent          float64
	Fees                float64
	HoldDurationSeconds int
	Outcome             string
	ExitReason          string
	RegimeAtEntry       string
	RegimeAtExit        string
	VolatilityAtEntry   float64
	VolatilityAtExit    float64
}

func generateID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("failed to generate ID: %v", err))
	}
	return hex.EncodeToString(b)
}

type PerformanceFeedbackRunner struct {
	feedback *PerformanceFeedbackService
}

func NewPerformanceFeedbackRunner(db *database.PostgresDB, config PerformanceFeedbackConfig) *PerformanceFeedbackRunner {
	return &PerformanceFeedbackRunner{
		feedback: NewPerformanceFeedbackService(db, config),
	}
}

func (r *PerformanceFeedbackRunner) RecordDecision(ctx context.Context, decision *DecisionRecord) error {
	return r.feedback.RecordDecision(ctx, decision)
}

func (r *PerformanceFeedbackRunner) RecordOutcome(ctx context.Context, outcome *OutcomeRecord) error {
	return r.feedback.RecordOutcome(ctx, outcome)
}

func (r *PerformanceFeedbackRunner) GetParameters(ctx context.Context, skillID, symbol, regime string) ([]models.StrategyParameter, error) {
	return r.feedback.GetStrategyParameters(ctx, skillID, symbol, regime)
}

func (r *PerformanceFeedbackRunner) GetFailurePatterns(ctx context.Context, skillID string) ([]models.FailurePattern, error) {
	return r.feedback.GetFailurePatterns(ctx, skillID)
}

func (r *PerformanceFeedbackRunner) RunParameterTuning(ctx context.Context, skillID, symbol string) error {
	var g errgroup.Group
	g.Go(func() error {
		return r.tuneStopLoss(ctx, skillID, symbol)
	})
	g.Go(func() error {
		return r.tuneTakeProfit(ctx, skillID, symbol)
	})
	return g.Wait()
}

func (r *PerformanceFeedbackRunner) tuneStopLoss(ctx context.Context, skillID, symbol string) error {
	query := `
		SELECT AVG(pnl_percent) as avg_pnl, COUNT(*) as count
		FROM trade_outcomes
		WHERE skill_id = $1 AND symbol = $2 AND outcome = 'loss' AND exit_reason = 'stop_loss'`

	var avgPnL float64
	var count int
	err := r.feedback.db.Pool.QueryRow(ctx, query, skillID, symbol).Scan(&avgPnL, &count)
	if err != nil || count < 5 {
		return err
	}

	if avgPnL < -2.0 {
		observability.CaptureException(ctx, fmt.Errorf("high stop loss hit rate: %.2f%%", avgPnL))
	}
	return nil
}

func (r *PerformanceFeedbackRunner) tuneTakeProfit(ctx context.Context, skillID, symbol string) error {
	query := `
		SELECT AVG(pnl_percent) as avg_pnl, COUNT(*) as count
		FROM trade_outcomes
		WHERE skill_id = $1 AND symbol = $2 AND outcome = 'win' AND exit_reason = 'take_profit'`

	var avgPnL float64
	var count int
	err := r.feedback.db.Pool.QueryRow(ctx, query, skillID, symbol).Scan(&avgPnL, &count)
	if err != nil || count < 5 {
		return err
	}

	if avgPnL > 1.0 {
		observability.CaptureException(ctx, fmt.Errorf("high take profit margin: %.2f%%", avgPnL))
	}
	return nil
}
