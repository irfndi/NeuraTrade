package handlers

import (
	"context"
	"errors"
	"fmt"

	"github.com/irfndi/neuratrade/internal/app/risk"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/services"
)

type collectorAdapter struct {
	service *services.CollectorService
}

// NewCollectorController wraps a CollectorService behind the CollectorController
// interface. Returns nil when svc is nil so that handler nil-guards (503) fire
// correctly instead of falling through to a generic 500.
func NewCollectorController(svc *services.CollectorService) CollectorController {
	if svc == nil {
		return nil
	}
	return &collectorAdapter{service: svc}
}

func (a *collectorAdapter) PauseExchange(exchangeID string) error {
	return a.service.PauseExchange(exchangeID)
}

func (a *collectorAdapter) ResumeExchange(exchangeID string) error {
	return a.service.ResumeExchange(exchangeID)
}

type riskAdapter struct {
	killSwitch *risk.KillSwitchImpl
	safeMode   *risk.SafeModeImpl
}

// NewRiskControllerAdapter creates a RiskController backed by the given
// KillSwitch and SafeMode. Returns nil when either dependency is nil so that
// handler nil-guards produce proper 503 responses — consistent with
// NewCollectorController and NewOrderController.
func NewRiskControllerAdapter(killSwitch *risk.KillSwitchImpl, safeMode *risk.SafeModeImpl) RiskController {
	if killSwitch == nil || safeMode == nil {
		return nil
	}
	return &riskAdapter{
		killSwitch: killSwitch,
		safeMode:   safeMode,
	}
}

func (a *riskAdapter) EnableSafeMode(ctx context.Context, reason string) error {
	return a.safeMode.EnableWithReason(ctx, reason)
}

func (a *riskAdapter) DisableSafeMode(ctx context.Context) error {
	return a.safeMode.Disable(ctx)
}

func (a *riskAdapter) EngageKillSwitch(ctx context.Context, reason string) error {
	return a.killSwitch.Engage(ctx, reason)
}

func (a *riskAdapter) DisengageKillSwitch(ctx context.Context) error {
	return a.killSwitch.Disengage(ctx)
}

type orderAdapter struct {
	ccxtService ccxt.CCXTService
}

// NewOrderController wraps a CCXTService behind the OrderController interface.
// Returns nil when ccxtSvc is nil so that handler nil-guards (503) fire correctly.
func NewOrderController(ccxtSvc ccxt.CCXTService) OrderController {
	if ccxtSvc == nil {
		return nil
	}
	return &orderAdapter{ccxtService: ccxtSvc}
}

func (a *orderAdapter) CancelAllOrders(ctx context.Context, exchange, symbol string) error {
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

func (a *orderAdapter) cancelExchangeOrders(ctx context.Context, exchange, symbol string) error {
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
