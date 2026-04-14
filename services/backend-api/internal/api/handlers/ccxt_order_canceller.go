package handlers

import (
	"context"
	"fmt"

	"github.com/irfndi/neuratrade/internal/ccxt"
)

type ccxtOrderCancellerAdapter struct {
	ccxtService ccxt.CCXTService
}

func NewCCXTOrderCanceller(ccxtService ccxt.CCXTService) OrderCanceller {
	return &ccxtOrderCancellerAdapter{ccxtService: ccxtService}
}

func (a *ccxtOrderCancellerAdapter) CancelAllOrders(ctx context.Context, exchange, symbol string) error {
	resp, err := a.ccxtService.FetchOpenOrders(ctx, exchange)
	if err != nil {
		return fmt.Errorf("failed to fetch open orders for %s: %w", exchange, err)
	}

	cancelled := 0
	for _, order := range resp.Orders {
		orderSymbol := order.Symbol
		if symbol != "" && orderSymbol != symbol {
			continue
		}
		if err := a.ccxtService.CancelOrder(ctx, exchange, order.ID, orderSymbol); err != nil {
			return fmt.Errorf("failed to cancel order %s on %s: %w", order.ID, exchange, err)
		}
		cancelled++
	}

	if symbol == "" && cancelled == 0 {
		return nil
	}

	return nil
}
