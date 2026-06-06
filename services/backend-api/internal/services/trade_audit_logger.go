package services

import (
	"context"
	"encoding/json"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/utils"
	"github.com/shopspring/decimal"
)

// TradeAuditEntry is the structured record written to trade_audit_log.
// Every order placement attempt — whether successful, rejected, or errored —
// produces one entry. JSON snapshot fields are masked for secrets before INSERT.
type TradeAuditEntry struct {
	// Core identifiers
	AuditID   string `json:"audit_id"`
	ChatID    string `json:"chat_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	OrderType string `json:"order_type"`

	// Order parameters
	Size           decimal.Decimal  `json:"size"`
	RequestedPrice *decimal.Decimal `json:"requested_price,omitempty"`

	// Signal provenance
	SignalID string `json:"signal_id,omitempty"`

	// JSON snapshots (masked before INSERT)
	AIReasoningSnapshot  string `json:"ai_reasoning_snapshot,omitempty"`
	PreTradeRiskSnapshot string `json:"pre_trade_risk_snapshot,omitempty"`
	OrderRequest         string `json:"order_request,omitempty"`
	OrderResponse        string `json:"order_response,omitempty"`
	PositionState        string `json:"position_state,omitempty"`

	// Outcome
	Outcome        string           `json:"outcome"` // pending, placed, rejected, error, filled, cancelled
	RealizedPNL    *decimal.Decimal `json:"realized_pnl,omitempty"`
	HoldingSeconds *int             `json:"holding_seconds,omitempty"`

	// Legacy fields (used by AuditedOrderExecutor)
	Exchange             string `json:"exchange,omitempty"`
	IntentID             string `json:"intent_id,omitempty"`
	Amount               string `json:"amount,omitempty"`
	Price                string `json:"price,omitempty"`
	StopLoss             string `json:"stop_loss,omitempty"`
	TakeProfit           string `json:"take_profit,omitempty"`
	ClientOrderID        string `json:"client_order_id,omitempty"`
	AIReasoning          string `json:"ai_reasoning,omitempty"`
	AIConfidence         string `json:"ai_confidence,omitempty"`
	PreTradeSafetyStatus string `json:"pre_trade_safety_status,omitempty"`
	OrderID              string `json:"order_id,omitempty"`
	OrderStatus          string `json:"order_status,omitempty"`
	ErrorMessage         string `json:"error_message,omitempty"`

	// Timestamps
	CreatedAt time.Time  `json:"created_at"`
	IndexedAt *time.Time `json:"indexed_at,omitempty"`
}

// TradeAuditLogger provides append-only persistence for trade audit records.
// All INSERTs go through this service, which handles secret masking and
// schema-compatible writes. The underlying table is protected against
// UPDATE/DELETE at the database level (triggers for SQLite, rules for Postgres).
//
// Retention:
//   - Hot data: 90 days in trade_audit_log (main table)
//   - Cold data: 1 year in trade_audit_log_archive (separate table)
//   - TODO: Implement a periodic archive job (e.g., cron or goroutine ticker)
//     that moves rows older than 90 days to the archive table and purges
//     from the main table. The archive job should log its progress and
//     emit metrics for observability.
type TradeAuditLogger struct {
	db DBPool
}

// NewTradeAuditLogger creates a TradeAuditLogger.
// If db is nil, Log* methods become no-ops (logged at Warn level).
func NewTradeAuditLogger(db DBPool) *TradeAuditLogger {
	return &TradeAuditLogger{db: db}
}

// LogTrade inserts a single audit entry. It masks sensitive fields in JSON
// snapshots before writing. Returns nil on success, or error if the INSERT
// fails (callers should treat this as best-effort: log the error but do not
// fail the trade).
func (l *TradeAuditLogger) LogTrade(ctx context.Context, entry *TradeAuditEntry) error {
	if entry == nil {
		return nil
	}
	if l.db == nil {
		zaplogrus.Warnf("[AUDIT] DBPool is nil; skipping audit write for %s", entry.AuditID)
		return nil
	}

	// Mask secrets in JSON snapshot fields before INSERT.
	entry.AIReasoningSnapshot = maskJSON(entry.AIReasoningSnapshot)
	entry.PreTradeRiskSnapshot = maskJSON(entry.PreTradeRiskSnapshot)
	entry.OrderRequest = maskJSON(entry.OrderRequest)
	entry.OrderResponse = maskJSON(entry.OrderResponse)
	entry.PositionState = maskJSON(entry.PositionState)

	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now().UTC()
	}
	if entry.AuditID == "" {
		entry.AuditID = uuid.New().String()
	}

	// Use ? placeholders (works on both SQLite and Postgres).
	// Insert covers both the new structured fields and legacy fields
	// so the same code path can serve both TradeAuditLogger.LogTrade and
	// the existing AuditedOrderExecutor writes.
	query := `INSERT INTO trade_audit_log (
		audit_id, chat_id, user_id, symbol, side, order_type,
		size, requested_price, signal_id,
		ai_reasoning_snapshot, pre_trade_risk_snapshot,
		order_request, order_response, position_state,
		outcome, realized_pnl, holding_seconds,
		exchange, intent_id, amount, price, stop_loss, take_profit,
		client_order_id, ai_reasoning, ai_confidence,
		pre_trade_safety_status, order_id, order_status, error_message,
		created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	_, err := l.db.Exec(ctx, query,
		entry.AuditID,
		nullIfEmpty(entry.ChatID),
		nullIfEmpty(entry.UserID),
		entry.Symbol,
		entry.Side,
		entry.OrderType,
		entry.Size.String(),
		nullableDecimalStr(entry.RequestedPrice),
		nullIfEmpty(entry.SignalID),
		nullIfEmpty(entry.AIReasoningSnapshot),
		nullIfEmpty(entry.PreTradeRiskSnapshot),
		nullIfEmpty(entry.OrderRequest),
		nullIfEmpty(entry.OrderResponse),
		nullIfEmpty(entry.PositionState),
		entry.Outcome,
		nullableDecimalStr(entry.RealizedPNL),
		entry.HoldingSeconds,
		nullIfEmpty(entry.Exchange),
		nullIfEmpty(entry.IntentID),
		nullIfEmpty(entry.Amount),
		nullIfEmpty(entry.Price),
		nullIfEmpty(entry.StopLoss),
		nullIfEmpty(entry.TakeProfit),
		nullIfEmpty(entry.ClientOrderID),
		nullIfEmpty(entry.AIReasoning),
		nullIfEmpty(entry.AIConfidence),
		nullIfEmpty(entry.PreTradeSafetyStatus),
		nullIfEmpty(entry.OrderID),
		nullIfEmpty(entry.OrderStatus),
		nullIfEmpty(entry.ErrorMessage),
		entry.CreatedAt,
	)
	if err != nil {
		zaplogrus.Warnf("[AUDIT] Failed to write audit entry %s: %v", entry.AuditID, err)
		return err
	}
	return nil
}

// LogTradeFromExecutor builds a TradeAuditEntry from order execution components
// and persists it. This is a convenience method for the decorator pattern.
//
// Parameters:
//   - ctx: context with optional chat_id via scalpingChatIDFromContext
//   - intent: the TradeDetails that describes the intended order
//   - orderID: the exchange-assigned order ID (empty if rejected/errored)
//   - orderErr: error returned by the exchange (nil if success)
//   - riskResult: JSON string of pre-trade risk check result
//   - aiReasoning: AI reasoning text
func (l *TradeAuditLogger) LogTradeFromExecutor(
	ctx context.Context,
	intent TradeDetails,
	orderID string,
	orderErr error,
	riskResult string,
	aiReasoning string,
) error {
	entry := &TradeAuditEntry{
		AuditID:              uuid.New().String(),
		ChatID:               scalpingChatIDFromContext(ctx),
		Symbol:               intent.Symbol,
		Side:                 intent.Side,
		OrderType:            intent.OrderType,
		Size:                 intent.AmountUSDT,
		RequestedPrice:       intent.EntryPrice,
		Exchange:             intent.Exchange,
		IntentID:             intent.IntentID,
		ClientOrderID:        intent.ClientOrderID,
		AIReasoning:          aiReasoning,
		AIConfidence:         formatConfidence(intent.Confidence),
		PreTradeSafetyStatus: intent.PreTradeSafetyStatus,
		PreTradeRiskSnapshot: riskResult,
		AIReasoningSnapshot:  aiReasoning,
		OrderID:              orderID,
		CreatedAt:            time.Now().UTC(),
	}

	if orderErr != nil {
		entry.Outcome = "error"
		entry.ErrorMessage = orderErr.Error()
		entry.OrderStatus = "error"
	} else if orderID == "" {
		entry.Outcome = "rejected"
		entry.OrderStatus = "rejected"
	} else {
		entry.Outcome = "placed"
		entry.OrderStatus = "placed"
		entry.OrderID = orderID
	}

	return l.LogTrade(ctx, entry)
}

// QueryTrades retrieves trade audit entries matching the given filters.
// All filters are optional; empty values are ignored. Results are ordered
// by created_at DESC, limited to at most 1000 entries.
func (l *TradeAuditLogger) QueryTrades(ctx context.Context, chatID, symbol string, from, to time.Time, limit int) ([]TradeAuditEntry, error) {
	if l.db == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	query := "SELECT audit_id, chat_id, user_id, symbol, side, order_type, size, requested_price, signal_id, ai_reasoning_snapshot, pre_trade_risk_snapshot, order_request, order_response, position_state, outcome, realized_pnl, holding_seconds, exchange, intent_id, amount, price, stop_loss, take_profit, client_order_id, ai_reasoning, ai_confidence, pre_trade_safety_status, order_id, order_status, error_message, created_at, indexed_at FROM trade_audit_log WHERE 1=1"
	args := make([]any, 0, 4)

	if chatID != "" {
		query += " AND chat_id = ?"
		args = append(args, chatID)
	}
	if symbol != "" {
		query += " AND symbol = ?"
		args = append(args, symbol)
	}
	if !from.IsZero() {
		query += " AND created_at >= ?"
		args = append(args, from)
	}
	if !to.IsZero() {
		query += " AND created_at <= ?"
		args = append(args, to)
	}

	query += " ORDER BY created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := l.db.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []TradeAuditEntry
	for rows.Next() {
		var e TradeAuditEntry
		var sizeStr, reqPriceStr, pnlStr sqlNullString
		var holdingSec sqlNullInt64
		var indexedAt sqlNullTime
		var chatID, userID, signalID, aiReasonSnap, preTradeRisk, orderReq, orderResp, posState sqlNullString
		var exchange, intentID, amount, price, stopLoss, takeProfit, clientOID, aiReason, aiConf, preTradeSafety, orderID, orderStatus, errMsg sqlNullString

		err := rows.Scan(
			&e.AuditID, &chatID, &userID, &e.Symbol, &e.Side, &e.OrderType,
			&sizeStr, &reqPriceStr, &signalID,
			&aiReasonSnap, &preTradeRisk,
			&orderReq, &orderResp, &posState,
			&e.Outcome, &pnlStr, &holdingSec,
			&exchange, &intentID, &amount, &price, &stopLoss, &takeProfit,
			&clientOID, &aiReason, &aiConf,
			&preTradeSafety, &orderID, &orderStatus, &errMsg,
			&e.CreatedAt, &indexedAt,
		)
		if err != nil {
			return nil, err
		}

		e.ChatID = chatID.String
		e.UserID = userID.String
		e.SignalID = signalID.String
		e.AIReasoningSnapshot = aiReasonSnap.String
		e.PreTradeRiskSnapshot = preTradeRisk.String
		e.OrderRequest = orderReq.String
		e.OrderResponse = orderResp.String
		e.PositionState = posState.String
		e.Exchange = exchange.String
		e.IntentID = intentID.String
		e.Amount = amount.String
		e.Price = price.String
		e.StopLoss = stopLoss.String
		e.TakeProfit = takeProfit.String
		e.ClientOrderID = clientOID.String
		e.AIReasoning = aiReason.String
		e.AIConfidence = aiConf.String
		e.PreTradeSafetyStatus = preTradeSafety.String
		e.OrderID = orderID.String
		e.OrderStatus = orderStatus.String
		e.ErrorMessage = errMsg.String

		if sizeStr.Valid {
			e.Size, _ = decimal.NewFromString(sizeStr.String)
		}
		if reqPriceStr.Valid {
			p, _ := decimal.NewFromString(reqPriceStr.String)
			e.RequestedPrice = &p
		}
		if pnlStr.Valid {
			p, _ := decimal.NewFromString(pnlStr.String)
			e.RealizedPNL = &p
		}
		if holdingSec.Valid {
			h := int(holdingSec.Int64)
			e.HoldingSeconds = &h
		}
		if indexedAt.Valid {
			e.IndexedAt = &indexedAt.Time
		}

		entries = append(entries, e)
	}

	if entries == nil {
		entries = []TradeAuditEntry{}
	}
	return entries, rows.Err()
}

// sqlNullString is a helper for scanning nullable TEXT columns.
type sqlNullString struct {
	String string
	Valid  bool
}

func (s *sqlNullString) Scan(src any) error {
	if src == nil {
		s.Valid = false
		return nil
	}
	s.Valid = true
	switch v := src.(type) {
	case string:
		s.String = v
	case []byte:
		s.String = string(v)
	default:
		s.String = ""
	}
	return nil
}

// sqlNullInt64 is a helper for scanning nullable INTEGER columns.
type sqlNullInt64 struct {
	Int64 int64
	Valid bool
}

func (s *sqlNullInt64) Scan(src any) error {
	if src == nil {
		s.Valid = false
		return nil
	}
	s.Valid = true
	switch v := src.(type) {
	case int64:
		s.Int64 = v
	case float64:
		s.Int64 = int64(v)
	default:
		s.Int64 = 0
	}
	return nil
}

// sqlNullTime is a helper for scanning nullable TIMESTAMP columns.
type sqlNullTime struct {
	Time  time.Time
	Valid bool
}

func (s *sqlNullTime) Scan(src any) error {
	if src == nil {
		s.Valid = false
		return nil
	}
	s.Valid = true
	switch v := src.(type) {
	case time.Time:
		s.Time = v
	case string:
		s.Time, _ = time.Parse(time.RFC3339, v)
	default:
		s.Time = time.Time{}
	}
	return nil
}

// maskJSON applies secret masking to JSON string content.
// Uses utils.MaskJSON with default sensitive field patterns.
func maskJSON(s string) string {
	if s == "" {
		return ""
	}
	// Verify it's valid JSON before attempting to mask.
	if !json.Valid([]byte(s)) {
		return s
	}
	return utils.MaskJSON(s, nil)
}

// nullIfEmpty returns nil for empty strings (SQL NULL).
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// formatConfidence formats a confidence float as a string suitable for storage.
func formatConfidence(c float64) string {
	return decimal.NewFromFloat(c).String()
}
