// Package client provides HTTP client for backend API communication.
package agentcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config holds backend client configuration.
type ClientConfig struct {
	BaseURL     string
	Timeout     time.Duration
	MaxRetries  int
	AdminAPIKey string
}

// BackendClient provides methods to interact with the backend API.
type BackendClient struct {
	config     ClientConfig
	httpClient *http.Client
}

// NewBackendClient creates a new backend client.
func NewBackendClient(config ClientConfig) *BackendClient {
	return &BackendClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

// PauseExchange pauses market data collection for an exchange.
func (c *BackendClient) PauseExchange(ctx context.Context, exchangeID string) error {
	return c.executeCommand(ctx, "/api/v1/agent/pause-exchange", map[string]any{
		"exchange_id": exchangeID,
	})
}

// ResumeExchange resumes market data collection for an exchange.
func (c *BackendClient) ResumeExchange(ctx context.Context, exchangeID string) error {
	return c.executeCommand(ctx, "/api/v1/agent/resume-exchange", map[string]any{
		"exchange_id": exchangeID,
	})
}

// EnableSafeMode enables safe mode (blocks new trades).
func (c *BackendClient) EnableSafeMode(ctx context.Context) error {
	return c.executeCommand(ctx, "/api/v1/agent/enable-safe-mode", nil)
}

// DisableSafeMode disables safe mode.
func (c *BackendClient) DisableSafeMode(ctx context.Context) error {
	return c.executeCommand(ctx, "/api/v1/agent/disable-safe-mode", nil)
}

// EngageKillSwitch engages the kill switch (hard stop).
func (c *BackendClient) EngageKillSwitch(ctx context.Context) error {
	return c.executeCommand(ctx, "/api/v1/agent/kill-switch", map[string]any{
		"engage": true,
	})
}

// DisengageKillSwitch disengages the kill switch.
func (c *BackendClient) DisengageKillSwitch(ctx context.Context) error {
	return c.executeCommand(ctx, "/api/v1/agent/kill-switch", map[string]any{
		"engage": false,
	})
}

// CancelAllOrders cancels all open orders.
func (c *BackendClient) CancelAllOrders(ctx context.Context, scope string) error {
	return c.executeCommand(ctx, "/api/v1/agent/cancel-all-orders", map[string]any{
		"scope": scope,
	})
}

func (c *BackendClient) executeCommand(ctx context.Context, endpoint string, payload any) error {
	var payloadBytes []byte
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal payload: %w", err)
		}
		payloadBytes = data
	}

	url := strings.TrimRight(c.config.BaseURL, "/") + endpoint

	// Retry logic
	var lastErr error
	for attempt := 0; attempt <= c.config.MaxRetries; attempt++ {
		var body io.Reader
		if len(payloadBytes) > 0 {
			body = bytes.NewReader(payloadBytes)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		if strings.TrimSpace(c.config.AdminAPIKey) != "" {
			req.Header.Set("X-API-Key", strings.TrimSpace(c.config.AdminAPIKey))
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			if err := waitRetry(ctx, attempt); err != nil {
				return err
			}
			continue
		}

		// Close body immediately after reading (not using defer in loop)
		bodyBytes, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			if readErr == nil {
				lastErr = fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
			} else {
				lastErr = fmt.Errorf("request failed with status %d: %w", resp.StatusCode, readErr)
			}
			if resp.StatusCode >= 500 {
				// Retry on server errors
				if err := waitRetry(ctx, attempt); err != nil {
					return err
				}
				continue
			}
			return lastErr
		}

		return nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("request failed")
	}
	return fmt.Errorf("max retries exceeded: %w", lastErr)
}

func waitRetry(ctx context.Context, attempt int) error {
	timer := time.NewTimer(time.Duration(attempt+1) * time.Second)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
