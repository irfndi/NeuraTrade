// Package ports defines the application's port interfaces.
package ports

import (
	"context"
	"time"
)

// ============================================================
// Notification - Alerting and messaging
// ============================================================

// NotificationPriority represents the priority of a notification.
type NotificationPriority string

const (
	PriorityLow      NotificationPriority = "low"
	PriorityNormal   NotificationPriority = "normal"
	PriorityHigh     NotificationPriority = "high"
	PriorityCritical NotificationPriority = "critical"
)

// NotificationType represents the type of notification.
type NotificationType string

const (
	NotificationTypeInfo    NotificationType = "info"
	NotificationTypeWarning NotificationType = "warning"
	NotificationTypeError   NotificationType = "error"
	NotificationTypeSuccess NotificationType = "success"
	NotificationTypeTrade   NotificationType = "trade"
	NotificationTypeSignal  NotificationType = "signal"
	NotificationTypeRisk    NotificationType = "risk"
	NotificationTypeSystem  NotificationType = "system"
)

// Notification represents a notification to be sent.
type Notification struct {
	ID         string
	Type       NotificationType
	Priority   NotificationPriority
	Title      string
	Message    string
	Exchange   string // Optional context
	Symbol     string // Optional context
	StrategyID string // Optional context
	TraceID    string // For tracing
	Timestamp  time.Time
	Metadata   map[string]any
}

// NotificationResult represents the result of sending a notification.
type NotificationResult struct {
	ID      string
	Sent    bool
	Channel string
	SentAt  time.Time
	Error   string
}

// Notifier provides notification capabilities.
type Notifier interface {
	// Send sends a notification.
	Send(ctx context.Context, notification Notification) error

	// SendAsync sends a notification asynchronously.
	SendAsync(ctx context.Context, notification Notification) (<-chan NotificationResult, error)

	// SendBatch sends multiple notifications.
	SendBatch(ctx context.Context, notifications []Notification) error

	// IsEnabled checks if notifications are enabled.
	IsEnabled() bool
}

// ============================================================
// Notification Channels
// ============================================================

// NotificationChannel represents a specific notification channel.
type NotificationChannel interface {
	// Name returns the channel name.
	Name() string

	// Send sends a notification through this channel.
	Send(ctx context.Context, notification Notification) error

	// IsEnabled checks if this channel is enabled.
	IsEnabled() bool
}

// ============================================================
// Alert Manager - Structured alerting
// ============================================================

// AlertSeverity represents the severity of an alert.
type AlertSeverity string

const (
	AlertSeverityInfo     AlertSeverity = "info"
	AlertSeverityWarning  AlertSeverity = "warning"
	AlertSeverityCritical AlertSeverity = "critical"
)

// Alert represents a system alert.
type Alert struct {
	ID          string
	Name        string
	Severity    AlertSeverity
	Message     string
	Labels      map[string]string
	Annotations map[string]string
	StartsAt    time.Time
	EndsAt      *time.Time
	Status      string // firing, resolved
}

// AlertManager provides alerting capabilities.
type AlertManager interface {
	// Fire fires an alert.
	Fire(ctx context.Context, alert Alert) error

	// Resolve resolves an alert.
	Resolve(ctx context.Context, alertID string) error

	// GetActive returns all active alerts.
	GetActive(ctx context.Context) ([]Alert, error)
}

// ============================================================
// Notification Templates
// ============================================================

// NotificationTemplate represents a notification template.
type NotificationTemplate struct {
	ID        string
	Name      string
	Type      NotificationType
	Template  string
	Variables []string
}

// TemplateRenderer renders notification templates.
type TemplateRenderer interface {
	// Render renders a template with variables.
	Render(templateID string, variables map[string]any) (string, error)

	// RegisterTemplate registers a new template.
	RegisterTemplate(template NotificationTemplate) error
}
