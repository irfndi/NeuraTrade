package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCCXTOrderExecutor_PlaceOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/order", r.URL.Path)
		assert.Equal(t, "POST", r.Method)

		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Equal(t, "binance", req["exchange"])
		assert.Equal(t, "BTC/USDT", req["symbol"])
		assert.Equal(t, "buy", req["side"])
		assert.Equal(t, "market", req["type"])
		assert.Equal(t, 0.5, req["amount"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"order": map[string]string{"id": "order-12345"},
		})
	}))
	defer server.Close()

	executor := NewCCXTOrderExecutor(CCXTOrderExecutorConfig{
		ServiceURL: server.URL,
		Timeout:    30 * time.Second,
	})

	orderID, err := executor.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "market", decimal.NewFromFloat(0.5), nil)

	require.NoError(t, err)
	assert.Equal(t, "order-12345", orderID)
}

func TestCCXTOrderExecutor_CancelOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/order/binance/order-12345", r.URL.Path)
		assert.Equal(t, "DELETE", r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":   true,
			"orderId":   "order-12345",
			"timestamp": "2026-02-15T12:00:00Z",
		})
	}))
	defer server.Close()

	executor := NewCCXTOrderExecutor(CCXTOrderExecutorConfig{
		ServiceURL: server.URL,
		Timeout:    30 * time.Second,
	})

	err := executor.CancelOrder(context.Background(), "binance", "order-12345")

	require.NoError(t, err)
}

func TestCCXTOrderExecutor_GetOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/order/binance/order-12345", r.URL.Path)
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"order": map[string]interface{}{
				"id":     "order-12345",
				"status": "closed",
				"amount": 0.5,
			},
			"timestamp": "2026-02-15T12:00:00Z",
		})
	}))
	defer server.Close()

	executor := NewCCXTOrderExecutor(CCXTOrderExecutorConfig{
		ServiceURL: server.URL,
		Timeout:    30 * time.Second,
	})

	order, err := executor.GetOrder(context.Background(), "binance", "order-12345")

	require.NoError(t, err)
	orderData := order["order"].(map[string]interface{})
	assert.Equal(t, "order-12345", orderData["id"])
}

func TestCCXTOrderExecutor_GetOpenOrders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/orders/binance", r.URL.Path)
		assert.Equal(t, "GET", r.Method)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"orders": []map[string]interface{}{
				{"id": "order-1", "status": "open"},
				{"id": "order-2", "status": "open"},
			},
			"timestamp": "2026-02-15T12:00:00Z",
		})
	}))
	defer server.Close()

	executor := NewCCXTOrderExecutor(CCXTOrderExecutorConfig{
		ServiceURL: server.URL,
		Timeout:    30 * time.Second,
	})

	orders, err := executor.GetOpenOrders(context.Background(), "binance", "")

	require.NoError(t, err)
	assert.Len(t, orders, 2)
}

func TestCCXTOrderExecutor_PlaceOrderWithPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&req)
		require.NoError(t, err)

		assert.Equal(t, 50000.0, req["price"])

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"order": map[string]string{"id": "order-limit-123"},
		})
	}))
	defer server.Close()

	executor := NewCCXTOrderExecutor(CCXTOrderExecutorConfig{
		ServiceURL: server.URL,
		Timeout:    30 * time.Second,
	})

	price := decimal.NewFromFloat(50000)
	orderID, err := executor.PlaceOrder(context.Background(), "binance", "BTC/USDT", "buy", "limit", decimal.NewFromFloat(0.1), &price)

	require.NoError(t, err)
	assert.Equal(t, "order-limit-123", orderID)
}
