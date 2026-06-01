package services

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateIdempotencyKey_Deterministic(t *testing.T) {
	chatID := "12345"
	symbol := "BTC/USDT"
	side := "buy"
	intentID := "intent-abc-123"

	key1, err1 := generateIdempotencyKey(chatID, symbol, side, intentID)
	require.NoError(t, err1)
	key2, err2 := generateIdempotencyKey(chatID, symbol, side, intentID)
	require.NoError(t, err2)

	assert.Equal(t, key1, key2, "same inputs must produce same key")
	assert.Contains(t, key1, "nt-")
	assert.Contains(t, key1, chatID)
	assert.Contains(t, key1, "BTCUSDT")
	assert.Contains(t, key1, side)
	assert.Contains(t, key1, intentID)
}

func TestGenerateIdempotencyKey_DifferentIntentIDs(t *testing.T) {
	chatID := "12345"
	symbol := "ETH/USDT"
	side := "sell"

	key1, err1 := generateIdempotencyKey(chatID, symbol, side, "intent-1")
	require.NoError(t, err1)
	key2, err2 := generateIdempotencyKey(chatID, symbol, side, "intent-2")
	require.NoError(t, err2)

	assert.NotEqual(t, key1, key2, "different intentIDs must produce different keys")
}

func TestGenerateIdempotencyKey_DifferentSymbols(t *testing.T) {
	chatID := "12345"
	side := "buy"
	intentID := "intent-x"

	key1, err1 := generateIdempotencyKey(chatID, "BTC/USDT", side, intentID)
	require.NoError(t, err1)
	key2, err2 := generateIdempotencyKey(chatID, "ETH/USDT", side, intentID)
	require.NoError(t, err2)

	assert.NotEqual(t, key1, key2, "different symbols must produce different keys")
}

func TestGenerateIdempotencyKey_EmptyIntentID(t *testing.T) {
	chatID := "12345"
	symbol := "BTC/USDT"
	side := "buy"

	_, err1 := generateIdempotencyKey(chatID, symbol, side, "")
	_, err2 := generateIdempotencyKey(chatID, symbol, side, "   ")

	assert.ErrorIs(t, err1, ErrMissingIntentID, "empty intentID must surface ErrMissingIntentID")
	assert.ErrorIs(t, err2, ErrMissingIntentID, "whitespace intentID must surface ErrMissingIntentID")
}

func TestGenerateIdempotencyKey_MaxLength(t *testing.T) {
	longChatID := strings.Repeat("a", 50)
	longSymbol := strings.Repeat("BTC/USDT/", 5)
	longIntentID := strings.Repeat("x", 100)

	key, err := generateIdempotencyKey(longChatID, longSymbol, "buy", longIntentID)
	require.NoError(t, err)

	assert.LessOrEqual(t, len(key), maxClientOrderIDLen, "key must not exceed %d chars", maxClientOrderIDLen)
	assert.Contains(t, key, "nt-")
}

func TestIsDuplicateOrderError_Codes(t *testing.T) {
	tests := []struct {
		code   string
		msg    string
		expect bool
	}{
		{"40094", "duplicate client order id", true},
		{"43025", "order already placed", true},
		{"00000", "success", false},
		{"40001", "symbol not exist", false},
		{"40094", "", true},
		{"", "duplicate order detected", true},
		{"", "client order already exists", true},
		{"99999", "some other error", false},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("code=%s_msg=%s", tt.code, tt.msg), func(t *testing.T) {
			assert.Equal(t, tt.expect, isDuplicateOrderError(tt.code, tt.msg))
		})
	}
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_SetsClientOrderID(t *testing.T) {
	server := newBitgetTestServer(t, bitgetTestServerOpts{placeOrderResponse: `{"code":"00000","msg":"ok","data":{"orderId":"bg-test-123"}}`})
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	details := TradeDetails{
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		AmountUSDT: decimal.NewFromInt(100),
		IntentID:   "intent-test-42",
	}

	orderID, err := executor.PlaceOrderWithDetails(t.Context(), details)
	require.NoError(t, err)
	assert.NotEmpty(t, orderID)
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_DuplicateOrderTreatedAsSuccess(t *testing.T) {
	server := newBitgetTestServer(t, bitgetTestServerOpts{placeOrderResponse: `{"code":"40094","msg":"duplicate client order id","data":{"orderId":""}}`})
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	details := TradeDetails{
		Exchange:   "bitget",
		Symbol:     "BTC/USDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		AmountUSDT: decimal.NewFromInt(100),
		IntentID:   "intent-dup-1",
	}

	orderID, err := executor.PlaceOrderWithDetails(t.Context(), details)
	require.NoError(t, err)
	assert.NotEmpty(t, orderID, "duplicate order must return the client order ID as fallback")
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_DuplicateWithExistingOrderID(t *testing.T) {
	server := newBitgetTestServer(t, bitgetTestServerOpts{placeOrderResponse: `{"code":"43025","msg":"order already placed","data":{"orderId":"bg-existing-456"}}`})
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL

	details := TradeDetails{
		Exchange:   "bitget",
		Symbol:     "ETH/USDT",
		Side:       "sell",
		OrderType:  "market",
		MarketType: "futures",
		AmountUSDT: decimal.NewFromInt(50),
		IntentID:   "intent-dup-2",
	}

	orderID, err := executor.PlaceOrderWithDetails(t.Context(), details)
	require.NoError(t, err)
	assert.Equal(t, "bg-existing-456", orderID, "must return existing order ID from API response")
}

func TestCCXTOrderExecutor_PlaceOrderWithDetails_UsesDeterministicKey(t *testing.T) {
	var receivedClientOrderID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		json.NewDecoder(r.Body).Decode(&body)
		receivedClientOrderID, _ = body["clientOrderId"].(string)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"order":{"id":"ccxt-order-789"}}`))
	}))
	defer server.Close()

	executor := NewCCXTOrderExecutor(CCXTOrderExecutorConfig{
		ServiceURL: server.URL,
		Timeout:    30 * time.Second,
		MaxRetries: 0,
	})

	details := TradeDetails{
		Exchange:      "bitget",
		Symbol:        "BTC/USDT",
		Side:          "buy",
		OrderType:     "market",
		AmountUSDT:    decimal.NewFromInt(100),
		ClientOrderID: "nt-deterministic-key-abc",
	}

	orderID, err := executor.PlaceOrderWithDetails(t.Context(), details)
	require.NoError(t, err)
	assert.Equal(t, "ccxt-order-789", orderID)
	assert.Equal(t, "nt-deterministic-key-abc", receivedClientOrderID, "must use the provided ClientOrderID")
}

func TestCCXTOrderExecutor_PlaceOrderWithDetails_409TreatedAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"duplicate order"}`))
	}))
	defer server.Close()

	executor := NewCCXTOrderExecutor(CCXTOrderExecutorConfig{
		ServiceURL: server.URL,
		Timeout:    30 * time.Second,
		MaxRetries: 0,
	})

	details := TradeDetails{
		Exchange:      "bitget",
		Symbol:        "BTC/USDT",
		Side:          "buy",
		OrderType:     "market",
		AmountUSDT:    decimal.NewFromInt(100),
		ClientOrderID: "nt-key-dup-409",
	}

	orderID, err := executor.PlaceOrderWithDetails(t.Context(), details)
	require.NoError(t, err)
	assert.Equal(t, "nt-key-dup-409", orderID, "HTTP 409 must return clientOrderID as order ID")
}

