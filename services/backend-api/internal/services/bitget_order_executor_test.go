package services

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestBitgetOrderExecutor_PlaceOrderWithDetails_Validation(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	tests := []struct {
		name        string
		details     TradeDetails
		wantErr     bool
		errContains string
	}{
		{
			name: "zero amount",
			details: TradeDetails{
				Symbol:     "BTCUSDT",
				Side:       "buy",
				AmountUSDT: decimal.Zero,
			},
			wantErr:     true,
			errContains: "invalid order amount",
		},
		{
			name: "negative amount",
			details: TradeDetails{
				Symbol:     "BTCUSDT",
				Side:       "buy",
				AmountUSDT: decimal.NewFromFloat(-10),
			},
			wantErr:     true,
			errContains: "invalid order amount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.PlaceOrderWithDetails(context.Background(), tt.details)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBitgetOrderExecutor_Sign(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	// Test that sign produces a base64 encoded HMAC-SHA256 signature
	signature := executor.sign("test message")
	assert.NotEmpty(t, signature)
	// Signature should be base64 encoded
	assert.Regexp(t, "^[A-Za-z0-9+/]+=*$", signature)
}

func TestBitgetOrderExecutor_SetNotificationService(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	// Should not panic when setting nil service
	executor.SetNotificationService(nil)

	// Should not panic when setting a service
	ns := &NotificationService{}
	executor.SetNotificationService(ns)
}

func TestBitgetOrderExecutor_SetChatID(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	executor.SetChatID("123456789")
	assert.Equal(t, "123456789", executor.chatID)
}

func TestBitgetOrderExecutor_SetWalletBalance(t *testing.T) {
	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")

	executor.SetWalletBalance(1000.0)
	assert.Equal(t, 1000.0, executor.walletBalance)
}

func TestContractInfo(t *testing.T) {
	info := &ContractInfo{
		SizeMultiplier: decimal.NewFromFloat(0.1),
		MinTradeNum:    decimal.NewFromInt(1),
		VolumePlace:    0,
	}

	assert.Equal(t, "0.1", info.SizeMultiplier.String())
	assert.Equal(t, "1", info.MinTradeNum.String())
	assert.Equal(t, 0, info.VolumePlace)
}

func TestTradeDetails(t *testing.T) {
	tp := decimal.NewFromFloat(50000)
	sl := decimal.NewFromFloat(45000)
	entry := decimal.NewFromFloat(48000)

	details := TradeDetails{
		Exchange:      "bitget",
		Symbol:        "BTC/USDT",
		Side:          "buy",
		OrderType:     "market",
		MarketType:    "futures",
		Leverage:      5,
		AmountUSDT:    decimal.NewFromFloat(100),
		WalletPercent: 5.0,
		EntryPrice:    &entry,
		TakeProfit:    &tp,
		StopLoss:      &sl,
		TradeType:     "scalping",
		Confidence:    0.85,
		Reasoning:     "Strong bullish momentum",
		IsPaperTrade:  false,
	}

	assert.Equal(t, "bitget", details.Exchange)
	assert.Equal(t, "BTC/USDT", details.Symbol)
	assert.Equal(t, "buy", details.Side)
	assert.Equal(t, "market", details.OrderType)
	assert.Equal(t, "futures", details.MarketType)
	assert.Equal(t, 5, details.Leverage)
	assert.Equal(t, "100", details.AmountUSDT.String())
	assert.Equal(t, 5.0, details.WalletPercent)
	assert.Equal(t, "48000", details.EntryPrice.String())
	assert.Equal(t, "50000", details.TakeProfit.String())
	assert.Equal(t, "45000", details.StopLoss.String())
	assert.Equal(t, "scalping", details.TradeType)
	assert.Equal(t, 0.85, details.Confidence)
	assert.Equal(t, "Strong bullish momentum", details.Reasoning)
	assert.False(t, details.IsPaperTrade)
}

func TestFormatPrice(t *testing.T) {
	tests := []struct {
		price    decimal.Decimal
		expected string
	}{
		{decimal.NewFromFloat(65000), "$65000.00"},
		{decimal.NewFromFloat(1.5234), "$1.5234"},
		{decimal.NewFromFloat(0.00012345), "$0.000123"},
		{decimal.NewFromFloat(0.0001), "$0.000100"},
		{decimal.NewFromFloat(1000.5), "$1000.50"},
	}

	for _, tt := range tests {
		result := formatPrice(tt.price)
		assert.Equal(t, tt.expected, result, "Price: %s", tt.price.String())
	}
}
