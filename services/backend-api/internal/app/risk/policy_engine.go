// Package risk implements risk management components including
// policy engine, kill switch, safe mode, and risk actor.
package risk

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// ============================================================
// Hard Rules - Cannot be overridden
// ============================================================

// MaxOrderSizeRule limits the maximum order size.
type MaxOrderSizeRule struct {
	maxSize decimal.Decimal
}

// NewMaxOrderSizeRule creates a new max order size rule.
func NewMaxOrderSizeRule(maxSize decimal.Decimal) *MaxOrderSizeRule {
	return &MaxOrderSizeRule{maxSize: maxSize}
}

func (r *MaxOrderSizeRule) Name() string { return "max_order_size" }

func (r *MaxOrderSizeRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	amount := intent.Amount
	if amount.GreaterThan(r.maxSize) {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   fmt.Sprintf("order size %s exceeds maximum %s", amount.String(), r.maxSize.String()),
			RuleName: r.Name(),
		}, nil
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
		Constraints: []ports.Constraint{
			{Type: "max_size", Value: r.maxSize.String()},
		},
	}, nil
}

func (r *MaxOrderSizeRule) IsHardRule() bool { return true }

// MaxLeverageRule limits the maximum leverage.
type MaxLeverageRule struct {
	maxLeverage decimal.Decimal
}

// NewMaxLeverageRule creates a new max leverage rule.
func NewMaxLeverageRule(maxLeverage decimal.Decimal) *MaxLeverageRule {
	return &MaxLeverageRule{maxLeverage: maxLeverage}
}

func (r *MaxLeverageRule) Name() string { return "max_leverage" }

func (r *MaxLeverageRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	if r.maxLeverage.LessThanOrEqual(decimal.Zero) {
		return ports.PolicyDecision{
			Approved: true,
			RuleName: r.Name(),
		}, nil
	}

	portfolioValue := decimal.NewFromFloat(intent.PortfolioValue)
	if portfolioValue.LessThanOrEqual(decimal.Zero) {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   "leverage unknown - conservative deny (missing portfolio value)",
			RuleName: r.Name(),
		}, nil
	}
	if intent.Price.LessThanOrEqual(decimal.Zero) {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   "leverage unknown - conservative deny (missing price)",
			RuleName: r.Name(),
		}, nil
	}

	notional := intent.Amount.Abs().Mul(intent.Price.Abs())
	effectiveLeverage := notional.Div(portfolioValue)
	if effectiveLeverage.GreaterThan(r.maxLeverage) {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   fmt.Sprintf("leverage %s exceeds maximum %s", effectiveLeverage.String(), r.maxLeverage.String()),
			RuleName: r.Name(),
		}, nil
	}

	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
		Constraints: []ports.Constraint{
			{Type: "effective_leverage", Value: effectiveLeverage.String()},
			{Type: "max_leverage", Value: r.maxLeverage.String()},
		},
	}, nil
}

func (r *MaxLeverageRule) IsHardRule() bool { return true }

// MaxNotionalRule limits the maximum notional value per trade.
type MaxNotionalRule struct {
	maxNotional decimal.Decimal
}

// NewMaxNotionalRule creates a new max notional rule.
func NewMaxNotionalRule(maxNotional decimal.Decimal) *MaxNotionalRule {
	return &MaxNotionalRule{maxNotional: maxNotional}
}

func (r *MaxNotionalRule) Name() string { return "max_notional" }

func (r *MaxNotionalRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	amount := intent.Amount
	price := intent.Price
	notional := amount.Mul(price)

	if notional.GreaterThan(r.maxNotional) {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   fmt.Sprintf("notional value %s exceeds maximum %s", notional.String(), r.maxNotional.String()),
			RuleName: r.Name(),
		}, nil
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
		Constraints: []ports.Constraint{
			{Type: "max_notional", Value: r.maxNotional.String()},
		},
	}, nil
}

func (r *MaxNotionalRule) IsHardRule() bool { return true }

// MaxDailyLossRule limits daily loss.
type MaxDailyLossRule struct {
	maxLoss   decimal.Decimal
	dailyLoss decimal.Decimal
	lastReset time.Time
	mu        sync.RWMutex
}