func TestTradeDetails_HasIntentIDAndClientOrderID(t *testing.T) {
	details := TradeDetails{
		Exchange:      "bitget",
		Symbol:        "SOL/USDT",
		Side:          "buy",
		IntentID:      "test-intent-01",
		ClientOrderID: "nt-client-oid-xyz",
	}

	assert.Equal(t, "test-intent-01", details.IntentID)
	assert.Equal(t, "nt-client-oid-xyz", details.ClientOrderID)
}

func TestBitgetOrderExecutor_PlaceOrderWithDetails_MinNotionalViaTestServer(t *testing.T) {
	server := newBitgetTestServer(t, bitgetTestServerOpts{
		placeOrderResponse: `{"code":"00000","msg":"ok","data":{"orderId":"bg-order-with-clientOid"}}`,
		accountResponse:    `{"code":"00000","msg":"ok","data":{"marginMode":"isolated","posMode":"one_way_mode","isolatedLongLever":10,"isolatedShortLever":10}}`,
		assertClientOid:    true,
	})
	defer server.Close()

	executor := NewBitgetOrderExecutor("test-key", "test-secret", "test-pass")
	executor.baseURL = server.URL
	executor.SetChatID("12345")

	details := TradeDetails{
		Exchange:   "bitget",
		Symbol:     "BTCUSDT",
		Side:       "buy",
		OrderType:  "market",
		MarketType: "futures",
		AmountUSDT: decimal.NewFromInt(100),
		Leverage:   10,
		IntentID:   "intent-min-notional",
	}

	orderID, err := executor.PlaceOrderWithDetails(t.Context(), details)
	require.NoError(t, err)
	assert.NotEmpty(t, orderID)
}

type bitgetTestServerOpts struct {
	placeOrderResponse string
	assertClientOid    bool
	contractsResponse  string
	tickerResponse     string
	accountResponse    string
}

func newBitgetTestServer(t *testing.T, opts bitgetTestServerOpts) *httptest.Server {
	t.Helper()
	contracts := opts.contractsResponse
	if contracts == "" {
		contracts = `{"code":"00000","msg":"ok","data":[{"sizeMultiplier":"0.001","minTradeNum":"1","volumePlace":"0","pricePlace":"2"}]}`
	}
	ticker := opts.tickerResponse
	if ticker == "" {
		ticker = `{"code":"00000","msg":"ok","data":[{"lastPr":"50000"}]}`
	}
	account := opts.accountResponse
	if account == "" {
		account = `{"code":"00000","msg":"ok","data":{"marginMode":"isolated","posMode":"one_way_mode","isolatedLongLever":10,"isolatedShortLever":10}}`
	}
	placeOrder := opts.placeOrderResponse
	if placeOrder == "" {
		placeOrder = `{"code":"00000","msg":"ok","data":{"orderId":"bg-default"}}`
	}

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v2/mix/market/contracts"):
			_, _ = w.Write([]byte(contracts))
		case strings.HasPrefix(r.URL.Path, "/api/v2/mix/market/ticker"):
			_, _ = w.Write([]byte(ticker))
		case strings.HasPrefix(r.URL.Path, "/api/v2/mix/account/account"):
			_, _ = w.Write([]byte(account))
		case strings.HasPrefix(r.URL.Path, "/api/v2/mix/order/place-order"):
			if opts.assertClientOid {
				var body map[string]interface{}
				_ = json.NewDecoder(r.Body).Decode(&body)
				assert.NotEmpty(t, body["clientOid"], "clientOid must be present in the order payload")
			}
			_, _ = w.Write([]byte(placeOrder))
		default:
			_, _ = w.Write([]byte(`{"code":"00000","msg":"ok","data":{}}`))
		}
	}))
}
