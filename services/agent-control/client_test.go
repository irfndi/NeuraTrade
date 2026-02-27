package agentcontrol

import (
	"context"
	"testing"
	"time"
)

func TestNewBackendClient(t *testing.T) {
	client := NewBackendClient(ClientConfig{
		BaseURL:    "http://localhost:8080",
		Timeout:    30 * time.Second,
		MaxRetries: 3,
	})
	if client == nil {
		t.Fatal("NewBackendClient returned nil")
	}
	if client.config.BaseURL != "http://localhost:8080" {
		t.Errorf("Expected BaseURL http://localhost:8080, got %s", client.config.BaseURL)
	}
}

func TestClientConfigDefaults(t *testing.T) {
	expectedTimeout := 30 * time.Second
	client := NewBackendClient(ClientConfig{
		BaseURL: "http://localhost:8080",
		Timeout: expectedTimeout,
	})
	if client.config.Timeout != expectedTimeout {
		t.Errorf("Expected timeout %v, got %v", expectedTimeout, client.config.Timeout)
	}
}

func TestClientMethodsExist(t *testing.T) {
	client := NewBackendClient(ClientConfig{
		BaseURL:    "http://localhost:8080",
		Timeout:    30 * time.Second,
		MaxRetries: 0,
	})

	ctx := context.Background()

	// Test that methods exist and handle connection errors gracefully
	err := client.PauseExchange(ctx, "binance")
	if err == nil {
		t.Error("Expected error for unreachable backend")
	}

	err = client.ResumeExchange(ctx, "binance")
	if err == nil {
		t.Error("Expected error for unreachable backend")
	}

	err = client.EnableSafeMode(ctx)
	if err == nil {
		t.Error("Expected error for unreachable backend")
	}

	err = client.DisableSafeMode(ctx)
	if err == nil {
		t.Error("Expected error for unreachable backend")
	}

	err = client.EngageKillSwitch(ctx)
	if err == nil {
		t.Error("Expected error for unreachable backend")
	}

	err = client.DisengageKillSwitch(ctx)
	if err == nil {
		t.Error("Expected error for unreachable backend")
	}

	err = client.CancelAllOrders(ctx, "all")
	if err == nil {
		t.Error("Expected error for unreachable backend")
	}
}
