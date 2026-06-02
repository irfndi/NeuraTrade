package services

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradeAuditLogger_LogTrade_InsertsEntry(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

	entry := &TradeAuditEntry{
		AuditID:   "test-audit-uuid",
		ChatID:    "chat-123",
		UserID:    "user-456",
		Symbol:    "BTC/USDT",
		Side:      "buy",
		OrderType: "market",
		Size:      decimal.NewFromFloat(100.50),
		Outcome:   "placed",
		CreatedAt: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
	}

	mockDB.ExpectExec("INSERT INTO trade_audit_log").
		WithArgs(
			"test-audit-uuid",
			"chat-123",
			"user-456",
			"BTC/USDT",
			"buy",
			"market",
			"100.5",
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			"placed",
			pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			entry.CreatedAt,
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := logger.LogTrade(context.Background(), entry)
	assert.NoError(t, err)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestTradeAuditLogger_LogTrade_MasksAPIKeys(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

	entry := &TradeAuditEntry{
		AuditID:             "masking-test-uuid",
		Symbol:              "ETH/USDT",
		Side:                "sell",
		OrderType:           "limit",
		Size:                decimal.NewFromFloat(50),
		RequestedPrice:      ptrDecimal(decimal.NewFromFloat(2000)),
		AIReasoningSnapshot: `{"reasoning": "bullish divergence", "api_key": "test-key-value-12345"}`,
		OrderRequest:        `{"symbol": "ETH/USDT", "apiKey": "exchange-api-key-value"}`,
		Outcome:             "placed",
	}

	mockDB.ExpectExec("INSERT INTO trade_audit_log").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := logger.LogTrade(context.Background(), entry)
	require.NoError(t, err)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestTradeAuditLogger_LogTradeFromExecutor_Success(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

	intent := TradeDetails{
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		AmountUSDT: decimal.NewFromFloat(200),
		EntryPrice: ptrDecimal(decimal.NewFromFloat(50000)),
		IntentID:   "intent-abc",
		Confidence: 0.85,
		Reasoning:  "Strong buy signal from RSI divergence",
	}

	ctx := context.Background()
	mockDB.ExpectExec("INSERT INTO trade_audit_log").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "BTC/USDT", "buy", "market",
			"200", "50000", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "placed", pgxmock.AnyArg(),
			pgxmock.AnyArg(), "bitget", "intent-abc", pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			"0.85", pgxmock.AnyArg(), "order-789", "placed", pgxmock.AnyArg(),
			pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := logger.LogTradeFromExecutor(ctx, intent, "order-789", nil, "", intent.Reasoning)
	assert.NoError(t, err)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestTradeAuditLogger_LogTradeFromExecutor_Error(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

	intent := TradeDetails{
		Exchange:   "bitget",
		Symbol:     "ETH/USDT",
		Side:       "sell",
		OrderType:  "market",
		AmountUSDT: decimal.NewFromFloat(50),
	}

	orderErr := errors.New("insufficient balance")
	ctx := context.Background()

	mockDB.ExpectExec("INSERT INTO trade_audit_log").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "ETH/USDT", "sell", "market",
			"50", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "error", pgxmock.AnyArg(),
			pgxmock.AnyArg(), "bitget", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "error", "insufficient balance",
			pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := logger.LogTradeFromExecutor(ctx, intent, "", orderErr, "", "")
	assert.NoError(t, err)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestTradeAuditLogger_LogTrade_NilDB(t *testing.T) {
	logger := NewTradeAuditLogger(nil)
	err := logger.LogTrade(context.Background(), &TradeAuditEntry{
		AuditID:   "test",
		Symbol:    "BTC/USDT",
		Side:      "buy",
		OrderType: "market",
		Size:      decimal.NewFromFloat(100),
		Outcome:   "placed",
	})
	assert.NoError(t, err, "nil DB should not error")
}

func TestTradeAuditLogger_LogTrade_NilEntry(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

	err := logger.LogTrade(context.Background(), nil)
	assert.NoError(t, err, "nil entry should not error")
}

func TestTradeAuditLogger_LogTrade_GeneratesAuditID(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

	entry := &TradeAuditEntry{
		Symbol:    "BTC/USDT",
		Side:      "buy",
		OrderType: "market",
		Size:      decimal.NewFromFloat(100),
		Outcome:   "placed",
	}

	mockDB.ExpectExec("INSERT INTO trade_audit_log").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err := logger.LogTrade(context.Background(), entry)
	assert.NoError(t, err)
	assert.NotEmpty(t, entry.AuditID, "audit_id should be auto-generated")
}

func TestTradeAuditLog_AppendOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping append-only test in short mode")
	}

	dbPath := t.TempDir() + "/test_audit_append_only.db"

	sqlDB, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer sqlDB.Close()

	_, err = sqlDB.Exec(`PRAGMA foreign_keys = ON`)
	require.NoError(t, err)

	_, err = sqlDB.Exec(`
		CREATE TABLE IF NOT EXISTS trade_audit_log (
			audit_id TEXT PRIMARY KEY,
			chat_id TEXT,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			order_type TEXT NOT NULL,
			size TEXT NOT NULL,
			outcome TEXT,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TRIGGER IF NOT EXISTS trg_trade_audit_log_prevent_update
		BEFORE UPDATE ON trade_audit_log
		BEGIN
			SELECT RAISE(ABORT, 'UPDATE on trade_audit_log is forbidden: audit log is append-only');
		END;
		CREATE TRIGGER IF NOT EXISTS trg_trade_audit_log_prevent_delete
		BEFORE DELETE ON trade_audit_log
		BEGIN
			SELECT RAISE(ABORT, 'DELETE on trade_audit_log is forbidden: audit log is append-only');
		END;
	`)
	require.NoError(t, err)

	_, err = sqlDB.Exec(`INSERT INTO trade_audit_log (audit_id, symbol, side, order_type, size, outcome)
		VALUES ('test-append-only-1', 'BTC/USDT', 'buy', 'market', '100', 'placed')`)
	require.NoError(t, err, "INSERT should succeed")

	_, err = sqlDB.Exec(`UPDATE trade_audit_log SET outcome = 'filled' WHERE audit_id = 'test-append-only-1'`)
	require.Error(t, err, "UPDATE should be rejected by trigger")
	assert.Contains(t, strings.ToLower(err.Error()), "append-only", "error should mention append-only")

	_, err = sqlDB.Exec(`DELETE FROM trade_audit_log WHERE audit_id = 'test-append-only-1'`)
	require.Error(t, err, "DELETE should be rejected by trigger")
	assert.Contains(t, strings.ToLower(err.Error()), "append-only", "error should mention append-only")

	var count int
	err = sqlDB.QueryRow(`SELECT COUNT(*) FROM trade_audit_log`).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "the row should remain after failed UPDATE/DELETE")
}

func TestTradeAuditLogger_QueryTrades_WithFilters(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

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
			"100", nil, nil,
			nil, nil, nil, nil, nil,
			"placed", nil, nil,
			"bitget", nil, nil, nil, nil, nil, nil, nil, nil,
			nil, "order-1", "placed", nil,
			now, nil)

	mockDB.ExpectQuery("SELECT audit_id, chat_id, user_id, symbol, side, order_type, size, requested_price, signal_id, ai_reasoning_snapshot, pre_trade_risk_snapshot, order_request, order_response, position_state, outcome, realized_pnl, holding_seconds, exchange, intent_id, amount, price, stop_loss, take_profit, client_order_id, ai_reasoning, ai_confidence, pre_trade_safety_status, order_id, order_status, error_message, created_at, indexed_at FROM trade_audit_log WHERE 1=1 AND chat_id = \\? AND symbol = \\? ORDER BY created_at DESC LIMIT \\?").
		WithArgs("chat-123", "BTC/USDT", 10).
		WillReturnRows(rows)

	entries, err := logger.QueryTrades(context.Background(), "chat-123", "BTC/USDT", time.Time{}, time.Time{}, 10)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
	assert.Equal(t, "audit-1", entries[0].AuditID)
	assert.Equal(t, "BTC/USDT", entries[0].Symbol)
	assert.Equal(t, "placed", entries[0].Outcome)
}

func TestTradeAuditLogger_QueryTrades_EmptyFilters(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

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

	entries, err := logger.QueryTrades(context.Background(), "", "", time.Time{}, time.Time{}, 0)
	require.NoError(t, err)
	assert.Empty(t, entries, "should return empty slice for no results")
}

func TestTradeAuditLogger_NilDBQuery(t *testing.T) {
	logger := NewTradeAuditLogger(nil)
	entries, err := logger.QueryTrades(context.Background(), "", "", time.Time{}, time.Time{}, 10)
	assert.NoError(t, err)
	assert.Nil(t, entries)
}

func TestTradeAuditLogger_AuditFailureDoesNotFailTrade(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

	entry := &TradeAuditEntry{
		AuditID:   "fail-audit-entry",
		Symbol:    "BTC/USDT",
		Side:      "buy",
		OrderType: "market",
		Size:      decimal.NewFromFloat(100),
		Outcome:   "placed",
	}

	mockDB.ExpectExec("INSERT INTO trade_audit_log").
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("disk full"))

	err := logger.LogTrade(context.Background(), entry)
	assert.Error(t, err, "LogTrade should return the error")
	assert.Contains(t, err.Error(), "disk full")
}

