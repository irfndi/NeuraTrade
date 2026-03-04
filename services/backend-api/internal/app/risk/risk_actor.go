// Package risk implements risk management components.
package risk

import (
	"context"
	"fmt"
	"log"
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
	Drawdown decimal.Decimal
}

func (m UpdateDrawdownMsg) MessageType() string { return "risk.update_drawdown" }

// UpdateDailyLossMsg updates the daily loss.
type UpdateDailyLossMsg struct {
	Loss decimal.Decimal
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
	PnL        decimal.Decimal
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
	CurrentDrawdown    decimal.Decimal
	DailyLoss          decimal.Decimal
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
	dailyLoss         decimal.Decimal
	drawdown          decimal.Decimal
	lastEvalTime      time.Time
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
	MaxDrawdown         decimal.Decimal
	MaxDailyLoss        decimal.Decimal
	CooldownPeriod      time.Duration
	CooldownAfterLosses int
}

// NewRiskActor creates a new RiskActor.
func NewRiskActor(config RiskActorConfig) (*RiskActor, error) {
	policyEngine := config.PolicyEngine
	if policyEngine == nil {
		policyEngine = NewEngine()
	}

	killSwitch := config.KillSwitch
	if killSwitch == nil {
		killSwitch = NewKillSwitch()
	}

	safeMode := config.SafeMode
	if safeMode == nil {
		safeMode = NewSafeMode(DefaultSafeModeConfig())
	}

	actorID := config.ID
	if actorID == "" {
		actorID = "risk-actor"
	}

	ra := &RiskActor{
		id:         actorID,
		policy:     policyEngine,
		killSwitch: killSwitch,
		safeMode:   safeMode,
		eventBus:   config.EventBus,
	}

	// Set up auto trigger if safe mode is provided
	if config.MaxDrawdown.GreaterThan(decimal.Zero) {
		ra.autoTrigger = NewAutoSafeModeTrigger(safeMode, config.MaxDrawdown.InexactFloat64(), config.CooldownAfterLosses)
	}

	// Initialize tracking rules
	if config.MaxDrawdown.GreaterThan(decimal.Zero) {
		ra.drawdownRule = NewMaxDrawdownRule(config.MaxDrawdown)
	}
	if config.MaxDailyLoss.GreaterThan(decimal.Zero) {
		ra.dailyLossRule = NewMaxDailyLossRule(config.MaxDailyLoss)
	}
	if config.CooldownPeriod > 0 && config.CooldownAfterLosses > 0 {
		ra.cooldownRule = NewCooldownRule(config.CooldownPeriod, config.CooldownAfterLosses)
	}

	// Add kill switch and safe mode rules to policy engine
	if err := policyEngine.AddRule(NewKillSwitchRule(killSwitch)); err != nil {
		return nil, fmt.Errorf("add kill switch rule: %w", err)
	}
	if err := policyEngine.AddRule(NewSafeModeRule(safeMode)); err != nil {
		return nil, fmt.Errorf("add safe mode rule: %w", err)
	}

	// Set up kill switch listener
	killSwitch.AddListener(func(state ports.KillSwitchState) {
		if ra.eventBus != nil && state.Enabled {
			ra.publishEvent(context.Background(), "", ports.EventTypeKillSwitchEngaged, map[string]any{
				"reason":        state.Reason,
				"engaged_by":    state.EngagedBy,
				"cancel_orders": state.CancelOrders,
			})
		}
	})

	// Set up safe mode listener
	safeMode.AddListener(func(enabled bool, reason string) {
		if ra.eventBus != nil && enabled {
			ra.publishEvent(context.Background(), "", ports.EventTypeSafeModeEnabled, map[string]any{
				"reason": reason,
			})
		}
	})

	return ra, nil
}

// ID returns the actor's identifier.
func (a *RiskActor) ID() string {
	return a.id
}

// Receive processes incoming messages.
func (a *RiskActor) Receive(ctx context.Context, env actor.Envelope) error {
	traceID := env.TraceID
	a.mu.Lock()
	a.lastEvalTime = time.Now()
	a.mu.Unlock()

	switch msg := env.Message.(type) {
	case EvaluateIntentMsg:
		a.handleEvaluateIntent(ctx, traceID, msg)
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

func (a *RiskActor) handleEvaluateIntent(ctx context.Context, traceID string, msg EvaluateIntentMsg) {
	decision, err := a.policy.Evaluate(ctx, msg.Intent)

	// Publish decision event
	if a.eventBus != nil {
		eventType := ports.EventTypeOrderIntentApproved
		if err != nil || !decision.Approved {
			eventType = ports.EventTypeOrderIntentRejected
		}
		a.publishEvent(ctx, traceID, eventType, map[string]any{
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
		a.drawdownRule.UpdateDrawdown(msg.Drawdown)
	}

	if a.autoTrigger != nil {
		a.autoTrigger.OnDrawdownUpdate(msg.Drawdown.InexactFloat64())
	}
}

func (a *RiskActor) handleUpdateDailyLoss(msg UpdateDailyLossMsg) {
	a.mu.Lock()
	prevLoss := a.dailyLoss
	a.dailyLoss = msg.Loss
	a.mu.Unlock()

	if a.dailyLossRule != nil {
		a.dailyLossRule.UpdateDailyLoss(msg.Loss.Sub(prevLoss))
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

func (a *RiskActor) publishEvent(ctx context.Context, traceID, eventType string, payload map[string]any) {
	if a.eventBus == nil {
		return
	}

	event := eventbus.NewEvent("risk", eventType, payload).
		WithSource(a.id).
		WithTraceID(traceID)

	publishCtx := ctx
	if publishCtx == nil {
		publishCtx = context.Background()
	}

	if err := a.eventBus.Publish(publishCtx, event); err != nil {
		log.Printf("[risk] %v", fmt.Errorf("publish risk event type=%s: %w", eventType, err))
	}
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

// UpdateDrawdown updates current drawdown tracking inside the risk actor.
func (r *RiskActorRef) UpdateDrawdown(ctx context.Context, drawdown decimal.Decimal) error {
	return r.ref.Send(ctx, UpdateDrawdownMsg{Drawdown: drawdown})
}

// UpdateDailyLoss updates current daily loss tracking inside the risk actor.
func (r *RiskActorRef) UpdateDailyLoss(ctx context.Context, loss decimal.Decimal) error {
	return r.ref.Send(ctx, UpdateDailyLossMsg{Loss: loss})
}

// RecordTradeResult records a trade outcome for cooldown/loss streak tracking.
func (r *RiskActorRef) RecordTradeResult(ctx context.Context, profitable bool, pnl decimal.Decimal) error {
	return r.ref.Send(ctx, RecordTradeResultMsg{
		Profitable: profitable,
		PnL:        pnl,
	})
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
