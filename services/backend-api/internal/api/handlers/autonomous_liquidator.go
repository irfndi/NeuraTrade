package handlers

import "context"

// ExchangeLiquidator closes positions on a remote exchange.
// When nil (not wired), liquidate handlers perform DB-only bookkeeping
// (paper mode / no exchange gateway). When wired, ClosePosition is
// called before the DB row is marked LIQUIDATED.
type ExchangeLiquidator interface {
	ClosePosition(ctx context.Context, exchangeID, orderID, positionID string) error
}
