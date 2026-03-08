package autonomous

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// StrategyMode represents the operational mode of a strategy.
type StrategyMode string

const (
	// ModeShadow runs strategy without placing orders, compares predictions to actual.
	ModeShadow StrategyMode = "shadow"
	// ModePaper places simulated orders, verifies behavior.
	ModePaper StrategyMode = "paper"
	// ModeLive places real orders with risk limits.
	ModeLive StrategyMode = "live"
)

// RolloutStage represents the deployment stage of a strategy.
type RolloutStage string

const (
	// StageShadow is the initial stage - strategy runs without orders.
	StageShadow RolloutStage = "shadow"
	// StagePaper is the validation stage - simulated orders.
	StagePaper RolloutStage = "paper"
	// StageLive is the production stage - real orders.
	StageLive RolloutStage = "live"
)

// StrategyStatus represents the current status of a strategy.
type StrategyStatus string

const (
	// StatusActive means the strategy is actively running.
	StatusActive StrategyStatus = "active"
	// StatusPaused means the strategy is temporarily paused.
	StatusPaused StrategyStatus = "paused"
	// StatusRolledBack means the strategy was rolled back due to issues.
	StatusRolledBack StrategyStatus = "rolled_back"
	// StatusPromoting means the strategy is being promoted to next stage.
	StatusPromoting StrategyStatus = "promoting"
)

// StrategyProposal represents a proposed trading strategy from the AI agent.
type StrategyProposal struct {
	// ID is the unique identifier for this proposal.
	ID string `json:"id"`
	// StrategyID is the identifier of the strategy template.
	StrategyID string `json:"strategy_id"`
	// Symbol is the trading pair (e.g., "BTC/USDT").
	Symbol string `json:"symbol"`
	// Exchange is the target exchange.
	Exchange string `json:"exchange"`
	// Side is the trading side (buy/sell).
	Side string `json:"side"`
	// Confidence is the AI confidence score (0.0-1.0).
	Confidence float64 `json:"confidence"`
	// Reasoning contains the AI's explanation for this proposal.
	Reasoning string `json:"reasoning"`
	// Parameters contains strategy-specific parameters.
	Parameters map[string]any `json:"parameters"`
	// RiskScore is the computed risk score (0.0-1.0).
	RiskScore float64 `json:"risk_score"`
	// ExpectedReturn is the expected return percentage.
	ExpectedReturn decimal.Decimal `json:"expected_return"`
	// MaxDrawdown is the maximum acceptable drawdown.
	MaxDrawdown decimal.Decimal `json:"max_drawdown"`
	// CreatedAt is when the proposal was created.
	CreatedAt time.Time `json:"created_at"`
	// ExpiresAt is when the proposal expires.
	ExpiresAt time.Time `json:"expires_at"`
}

// RolloutState represents the current state of a strategy rollout.
type RolloutState struct {
	// StrategyID is the unique strategy identifier.
	StrategyID string `json:"strategy_id"`
	// CurrentStage is the current rollout stage.
	CurrentStage RolloutStage `json:"current_stage"`
	// Status is the current status.
	Status StrategyStatus `json:"status"`
	// EnteredAt is when the strategy entered the current stage.
	EnteredAt time.Time `json:"entered_at"`
	// PromotionCriteria tracks criteria for promotion to next stage.
	PromotionCriteria PromotionCriteria `json:"promotion_criteria"`
	// Metrics tracks performance metrics for this rollout.
	Metrics RolloutMetrics `json:"metrics"`
	// History tracks stage transitions.
	History []StageTransition `json:"history"`
}

// PromotionCriteria tracks criteria for promoting a strategy.
type PromotionCriteria struct {
	// MinTrades is the minimum number of trades required.
	MinTrades int `json:"min_trades"`
	// MinWinRate is the minimum win rate required (0.0-1.0).
	MinWinRate float64 `json:"min_win_rate"`
	// MaxSlippage is the maximum acceptable slippage.
	MaxSlippage decimal.Decimal `json:"max_slippage"`
	// MaxRejectionRate is the maximum order rejection rate (0.0-1.0).
	MaxRejectionRate float64 `json:"max_rejection_rate"`
	// MinUptime is the minimum uptime percentage (0.0-100.0).
	MinUptime float64 `json:"min_uptime"`
	// DurationRequired is the minimum time in current stage.
	DurationRequired time.Duration `json:"duration_required"`
}

