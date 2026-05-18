package services

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
)

func TestMaxDrawdownConfig_Defaults(t *testing.T) {
	config := DefaultMaxDrawdownConfig()

	if !config.WarningThreshold.Equal(decimal.NewFromFloat(0.05)) {
		t.Errorf("expected WarningThreshold to be 0.05, got %s", config.WarningThreshold)
	}

	if !config.HaltThreshold.Equal(decimal.NewFromFloat(0.15)) {
		t.Errorf("expected HaltThreshold to be 0.15, got %s", config.HaltThreshold)
	}
}

func TestMaxDrawdownHalt_NewHalt(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	if halt == nil {
		t.Fatal("expected halt to not be nil")
	}
}

func TestMaxDrawdownHalt_CheckDrawdown_Normal(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	state, err := halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if state.Status != DrawdownStatusNormal {
		t.Errorf("expected normal status, got %s", state.Status)
	}

	if state.TradingHalted {
		t.Error("expected trading to not be halted")
	}
}

func TestMaxDrawdownHalt_CheckDrawdown_Warning(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	state, _ := halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(940))

	if state.Status != DrawdownStatusWarning {
		t.Errorf("expected warning status, got %s", state.Status)
	}
}

func TestMaxDrawdownHalt_CheckDrawdown_Halt(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	state, _ := halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(800))

	if state.Status != DrawdownStatusHalted {
		t.Errorf("expected halted status, got %s", state.Status)
	}

	if !state.TradingHalted {
		t.Error("expected trading to be halted")
	}
}

func TestMaxDrawdownHalt_IsTradingHalted(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	if halt.IsTradingHalted("nonexistent") {
		t.Error("expected nonexistent chat to not be halted")
	}

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(800))

	if !halt.IsTradingHalted("chat-1") {
		t.Error("expected chat-1 to be halted after 20% drawdown")
	}
}

func TestMaxDrawdownHalt_ShouldAllowTrade(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	if !halt.ShouldAllowTrade("nonexistent") {
		t.Error("expected to allow trade for nonexistent chat")
	}

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(800))

	if halt.ShouldAllowTrade("chat-1") {
		t.Error("expected to not allow trade for halted chat")
	}
}

func TestMaxDrawdownHalt_ForceHalt(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	err := halt.ForceHalt(context.Background(), "chat-1", "manual halt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !halt.IsTradingHalted("chat-1") {
		t.Error("expected chat-1 to be halted")
	}
}

func TestMaxDrawdownHalt_ResumeTrading(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	state, err := halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	if err != nil {
		t.Fatalf("unexpected error on initial CheckDrawdown: %v", err)
	}
	if state.TradingHalted {
		t.Error("expected trading to not be halted at initial peak")
	}

	state, err = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(800))
	if err != nil {
		t.Fatalf("unexpected error on drawdown CheckDrawdown: %v", err)
	}
	if !state.TradingHalted {
		t.Error("expected trading to be halted after drawdown before resume")
	}

	err = halt.ResumeTrading(context.Background(), "chat-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if halt.IsTradingHalted("chat-1") {
		t.Error("expected chat-1 to not be halted after resume")
	}

	state, exists := halt.GetState("chat-1")
	if !exists {
		t.Fatal("expected drawdown state after resume")
	}
	if !state.PeakValue.Equal(decimal.NewFromInt(800)) {
		t.Errorf("expected active peak reset to current value 800, got %s", state.PeakValue)
	}
	if !state.CurrentDrawdown.IsZero() {
		t.Errorf("expected active drawdown reset to zero, got %s", state.CurrentDrawdown)
	}
	if !state.MaxDrawdownSeen.Equal(decimal.RequireFromString("0.2")) {
		t.Errorf("expected historical max drawdown to remain 0.2, got %s", state.MaxDrawdownSeen)
	}

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(800))
	if halt.IsTradingHalted("chat-1") {
		t.Error("expected resumed chat not to immediately re-halt at the same current value")
	}
}

func TestMaxDrawdownHalt_ResumeTrading_NotHalted(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))

	err := halt.ResumeTrading(context.Background(), "chat-1")
	if err == nil {
		t.Error("expected error when resuming non-halted chat")
	}
}

func TestMaxDrawdownHalt_GetMetrics(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(800))

	metrics := halt.GetMetrics()

	if metrics.HaltEvents != 1 {
		t.Errorf("expected 1 halt event, got %d", metrics.HaltEvents)
	}
}

func TestMaxDrawdownHalt_ResetPeak(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(800))

	_ = halt.ResetPeak(context.Background(), "chat-1", decimal.NewFromInt(900))

	state, _ := halt.GetState("chat-1")
	if !state.PeakValue.Equal(decimal.NewFromInt(900)) {
		t.Errorf("expected peak to be 900, got %s", state.PeakValue)
	}
}

