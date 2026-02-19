package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockExecutor struct {
	orderID    string
	failCount  int
	filledAmt  decimal.Decimal
	fillPrice  decimal.Decimal
	callCount  int
	shouldFail bool
	failErr    error
}

func (m *mockExecutor) PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	m.callCount++
	if m.shouldFail && m.callCount <= m.failCount {
		return "", m.failErr
	}
	return m.orderID, nil
}

func (m *mockExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	if m.filledAmt.IsZero() {
		return nil, nil
	}
	return []map[string]interface{}{
		{
			"id":      m.orderID,
			"filled":  m.filledAmt.InexactFloat64(),
			"average": m.fillPrice.InexactFloat64(),
			"status":  "closed",
		},
	}, nil
}

func (m *mockExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	if m.filledAmt.IsZero() {
		return nil, nil
	}
	return []map[string]interface{}{
		{
			"id":       m.orderID,
			"filled":   m.filledAmt.InexactFloat64(),
			"average":  m.fillPrice.InexactFloat64(),
			"price":    m.fillPrice.InexactFloat64(),
			"status":   "closed",
			"symbol":   symbol,
			"exchange": exchange,
		},
	}, nil
}

func (m *mockExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	return nil
}

func TestNewSmartOrderExecutor_Defaults(t *testing.T) {
	cfg := DefaultSmartOrderExecutorConfig()
	assert.Equal(t, 4, cfg.MaxRetries)
	assert.Equal(t, 1*time.Second, cfg.InitialDelay)
	assert.Equal(t, 8*time.Second, cfg.MaxDelay)
	assert.Equal(t, 2.0, cfg.BackoffFactor)
	assert.Equal(t, 0.5, cfg.MaxSlippagePercent)
	assert.Equal(t, 30*time.Second, cfg.DefaultTimeout)
	assert.Equal(t, 50.0, cfg.MinPartialFillPercent)
}

func TestSmartOrderExecutor_Validation(t *testing.T) {
	executor := NewSmartOrderExecutor(SmartOrderExecutorConfig{
		BaseExecutor: &mockExecutor{orderID: "test-123"},
	})

	tests := []struct {
		name    string
		req     SmartOrderRequest
		wantErr string
	}{
		{
			name: "missing exchange",
			req: SmartOrderRequest{
				Symbol: "BTC/USDT",
				Side:   "buy",
				Amount: decimal.NewFromFloat(1),
			},
			wantErr: "exchange is required",
		},
		{
			name: "missing symbol",
			req: SmartOrderRequest{
				Exchange: "binance",
				Side:     "buy",
				Amount:   decimal.NewFromFloat(1),
			},
			wantErr: "symbol is required",
		},
		{
			name: "missing side",
			req: SmartOrderRequest{
				Exchange: "binance",
				Symbol:   "BTC/USDT",
				Amount:   decimal.NewFromFloat(1),
			},
			wantErr: "side is required",
		},
		{
			name: "zero amount",
			req: SmartOrderRequest{
				Exchange: "binance",
				Symbol:   "BTC/USDT",
				Side:     "buy",
				Amount:   decimal.Zero,
			},
			wantErr: "amount must be greater than zero",
		},
		{
			name: "negative amount",
			req: SmartOrderRequest{
				Exchange: "binance",
				Symbol:   "BTC/USDT",
				Side:     "buy",
				Amount:   decimal.NewFromFloat(-1),
			},
			wantErr: "amount must be greater than zero",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.PlaceOrderSmart(context.Background(), tt.req)
			require.Error(t, err)
			assert.Equal(t, tt.wantErr, err.Error())
		})
	}
}

