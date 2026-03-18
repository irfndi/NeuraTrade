package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChannelRegistry(t *testing.T) {
	registry := NewChannelRegistry()

	// Test registering a channel
	discord := NewDiscordChannel(DiscordChannelConfig{
		WebhookURL: "https://discord.com/api/webhooks/test",
		Enabled:    true,
	})
	registry.Register(discord)

	// Test getting channels
	channels := registry.GetChannels(PriorityHigh)
	assert.Len(t, channels, 1)
	assert.Equal(t, "discord", channels[0].Name())
}

func TestDiscordChannel_Send(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var payload map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&payload)
		require.NoError(t, err)

		// Check embeds exist
		embeds, ok := payload["embeds"].([]interface{})
		require.True(t, ok)
		require.Len(t, embeds, 1)

		embed := embeds[0].(map[string]interface{})
		assert.Equal(t, "Test Title", embed["title"])

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	// Create channel with mock URL
	channel := NewDiscordChannel(DiscordChannelConfig{
		WebhookURL: server.URL,
		Enabled:    true,
	})

	notification := &Notification{
		ID:        "test-1",
		Type:      NotificationTypeTradeExecuted,
		Priority:  PriorityHigh,
		Title:     "Test Title",
		Message:   "Test message",
		Metadata:  map[string]string{"symbol": "BTCUSDT"},
		Timestamp: time.Now(),
	}

	err := channel.Send(context.Background(), notification)
	assert.NoError(t, err)
}

func TestDiscordChannel_Disabled(t *testing.T) {
	channel := NewDiscordChannel(DiscordChannelConfig{
		Enabled: false,
	})

	assert.False(t, channel.IsEnabled())

	notification := &Notification{
		ID:        "test-1",
		Title:     "Test",
		Message:   "Test",
		Timestamp: time.Now(),
	}

	err := channel.Send(context.Background(), notification)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not enabled")
}

func TestWebhookChannel_Send(t *testing.T) {
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Store(true)
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, "test-token", r.Header.Get("X-Auth-Token"))

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	channel := NewWebhookChannel(WebhookChannelConfig{
		URL: server.URL,
		Headers: map[string]string{
			"X-Auth-Token": "test-token",
		},
		Enabled:    true,
		RetryCount: 0,
	})

	notification := &Notification{
		ID:        "test-1",
		Type:      NotificationTypeSignalDetected,
		Priority:  PriorityHigh,
		Title:     "Signal Detected",
		Message:   "BTC/USDT showing strong buy signal",
		Metadata:  map[string]string{"confidence": "0.85"},
		Timestamp: time.Now(),
	}

	err := channel.Send(context.Background(), notification)
	assert.NoError(t, err)
	assert.True(t, received.Load())
}

func TestWebhookChannel_Retry(t *testing.T) {
	var attemptCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attemptCount.Add(1)
		if attemptCount.Load() < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	channel := NewWebhookChannel(WebhookChannelConfig{
		URL:        server.URL,
		Enabled:    true,
		RetryCount: 3,
		Timeout:    time.Second,
	})

	notification := &Notification{
		ID:        "test-1",
		Title:     "Test",
		Message:   "Test",
		Timestamp: time.Now(),
	}

	err := channel.Send(context.Background(), notification)
	assert.NoError(t, err)
	assert.Equal(t, int32(2), attemptCount.Load())
}

func TestNotificationPriorityRouting(t *testing.T) {
	registry := NewChannelRegistry()

	discord := NewDiscordChannel(DiscordChannelConfig{
		WebhookURL: "https://discord.com/api/webhooks/test",
		Enabled:    true,
	})
	registry.Register(discord)

	email := NewEmailChannel(EmailChannelConfig{
		Enabled:     true,
		SMTPHost:    "smtp.example.com",
		SMTPPort:    587,
		FromAddress: "alerts@neuratrade.com",
		Username:    "user",
		Password:    "pass",
	})
	registry.Register(email)

	// Test HIGH priority - should get Discord
	highChannels := registry.GetChannels(PriorityHigh)
	assert.Len(t, highChannels, 1)
	assert.Equal(t, "discord", highChannels[0].Name())

	// Test LOW priority - should get Email
	lowChannels := registry.GetChannels(PriorityLow)
	assert.Len(t, lowChannels, 1)
	assert.Equal(t, "email", lowChannels[0].Name())

	// Test EMERGENCY - both Discord and Email handle critical priority
	emergencyChannels := registry.GetChannels(PriorityCritical)
	assert.Len(t, emergencyChannels, 2)
}

