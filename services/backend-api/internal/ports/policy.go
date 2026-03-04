// Package ports defines the application's port interfaces.
package ports

import (
	"context"

	"github.com/shopspring/decimal"
)

// ============================================================
// Policy Engine - Risk and Safety Gate
// ============================================================

// PolicyDecision represents a policy decision.
type PolicyDecision struct {
	Approved    bool
	Reason      string
	RuleName    string
	Constraints []Constraint
}

// Constraint represents a constraint on an order.
type Constraint struct {
	Type  string // "max_size", "max_price", "deadline"
	Value any
}

// PolicyRule represents a policy rule.
type PolicyRule interface {
	// Name returns the rule name.
	Name() string

	// Evaluate evaluates the rule against an order intent.
	Evaluate(ctx context.Context, intent OrderIntent) (PolicyDecision, error)

	// IsHardRule returns true if this is a hard rule (cannot override).
	IsHardRule() bool
}

// OrderIntent represents an intent to place an order.
type OrderIntent struct {
	IntentID        string
	Exchange        string
	Symbol          string
	Side            OrderSide
	Type            OrderType
	Amount          decimal.Decimal
	Price           decimal.Decimal
	StrategyID      string
	SignalID        string
	Confidence      float64
	StopLoss        float64
	TakeProfit      float64
	CurrentPosition float64
	PortfolioValue  float64
}

// PolicyEngine evaluates order intents against policy rules.
type PolicyEngine interface {
	// Evaluate evaluates an order intent.
	Evaluate(ctx context.Context, intent OrderIntent) (PolicyDecision, error)

	// AddRule adds a policy rule.
	AddRule(rule PolicyRule) error

	// RemoveRule removes a policy rule.
	RemoveRule(ruleName string) error

	// ListRules lists all policy rules.
	ListRules() []string
}

// ============================================================
// Kill Switch
// ============================================================

// KillSwitchState represents the state of the kill switch.
type KillSwitchState struct {
	Enabled      bool
	EngagedAt    int64
	EngagedBy    string
	Reason       string
	CancelOrders bool // Whether to cancel orders on engage
}

// KillSwitch provides emergency stop capabilities.
type KillSwitch interface {
	// Engage engages the kill switch (blocks all trading).
	Engage(ctx context.Context, reason string) error

	// Disengage disengages the kill switch.
	Disengage(ctx context.Context) error

	// IsEngaged returns whether the kill switch is engaged.
	IsEngaged() bool

	// State returns the current state.
	State() KillSwitchState
}

// ============================================================
// Safe Mode
// ============================================================

// SafeMode provides a reduced-risk operating mode.
type SafeMode interface {
	// Enable enables safe mode.
	Enable(ctx context.Context) error

	// Disable disables safe mode.
	Disable(ctx context.Context) error

	// IsEnabled returns whether safe mode is enabled.
	IsEnabled() bool
}
