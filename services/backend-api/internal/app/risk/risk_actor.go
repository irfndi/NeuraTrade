// Package risk implements risk management components.
package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// ============================================================
// RiskActor Messages
// ============================================================

// EvaluateIntentMsg requests evaluation of an order intent.
type EvaluateIntentMsg struct {
	Intent ports.OrderIntent
	Reply  chan<- EvaluateIntentReply
}

func (m EvaluateIntentMsg) MessageType() string { return "risk.evaluate_intent" }

// EvaluateIntentReply contains the result of intent evaluation.
type EvaluateIntentReply struct {
	Decision ports.PolicyDecision
	Error    error
}

// EngageKillSwitchMsg requests kill switch engagement.
type EngageKillSwitchMsg struct {
	Reason string
	Reply  chan<- error
}

func (m EngageKillSwitchMsg) MessageType() string { return "risk.engage_kill_switch" }

// DisengageKillSwitchMsg requests kill switch disengagement.
type DisengageKillSwitchMsg struct {
	Reply chan<- error
}

func (m DisengageKillSwitchMsg) MessageType() string { return "risk.disengage_kill_switch" }

// EnableSafeModeMsg requests safe mode enablement.
type EnableSafeModeMsg struct {
	Reason string
	Reply  chan<- error
}

func (m EnableSafeModeMsg) MessageType() string { return "risk.enable_safe_mode" }

// DisableSafeModeMsg requests safe mode disablement.
type DisableSafeModeMsg struct {
	Reply chan<- error
}

func (m DisableSafeModeMsg) MessageType() string { return "risk.disable_safe_mode" }

// UpdateDrawdownMsg updates the current drawdown.
type UpdateDrawdownMsg struct {
	Drawdown float64
}

func (m UpdateDrawdownMsg) MessageType() string { return "risk.update_drawdown" }

// UpdateDailyLossMsg updates the daily loss.
type UpdateDailyLossMsg struct {
	Loss float64
}

func (m UpdateDailyLossMsg) MessageType() string { return "risk.update_daily_loss" }

// GetStateMsg requests current risk state.
type GetStateMsg struct {
	Reply chan<- RiskState
}

func (m GetStateMsg) MessageType() string { return "risk.get_state" }

// AddRuleMsg adds a new policy rule.
type AddRuleMsg struct {
	Rule  ports.PolicyRule
	Reply chan<- error
}

func (m AddRuleMsg) MessageType() string { return "risk.add_rule" }

// RemoveRuleMsg removes a policy rule.
type RemoveRuleMsg struct {
	RuleName string
	Reply    chan<- error
}

func (m RemoveRuleMsg) MessageType() string { return "risk.remove_rule" }

// RecordTradeResultMsg records a trade result for risk tracking.
type RecordTradeResultMsg struct {
	Profitable bool
	PnL        float64
}

func (m RecordTradeResultMsg) MessageType() string { return "risk.record_trade_result" }

// ============================================================
// Risk State
// ============================================================

// RiskState represents the current state of the risk system.
type RiskState struct {
	KillSwitchEngaged  bool
	SafeModeEnabled    bool
	SafeModeReason     string
	CurrentDrawdown    float64
	DailyLoss          float64
	ConsecutiveLosses  int
	ActiveRules        []string
	LastEvaluationTime time.Time
}

// ============================================================
// RiskActor
// ============================================================

// RiskActor processes risk evaluation requests and manages risk state.
// It is the single source of truth for risk decisions.
type RiskActor struct {
	id          string
	policy      *Engine
	killSwitch  *KillSwitchImpl
	safeMode    *SafeModeImpl
	autoTrigger *AutoSafeModeTrigger

	// Event bus for publishing risk events
	eventBus *eventbus.Bus

	// State tracking
	mu                sync.RWMutex
	consecutiveLosses int
	dailyLoss         float64
	drawdown          float64
	lastEvalTime      time.Time
	traceID           string // Current trace ID for event propagation
	// Drawdown tracking for MaxDrawdownRule
	drawdownRule  *MaxDrawdownRule
	dailyLossRule *MaxDailyLossRule
	cooldownRule  *CooldownRule
}

