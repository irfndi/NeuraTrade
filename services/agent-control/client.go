// Package client provides HTTP client for backend API communication.
package agentcontrol

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
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

const maxErrorBodyRead = 1 << 20 // 1 MiB

// NewBackendClient creates a new backend client.
func NewBackendClient(config ClientConfig) *BackendClient {
	if config.MaxRetries < 0 {
		config.MaxRetries = 0
	}

	return &BackendClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

// PauseExchange pauses market data collection for an exchange.
func (c *BackendClient) PauseExchange(ctx context.Context, exchangeID string) error {
	exchangeID = strings.TrimSpace(exchangeID)
	if exchangeID == "" {
		return fmt.Errorf("invalid exchangeID")
	}

	return c.executeCommand(ctx, "/api/v1/agent/pause-exchange", map[string]any{
		"exchange_id": exchangeID,
	})
}

// ResumeExchange resumes market data collection for an exchange.
func (c *BackendClient) ResumeExchange(ctx context.Context, exchangeID string) error {
	exchangeID = strings.TrimSpace(exchangeID)
	if exchangeID == "" {
		return fmt.Errorf("invalid exchangeID")
	}

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
			if attempt >= c.config.MaxRetries {
				break
			}
			if err := waitRetry(ctx, attempt); err != nil {
				return fmt.Errorf("waiting before retrying agent command (attempt %d): %w", attempt+1, err)
			}
			continue
		}

		// Close body immediately after reading (not using defer in loop)
		bodyBytes, readErr := readBodyWithLimit(resp.Body, maxErrorBodyRead)
		resp.Body.Close()

		if resp.StatusCode >= 400 {
			if readErr == nil {
				lastErr = fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
			} else {
				lastErr = fmt.Errorf("request failed with status %d: %w", resp.StatusCode, readErr)
			}
			if resp.StatusCode >= 500 {
				// Retry on server errors
				if attempt >= c.config.MaxRetries {
					break
				}
				if err := waitRetry(ctx, attempt); err != nil {
					return fmt.Errorf("waiting before retrying agent command after server error (attempt %d): %w", attempt+1, err)
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

func readBodyWithLimit(reader io.Reader, maxBytes int64) ([]byte, error) {
	limitedReader := io.LimitReader(reader, maxBytes+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}

	if int64(len(body)) > maxBytes {
		truncatedBody := append([]byte{}, body[:maxBytes]...)
		truncatedBody = append(truncatedBody, []byte("...(truncated)")...)
		return truncatedBody, nil
	}

	return body, nil
}

func waitRetry(ctx context.Context, attempt int) error {
	if attempt < 0 {
		attempt = 0
	}
	if attempt > 5 {
		attempt = 5
	}

	baseDelay := time.Second << attempt
	jitterWindow := baseDelay / 2
	if jitterWindow < time.Millisecond {
		jitterWindow = time.Millisecond
	}
	jitter := time.Duration(rand.Int63n(int64(jitterWindow)))
	delay := baseDelay + jitter

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
