package risk

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// ============================================================
// Policy Engine Tests
// ============================================================

func TestEngineEvaluateEmpty(t *testing.T) {
	engine := NewEngine()
	intent := ports.OrderIntent{
		IntentID: "test-1",
		Exchange: "binance",
		Symbol:   "BTC/USDT",
		Amount:   decimal.NewFromFloat(1.0),
		Price:    decimal.NewFromFloat(50000.0),
	}

	decision, err := engine.Evaluate(context.Background(), intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("empty engine should approve everything")
	}
}

func TestMaxOrderSizeRule(t *testing.T) {
	rule := NewMaxOrderSizeRule(decimal.NewFromFloat(10.0))

	tests := []struct {
		name     string
		amount   float64
		approved bool
	}{
		{"under limit", 5.0, true},
		{"at limit", 10.0, true},
		{"over limit", 15.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := ports.OrderIntent{Amount: decimal.NewFromFloat(tt.amount)}
			decision, err := rule.Evaluate(context.Background(), intent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Approved != tt.approved {
				t.Errorf("expected approved=%v, got %v", tt.approved, decision.Approved)
			}
			if decision.RuleName != "max_order_size" {
				t.Errorf("wrong rule name: %s", decision.RuleName)
			}
		})
	}
}

func TestMaxNotionalRule(t *testing.T) {
	rule := NewMaxNotionalRule(decimal.NewFromFloat(10000.0))

	tests := []struct {
		name     string
		amount   float64
		price    float64
		approved bool
	}{
		{"under limit", 1.0, 5000.0, true},
		{"at limit", 2.0, 5000.0, true},
		{"over limit", 3.0, 5000.0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := ports.OrderIntent{
				Amount: decimal.NewFromFloat(tt.amount),
				Price:  decimal.NewFromFloat(tt.price),
			}
			decision, err := rule.Evaluate(context.Background(), intent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Approved != tt.approved {
				t.Errorf("expected approved=%v, got %v", tt.approved, decision.Approved)
			}
		})
	}
}

func TestAllowedSymbolsRule(t *testing.T) {
	rule := NewAllowedSymbolsRule([]string{"BTC/USDT", "ETH/USDT"})

	tests := []struct {
		name     string
		symbol   string
		approved bool
	}{
		{"allowed symbol", "BTC/USDT", true},
		{"not allowed", "DOGE/USDT", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := ports.OrderIntent{Symbol: tt.symbol}
			decision, err := rule.Evaluate(context.Background(), intent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Approved != tt.approved {
				t.Errorf("expected approved=%v, got %v", tt.approved, decision.Approved)
			}
		})
	}
}

func TestMinConfidenceRule(t *testing.T) {
	rule := NewMinConfidenceRule(0.7)

	tests := []struct {
		name       string
		confidence float64
		approved   bool
	}{
		{"above threshold", 0.8, true},
		{"at threshold", 0.7, true},
		{"below threshold", 0.5, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := ports.OrderIntent{Confidence: tt.confidence}
			decision, err := rule.Evaluate(context.Background(), intent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Approved != tt.approved {
				t.Errorf("expected approved=%v, got %v", tt.approved, decision.Approved)
			}
		})
	}
}

