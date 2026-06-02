package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuditHandler_GetTrades_WithFilters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockDB.Close()

	dbPool := database.NewMockDBPool(mockDB)
	logger := services.NewTradeAuditLogger(dbPool)
	handler := NewAuditHandler(logger)

	now := time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC)

	rows := pgxmock.NewRows([]string{
		"audit_id", "chat_id", "user_id", "symbol", "side", "order_type",
		"size", "requested_price", "signal_id",
		"ai_reasoning_snapshot", "pre_trade_risk_snapshot",
		"order_request", "order_response", "position_state",
		"outcome", "realized_pnl", "holding_seconds",
		"exchange", "intent_id", "amount", "price", "stop_loss", "take_profit",
		"client_order_id", "ai_reasoning", "ai_confidence",
		"pre_trade_safety_status", "order_id", "order_status", "error_message",
		"created_at", "indexed_at",
	}).
		AddRow("audit-1", "chat-123", nil, "BTC/USDT", "buy", "market",
			"100.5", nil, nil,
			nil, nil, nil, nil, nil,
			"placed", nil, nil,
			"bitget", nil, nil, nil, nil, nil, nil, nil, nil,
			nil, "order-1", "placed", nil,
			now, nil).
		AddRow("audit-2", "chat-123", nil, "ETH/USDT", "sell", "limit",
			"50", "2000", nil,
			nil, nil, nil, nil, nil,
			"placed", nil, nil,
			"bitget", nil, nil, nil, nil, nil, nil, nil, nil,
			nil, "order-2", "placed", nil,
			now.Add(-time.Hour), nil)

	mockDB.ExpectQuery("SELECT audit_id, chat_id, user_id, symbol, side, order_type, size, requested_price, signal_id, ai_reasoning_snapshot, pre_trade_risk_snapshot, order_request, order_response, position_state, outcome, realized_pnl, holding_seconds, exchange, intent_id, amount, price, stop_loss, take_profit, client_order_id, ai_reasoning, ai_confidence, pre_trade_safety_status, order_id, order_status, error_message, created_at, indexed_at FROM trade_audit_log WHERE 1=1 AND chat_id = \\? AND symbol = \\? ORDER BY created_at DESC LIMIT \\?").
		WithArgs("chat-123", "BTC/USDT", 100).
		WillReturnRows(rows)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/audit/trades?chat_id=chat-123&symbol=BTC/USDT", nil)

	handler.GetTrades(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Entries []services.TradeAuditEntry `json:"entries"`
		Count   int                        `json:"count"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Count)
	assert.Len(t, resp.Entries, 2)
	assert.Equal(t, "BTC/USDT", resp.Entries[0].Symbol)
	assert.Equal(t, "placed", resp.Entries[0].Outcome)
}

func TestAuditHandler_GetTrades_EmptyResult(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockDB.Close()

	dbPool := database.NewMockDBPool(mockDB)
	logger := services.NewTradeAuditLogger(dbPool)
	handler := NewAuditHandler(logger)

	rows := pgxmock.NewRows([]string{
		"audit_id", "chat_id", "user_id", "symbol", "side", "order_type",
		"size", "requested_price", "signal_id",
		"ai_reasoning_snapshot", "pre_trade_risk_snapshot",
		"order_request", "order_response", "position_state",
		"outcome", "realized_pnl", "holding_seconds",
		"exchange", "intent_id", "amount", "price", "stop_loss", "take_profit",
		"client_order_id", "ai_reasoning", "ai_confidence",
		"pre_trade_safety_status", "order_id", "order_status", "error_message",
		"created_at", "indexed_at",
	})

	mockDB.ExpectQuery("SELECT audit_id, chat_id, user_id, symbol, side, order_type, size, requested_price, signal_id, ai_reasoning_snapshot, pre_trade_risk_snapshot, order_request, order_response, position_state, outcome, realized_pnl, holding_seconds, exchange, intent_id, amount, price, stop_loss, take_profit, client_order_id, ai_reasoning, ai_confidence, pre_trade_safety_status, order_id, order_status, error_message, created_at, indexed_at FROM trade_audit_log WHERE 1=1 ORDER BY created_at DESC LIMIT \\?").
		WithArgs(100).
		WillReturnRows(rows)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/audit/trades", nil)

	handler.GetTrades(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Entries []services.TradeAuditEntry `json:"entries"`
		Count   int                        `json:"count"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 0, resp.Count)
	assert.Empty(t, resp.Entries)
}

