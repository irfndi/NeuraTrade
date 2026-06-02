package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutonomousHandler_BuildLifecyclePerformanceSummary(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomous-lifecycle-performance.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := services.NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	ctx := context.Background()
	require.NoError(t, store.RecordClosedOrder(ctx, services.LifecycleCloseRecord{
		OrderID:     "ord-win",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "ADA/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(2),
		EntryPrice:  decimal.NewFromFloat(1),
		ExitPrice:   decimal.NewFromFloat(1.05),
		RealizedPnL: decimal.NewFromFloat(0.1),
		ClosedAt:    now.Add(-30 * time.Minute),
	}))
	require.NoError(t, store.RecordClosedOrder(ctx, services.LifecycleCloseRecord{
		OrderID:     "ord-loss",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "DOGE/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(5),
		EntryPrice:  decimal.NewFromFloat(0.2),
		ExitPrice:   decimal.NewFromFloat(0.19),
		RealizedPnL: decimal.NewFromFloat(-0.05),
		ClosedAt:    now.Add(-15 * time.Minute),
	}))

	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetLifecycleStore(store)

	summary, ok := handler.buildLifecyclePerformanceSummary(ctx, "chat-1", "24h")
	require.True(t, ok)
	assert.Equal(t, "24h", summary.Timeframe)
	assert.Equal(t, "0.05", summary.PnL)
	assert.Equal(t, "50.0%", summary.WinRate)
	assert.NotEqual(t, "N/A", summary.Sharpe)
	assert.NotEqual(t, "N/A", summary.Sortino)
	assert.NotEqual(t, "N/A", summary.Drawdown)
	assert.Equal(t, 2, summary.Trades)
	assert.Contains(t, summary.Note, "Exchange-reconciled")
}

func TestAutonomousHandler_BuildLifecyclePerformanceSummary_UsesNetReturnsForRiskMetrics(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomous-lifecycle-performance-net-risk.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := services.NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	now := time.Now().UTC()
	ctx := context.Background()
	require.NoError(t, store.RecordClosedOrder(ctx, services.LifecycleCloseRecord{
		OrderID:     "ord-net-win",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "ADA/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(2),
		EntryPrice:  decimal.NewFromFloat(1),
		ExitPrice:   decimal.NewFromFloat(1.05),
		RealizedPnL: decimal.NewFromFloat(0.10),
		Fees:        decimal.NewFromFloat(0.01),
		ClosedAt:    now.Add(-30 * time.Minute),
	}))
	require.NoError(t, store.RecordClosedOrder(ctx, services.LifecycleCloseRecord{
		OrderID:     "ord-fee-flipped-loss",
		ChatID:      "chat-1",
		Exchange:    "bitget",
		Symbol:      "DOGE/USDT",
		Side:        "buy",
		MarketType:  "futures",
		Filled:      decimal.NewFromFloat(5),
		EntryPrice:  decimal.NewFromFloat(0.2),
		ExitPrice:   decimal.NewFromFloat(0.202),
		RealizedPnL: decimal.NewFromFloat(0.01),
		Fees:        decimal.NewFromFloat(0.03),
		ClosedAt:    now.Add(-15 * time.Minute),
	}))

	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetLifecycleStore(store)

	netSeries, err := store.GetNetRealizedReturnSeries(ctx, "chat-1", "", now.Add(-24*time.Hour))
	require.NoError(t, err)
	grossSeries, err := store.GetGrossRealizedReturnSeries(ctx, "chat-1", "", now.Add(-24*time.Hour))
	require.NoError(t, err)

	netRisk := services.ComputeRiskAdjustedMetrics(netSeries)
	grossRisk := services.ComputeRiskAdjustedMetrics(grossSeries)
	require.NotEqual(t, formatRiskRatio(netRisk.Sharpe, netRisk.SampleSize), formatRiskRatio(grossRisk.Sharpe, grossRisk.SampleSize))

	summary, ok := handler.buildLifecyclePerformanceSummary(ctx, "chat-1", "24h")
	require.True(t, ok)
	assert.Equal(t, formatRiskRatio(netRisk.Sharpe, netRisk.SampleSize), summary.Sharpe)
	assert.Equal(t, formatRiskRatio(netRisk.Sortino, netRisk.SampleSize), summary.Sortino)
	assert.Equal(t, formatDrawdown(netRisk.MaxDrawdown, netRisk.SampleSize), summary.Drawdown)
}

