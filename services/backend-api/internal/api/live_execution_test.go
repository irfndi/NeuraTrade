package api

import (
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

func TestOrderResultFromObservedOrderUsesExchangeFill(t *testing.T) {
	observed := ccxt.Order{
		ID:            "exchange-123",
		ClientOrderID: "client-123",
		Symbol:        "BTC/USDT",
		Type:          "market",
		Side:          "buy",
		Status:        "closed",
		Price:         decimal.RequireFromString("100.000000000000000001"),
		Amount:        decimal.RequireFromString("0.123456789012345678"),
		Filled:        decimal.RequireFromString("0.123456789012345677"),
		Cost:          decimal.RequireFromString("12.3456789012345677"),
		CreatedAt:     time.Unix(1710000000, 0).UTC(),
	}

	result := orderResultFromObservedOrder("bitget-futures", "BTC/USDT", ports.OrderSideBuy, ports.OrderTypeMarket, observed)

	if result.OrderID != "exchange-123" || result.ClientID != "client-123" {
		t.Fatalf("unexpected identity: %+v", result)
	}
	if !result.Filled.Equal(observed.Filled) || !result.AveragePrice.Equal(observed.Cost.Div(observed.Filled)) {
		t.Fatalf("exchange fill was not preserved: %+v", result)
	}
	if result.Status != ports.OrderStatusPartial {
		t.Fatalf("expected partial status for an incomplete closed order, got %q", result.Status)
	}
}

func TestOrderResultFromObservedOrderDoesNotInventFill(t *testing.T) {
	observed := ccxt.Order{
		ID:     "exchange-open",
		Symbol: "BTC/USDT",
		Type:   "market",
		Side:   "sell",
		Status: "open",
		Amount: decimal.RequireFromString("2"),
		Price:  decimal.RequireFromString("100"),
		Filled: decimal.Zero,
		Cost:   decimal.Zero,
	}

	result := orderResultFromObservedOrder("bitget-futures", "BTC/USDT", ports.OrderSideSell, ports.OrderTypeMarket, observed)

	if !result.Filled.IsZero() || !result.AveragePrice.IsZero() {
		t.Fatalf("open order must not be represented as filled: %+v", result)
	}
	if result.Status != ports.OrderStatusOpen {
		t.Fatalf("expected open status, got %q", result.Status)
	}
}
