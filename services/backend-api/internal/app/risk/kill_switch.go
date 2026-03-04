// Package risk implements risk management components.
package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ports"
)

// KillSwitchImpl implements ports.KillSwitch.
type KillSwitchImpl struct {
	mu           sync.RWMutex
	engaged      bool
	engagedAt    time.Time
	engagedBy    string
	reason       string
	cancelOrders bool
	listeners    []KillSwitchListener
}

// KillSwitchListener is called when kill switch state changes.
type KillSwitchListener func(state ports.KillSwitchState)

// NewKillSwitch creates a new kill switch.
func NewKillSwitch() *KillSwitchImpl {
	return &KillSwitchImpl{
		listeners: make([]KillSwitchListener, 0),
	}
}

// Engage engages the kill switch, blocking all trading.
func (k *KillSwitchImpl) Engage(ctx context.Context, reason string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.engaged {
		return nil // Already engaged
	}

	k.engaged = true
	k.engagedAt = time.Now()
	k.engagedBy = extractSourceFromContext(ctx)
	k.reason = reason
	k.cancelOrders = true // Default to cancel orders on engage

	// Notify listeners
	state := k.stateLocked()
	for _, listener := range k.listeners {
		go listener(state)
	}

	return nil
}

// Disengage disengages the kill switch.
func (k *KillSwitchImpl) Disengage(ctx context.Context) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.engaged {
		return nil // Already disengaged
	}

	k.engaged = false
	k.engagedAt = time.Time{}
	k.engagedBy = ""
	k.reason = ""
	k.cancelOrders = false

	// Notify listeners
	state := k.stateLocked()
	for _, listener := range k.listeners {
		go listener(state)
	}

	return nil
}

// IsEngaged returns whether the kill switch is engaged.
func (k *KillSwitchImpl) IsEngaged() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.engaged
}

// State returns the current kill switch state.
func (k *KillSwitchImpl) State() ports.KillSwitchState {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.stateLocked()
}

func (k *KillSwitchImpl) stateLocked() ports.KillSwitchState {
	var engagedAt int64
	if !k.engagedAt.IsZero() {
		engagedAt = k.engagedAt.Unix()
	}
	return ports.KillSwitchState{
		Enabled:      k.engaged,
		EngagedAt:    engagedAt,
		EngagedBy:    k.engagedBy,
		Reason:       k.reason,
		CancelOrders: k.cancelOrders,
	}
}

// AddListener adds a listener for kill switch state changes.
func (k *KillSwitchImpl) AddListener(listener KillSwitchListener) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.listeners = append(k.listeners, listener)
}

// SetCancelOrders sets whether to cancel orders on engage.
func (k *KillSwitchImpl) SetCancelOrders(cancel bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.cancelOrders = cancel
}

// ShouldCancelOrders returns whether orders should be cancelled.
func (k *KillSwitchImpl) ShouldCancelOrders() bool {
	k.mu.RLock()
	defer k.mu.RUnlock()
	return k.engaged && k.cancelOrders
}

// extractSourceFromContext extracts the source from context.
func extractSourceFromContext(ctx context.Context) string {
	if source, ok := ctx.Value("source").(string); ok {
		return source
	}
	if source, ok := ctx.Value("user").(string); ok {
		return source
	}
	return "system"
}

// KillSwitchRule is a hard rule that rejects all orders when kill switch is engaged.
type KillSwitchRule struct {
	killSwitch *KillSwitchImpl
}

// NewKillSwitchRule creates a new kill switch rule.
func NewKillSwitchRule(ks *KillSwitchImpl) *KillSwitchRule {
	return &KillSwitchRule{killSwitch: ks}
}

func (r *KillSwitchRule) Name() string { return "kill_switch" }

func (r *KillSwitchRule) Evaluate(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	if r.killSwitch.IsEngaged() {
		state := r.killSwitch.State()
		return ports.PolicyDecision{
			Approved: false,
			Reason:   fmt.Sprintf("kill switch engaged: %s (by %s)", state.Reason, state.EngagedBy),
			RuleName: r.Name(),
		}, nil
	}
	return ports.PolicyDecision{
		Approved: true,
		RuleName: r.Name(),
	}, nil
}

func (r *KillSwitchRule) IsHardRule() bool { return true }
