package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type mockAuditInner struct {
	mock.Mock
	safetyAllowed bool
	safetyReason  string
	safetyErr     error
}

func (m *mockAuditInner) PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error) {
	args := m.Called(ctx, exchange, symbol, side, orderType, amount, price)
	return args.String(0), args.Error(1)
}

func (m *mockAuditInner) PlaceOrderWithDetails(ctx context.Context, details TradeDetails) (string, error) {
	args := m.Called(ctx, details)
	return args.String(0), args.Error(1)
}

func (m *mockAuditInner) GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error) {
	args := m.Called(ctx, exchange, symbol)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]map[string]interface{}), args.Error(1)
}

func (m *mockAuditInner) GetClosedOrders(ctx context.Context, exchange, symbol string, limit int) ([]map[string]interface{}, error) {
	args := m.Called(ctx, exchange, symbol, limit)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]map[string]interface{}), args.Error(1)
}

func (m *mockAuditInner) CancelOrder(ctx context.Context, exchange, orderID string) error {
	args := m.Called(ctx, exchange, orderID)
	return args.Error(0)
}

func (m *mockAuditInner) IsPaperTrading() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *mockAuditInner) CheckSafety(ctx context.Context, exchange, symbol string, amount decimal.Decimal) (bool, string, error) {
	return m.safetyAllowed, m.safetyReason, m.safetyErr
}

var _ ScalpingOrderExecutor = (*mockAuditInner)(nil)
var _ safetyChecker = (*mockAuditInner)(nil)

func setupAuditTestDB(t *testing.T) (database.DBPool, pgxmock.PgxPoolIface) {
	t.Helper()
	mockDB, err := pgxmock.NewPool()
	require.NoError(t, err)
	return database.NewMockDBPool(mockDB), mockDB
}

