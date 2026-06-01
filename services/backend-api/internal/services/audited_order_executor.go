package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/database"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

type SafetySnapshot struct {
	Allowed bool   `json:"allowed"`
	Reason  string `json:"reason"`
	Error   string `json:"error,omitempty"`
}

type safetyChecker interface {
	CheckSafety(ctx context.Context, exchange, symbol string, amount decimal.Decimal) (allowed bool, reason string, err error)
}

type AuditedOrderExecutor struct {
	inner ScalpingOrderExecutor
	db    database.DBPool
}

func NewAuditedOrderExecutor(inner ScalpingOrderExecutor, db database.DBPool) *AuditedOrderExecutor {
	return &AuditedOrderExecutor{inner: inner, db: db}
}

func (e *AuditedOrderExecutor) IsPaperTrading() bool {
	if e.inner == nil {
		return true
	}
	return e.inner.IsPaperTrading()
}

var ErrNoInnerExecutor = errors.New("audited order executor has no inner executor")

func (e *AuditedOrderExecutor) PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	if e.inner == nil {
		return "", ErrNoInnerExecutor
	}
	auditID := uuid.New().String()
	details := TradeDetails{Exchange: exchange, Symbol: symbol, Side: side, OrderType: orderType, AmountUSDT: amount, EntryPrice: price}
	e.writeAuditPending(ctx, auditID, details)
	orderID, err := e.inner.PlaceOrder(ctx, exchange, symbol, side, orderType, amount, price)
	if err != nil {
		e.updateAuditError(ctx, auditID, err)
		return orderID, err
	}
	e.updateAuditSuccess(ctx, auditID, orderID)
	return orderID, nil
}

func (e *AuditedOrderExecutor) PlaceOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	if e.inner == nil {
		return "", ErrNoInnerExecutor
	}
	auditID := uuid.New().String()
	e.captureSafetySnapshot(ctx, &details)
	e.writeAuditPending(ctx, auditID, details)
	orderID, err := e.inner.PlaceOrderWithDetails(ctx, details)
	if err != nil {
		e.updateAuditError(ctx, auditID, err)
		return orderID, err
	}
	e.updateAuditSuccess(ctx, auditID, orderID)
	return orderID, nil
}

func (e *AuditedOrderExecutor) PlaceRiskReductionOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	if e.inner == nil {
		return "", ErrNoInnerExecutor
	}
	auditID := uuid.New().String()
	e.writeAuditPending(ctx, auditID, details)
	bypass, ok := e.inner.(interface {
		PlaceRiskReductionOrderWithDetails(context.Context, TradeDetails) (string, error)
	})
	if !ok {
		orderID, err := e.inner.PlaceOrderWithDetails(ctx, details)
		if err != nil {
			e.updateAuditError(ctx, auditID, err)
			return orderID, err
		}
		e.updateAuditSuccess(ctx, auditID, orderID)
		return orderID, nil
	}
	orderID, err := bypass.PlaceRiskReductionOrderWithDetails(ctx, details)
	if err != nil {
		e.updateAuditError(ctx, auditID, err)
		return orderID, err
	}
	e.updateAuditSuccess(ctx, auditID, orderID)
	return orderID, nil
}

func (e *AuditedOrderExecutor) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	if e.inner == nil {
		return nil, ErrNoInnerExecutor
	}
	return e.inner.GetOpenOrders(ctx, exchange, symbol)
}

func (e *AuditedOrderExecutor) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	if e.inner == nil {
		return nil, ErrNoInnerExecutor
	}
	return e.inner.GetClosedOrders(ctx, exchange, symbol, limit)
}

func (e *AuditedOrderExecutor) CancelOrder(ctx context.Context, exchange, orderID string) error {
	if e.inner == nil {
		return ErrNoInnerExecutor
	}
	return e.inner.CancelOrder(ctx, exchange, orderID)
}

func (e *AuditedOrderExecutor) captureSafetySnapshot(ctx context.Context, details *TradeDetails) {
	checker, ok := e.inner.(safetyChecker)
	if !ok || details == nil {
		return
	}
	amount := details.AmountUSDT
	if details.Symbol == "" || amount.LessThanOrEqual(decimal.Zero) {
		return
	}
	allowed, reason, checkErr := checker.CheckSafety(ctx, details.Exchange, details.Symbol, amount)
	snap := SafetySnapshot{Allowed: allowed, Reason: reason}
	if checkErr != nil {
		snap.Error = checkErr.Error()
		snap.Allowed = false
	}
	payload, _ := json.Marshal(snap)
	details.PreTradeSafetyStatus = string(payload)
}

func (e *AuditedOrderExecutor) writeAuditPending(ctx context.Context, auditID string, details TradeDetails) {
	if e.db == nil {
		zaplogrus.Warnf("[AUDIT] DBPool is nil; skipping audit write for %s", auditID)
		return
	}
	chatID := strings.TrimSpace(scalpingChatIDFromContext(ctx))
	now := time.Now().UTC()
	query := "INSERT INTO trade_audit_log (audit_id, chat_id, intent_id, symbol, exchange, side, order_type, amount, price, stop_loss, take_profit, client_order_id, ai_reasoning, ai_confidence, pre_trade_safety_status, order_status, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)"
	_, err := e.db.Exec(ctx, query,
		auditID, chatID, details.IntentID, details.Symbol, details.Exchange, details.Side, details.OrderType,
		details.AmountUSDT.String(), nullableDecimalStr(details.EntryPrice), nullableDecimalStr(details.StopLoss),
		nullableDecimalStr(details.TakeProfit), details.ClientOrderID, details.Reasoning,
		fmt.Sprintf("%f", details.Confidence), details.PreTradeSafetyStatus, "pending", now)
	if err != nil {
		zaplogrus.Warnf("[AUDIT] Failed to write pending audit row %s: %v", auditID, err)
	}
}

func (e *AuditedOrderExecutor) updateAuditSuccess(ctx context.Context, auditID, orderID string) {
	if e.db == nil {
		return
	}
	_, err := e.db.Exec(ctx, "UPDATE trade_audit_log SET order_id = ?, order_status = ?, indexed_at = ? WHERE audit_id = ?", orderID, "placed", time.Now().UTC(), auditID)
	if err != nil {
		zaplogrus.Warnf("[AUDIT] Failed to update audit row %s: %v", auditID, err)
	}
}

func (e *AuditedOrderExecutor) updateAuditError(ctx context.Context, auditID string, orderErr error) {
	if e.db == nil {
		return
	}
	errMsg := ""
	if orderErr != nil {
		errMsg = orderErr.Error()
	}
	_, err := e.db.Exec(ctx, "UPDATE trade_audit_log SET order_status = ?, error_message = ?, indexed_at = ? WHERE audit_id = ?", "error", errMsg, time.Now().UTC(), auditID)
	if err != nil {
		zaplogrus.Warnf("[AUDIT] Failed to update audit row %s: %v", auditID, err)
	}
}

func nullableDecimalStr(d *decimal.Decimal) string {
	if d == nil {
		return ""
	}
	return d.String()
}

var _ ScalpingOrderExecutor = (*AuditedOrderExecutor)(nil)