func TestNotificationChannelsService(t *testing.T) {
	service := NewNotificationChannelsService(nil)

	service.ConfigureDiscord(DiscordChannelConfig{
		WebhookURL: "https://discord.com/api/webhooks/test",
		Enabled:    true,
	})

	service.ConfigureEmail(EmailChannelConfig{
		Enabled:     true,
		SMTPHost:    "smtp.example.com",
		SMTPPort:    587,
		FromAddress: "alerts@neuratrade.com",
		Username:    "user",
		Password:    "pass",
	})

	service.ConfigureWebhook(WebhookChannelConfig{
		URL:     "https://example.com/webhook",
		Enabled: true,
	})

	registry := service.Registry()
	assert.NotNil(t, registry)
}

func TestEmailChannel_Disabled(t *testing.T) {
	channel := NewEmailChannel(EmailChannelConfig{
		Enabled: false,
	})

	assert.False(t, channel.IsEnabled())

	// Even with config, disabled should return false
	channel = NewEmailChannel(EmailChannelConfig{
		Enabled:     true,
		SMTPHost:    "", // Missing host
		FromAddress: "test@example.com",
	})

	assert.False(t, channel.IsEnabled())
}

func TestWebhookChannel_Disabled(t *testing.T) {
	channel := NewWebhookChannel(WebhookChannelConfig{
		Enabled: false,
		URL:     "",
	})

	assert.False(t, channel.IsEnabled())
}

func TestDispatchSystemAlert_Success(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Write([]byte(`{"ok":true,"message_id":"123"}`))
	}))
	defer server.Close()

	ns := NewNotificationService(nil, nil, server.URL, "", "")

	ctx := context.Background()
	err := ns.DispatchSystemAlert(ctx, 123456, SystemAlertNotification{
		Level:   "critical",
		Source:  "test-source",
		Message: "Something went wrong",
		Details: map[string]any{"host": "db-1"},
	})

	assert.NoError(t, err)
	assert.NotNil(t, receivedBody)
	assert.Equal(t, "123456", receivedBody["chatId"])
	assert.Contains(t, receivedBody["text"].(string), "CRITICAL")
	assert.Contains(t, receivedBody["text"].(string), "test-source")
	assert.Contains(t, receivedBody["text"].(string), "Something went wrong")
	assert.Contains(t, receivedBody["text"].(string), "host")
}

func TestDispatchSystemAlert_Failure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"ok":false,"error":"bad request","errorCode":"INVALID_REQUEST"}`))
	}))
	defer server.Close()

	ns := NewNotificationService(nil, nil, server.URL, "", "")

	ctx := context.Background()
	err := ns.DispatchSystemAlert(ctx, 123456, SystemAlertNotification{
		Level:   "error",
		Source:  "db",
		Message: "connection lost",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "INVALID_REQUEST")
}

func TestDispatchSystemAlert_NoTelegramURL(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	ctx := context.Background()
	err := ns.DispatchSystemAlert(ctx, 123456, SystemAlertNotification{
		Level:   "critical",
		Source:  "test",
		Message: "no url configured",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestFormatSystemAlertMessage_AllLevels(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	levels := []struct {
		level    string
		contains string
	}{
		{"critical", "CRITICAL"},
		{"error", "ERROR"},
		{"warning", "WARNING"},
		{"info", "INFO"},
	}

	for _, tc := range levels {
		t.Run(tc.level, func(t *testing.T) {
			msg := ns.formatSystemAlertMessage(SystemAlertNotification{
				Level:   tc.level,
				Source:  "src",
				Message: "test msg",
				Details: map[string]any{"k": "v"},
			})
			assert.Contains(t, msg, tc.contains)
			assert.Contains(t, msg, "src")
			assert.Contains(t, msg, "test msg")
		})
	}
}

func TestFormatSystemAlertMessage_NoDetails(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	msg := ns.formatSystemAlertMessage(SystemAlertNotification{
		Level:   "error",
		Source:  "src",
		Message: "simple alert",
	})

	assert.NotContains(t, msg, "Details:")
	assert.Contains(t, msg, "simple alert")
}