func TestSummarizeQuestInvestigation_UsesExecutedCyclesForWinRate(t *testing.T) {
	totalCycles, executedCycles, winRate := summarizeQuestInvestigation([]services.RegimeOutcomeStat{
		{Regime: "trend", Count: 4, Wins: 3},
		{Regime: "range", Count: 6, Wins: 4},
	}, 100)

	assert.Equal(t, 100, totalCycles)
	assert.Equal(t, 10, executedCycles)
	assert.InDelta(t, 0.7, winRate, 1e-9)
}

func TestAutonomousHandler_BuildLifecyclePerformanceSummary_NoVisibleTradesReturnsFalse(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomous-lifecycle-performance-empty.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := services.NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetLifecycleStore(store)

	summary, ok := handler.buildLifecyclePerformanceSummary(context.Background(), "chat-1", "24h")
	assert.False(t, ok)
	assert.Equal(t, PerformanceSummaryResponse{}, summary)
}

func TestPerformanceResponses_OmitZeroTrades(t *testing.T) {
	summaryPayload, err := json.Marshal(PerformanceSummaryResponse{
		Timeframe: "24h",
		PnL:       "0",
	})
	require.NoError(t, err)
	assert.NotContains(t, string(summaryPayload), "\"trades\"")

	breakdownPayload, err := json.Marshal(PerformanceBreakdownResponse{
		Timeframe: "24h",
		Overall: PerformanceSummaryResponse{
			Timeframe: "24h",
			PnL:       "0",
		},
		Strategies: []StrategyPerformance{{
			Strategy: "scalping",
			PnL:      "0",
		}},
	})
	require.NoError(t, err)
	assert.NotContains(t, string(breakdownPayload), "\"trades\"")
}

func TestAutonomousHandler_EnrichPortfolioWithLifecycle(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomous-lifecycle-portfolio.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store, err := services.NewTradingLifecycleStore(sqliteDB, nil)
	require.NoError(t, err)

	ctx := context.Background()
	require.NoError(t, store.RecordOrderExecution(ctx, services.LifecycleExecutionRecord{
		OrderID:    "open-ord-1",
		ChatID:     "chat-1",
		Exchange:   "bitget",
		Symbol:     "ADA/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		Amount:     decimal.NewFromFloat(5),
		EntryPrice: decimal.NewFromFloat(1),
		StopLoss:   decimal.NewFromFloat(0.99),
		TakeProfit: decimal.NewFromFloat(1.02),
		OpenedAt:   time.Now().UTC().Add(-5 * time.Minute),
	}))

	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetLifecycleStore(store)

	response := PortfolioResponse{
		TotalEquity: "0.00",
		Positions:   []PortfolioPosition{},
	}
	handler.enrichPortfolioWithLifecycle(ctx, "chat-1", &response)

	assert.Equal(t, 1, response.OpenOrders)
	require.Len(t, response.Positions, 1)
	assert.Equal(t, "ADA/USDT", response.Positions[0].Symbol)
	assert.Equal(t, "buy", response.Positions[0].Side)
	assert.Contains(t, response.Note, "lifecycle store")
}