func TestEngineWithMultipleRules(t *testing.T) {
	engine := NewEngine()
	if err := engine.AddRule(NewMaxOrderSizeRule(decimal.NewFromFloat(10.0))); err != nil {
		t.Fatalf("add max order size rule: %v", err)
	}
	if err := engine.AddRule(NewAllowedSymbolsRule([]string{"BTC/USDT"})); err != nil {
		t.Fatalf("add allowed symbols rule: %v", err)
	}
	if err := engine.AddRule(NewMinConfidenceRule(0.5)); err != nil {
		t.Fatalf("add min confidence rule: %v", err)
	}

	// Should pass all rules
	intent1 := ports.OrderIntent{
		IntentID:   "test-1",
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Amount:     decimal.NewFromFloat(5.0),
		Confidence: 0.7,
	}
	decision, err := engine.Evaluate(context.Background(), intent1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("should pass all rules")
	}

	// Should fail max order size
	intent2 := ports.OrderIntent{
		IntentID:   "test-2",
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Amount:     decimal.NewFromFloat(15.0),
		Confidence: 0.7,
	}
	decision, err = engine.Evaluate(context.Background(), intent2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Approved {
		t.Error("should fail max order size")
	}

	// Should fail allowed symbols
	intent3 := ports.OrderIntent{
		IntentID:   "test-3",
		Exchange:   "binance",
		Symbol:     "ETH/USDT",
		Amount:     decimal.NewFromFloat(5.0),
		Confidence: 0.7,
	}
	decision, err = engine.Evaluate(context.Background(), intent3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Approved {
		t.Error("should fail allowed symbols")
	}
}

func TestEngineRemoveRule(t *testing.T) {
	engine := NewEngine()
	if err := engine.AddRule(NewMaxOrderSizeRule(decimal.NewFromFloat(10.0))); err != nil {
		t.Fatalf("add max order size rule: %v", err)
	}

	rules := engine.ListRules()
	if len(rules) != 1 {
		t.Errorf("expected 1 rule, got %d", len(rules))
	}

	err := engine.RemoveRule("max_order_size")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rules = engine.ListRules()
	if len(rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(rules))
	}

	err = engine.RemoveRule("nonexistent")
	if err == nil {
		t.Error("should return error for nonexistent rule")
	}
}

// ============================================================
// Kill Switch Tests
// ============================================================

func TestKillSwitchEngage(t *testing.T) {
	ks := NewKillSwitch()

	if ks.IsEngaged() {
		t.Error("kill switch should start disengaged")
	}

	err := ks.Engage(context.Background(), "test emergency")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !ks.IsEngaged() {
		t.Error("kill switch should be engaged")
	}

	state := ks.State()
	if !state.Enabled {
		t.Error("state should show enabled")
	}
	if state.Reason != "test emergency" {
		t.Errorf("wrong reason: %s", state.Reason)
	}
}

func TestKillSwitchDisengage(t *testing.T) {
	ks := NewKillSwitch()
	if err := ks.Engage(context.Background(), "test"); err != nil {
		t.Fatalf("engage kill switch: %v", err)
	}

	err := ks.Disengage(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ks.IsEngaged() {
		t.Error("kill switch should be disengaged")
	}
}

func TestKillSwitchRule(t *testing.T) {
	ks := NewKillSwitch()
	rule := NewKillSwitchRule(ks)

	// Should approve when not engaged
	intent := ports.OrderIntent{IntentID: "test"}
	decision, err := rule.Evaluate(context.Background(), intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve when kill switch not engaged")
	}

	// Should reject when engaged
	if err := ks.Engage(context.Background(), "emergency"); err != nil {
		t.Fatalf("engage kill switch: %v", err)
	}
	decision, err = rule.Evaluate(context.Background(), intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Approved {
		t.Error("should reject when kill switch engaged")
	}
}

func TestKillSwitchListener(t *testing.T) {
	ks := NewKillSwitch()
	var called atomic.Bool

	ks.AddListener(func(state ports.KillSwitchState) {
		called.Store(true)
	})

	if err := ks.Engage(context.Background(), "test"); err != nil {
		t.Fatalf("engage kill switch: %v", err)
	}

	time.Sleep(50 * time.Millisecond) // Wait for goroutine

	if !called.Load() {
		t.Error("listener should have been called")
	}
}

// ============================================================
// Safe Mode Tests
// ============================================================

func TestSafeModeEnable(t *testing.T) {
	sm := NewSafeMode(DefaultSafeModeConfig())

	if sm.IsEnabled() {
		t.Error("safe mode should start disabled")
	}

	err := sm.Enable(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sm.IsEnabled() {
		t.Error("safe mode should be enabled")
	}
}

func TestSafeModeEnableWithReason(t *testing.T) {
	sm := NewSafeMode(DefaultSafeModeConfig())

	if err := sm.EnableWithReason(context.Background(), "high volatility"); err != nil {
		t.Fatalf("enable safe mode with reason: %v", err)
	}

	if sm.GetReason() != "high volatility" {
		t.Errorf("wrong reason: %s", sm.GetReason())
	}
}

func TestSafeModeMultipliers(t *testing.T) {
	config := SafeModeConfig{
		MaxOrderSizeMultiplier: 0.5,
		MaxLeverageMultiplier:  0.25,
		MaxPositionMultiplier:  0.1,
		RestrictToPaper:        true,
	}
	sm := NewSafeMode(config)

	// When disabled, multipliers should be 1.0
	os, lev, pos := sm.GetMultipliers()
	if os != 1.0 || lev != 1.0 || pos != 1.0 {
		t.Error("multipliers should be 1.0 when disabled")
	}

	// When enabled, should return configured values
	if err := sm.Enable(context.Background()); err != nil {
		t.Fatalf("enable safe mode: %v", err)
	}
	os, lev, pos = sm.GetMultipliers()
	if os != 0.5 || lev != 0.25 || pos != 0.1 {
		t.Errorf("wrong multipliers: os=%v, lev=%v, pos=%v", os, lev, pos)
	}
}

func TestSafeModeRule(t *testing.T) {
	sm := NewSafeMode(DefaultSafeModeConfig())
	rule := NewSafeModeRule(sm)

	// Should approve when not enabled
	intent := ports.OrderIntent{IntentID: "test"}
	decision, err := rule.Evaluate(context.Background(), intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve when safe mode disabled")
	}

	// When enabled, check constraints are applied
	if err := sm.Enable(context.Background()); err != nil {
		t.Fatalf("enable safe mode: %v", err)
	}
	decision, err = rule.Evaluate(context.Background(), intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if decision.Approved {
		t.Error("should reject when trading_mode is missing and safe mode is paper-only")
	}

	paperCtx := WithTradingMode(context.Background(), "paper")
	decision, err = rule.Evaluate(paperCtx, intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve when trading_mode=paper")
	}
}

// ============================================================
// Cooldown Rule Tests
// ============================================================

func TestCooldownRule(t *testing.T) {
	rule := NewCooldownRule(100*time.Millisecond, 2)

	intent := ports.OrderIntent{IntentID: "test"}
	ctx := context.Background()

	// First loss - should still approve
	rule.RecordLoss()
	decision, err := rule.Evaluate(ctx, intent)
	if err != nil {
		t.Fatalf("evaluate cooldown after first loss: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve after 1 loss")
	}

	// Second loss - triggers cooldown
	rule.RecordLoss()
	decision, err = rule.Evaluate(ctx, intent)
	if err != nil {
		t.Fatalf("evaluate cooldown after second loss: %v", err)
	}
	if decision.Approved {
		t.Error("should reject during cooldown")
	}

	// Wait for cooldown
	time.Sleep(150 * time.Millisecond)
	decision, err = rule.Evaluate(ctx, intent)
	if err != nil {
		t.Fatalf("evaluate cooldown after waiting: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve after cooldown")
	}

	// Win resets counter
	rule.RecordLoss()
	rule.RecordWin()
	rule.RecordLoss()
	decision, err = rule.Evaluate(ctx, intent)
	if err != nil {
		t.Fatalf("evaluate cooldown after reset by win: %v", err)
	}
	if !decision.Approved {
		t.Error("should approve - counter reset by win")
	}
}

// ============================================================
// Auto Safe Mode Trigger Tests
// ============================================================

func TestAutoSafeModeOnDrawdown(t *testing.T) {
	sm := NewSafeMode(DefaultSafeModeConfig())
	trigger := NewAutoSafeModeTrigger(sm, 0.1, 3) // 10% drawdown, 3 losses

	if sm.IsEnabled() {
		t.Error("safe mode should start disabled")
	}

	trigger.OnDrawdownUpdate(0.15) // 15% drawdown

	if !sm.IsEnabled() {
		t.Error("safe mode should auto-enable at drawdown limit")
	}
}

func TestAutoSafeModeOnLossStreak(t *testing.T) {
	sm := NewSafeMode(DefaultSafeModeConfig())
	trigger := NewAutoSafeModeTrigger(sm, 0.5, 2) // 50% drawdown, 2 losses

	trigger.OnTradeResult(false) // 1 loss
	if sm.IsEnabled() {
		t.Error("should not trigger after 1 loss")
	}

	trigger.OnTradeResult(false) // 2 losses
	if !sm.IsEnabled() {
		t.Error("should trigger after 2 consecutive losses")
	}
}

func TestAutoSafeModeOnLossStreak_DisabledWhenThresholdNonPositive(t *testing.T) {
	sm := NewSafeMode(DefaultSafeModeConfig())
	trigger := NewAutoSafeModeTrigger(sm, 0.5, 0)

	trigger.OnTradeResult(false)
	trigger.OnTradeResult(false)
	trigger.OnTradeResult(false)

	if sm.IsEnabled() {
		t.Error("safe mode should stay disabled when loss streak trigger is disabled")
	}
}

func TestMinLiquidityRule(t *testing.T) {
	minLiq := decimal.NewFromInt(1000)
	rule := NewMinLiquidityRule(minLiq)
	bid1500 := decimal.NewFromInt(1500)
	bid500 := decimal.NewFromInt(500)
	ask2000 := decimal.NewFromInt(2000)

	tests := []struct {
		name      string
		bidDepth  *decimal.Decimal
		askDepth  *decimal.Decimal
		approved  bool
	}{
		{"both nil — backward compatible", nil, nil, true},
		{"only bid — backward compatible", &bid1500, nil, true},
		{"only ask — backward compatible", nil, &ask2000, true},
		{"min(1500,2000)=1500 >= 1000 — approved", &bid1500, &ask2000, true},
		{"min(500,2000)=500 < 1000 — rejected", &bid500, &ask2000, false},
		{"min(500,1500)=500 < 1000 — rejected", &bid500, &bid1500, false},
		{"min(1000,2000)=1000 >= 1000 — approved at boundary", func() *decimal.Decimal { d := decimal.NewFromInt(1000); return &d }(), &ask2000, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := ports.OrderIntent{
				IntentID: "test",
				Exchange: "binance",
				Symbol:   "BTC/USDT",
				Side:     ports.OrderSideBuy,
				Amount:   decimal.NewFromInt(1),
				Price:    decimal.NewFromInt(50000),
				BidDepth: tt.bidDepth,
				AskDepth: tt.askDepth,
			}
			decision, err := rule.Evaluate(context.Background(), intent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Approved != tt.approved {
				t.Errorf("approved=%v want=%v reason=%s", decision.Approved, tt.approved, decision.Reason)
			}
		})
	}
}

func decimalRequireFromInt(i int64) *decimal.Decimal {
	d := decimal.NewFromInt(i)
	return &d
}

func TestMaxSpreadRule(t *testing.T) {
	maxSpread := decimal.NewFromFloat(0.5)
	rule := NewMaxSpreadRule(maxSpread)
	bid := decimal.NewFromInt(10000)
	ask10010 := decimal.NewFromInt(10010)
	ask10060 := decimal.NewFromInt(10060)
	bidZero := decimal.Zero

	tests := []struct {
		name     string
		bestBid  *decimal.Decimal
		bestAsk  *decimal.Decimal
		approved bool
	}{
		{"both nil — backward compatible", nil, nil, true},
		{"only bid — backward compatible", &bid, nil, true},
		{"only ask — backward compatible", nil, &ask10010, true},
		{"BestBid is zero — div-by-zero guard", &bidZero, &ask10010, true},
		{"spread=0.10% <= 0.5% — approved", &bid, &ask10010, true},
		{"spread=0.60% > 0.5% — rejected", &bid, &ask10060, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent := ports.OrderIntent{
				IntentID: "test",
				Exchange: "binance",
				Symbol:   "BTC/USDT",
				Side:     ports.OrderSideBuy,
				Amount:   decimal.NewFromInt(1),
				Price:    decimal.NewFromInt(50000),
				BestBid:  tt.bestBid,
				BestAsk:  tt.bestAsk,
			}
			decision, err := rule.Evaluate(context.Background(), intent)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if decision.Approved != tt.approved {
				t.Errorf("approved=%v want=%v reason=%s", decision.Approved, tt.approved, decision.Reason)
			}
		})
	}
}
