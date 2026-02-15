package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/telemetry"
)

// NotificationPriority represents the priority level of a notification.
// Uses ActionPriority from action_streaming.go for consistency.
type NotificationPriority = ActionPriority

// NotificationType represents the type of notification.
type NotificationType string

const (
	NotificationTypeTradeExecuted  NotificationType = "TRADE_EXECUTED"
	NotificationTypeSignalDetected NotificationType = "SIGNAL_DETECTED"
	NotificationTypePositionOpened NotificationType = "POSITION_OPENED"
	NotificationTypePositionClosed NotificationType = "POSITION_CLOSED"
	NotificationTypeDailySummary   NotificationType = "DAILY_SUMMARY"
	NotificationTypeEmergency      NotificationType = "EMERGENCY"
	NotificationTypeArbitrageAlert NotificationType = "ARBITRAGE_ALERT"
)

// Notification priority constants (using ActionPriority values)
const (
	PriorityNotificationEmergency ActionPriority = "critical"
	PriorityNotificationHigh      ActionPriority = "high"
	PriorityNotificationNormal    ActionPriority = "normal"
	PriorityNotificationLow       ActionPriority = "low"
)

// Notification represents a notification message.
type Notification struct {
	ID         string               `json:"id"`
	Type       NotificationType     `json:"type"`
	Priority   NotificationPriority `json:"priority"`
	Title      string               `json:"title"`
	Message    string               `json:"message"`
	Metadata   map[string]string    `json:"metadata,omitempty"`
	Timestamp  time.Time            `json:"timestamp"`
	Recipients []string             `json:"recipients,omitempty"`
}

// NotificationChannel is the interface for notification channels.
type NotificationChannel interface {
	// Send sends a notification through this channel.
	Send(ctx context.Context, notification *Notification) error
	// Name returns the channel name.
	Name() string
	// IsEnabled returns whether the channel is enabled.
	IsEnabled() bool
	// Priorities returns the priority levels this channel handles.
	Priorities() []NotificationPriority
}

// ChannelRegistry manages notification channels and routing.
type ChannelRegistry struct {
	mu       sync.RWMutex
	channels map[NotificationPriority]map[string]NotificationChannel
	logger   *slog.Logger
}

// NewChannelRegistry creates a new channel registry.
func NewChannelRegistry() *ChannelRegistry {
	return &ChannelRegistry{
		channels: make(map[NotificationPriority]map[string]NotificationChannel),
		logger:   telemetry.Logger(),
	}
}

// Register adds a channel to the registry for all supported priorities.
func (r *ChannelRegistry) Register(channel NotificationChannel) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, priority := range channel.Priorities() {
		if r.channels[priority] == nil {
			r.channels[priority] = make(map[string]NotificationChannel)
		}
		r.channels[priority][channel.Name()] = channel
		r.logger.Info("Registered notification channel", "channel", channel.Name(), "priority", priority)
	}
}

// GetChannels returns all enabled channels for a given priority.
func (r *ChannelRegistry) GetChannels(priority NotificationPriority) []NotificationChannel {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []NotificationChannel
	if channels, ok := r.channels[priority]; ok {
		for _, ch := range channels {
			if ch.IsEnabled() {
				result = append(result, ch)
			}
		}
	}
	return result
}

// Send sends a notification to all appropriate channels based on priority routing.
func (r *ChannelRegistry) Send(ctx context.Context, notification *Notification) error {
	channels := r.GetChannels(notification.Priority)
	if len(channels) == 0 {
		r.logger.Warn("No channels available for priority", "priority", notification.Priority)
		return fmt.Errorf("no channels available for priority %s", notification.Priority)
	}

	var errors []string
	for _, ch := range channels {
		if err := ch.Send(ctx, notification); err != nil {
			errMsg := fmt.Sprintf("%s: %v", ch.Name(), err)
			errors = append(errors, errMsg)
			r.logger.Error("Failed to send notification via channel",
				"channel", ch.Name(),
				"error", err)
		}
	}

	if len(errors) > 0 && len(errors) == len(channels) {
		return fmt.Errorf("all channels failed: %s", strings.Join(errors, "; "))
	}
	return nil
}