func TestAuditedOrderExecutor_SuccessfulOrderCreatesPlacedAuditRow(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	inner := new(mockAuditInner)
	inner.safetyAllowed = true
	inner.safetyReason = "all checks passed"
	inner.On("IsPaperTrading").Return(false)
	details := TradeDetails{Exchange: "bitget", Symbol: "BTC/USDT", Side: "buy", OrderType: "market", AmountUSDT: decimal.NewFromFloat(100), Reasoning: "RSI oversold + MACD bullish crossover", Confidence: 0.85}
	inner.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(d TradeDetails) bool { return d.Symbol == details.Symbol && d.Reasoning == details.Reasoning })).Return("order-abc-123", nil)
	auditExec := NewAuditedOrderExecutor(inner, dbPool)
	mockDB.ExpectExec("INSERT INTO trade_audit_log").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "BTC/USDT", "bitget", "buy", "market", "100", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "RSI oversold + MACD bullish crossover", "0.850000", pgxmock.AnyArg(), "pending", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mockDB.ExpectExec("UPDATE trade_audit_log").WithArgs("order-abc-123", "placed", pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	orderID, err := auditExec.PlaceOrderWithDetails(context.Background(), details)
	assert.NoError(t, err)
	assert.Equal(t, "order-abc-123", orderID)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestAuditedOrderExecutor_FailedOrderCreatesErrorAuditRow(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	inner := new(mockAuditInner)
	inner.On("IsPaperTrading").Return(false)
	orderErr := errors.New("Bitget API returned 403: insufficient margin")
	details := TradeDetails{Exchange: "bitget", Symbol: "ETH/USDT", Side: "sell", OrderType: "market", AmountUSDT: decimal.NewFromFloat(50)}
	inner.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(d TradeDetails) bool { return d.Symbol == "ETH/USDT" })).Return("", orderErr)
	auditExec := NewAuditedOrderExecutor(inner, dbPool)
	mockDB.ExpectExec("INSERT INTO trade_audit_log").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "ETH/USDT", "bitget", "sell", "market", "50", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "pending", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mockDB.ExpectExec("UPDATE trade_audit_log").WithArgs("error", "Bitget API returned 403: insufficient margin", pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	orderID, err := auditExec.PlaceOrderWithDetails(context.Background(), details)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient margin")
	assert.Empty(t, orderID)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestAuditedOrderExecutor_AiReasoningIsCaptured(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	inner := new(mockAuditInner)
	inner.On("IsPaperTrading").Return(false)
	reasoning := "Price at key support level $0.45. RSI=28 indicating oversold conditions."
	details := TradeDetails{Exchange: "bitget", Symbol: "ADA/USDT", Side: "buy", OrderType: "market", AmountUSDT: decimal.NewFromFloat(25), Reasoning: reasoning, Confidence: 0.72}
	inner.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(d TradeDetails) bool { return d.Reasoning == reasoning })).Return("order-reasoning-456", nil)
	auditExec := NewAuditedOrderExecutor(inner, dbPool)
	mockDB.ExpectExec("INSERT INTO trade_audit_log").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "ADA/USDT", "bitget", "buy", "market", "25", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), reasoning, "0.720000", pgxmock.AnyArg(), "pending", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mockDB.ExpectExec("UPDATE trade_audit_log").WithArgs("order-reasoning-456", "placed", pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	orderID, err := auditExec.PlaceOrderWithDetails(context.Background(), details)
	assert.NoError(t, err)
	assert.Equal(t, "order-reasoning-456", orderID)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestAuditedOrderExecutor_SafetySnapshotIsCaptured(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	inner := new(mockAuditInner)
	inner.safetyAllowed = false
	inner.safetyReason = "Daily loss limit exceeded: 150.00/100.00"
	inner.On("IsPaperTrading").Return(false)
	var capturedDetails TradeDetails
	inner.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(d TradeDetails) bool { return d.Symbol == "SOL/USDT" })).Run(func(args mock.Arguments) {
		capturedDetails = args.Get(1).(TradeDetails)
	}).Return("order-sol-789", nil)
	details := TradeDetails{Exchange: "bitget", Symbol: "SOL/USDT", Side: "buy", OrderType: "market", AmountUSDT: decimal.NewFromFloat(200)}
	auditExec := NewAuditedOrderExecutor(inner, dbPool)
	mockDB.ExpectExec("INSERT INTO trade_audit_log").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "SOL/USDT", "bitget", "buy", "market", "200", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "pending", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mockDB.ExpectExec("UPDATE trade_audit_log").WithArgs("order-sol-789", "placed", pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	orderID, err := auditExec.PlaceOrderWithDetails(context.Background(), details)
	assert.NoError(t, err)
	assert.Equal(t, "order-sol-789", orderID)
	assert.NoError(t, mockDB.ExpectationsWereMet())
	assert.NotEmpty(t, capturedDetails.PreTradeSafetyStatus)
	var snap SafetySnapshot
	err = json.Unmarshal([]byte(capturedDetails.PreTradeSafetyStatus), &snap)
	require.NoError(t, err)
	assert.False(t, snap.Allowed)
	assert.Contains(t, snap.Reason, "Daily loss limit exceeded")
}

func TestAuditedOrderExecutor_AuditWriteFailureDoesNotBlockOrder(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	inner := new(mockAuditInner)
	inner.On("IsPaperTrading").Return(false)
	details := TradeDetails{Exchange: "bitget", Symbol: "BTC/USDT", Side: "buy", OrderType: "market", AmountUSDT: decimal.NewFromFloat(100), Reasoning: "strong momentum", Confidence: 0.80}
	inner.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(d TradeDetails) bool { return d.Confidence == 0.80 })).Return("order-despite-audit-failure", nil)
	auditExec := NewAuditedOrderExecutor(inner, dbPool)
	mockDB.ExpectExec("INSERT INTO trade_audit_log").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "BTC/USDT", "bitget", "buy", "market", "100", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "strong momentum", "0.800000", pgxmock.AnyArg(), "pending", pgxmock.AnyArg()).WillReturnError(errors.New("audit DB connection lost"))
	orderID, err := auditExec.PlaceOrderWithDetails(context.Background(), details)
	assert.NoError(t, err, "audit failure must not block the order")
	assert.Equal(t, "order-despite-audit-failure", orderID)
}