// RiskActorConfig holds configuration for RiskActor.
type RiskActorConfig struct {
	ID                  string
	PolicyEngine        *Engine
	KillSwitch          *KillSwitchImpl
	SafeMode            *SafeModeImpl
	EventBus            *eventbus.Bus
	MaxDrawdown         float64
	MaxDailyLoss        float64
	CooldownPeriod      time.Duration
	CooldownAfterLosses int
}

// NewRiskActor creates a new RiskActor.
func NewRiskActor(config RiskActorConfig) *RiskActor {
	ra := &RiskActor{
		id:         config.ID,
		policy:     config.PolicyEngine,
		killSwitch: config.KillSwitch,
		safeMode:   config.SafeMode,
		eventBus:   config.EventBus,
	}

	// Set up auto trigger if safe mode is provided
	if config.SafeMode != nil && config.MaxDrawdown > 0 {
		ra.autoTrigger = NewAutoSafeModeTrigger(config.SafeMode, config.MaxDrawdown, config.CooldownAfterLosses)
	}

	// Initialize tracking rules
	if config.MaxDrawdown > 0 {
		ra.drawdownRule = NewMaxDrawdownRule(floatToDecimal(config.MaxDrawdown))
	}
	if config.MaxDailyLoss > 0 {
		ra.dailyLossRule = NewMaxDailyLossRule(floatToDecimal(config.MaxDailyLoss))
	}
	if config.CooldownPeriod > 0 && config.CooldownAfterLosses > 0 {
		ra.cooldownRule = NewCooldownRule(config.CooldownPeriod, config.CooldownAfterLosses)
	}

	// Add kill switch and safe mode rules to policy engine
	if config.PolicyEngine != nil {
		if config.KillSwitch != nil {
			_ = config.PolicyEngine.AddRule(NewKillSwitchRule(config.KillSwitch))
		}
		if config.SafeMode != nil {
			_ = config.PolicyEngine.AddRule(NewSafeModeRule(config.SafeMode))
		}
	}

	// Set up kill switch listener
	if config.KillSwitch != nil {
		config.KillSwitch.AddListener(func(state ports.KillSwitchState) {
			if ra.eventBus != nil && state.Enabled {
				ra.publishEvent(ports.EventTypeKillSwitchEngaged, map[string]any{
					"reason":        state.Reason,
					"engaged_by":    state.EngagedBy,
					"cancel_orders": state.CancelOrders,
				})
			}
		})
	}

	// Set up safe mode listener
	if config.SafeMode != nil {
		config.SafeMode.AddListener(func(enabled bool, reason string) {
			if ra.eventBus != nil && enabled {
				ra.publishEvent(ports.EventTypeSafeModeEnabled, map[string]any{
					"reason": reason,
				})
			}
		})
	}

	return ra
}

// ID returns the actor's identifier.
func (a *RiskActor) ID() string {
	return a.id
}

// Receive processes incoming messages.
func (a *RiskActor) Receive(ctx context.Context, env actor.Envelope) error {
	a.mu.Lock()
	a.lastEvalTime = time.Now()
	a.traceID = env.TraceID // Capture trace ID for event propagation
	a.mu.Unlock()

	switch msg := env.Message.(type) {
	case EvaluateIntentMsg:
		a.handleEvaluateIntent(ctx, msg)
	case EngageKillSwitchMsg:
		a.handleEngageKillSwitch(ctx, msg)
	case DisengageKillSwitchMsg:
		a.handleDisengageKillSwitch(ctx, msg)
	case EnableSafeModeMsg:
		a.handleEnableSafeMode(ctx, msg)
	case DisableSafeModeMsg:
		a.handleDisableSafeMode(ctx, msg)
	case UpdateDrawdownMsg:
		a.handleUpdateDrawdown(msg)
	case UpdateDailyLossMsg:
		a.handleUpdateDailyLoss(msg)
	case GetStateMsg:
		a.handleGetState(msg)
	case AddRuleMsg:
		a.handleAddRule(msg)
	case RemoveRuleMsg:
		a.handleRemoveRule(msg)
	case RecordTradeResultMsg:
		a.handleRecordTradeResult(msg)
	default:
		return fmt.Errorf("unknown message type: %T", msg)
	}

	return nil
}

