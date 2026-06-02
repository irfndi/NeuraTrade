package handlers

import (
	"context"

	"github.com/irfndi/neuratrade/internal/ccxt"
)

// StopLossLiquidator is an ExchangeLiquidator that calls the exchange
// via ccxt.CCXTService.CancelOrder. When no orderID is present (nil or
// empty) the liquidator returns nil — there is nothing to cancel.
//
// For positions where the order has already been filled (no open order
// on the exchange), CancelOrder is a no-op that returns nil. A true
// market-order close requires CCXTService.CreateOrder or equivalent,
// which is not yet exposed on the CCXTService interface.
type StopLossLiquidator struct {
	ccxtService ccxt.CCXTService
}

func NewStopLossLiquidator(svc ccxt.CCXTService) *StopLossLiquidator {
	return &StopLossLiquidator{ccxtService: svc}
}

func (l *StopLossLiquidator) ClosePosition(ctx context.Context, exchangeID, orderID, positionID, symbol string) error {
	if orderID == "" {
		return nil
	}
	return l.ccxtService.CancelOrder(ctx, exchangeID, orderID, symbol)
}
