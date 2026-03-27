package services

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubModeExecutor struct {
	orderID string
	err     error
	calls   int
}

func (s *stubModeExecutor) PlaceOrder(context.Context, string, string, string, string, decimal.Decimal, *decimal.Decimal) (string, error) {
	s.calls++
	return s.orderID, s.err
}

func (s *stubModeExecutor) PlaceOrderWithDetails(context.Context, TradeDetails) (string, error) {
	s.calls++
	return s.orderID, s.err
}

func (s *stubModeExecutor) GetOpenOrders(context.Context, string, string) ([]map[string]interface{}, error) {
	s.calls++
	return nil, s.err
}

func (s *stubModeExecutor) GetClosedOrders(context.Context, string, string, int) ([]map[string]interface{}, error) {
	s.calls++
	return nil, s.err
}

func (s *stubModeExecutor) CancelOrder(context.Context, string, string) error {
	s.calls++
	return s.err
}

func (s *stubModeExecutor) IsPaperTrading() bool { return false }

func TestModeAwareOrderExecutor_RoutesByOperationalMode(t *testing.T) {
	liveExec := &stubModeExecutor{orderID: "live-1"}
	paperExec := &stubModeExecutor{orderID: "paper-1"}
	exec := NewModeAwareOrderExecutor(liveExec, paperExec, nil)

	liveCtx := WithOperationalMode(context.Background(), OpModeLive)
	orderID, err := exec.PlaceOrderWithDetails(liveCtx, TradeDetails{Symbol: "BTC/USDT"})
	require.NoError(t, err)
	assert.Equal(t, "live-1", orderID)
	assert.Equal(t, 1, liveExec.calls)
	assert.Zero(t, paperExec.calls)

	paperCtx := WithOperationalMode(context.Background(), ModePaper)
	orderID, err = exec.PlaceOrderWithDetails(paperCtx, TradeDetails{Symbol: "BTC/USDT"})
	require.NoError(t, err)
	assert.Equal(t, "paper-1", orderID)
	assert.Equal(t, 1, paperExec.calls)
}

func TestModeAwareOrderExecutor_BlocksLiveModeWithoutLiveExecutor(t *testing.T) {
	exec := NewModeAwareOrderExecutor(nil, &stubModeExecutor{orderID: "paper-1"}, nil)
	_, err := exec.PlaceOrderWithDetails(WithOperationalMode(context.Background(), OpModeLive), TradeDetails{Symbol: "BTC/USDT"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "live mode selected but real order execution is unavailable")
}

func TestPaperTradeFlagForContext_PrefersOperationalMode(t *testing.T) {
	assert.False(t, paperTradeFlagForContext(WithOperationalMode(context.Background(), OpModeLive), true))
	assert.True(t, paperTradeFlagForContext(WithOperationalMode(context.Background(), ModePaper), false))
	assert.True(t, paperTradeFlagForContext(context.Background(), true))
	assert.False(t, paperTradeFlagForContext(context.Background(), false))
}

func TestModeAwareOrderExecutor_UsesOperationalModeServiceWhenContextMissing(t *testing.T) {
	liveExec := &stubModeExecutor{orderID: "live-1"}
	paperExec := &stubModeExecutor{orderID: "paper-1"}
	opMode := &OperationalModeService{config: DefaultOperationalModeConfig(), states: map[string]*OperationalModeState{
		"chat-1": {ChatID: "chat-1", Mode: ModePaper},
	}}
	exec := NewModeAwareOrderExecutor(liveExec, paperExec, opMode)
	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{ChatID: "chat-1"})
	orderID, err := exec.PlaceOrderWithDetails(ctx, TradeDetails{Symbol: "BTC/USDT"})
	require.NoError(t, err)
	assert.Equal(t, "paper-1", orderID)
	assert.Equal(t, 1, paperExec.calls)
	assert.Zero(t, liveExec.calls)
}

func TestModeAwareOrderExecutor_PropagatesDelegateErrors(t *testing.T) {
	expected := errors.New("paper failed")
	exec := NewModeAwareOrderExecutor(nil, &stubModeExecutor{err: expected}, nil)
	_, err := exec.PlaceOrderWithDetails(WithOperationalMode(context.Background(), ModePaper), TradeDetails{})
	require.Error(t, err)
	assert.ErrorIs(t, err, expected)
}