// RolloutMetrics tracks performance metrics for a rollout.
type RolloutMetrics struct {
	// TotalTrades is the total number of trades executed.
	TotalTrades int `json:"total_trades"`
	// WinningTrades is the number of winning trades.
	WinningTrades int `json:"winning_trades"`
	// LosingTrades is the number of losing trades.
	LosingTrades int `json:"losing_trades"`
	// TotalPnL is the total profit/loss.
	TotalPnL decimal.Decimal `json:"total_pnl"`
	// WinRate is the win rate (0.0-1.0).
	WinRate float64 `json:"win_rate"`
	// AvgSlippage is the average slippage percentage.
	AvgSlippage decimal.Decimal `json:"avg_slippage"`
	// RejectionCount is the number of rejected orders.
	RejectionCount int `json:"rejection_count"`
	// UptimePercent is the uptime percentage.
	UptimePercent float64 `json:"uptime_percent"`
	// MaxDrawdown is the maximum drawdown observed.
	MaxDrawdown decimal.Decimal `json:"max_drawdown"`
	// SharpeRatio is the Sharpe ratio.
	SharpeRatio float64 `json:"sharpe_ratio"`
	// LastUpdated is when metrics were last updated.
	LastUpdated time.Time `json:"last_updated"`
}

// StageTransition records a stage transition event.
type StageTransition struct {
	// FromStage is the previous stage.
	FromStage RolloutStage `json:"from_stage"`
	// ToStage is the new stage.
	ToStage RolloutStage `json:"to_stage"`
	// Reason is why the transition occurred.
	Reason string `json:"reason"`
	// Timestamp is when the transition occurred.
	Timestamp time.Time `json:"timestamp"`
	// MetricsAtTransition are the metrics at time of transition.
	MetricsAtTransition RolloutMetrics `json:"metrics_at_transition"`
}

// RollbackTrigger represents a condition that triggers automatic rollback.
type RollbackTrigger string

const (
	// TriggerPnLBreach triggers rollback when PnL falls below threshold.
	TriggerPnLBreach RollbackTrigger = "pnl_breach"
	// TriggerSlippageSpike triggers rollback on slippage spike.
	TriggerSlippageSpike RollbackTrigger = "slippage_spike"
	// TriggerRejectionRate triggers rollback on high rejection rate.
	TriggerRejectionRate RollbackTrigger = "rejection_rate"
	// TriggerMaxDrawdown triggers rollback on max drawdown breach.
	TriggerMaxDrawdown RollbackTrigger = "max_drawdown"
	// TriggerNetLoss triggers rollback when net losses exceed the configured limit.
	TriggerNetLoss RollbackTrigger = "net_loss"
	// TriggerConsecutiveLoss is kept as a backward-compatible alias.
	TriggerConsecutiveLoss RollbackTrigger = TriggerNetLoss
	// TriggerKillSwitch triggers rollback when kill switch is engaged.
	TriggerKillSwitch RollbackTrigger = "kill_switch"
	// TriggerSafeMode triggers rollback when safe mode is enabled.
	TriggerSafeMode RollbackTrigger = "safe_mode"
	// TriggerOperatorSetMode records an operator-requested stage change.
	TriggerOperatorSetMode RollbackTrigger = "operator_set_mode"
)

// RollbackConfig configures automatic rollback behavior.
type RollbackConfig struct {
	// PnLThreshold is the minimum PnL before rollback (negative).
	PnLThreshold decimal.Decimal `json:"pnl_threshold"`
	// SlippageThreshold is the max acceptable slippage percentage.
	SlippageThreshold decimal.Decimal `json:"slippage_threshold"`
	// RejectionRateThreshold is the max rejection rate (0.0-1.0).
	RejectionRateThreshold float64 `json:"rejection_rate_threshold"`
	// MaxDrawdownThreshold is the max drawdown percentage.
	MaxDrawdownThreshold decimal.Decimal `json:"max_drawdown_threshold"`
	// ConsecutiveLossLimit is the max net losses (losing minus winning trades).
	ConsecutiveLossLimit int `json:"consecutive_loss_limit"`
	// CooldownPeriod is the time before re-enabling after rollback.
	CooldownPeriod time.Duration `json:"cooldown_period"`
	// GracefulRollback enables graceful rollback (cancel orders first).
	GracefulRollback bool `json:"graceful_rollback"`
}