func (a *RiskActor) handleEvaluateIntent(ctx context.Context, msg EvaluateIntentMsg) {
	decision, err := a.policy.Evaluate(ctx, msg.Intent)

	// Publish decision event
	if a.eventBus != nil {
		eventType := ports.EventTypeOrderIntentApproved
		if err != nil || !decision.Approved {
			eventType = ports.EventTypeOrderIntentRejected
		}
		a.publishEvent(eventType, map[string]any{
			"intent_id": msg.Intent.IntentID,
			"approved":  decision.Approved,
			"reason":    decision.Reason,
			"rule":      decision.RuleName,
		})
	}

	if msg.Reply != nil {
		msg.Reply <- EvaluateIntentReply{
			Decision: decision,
			Error:    err,
		}
	}
}

func (a *RiskActor) handleEngageKillSwitch(ctx context.Context, msg EngageKillSwitchMsg) {
	err := a.killSwitch.Engage(ctx, msg.Reason)
	if msg.Reply != nil {
		msg.Reply <- err
	}
}

func (a *RiskActor) handleDisengageKillSwitch(ctx context.Context, msg DisengageKillSwitchMsg) {
	err := a.killSwitch.Disengage(ctx)
	if msg.Reply != nil {
		msg.Reply <- err
	}
}

func (a *RiskActor) handleEnableSafeMode(ctx context.Context, msg EnableSafeModeMsg) {
	err := a.safeMode.EnableWithReason(ctx, msg.Reason)
	if msg.Reply != nil {
		msg.Reply <- err
	}
}

func (a *RiskActor) handleDisableSafeMode(ctx context.Context, msg DisableSafeModeMsg) {
	err := a.safeMode.Disable(ctx)
	if msg.Reply != nil {
		msg.Reply <- err
	}
}

func (a *RiskActor) handleUpdateDrawdown(msg UpdateDrawdownMsg) {
	a.mu.Lock()
	a.drawdown = msg.Drawdown
	a.mu.Unlock()

	if a.drawdownRule != nil {
		a.drawdownRule.UpdatePortfolioValue(floatToDecimal(1.0 - msg.Drawdown))
	}

	if a.autoTrigger != nil {
		a.autoTrigger.OnDrawdownUpdate(msg.Drawdown)
	}
}

func (a *RiskActor) handleUpdateDailyLoss(msg UpdateDailyLossMsg) {
	a.mu.Lock()
	a.dailyLoss = msg.Loss
	a.mu.Unlock()

	if a.dailyLossRule != nil {
		a.dailyLossRule.UpdateDailyLoss(floatToDecimal(msg.Loss))
	}
}

func (a *RiskActor) handleGetState(msg GetStateMsg) {
	a.mu.RLock()
	state := RiskState{
		KillSwitchEngaged:  a.killSwitch.IsEngaged(),
		SafeModeEnabled:    a.safeMode.IsEnabled(),
		SafeModeReason:     a.safeMode.GetReason(),
		CurrentDrawdown:    a.drawdown,
		DailyLoss:          a.dailyLoss,
		ConsecutiveLosses:  a.consecutiveLosses,
		ActiveRules:        a.policy.ListRules(),
		LastEvaluationTime: a.lastEvalTime,
	}
	a.mu.RUnlock()

	if msg.Reply != nil {
		msg.Reply <- state
	}
}

func (a *RiskActor) handleAddRule(msg AddRuleMsg) {
	err := a.policy.AddRule(msg.Rule)
	if msg.Reply != nil {
		msg.Reply <- err
	}
}

