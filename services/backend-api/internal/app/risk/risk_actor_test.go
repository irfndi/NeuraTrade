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

func TestRiskActorEvaluateIntent(t *testing.T) {
	// Setup
	policy := NewEngine()
	ks := NewKillSwitch()
	sm := NewSafeMode(DefaultSafeModeConfig())

	ra := NewRiskActor(RiskActorConfig{
		ID:           "test-risk-actor",
		PolicyEngine: policy,
		KillSwitch:   ks,
		SafeMode:     sm,
	})

	// Create actor ref
	ref := actor.NewRef(ra, actor.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ref.Run(ctx)
	time.Sleep(10 * time.Millisecond) // Let actor start

	// Create client ref
	client := NewRiskActorRef(ref)

	// Test evaluation
	intent := ports.OrderIntent{
		IntentID: "test-1",
		Exchange: "binance",
		Symbol:   "BTC/USDT",
		Amount:   decimal.NewFromFloat(1.0),
		Price:    decimal.NewFromFloat(50000.0),
	}

	decision, err := client.EvaluateIntent(ctx, intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve with no rules")
	}
}

func TestNewRiskActor_DefaultDependencies(t *testing.T) {
	ra := NewRiskActor(RiskActorConfig{})
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
	// Setup
	policy := NewEngine()
	ks := NewKillSwitch()
	sm := NewSafeMode(DefaultSafeModeConfig())

	ra := NewRiskActor(RiskActorConfig{
		ID:           "test-risk-actor",
		PolicyEngine: policy,
		KillSwitch:   ks,
		SafeMode:     sm,
	})

	ref := actor.NewRef(ra, actor.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ref.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	client := NewRiskActorRef(ref)

	// Engage kill switch
	err := client.EngageKillSwitch(ctx, "test emergency")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should reject
	intent := ports.OrderIntent{IntentID: "test-1"}
	decision, err := client.EvaluateIntent(ctx, intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Approved {
		t.Error("should reject when kill switch engaged")
	}

	// Disengage
	err = client.DisengageKillSwitch(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should approve now
	decision, err = client.EvaluateIntent(ctx, intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve after disengaging kill switch")
	}
}

func TestRiskActorSafeMode(t *testing.T) {
	policy := NewEngine()
	ks := NewKillSwitch()
	sm := NewSafeMode(DefaultSafeModeConfig())

	ra := NewRiskActor(RiskActorConfig{
		ID:           "test-risk-actor",
		PolicyEngine: policy,
		KillSwitch:   ks,
		SafeMode:     sm,
	})

	ref := actor.NewRef(ra, actor.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ref.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	client := NewRiskActorRef(ref)

	// Enable safe mode
	err := client.EnableSafeMode(ctx, "testing")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Get state
	state, err := client.GetState(ctx)
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
	err = client.DisableSafeMode(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	state, _ = client.GetState(ctx)
	if state.SafeModeEnabled {
		t.Error("safe mode should be disabled")
	}
}

func TestRiskActorGetState(t *testing.T) {
	policy := NewEngine()
	policy.AddRule(NewMaxOrderSizeRule(decimal.NewFromFloat(10.0)))
	ks := NewKillSwitch()
	sm := NewSafeMode(DefaultSafeModeConfig())

	ra := NewRiskActor(RiskActorConfig{
		ID:           "test-risk-actor",
		PolicyEngine: policy,
		KillSwitch:   ks,
		SafeMode:     sm,
	})

	ref := actor.NewRef(ra, actor.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ref.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	client := NewRiskActorRef(ref)

	state, err := client.GetState(ctx)
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

	ra := NewRiskActor(RiskActorConfig{
		ID:           "test-risk-actor",
		PolicyEngine: policy,
		KillSwitch:   ks,
		SafeMode:     sm,
		EventBus:     bus,
	})

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
	time.Sleep(20 * time.Millisecond) // Wait for actor to start

	client := NewRiskActorRef(ref)

	// This should trigger an event
	client.EngageKillSwitch(ctx, "test")

	// Wait for event with timeout
	select {
	case <-receivedChan:
		// Good, event received
	case <-time.After(500 * time.Millisecond):
		t.Error("should have received kill switch event")
	}
}

func TestRiskActorAddRemoveRule(t *testing.T) {
	policy := NewEngine()
	ks := NewKillSwitch()
	sm := NewSafeMode(DefaultSafeModeConfig())

	ra := NewRiskActor(RiskActorConfig{
		ID:           "test-risk-actor",
		PolicyEngine: policy,
		KillSwitch:   ks,
		SafeMode:     sm,
	})

	ref := actor.NewRef(ra, actor.DefaultConfig())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go ref.Run(ctx)
	time.Sleep(10 * time.Millisecond)

	// Test adding rule via message
	reply := make(chan error, 1)
	ref.Send(ctx, AddRuleMsg{
		Rule:  NewMinConfidenceRule(0.8),
		Reply: reply,
	})

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
	ref.Send(ctx, RemoveRuleMsg{
		RuleName: "min_confidence",
		Reply:    reply2,
	})

	select {
	case err := <-reply2:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for response")
	}
}
