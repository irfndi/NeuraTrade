package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdapter_Send_Smoke(t *testing.T) {
	type requestBody struct {
		ChatID    string `json:"chatId"`
		Text      string `json:"text"`
		ParseMode string `json:"parseMode"`
	}

	var payload requestBody
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/send-message", r.URL.Path)
		require.Equal(t, "test-api-key", r.Header.Get("X-API-Key"))

		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := NewAdapter(Config{
		BaseURL: server.URL,
		APIKey:  "test-api-key",
		ChatID:  "123456",
		Timeout: time.Second,
		Enabled: true,
	})

	err := adapter.Send(context.Background(), ports.Notification{
		ID:         "n-1",
		Type:       ports.NotificationTypeTrade,
		Title:      "Order Filled",
		Message:    "Bought BTC",
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		StrategyID: "scalp-1",
	})
	require.NoError(t, err)

	assert.Equal(t, "123456", payload.ChatID)
	assert.Equal(t, "Markdown", payload.ParseMode)
	assert.Contains(t, payload.Text, "*Order Filled*")
	assert.Contains(t, payload.Text, "Bought BTC")
	assert.Contains(t, payload.Text, "Exchange: binance")
}

func TestAdapter_Send_MapsServiceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failed", http.StatusBadGateway)
	}))
	defer server.Close()

	adapter := NewAdapter(Config{
		BaseURL: server.URL,
		APIKey:  "test-api-key",
		ChatID:  "123456",
		Timeout: time.Second,
		Enabled: true,
	})

	err := adapter.Send(context.Background(), ports.Notification{
		ID:      "n-2",
		Type:    ports.NotificationTypeError,
		Message: "failed",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "telegram service error")
	assert.Contains(t, err.Error(), "status 502")
	assert.Contains(t, err.Error(), "upstream failed")
}

func TestAdapter_SendBatch_JoinsAllErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "temporary failure", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	adapter := NewAdapter(Config{
		BaseURL: server.URL,
		APIKey:  "test-api-key",
		ChatID:  "123456",
		Timeout: time.Second,
		Enabled: true,
	})

	err := adapter.SendBatch(context.Background(), []ports.Notification{
		{ID: "n-3", Message: "one"},
		{ID: "n-4", Message: "two"},
	})
	require.Error(t, err)

	type unwrapMany interface {
		Unwrap() []error
	}

	var multiErr unwrapMany
	require.ErrorAs(t, err, &multiErr)
	assert.Len(t, multiErr.Unwrap(), 2)
	assert.Contains(t, err.Error(), "status 503")
}

func TestAdapter_Send_DisabledDoesNothing(t *testing.T) {
	adapter := NewAdapter(Config{
		Enabled: false,
	})

	err := adapter.Send(context.Background(), ports.Notification{
		ID:      "n-5",
		Message: "ignored",
	})
	assert.NoError(t, err)
}

func TestAdapter_SendBatch_DisabledDoesNothing(t *testing.T) {
	adapter := NewAdapter(Config{
		Enabled: false,
	})

	err := adapter.SendBatch(context.Background(), []ports.Notification{
		{ID: "n-6", Message: "ignored"},
	})
	assert.NoError(t, err)
}

func TestAdapter_Send_RequiresChatID(t *testing.T) {
	adapter := NewAdapter(Config{
		BaseURL: "http://localhost:9999",
		APIKey:  "test-api-key",
		Enabled: true,
	})

	err := adapter.Send(context.Background(), ports.Notification{
		ID:      "n-7",
		Message: "missing chat id",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no chat ID configured")
}

func TestAdapter_SendAsync_Smoke(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	adapter := NewAdapter(Config{
		BaseURL: server.URL,
		APIKey:  "test-api-key",
		ChatID:  "123456",
		Timeout: time.Second,
		Enabled: true,
	})

	resultChan, err := adapter.SendAsync(context.Background(), ports.Notification{
		ID:      "n-async-1",
		Type:    ports.NotificationTypeTrade,
		Message: "async message",
	})
	require.NoError(t, err)

	result := <-resultChan
	assert.True(t, result.Sent)
	assert.Equal(t, "n-async-1", result.ID)
	assert.Equal(t, "telegram", result.Channel)
}

func TestAdapter_SendAsync_MapsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	adapter := NewAdapter(Config{
		BaseURL: server.URL,
		APIKey:  "test-api-key",
		ChatID:  "123456",
		Timeout: time.Second,
		Enabled: true,
	})

	resultChan, err := adapter.SendAsync(context.Background(), ports.Notification{
		ID:      "n-async-2",
		Message: "error test",
	})
	require.NoError(t, err)

	result := <-resultChan
	assert.False(t, result.Sent)
	assert.Contains(t, result.Error, "telegram service error")
}

func TestAdapter_SendAsync_DisabledDoesNothing(t *testing.T) {
	adapter := NewAdapter(Config{
		Enabled: false,
	})

	resultChan, err := adapter.SendAsync(context.Background(), ports.Notification{
		ID:      "n-async-3",
		Message: "should be ignored",
	})
	require.NoError(t, err)

	result := <-resultChan
	assert.True(t, result.Sent)
	assert.NoError(t, result.Error)
}
