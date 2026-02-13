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

	_ = halt.ForceHalt(context.Background(), "chat-1", "test")

	err := halt.ResumeTrading(context.Background(), "chat-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if halt.IsTradingHalted("chat-1") {
		t.Error("expected chat-1 to not be halted after resume")
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