// RollbackEvent records a rollback occurrence.
type RollbackEvent struct {
	// ID is the unique event identifier.
	ID string `json:"id"`
	// StrategyID is the affected strategy.
	StrategyID string `json:"strategy_id"`
	// Trigger is what triggered the rollback.
	Trigger RollbackTrigger `json:"trigger"`
	// FromStage is the stage before rollback.
	FromStage RolloutStage `json:"from_stage"`
	// ToStage is the stage after rollback.
	ToStage RolloutStage `json:"to_stage"`
	// Reason is the detailed reason for rollback.
	Reason string `json:"reason"`
	// MetricsAtRollback are the metrics at time of rollback.
	MetricsAtRollback RolloutMetrics `json:"metrics_at_rollback"`
	// ActionsTaken are the actions taken during rollback.
	ActionsTaken []string `json:"actions_taken"`
	// Timestamp is when the rollback occurred.
	Timestamp time.Time `json:"timestamp"`
}

// GateState represents the state of the live trading gate.
type GateState struct {
	// StrategyID is the strategy being evaluated.
	StrategyID string `json:"strategy_id"`
	// IsOpen indicates if the gate is open for trading.
	IsOpen bool `json:"is_open"`
	// BlockReasons are the reasons blocking live trading.
	BlockReasons []string `json:"block_reasons"`
	// Checks are the individual gate check results.
	Checks GateChecks `json:"checks"`
	// LastEvaluated is when the gate was last evaluated.
	LastEvaluated time.Time `json:"last_evaluated"`
}

// GateChecks contains the results of all gate checks.
type GateChecks struct {
	// SafeModeOff indicates if safe mode is disabled.
	SafeModeOff bool `json:"safe_mode_off"`
	// KillSwitchOff indicates if kill switch is disabled.
	KillSwitchOff bool `json:"kill_switch_off"`
	// StrategyLive indicates if strategy is in live mode.
	StrategyLive bool `json:"strategy_live"`
	// RiskBudgetAvailable indicates if risk budget is available.
	RiskBudgetAvailable bool `json:"risk_budget_available"`
	// ExchangeConnected indicates if exchange is connected.
	ExchangeConnected bool `json:"exchange_connected"`
}

// GateConfig configures the live trading gate.
type GateConfig struct {
	// RequireAllChecks requires all checks to pass.
	RequireAllChecks bool `json:"require_all_checks"`
	// CacheDuration is how long to cache gate results.
	CacheDuration time.Duration `json:"cache_duration"`
	// EvaluationTimeout is the timeout for gate evaluation.
	EvaluationTimeout time.Duration `json:"evaluation_timeout"`
}

// PolicyValidator validates trading policies.
type PolicyValidator interface {
	// ValidateProposal validates a strategy proposal against policies.
	ValidateProposal(ctx context.Context, proposal *StrategyProposal) (bool, string, error)
	// IsSafeModeEnabled returns whether safe mode is enabled.
	IsSafeModeEnabled(ctx context.Context) (bool, error)
	// IsKillSwitchEngaged returns whether kill switch is engaged.
	IsKillSwitchEngaged(ctx context.Context) (bool, error)
}

// RiskManager manages risk budgets and limits.
type RiskManager interface {
	// GetAvailableBudget returns the available risk budget.
	GetAvailableBudget(ctx context.Context, strategyID string) (decimal.Decimal, error)
	// CheckRiskLimits checks if the proposal is within risk limits.
	CheckRiskLimits(ctx context.Context, proposal *StrategyProposal) (bool, string, error)
}

// ExchangeConnector manages exchange connections.
type ExchangeConnector interface {
	// IsConnected returns whether the exchange is connected.
	IsConnected(ctx context.Context, exchange string) (bool, error)
	// CancelAllOrders cancels all open orders for a strategy.
	CancelAllOrders(ctx context.Context, strategyID, exchange string) error
	// FlattenPositions flattens all positions for a strategy.
	FlattenPositions(ctx context.Context, strategyID, exchange string) error
}

// EventPublisher publishes events for audit and monitoring.
type EventPublisher interface {
	// PublishRollbackEvent publishes a rollback event.
	PublishRollbackEvent(ctx context.Context, event *RollbackEvent) error
	// PublishStageTransition publishes a stage transition event.
	PublishStageTransition(ctx context.Context, transition *StageTransition) error
	// PublishGateStateChange publishes a gate state change.
	PublishGateStateChange(ctx context.Context, state *GateState) error
}

// StrategyRepository persists strategy data.
type StrategyRepository interface {
	// SaveRolloutState saves the rollout state.
	SaveRolloutState(ctx context.Context, state *RolloutState) error
	// GetRolloutState retrieves the rollout state.
	GetRolloutState(ctx context.Context, strategyID string) (*RolloutState, error)
	// SaveRollbackEvent saves a rollback event.
	SaveRollbackEvent(ctx context.Context, event *RollbackEvent) error
	// GetRollbackHistory retrieves rollback history.
	GetRollbackHistory(ctx context.Context, strategyID string, limit int) ([]RollbackEvent, error)
}
