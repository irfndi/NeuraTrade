// Package risk implements risk management components.
package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ports"
)

// SafeModeImpl implements ports.SafeMode.
type SafeModeImpl struct {
	mu        sync.RWMutex
	enabled   bool
	enabledAt time.Time
	enabledBy string
	reason    string

	// Reduced limits when in safe mode
	maxOrderSizeMultiplier float64
	maxLeverageMultiplier  float64
	maxPositionMultiplier  float64
	restrictToPaper        bool

	listeners []SafeModeListener
}

// SafeModeListener is called when safe mode state changes.
type SafeModeListener func(enabled bool, reason string)

// SafeModeConfig holds safe mode configuration.
type SafeModeConfig struct {
	// Multipliers applied when safe mode is enabled
	MaxOrderSizeMultiplier float64 // e.g., 0.5 = 50% of normal
	MaxLeverageMultiplier  float64
	MaxPositionMultiplier  float64
	// Whether to force paper trading in safe mode
	RestrictToPaper bool
}

// DefaultSafeModeConfig returns default safe mode config.
func DefaultSafeModeConfig() SafeModeConfig {
	return SafeModeConfig{
		MaxOrderSizeMultiplier: 0.5,
		MaxLeverageMultiplier:  0.5,
		MaxPositionMultiplier:  0.25,
		RestrictToPaper:        true,
	}
}

// NewSafeMode creates a new safe mode controller.
func NewSafeMode(config SafeModeConfig) *SafeModeImpl {
	return &SafeModeImpl{
		maxOrderSizeMultiplier: config.MaxOrderSizeMultiplier,
		maxLeverageMultiplier:  config.MaxLeverageMultiplier,
		maxPositionMultiplier:  config.MaxPositionMultiplier,
		restrictToPaper:        config.RestrictToPaper,
		listeners:              make([]SafeModeListener, 0),
	}
}

// Enable enables safe mode.
func (s *SafeModeImpl) Enable(ctx context.Context) error {
	return s.EnableWithReason(ctx, "manual activation")
}

// EnableWithReason enables safe mode with a specific reason.
func (s *SafeModeImpl) EnableWithReason(ctx context.Context, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.enabled {
		return nil // Already enabled
	}

	s.enabled = true
	s.enabledAt = time.Now()
	s.enabledBy = extractSourceFromContext(ctx)
	s.reason = reason

	// Notify listeners
	for _, listener := range s.listeners {
		go listener(true, reason)
	}

	return nil
}

// Disable disables safe mode.
func (s *SafeModeImpl) Disable(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.enabled {
		return nil // Already disabled
	}

	s.enabled = false
	s.enabledAt = time.Time{}
	s.enabledBy = ""
	s.reason = ""

	// Notify listeners
	for _, listener := range s.listeners {
		go listener(false, "")
	}

	return nil
}

// IsEnabled returns whether safe mode is enabled.
func (s *SafeModeImpl) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// GetReason returns the reason for safe mode being enabled.
func (s *SafeModeImpl) GetReason() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.reason
}

// AddListener adds a listener for safe mode state changes.
func (s *SafeModeImpl) AddListener(listener SafeModeListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

// GetMultipliers returns the current multipliers (affected by safe mode).
func (s *SafeModeImpl) GetMultipliers() (orderSize, leverage, position float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.enabled {
		return s.maxOrderSizeMultiplier, s.maxLeverageMultiplier, s.maxPositionMultiplier
	}
	return 1.0, 1.0, 1.0
}

// IsRestrictedToPaper returns whether trading is restricted to paper mode.
func (s *SafeModeImpl) IsRestrictedToPaper() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled && s.restrictToPaper
}

// SafeModeRule is a hard rule that rejects live orders in safe mode.
type SafeModeRule struct {
	safeMode *SafeModeImpl
}

// NewSafeModeRule creates a new safe mode rule.
func NewSafeModeRule(sm *SafeModeImpl) *SafeModeRule {
	return &SafeModeRule{safeMode: sm}
}

func (r *SafeModeRule) Name() string { return "safe_mode" }

func (r *SafeModeRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	if r.safeMode.IsEnabled() {
		if r.safeMode.IsRestrictedToPaper() {
			// Deny by default unless the caller explicitly sets trading_mode=paper.
			if mode, ok := ctx.Value("trading_mode").(string); !ok || mode != "paper" {
				return ports.PolicyDecision{
					Approved: false,
					Reason:   fmt.Sprintf("safe mode restricts trading to paper mode: %s", r.safeMode.GetReason()),
					RuleName: r.Name(),
				}, nil
			}
		}
		// Allow but with reduced limits
		orderSizeMult, _, _ := r.safeMode.GetMultipliers()
		return ports.PolicyDecision{
			Approved: true,
			RuleName: r.Name(),
			Constraints: []ports.Constraint{
				{Type: "safe_mode_enabled", Value: true},
				{Type: "order_size_multiplier", Value: orderSizeMult},
			},
		}, nil
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
	}, nil
}

func (r *SafeModeRule) IsHardRule() bool { return true }

// AutoSafeModeTrigger automatically enables safe mode based on conditions.
type AutoSafeModeTrigger struct {
	safeMode      *SafeModeImpl
	drawdownLimit float64 // Trigger safe mode at this drawdown
	lossStreak    int     // Trigger after this many consecutive losses
	currentStreak int
	mu            sync.RWMutex
}

// NewAutoSafeModeTrigger creates a new auto safe mode trigger.
func NewAutoSafeModeTrigger(sm *SafeModeImpl, drawdownLimit float64, lossStreak int) *AutoSafeModeTrigger {
	// Non-positive values disable streak-based triggering.
	if lossStreak <= 0 {
		lossStreak = 0
	}
	return &AutoSafeModeTrigger{
		safeMode:      sm,
		drawdownLimit: drawdownLimit,
		lossStreak:    lossStreak,
	}
}

// OnDrawdownUpdate is called when drawdown is updated.
func (t *AutoSafeModeTrigger) OnDrawdownUpdate(drawdown float64) {
	if drawdown >= t.drawdownLimit {
		_ = t.safeMode.EnableWithReason(context.Background(),
			fmt.Sprintf("auto-triggered: drawdown %.2f%% >= limit %.2f%%", drawdown*100, t.drawdownLimit*100))
	}
}

// OnTradeResult is called after each trade result.
func (t *AutoSafeModeTrigger) OnTradeResult(profitable bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if profitable {
		t.currentStreak = 0
		return
	}

	if t.lossStreak <= 0 {
		return
	}

	t.currentStreak++
	if t.currentStreak >= t.lossStreak {
		_ = t.safeMode.EnableWithReason(context.Background(),
			fmt.Sprintf("auto-triggered: %d consecutive losses", t.currentStreak))
		t.currentStreak = 0 // Reset after triggering
	}
}

// Reset resets the streak counter.
func (t *AutoSafeModeTrigger) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.currentStreak = 0
}
