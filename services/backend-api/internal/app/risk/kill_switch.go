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
	store        KillSwitchStore
}

// KillSwitchListener is called when kill switch state changes.
type KillSwitchListener func(state ports.KillSwitchState)

// NewKillSwitch creates a new kill switch.
func NewKillSwitch() *KillSwitchImpl {
	return &KillSwitchImpl{
		cancelOrders: true,
		listeners:    make([]KillSwitchListener, 0),
	}
}

// SetStore installs a persistence backend. Engage/Disengage will write to the
// store on every state change. Call Reconcile after SetStore to load the
// previous state on startup.
func (k *KillSwitchImpl) SetStore(store KillSwitchStore) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.store = store
}

// Reconcile reads the persisted state (if any) and applies it to the
// in-memory actor. If no state is stored, the in-memory defaults are kept.
func (k *KillSwitchImpl) Reconcile(ctx context.Context) error {
	k.mu.Lock()
	store := k.store
	k.mu.Unlock()
	if store == nil {
		return nil
	}
	state, found, err := store.Load(ctx)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	k.engaged = state.Engaged
	k.engagedAt = state.EngagedAt
	k.engagedBy = state.EngagedBy
	k.reason = state.Reason
	k.cancelOrders = state.CancelOrders
	return nil
}

func (k *KillSwitchImpl) persistLocked(state ports.KillSwitchState) {
	if k.store == nil {
		return
	}
	persisted := PersistedKillSwitchState{
		Engaged:      state.Enabled,
		EngagedBy:    state.EngagedBy,
		Reason:       state.Reason,
		CancelOrders: state.CancelOrders,
		UpdatedAt:    time.Now(),
	}
	if !k.engagedAt.IsZero() {
		persisted.EngagedAt = k.engagedAt
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = k.store.Save(ctx, persisted)
	}()
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

	state := k.stateLocked()
	k.persistLocked(state)

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

	state := k.stateLocked()
	k.persistLocked(state)

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
	return sourceFromContext(ctx)
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