func TestMaxDrawdownHalt_ForceResumeAllResetsActiveBaseline(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	state, err := halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	if err != nil {
		t.Fatalf("unexpected error on initial CheckDrawdown: %v", err)
	}
	if state.TradingHalted {
		t.Error("expected chat-1 to not be halted at initial peak")
	}
	state, err = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(800))
	if err != nil {
		t.Fatalf("unexpected error on drawdown CheckDrawdown: %v", err)
	}
	if !state.TradingHalted {
		t.Error("expected chat-1 to be halted before force resume")
	}
	state, err = halt.CheckDrawdown(context.Background(), "chat-2", decimal.NewFromInt(500))
	if err != nil {
		t.Fatalf("unexpected error on chat-2 CheckDrawdown: %v", err)
	}
	if state.TradingHalted {
		t.Error("expected chat-2 to not be halted")
	}

	resumed := halt.ForceResumeAll(context.Background())
	if len(resumed) != 1 || resumed[0] != "chat-1" {
		t.Fatalf("expected only chat-1 to resume, got %#v", resumed)
	}
	if halt.IsTradingHalted("chat-1") {
		t.Fatal("expected chat-1 to be resumed")
	}

	state, exists := halt.GetState("chat-1")
	if !exists {
		t.Fatal("expected drawdown state for chat-1")
	}
	if !state.PeakValue.Equal(decimal.NewFromInt(800)) {
		t.Errorf("expected active peak reset to current value 800, got %s", state.PeakValue)
	}
	if !state.CurrentDrawdown.IsZero() {
		t.Errorf("expected active drawdown reset to zero, got %s", state.CurrentDrawdown)
	}
	if !state.MaxDrawdownSeen.Equal(decimal.RequireFromString("0.2")) {
		t.Errorf("expected historical max drawdown to remain 0.2, got %s", state.MaxDrawdownSeen)
	}

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(800))
	if halt.IsTradingHalted("chat-1") {
		t.Error("expected force-resumed chat not to immediately re-halt at the same current value")
	}
}

func TestMaxDrawdownHalt_ForceHaltFreshAccountResumeInitializesBaseline(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	err := halt.ForceHalt(context.Background(), "chat-fresh", "manual")
	if err != nil {
		t.Fatalf("unexpected ForceHalt error: %v", err)
	}

	err = halt.ResumeTrading(context.Background(), "chat-fresh")
	if err != nil {
		t.Fatalf("unexpected ResumeTrading error: %v", err)
	}

	state, err := halt.CheckDrawdown(context.Background(), "chat-fresh", decimal.NewFromInt(1000))
	if err != nil {
		t.Fatalf("unexpected CheckDrawdown error after fresh resume: %v", err)
	}
	if halt.IsTradingHalted("chat-fresh") {
		t.Error("expected fresh-resumed account not to re-halt on first check")
	}
	if !state.CurrentDrawdown.IsZero() {
		t.Errorf("expected zero drawdown after fresh resume, got %s", state.CurrentDrawdown)
	}
	if !state.PeakValue.Equal(decimal.NewFromInt(1000)) {
		t.Errorf("expected peak initialized to 1000, got %s", state.PeakValue)
	}
	if !state.CurrentValue.Equal(decimal.NewFromInt(1000)) {
		t.Errorf("expected current value initialized to 1000, got %s", state.CurrentValue)
	}
}

func TestMaxDrawdownHalt_CalculateDrawdown(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	tests := []struct {
		peak     decimal.Decimal
		current  decimal.Decimal
		expected float64
	}{
		{decimal.NewFromInt(1000), decimal.NewFromInt(900), 0.1},
		{decimal.NewFromInt(1000), decimal.NewFromInt(1000), 0.0},
		{decimal.NewFromInt(1000), decimal.NewFromInt(800), 0.2},
	}

	for _, tt := range tests {
		result := halt.CalculateDrawdown(tt.peak, tt.current)
		if !result.Equal(decimal.NewFromFloat(tt.expected)) {
			t.Errorf("CalculateDrawdown(%s, %s) = %s, expected %f", tt.peak, tt.current, result, tt.expected)
		}
	}
}

func TestMaxDrawdownHalt_GetStatusSummary(t *testing.T) {
	halt := NewMaxDrawdownHalt(nil, DefaultMaxDrawdownConfig())

	_, _ = halt.CheckDrawdown(context.Background(), "chat-1", decimal.NewFromInt(1000))
	_, _ = halt.CheckDrawdown(context.Background(), "chat-2", decimal.NewFromInt(1000))
	_, _ = halt.CheckDrawdown(context.Background(), "chat-2", decimal.NewFromInt(800))

	summary := halt.GetStatusSummary()

	total, ok := summary["total_accounts"].(int)
	if !ok || total != 2 {
		t.Errorf("expected 2 total accounts, got %v", summary["total_accounts"])
	}
}
