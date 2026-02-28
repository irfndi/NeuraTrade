// Package telegram provides an adapter that calls the telegram-service
// to implement the ports.Notifier interface.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ports"
)

// Config holds configuration for the Telegram adapter.
type Config struct {
	// BaseURL is the telegram-service URL (e.g., http://localhost:8443)
	BaseURL string
	// APIKey is the admin API key for authentication
	APIKey string
	// ChatID is the default Telegram chat ID to send notifications to
	ChatID string
	// Timeout is the HTTP client timeout
	Timeout time.Duration
	// Enabled controls whether notifications are sent
	Enabled bool
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		Timeout: 10 * time.Second,
		Enabled: true,
	}
}

// Adapter implements ports.Notifier by calling the telegram-service HTTP API.
type Adapter struct {
	config Config
	client *http.Client
}

// NewAdapter creates a new Telegram notifier adapter.
func NewAdapter(config Config) *Adapter {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Adapter{
		config: config,
		client: &http.Client{Timeout: timeout},
	}
}

// Send sends a notification synchronously.
func (a *Adapter) Send(ctx context.Context, notification ports.Notification) error {
	if !a.config.Enabled {
		return nil
	}

	chatID := a.config.ChatID
	if chatID == "" {
		return fmt.Errorf("no chat ID configured")
	}

	message := a.formatMessage(notification)
	return a.sendMessage(ctx, chatID, message)
}

// SendAsync sends a notification asynchronously.
func (a *Adapter) SendAsync(ctx context.Context, notification ports.Notification) (<-chan ports.NotificationResult, error) {
	resultChan := make(chan ports.NotificationResult, 1)

	if !a.config.Enabled {
		resultChan <- ports.NotificationResult{
			ID:      notification.ID,
			Sent:    true,
			Channel: "telegram",
			SentAt:  time.Now(),
		}
		close(resultChan)
		return resultChan, nil
	}

	go func() {
		defer close(resultChan)
		err := a.Send(ctx, notification)
		result := ports.NotificationResult{
			ID:      notification.ID,
			Channel: "telegram",
			SentAt:  time.Now(),
		}
		if err != nil {
			result.Sent = false
			result.Error = err.Error()
		} else {
			result.Sent = true
		}
		resultChan <- result
	}()

	return resultChan, nil
}

// SendBatch sends multiple notifications.
func (a *Adapter) SendBatch(ctx context.Context, notifications []ports.Notification) error {
	if !a.config.Enabled {
		return nil
	}

	var wg sync.WaitGroup
	errChan := make(chan error, len(notifications))

	for _, n := range notifications {
		wg.Add(1)
		go func(notification ports.Notification) {
			defer wg.Done()
			if err := a.Send(ctx, notification); err != nil {
				errChan <- err
			}
		}(n)
	}

	wg.Wait()
	close(errChan)

	// Collect and return all errors
	var allErrors []error
	for err := range errChan {
		if err != nil {
			allErrors = append(allErrors, err)
		}
	}

	if len(allErrors) > 0 {
		return errors.Join(allErrors...)
	}
	return nil
}

// IsEnabled checks if notifications are enabled.
func (a *Adapter) IsEnabled() bool {
	return a.config.Enabled && a.config.ChatID != ""
}

// formatMessage formats a notification into a Telegram message string.
func (a *Adapter) formatMessage(n ports.Notification) string {
	var buf bytes.Buffer

	// Add emoji based on type
	switch n.Type {
	case ports.NotificationTypeError:
		buf.WriteString("🔴 ")
	case ports.NotificationTypeWarning:
		buf.WriteString("🟡 ")
	case ports.NotificationTypeSuccess:
		buf.WriteString("🟢 ")
	case ports.NotificationTypeTrade:
		buf.WriteString("💰 ")
	case ports.NotificationTypeSignal:
		buf.WriteString("📊 ")
	case ports.NotificationTypeRisk:
		buf.WriteString("⚠️ ")
	case ports.NotificationTypeSystem:
		buf.WriteString("ℹ️ ")
	default:
		buf.WriteString("📢 ")
	}

	// Add title
	if n.Title != "" {
		buf.WriteString("*")
		buf.WriteString(n.Title)
		buf.WriteString("*\n\n")
	}

	// Add message
	buf.WriteString(n.Message)

	// Add context info

	// Add context info
	if n.Exchange != "" || n.Symbol != "" || n.StrategyID != "" {
		buf.WriteString("\n\n_")
		if n.Exchange != "" {
			fmt.Fprintf(&buf, "Exchange: %s ", n.Exchange)
		}
		if n.Symbol != "" {
			fmt.Fprintf(&buf, "Symbol: %s ", n.Symbol)
		}
		if n.StrategyID != "" {
			fmt.Fprintf(&buf, "Strategy: %s", n.StrategyID)
		}
		buf.WriteString("_")
	}

	return buf.String()
}

// sendMessage calls the telegram-service /send-message endpoint.
func (a *Adapter) sendMessage(ctx context.Context, chatID, text string) error {
	url := fmt.Sprintf("%s/send-message", a.config.BaseURL)

	payload := map[string]interface{}{
		"chatId":    chatID,
		"text":      text,
		"parseMode": "Markdown",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", a.config.APIKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("telegram service error: status %d, body: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// Compile-time interface check
var _ ports.Notifier = (*Adapter)(nil)
