package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTradingHandlerWithMock creates a TradingHandler with a pgxmock for testing.
// It sets up expectations for the initialization SQL (CREATE TABLE, CREATE INDEX).
func setupTradingHandlerWithMock(t *testing.T) (*TradingHandler, pgxmock.PgxPoolIface) {
	t.Helper()

	mock, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")

	// Expect initialization queries
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS trading_orders").
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS trading_positions").
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_trading_orders_position_id").
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_trading_positions_symbol_status").
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	dbPool := database.NewMockDBPool(mock)
	h := NewTradingHandler(dbPool)

	return h, mock
}

// closeMock closes the mock and verifies expectations
func closeMock(t *testing.T, mock pgxmock.PgxPoolIface) {
	t.Helper()
	require.NoError(t, mock.ExpectationsWereMet(), "Mock expectations were not met")
	mock.Close()
}

func TestTradingHandlerPlaceOrderAndReadPositions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock := setupTradingHandlerWithMock(t)
	defer closeMock(t, mock)

	r := gin.New()
	r.POST("/trading/place_order", h.PlaceOrder)
	r.GET("/trading/positions", h.ListPositions)
	r.GET("/trading/positions/:position_id", h.GetPosition)

	body := `{"exchange":"binance","symbol":"BTC/USDT","side":"BUY","type":"LIMIT","amount":"0.5","price":"50000"}`

	// Expect order insert
	mock.ExpectExec("INSERT INTO trading_orders").
		WithArgs(
			pgxmock.AnyArg(),                   // order_id
			pgxmock.AnyArg(),                   // position_id
			"binance",                          // exchange
			"BTC/USDT",                         // symbol
			"BUY",                              // side
			"LIMIT",                            // type
			decimal.RequireFromString("0.5"),   // amount
			decimal.RequireFromString("50000"), // price
			"OPEN",                             // status
			pgxmock.AnyArg(),                   // created_at
			pgxmock.AnyArg(),                   // updated_at
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	// Expect position insert
	mock.ExpectExec("INSERT INTO trading_positions").
		WithArgs(
			pgxmock.AnyArg(),                   // position_id
			pgxmock.AnyArg(),                   // order_id
			"binance",                          // exchange
			"BTC/USDT",                         // symbol
			"BUY",                              // side
			decimal.RequireFromString("0.5"),   // size
			decimal.RequireFromString("50000"), // entry_price
			"OPEN",                             // status
			pgxmock.AnyArg(),                   // opened_at
			pgxmock.AnyArg(),                   // updated_at
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	var placeResp struct {
		Status string `json:"status"`
		Data   struct {
			Order struct {
				OrderID    string `json:"order_id"`
				PositionID string `json:"position_id"`
			} `json:"order"`
			Position struct {
				PositionID string `json:"position_id"`
				Status     string `json:"status"`
			} `json:"position"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &placeResp))
	assert.Equal(t, "success", placeResp.Status)
	require.NotEmpty(t, placeResp.Data.Order.OrderID)
	require.NotEmpty(t, placeResp.Data.Order.PositionID)
	assert.Equal(t, "OPEN", placeResp.Data.Position.Status)

	// List positions with OPEN filter
	mock.ExpectQuery("SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at FROM trading_positions").
		WithArgs("OPEN").
		WillReturnRows(pgxmock.NewRows([]string{
			"position_id", "order_id", "exchange", "symbol", "side", "size", "entry_price", "status", "opened_at", "updated_at",
		}).AddRow(
			placeResp.Data.Order.PositionID,
			placeResp.Data.Order.OrderID,
			"binance",
			"BTC/USDT",
			"BUY",
			decimal.RequireFromString("0.5"),
			decimal.RequireFromString("50000"),
			"OPEN",
			time.Now(),
			time.Now(),
		))

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/trading/positions?status=OPEN", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var listResp struct {
		Status string `json:"status"`
		Data   struct {
			Count int `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Equal(t, "success", listResp.Status)
	assert.Equal(t, 1, listResp.Data.Count)

	// Get single position
	mock.ExpectQuery("SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at FROM trading_positions").
		WithArgs(placeResp.Data.Order.PositionID).
		WillReturnRows(pgxmock.NewRows([]string{
			"position_id", "order_id", "exchange", "symbol", "side", "size", "entry_price", "status", "opened_at", "updated_at",
		}).AddRow(
			placeResp.Data.Order.PositionID,
			placeResp.Data.Order.OrderID,
			"binance",
			"BTC/USDT",
			"BUY",
			decimal.RequireFromString("0.5"),
			decimal.RequireFromString("50000"),
			"OPEN",
			time.Now(),
			time.Now(),
		))

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/trading/positions/"+placeResp.Data.Order.PositionID, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestTradingHandlerPlaceOrderValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock := setupTradingHandlerWithMock(t)
	defer closeMock(t, mock)

	r := gin.New()
	r.POST("/trading/place_order", h.PlaceOrder)

	// Invalid side "HOLD" should fail validation before any DB operations
	body := `{"exchange":"binance","symbol":"BTC/USDT","side":"HOLD","amount":"1"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestTradingHandlerCancelOrder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock := setupTradingHandlerWithMock(t)
	defer closeMock(t, mock)

	r := gin.New()
	r.POST("/trading/place_order", h.PlaceOrder)
	r.POST("/trading/cancel_order", h.CancelOrder)
	r.GET("/trading/positions", h.ListPositions)

	// Place order - expect inserts
	mock.ExpectExec("INSERT INTO trading_orders").
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), // order_id, position_id
			"binance", "ETH/USDT", "BUY", "MARKET",
			pgxmock.AnyArg(), pgxmock.AnyArg(), // amount, price
			"OPEN",
			pgxmock.AnyArg(), pgxmock.AnyArg(), // created_at, updated_at
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	mock.ExpectExec("INSERT INTO trading_positions").
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), // position_id, order_id
			"binance", "ETH/USDT", "BUY",
			pgxmock.AnyArg(), pgxmock.AnyArg(), // size, entry_price
			"OPEN",
			pgxmock.AnyArg(), pgxmock.AnyArg(), // opened_at, updated_at
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	w := httptest.NewRecorder()
	placeReq := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(`{"exchange":"binance","symbol":"ETH/USDT","side":"BUY","amount":"1"}`))
	placeReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, placeReq)
	require.Equal(t, http.StatusCreated, w.Code)

	var placeResp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &placeResp))
	data := placeResp["data"].(map[string]any)
	order := data["order"].(map[string]any)
	orderID := order["order_id"].(string)

	// Cancel order - expect select then updates
	mock.ExpectQuery("SELECT order_id, position_id, exchange, symbol, side, type, amount, price, status, created_at, updated_at FROM trading_orders").
		WithArgs(orderID).
		WillReturnRows(pgxmock.NewRows([]string{
			"order_id", "position_id", "exchange", "symbol", "side", "type", "amount", "price", "status", "created_at", "updated_at",
		}).AddRow(
			orderID,
			order["position_id"].(string),
			"binance",
			"ETH/USDT",
			"BUY",
			"MARKET",
			decimal.RequireFromString("1"),
			decimal.RequireFromString("0"),
			"OPEN",
			time.Now(),
			time.Now(),
		))

	mock.ExpectExec("UPDATE trading_orders").
		WithArgs(orderID, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	mock.ExpectExec("UPDATE trading_positions").
		WithArgs(order["position_id"].(string), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	w = httptest.NewRecorder()
	cancelBody := `{"order_id":"` + orderID + `"}`
	cancelReq := httptest.NewRequest(http.MethodPost, "/trading/cancel_order", bytes.NewBufferString(cancelBody))
	cancelReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, cancelReq)
	require.Equal(t, http.StatusOK, w.Code)

	// List positions with OPEN filter - should return empty
	mock.ExpectQuery("SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at FROM trading_positions").
		WithArgs("OPEN").
		WillReturnRows(pgxmock.NewRows([]string{
			"position_id", "order_id", "exchange", "symbol", "side", "size", "entry_price", "status", "opened_at", "updated_at",
		}))

	w = httptest.NewRecorder()
	listReq := httptest.NewRequest(http.MethodGet, "/trading/positions?status=OPEN", nil)
	r.ServeHTTP(w, listReq)
	require.Equal(t, http.StatusOK, w.Code)

	var listResp struct {
		Data struct {
			Count int `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listResp))
	assert.Equal(t, 0, listResp.Data.Count)
}

func TestTradingHandlerLiquidateAndLiquidateAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock := setupTradingHandlerWithMock(t)
	defer closeMock(t, mock)

	r := gin.New()
	r.POST("/trading/place_order", h.PlaceOrder)
	r.POST("/trading/liquidate", h.Liquidate)
	r.POST("/trading/liquidate_all", h.LiquidateAll)
	r.GET("/trading/positions", h.ListPositions)

	// Place first order (SOL/USDT) - order_id, position_id, exchange, symbol, side, type, amount, price, status, created_at, updated_at
	mock.ExpectExec("INSERT INTO trading_orders").
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), "binance", "SOL/USDT", "BUY", "MARKET",
			pgxmock.AnyArg(), pgxmock.AnyArg(), "OPEN",
			pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO trading_positions").
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), "binance", "SOL/USDT", "BUY",
			pgxmock.AnyArg(), pgxmock.AnyArg(), "OPEN",
			pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	first := httptest.NewRecorder()
	firstReq := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(`{"exchange":"binance","symbol":"SOL/USDT","side":"BUY","amount":"2"}`))
	firstReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(first, firstReq)
	require.Equal(t, http.StatusCreated, first.Code)

	// Place second order (ADA/USDT) - order_id, position_id, exchange, symbol, side, type, amount, price, status, created_at, updated_at
	mock.ExpectExec("INSERT INTO trading_orders").
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), "binance", "ADA/USDT", "BUY", "MARKET",
			pgxmock.AnyArg(), pgxmock.AnyArg(), "OPEN",
			pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mock.ExpectExec("INSERT INTO trading_positions").
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), "binance", "ADA/USDT", "BUY",
			pgxmock.AnyArg(), pgxmock.AnyArg(), "OPEN",
			pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	second := httptest.NewRecorder()
	secondReq := httptest.NewRequest(http.MethodPost, "/trading/place_order", bytes.NewBufferString(`{"exchange":"binance","symbol":"ADA/USDT","side":"BUY","amount":"3"}`))
	secondReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(second, secondReq)
	require.Equal(t, http.StatusCreated, second.Code)

	// Liquidate SOL/USDT
	mock.ExpectQuery("SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at FROM trading_positions").
		WithArgs("SOL/USDT").
		WillReturnRows(pgxmock.NewRows([]string{
			"position_id", "order_id", "exchange", "symbol", "side", "size", "entry_price", "status", "opened_at", "updated_at",
		}).AddRow(
			"pos-sol-1", "ord-sol-1", "binance", "SOL/USDT", "BUY",
			decimal.RequireFromString("2"), decimal.RequireFromString("0"), "OPEN",
			time.Now(), time.Now(),
		))
	mock.ExpectExec("UPDATE trading_positions").
		WithArgs("pos-sol-1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("UPDATE trading_orders").
		WithArgs("ord-sol-1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	liquidate := httptest.NewRecorder()
	liquidateReq := httptest.NewRequest(http.MethodPost, "/trading/liquidate", bytes.NewBufferString(`{"symbol":"SOL/USDT"}`))
	liquidateReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(liquidate, liquidateReq)
	require.Equal(t, http.StatusOK, liquidate.Code)

	// Liquidate all
	mock.ExpectQuery("SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at FROM trading_positions").
		WillReturnRows(pgxmock.NewRows([]string{
			"position_id", "order_id", "exchange", "symbol", "side", "size", "entry_price", "status", "opened_at", "updated_at",
		}).AddRow(
			"pos-ada-1", "ord-ada-1", "binance", "ADA/USDT", "BUY",
			decimal.RequireFromString("3"), decimal.RequireFromString("0"), "OPEN",
			time.Now(), time.Now(),
		))
	mock.ExpectExec("UPDATE trading_positions").
		WithArgs("pos-ada-1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	mock.ExpectExec("UPDATE trading_orders").
		WithArgs("ord-ada-1", pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	liquidateAll := httptest.NewRecorder()
	liquidateAllReq := httptest.NewRequest(http.MethodPost, "/trading/liquidate_all", nil)
	r.ServeHTTP(liquidateAll, liquidateAllReq)
	require.Equal(t, http.StatusOK, liquidateAll.Code)

	// Verify no open positions
	mock.ExpectQuery("SELECT position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at FROM trading_positions").
		WithArgs("OPEN").
		WillReturnRows(pgxmock.NewRows([]string{
			"position_id", "order_id", "exchange", "symbol", "side", "size", "entry_price", "status", "opened_at", "updated_at",
		}))

	openPositions := httptest.NewRecorder()
	openReq := httptest.NewRequest(http.MethodGet, "/trading/positions?status=OPEN", nil)
	r.ServeHTTP(openPositions, openReq)
	require.Equal(t, http.StatusOK, openPositions.Code)

	var listResp struct {
		Data struct {
			Count int `json:"count"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(openPositions.Body.Bytes(), &listResp))
	assert.Equal(t, 0, listResp.Data.Count)
}

func TestTradingHandlerCancelOrderNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, mock := setupTradingHandlerWithMock(t)
	defer closeMock(t, mock)

	r := gin.New()
	r.POST("/trading/cancel_order", h.CancelOrder)

	// Query returns no rows
	mock.ExpectQuery("SELECT order_id, position_id, exchange, symbol, side, type, amount, price, status, created_at, updated_at FROM trading_orders").
		WithArgs("ord-missing").
		WillReturnRows(pgxmock.NewRows([]string{
			"order_id", "position_id", "exchange", "symbol", "side", "type", "amount", "price", "status", "created_at", "updated_at",
		}))

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/trading/cancel_order", bytes.NewBufferString(`{"order_id":"ord-missing"}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestNewTradingHandler_RequiresDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// Test that NewTradingHandler panics without database
	require.Panics(t, func() {
		NewTradingHandler()
	}, "NewTradingHandler should panic when called without database")

	require.Panics(t, func() {
		NewTradingHandler(nil)
	}, "NewTradingHandler should panic when called with nil database")
}

func TestTradingHandler_WithDBPool(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mock, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mock.Close()

	// Expect initialization queries
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS trading_orders").
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS trading_positions").
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_trading_orders_position_id").
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_trading_positions_symbol_status").
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	dbPool := database.NewMockDBPool(mock)
	h := NewTradingHandler(dbPool)

	require.NotNil(t, h)
	require.NotNil(t, h.db)

	require.NoError(t, mock.ExpectationsWereMet())
}

// Test helper for capturing mock closer behavior
func TestMockCleanup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mock, err := pgxmock.NewPool()
	require.NoError(t, err)

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS trading_orders").
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS trading_positions").
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_trading_orders_position_id").
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_trading_positions_symbol_status").
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	dbPool := database.NewMockDBPool(mock)
	h := NewTradingHandler(dbPool)
	require.NotNil(t, h)

	// Verify we can call context with timeout
	ctx := context.Background()
	require.NotNil(t, ctx)

	require.NoError(t, mock.ExpectationsWereMet())
	mock.Close()
}
