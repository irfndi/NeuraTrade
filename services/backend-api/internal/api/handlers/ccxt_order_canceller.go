package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/irfndi/neuratrade/internal/ccxt"
)

type ccxtOrderCancellerAdapter struct {
	ccxtService ccxt.CCXTService
}

// NewCCXTOrderCanceller wraps a CCXTService behind the OrderCanceller interface.
// Returns nil when ccxtService is nil so that handler nil-guards (503) fire correctly.
func NewCCXTOrderCanceller(ccxtService ccxt.CCXTService) OrderCanceller {
	if ccxtService == nil {
		return nil
	}
	return &ccxtOrderCancellerAdapter{ccxtService: ccxtService}
}

func (a *ccxtOrderCancellerAdapter) CancelAllOrders(ctx context.Context, exchange, symbol string) error {
	if exchange != "" {
		return a.cancelExchangeOrders(ctx, exchange, symbol)
	}

	exchanges := a.ccxtService.GetSupportedExchanges()
	if len(exchanges) == 0 {
		return errors.New("no supported exchanges available")
	}

	var errs []error
	for _, ex := range exchanges {
		if err := ctx.Err(); err != nil {
			errs = append(errs, fmt.Errorf("cancelled while iterating exchanges: %w", err))
			break
		}
		if err := a.cancelExchangeOrders(ctx, ex, symbol); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func (a *ccxtOrderCancellerAdapter) cancelExchangeOrders(ctx context.Context, exchange, symbol string) error {
	var (
		resp *ccxt.OpenOrdersResponse
		err  error
	)

	if symbol != "" {
		resp, err = a.ccxtService.FetchOpenOrdersForSymbol(ctx, exchange, symbol)
	} else {
		resp, err = a.ccxtService.FetchOpenOrders(ctx, exchange)
	}
	if err != nil {
		return fmt.Errorf("fetch open orders for %s failed: %w", exchange, err)
	}

	if resp == nil || len(resp.Orders) == 0 {
		return nil
	}

	var cancelErrs []error
	for _, order := range resp.Orders {
		if err := ctx.Err(); err != nil {
			cancelErrs = append(cancelErrs, fmt.Errorf("cancellation aborted: %w", err))
			break
		}
		if err := a.ccxtService.CancelOrder(ctx, exchange, order.ID, order.Symbol); err != nil {
			cancelErrs = append(cancelErrs, fmt.Errorf("cancel order %s on %s failed: %w", order.ID, exchange, err))
		}
	}

	if len(cancelErrs) > 0 {
		return errors.Join(cancelErrs...)
	}

	return nil
}