// NewMaxDailyLossRule creates a new max daily loss rule.
func NewMaxDailyLossRule(maxLoss decimal.Decimal) *MaxDailyLossRule {
	return &MaxDailyLossRule{
		maxLoss:   maxLoss,
		lastReset: time.Now(),
	}
}

// UpdateDailyLoss updates the current daily loss.
func (r *MaxDailyLossRule) UpdateDailyLoss(loss decimal.Decimal) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Reset if new day
	now := time.Now()
	if !sameCalendarDay(now, r.lastReset) {
		r.dailyLoss = decimal.Zero
		r.lastReset = now
	}

	r.dailyLoss = r.dailyLoss.Add(loss)
}

func (r *MaxDailyLossRule) Name() string { return "max_daily_loss" }

func (r *MaxDailyLossRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.dailyLoss.GreaterThan(r.maxLoss) {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   fmt.Sprintf("daily loss %s exceeds maximum %s", r.dailyLoss.String(), r.maxLoss.String()),
			RuleName: r.Name(),
		}, nil
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
		Constraints: []ports.Constraint{
			{Type: "current_daily_loss", Value: r.dailyLoss.String()},
			{Type: "max_daily_loss", Value: r.maxLoss.String()},
		},
	}, nil
}

func (r *MaxDailyLossRule) IsHardRule() bool { return true }

// MaxDrawdownRule limits maximum drawdown.
type MaxDrawdownRule struct {
	maxDrawdown   decimal.Decimal
	currentDraw   decimal.Decimal
	highWaterMark decimal.Decimal
	mu            sync.RWMutex
}

// NewMaxDrawdownRule creates a new max drawdown rule.
func NewMaxDrawdownRule(maxDrawdown decimal.Decimal) *MaxDrawdownRule {
	return &MaxDrawdownRule{maxDrawdown: maxDrawdown}
}

// UpdatePortfolioValue updates the portfolio value for drawdown calculation.
func (r *MaxDrawdownRule) UpdatePortfolioValue(value decimal.Decimal) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if value.GreaterThan(r.highWaterMark) {
		r.highWaterMark = value
	}

	if r.highWaterMark.GreaterThan(decimal.Zero) {
		r.currentDraw = r.highWaterMark.Sub(value).Div(r.highWaterMark)
	}
}

// UpdateDrawdown directly updates current drawdown.
func (r *MaxDrawdownRule) UpdateDrawdown(drawdown decimal.Decimal) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.currentDraw = drawdown
}

func (r *MaxDrawdownRule) Name() string { return "max_drawdown" }

func (r *MaxDrawdownRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.currentDraw.GreaterThan(r.maxDrawdown) {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   fmt.Sprintf("drawdown %s exceeds maximum %s", r.currentDraw.String(), r.maxDrawdown.String()),
			RuleName: r.Name(),
		}, nil
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
		Constraints: []ports.Constraint{
			{Type: "current_drawdown", Value: r.currentDraw.String()},
			{Type: "max_drawdown", Value: r.maxDrawdown.String()},
		},
	}, nil
}

func (r *MaxDrawdownRule) IsHardRule() bool { return true }

// AllowedSymbolsRule restricts trading to allowed symbols.
type AllowedSymbolsRule struct {
	allowedSymbols map[string]bool
}

// NewAllowedSymbolsRule creates a new allowed symbols rule.
func NewAllowedSymbolsRule(symbols []string) *AllowedSymbolsRule {
	allowed := make(map[string]bool)
	for _, s := range symbols {
		allowed[s] = true
	}
	return &AllowedSymbolsRule{allowedSymbols: allowed}
}

func (r *AllowedSymbolsRule) Name() string { return "allowed_symbols" }

func (r *AllowedSymbolsRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	symbolKey := fmt.Sprintf("%s:%s", intent.Exchange, intent.Symbol)
	if len(r.allowedSymbols) > 0 && !r.allowedSymbols[symbolKey] && !r.allowedSymbols[intent.Symbol] {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   fmt.Sprintf("symbol %s not in allowed list", intent.Symbol),
			RuleName: r.Name(),
		}, nil
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
	}, nil
}

