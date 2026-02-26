package services

import (
	"context"
	"fmt"
	"sync"

	"github.com/shopspring/decimal"
)

type PortfolioSafetyChecker interface {
	CanExecuteTrade(ctx context.Context, chatID, exchange, symbol string, size decimal.Decimal) (bool, string, error)
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
	allowed, reason, err := s.checkSafety(ctx, exchange, symbol, amount)
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
	allowed, reason, err := s.checkSafety(ctx, exchange, symbol, amount)
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
	return s.checkSafety(ctx, exchange, symbol, amount)
}

func (s *SafeOrderExecutor) checkSafety(ctx context.Context, exchange, symbol string, amount decimal.Decimal) (bool, string, error) {
	s.mu.RLock()
	chatID := s.chatID
	safetyService := s.safetyService
	s.mu.RUnlock()

	if safetyService == nil {
		return true, "", nil
	}

	allowed, reason, err := safetyService.CanExecuteTrade(ctx, chatID, exchange, symbol, amount)
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

	allowed, reason, err := s.checkSafety(ctx, details.Exchange, details.Symbol, amount)
	if err != nil {
		return "", fmt.Errorf("safety check failed: %w", err)
	}
	if !allowed {
		return "", fmt.Errorf("portfolio safety blocked: %s", reason)
	}

	return s.baseExecutor.PlaceOrderWithDetails(ctx, details)
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

// IsPaperTrading delegates to the base executor
func (s *SafeOrderExecutor) IsPaperTrading() bool {
	return s.baseExecutor.IsPaperTrading()
}

var _ ScalpingOrderExecutor = (*SafeOrderExecutor)(nil)
