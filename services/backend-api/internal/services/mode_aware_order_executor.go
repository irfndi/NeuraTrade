package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/irfndi/neuratrade/internal/app/execution/liveguard"
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
	liveExecutor   ScalpingOrderExecutor
	paperExecutor  ScalpingOrderExecutor
	opModeService  *OperationalModeService
	liveGuard      *liveguard.Guard
	chatLiveLookup func(ctx context.Context, chatID string) bool
	chatIDLookup   func(ctx context.Context) string
}

func NewModeAwareOrderExecutor(liveExecutor, paperExecutor ScalpingOrderExecutor, opModeService *OperationalModeService) *ModeAwareOrderExecutor {
	return &ModeAwareOrderExecutor{
		liveExecutor:  liveExecutor,
		paperExecutor: paperExecutor,
		opModeService: opModeService,
	}
}

// WithLiveGuard installs the process-level live trading safety guard. The
// guard fires only when the resolved mode is OpModeLive.
func (e *ModeAwareOrderExecutor) WithLiveGuard(guard *liveguard.Guard) *ModeAwareOrderExecutor {
	if e == nil {
		return e
	}
	e.liveGuard = guard
	return e
}

// WithChatLiveLookups installs the chat-ID extractor and live-mode check used
// by the live guard. Both may be nil in test setups.
func (e *ModeAwareOrderExecutor) WithChatLiveLookups(chatIDLookup func(ctx context.Context) string, liveLookup func(ctx context.Context, chatID string) bool) *ModeAwareOrderExecutor {
	if e == nil {
		return e
	}
	e.chatIDLookup = chatIDLookup
	e.chatLiveLookup = liveLookup
	return e
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
	if e == nil {
		return nil, ModePaper, fmt.Errorf("ModeAwareOrderExecutor receiver is nil; check service wiring in cmd/server/main.go")
	}
	mode := e.resolveMode(ctx)
	if mode == OpModeLive {
		if e.liveExecutor == nil {
			return nil, mode, fmt.Errorf("live mode selected but real order execution is unavailable; verify Bitget credentials, passphrase, and connected wallet mapping (routes.go liveOrderExecutor wiring)")
		}
		return e.liveExecutor, mode, nil
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

	if mode == OpModeLive && e.liveGuard != nil {
		chatID := ""
		if e.chatIDLookup != nil {
			chatID = strings.TrimSpace(e.chatIDLookup(ctx))
		}
		chatIsLive := true
		if e.chatLiveLookup != nil && chatID != "" {
			chatIsLive = e.chatLiveLookup(ctx, chatID)
		}
		intentID := strings.TrimSpace(details.IntentID)
		if intentID == "" {
			intentID = strings.TrimSpace(details.ClientOrderID)
		}
		if intentID == "" {
			intentID = fmt.Sprintf("scalping-%d", time.Now().UTC().UnixNano())
		}
		result, gerr := e.liveGuard.CheckOrder(intentID, chatID, details.TradeType, details.Symbol, strings.ToLower(details.Side), strings.ToLower(details.OrderType), details.Amount, chatIsLive)
		if gerr != nil {
			zaplogrus.Errorf("[ORDER-EXECUTOR] live guard rejected: %v", gerr)
			return "", fmt.Errorf("live guard: %w", gerr)
		}
		if !result.Allowed {
			return "", fmt.Errorf("live guard: %s", result.Reason)
		}
		if result.WasCapped {
			zaplogrus.Warnf("[ORDER-EXECUTOR] live guard capped order %s: %s -> %s", intentID, details.Amount.String(), result.CappedAmount.String())
			details.Amount = result.CappedAmount
		}
	}

	orderID, err := executor.PlaceOrderWithDetails(ctx, details)
	if err == nil && mode == OpModeLive && e.liveGuard != nil {
		e.liveGuard.RecordPlaced(strings.TrimSpace(details.IntentID))
	}
	return orderID, err
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