func TestAuditedOrderExecutor_NilDBPool(t *testing.T) {
	inner := new(mockAuditInner)
	inner.On("IsPaperTrading").Return(false)
	details := TradeDetails{Exchange: "bitget", Symbol: "BTC/USDT", Side: "buy", OrderType: "market", AmountUSDT: decimal.NewFromFloat(100)}
	inner.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(d TradeDetails) bool { return d.Symbol == "BTC/USDT" })).Return("order-nil-db", nil)
	auditExec := NewAuditedOrderExecutor(inner, nil)
	orderID, err := auditExec.PlaceOrderWithDetails(context.Background(), details)
	assert.NoError(t, err, "nil DB should not block order")
	assert.Equal(t, "order-nil-db", orderID)
}

func TestAuditedOrderExecutor_NilInnerExecutor(t *testing.T) {
	auditExec := NewAuditedOrderExecutor(nil, nil)
	_, err := auditExec.PlaceOrderWithDetails(context.Background(), TradeDetails{Exchange: "bitget", Symbol: "BTC/USDT", AmountUSDT: decimal.NewFromFloat(100)})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no inner executor")
}

func TestAuditedOrderExecutor_PassthroughReadMethods(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	inner := new(mockAuditInner)
	auditExec := NewAuditedOrderExecutor(inner, dbPool)
	expectedOrders := []map[string]interface{}{{"id": "order-1"}}
	inner.On("GetOpenOrders", mock.Anything, "bitget", "BTC/USDT").Return(expectedOrders, nil)
	orders, err := auditExec.GetOpenOrders(context.Background(), "bitget", "BTC/USDT")
	assert.NoError(t, err)
	assert.Equal(t, expectedOrders, orders)
	inner.On("GetClosedOrders", mock.Anything, "bitget", "BTC/USDT", 10).Return(expectedOrders, nil)
	closed, err := auditExec.GetClosedOrders(context.Background(), "bitget", "BTC/USDT", 10)
	assert.NoError(t, err)
	assert.Equal(t, expectedOrders, closed)
	inner.On("CancelOrder", mock.Anything, "bitget", "order-1").Return(nil)
	err = auditExec.CancelOrder(context.Background(), "bitget", "order-1")
	assert.NoError(t, err)
	inner.AssertExpectations(t)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestAuditedOrderExecutor_InterfaceCompliance(t *testing.T) {
	var _ ScalpingOrderExecutor = (*AuditedOrderExecutor)(nil)
}

func TestAuditedOrderExecutor_SafetySnapshotNotWrittenForNilTradeDetails(t *testing.T) {
	inner := new(mockAuditInner)
	inner.safetyAllowed = true
	inner.On("IsPaperTrading").Return(false)
	auditExec := NewAuditedOrderExecutor(inner, nil)
	auditExec.captureSafetySnapshot(context.Background(), nil)
}

func TestAuditedOrderExecutor_SafetySnapshotNotWrittenForZeroSymbol(t *testing.T) {
	inner := new(mockAuditInner)
	inner.safetyAllowed = true
	inner.On("IsPaperTrading").Return(false)
	auditExec := NewAuditedOrderExecutor(inner, nil)
	details := &TradeDetails{Exchange: "bitget", Symbol: "", AmountUSDT: decimal.NewFromFloat(100)}
	auditExec.captureSafetySnapshot(context.Background(), details)
	assert.Empty(t, details.PreTradeSafetyStatus)
}

func TestAuditedOrderExecutor_StopLossTakeProfitAreCaptured(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	inner := new(mockAuditInner)
	inner.On("IsPaperTrading").Return(false)
	stopLoss := decimal.NewFromFloat(48500.0)
	takeProfit := decimal.NewFromFloat(51500.0)
	entryPrice := decimal.NewFromFloat(50000.0)
	details := TradeDetails{Exchange: "bitget", Symbol: "BTC/USDT", Side: "buy", OrderType: "market", AmountUSDT: decimal.NewFromFloat(200), StopLoss: &stopLoss, TakeProfit: &takeProfit, EntryPrice: &entryPrice}
	inner.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(d TradeDetails) bool { return d.StopLoss != nil && d.StopLoss.Equal(stopLoss) })).Return("order-sl-tp", nil)
	auditExec := NewAuditedOrderExecutor(inner, dbPool)
	mockDB.ExpectExec("INSERT INTO trade_audit_log").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "BTC/USDT", "bitget", "buy", "market", "200", "50000", "48500", "51500", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "pending", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mockDB.ExpectExec("UPDATE trade_audit_log").WithArgs("order-sl-tp", "placed", pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	orderID, err := auditExec.PlaceOrderWithDetails(context.Background(), details)
	assert.NoError(t, err)
	assert.Equal(t, "order-sl-tp", orderID)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestAuditedOrderExecutor_LongReasoningTruncation(t *testing.T) {
	dbPool, mockDB := setupAuditTestDB(t)
	defer mockDB.Close()
	inner := new(mockAuditInner)
	inner.On("IsPaperTrading").Return(false)
	longReasoning := strings.Repeat("Analysis is showing strong buy signals. ", 100)
	details := TradeDetails{Exchange: "bitget", Symbol: "BTC/USDT", Side: "buy", OrderType: "market", AmountUSDT: decimal.NewFromFloat(100), Reasoning: longReasoning}
	inner.On("PlaceOrderWithDetails", mock.Anything, mock.MatchedBy(func(d TradeDetails) bool { return d.Reasoning == longReasoning })).Return("order-long-reason", nil)
	auditExec := NewAuditedOrderExecutor(inner, dbPool)
	mockDB.ExpectExec("INSERT INTO trade_audit_log").WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), "BTC/USDT", "bitget", "buy", "market", "100", pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), longReasoning, pgxmock.AnyArg(), pgxmock.AnyArg(), "pending", pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mockDB.ExpectExec("UPDATE trade_audit_log").WithArgs("order-long-reason", "placed", pgxmock.AnyArg(), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	orderID, err := auditExec.PlaceOrderWithDetails(context.Background(), details)
	assert.NoError(t, err)
	assert.Equal(t, "order-long-reason", orderID)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestAuditedOrderExecutor_NilInnerReturnsErrorAcrossAllMethods(t *testing.T) {
	e := NewAuditedOrderExecutor(nil, nil)
	ctx := context.Background()

	_, err := e.PlaceOrder(ctx, "bitget", "BTC/USDT", "buy", "market", decimal.NewFromInt(100), nil)
	assert.ErrorIs(t, err, ErrNoInnerExecutor)

	_, err = e.PlaceOrderWithDetails(ctx, TradeDetails{Exchange: "bitget", Symbol: "BTC/USDT"})
	assert.ErrorIs(t, err, ErrNoInnerExecutor)

	_, err = e.PlaceRiskReductionOrderWithDetails(ctx, TradeDetails{Exchange: "bitget", Symbol: "BTC/USDT"})
	assert.ErrorIs(t, err, ErrNoInnerExecutor)

	_, err = e.GetOpenOrders(ctx, "bitget", "BTC/USDT")
	assert.ErrorIs(t, err, ErrNoInnerExecutor)

	_, err = e.GetClosedOrders(ctx, "bitget", "BTC/USDT", 10)
	assert.ErrorIs(t, err, ErrNoInnerExecutor)

	err = e.CancelOrder(ctx, "bitget", "order-123")
	assert.ErrorIs(t, err, ErrNoInnerExecutor)

	assert.True(t, e.IsPaperTrading(), "nil inner should report paper trading (safe default)")
}