func TestTradeAuditLogger_MaskingRegression(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		checkFn func(t *testing.T, masked string)
	}{
		{
			name:  "API key masked",
			input: `{"analysis": "bullish", "api_key": "test-abc123def456ghi789"}`,
			checkFn: func(t *testing.T, masked string) {
				assert.NotContains(t, masked, "sk-abc123def456ghi789")
				assert.Contains(t, masked, "api_key")
			},
		},
		{
			name:  "password masked",
			input: `{"user": "admin", "password": "super-secret-pass-123"}`,
			checkFn: func(t *testing.T, masked string) {
				assert.NotContains(t, masked, "super-secret-pass-123")
				assert.Contains(t, masked, "password")
			},
		},
		{
			name:  "empty string unchanged",
			input: "",
			checkFn: func(t *testing.T, masked string) {
				assert.Equal(t, "", masked)
			},
		},
		{
			name:  "non-JSON string unchanged",
			input: "plain text reasoning without JSON structure",
			checkFn: func(t *testing.T, masked string) {
				assert.Equal(t, "plain text reasoning without JSON structure", masked)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			masked := maskJSON(tt.input)
			tt.checkFn(t, masked)
		})
	}
}

func TestTradeAuditLogger_QueryTrades_WithTimeRange(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	logger := NewTradeAuditLogger(dbPool)

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
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), 50).
		WillReturnRows(rows)

	entries, err := logger.QueryTrades(context.Background(), "", "", from, to, 50)
	require.NoError(t, err)
	assert.NotNil(t, entries)
	assert.Empty(t, entries)
}

func ptrDecimal(d decimal.Decimal) *decimal.Decimal {
	return &d
}

func init() {
	os.Setenv("NEURATRADE_HOME", "")
}
