package autonomous

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// DefaultGateConfig returns default gate configuration.
func DefaultGateConfig() GateConfig {
	return GateConfig{
		RequireAllChecks:  true,
		CacheDuration:     5 * time.Second,
		EvaluationTimeout: 10 * time.Second,
	}
}

// LiveTradingGate is the final authorization gate for live trading.
type LiveTradingGate struct {
	config    GateConfig
	validator PolicyValidator
	risk      RiskManager
	exchange  ExchangeConnector
	rollout   *StagedRolloutManager

	// Cache for gate states
	cache      map[string]*cachedGateState
	cacheMutex sync.RWMutex
}

type cachedGateState struct {
	state    *GateState
	cachedAt time.Time
}

// NewLiveTradingGate creates a new live trading gate.
func NewLiveTradingGate(
	config GateConfig,
	validator PolicyValidator,
	risk RiskManager,
	exchange ExchangeConnector,
	rollout *StagedRolloutManager,
) *LiveTradingGate {
	return &LiveTradingGate{
		config:    config,
		validator: validator,
		risk:      risk,
		exchange:  exchange,
		rollout:   rollout,
		cache:     make(map[string]*cachedGateState),
	}
}

// Evaluate evaluates all gate checks for a strategy.
func (g *LiveTradingGate) Evaluate(ctx context.Context, strategyID string) (*GateState, error) {
	// Check cache first
	if cached := g.getFromCache(strategyID); cached != nil {
		return cached, nil
	}

	// Evaluate with timeout
	ctx, cancel := context.WithTimeout(ctx, g.config.EvaluationTimeout)
	defer cancel()

	checks := GateChecks{}
	var blockReasons []string

	// Check policy validation
	if g.validator != nil {
		passes, _, err := g.validator.ValidateProposal(ctx, &StrategyProposal{
			StrategyID: strategyID,
		})
		if err != nil {
			blockReasons = append(blockReasons, fmt.Sprintf("policy_check_error: %v", err))
			checks.PolicyPasses = false
		} else {
			checks.PolicyPasses = passes
			if !passes {
				blockReasons = append(blockReasons, "policy_validation_failed")
			}
		}
	} else {
		checks.PolicyPasses = true // No validator = pass
	}

	// Check safe mode
	if g.validator != nil {
		safeMode, err := g.validator.IsSafeModeEnabled(ctx)
		if err != nil {
			blockReasons = append(blockReasons, fmt.Sprintf("safe_mode_check_error: %v", err))
			checks.SafeModeOff = false
		} else {
			checks.SafeModeOff = !safeMode
			if safeMode {
				blockReasons = append(blockReasons, "safe_mode_enabled")
			}
		}
	} else {
		checks.SafeModeOff = true
	}

	// Check kill switch
	if g.validator != nil {
		killSwitch, err := g.validator.IsKillSwitchEngaged(ctx)
		if err != nil {
			blockReasons = append(blockReasons, fmt.Sprintf("kill_switch_check_error: %v", err))
			checks.KillSwitchOff = false
		} else {
			checks.KillSwitchOff = !killSwitch
			if killSwitch {
				blockReasons = append(blockReasons, "kill_switch_engaged")
			}
		}
	} else {
		checks.KillSwitchOff = true
	}

	// Check strategy mode using rollout state
	if g.rollout != nil {
		rolloutState, err := g.rollout.GetRolloutState(ctx, strategyID)
		if err != nil {
			blockReasons = append(blockReasons, fmt.Sprintf("rollout_state_check_error: %v", err))
			checks.StrategyLive = false
		} else if rolloutState == nil {
			blockReasons = append(blockReasons, "rollout_state_not_found")
			checks.StrategyLive = false
		} else {
			isLive := rolloutState.CurrentStage == StageLive && rolloutState.Status == StatusActive
			checks.StrategyLive = isLive
			if !isLive {
				blockReasons = append(blockReasons, fmt.Sprintf("strategy_not_live (stage: %s, status: %s)", rolloutState.CurrentStage, rolloutState.Status))
			}
		}
	} else {
		// For safety, default to false if rollout manager is not configured
		checks.StrategyLive = false
		blockReasons = append(blockReasons, "rollout_manager_not_configured")
	}
	// Check risk budget
	if g.risk != nil {
		budget, err := g.risk.GetAvailableBudget(ctx, strategyID)
		if err != nil {
			blockReasons = append(blockReasons, fmt.Sprintf("budget_check_error: %v", err))
			checks.RiskBudgetAvailable = false
		} else {
			checks.RiskBudgetAvailable = budget.GreaterThan(decimal.Zero)
			if !checks.RiskBudgetAvailable {
				blockReasons = append(blockReasons, "no_risk_budget_available")
			}
		}
	} else {
		checks.RiskBudgetAvailable = true
	}

	// Check exchange connection
	if g.exchange != nil {
		connected, err := g.exchange.IsConnected(ctx, "")
		if err != nil {
			blockReasons = append(blockReasons, fmt.Sprintf("exchange_check_error: %v", err))
			checks.ExchangeConnected = false
		} else {
			checks.ExchangeConnected = connected
			if !connected {
				blockReasons = append(blockReasons, "exchange_not_connected")
			}
		}
	} else {
		checks.ExchangeConnected = true
	}

	// Determine if gate is open
	isOpen := g.determineGateOpen(checks)

	state := &GateState{
		StrategyID:    strategyID,
		IsOpen:        isOpen,
		BlockReasons:  blockReasons,
		Checks:        checks,
		LastEvaluated: time.Now(),
	}

	// Cache the result
	g.storeInCache(strategyID, state)

	return state, nil
}