func setupAutonomousLiquidationDB(t *testing.T) *database.SQLiteDB {
	t.Helper()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "autonomous-liquidate.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })

	ctx := context.Background()
	_, err = sqliteDB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS trading_positions (
			position_id TEXT PRIMARY KEY,
			order_id TEXT NOT NULL,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			size NUMERIC NOT NULL,
			entry_price NUMERIC NOT NULL,
			status TEXT NOT NULL,
			opened_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS trading_orders (
			order_id TEXT PRIMARY KEY,
			position_id TEXT NOT NULL,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			type TEXT NOT NULL,
			amount NUMERIC NOT NULL,
			price NUMERIC NOT NULL,
			status TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL
		)`)
	require.NoError(t, err)
	return sqliteDB
}

func insertOpenPosition(t *testing.T, db *database.SQLiteDB, positionID, orderID, symbol, side string, openedAt time.Time) {
	t.Helper()
	ctx := context.Background()
	_, err := db.Exec(ctx, `
		INSERT INTO trading_positions (position_id, order_id, exchange, symbol, side, size, entry_price, status, opened_at, updated_at)
		VALUES (?, ?, 'bitget', ?, ?, 1, 100, 'OPEN', ?, ?)
	`, positionID, orderID, symbol, side, openedAt, openedAt)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `
		INSERT INTO trading_orders (order_id, position_id, exchange, symbol, side, type, amount, price, status, created_at, updated_at)
		VALUES (?, ?, 'bitget', ?, ?, 'market', 1, 100, 'OPEN', ?, ?)
	`, orderID, positionID, symbol, side, openedAt, openedAt)
	require.NoError(t, err)
}

func positionStatus(t *testing.T, db *database.SQLiteDB, positionID string) string {
	t.Helper()
	var status string
	require.NoError(t, db.QueryRow(context.Background(), `SELECT status FROM trading_positions WHERE position_id = ?`, positionID).Scan(&status))
	return status
}

func orderStatus(t *testing.T, db *database.SQLiteDB, orderID string) string {
	t.Helper()
	var status string
	require.NoError(t, db.QueryRow(context.Background(), `SELECT status FROM trading_orders WHERE order_id = ?`, orderID).Scan(&status))
	return status
}

func TestAutonomousHandler_Liquidate_RequiresDBPool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAutonomousHandler(nil, nil, nil)
	router := gin.New()
	router.POST("/liquidate", handler.Liquidate)

	req := httptest.NewRequest(http.MethodPost, "/liquidate", strings.NewReader(`{"chat_id":"c1","symbol":"DOGE/USDT"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "Database pool not wired")
}

func TestAutonomousHandler_Liquidate_NoOpenPositionReturnsSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAutonomousLiquidationDB(t)
	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetDBPool(db)
	router := gin.New()
	router.POST("/liquidate", handler.Liquidate)

	req := httptest.NewRequest(http.MethodPost, "/liquidate", strings.NewReader(`{"chat_id":"c1","symbol":"DOGE/USDT"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "No open position")
	var response LiquidationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.True(t, response.Ok)
	assert.Equal(t, 0, response.LiquidatedCount)
}

func TestAutonomousHandler_Liquidate_MarksOpenPositionAsLiquidated(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAutonomousLiquidationDB(t)
	insertOpenPosition(t, db, "pos-1", "ord-1", "DOGE/USDT", "buy", time.Now().UTC().Add(-time.Minute))
	insertOpenPosition(t, db, "pos-2", "ord-2", "DOGE/USDT", "buy", time.Now().UTC())

	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetDBPool(db)
	router := gin.New()
	router.POST("/liquidate", handler.Liquidate)

	req := httptest.NewRequest(http.MethodPost, "/liquidate", strings.NewReader(`{"chat_id":"c1","symbol":"DOGE/USDT"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	t.Logf("response body: %s", w.Body.String())
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"liquidated_count":1`)

	assert.Equal(t, "LIQUIDATED", positionStatus(t, db, "pos-1"))
	assert.Equal(t, "CLOSED", orderStatus(t, db, "ord-1"))
	assert.Equal(t, "OPEN", positionStatus(t, db, "pos-2"), "oldest matching position should be liquidated first; pos-2 must remain open")
}

func TestAutonomousHandler_LiquidateAll_LiquidatesEverything(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAutonomousLiquidationDB(t)
	insertOpenPosition(t, db, "pos-1", "ord-1", "DOGE/USDT", "buy", time.Now().UTC().Add(-2*time.Minute))
	insertOpenPosition(t, db, "pos-2", "ord-2", "ADA/USDT", "buy", time.Now().UTC().Add(-time.Minute))
	insertOpenPosition(t, db, "pos-3", "ord-3", "BTC/USDT", "sell", time.Now().UTC())

	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetDBPool(db)
	router := gin.New()
	router.POST("/liquidate_all", handler.LiquidateAll)

	req := httptest.NewRequest(http.MethodPost, "/liquidate_all", strings.NewReader(`{"chat_id":"c1"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"liquidated_count":3`)

	assert.Equal(t, "LIQUIDATED", positionStatus(t, db, "pos-1"))
	assert.Equal(t, "LIQUIDATED", positionStatus(t, db, "pos-2"))
	assert.Equal(t, "LIQUIDATED", positionStatus(t, db, "pos-3"))
	assert.Equal(t, "CLOSED", orderStatus(t, db, "ord-1"))
	assert.Equal(t, "CLOSED", orderStatus(t, db, "ord-2"))
	assert.Equal(t, "CLOSED", orderStatus(t, db, "ord-3"))
}

func TestAutonomousHandler_Liquidate_SkipsDBMarkWhenExchangeCloseFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAutonomousLiquidationDB(t)
	insertOpenPosition(t, db, "pos-1", "ord-1", "DOGE/USDT", "buy", time.Now().UTC().Add(-time.Minute))

	stub := &stubExchangeLiquidator{err: errors.New("exchange unreachable")}
	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetDBPool(db)
	handler.SetExchangeLiquidator(stub)
	router := gin.New()
	router.POST("/liquidate", handler.Liquidate)

	req := httptest.NewRequest(http.MethodPost, "/liquidate", strings.NewReader(`{"chat_id":"c1","symbol":"DOGE/USDT"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)
	assert.Contains(t, w.Body.String(), "Exchange close failed")
	assert.Equal(t, "OPEN", positionStatus(t, db, "pos-1"), "position must remain OPEN when exchange close fails")
	assert.Equal(t, "OPEN", orderStatus(t, db, "ord-1"), "order must remain OPEN when exchange close fails")
	assert.Equal(t, "bitget", stub.calledExchange)
	assert.Equal(t, "ord-1", stub.calledOrderID)
	assert.Equal(t, "pos-1", stub.calledPositionID)
}

func TestAutonomousHandler_Liquidate_MarksDBOnExchangeCloseSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAutonomousLiquidationDB(t)
	insertOpenPosition(t, db, "pos-1", "ord-1", "DOGE/USDT", "buy", time.Now().UTC().Add(-time.Minute))

	stub := &stubExchangeLiquidator{err: nil}
	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetDBPool(db)
	handler.SetExchangeLiquidator(stub)
	router := gin.New()
	router.POST("/liquidate", handler.Liquidate)

	req := httptest.NewRequest(http.MethodPost, "/liquidate", strings.NewReader(`{"chat_id":"c1","symbol":"DOGE/USDT"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "LIQUIDATED", positionStatus(t, db, "pos-1"))
	assert.Equal(t, "CLOSED", orderStatus(t, db, "ord-1"))
	assert.Equal(t, "bitget", stub.calledExchange)
	assert.Equal(t, "ord-1", stub.calledOrderID)
	assert.Equal(t, "pos-1", stub.calledPositionID)
}

func TestAutonomousHandler_LiquidateAll_SkipsDBMarkWhenExchangeCloseFails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAutonomousLiquidationDB(t)
	insertOpenPosition(t, db, "pos-1", "ord-1", "DOGE/USDT", "buy", time.Now().UTC().Add(-2*time.Minute))
	insertOpenPosition(t, db, "pos-2", "ord-2", "ADA/USDT", "buy", time.Now().UTC().Add(-time.Minute))

	stub := &stubExchangeLiquidator{err: errors.New("exchange unreachable")}
	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetDBPool(db)
	handler.SetExchangeLiquidator(stub)
	router := gin.New()
	router.POST("/liquidate_all", handler.LiquidateAll)

	req := httptest.NewRequest(http.MethodPost, "/liquidate_all", strings.NewReader(`{"chat_id":"c1"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Liquidated 0 of 2")
	assert.Equal(t, "OPEN", positionStatus(t, db, "pos-1"), "position must remain OPEN when exchange close fails")
	assert.Equal(t, "OPEN", orderStatus(t, db, "ord-1"), "order must remain OPEN when exchange close fails")
	assert.Equal(t, "OPEN", positionStatus(t, db, "pos-2"))
	assert.Equal(t, "OPEN", orderStatus(t, db, "ord-2"))
}

func TestAutonomousHandler_LiquidateAll_MarksDBOnExchangeCloseSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := setupAutonomousLiquidationDB(t)
	insertOpenPosition(t, db, "pos-1", "ord-1", "DOGE/USDT", "buy", time.Now().UTC().Add(-2*time.Minute))
	insertOpenPosition(t, db, "pos-2", "ord-2", "ADA/USDT", "buy", time.Now().UTC().Add(-time.Minute))

	stub := &stubExchangeLiquidator{err: nil}
	handler := NewAutonomousHandler(nil, nil, nil)
	handler.SetDBPool(db)
	handler.SetExchangeLiquidator(stub)
	router := gin.New()
	router.POST("/liquidate_all", handler.LiquidateAll)

	req := httptest.NewRequest(http.MethodPost, "/liquidate_all", strings.NewReader(`{"chat_id":"c1"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"liquidated_count":2`)
	assert.Equal(t, "LIQUIDATED", positionStatus(t, db, "pos-1"))
	assert.Equal(t, "CLOSED", orderStatus(t, db, "ord-1"))
	assert.Equal(t, "LIQUIDATED", positionStatus(t, db, "pos-2"))
	assert.Equal(t, "CLOSED", orderStatus(t, db, "ord-2"))
}

func TestReadinessChecker_CheckDatabase_WithoutDBPoolReportsWarning(t *testing.T) {
	rc := NewReadinessChecker()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	result := rc.checkDatabase(c)
	assert.Equal(t, "warning", result.Status)
	assert.Contains(t, result.Message, "Database pool not configured")
}

func TestReadinessChecker_CheckDatabase_WithHealthyDBReportsHealthy(t *testing.T) {
	db := setupAutonomousLiquidationDB(t)
	rc := NewReadinessChecker()
	rc.SetDBPool(db)
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	result := rc.checkDatabase(c)
	assert.Equal(t, "healthy", result.Status)
	assert.Contains(t, result.Message, "Database ping successful")
}

func TestReadinessChecker_CheckDatabase_WithBrokenDBReportsCritical(t *testing.T) {
	rc := NewReadinessChecker()
	rc.SetDBPool(closedDBPool{})
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	result := rc.checkDatabase(c)
	assert.Equal(t, "critical", result.Status)
	assert.Contains(t, result.Message, "Database ping failed")
}

func TestReadinessChecker_CheckRedis_WithoutClientReportsWarning(t *testing.T) {
	rc := NewReadinessChecker()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	result := rc.checkRedis(c)
	assert.Equal(t, "warning", result.Status)
	assert.Contains(t, result.Message, "Redis client not configured")
}

func TestReadinessChecker_CheckRedis_WithUnreachableRedisReportsWarning(t *testing.T) {
	rc := NewReadinessChecker()
	rc.SetRedisClient(redis.NewClient(&redis.Options{Addr: "127.0.0.1:1", DialTimeout: 100 * time.Millisecond}))
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	result := rc.checkRedis(c)
	assert.Equal(t, "warning", result.Status, "Redis outage should not block autonomous mode (degraded-only check)")
	assert.Contains(t, result.Message, "Redis ping failed")
}

func TestReadinessChecker_CheckRiskLimits_WithoutPortfolioSafetyReportsWarning(t *testing.T) {
	rc := NewReadinessChecker()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	result := rc.checkRiskLimits(c)
	assert.Equal(t, "warning", result.Status)
	assert.Contains(t, result.Message, "PortfolioSafetyService not wired")
}

func TestReadinessChecker_CheckRiskLimits_WithDefaultConfigReportsHealthy(t *testing.T) {
	rc := NewReadinessChecker()
	rc.SetPortfolioSafety(services.NewPortfolioSafetyService(
		services.DefaultPortfolioSafetyConfig(),
		nil, nil, nil, nil, nil, nil, nil, nil,
	))
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/", nil)

	result := rc.checkRiskLimits(c)
	assert.Equal(t, "healthy", result.Status)
	assert.Contains(t, result.Message, "Risk limits configured")
	assert.Contains(t, result.Details, "max_position_size_pct")
	assert.Contains(t, result.Details, "max_exposure_pct")
}

type stubExchangeLiquidator struct {
	err              error
	calledExchange   string
	calledOrderID    string
	calledPositionID string
	calledSymbol     string
}

func (s *stubExchangeLiquidator) ClosePosition(_ context.Context, exchangeID, orderID, positionID, symbol string) error {
	s.calledExchange = exchangeID
	s.calledOrderID = orderID
	s.calledPositionID = positionID
	s.calledSymbol = symbol
	return s.err
}

// closedDBPool simulates a DBPool whose Exec always errors.
type closedDBPool struct{}

func (closedDBPool) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	return nil, errors.New("connection is closed")
}
func (closedDBPool) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return nilRow{err: errors.New("connection is closed")}
}
func (closedDBPool) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	return nil, errors.New("connection is closed")
}
func (closedDBPool) Begin(ctx context.Context) (database.Tx, error) {
	return nil, errors.New("connection is closed")
}

type nilRow struct{ err error }

func (r nilRow) Scan(dest ...any) error { return r.err }

func TestAutonomousHandler_ConnectExchange_RequiresDBPool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAutonomousHandler(nil, nil, nil)
	router := gin.New()
	router.POST("/connect", h.ConnectExchange)

	req := httptest.NewRequest(http.MethodPost, "/connect",
		strings.NewReader(`{"chat_id":"c1","exchange":"bitget"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
}

func TestAutonomousHandler_AddWallet_RejectsInvalidAddress(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "add-wallet-bad.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })

	h := NewAutonomousHandler(nil, nil, nil)
	h.SetDBPool(sqliteDB)
	router := gin.New()
	router.POST("/add", h.AddWallet)

	req := httptest.NewRequest(http.MethodPost, "/add",
		strings.NewReader(`{"chat_id":"c1","wallet_address":"not-an-address"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAutonomousHandler_GetWallets_RequiresChatID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewAutonomousHandler(nil, nil, nil)
	router := gin.New()
	router.GET("/wallets", h.GetWallets)

	req := httptest.NewRequest(http.MethodGet, "/wallets", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestAutonomousHandler_GetWallets_EmptyForUnknownChat(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "wallets-empty.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })

	ctx := context.Background()
	_, err = sqliteDB.Exec(ctx, `CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		telegram_chat_id TEXT(50)
	)`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `CREATE TABLE IF NOT EXISTS wallets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		chain TEXT NOT NULL,
		address TEXT NOT NULL,
		wallet_type TEXT NOT NULL,
		label TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)

	h := NewAutonomousHandler(nil, nil, nil)
	h.SetDBPool(sqliteDB)
	router := gin.New()
	router.GET("/wallets", h.GetWallets)

	req := httptest.NewRequest(http.MethodGet, "/wallets?chat_id=99999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"wallets":[]`)
}

func TestAutonomousHandler_ConnectExchange_RequiresUserWithKeys(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "connect-exchange-no-user.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })

	ctx := context.Background()
	_, err = sqliteDB.Exec(ctx, `CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		telegram_chat_id TEXT(50)
	)`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `CREATE TABLE IF NOT EXISTS exchange_api_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		exchange_name TEXT NOT NULL,
		key_name TEXT NOT NULL,
		encrypted_key TEXT NOT NULL,
		encrypted_secret TEXT NOT NULL,
		encrypted_passphrase TEXT NOT NULL DEFAULT '',
		permissions TEXT DEFAULT '["read"]',
		is_active INTEGER DEFAULT 1,
		last_used_at TEXT,
		expires_at TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, exchange_name, key_name)
	)`)
	require.NoError(t, err)

	h := NewAutonomousHandler(nil, nil, nil)
	h.SetDBPool(sqliteDB)
	router := gin.New()
	router.POST("/connect", h.ConnectExchange)

	req := httptest.NewRequest(http.MethodPost, "/connect",
		strings.NewReader(`{"chat_id":"no-such-chat","exchange":"bitget"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestAutonomousHandler_ConnectExchange_SuccessWithKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "connect-exchange-ok.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })

	ctx := context.Background()
	_, err = sqliteDB.Exec(ctx, `CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		telegram_chat_id TEXT(50)
	)`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `INSERT INTO users (id, email, password_hash, telegram_chat_id) VALUES ('u1', 'e@x', 'h', 'chat-1')`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `CREATE TABLE IF NOT EXISTS exchange_api_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		exchange_name TEXT NOT NULL,
		key_name TEXT NOT NULL,
		encrypted_key TEXT NOT NULL,
		encrypted_secret TEXT NOT NULL,
		encrypted_passphrase TEXT NOT NULL DEFAULT '',
		permissions TEXT DEFAULT '["read"]',
		is_active INTEGER DEFAULT 1,
		last_used_at TEXT,
		expires_at TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(user_id, exchange_name, key_name)
	)`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `INSERT INTO exchange_api_keys
		(id, user_id, exchange_name, key_name, encrypted_key, encrypted_secret, is_active)
		VALUES ('k1', 'u1', 'bitget', 'main', 'k', 's', 1)`)
	require.NoError(t, err)

	h := NewAutonomousHandler(nil, nil, nil)
	h.SetDBPool(sqliteDB)
	router := gin.New()
	router.POST("/connect", h.ConnectExchange)

	req := httptest.NewRequest(http.MethodPost, "/connect",
		strings.NewReader(`{"chat_id":"chat-1","exchange":"bitget","account_label":"trading"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"ok":true`)
	assert.Contains(t, w.Body.String(), `trading`)
}

func TestAutonomousHandler_AddRemoveWallet_RoundTrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "wallet-roundtrip.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqliteDB.Close() })

	ctx := context.Background()
	_, err = sqliteDB.Exec(ctx, `CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		email TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		telegram_chat_id TEXT(50)
	)`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `INSERT INTO users (id, email, password_hash, telegram_chat_id) VALUES ('u1', 'e@x', 'h', 'chat-1')`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `CREATE TABLE IF NOT EXISTS wallets (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		chain TEXT NOT NULL,
		address TEXT NOT NULL,
		wallet_type TEXT NOT NULL,
		label TEXT,
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	require.NoError(t, err)

	h := NewAutonomousHandler(nil, nil, nil)
	h.SetDBPool(sqliteDB)
	router := gin.New()
	router.POST("/add", h.AddWallet)
	router.POST("/remove", h.RemoveWallet)
	router.GET("/list", h.GetWallets)

	addr := "0xAbCdEf0123456789AbCdEf0123456789AbCdEf01"
	req := httptest.NewRequest(http.MethodPost, "/add",
		strings.NewReader(`{"chat_id":"chat-1","wallet_address":"`+addr+`","chain":"evm","label":"main"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	req = httptest.NewRequest(http.MethodGet, "/list?chat_id=chat-1", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "0xabcd")
	assert.Contains(t, w.Body.String(), "main")

	req = httptest.NewRequest(http.MethodPost, "/remove",
		strings.NewReader(`{"chat_id":"chat-1","wallet_id_or_address":"`+addr+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

	req = httptest.NewRequest(http.MethodGet, "/list?chat_id=chat-1", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"wallets":[]`)
}
