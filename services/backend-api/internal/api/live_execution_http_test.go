package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/app/risk"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

type liveExecutionTestExecutor struct {
	placed atomic.Int32
}

func (e *liveExecutionTestExecutor) PlaceOrder(context.Context, string, string, string, string, decimal.Decimal, *decimal.Decimal) (string, error) {
	return "unused", nil
}

func (e *liveExecutionTestExecutor) PlaceOrderWithDetails(context.Context, services.TradeDetails) (string, error) {
	e.placed.Add(1)
	return "exchange-order-1", nil
}

func (e *liveExecutionTestExecutor) GetOpenOrders(context.Context, string, string) ([]map[string]interface{}, error) {
	return nil, nil
}

func (e *liveExecutionTestExecutor) GetClosedOrders(context.Context, string, string, int) ([]map[string]interface{}, error) {
	return nil, nil
}

func (e *liveExecutionTestExecutor) CancelOrder(context.Context, string, string) error { return nil }

func (e *liveExecutionTestExecutor) IsPaperTrading() bool { return false }

type liveExecutionTestLookup struct {
	order     ccxt.OrderResponse
	positions ccxt.PositionsResponse
	err       error
	ticker    ccxt.Ticker
}

func (l liveExecutionTestLookup) FetchOrder(context.Context, string, string, string) (*ccxt.OrderResponse, error) {
	if l.err != nil {
		return nil, l.err
	}
	return &l.order, nil
}

func (liveExecutionTestLookup) FetchBalance(context.Context, string) (*ccxt.BalanceResponse, error) {
	return &ccxt.BalanceResponse{
		Free:  map[string]decimal.Decimal{"USDT": decimal.RequireFromString("1000")},
		Total: map[string]decimal.Decimal{"USDT": decimal.RequireFromString("1000")},
	}, nil
}

func (l liveExecutionTestLookup) FetchPositions(context.Context, string) (*ccxt.PositionsResponse, error) {
	if l.err != nil {
		return nil, l.err
	}
	return &l.positions, nil
}

func (l liveExecutionTestLookup) FetchSingleTicker(_ context.Context, _, _ string) (ccxt.MarketPriceInterface, error) {
	ticker := l.ticker
	if ticker.Last.IsZero() {
		// Default to 100 to match the price used by existing tests.
		ticker.Last = decimal.RequireFromString("100")
	}
	return testMarketPrice{last: ticker.Last, ask: ticker.Ask, bid: ticker.Bid}, nil
}

// testMarketPrice is a minimal MarketPriceInterface implementation for tests.
type testMarketPrice struct {
	last decimal.Decimal
	ask  decimal.Decimal
	bid  decimal.Decimal
}

func (p testMarketPrice) GetPrice() decimal.Decimal      { return p.last }
func (p testMarketPrice) GetVolume() decimal.Decimal     { return decimal.Zero }
func (p testMarketPrice) GetTimestamp() time.Time        { return time.Now() }
func (p testMarketPrice) GetExchangeName() string        { return "bitget" }
func (p testMarketPrice) GetSymbol() string              { return "" }
func (p testMarketPrice) GetBid() decimal.Decimal        { return p.bid }
func (p testMarketPrice) GetAsk() decimal.Decimal        { return p.ask }
func (p testMarketPrice) GetHigh() decimal.Decimal       { return decimal.Zero }
func (p testMarketPrice) GetLow() decimal.Decimal        { return decimal.Zero }
func (p testMarketPrice) GetPriceChange24h() float64     { return 0 }

func TestLiveExecutionHTTPReturnsFuturesPositions(t *testing.T) {
	bridge, _ := newLiveExecutionHTTPTestBridge(t, liveExecutionTestLookup{positions: ccxt.PositionsResponse{
		Exchange: "bitget",
		Positions: []ccxt.Position{{
			ID:            "position-1",
			Symbol:        "BTC/USDT",
			Side:          "long",
			Size:          decimal.RequireFromString("0.1"),
			EntryPrice:    decimal.RequireFromString("70000"),
			MarkPrice:     decimal.RequireFromString("70100"),
			UnrealizedPnl: decimal.RequireFromString("10"),
			Leverage:      5,
			MarginMode:    "crossed",
		}},
		Count:     1,
		Timestamp: "2026-08-02T00:00:00Z",
	}})
	defer bridge.close()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("GET", "/api/v1/execution/futures/positions?exchange=bitget-futures", nil)
	bridge.getFuturesPositions(c)

	require.Equal(t, http.StatusOK, recorder.Code)
	var response liveFuturesPositionsResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, "bitget", response.Exchange)
	require.Len(t, response.Positions, 1)
	require.Equal(t, "0.1", response.Positions[0].Quantity)
	require.Equal(t, "USDT-FUTURES", response.Positions[0].ProductType)
}

