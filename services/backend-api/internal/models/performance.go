package models

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
)

type DecisionJournal struct {
	ID               string          `json:"id" db:"id"`
	SessionID        string          `json:"session_id" db:"session_id"`
	Symbol           string          `json:"symbol" db:"symbol"`
	Exchange         string          `json:"exchange" db:"exchange"`
	SkillID          string          `json:"skill_id" db:"skill_id"`
	DecisionType     string          `json:"decision_type" db:"decision_type"`
	Action           string          `json:"action" db:"action"`
	Side             string          `json:"side" db:"side"`
	SizePercent      decimal.Decimal `json:"size_percent" db:"size_percent"`
	EntryPrice       decimal.Decimal `json:"entry_price" db:"entry_price"`
	StopLoss         decimal.Decimal `json:"stop_loss" db:"stop_loss"`
	TakeProfit       decimal.Decimal `json:"take_profit" db:"take_profit"`
	Confidence       decimal.Decimal `json:"confidence" db:"confidence"`
	Reasoning        string          `json:"reasoning" db:"reasoning"`
	RegimeTrend      string          `json:"regime_trend" db:"regime_trend"`
	RegimeVolatility string          `json:"regime_volatility" db:"regime_volatility"`
	MarketConditions json.RawMessage `json:"market_conditions" db:"market_conditions"`
	SignalsUsed      json.RawMessage `json:"signals_used" db:"signals_used"`
	RiskAssessment   json.RawMessage `json:"risk_assessment" db:"risk_assessment"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
}

type TradeOutcome struct {
	ID                  string          `json:"id" db:"id"`
	DecisionJournalID   string          `json:"decision_journal_id" db:"decision_journal_id"`
	Symbol              string          `json:"symbol" db:"symbol"`
	Exchange            string          `json:"exchange" db:"exchange"`
	SkillID             string          `json:"skill_id" db:"skill_id"`
	Side                string          `json:"side" db:"side"`
	EntryPrice          decimal.Decimal `json:"entry_price" db:"entry_price"`
	ExitPrice           decimal.Decimal `json:"exit_price" db:"exit_price"`
	Size                decimal.Decimal `json:"size" db:"size"`
	PnL                 decimal.Decimal `json:"pnl" db:"pnl"`
	PnLPercent          decimal.Decimal `json:"pnl_percent" db:"pnl_percent"`
	Fees                decimal.Decimal `json:"fees" db:"fees"`
	HoldDurationSeconds int             `json:"hold_duration_seconds" db:"hold_duration_seconds"`
	Outcome             string          `json:"outcome" db:"outcome"`
	ExitReason          string          `json:"exit_reason" db:"exit_reason"`
	RegimeAtEntry       string          `json:"regime_at_entry" db:"regime_at_entry"`
	RegimeAtExit        string          `json:"regime_at_exit" db:"regime_at_exit"`
	VolatilityAtEntry   decimal.Decimal `json:"volatility_at_entry" db:"volatility_at_entry"`
	VolatilityAtExit    decimal.Decimal `json:"volatility_at_exit" db:"volatility_at_exit"`
	CreatedAt           time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at" db:"updated_at"`
}

type FailurePattern struct {
	ID              string          `json:"id" db:"id"`
	SkillID         string          `json:"skill_id" db:"skill_id"`
	PatternType     string          `json:"pattern_type" db:"pattern_type"`
	Description     string          `json:"description" db:"description"`
	OccurrenceCount int             `json:"occurrence_count" db:"occurrence_count"`
	FirstObserved   time.Time       `json:"first_observed" db:"first_observed"`
	LastObserved    time.Time       `json:"last_observed" db:"last_observed"`
	TotalPnLImpact  decimal.Decimal `json:"total_pnl_impact" db:"total_pnl_impact"`
	AffectedSymbols json.RawMessage `json:"affected_symbols" db:"affected_symbols"`
	SuggestedFix    string          `json:"suggested_fix" db:"suggested_fix"`
	Enabled         bool            `json:"enabled" db:"enabled"`
	CreatedAt       time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at" db:"updated_at"`
}

type StrategyParameter struct {
	ID               string          `json:"id" db:"id"`
	SkillID          string          `json:"skill_id" db:"skill_id"`
	Symbol           string          `json:"symbol" db:"symbol"`
	ParameterName    string          `json:"parameter_name" db:"parameter_name"`
	ParameterValue   decimal.Decimal `json:"parameter_value" db:"parameter_value"`
	MinValue         decimal.Decimal `json:"min_value" db:"min_value"`
	MaxValue         decimal.Decimal `json:"max_value" db:"max_value"`
	TuningReason     string          `json:"tuning_reason" db:"tuning_reason"`
	BasedOnOutcomeID string          `json:"based_on_outcome_id" db:"based_on_outcome_id"`
	RegimeContext    string          `json:"regime_context" db:"regime_context"`
	Confidence       decimal.Decimal `json:"confidence" db:"confidence"`
	SampleSize       int             `json:"sample_size" db:"sample_size"`
	IsActive         bool            `json:"is_active" db:"is_active"`
	CreatedAt        time.Time       `json:"created_at" db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at" db:"updated_at"`
}