func (r *AllowedSymbolsRule) IsHardRule() bool { return true }

// AllowedExchangesRule restricts trading to allowed exchanges.
type AllowedExchangesRule struct {
	allowedExchanges map[string]bool
}

// NewAllowedExchangesRule creates a new allowed exchanges rule.
func NewAllowedExchangesRule(exchanges []string) *AllowedExchangesRule {
	allowed := make(map[string]bool)
	for _, e := range exchanges {
		allowed[e] = true
	}
	return &AllowedExchangesRule{allowedExchanges: allowed}
}

func (r *AllowedExchangesRule) Name() string { return "allowed_exchanges" }

func (r *AllowedExchangesRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	if len(r.allowedExchanges) > 0 && !r.allowedExchanges[intent.Exchange] {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   fmt.Sprintf("exchange %s not in allowed list", intent.Exchange),
			RuleName: r.Name(),
		}, nil
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
	}, nil
}

func (r *AllowedExchangesRule) IsHardRule() bool { return true }

// ============================================================
// Soft Rules - Can be tuned/overridden
// ============================================================

// MinLiquidityRule requires minimum liquidity.
type MinLiquidityRule struct {
	minLiquidity decimal.Decimal
}

// NewMinLiquidityRule creates a new min liquidity rule.
func NewMinLiquidityRule(minLiquidity decimal.Decimal) *MinLiquidityRule {
	return &MinLiquidityRule{minLiquidity: minLiquidity}
}

func (r *MinLiquidityRule) Name() string { return "min_liquidity" }

func (r *MinLiquidityRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	// Placeholder - would need actual liquidity data from orderbook
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
	}, nil
}

func (r *MinLiquidityRule) IsHardRule() bool { return false }

// MaxSpreadRule limits maximum spread.
type MaxSpreadRule struct {
	maxSpread decimal.Decimal
}

// NewMaxSpreadRule creates a new max spread rule.
func NewMaxSpreadRule(maxSpread decimal.Decimal) *MaxSpreadRule {
	return &MaxSpreadRule{maxSpread: maxSpread}
}

func (r *MaxSpreadRule) Name() string { return "max_spread" }

func (r *MaxSpreadRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	// Placeholder - would need actual spread data
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
	}, nil
}

func (r *MaxSpreadRule) IsHardRule() bool { return false }

// MinConfidenceRule requires minimum strategy confidence.
type MinConfidenceRule struct {
	minConfidence float64
}

// NewMinConfidenceRule creates a new min confidence rule.
func NewMinConfidenceRule(minConfidence float64) *MinConfidenceRule {
	return &MinConfidenceRule{minConfidence: minConfidence}
}

func (r *MinConfidenceRule) Name() string { return "min_confidence" }

func (r *MinConfidenceRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	if intent.Confidence < r.minConfidence {
		return ports.PolicyDecision{
			Approved: false,
			Reason:   fmt.Sprintf("confidence %.2f below minimum %.2f", intent.Confidence, r.minConfidence),
			RuleName: r.Name(),
		}, nil
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
	}, nil
}

func (r *MinConfidenceRule) IsHardRule() bool { return false }

// CooldownRule enforces cooldown after consecutive losses.
type CooldownRule struct {
	cooldownPeriod    time.Duration
	consecutiveLosses int
	lossCount         int
	lastLossTime      time.Time
	mu                sync.RWMutex
}

// NewCooldownRule creates a new cooldown rule.
func NewCooldownRule(cooldownPeriod time.Duration, afterLosses int) *CooldownRule {
	return &CooldownRule{
		cooldownPeriod:    cooldownPeriod,
		consecutiveLosses: afterLosses,
	}
}

// RecordLoss records a loss for cooldown tracking.
func (r *CooldownRule) RecordLoss() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lossCount++
	r.lastLossTime = time.Now()
}

// RecordWin resets the loss counter.
func (r *CooldownRule) RecordWin() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lossCount = 0
}

