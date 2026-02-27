// Package agentcontrol provides policy engine for agent action authorization.

package agentcontrol

import (
	"context"
	"sync"
	"time"
)

// ActionType represents the type of action being validated.
const (
	ActionPlaybookExecution ActionType = "playbook_execution"
	ActionCommandExecution  ActionType = "command_execution"
)

// Action represents an action to be validated.
type Action struct {
	Type      ActionType
	Playbook  string
	Command   string
	Event     Event
	Timestamp time.Time
}

// ValidationResult represents the result of policy validation.
type ValidationResult struct {
	Approved bool
	Reason   string
}

// PolicyConfig holds policy engine configuration.
type PolicyConfig struct {
	MaxOrderSize     float64
	MaxLeverage      float64
	MaxDailyLoss     float64
	AllowedExchanges []string
	SafeModeEnabled  bool
	KillSwitchActive bool
}

// Engine provides policy validation functionality.
type Engine struct {
	config PolicyConfig
	mu     sync.RWMutex
}

// NewEngine creates a new policy engine.
func NewEngine(config PolicyConfig) *Engine {
	return &Engine{
		config: config,
	}
}

// Validate checks if an action is allowed by policy.
func (e *Engine) Validate(ctx context.Context, action Action) ValidationResult {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Check kill switch (hard block)
	if e.config.KillSwitchActive {
		return ValidationResult{
			Approved: false,
			Reason:   "kill_switch_active",
		}
	}

	// Check safe mode (blocks trading playbooks)
	if e.config.SafeModeEnabled && e.isTradingPlaybook(action.Playbook) {
		return ValidationResult{
			Approved: false,
			Reason:   "safe_mode_enabled",
		}
	}

	// Check exchange allowlist
	if len(e.config.AllowedExchanges) > 0 {
		if eventPayload, ok := action.Event.Payload.(map[string]any); ok {
			if exchangeID, exists := eventPayload["exchange_id"].(string); exists {
				allowed := false
				for _, allowedExchange := range e.config.AllowedExchanges {
					if allowedExchange == exchangeID {
						allowed = true
						break
					}
				}
				if !allowed {
					return ValidationResult{
						Approved: false,
						Reason:   "exchange_not_allowed",
					}
				}
			}
		}
	}

	return ValidationResult{
		Approved: true,
		Reason:   "all_policies_passed",
	}
}

// isTradingPlaybook checks if a playbook involves trading.
func (e *Engine) isTradingPlaybook(playbook string) bool {
	tradingPlaybooks := []string{
		"enable_strategy",
		"place_order",
		"adjust_position",
	}
	for _, tp := range tradingPlaybooks {
		if playbook == tp {
			return true
		}
	}
	return false
}

// UpdateConfig updates the policy configuration.
func (e *Engine) UpdateConfig(config PolicyConfig) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.config = config
}