func TestLiveExecutionHTTPRejectsOverLimitWithoutCallingExecutor(t *testing.T) {
	bridge, executor := newLiveExecutionHTTPTestBridge(t, liveExecutionTestLookup{})
	defer bridge.close()

	recorder := invokeLiveExecutionHandler(t, bridge, `{"intent_id":"reject-1","chat_id":"123","exchange":"bitget-futures","symbol":"BTC/USDT","side":"buy","size":"0.3","price":"100","portfolio_value":"100","confidence":"0.9"}`)

	require.Equal(t, 400, recorder.Code)
	require.Equal(t, int32(0), executor.placed.Load())
}

func TestLiveExecutionHTTPReturnsObservedFill(t *testing.T) {
	bridge, executor := newLiveExecutionHTTPTestBridge(t, liveExecutionTestLookup{order: ccxt.OrderResponse{
		Order: ccxt.Order{
			ID:            "exchange-order-1",
			ClientOrderID: "NTclient",
			Symbol:        "BTC/USDT",
			Type:          "market",
			Side:          "buy",
			Status:        "closed",
			Amount:        decimal.RequireFromString("0.1"),
			Filled:        decimal.RequireFromString("0.1"),
			Price:         decimal.RequireFromString("100"),
			Cost:          decimal.RequireFromString("10.01"),
			Fee:           decimal.RequireFromString("0.0123"),
		}},
	})
	defer bridge.close()

	recorder := invokeLiveExecutionHandler(t, bridge, `{"intent_id":"fill-1","chat_id":"123","exchange":"bitget-futures","symbol":"BTC/USDT","side":"buy","size":"0.1","price":"100","portfolio_value":"100","confidence":"0.9"}`)

	var response liveFuturesOrderResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 200, recorder.Code)
	require.Equal(t, int32(1), executor.placed.Load())
	require.Equal(t, "100.1", response.FilledPrice)
	require.Equal(t, "0.0123", response.Fee)
	require.Equal(t, "filled", response.Status)
}

func TestLiveExecutionHTTPServerReturnsObservedFill(t *testing.T) {
	bridge, executor := newLiveExecutionHTTPTestBridge(t, liveExecutionTestLookup{order: ccxt.OrderResponse{
		Order: ccxt.Order{
			ID:            "exchange-order-1",
			ClientOrderID: "NTclient",
			Symbol:        "BTC/USDT",
			Type:          "market",
			Side:          "buy",
			Status:        "closed",
			Amount:        decimal.RequireFromString("0.1"),
			Filled:        decimal.RequireFromString("0.1"),
			Price:         decimal.RequireFromString("100"),
			Cost:          decimal.RequireFromString("10.01"),
			Fee:           decimal.RequireFromString("0.0123"),
		}},
	})
	defer bridge.close()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/api/v1/execution/futures/order", bridge.placeFuturesOrder)
	server := httptest.NewServer(router)
	defer server.Close()

	request, err := http.NewRequest(
		http.MethodPost,
		server.URL+"/api/v1/execution/futures/order",
		bytes.NewBufferString(`{"intent_id":"wire-fill-1","chat_id":"123","exchange":"bitget-futures","symbol":"BTC/USDT","side":"buy","size":"0.1","price":"100","portfolio_value":"100","confidence":"0.9"}`),
	)
	require.NoError(t, err)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	require.NoError(t, err)
	defer response.Body.Close()

	var payload liveFuturesOrderResponse
	require.NoError(t, json.NewDecoder(response.Body).Decode(&payload))
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, int32(1), executor.placed.Load())
	require.Equal(t, "exchange-order-1", payload.OrderID)
	require.Equal(t, "0.0123", payload.Fee)
}

func TestLiveExecutionHTTPSurfacesUnconfirmedOrderAsAccepted(t *testing.T) {
	bridge, executor := newLiveExecutionHTTPTestBridge(t, liveExecutionTestLookup{err: errors.New("order lookup timed out")})
	defer bridge.close()

	recorder := invokeLiveExecutionHandler(t, bridge, `{"intent_id":"open-1","chat_id":"123","exchange":"bitget-futures","symbol":"BTC/USDT","side":"buy","size":"0.1","price":"100","portfolio_value":"100","confidence":"0.9"}`)

	var response liveFuturesOrderResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 202, recorder.Code)
	require.Equal(t, int32(1), executor.placed.Load())
	require.Equal(t, "open", response.Status)
	require.Equal(t, "0", response.FilledQty)
}