// IsOpen returns whether the gate is open for live trading.
func (g *LiveTradingGate) IsOpen(ctx context.Context, strategyID string) (bool, error) {
	state, err := g.Evaluate(ctx, strategyID)
	if err != nil {
		return false, err
	}
	return state.IsOpen, nil
}

// GetBlockReasons returns the reasons blocking live trading.
func (g *LiveTradingGate) GetBlockReasons(ctx context.Context, strategyID string) ([]string, error) {
	state, err := g.Evaluate(ctx, strategyID)
	if err != nil {
		return nil, err
	}
	return state.BlockReasons, nil
}

// ForceOpen forces the gate open (for emergency situations).
// WARNING: This bypasses safety checks and should only be used in emergencies.
func (g *LiveTradingGate) ForceOpen(ctx context.Context, strategyID string) error {
	state := &GateState{
		StrategyID:    strategyID,
		IsOpen:        true,
		BlockReasons:  []string{"force_opened"},
		Checks:        GateChecks{},
		LastEvaluated: time.Now(),
	}
	g.storeInCache(strategyID, state)
	return nil
}

// ForceClose forces the gate closed.
func (g *LiveTradingGate) ForceClose(ctx context.Context, strategyID string, reason string) error {
	state := &GateState{
		StrategyID:    strategyID,
		IsOpen:        false,
		BlockReasons:  []string{fmt.Sprintf("force_closed: %s", reason)},
		Checks:        GateChecks{},
		LastEvaluated: time.Now(),
	}
	g.storeInCache(strategyID, state)
	return nil
}

// ClearCache clears the gate cache for a strategy.
func (g *LiveTradingGate) ClearCache(strategyID string) {
	g.cacheMutex.Lock()
	defer g.cacheMutex.Unlock()
	delete(g.cache, strategyID)
}

// ClearAllCache clears all cached gate states.
func (g *LiveTradingGate) ClearAllCache() {
	g.cacheMutex.Lock()
	defer g.cacheMutex.Unlock()
	g.cache = make(map[string]*cachedGateState)
}

// determineGateOpen determines if the gate should be open based on checks.
func (g *LiveTradingGate) determineGateOpen(checks GateChecks) bool {
	if g.config.RequireAllChecks {
		return checks.PolicyPasses &&
			checks.SafeModeOff &&
			checks.KillSwitchOff &&
			checks.StrategyLive &&
			checks.RiskBudgetAvailable &&
			checks.ExchangeConnected
	}

	// If not requiring all checks, only block on critical ones
	return checks.SafeModeOff && checks.KillSwitchOff
}

// getFromCache retrieves a cached gate state.
func (g *LiveTradingGate) getFromCache(strategyID string) *GateState {
	g.cacheMutex.RLock()
	defer g.cacheMutex.RUnlock()

	cached, exists := g.cache[strategyID]
	if !exists {
		return nil
	}

	// Check if cache is still valid
	if time.Since(cached.cachedAt) > g.config.CacheDuration {
		return nil
	}

	return cached.state
}

// storeInCache stores a gate state in cache.
func (g *LiveTradingGate) storeInCache(strategyID string, state *GateState) {
	g.cacheMutex.Lock()
	defer g.cacheMutex.Unlock()

	g.cache[strategyID] = &cachedGateState{
		state:    state,
		cachedAt: time.Now(),
	}
}
