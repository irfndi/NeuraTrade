package services

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"
)

type PortfolioSafetyChecker interface {
	CanExecuteTrade(ctx context.Context, chatID, exchange, symbol string, size decimal.Decimal) (bool, string, error)
}

// SafeOrderExecutor wraps a ScalpingOrderExecutor with portfolio safety checks.
// It enforces portfolio safety gates before any live order placement.
type SafeOrderExecutor struct {
	baseExecutor  ScalpingOrderExecutor
	safetyService PortfolioSafetyChecker
	chatID        string
}

// NewSafeOrderExecutor creates a new safe order executor with portfolio safety checks.
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

// SafetyCheckResult represents the result of a safety check.
type SafetyCheckResult struct {
	Allowed bool
	Reason  string
}

// PlaceOrder executes an order only if portfolio safety checks pass.
// Returns error if safety check fails.
func (s *SafeOrderExecutor) PlaceOrder(
	ctx context.Context,
	exchange, symbol, side, orderType string,
	amount decimal.Decimal,
	price *decimal.Decimal,
) (string, error) {
	// Perform safety check before order execution
	allowed, reason, err := s.checkSafety(ctx, exchange, symbol, amount)
	if err != nil {
		return "", fmt.Errorf("safety check failed: %w", err)
	}

	if !allowed {
		return "", fmt.Errorf("portfolio safety blocked: %s", reason)
	}

	// Execute the order through the base executor
	return s.baseExecutor.PlaceOrder(ctx, exchange, symbol, side, orderType, amount, price)
}

// PlaceOrderWithSafetyCheck performs safety check and returns detailed result.
// This method allows callers to handle blocked orders gracefully.
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
		return "", result, nil // Return safety result, not error - caller handles blocked state
	}

	orderID, err := s.baseExecutor.PlaceOrder(ctx, exchange, symbol, side, orderType, amount, price)
	return orderID, result, err
}

// CheckSafety returns whether trading is allowed and the reason if blocked.
func (s *SafeOrderExecutor) CheckSafety(ctx context.Context, exchange string, symbol string, amount decimal.Decimal) (bool, string, error) {
	return s.checkSafety(ctx, exchange, symbol, amount)
}

// checkSafety performs the portfolio safety check.
func (s *SafeOrderExecutor) checkSafety(ctx context.Context, exchange, symbol string, amount decimal.Decimal) (bool, string, error) {
	if s.safetyService == nil {
		// No safety service configured - allow execution (backward compatibility)
		return true, "", nil
	}

	// Call the portfolio safety service
	allowed, reason, err := s.safetyService.CanExecuteTrade(ctx, s.chatID, exchange, symbol, amount)
	if err != nil {
		// On error, fail closed - block execution for safety
		return false, fmt.Sprintf("safety check error: %v", err), err
	}

	return allowed, reason, nil
}

// GetOpenOrders delegates to the base executor.
func (s *SafeOrderExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	return s.baseExecutor.GetOpenOrders(ctx, exchange, symbol)
}

// GetClosedOrders delegates to the base executor.
func (s *SafeOrderExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	return s.baseExecutor.GetClosedOrders(ctx, exchange, symbol, limit)
}

// CancelOrder delegates to the base executor.
func (s *SafeOrderExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	return s.baseExecutor.CancelOrder(ctx, exchange, orderID)
}

// SetChatID updates the default chatID for safety checks.
func (s *SafeOrderExecutor) SetChatID(chatID string) {
	s.chatID = chatID
}

// GetChatID returns the current default chatID.
func (s *SafeOrderExecutor) GetChatID() string {
	return s.chatID
}

// Ensure SafeOrderExecutor implements ScalpingOrderExecutor interface
var _ ScalpingOrderExecutor = (*SafeOrderExecutor)(nil)