func (r *CooldownRule) Name() string { return "cooldown_after_losses" }

func (r *CooldownRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.lossCount >= r.consecutiveLosses {
		elapsed := time.Since(r.lastLossTime)
		if elapsed < r.cooldownPeriod {
			remaining := r.cooldownPeriod - elapsed
			return ports.PolicyDecision{
				Approved: false,
				Reason:   fmt.Sprintf("cooldown active, %v remaining", remaining.Round(time.Second)),
				RuleName: r.Name(),
			}, nil
		}
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
	}, nil
}

func (r *CooldownRule) IsHardRule() bool { return false }

// ============================================================
// Policy Engine
// ============================================================

// Engine implements ports.PolicyEngine with hard and soft rules.
type Engine struct {
	hardRules []ports.PolicyRule
	softRules []ports.PolicyRule
	mu        sync.RWMutex
}

// NewEngine creates a new policy engine.
func NewEngine() *Engine {
	return &Engine{
		hardRules: make([]ports.PolicyRule, 0),
		softRules: make([]ports.PolicyRule, 0),
	}
}

// AddRule adds a policy rule.
func (e *Engine) AddRule(rule ports.PolicyRule) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if rule.IsHardRule() {
		e.hardRules = append(e.hardRules, rule)
	} else {
		e.softRules = append(e.softRules, rule)
	}
	return nil
}

// RemoveRule removes a policy rule.
func (e *Engine) RemoveRule(ruleName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Remove from hard rules
	for i, r := range e.hardRules {
		if r.Name() == ruleName {
			e.hardRules = append(e.hardRules[:i], e.hardRules[i+1:]...)
			return nil
		}
	}

	// Remove from soft rules
	for i, r := range e.softRules {
		if r.Name() == ruleName {
			e.softRules = append(e.softRules[:i], e.softRules[i+1:]...)
			return nil
		}
	}

	return fmt.Errorf("rule %s not found", ruleName)
}

// ListRules lists all policy rules.
func (e *Engine) ListRules() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rules := make([]string, 0, len(e.hardRules)+len(e.softRules))
	for _, r := range e.hardRules {
		rules = append(rules, r.Name()+" (hard)")
	}
	for _, r := range e.softRules {
		rules = append(rules, r.Name()+" (soft)")
	}
	return rules
}

// Evaluate evaluates an order intent against all rules.
// Hard rules are evaluated first and must all pass.
// Soft rules are evaluated after and provide warnings/constraints.
func (e *Engine) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	// Evaluate hard rules first
	for _, rule := range e.hardRules {
		decision, err := rule.Evaluate(ctx, intent)
		if err != nil {
			return ports.PolicyDecision{}, fmt.Errorf("rule %s evaluation failed: %w", rule.Name(), err)
		}
		if !decision.Approved {
			// Hard rule rejection is final
			return decision, nil
		}
	}

	// Evaluate soft rules
	var constraints []ports.Constraint
	for _, rule := range e.softRules {
		decision, err := rule.Evaluate(ctx, intent)
		if err != nil {
			wrappedErr := fmt.Errorf("soft rule %s evaluation failed: %w", rule.Name(), err)
			log.Printf("[risk] %v", wrappedErr)
			continue
		}
		if !decision.Approved {
			// Soft rule rejection is also blocking in default mode
			return decision, nil
		}
		constraints = append(constraints, decision.Constraints...)
	}

	return ports.PolicyDecision{
		Approved:    true,
		Reason:      "all rules passed",
		Constraints: constraints,
	}, nil
}

// GetHardRules returns all hard rules.
func (e *Engine) GetHardRules() []ports.PolicyRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]ports.PolicyRule{}, e.hardRules...)
}

// GetSoftRules returns all soft rules.
func (e *Engine) GetSoftRules() []ports.PolicyRule {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return append([]ports.PolicyRule{}, e.softRules...)
}

func sameCalendarDay(a, b time.Time) bool {
	yearA, monthA, dayA := a.Date()
	yearB, monthB, dayB := b.Date()
	return yearA == yearB && monthA == monthB && dayA == dayB
}