func (a *RiskActor) handleRemoveRule(msg RemoveRuleMsg) {
	err := a.policy.RemoveRule(msg.RuleName)
	if msg.Reply != nil {
		msg.Reply <- err
	}
}

func (a *RiskActor) handleRecordTradeResult(msg RecordTradeResultMsg) {
	a.mu.Lock()
	if msg.Profitable {
		a.consecutiveLosses = 0
	} else {
		a.consecutiveLosses++
	}
	a.mu.Unlock()

	if a.cooldownRule != nil {
		if msg.Profitable {
			a.cooldownRule.RecordWin()
		} else {
			a.cooldownRule.RecordLoss()
		}
	}

	if a.autoTrigger != nil {
		a.autoTrigger.OnTradeResult(msg.Profitable)
	}
}

func (a *RiskActor) publishEvent(eventType string, payload map[string]any) {
	if a.eventBus == nil {
		return
	}

	a.mu.RLock()
	traceID := a.traceID
	a.mu.RUnlock()

	event := eventbus.NewEvent("risk", eventType, payload).
		WithSource(a.id).
		WithTraceID(traceID)

	_ = a.eventBus.Publish(context.Background(), event)
}

// ============================================================
// RiskActorRef - Convenience wrapper for sending messages
// ============================================================

// RiskActorRef provides a type-safe interface to interact with a RiskActor.
type RiskActorRef struct {
	ref *actor.Ref
}

// NewRiskActorRef creates a new RiskActorRef.
func NewRiskActorRef(ref *actor.Ref) *RiskActorRef {
	return &RiskActorRef{ref: ref}
}

// EvaluateIntent evaluates an order intent.
func (r *RiskActorRef) EvaluateIntent(ctx context.Context, intent ports.OrderIntent) (ports.PolicyDecision, error) {
	reply := make(chan EvaluateIntentReply, 1)
	msg := EvaluateIntentMsg{
		Intent: intent,
		Reply:  reply,
	}

	if err := r.ref.Send(ctx, msg); err != nil {
		return ports.PolicyDecision{}, err
	}

	select {
	case resp := <-reply:
		return resp.Decision, resp.Error
	case <-ctx.Done():
		return ports.PolicyDecision{}, ctx.Err()
	}
}

// EngageKillSwitch engages the kill switch.
func (r *RiskActorRef) EngageKillSwitch(ctx context.Context, reason string) error {
	reply := make(chan error, 1)
	msg := EngageKillSwitchMsg{Reason: reason, Reply: reply}

	if err := r.ref.Send(ctx, msg); err != nil {
		return err
	}

	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DisengageKillSwitch disengages the kill switch.
func (r *RiskActorRef) DisengageKillSwitch(ctx context.Context) error {
	reply := make(chan error, 1)
	msg := DisengageKillSwitchMsg{Reply: reply}

	if err := r.ref.Send(ctx, msg); err != nil {
		return err
	}

	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// EnableSafeMode enables safe mode.
func (r *RiskActorRef) EnableSafeMode(ctx context.Context, reason string) error {
	reply := make(chan error, 1)
	msg := EnableSafeModeMsg{Reason: reason, Reply: reply}

	if err := r.ref.Send(ctx, msg); err != nil {
		return err
	}

	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// DisableSafeMode disables safe mode.
func (r *RiskActorRef) DisableSafeMode(ctx context.Context) error {
	reply := make(chan error, 1)
	msg := DisableSafeModeMsg{Reply: reply}

	if err := r.ref.Send(ctx, msg); err != nil {
		return err
	}

	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetState gets the current risk state.
func (r *RiskActorRef) GetState(ctx context.Context) (RiskState, error) {
	reply := make(chan RiskState, 1)
	msg := GetStateMsg{Reply: reply}

	if err := r.ref.Send(ctx, msg); err != nil {
		return RiskState{}, err
	}

	select {
	case state := <-reply:
		return state, nil
	case <-ctx.Done():
		return RiskState{}, ctx.Err()
	}
}

// Helper function
func floatToDecimal(f float64) decimal.Decimal {
	return decimal.NewFromFloat(f)
}
