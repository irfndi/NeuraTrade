package services

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/shopspring/decimal"
)

type PortfolioSafetyChecker interface {
	CanExecuteTrade(ctx context.Context, chatID, exchange, symbol, marketType string, size decimal.Decimal) (bool, string, error)
}

type SafeOrderExecutor struct {
	baseExecutor  ScalpingOrderExecutor
	safetyService PortfolioSafetyChecker
	chatID        string
	mu            sync.RWMutex
}

func NewSafeOrderExecutor(
	baseExecutor ScalpingOrderExecutor,
	safetyService PortfolioSafetyChecker,
	chatID string,
) *SafeOrderExecutor {
	return &SafeOrderExecutor{
		baseExecutor:  baseExecutor,
		safetyService: safetyService,
		chatID:        chatID,
	}
}

type SafetyCheckResult struct {
	Allowed bool
	Reason  string
}

func (s *SafeOrderExecutor) PlaceOrder(
	ctx context.Context,
	exchange, symbol, side, orderType string,
	amount decimal.Decimal,
	price *decimal.Decimal,
) (string, error) {
	allowed, reason, err := s.checkSafety(ctx, exchange, symbol, resolveSafetyMarketType(ctx, exchange, symbol), amount)
	if err != nil {
		return "", fmt.Errorf("safety check failed: %w", err)
	}

	if !allowed {
		return "", fmt.Errorf("portfolio safety blocked: %s", reason)
	}

	return s.baseExecutor.PlaceOrder(ctx, exchange, symbol, side, orderType, amount, price)
}

func (s *SafeOrderExecutor) PlaceOrderWithSafetyCheck(
	ctx context.Context,
	exchange, symbol, side, orderType string,
	amount decimal.Decimal,
	price *decimal.Decimal,
) (string, *SafetyCheckResult, error) {
	allowed, reason, err := s.checkSafety(ctx, exchange, symbol, resolveSafetyMarketType(ctx, exchange, symbol), amount)
	if err != nil {
		return "", nil, fmt.Errorf("safety check failed: %w", err)
	}

	result := &SafetyCheckResult{
		Allowed: allowed,
		Reason:  reason,
	}

	if !allowed {
		return "", result, nil
	}

	orderID, err := s.baseExecutor.PlaceOrder(ctx, exchange, symbol, side, orderType, amount, price)
	return orderID, result, err
}

func (s *SafeOrderExecutor) CheckSafety(ctx context.Context, exchange string, symbol string, amount decimal.Decimal) (bool, string, error) {
	return s.checkSafety(ctx, exchange, symbol, resolveSafetyMarketType(ctx, exchange, symbol), amount)
}

func (s *SafeOrderExecutor) checkSafety(ctx context.Context, exchange, symbol, marketType string, amount decimal.Decimal) (bool, string, error) {
	s.mu.RLock()
	chatID := s.chatID
	safetyService := s.safetyService
	s.mu.RUnlock()
	if scopedChatID := scalpingChatIDFromContext(ctx); scopedChatID != "" {
		chatID = scopedChatID
	}

	if safetyService == nil {
		return true, "", nil
	}

	if leverage := scalpingLeverageFromContext(ctx); leverage > 0 {
		if typedSafety, ok := safetyService.(interface {
			EvaluateTradeWithLeverage(context.Context, string, string, string, string, int, decimal.Decimal) (TradeSafetyDecision, error)
		}); ok {
			decision, err := typedSafety.EvaluateTradeWithLeverage(ctx, chatID, exchange, symbol, marketType, leverage, amount)
			if err != nil {
				return false, fmt.Sprintf("safety check error: %v", err), err
			}
			if decision.ZeroMaxMinNotionalBypass {
				return true, "", nil
			}
			return decision.Allowed, decision.Reason, nil
		}
		if typedSafety, ok := safetyService.(interface {
			CanExecuteTradeWithLeverage(context.Context, string, string, string, string, int, decimal.Decimal) (bool, string, error)
		}); ok {
			allowed, reason, err := typedSafety.CanExecuteTradeWithLeverage(ctx, chatID, exchange, symbol, marketType, leverage, amount)
			if err != nil {
				return false, fmt.Sprintf("safety check error: %v", err), err
			}
			return allowed, reason, nil
		}
	}

	allowed, reason, err := safetyService.CanExecuteTrade(ctx, chatID, exchange, symbol, marketType, amount)
	if err != nil {
		return false, fmt.Sprintf("safety check error: %v", err), err
	}

	return allowed, reason, nil
}

func (s *SafeOrderExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	return s.baseExecutor.GetOpenOrders(ctx, exchange, symbol)
}

func (s *SafeOrderExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	return s.baseExecutor.GetClosedOrders(ctx, exchange, symbol, limit)
}

func (s *SafeOrderExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	return s.baseExecutor.CancelOrder(ctx, exchange, orderID)
}