// DiscordChannelConfig holds Discord webhook configuration.
type DiscordChannelConfig struct {
	WebhookURL string
	Enabled    bool
}

// DiscordChannel sends notifications to Discord via webhook.
type DiscordChannel struct {
	config DiscordChannelConfig
	client *http.Client
	logger *slog.Logger
}

// NewDiscordChannel creates a new Discord channel.
func NewDiscordChannel(config DiscordChannelConfig) *DiscordChannel {
	return &DiscordChannel{
		config: config,
		client: &http.Client{Timeout: 10 * time.Second},
		logger: telemetry.Logger().With("channel", "discord"),
	}
}

// Name returns the channel name.
func (c *DiscordChannel) Name() string {
	return "discord"
}

// IsEnabled returns whether the channel is enabled.
func (c *DiscordChannel) IsEnabled() bool {
	return c.config.Enabled && c.config.WebhookURL != ""
}

// Priorities returns the priority levels this channel handles.
func (c *DiscordChannel) Priorities() []NotificationPriority {
	return []NotificationPriority{PriorityNotificationHigh, PriorityNotificationEmergency}
}

// Send sends a notification to Discord.
func (c *DiscordChannel) Send(ctx context.Context, notification *Notification) error {
	if !c.IsEnabled() {
		return fmt.Errorf("discord channel not enabled")
	}

	// Map notification priority to Discord color
	color := 3447003 // Default blue
	switch notification.Priority {
	case PriorityNotificationEmergency:
		color = 15158332 // Red
	case PriorityNotificationHigh:
		color = 3066993 // Green
	}

	// Build Discord embed
	embed := map[string]interface{}{
		"title":       notification.Title,
		"description": notification.Message,
		"color":       color,
		"timestamp":   notification.Timestamp.Format(time.RFC3339),
		"footer": map[string]string{
			"text": fmt.Sprintf("NeuraTrade - %s", notification.Type),
		},
	}

	// Add fields for metadata if present
	if len(notification.Metadata) > 0 {
		var fields []map[string]interface{}
		for k, v := range notification.Metadata {
			fields = append(fields, map[string]interface{}{"name": k, "value": v, "inline": true})
		}
		embed["fields"] = fields
	}

	payload := map[string]interface{}{
		"embeds": []map[string]interface{}{embed},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.config.WebhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}

	c.logger.Info("Sent notification to Discord", "notification_id", notification.ID)
	return nil
}

// EmailChannelConfig holds email configuration.
type EmailChannelConfig struct {
	SMTPHost    string
	SMTPPort    int
	Username    string
	Password    string
	FromAddress string
	FromName    string
	Enabled     bool
}

// EmailChannel sends notifications via email.
type EmailChannel struct {
	config EmailChannelConfig
	logger *slog.Logger
}

// NewEmailChannel creates a new Email channel.
func NewEmailChannel(config EmailChannelConfig) *EmailChannel {
	return &EmailChannel{
		config: config,
		logger: telemetry.Logger().With("channel", "email"),
	}
}

// Name returns the channel name.
func (c *EmailChannel) Name() string {
	return "email"
}

// IsEnabled returns whether the channel is enabled.
func (c *EmailChannel) IsEnabled() bool {
	return c.config.Enabled &&
		c.config.SMTPHost != "" &&
		c.config.FromAddress != ""
}

// Priorities returns the priority levels this channel handles.
func (c *EmailChannel) Priorities() []NotificationPriority {
	return []NotificationPriority{PriorityNotificationLow, PriorityNotificationEmergency}
}