func TestAuditHandler_GetTrades_InvalidFrom(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := services.NewTradeAuditLogger(nil)
	handler := NewAuditHandler(logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/audit/trades?from=invalid-date", nil)

	handler.GetTrades(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]string
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Contains(t, resp["error"], "RFC3339")
}

func TestAuditHandler_GetTrades_InvalidTo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := services.NewTradeAuditLogger(nil)
	handler := NewAuditHandler(logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/audit/trades?to=not-a-time", nil)

	handler.GetTrades(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuditHandler_GetTrades_InvalidLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)

	logger := services.NewTradeAuditLogger(nil)
	handler := NewAuditHandler(logger)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/audit/trades?limit=-1", nil)

	handler.GetTrades(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuditHandler_GetTrades_LimitCap(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockDB.Close()

	dbPool := database.NewMockDBPool(mockDB)
	logger := services.NewTradeAuditLogger(dbPool)
	handler := NewAuditHandler(logger)

	rows := pgxmock.NewRows([]string{
		"audit_id", "chat_id", "user_id", "symbol", "side", "order_type",
		"size", "requested_price", "signal_id",
		"ai_reasoning_snapshot", "pre_trade_risk_snapshot",
		"order_request", "order_response", "position_state",
		"outcome", "realized_pnl", "holding_seconds",
		"exchange", "intent_id", "amount", "price", "stop_loss", "take_profit",
		"client_order_id", "ai_reasoning", "ai_confidence",
		"pre_trade_safety_status", "order_id", "order_status", "error_message",
		"created_at", "indexed_at",
	})

	// Limit should be capped at 1000
	mockDB.ExpectQuery("SELECT audit_id, chat_id, user_id, symbol, side, order_type, size, requested_price, signal_id, ai_reasoning_snapshot, pre_trade_risk_snapshot, order_request, order_response, position_state, outcome, realized_pnl, holding_seconds, exchange, intent_id, amount, price, stop_loss, take_profit, client_order_id, ai_reasoning, ai_confidence, pre_trade_safety_status, order_id, order_status, error_message, created_at, indexed_at FROM trade_audit_log WHERE 1=1 ORDER BY created_at DESC LIMIT \\?").
		WithArgs(1000).
		WillReturnRows(rows)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/audit/trades?limit=5000", nil)

	handler.GetTrades(c)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuditHandler_NilLogger(t *testing.T) {
	gin.SetMode(gin.TestMode)

	handler := NewAuditHandler(nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/audit/trades", nil)

	handler.GetTrades(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestAuditHandler_GetTrades_WithTimeRange(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockDB.Close()

	dbPool := database.NewMockDBPool(mockDB)
	logger := services.NewTradeAuditLogger(dbPool)
	handler := NewAuditHandler(logger)

	from := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 2, 23, 59, 59, 0, time.UTC)

	rows := pgxmock.NewRows([]string{
		"audit_id", "chat_id", "user_id", "symbol", "side", "order_type",
		"size", "requested_price", "signal_id",
		"ai_reasoning_snapshot", "pre_trade_risk_snapshot",
		"order_request", "order_response", "position_state",
		"outcome", "realized_pnl", "holding_seconds",
		"exchange", "intent_id", "amount", "price", "stop_loss", "take_profit",
		"client_order_id", "ai_reasoning", "ai_confidence",
		"pre_trade_safety_status", "order_id", "order_status", "error_message",
		"created_at", "indexed_at",
	})

	mockDB.ExpectQuery("SELECT audit_id, chat_id, user_id, symbol, side, order_type, size, requested_price, signal_id, ai_reasoning_snapshot, pre_trade_risk_snapshot, order_request, order_response, position_state, outcome, realized_pnl, holding_seconds, exchange, intent_id, amount, price, stop_loss, take_profit, client_order_id, ai_reasoning, ai_confidence, pre_trade_safety_status, order_id, order_status, error_message, created_at, indexed_at FROM trade_audit_log WHERE 1=1 AND created_at >= \\? AND created_at <= \\? ORDER BY created_at DESC LIMIT \\?").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), 100).
		WillReturnRows(rows)

	fromStr := from.Format(time.RFC3339)
	toStr := to.Format(time.RFC3339)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/audit/trades?from="+fromStr+"&to="+toStr, nil)

	handler.GetTrades(c)

	assert.Equal(t, http.StatusOK, w.Code)
}

// Ensure the handler compiles with the concrete type
func TestAuditHandler_InterfaceCompiles(t *testing.T) {
	var _ interface{ GetTrades(*gin.Context) } = &AuditHandler{}
}
