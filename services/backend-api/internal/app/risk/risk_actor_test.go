package risk

import (
	"context"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

type riskActorFixture struct {
	ctx    context.Context
	cancel context.CancelFunc
	ref    *actor.Ref
	client *RiskActorRef
}

func newTestRiskFixture(t *testing.T, cfg RiskActorConfig) *riskActorFixture {
	t.Helper()

	if cfg.ID == "" {
		cfg.ID = "test-risk-actor"
	}
	if cfg.PolicyEngine == nil {
		cfg.PolicyEngine = NewEngine()
	}
	if cfg.KillSwitch == nil {
		cfg.KillSwitch = NewKillSwitch()
	}
	if cfg.SafeMode == nil {
		cfg.SafeMode = NewSafeMode(DefaultSafeModeConfig())
	}

	ra, err := NewRiskActor(cfg)
	if err != nil {
		t.Fatalf("new risk actor: %v", err)
	}

	ref := actor.NewRef(ra, actor.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	go ref.Run(ctx)
	waitForActorRunning(t, ref, time.Second)

	return &riskActorFixture{
		ctx:    ctx,
		cancel: cancel,
		ref:    ref,
		client: NewRiskActorRef(ref),
	}
}

func (f *riskActorFixture) Close() {
	f.cancel()
	f.ref.Stop()
}

func TestRiskActorEvaluateIntent(t *testing.T) {
	fixture := newTestRiskFixture(t, RiskActorConfig{})
	defer fixture.Close()

	// Test evaluation
	intent := ports.OrderIntent{
		IntentID: "test-1",
		Exchange: "binance",
		Symbol:   "BTC/USDT",
		Amount:   decimal.NewFromFloat(1.0),
		Price:    decimal.NewFromFloat(50000.0),
	}

	decision, err := fixture.client.EvaluateIntent(fixture.ctx, intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve with no rules")
	}
}

func TestNewRiskActor_DefaultDependencies(t *testing.T) {
	ra, err := NewRiskActor(RiskActorConfig{})
	if err != nil {
		t.Fatalf("new risk actor: %v", err)
	}
	if ra.policy == nil {
		t.Fatal("policy engine should be initialized")
	}
	if ra.killSwitch == nil {
		t.Fatal("kill switch should be initialized")
	}
	if ra.safeMode == nil {
		t.Fatal("safe mode should be initialized")
	}
}

func TestRiskActorKillSwitchViaMessages(t *testing.T) {
	fixture := newTestRiskFixture(t, RiskActorConfig{})
	defer fixture.Close()

	// Engage kill switch
	err := fixture.client.EngageKillSwitch(fixture.ctx, "test emergency")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should reject
	intent := ports.OrderIntent{IntentID: "test-1"}
	decision, err := fixture.client.EvaluateIntent(fixture.ctx, intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Approved {
		t.Error("should reject when kill switch engaged")
	}

	// Disengage
	err = fixture.client.DisengageKillSwitch(fixture.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should approve now
	decision, err = fixture.client.EvaluateIntent(fixture.ctx, intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve after disengaging kill switch")
	}
}

func TestRiskActorSafeMode(t *testing.T) {
	fixture := newTestRiskFixture(t, RiskActorConfig{})
	defer fixture.Close()

	// Enable safe mode
	err := fixture.client.EnableSafeMode(fixture.ctx, "testing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get state
	state, err := fixture.client.GetState(fixture.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !state.SafeModeEnabled {
		t.Error("safe mode should be enabled")
	}
	if state.SafeModeReason != "testing" {
		t.Errorf("wrong reason: %s", state.SafeModeReason)
	}

	// Disable safe mode
	err = fixture.client.DisableSafeMode(fixture.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, err = fixture.client.GetState(fixture.ctx)
	if err != nil {
		t.Fatalf("GetState after disabling safe mode: %v", err)
	}
	if state.SafeModeEnabled {
		t.Error("safe mode should be disabled")
	}
}

func TestRiskActorGetState(t *testing.T) {
	policy := NewEngine()
	if err := policy.AddRule(NewMaxOrderSizeRule(decimal.NewFromFloat(10.0))); err != nil {
		t.Fatalf("add max order size rule: %v", err)
	}
	fixture := newTestRiskFixture(t, RiskActorConfig{PolicyEngine: policy})
	defer fixture.Close()

	state, err := fixture.client.GetState(fixture.ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state.KillSwitchEngaged {
		t.Error("kill switch should not be engaged")
	}
	if state.SafeModeEnabled {
		t.Error("safe mode should not be enabled")
	}
	if len(state.ActiveRules) < 1 {
		t.Error("should have at least one rule")
	}
}

func TestRiskActorWithEventBus(t *testing.T) {
	policy := NewEngine()
	ks := NewKillSwitch()
	sm := NewSafeMode(DefaultSafeModeConfig())
	bus := eventbus.New(eventbus.DefaultConfig())

	ra, err := NewRiskActor(RiskActorConfig{
		ID:           "test-risk-actor",
		PolicyEngine: policy,
		KillSwitch:   ks,
		SafeMode:     sm,
		EventBus:     bus,
	})
	if err != nil {
		t.Fatalf("new risk actor: %v", err)
	}

	ref := actor.NewRef(ra, actor.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe to risk events BEFORE starting actor
	receivedChan := make(chan eventbus.Event, 1)
	bus.Subscribe(ctx, "risk", func(ctx context.Context, event eventbus.Event) error {
		select {
		case receivedChan <- event:
		default:
		}
		return nil
	})

	go ref.Run(ctx)
	waitForActorRunning(t, ref, time.Second)

	client := NewRiskActorRef(ref)

	// This should trigger an event
	if err := client.EngageKillSwitch(ctx, "test"); err != nil {
		t.Fatalf("EngageKillSwitch failed: %v", err)
	}

	// Wait for event with timeout
	select {
	case <-receivedChan:
		// Good, event received
	case <-time.After(500 * time.Millisecond):
		t.Error("should have received kill switch event")
	}
}

func TestRiskActorAddRemoveRule(t *testing.T) {
	fixture := newTestRiskFixture(t, RiskActorConfig{})
	defer fixture.Close()

	// Test adding rule via message
	reply := make(chan error, 1)
	if err := fixture.ref.Send(fixture.ctx, AddRuleMsg{
		Rule:  NewMinConfidenceRule(0.8),
		Reply: reply,
	}); err != nil {
		t.Fatalf("send AddRuleMsg failed: %v", err)
	}

	select {
	case err := <-reply:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}

	// Test removing rule via message
	reply2 := make(chan error, 1)
	if err := fixture.ref.Send(fixture.ctx, RemoveRuleMsg{
		RuleName: "min_confidence",
		Reply:    reply2,
	}); err != nil {
		t.Fatalf("send RemoveRuleMsg failed: %v", err)
	}

	select {
	case err := <-reply2:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}
}