func TestLiveExecutionHTTPIsIdempotentForIntent(t *testing.T) {
	bridge, executor := newLiveExecutionHTTPTestBridge(t, liveExecutionTestLookup{order: ccxt.OrderResponse{
		Order: ccxt.Order{
			ID:     "exchange-order-1",
			Symbol: "BTC/USDT",
			Type:   "market",
			Side:   "buy",
			Status: "closed",
			Amount: decimal.RequireFromString("0.1"),
			Filled: decimal.RequireFromString("0.1"),
			Price:  decimal.RequireFromString("100"),
			Cost:   decimal.RequireFromString("10"),
		},
	}})
	defer bridge.close()
	body := `{"intent_id":"same-intent","chat_id":"123","exchange":"bitget-futures","symbol":"BTC/USDT","side":"buy","size":"0.1","price":"100","portfolio_value":"100","confidence":"0.9"}`

	first := invokeLiveExecutionHandler(t, bridge, body)
	second := invokeLiveExecutionHandler(t, bridge, body)

	require.Equal(t, 200, first.Code)
	require.Equal(t, 200, second.Code)
	require.Equal(t, int32(1), executor.placed.Load())
}

func TestLiveExecutionHTTPRejectsPriceDeviationFromMarket(t *testing.T) {
	bridge, executor := newLiveExecutionHTTPTestBridge(t, liveExecutionTestLookup{
		ticker: ccxt.Ticker{Last: decimal.RequireFromString("100")},
	})
	defer bridge.close()

	// Client price 200 deviates 100% from the market price of 100: the
	// order must be rejected before the executor is reached (the notional
	// gate must never trust a client-supplied price).
	recorder := invokeLiveExecutionHandler(t, bridge, `{"intent_id":"dev-1","chat_id":"123","exchange":"bitget-futures","symbol":"BTC/USDT","side":"buy","size":"0.1","price":"200","portfolio_value":"100","confidence":"0.9"}`)

	require.Equal(t, 400, recorder.Code)
	require.Equal(t, int32(0), executor.placed.Load())
}

func TestLiveExecutionHTTPNotionalUsesMarketPrice(t *testing.T) {
	// The bypass: client price within tolerance of market but low enough
	// that client-price notional looks safe while market-price notional
	// exceeds the limit. market=210, client=205 (2.4% deviation), size
	// 0.12: client notional = 24.6 < 25, market notional = 25.2 > 25.
	bridge, executor := newLiveExecutionHTTPTestBridge(t, liveExecutionTestLookup{
		ticker: ccxt.Ticker{Last: decimal.RequireFromString("210")},
	})
	defer bridge.close()

	recorder := invokeLiveExecutionHandler(t, bridge, `{"intent_id":"notional-1","chat_id":"123","exchange":"bitget-futures","symbol":"BTC/USDT","side":"buy","size":"0.12","price":"205","portfolio_value":"100","confidence":"0.9"}`)

	require.Equal(t, 400, recorder.Code)
	require.Equal(t, int32(0), executor.placed.Load())
}

func TestLiveExecutionHTTPMarketOrderWithoutPrice(t *testing.T) {
	bridge, executor := newLiveExecutionHTTPTestBridge(t, liveExecutionTestLookup{order: ccxt.OrderResponse{
		Order: ccxt.Order{
			ID:            "exchange-order-1",
			ClientOrderID: "NTclient",
			Symbol:        "BTC/USDT",
			Type:          "market",
			Side:          "buy",
			Status:        "closed",
			Amount:        decimal.RequireFromString("0.1"),
			Filled:        decimal.RequireFromString("0.1"),
			Price:         decimal.RequireFromString("100"),
			Cost:          decimal.RequireFromString("10.01"),
			Fee:           decimal.RequireFromString("0.0123"),
		},
	}})
	defer bridge.close()

	// Market orders may omit the price; the notional gate uses the fetched
	// market price (0.1 * 100 = 10 < 25).
	recorder := invokeLiveExecutionHandler(t, bridge, `{"intent_id":"noprice-1","chat_id":"123","exchange":"bitget-futures","symbol":"BTC/USDT","side":"buy","size":"0.1","portfolio_value":"100","confidence":"0.9"}`)

	var response liveFuturesOrderResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
	require.Equal(t, 200, recorder.Code)
	require.Equal(t, int32(1), executor.placed.Load())
	require.Equal(t, "filled", response.Status)
}

func newLiveExecutionHTTPTestBridge(t *testing.T, lookup liveExecutionTestLookup) (*riskGatedLiveExecution, *liveExecutionTestExecutor) {
	t.Helper()
	t.Setenv("NEURATRADE_LIVE_MAX_ORDER_NOTIONAL", "25")
	db, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "live-execution.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.DB.Close() })
	executor := &liveExecutionTestExecutor{}
	bridge, err := newRiskGatedLiveExecution(
		context.Background(),
		db.DB,
		executor,
		risk.NewKillSwitch(),
		risk.NewSafeMode(risk.DefaultSafeModeConfig()),
		nil,
		lookup,
	)
	require.NoError(t, err)
	return bridge, executor
}

func invokeLiveExecutionHandler(t *testing.T, bridge *riskGatedLiveExecution, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/api/v1/execution/futures/order", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	bridge.placeFuturesOrder(c)
	return recorder
}