func TestSmartOrderExecutor_CalcBackoff(t *testing.T) {
	executor := NewSmartOrderExecutor(SmartOrderExecutorConfig{
		InitialDelay:  1 * time.Second,
		MaxDelay:      8 * time.Second,
		BackoffFactor: 2.0,
	})

	delays := []time.Duration{
		executor.calcBackoff(1),
		executor.calcBackoff(2),
		executor.calcBackoff(3),
		executor.calcBackoff(4),
		executor.calcBackoff(5),
	}

	assert.Equal(t, 1*time.Second, delays[0])
	assert.Equal(t, 2*time.Second, delays[1])
	assert.Equal(t, 4*time.Second, delays[2])
	assert.Equal(t, 8*time.Second, delays[3])
	assert.Equal(t, 8*time.Second, delays[4])
}

func TestSmartOrderExecutor_CalcSlippage(t *testing.T) {
	executor := NewSmartOrderExecutor(SmartOrderExecutorConfig{})

	price100 := decimal.NewFromFloat(100)
	req := SmartOrderRequest{
		Price: &price100,
	}

	slippage := executor.calcSlippage(req, decimal.NewFromFloat(101))
	assert.Equal(t, 1.0, slippage)

	slippage = executor.calcSlippage(req, decimal.NewFromFloat(99))
	assert.Equal(t, 1.0, slippage)

	slippage = executor.calcSlippage(req, decimal.NewFromFloat(100))
	assert.Equal(t, 0.0, slippage)

	var zeroPrice *decimal.Decimal
	req.Price = zeroPrice
	slippage = executor.calcSlippage(req, decimal.NewFromFloat(101))
	assert.Equal(t, 0.0, slippage)
}

func TestSmartOrderExecutor_IsRetryableError(t *testing.T) {
	executor := NewSmartOrderExecutor(SmartOrderExecutorConfig{})

	tests := []struct {
		name      string
		err       error
		wantRetry bool
	}{
		{
			name:      "nil error",
			err:       nil,
			wantRetry: false,
		},
		{
			name:      "connection refused",
			err:       errors.New("connection refused"),
			wantRetry: true,
		},
		{
			name:      "timeout",
			err:       errors.New("timeout waiting for response"),
			wantRetry: true,
		},
		{
			name:      "rate limit",
			err:       errors.New("rate limit exceeded (429)"),
			wantRetry: true,
		},
		{
			name:      "service unavailable",
			err:       errors.New("service unavailable (503)"),
			wantRetry: true,
		},
		{
			name:      "invalid order",
			err:       errors.New("invalid order parameters"),
			wantRetry: false,
		},
		{
			name:      "insufficient balance",
			err:       errors.New("insufficient balance"),
			wantRetry: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := executor.isRetryableError(tt.err)
			assert.Equal(t, tt.wantRetry, got)
		})
	}
}

func TestSmartOrderExecutor_PlaceOrder_RetriesOnFailure(t *testing.T) {
	mock := &mockExecutor{
		orderID:    "order-123",
		shouldFail: true,
		failCount:  2,
		failErr:    errors.New("connection refused"),
	}

	executor := NewSmartOrderExecutor(SmartOrderExecutorConfig{
		BaseExecutor:  mock,
		MaxRetries:    3,
		InitialDelay:  1 * time.Millisecond,
		MaxDelay:      1 * time.Millisecond,
		BackoffFactor: 2.0,
	})

	price := decimal.NewFromFloat(100)
	orderID, err := executor.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "limit", decimal.NewFromFloat(1), &price)

	require.NoError(t, err)
	assert.Equal(t, "order-123", orderID)
	assert.Equal(t, 3, mock.callCount)
}

func TestSmartOrderExecutor_PlaceOrder_MaxRetriesExceeded(t *testing.T) {
	mock := &mockExecutor{
		orderID:    "order-123",
		shouldFail: true,
		failCount:  10,
		failErr:    errors.New("connection refused"),
	}

	executor := NewSmartOrderExecutor(SmartOrderExecutorConfig{
		BaseExecutor: mock,
		MaxRetries:   3,
		InitialDelay: 1 * time.Millisecond,
		MaxDelay:     1 * time.Millisecond,
	})

	price := decimal.NewFromFloat(100)
	_, err := executor.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "limit", decimal.NewFromFloat(1), &price)

	require.Error(t, err)
	assert.Equal(t, 3, mock.callCount)
}