// Send sends a notification via email.
func (c *EmailChannel) Send(ctx context.Context, notification *Notification) error {
	if !c.IsEnabled() {
		return fmt.Errorf("email channel not enabled")
	}

	// Validate recipients
	if len(notification.Recipients) == 0 {
		return fmt.Errorf("no email recipients provided")
	}

	// Build email headers
	from := fmt.Sprintf("%s <%s>", c.config.FromName, c.config.FromAddress)
	to := strings.Join(notification.Recipients, ",")

	// Build email body
	subject := fmt.Sprintf("[NeuraTrade] %s - %s", notification.Priority, notification.Title)
	body := fmt.Sprintf(`%s

---
NeuraTrade Notification
Type: %s
Priority: %s
Timestamp: %s
`,
		notification.Message,
		notification.Type,
		notification.Priority,
		notification.Timestamp.Format(time.RFC3339),
	)

	// Add metadata if present
	if len(notification.Metadata) > 0 {
		body += "\nAdditional Info:\n"
		for k, v := range notification.Metadata {
			body += fmt.Sprintf("  %s: %s\n", k, v)
		}
	}

	// Set up email headers
	headers := make(map[string]string)
	headers["From"] = from
	headers["To"] = to
	headers["Subject"] = subject
	headers["MIME-Version"] = "1.0"
	headers["Content-Type"] = "text/plain; charset=\"utf-8\""

	// Build message
	var msg strings.Builder
	for k, v := range headers {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n")
	msg.WriteString(body)

	// Connect to SMTP server and send
	addr := fmt.Sprintf("%s:%d", c.config.SMTPHost, c.config.SMTPPort)

	auth := smtp.PlainAuth("", c.config.Username, c.config.Password, c.config.SMTPHost)

	err := smtp.SendMail(addr, auth, c.config.FromAddress, notification.Recipients, []byte(msg.String()))
	if err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	c.logger.Info("Sent notification via email", "notification_id", notification.ID, "recipients", len(notification.Recipients))
	return nil
}

// WebhookChannelConfig holds custom webhook configuration.
type WebhookChannelConfig struct {
	URL        string
	Headers    map[string]string
	Enabled    bool
	RetryCount int
	Timeout    time.Duration
}

// WebhookChannel sends notifications to a custom webhook.
type WebhookChannel struct {
	config WebhookChannelConfig
	client *http.Client
	logger *slog.Logger
}

// NewWebhookChannel creates a new Webhook channel.
func NewWebhookChannel(config WebhookChannelConfig) *WebhookChannel {
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	return &WebhookChannel{
		config: config,
		client: &http.Client{Timeout: timeout},
		logger: telemetry.Logger().With("channel", "webhook"),
	}
}

// Name returns the channel name.
func (c *WebhookChannel) Name() string {
	return "webhook"
}

// IsEnabled returns whether the channel is enabled.
func (c *WebhookChannel) IsEnabled() bool {
	return c.config.Enabled && c.config.URL != ""
}

// Priorities returns the priority levels this channel handles.
func (c *WebhookChannel) Priorities() []NotificationPriority {
	return []NotificationPriority{PriorityNotificationHigh, PriorityNotificationEmergency}
}

// Send sends a notification to a custom webhook.
func (c *WebhookChannel) Send(ctx context.Context, notification *Notification) error {
	if !c.IsEnabled() {
		return fmt.Errorf("webhook channel not enabled")
	}

	// Build payload
	payload := map[string]interface{}{
		"id":        notification.ID,
		"type":      notification.Type,
		"priority":  notification.Priority,
		"title":     notification.Title,
		"message":   notification.Message,
		"metadata":  notification.Metadata,
		"timestamp": notification.Timestamp.Format(time.RFC3339),
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	// Retry logic - create new request for each attempt
	var lastErr error
	for attempt := 0; attempt <= c.config.RetryCount; attempt++ {
		if attempt > 0 {
			c.logger.Info("Retrying webhook delivery", "attempt", attempt, "notification_id", notification.ID)
			time.Sleep(time.Duration(attempt) * time.Second)
		}

		// Create fresh request for each attempt
		req, err := http.NewRequestWithContext(ctx, "POST", c.config.URL, bytes.NewReader(jsonData))
		if err != nil {
			return fmt.Errorf("failed to create request: %w", err)
		}

		req.Header.Set("Content-Type", "application/json")
		for k, v := range c.config.Headers {
			req.Header.Set(k, v)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		_ = resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			c.logger.Info("Sent notification to webhook", "notification_id", notification.ID)
			return nil
		}

		lastErr = fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return fmt.Errorf("failed after %d retries: %w", c.config.RetryCount+1, lastErr)
}

// NotificationChannelsService wraps the channel registry for easy injection.
type NotificationChannelsService struct {
	registry *ChannelRegistry
	logger   *slog.Logger
}

// NewNotificationChannelsService creates a new notification channels service.
// Accepts an optional ChannelRegistry for dependency injection and testability.
func NewNotificationChannelsService(registry *ChannelRegistry) *NotificationChannelsService {
	if registry == nil {
		registry = NewChannelRegistry()
	}
	return &NotificationChannelsService{
		registry: registry,
		logger:   telemetry.Logger(),
	}
}

// Registry returns the channel registry.
func (s *NotificationChannelsService) Registry() *ChannelRegistry {
	return s.registry
}

// ConfigureDiscord configures the Discord channel.
func (s *NotificationChannelsService) ConfigureDiscord(config DiscordChannelConfig) {
	channel := NewDiscordChannel(config)
	s.registry.Register(channel)
}

// ConfigureEmail configures the Email channel.
func (s *NotificationChannelsService) ConfigureEmail(config EmailChannelConfig) {
	channel := NewEmailChannel(config)
	s.registry.Register(channel)
}

// ConfigureWebhook configures the custom Webhook channel.
func (s *NotificationChannelsService) ConfigureWebhook(config WebhookChannelConfig) {
	channel := NewWebhookChannel(config)
	s.registry.Register(channel)
}

// SendNotification sends a notification through appropriate channels.
func (s *NotificationChannelsService) SendNotification(ctx context.Context, notification *Notification) error {
	return s.registry.Send(ctx, notification)
}

// SendTradeExecuted sends a trade executed notification.
func (s *NotificationChannelsService) SendTradeExecuted(ctx context.Context, symbol, side, price, pnl string) error {
	notification := &Notification{
		ID:       fmt.Sprintf("trade-%s", uuid.New().String()),
		Type:     NotificationTypeTradeExecuted,
		Priority: PriorityNotificationHigh,
		Title:    fmt.Sprintf("Trade Executed: %s", symbol),
		Message:  fmt.Sprintf("%s %s @ %s\nPnL: %s", symbol, side, price, pnl),
		Metadata: map[string]string{
			"symbol": symbol,
			"side":   side,
			"price":  price,
			"pnl":    pnl,
		},
		Timestamp: time.Now(),
	}
	return s.SendNotification(ctx, notification)
}

// SendEmergency sends an emergency notification to all channels.
func (s *NotificationChannelsService) SendEmergency(ctx context.Context, title, message string) error {
	notification := &Notification{
		ID:       fmt.Sprintf("emergency-%s", uuid.New().String()),
		Type:     NotificationTypeEmergency,
		Priority: PriorityNotificationEmergency,
		Title:    "🚨 EMERGENCY: " + title,
		Message:  message,
		Metadata: map[string]string{
			"alert": "true",
		},
		Timestamp: time.Now(),
	}
	return s.SendNotification(ctx, notification)
}

// SendDailySummary sends a daily summary notification.
func (s *NotificationChannelsService) SendDailySummary(ctx context.Context, summary string, recipients []string) error {
	notification := &Notification{
		ID:         fmt.Sprintf("daily-%s", uuid.New().String()),
		Type:       NotificationTypeDailySummary,
		Priority:   PriorityNotificationLow,
		Title:      "Daily Trading Summary",
		Message:    summary,
		Timestamp:  time.Now(),
		Recipients: recipients,
	}
	return s.SendNotification(ctx, notification)
}
