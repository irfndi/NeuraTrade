package services

import (
	"context"
	"fmt"
	"strings"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/shopspring/decimal"
)

type operationalModeContextKey struct{}

func WithOperationalMode(ctx context.Context, mode OperationalMode) context.Context {
	return context.WithValue(ctx, operationalModeContextKey{}, mode)
}

func operationalModeFromContext(ctx context.Context) (OperationalMode, bool) {
	mode, ok := ctx.Value(operationalModeContextKey{}).(OperationalMode)
	if !ok {
		return "", false
	}
	return normalizeOperationalMode(mode), true
}

func normalizeOperationalMode(mode OperationalMode) OperationalMode {
	switch mode {
	case OpModeLive:
		return OpModeLive
	case ModePaper:
		return ModePaper
	default:
		return OpModeDry
	}
}

func paperTradeFlagForContext(ctx context.Context, fallback bool) bool {
	if mode, ok := operationalModeFromContext(ctx); ok {
		return mode != OpModeLive
	}
	return fallback
}

type ModeAwareOrderExecutor struct {
	liveExecutor  ScalpingOrderExecutor
	paperExecutor ScalpingOrderExecutor
	opModeService *OperationalModeService
}

func NewModeAwareOrderExecutor(liveExecutor, paperExecutor ScalpingOrderExecutor, opModeService *OperationalModeService) *ModeAwareOrderExecutor {
	return &ModeAwareOrderExecutor{
		liveExecutor:  liveExecutor,
		paperExecutor: paperExecutor,
		opModeService: opModeService,
	}
}

func (e *ModeAwareOrderExecutor) resolveMode(ctx context.Context) OperationalMode {
	if mode, ok := operationalModeFromContext(ctx); ok {
		return mode
	}
	if e != nil && e.opModeService != nil {
		if chatID := strings.TrimSpace(scalpingChatIDFromContext(ctx)); chatID != "" {
			return normalizeOperationalMode(e.opModeService.GetMode(chatID))
		}
	}
	zaplogrus.Warnf("[ORDER-EXECUTOR] WARNING: no operational mode in context, no mode service available; defaulting to paper mode")
	return ModePaper
}

func (e *ModeAwareOrderExecutor) executorForContext(ctx context.Context) (ScalpingOrderExecutor, OperationalMode, error) {
	mode := e.resolveMode(ctx)
	if mode == OpModeLive {
		if e == nil {
			return nil, mode, fmt.Errorf("live mode selected but ModeAwareOrderExecutor receiver is nil; check service wiring in cmd/server/main.go")
		}
		if e.liveExecutor == nil {
			return nil, mode, fmt.Errorf("live mode selected but real order execution is unavailable; verify Bitget credentials, passphrase, and connected wallet mapping (routes.go liveOrderExecutor wiring)")
		}
		return e.liveExecutor, mode, nil
	}
	if e == nil {
		return nil, mode, fmt.Errorf("paper mode selected but ModeAwareOrderExecutor receiver is nil; check service wiring in cmd/server/main.go")
	}
	if e.paperExecutor == nil {
		return nil, mode, fmt.Errorf("paper execution is unavailable; check paper executor wiring in cmd/server/main.go")
	}
	return e.paperExecutor, mode, nil
}

func (e *ModeAwareOrderExecutor) PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	executor, _, err := e.executorForContext(ctx)
	if err != nil {
		return "", err
	}
	return executor.PlaceOrder(ctx, exchange, symbol, side, orderType, amount, price)
}

func (e *ModeAwareOrderExecutor) PlaceOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	executor, mode, err := e.executorForContext(ctx)
	if err != nil {
		return "", err
	}
	details.IsPaperTrade = mode != OpModeLive
	return executor.PlaceOrderWithDetails(ctx, details)
}

func (e *ModeAwareOrderExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	executor, _, err := e.executorForContext(ctx)
	if err != nil {
		return nil, err
	}
	return executor.GetOpenOrders(ctx, exchange, symbol)
}

func (e *ModeAwareOrderExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	executor, _, err := e.executorForContext(ctx)
	if err != nil {
		return nil, err
	}
	return executor.GetClosedOrders(ctx, exchange, symbol, limit)
}

func (e *ModeAwareOrderExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	executor, _, err := e.executorForContext(ctx)
	if err != nil {
		return err
	}
	return executor.CancelOrder(ctx, exchange, orderID)
}

func (e *ModeAwareOrderExecutor) IsPaperTrading() bool {
	return e == nil || e.liveExecutor == nil
}

var _ ScalpingOrderExecutor = (*ModeAwareOrderExecutor)(nil)
