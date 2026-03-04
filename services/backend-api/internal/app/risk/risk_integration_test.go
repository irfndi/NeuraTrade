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

// ============================================================
// Integration Tests - Trading Pipeline Risk Gates
// ============================================================

// TestIntegration_SignalProposed_RiskRejected_NoOrder tests the critical path:
// SignalProposed -> Risk evaluates -> Rejected -> No order should be placed
func TestIntegration_SignalProposed_RiskRejected_NoOrder(t *testing.T) {
	// Setup risk system
	policy := NewEngine()

	// Add rule that will reject: only allow BTC/USDT, signal is for ETH/USDT
	if err := policy.AddRule(NewAllowedSymbolsRule([]string{"BTC/USDT"})); err != nil {
		t.Fatalf("add allowed symbols rule: %v", err)
	}
	if err := policy.AddRule(NewMinConfidenceRule(0.7)); err != nil {
		t.Fatalf("add min confidence rule: %v", err)
	}

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

	// Track events
	approvedEvents := make(chan eventbus.Event, 1)
	rejectedEvents := make(chan eventbus.Event, 1)
	bus.Subscribe(ctx, "risk", func(ctx context.Context, event eventbus.Event) error {
		switch event.Type {
		case ports.EventTypeOrderIntentApproved:
			select {
			case approvedEvents <- event:
			default:
			}
		case ports.EventTypeOrderIntentRejected:
			select {
			case rejectedEvents <- event:
			default:
			}
		}
		return nil
	})

	go ref.Run(ctx)
	waitForActorRunning(t, ref, time.Second)

	client := NewRiskActorRef(ref)

	// Simulate SignalProposed from strategy -> creates order intent
	// This intent should be REJECTED because symbol is not allowed
	intent := ports.OrderIntent{
		IntentID:   "signal-eth-buy-001",
		Exchange:   "binance",
		Symbol:     "ETH/USDT", // Not in allowed list
		Side:       ports.OrderSideBuy,
		Type:       ports.OrderTypeMarket,
		Amount:     decimal.NewFromFloat(1.0),
		Price:      decimal.NewFromFloat(3000.0),
		StrategyID: "scalping-v1",
		SignalID:   "signal-001",
		Confidence: 0.8,
	}

	decision, err := client.EvaluateIntent(ctx, intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MUST be rejected
	if decision.Approved {
		t.Error("intent should be rejected - symbol not in allowed list")
	}

	// Verify rejection reason
	if decision.RuleName != "allowed_symbols" {
		t.Errorf("expected rejection by allowed_symbols rule, got: %s", decision.RuleName)
	}

	// Verify rejection event was published
	select {
	case event := <-rejectedEvents:
		if event.Type != ports.EventTypeOrderIntentRejected {
			t.Errorf("expected rejection event, got: %s", event.Type)
		}
		payload, ok := event.Payload.(map[string]any)
		if !ok {
			t.Fatal("payload is not a map")
		}
		if payload["intent_id"] != intent.IntentID {
			t.Errorf("wrong intent_id in event: %v", payload["intent_id"])
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("expected rejection event but none received")
	}

	// Verify no approval event
	select {
	case <-approvedEvents:
		t.Error("should not receive approval event for rejected intent")
	default:
		// Good - no approval event
	}
}

// TestIntegration_SignalProposed_RiskApproved tests the happy path:
// SignalProposed -> Risk evaluates -> Approved -> Order can proceed
func TestIntegration_SignalProposed_RiskApproved(t *testing.T) {
	policy := NewEngine()
	if err := policy.AddRule(NewAllowedSymbolsRule([]string{"BTC/USDT", "ETH/USDT"})); err != nil {
		t.Fatalf("add allowed symbols rule: %v", err)
	}
	if err := policy.AddRule(NewMinConfidenceRule(0.6)); err != nil {
		t.Fatalf("add min confidence rule: %v", err)
	}
	if err := policy.AddRule(NewMaxOrderSizeRule(decimal.NewFromFloat(10.0))); err != nil {
		t.Fatalf("add max order size rule: %v", err)
	}

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

	approvedEvents := make(chan eventbus.Event, 1)
	bus.Subscribe(ctx, "risk", func(ctx context.Context, event eventbus.Event) error {
		if event.Type == ports.EventTypeOrderIntentApproved {
			select {
			case approvedEvents <- event:
			default:
			}
		}
		return nil
	})

	go ref.Run(ctx)
	waitForActorRunning(t, ref, time.Second)

	client := NewRiskActorRef(ref)

	// Valid intent that should pass all rules
	intent := ports.OrderIntent{
		IntentID:   "signal-btc-buy-001",
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Side:       ports.OrderSideBuy,
		Type:       ports.OrderTypeMarket,
		Amount:     decimal.NewFromFloat(0.5), // Under max order size
		Price:      decimal.NewFromFloat(50000.0),
		StrategyID: "scalping-v1",
		SignalID:   "signal-002",
		Confidence: 0.75, // Above threshold
	}

	decision, err := client.EvaluateIntent(ctx, intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MUST be approved
	if !decision.Approved {
		t.Errorf("intent should be approved, reason: %s", decision.Reason)
	}

	// Verify approval event
	select {
	case event := <-approvedEvents:
		if event.Type != ports.EventTypeOrderIntentApproved {
			t.Errorf("expected approval event, got: %s", event.Type)
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("expected approval event but none received")
	}
}

// TestIntegration_KillSwitch_ImmediateBlocking tests that kill switch
// immediately blocks all trading without delay
func TestIntegration_KillSwitch_ImmediateBlocking(t *testing.T) {
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

	go ref.Run(ctx)
	waitForActorRunning(t, ref, time.Second)

	client := NewRiskActorRef(ref)

	// First, verify trading works
	intent := ports.OrderIntent{
		IntentID: "test-1",
		Exchange: "binance",
		Symbol:   "BTC/USDT",
		Amount:   decimal.NewFromFloat(1.0),
	}

	decision, err := client.EvaluateIntent(ctx, intent)
	if err != nil {
		t.Fatalf("evaluate intent before kill switch: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve before kill switch")
	}

	// Engage kill switch
	err = client.EngageKillSwitch(ctx, "emergency stop test")
	if err != nil {
		t.Fatalf("failed to engage kill switch: %v", err)
	}

	// Verify kill switch is engaged
	state, err := client.GetState(ctx)
	if err != nil {
		t.Fatalf("get state after engaging kill switch: %v", err)
	}
	if !state.KillSwitchEngaged {
		t.Error("kill switch should be engaged")
	}

	// ANY intent should be immediately rejected
	for i := 0; i < 5; i++ {
		intent := ports.OrderIntent{
			IntentID:   "test-emergency",
			Exchange:   "binance",
			Symbol:     "BTC/USDT",
			Amount:     decimal.NewFromFloat(1.0),
			Confidence: 0.99,
		}

		decision, err := client.EvaluateIntent(ctx, intent)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if decision.Approved {
			t.Errorf("intent %d should be rejected - kill switch engaged", i)
		}
		if decision.RuleName != "kill_switch" {
			t.Errorf("expected kill_switch rejection, got: %s", decision.RuleName)
		}
	}

	// Disengage and verify trading resumes
	if err := client.DisengageKillSwitch(ctx); err != nil {
		t.Fatalf("disengage kill switch: %v", err)
	}
	decision, err = client.EvaluateIntent(ctx, intent)
	if err != nil {
		t.Fatalf("evaluate intent after disengaging kill switch: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve after kill switch disengaged")
	}
}

// TestIntegration_SafeMode_ImmediateBlocking tests that safe mode
// immediately applies reduced limits and can be enabled/disabled
func TestIntegration_SafeMode_ImmediateBlocking(t *testing.T) {
	config := SafeModeConfig{
		MaxOrderSizeMultiplier: 0.5,
		MaxLeverageMultiplier:  0.5,
		MaxPositionMultiplier:  0.25,
		RestrictToPaper:        false, // Context values don't propagate through actor messages
	}

	policy := NewEngine()
	ks := NewKillSwitch()
	sm := NewSafeMode(config)
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

	go ref.Run(ctx)
	waitForActorRunning(t, ref, time.Second)

	client := NewRiskActorRef(ref)

	// Verify safe mode is initially disabled
	state, err := client.GetState(ctx)
	if err != nil {
		t.Fatalf("get initial safe mode state: %v", err)
	}
	if state.SafeModeEnabled {
		t.Error("safe mode should start disabled")
	}

	// Verify multipliers are 1.0 when safe mode is off
	os, lev, pos := sm.GetMultipliers()
	if os != 1.0 || lev != 1.0 || pos != 1.0 {
		t.Error("multipliers should be 1.0 when safe mode disabled")
	}

	// Enable safe mode
	err = client.EnableSafeMode(ctx, "high volatility detected")
	if err != nil {
		t.Fatalf("failed to enable safe mode: %v", err)
	}

	// Verify safe mode is enabled immediately
	state, err = client.GetState(ctx)
	if err != nil {
		t.Fatalf("get safe mode state after enabling: %v", err)
	}
	if !state.SafeModeEnabled {
		t.Error("safe mode should be enabled")
	}
	if state.SafeModeReason != "high volatility detected" {
		t.Errorf("wrong reason: %s", state.SafeModeReason)
	}

	// Verify multipliers are reduced immediately when safe mode is on
	os, lev, pos = sm.GetMultipliers()
	if os != 0.5 {
		t.Errorf("order size multiplier should be 0.5, got %f", os)
	}
	if lev != 0.5 {
		t.Errorf("leverage multiplier should be 0.5, got %f", lev)
	}
	if pos != 0.25 {
		t.Errorf("position multiplier should be 0.25, got %f", pos)
	}

	// Trading should still work but with reduced limits
	intent := ports.OrderIntent{
		IntentID: "safe-mode-test-1",
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
		t.Error("trading should still work in safe mode (with reduced limits)")
	}

	// Disable safe mode and verify multipliers return to normal immediately
	if err := client.DisableSafeMode(ctx); err != nil {
		t.Fatalf("disable safe mode: %v", err)
	}
	os, lev, pos = sm.GetMultipliers()
	if os != 1.0 || lev != 1.0 || pos != 1.0 {
		t.Error("multipliers should return to 1.0 when safe mode disabled")
	}
}

// TestIntegration_FullRiskPipeline tests the complete risk evaluation pipeline
func TestIntegration_FullRiskPipeline(t *testing.T) {
	// Setup complete risk system with all rules
	policy := NewEngine()
	if err := policy.AddRule(NewMaxOrderSizeRule(decimal.NewFromFloat(5.0))); err != nil {
		t.Fatalf("add max order size rule: %v", err)
	}
	if err := policy.AddRule(NewMaxNotionalRule(decimal.NewFromFloat(100000.0))); err != nil {
		t.Fatalf("add max notional rule: %v", err)
	}
	if err := policy.AddRule(NewAllowedSymbolsRule([]string{"BTC/USDT", "ETH/USDT", "SOL/USDT"})); err != nil {
		t.Fatalf("add allowed symbols rule: %v", err)
	}
	if err := policy.AddRule(NewAllowedExchangesRule([]string{"binance", "coinbase"})); err != nil {
		t.Fatalf("add allowed exchanges rule: %v", err)
	}
	if err := policy.AddRule(NewMinConfidenceRule(0.65)); err != nil {
		t.Fatalf("add min confidence rule: %v", err)
	}
	if err := policy.AddRule(NewMaxDrawdownRule(decimal.NewFromFloat(0.15))); err != nil {
		t.Fatalf("add max drawdown rule: %v", err)
	}
	if err := policy.AddRule(NewMaxDailyLossRule(decimal.NewFromFloat(5000.0))); err != nil {
		t.Fatalf("add max daily loss rule: %v", err)
	}

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

	go ref.Run(ctx)
	waitForActorRunning(t, ref, time.Second)

	client := NewRiskActorRef(ref)

	tests := []struct {
		name     string
		intent   ports.OrderIntent
		approved bool
		rejectBy string
	}{
		{
			name: "valid order passes all checks",
			intent: ports.OrderIntent{
				IntentID:   "valid-1",
				Exchange:   "binance",
				Symbol:     "BTC/USDT",
				Side:       ports.OrderSideBuy,
				Type:       ports.OrderTypeLimit,
				Amount:     decimal.NewFromFloat(1.0),
				Price:      decimal.NewFromFloat(50000.0),
				Confidence: 0.8,
			},
			approved: true,
		},
		{
			name: "rejected - order too large",
			intent: ports.OrderIntent{
				IntentID:   "too-big-1",
				Exchange:   "binance",
				Symbol:     "BTC/USDT",
				Amount:     decimal.NewFromFloat(10.0), // Exceeds max 5.0
				Price:      decimal.NewFromFloat(50000.0),
				Confidence: 0.8,
			},
			approved: false,
			rejectBy: "max_order_size",
		},
		{
			name: "rejected - symbol not allowed",
			intent: ports.OrderIntent{
				IntentID:   "bad-symbol-1",
				Exchange:   "binance",
				Symbol:     "DOGE/USDT", // Not in allowed list
				Amount:     decimal.NewFromFloat(1.0),
				Price:      decimal.NewFromFloat(0.1),
				Confidence: 0.8,
			},
			approved: false,
			rejectBy: "allowed_symbols",
		},
		{
			name: "rejected - exchange not allowed",
			intent: ports.OrderIntent{
				IntentID:   "bad-exchange-1",
				Exchange:   "kraken", // Not in allowed list
				Symbol:     "BTC/USDT",
				Amount:     decimal.NewFromFloat(1.0),
				Price:      decimal.NewFromFloat(50000.0),
				Confidence: 0.8,
			},
			approved: false,
			rejectBy: "allowed_exchanges",
		},
		{
			name: "rejected - low confidence",
			intent: ports.OrderIntent{
				IntentID:   "low-conf-1",
				Exchange:   "binance",
				Symbol:     "BTC/USDT",
				Amount:     decimal.NewFromFloat(1.0),
				Price:      decimal.NewFromFloat(50000.0),
				Confidence: 0.5, // Below 0.65 threshold
			},
			approved: false,
			rejectBy: "min_confidence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision, err := client.EvaluateIntent(ctx, tt.intent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if decision.Approved != tt.approved {
				t.Errorf("expected approved=%v, got %v (reason: %s)",
					tt.approved, decision.Approved, decision.Reason)
			}

			if !tt.approved && decision.RuleName != tt.rejectBy {
				t.Errorf("expected rejection by %s, got %s", tt.rejectBy, decision.RuleName)
			}
		})
	}
}