// PlaceOrderWithDetails places an order with full trade details
func (s *SafeOrderExecutor) PlaceOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	amount := details.AmountUSDT
	if amount.LessThanOrEqual(decimal.Zero) {
		return "", fmt.Errorf("invalid order size: amount_usdt must be positive")
	}

	s.mu.RLock()
	chatID := s.chatID
	safetyService := s.safetyService
	s.mu.RUnlock()
	if scopedChatID := scalpingChatIDFromContext(ctx); scopedChatID != "" {
		chatID = scopedChatID
	}

	if safetyService == nil {
		return s.baseExecutor.PlaceOrderWithDetails(ctx, details)
	}

	allowed := false
	reason := ""
	var err error

	if typedSafety, ok := safetyService.(interface {
		EvaluateTradeWithLeverage(context.Context, string, string, string, string, int, decimal.Decimal) (TradeSafetyDecision, error)
	}); ok {
		decision, decisionErr := typedSafety.EvaluateTradeWithLeverage(
			ctx,
			chatID,
			details.Exchange,
			details.Symbol,
			details.MarketType,
			details.Leverage,
			amount,
		)
		err = decisionErr
		if err != nil {
			return "", fmt.Errorf("safety check failed: %w", err)
		}
		if decision.ZeroMaxMinNotionalBypass {
			return s.baseExecutor.PlaceOrderWithDetails(ctx, details)
		}
		allowed = decision.Allowed
		reason = decision.Reason
	} else {
		if leverageAware, ok := safetyService.(interface {
			CanExecuteTradeWithLeverage(context.Context, string, string, string, string, int, decimal.Decimal) (bool, string, error)
		}); ok {
			allowed, reason, err = leverageAware.CanExecuteTradeWithLeverage(
				ctx,
				chatID,
				details.Exchange,
				details.Symbol,
				details.MarketType,
				details.Leverage,
				amount,
			)
		} else {
			allowed, reason, err = safetyService.CanExecuteTrade(ctx, chatID, details.Exchange, details.Symbol, details.MarketType, amount)
		}
	}
	if err != nil {
		return "", fmt.Errorf("safety check failed: %w", err)
	}
	if !allowed {
		return "", fmt.Errorf("portfolio safety blocked: %s", reason)
	}

	return s.baseExecutor.PlaceOrderWithDetails(ctx, details)
}

// PlaceRiskReductionOrderWithDetails bypasses pre-trade safety checks for forced de-risking actions.
// This is only intended for emergency exposure reduction where normal entries are already blocked.
func (s *SafeOrderExecutor) PlaceRiskReductionOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	amount := details.AmountUSDT
	if amount.LessThanOrEqual(decimal.Zero) {
		return "", fmt.Errorf("invalid order size: amount_usdt must be positive")
	}
	if !details.ReduceOnly {
		return "", fmt.Errorf("risk-reduction orders require ReduceOnly=true to prevent opening new positions")
	}
	orderID, err := s.baseExecutor.PlaceOrderWithDetails(ctx, details)
	if err != nil {
		return "", fmt.Errorf("place risk-reduction order with details: %w", err)
	}
	return orderID, nil
}

func (s *SafeOrderExecutor) SyncPositionProtection(
	ctx context.Context,
	exchange string,
	position ManagedOpenPosition,
	stopLoss decimal.Decimal,
	takeProfit decimal.Decimal,
) error {
	syncable, ok := s.baseExecutor.(interface {
		SyncPositionProtection(context.Context, string, ManagedOpenPosition, decimal.Decimal, decimal.Decimal) error
	})
	if !ok {
		return fmt.Errorf("%w: base executor does not support exchange-side protection sync", ErrProtectionSyncUnsupported)
	}
	if err := syncable.SyncPositionProtection(ctx, exchange, position, stopLoss, takeProfit); err != nil {
		return fmt.Errorf("sync position protection: %w", err)
	}
	return nil
}

func (s *SafeOrderExecutor) SetChatID(chatID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chatID = chatID
}

func (s *SafeOrderExecutor) GetChatID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.chatID
}

func resolveSafetyMarketType(ctx context.Context, exchange, symbol string) string {
	if scopedMarketType := scalpingMarketTypeFromContext(ctx); scopedMarketType != "" {
		return scopedMarketType
	}
	inferred := inferSafetyMarketType(exchange, symbol)
	if inferred != "" {
		return inferred
	}
	if scalpingLeverageFromContext(ctx) > 0 {
		return "futures"
	}
	return ""
}

func inferSafetyMarketType(exchange, symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return ""
	}
	if strings.Contains(normalized, ":") || strings.Contains(normalized, "PERP") || strings.Contains(normalized, "SWAP") {
		return "futures"
	}
	if strings.EqualFold(strings.TrimSpace(exchange), "bitget") && strings.Contains(normalized, "/") {
		return "futures"
	}
	return ""
}

// IsPaperTrading delegates to the base executor
func (s *SafeOrderExecutor) IsPaperTrading() bool {
	return s.baseExecutor.IsPaperTrading()
}

var _ ScalpingOrderExecutor = (*SafeOrderExecutor)(nil)
