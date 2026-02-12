package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfndi/neuratrade/internal/observability"
	"github.com/irfndi/neuratrade/internal/telemetry"
)

// ActionType defines the type of action being streamed
type ActionType string

const (
	// Action type constants for all streaming action types
	ActionTypeTrade          ActionType = "trade"
	ActionTypeQuestProgress  ActionType = "quest_progress"
	ActionTypeRiskEvent      ActionType = "risk_event"
	ActionTypeFundMilestone  ActionType = "fund_milestone"
	ActionTypeAIReasoning    ActionType = "ai_reasoning"
	ActionTypeArbitrage      ActionType = "arbitrage"
	ActionTypeSystemAlert    ActionType = "system_alert"
	ActionTypePositionUpdate ActionType = "position_update"
)

// ActionPriority defines the priority level of an action
type ActionPriority string

const (
	PriorityLow      ActionPriority = "low"
	PriorityNormal   ActionPriority = "normal"
	PriorityHigh     ActionPriority = "high"
	PriorityCritical ActionPriority = "critical"
)

// ActionStatus defines the status of an action
type ActionStatus string

const (
	StatusPending   ActionStatus = "pending"
	StatusExecuted  ActionStatus = "executed"
	StatusFailed    ActionStatus = "failed"
	StatusCancelled ActionStatus = "cancelled"
)

// StreamingAction represents a standardized action for streaming
type StreamingAction struct {
	// ID is the unique identifier for this action
	ID string `json:"id"`
	// Type is the action type
	Type ActionType `json:"type"`
	// Priority is the action priority level
	Priority ActionPriority `json:"priority"`
	// Status is the current action status
	Status ActionStatus `json:"status"`
	// Timestamp is when the action occurred
	Timestamp time.Time `json:"timestamp"`
	// UserID is the user this action relates to (optional)
	UserID string `json:"user_id,omitempty"`
	// ChatID is the Telegram chat ID for notifications (optional)
	ChatID string `json:"chat_id,omitempty"`
	// Title is a short summary of the action
	Title string `json:"title"`
	// Description provides detailed information about the action
	Description string `json:"description,omitempty"`
	// Data contains type-specific payload
	Data map[string]interface{} `json:"data,omitempty"`
	// Metadata contains additional context
	Metadata map[string]string `json:"metadata,omitempty"`
	// Source identifies the component that generated this action
	Source string `json:"source"`
	// CorrelationID links related actions together
	CorrelationID string `json:"correlation_id,omitempty"`
	// RequiresNotification indicates if this action should trigger a notification
	RequiresNotification bool `json:"requires_notification"`
	// NotificationSent tracks if notification was already sent
	NotificationSent bool `json:"notification_sent,omitempty"`
}

// ActionStreamer handles streaming actions to subscribers
type ActionStreamer struct {
	mu              sync.RWMutex
	subscribers     map[string]chan StreamingAction
	actionHistory   []StreamingAction
	maxHistorySize  int
	logger          *slog.Logger
	notificationSvc *NotificationService
}

// NewActionStreamer creates a new action streamer
func NewActionStreamer(notificationSvc *NotificationService) *ActionStreamer {
	return &ActionStreamer{
		subscribers:     make(map[string]chan StreamingAction),
		actionHistory:   make([]StreamingAction, 0),
		maxHistorySize:  1000,
		logger:          telemetry.Logger(),
		notificationSvc: notificationSvc,
	}
}

// Subscribe registers a new subscriber and returns a channel for actions
func (as *ActionStreamer) Subscribe(subscriberID string) <-chan StreamingAction {
	as.mu.Lock()
	defer as.mu.Unlock()

	// Check if subscriber already exists
	if _, exists := as.subscribers[subscriberID]; exists {
		as.logger.Warn("Subscriber already exists, replacing", "subscriber_id", subscriberID)
	}

	// Create buffered channel to prevent blocking
	ch := make(chan StreamingAction, 100)
	as.subscribers[subscriberID] = ch

	as.logger.Info("New action subscriber registered", "subscriber_id", subscriberID)
	return ch
}

// Unsubscribe removes a subscriber
func (as *ActionStreamer) Unsubscribe(subscriberID string) {
	as.mu.Lock()
	defer as.mu.Unlock()

	if ch, exists := as.subscribers[subscriberID]; exists {
		close(ch)
		delete(as.subscribers, subscriberID)
		as.logger.Info("Action subscriber unregistered", "subscriber_id", subscriberID)
	}
}

// Publish broadcasts an action to all subscribers
func (as *ActionStreamer) Publish(ctx context.Context, action StreamingAction) error {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpNotification, "ActionStreamer.Publish", map[string]string{
		"action_id":   action.ID,
		"action_type": string(action.Type),
	})
	defer observability.FinishSpan(span, nil)

	// Ensure action has required fields
	if action.ID == "" {
		action.ID = generateActionID(action.Type, action.Timestamp)
	}
	if action.Timestamp.IsZero() {
		action.Timestamp = time.Now().UTC()
	}

	as.mu.Lock()
	// Add to history
	as.actionHistory = append(as.actionHistory, action)
	if len(as.actionHistory) > as.maxHistorySize {
		as.actionHistory = as.actionHistory[1:]
	}

	// Get subscriber channels
	subscriberCount := len(as.subscribers)
	as.mu.Unlock()

	as.logger.Info("Publishing action",
		"action_id", action.ID,
		"action_type", action.Type,
		"subscribers", subscriberCount,
	)

	// Broadcast to subscribers (non-blocking)
	as.mu.RLock()
	for subID, ch := range as.subscribers {
		select {
		case ch <- action:
			// Successfully sent
		default:
			// Channel full, log warning
			as.logger.Warn("Subscriber channel full, dropping action",
				"subscriber_id", subID,
				"action_id", action.ID,
			)
		}
	}
	as.mu.RUnlock()

	// Send notification if required
	if action.RequiresNotification && as.notificationSvc != nil && !action.NotificationSent {
		go as.sendNotification(spanCtx, action)
	}

	observability.AddBreadcrumb(spanCtx, "action_stream", fmt.Sprintf("Action %s published to %d subscribers", action.ID, subscriberCount), sentry.LevelInfo)

	return nil
}

// sendNotification sends a notification for the action
func (as *ActionStreamer) sendNotification(ctx context.Context, action StreamingAction) {
	if as.notificationSvc == nil || action.ChatID == "" {
		return
	}

	message := as.formatActionMessage(action)

	// Parse chat ID
	var chatID int64
	if _, err := fmt.Sscanf(action.ChatID, "%d", &chatID); err != nil {
		as.logger.Error("Failed to parse chat ID", "chat_id", action.ChatID, "error", err)
		return
	}

	if err := as.notificationSvc.sendTelegramMessage(ctx, chatID, message); err != nil {
		as.logger.Error("Failed to send action notification",
			"action_id", action.ID,
			"chat_id", action.ChatID,
			"error", err,
		)
		return
	}

	// Mark notification as sent
	as.mu.Lock()
	for i := range as.actionHistory {
		if as.actionHistory[i].ID == action.ID {
			as.actionHistory[i].NotificationSent = true
			break
		}
	}
	as.mu.Unlock()

	as.logger.Info("Action notification sent", "action_id", action.ID, "chat_id", action.ChatID)
}

// formatActionMessage formats an action for Telegram notification
func (as *ActionStreamer) formatActionMessage(action StreamingAction) string {
	var emoji string
	switch action.Type {
	case ActionTypeTrade:
		emoji = "📈"
	case ActionTypeQuestProgress:
		emoji = "🎯"
	case ActionTypeRiskEvent:
		emoji = "⚠️"
	case ActionTypeFundMilestone:
		emoji = "💰"
	case ActionTypeAIReasoning:
		emoji = "🤖"
	case ActionTypeArbitrage:
		emoji = "⚡"
	case ActionTypeSystemAlert:
		emoji = "🔔"
	case ActionTypePositionUpdate:
		emoji = "📊"
	default:
		emoji = "📤"
	}

	priorityEmoji := ""
	switch action.Priority {
	case PriorityCritical:
		priorityEmoji = "🚨"
	case PriorityHigh:
		priorityEmoji = "⬆️"
	case PriorityLow:
		priorityEmoji = "⬇️"
	}

	lines := []string{
		fmt.Sprintf("%s%s **%s**", emoji, priorityEmoji, action.Title),
		"",
		fmt.Sprintf("Status: %s", action.Status),
		fmt.Sprintf("Time: %s", action.Timestamp.Format(time.RFC3339)),
	}

	if action.Description != "" {
		lines = append(lines, "", action.Description)
	}

	// Add type-specific data
	if len(action.Data) > 0 {
		lines = append(lines, "", "**Details:**")
		for key, value := range action.Data {
			lines = append(lines, fmt.Sprintf("• %s: %v", key, value))
		}
	}

	return fmt.Sprintf("```\n%s\n```", joinLines(lines))
}

// GetHistory returns recent action history
func (as *ActionStreamer) GetHistory(limit int, filterType ...ActionType) []StreamingAction {
	as.mu.RLock()
	defer as.mu.RUnlock()

	var result []StreamingAction
	for i := len(as.actionHistory) - 1; i >= 0 && len(result) < limit; i-- {
		action := as.actionHistory[i]
		// Apply type filter if specified
		if len(filterType) > 0 {
			matched := false
			for _, t := range filterType {
				if action.Type == t {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		result = append(result, action)
	}

	return result
}

// GetActionByID retrieves a specific action by ID
func (as *ActionStreamer) GetActionByID(actionID string) *StreamingAction {
	as.mu.RLock()
	defer as.mu.RUnlock()

	for i := len(as.actionHistory) - 1; i >= 0; i-- {
		if as.actionHistory[i].ID == actionID {
			action := as.actionHistory[i]
			return &action
		}
	}

	return nil
}

// GetSubscriberCount returns the current number of subscribers
func (as *ActionStreamer) GetSubscriberCount() int {
	as.mu.RLock()
	defer as.mu.RUnlock()
	return len(as.subscribers)
}

// Helper functions

func generateActionID(actionType ActionType, timestamp time.Time) string {
	return fmt.Sprintf("%s-%d", actionType, timestamp.UnixNano())
}

func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}

// ActionBuilder provides a fluent interface for building actions
type ActionBuilder struct {
	action StreamingAction
}

// NewActionBuilder creates a new action builder
func NewActionBuilder(actionType ActionType) *ActionBuilder {
	return &ActionBuilder{
		action: StreamingAction{
			ID:                   generateActionID(actionType, time.Now().UTC()),
			Type:                 actionType,
			Priority:             PriorityNormal,
			Status:               StatusExecuted,
			Timestamp:            time.Now().UTC(),
			Data:                 make(map[string]interface{}),
			Metadata:             make(map[string]string),
			RequiresNotification: false,
		},
	}
}

// WithID sets the action ID
func (ab *ActionBuilder) WithID(id string) *ActionBuilder {
	ab.action.ID = id
	return ab
}

// WithPriority sets the action priority
func (ab *ActionBuilder) WithPriority(priority ActionPriority) *ActionBuilder {
	ab.action.Priority = priority
	return ab
}

// WithStatus sets the action status
func (ab *ActionBuilder) WithStatus(status ActionStatus) *ActionBuilder {
	ab.action.Status = status
	return ab
}

// WithUserID sets the user ID
func (ab *ActionBuilder) WithUserID(userID string) *ActionBuilder {
	ab.action.UserID = userID
	return ab
}

// WithChatID sets the chat ID for notifications
func (ab *ActionBuilder) WithChatID(chatID string) *ActionBuilder {
	ab.action.ChatID = chatID
	return ab
}

// WithTitle sets the action title
func (ab *ActionBuilder) WithTitle(title string) *ActionBuilder {
	ab.action.Title = title
	return ab
}

// WithDescription sets the action description
func (ab *ActionBuilder) WithDescription(description string) *ActionBuilder {
	ab.action.Description = description
	return ab
}

// WithData adds a key-value pair to the action data
func (ab *ActionBuilder) WithData(key string, value interface{}) *ActionBuilder {
	ab.action.Data[key] = value
	return ab
}

// WithMetadata adds a key-value pair to the action metadata
func (ab *ActionBuilder) WithMetadata(key, value string) *ActionBuilder {
	ab.action.Metadata[key] = value
	return ab
}

// WithSource sets the action source
func (ab *ActionBuilder) WithSource(source string) *ActionBuilder {
	ab.action.Source = source
	return ab
}

// WithCorrelationID sets the correlation ID
func (ab *ActionBuilder) WithCorrelationID(correlationID string) *ActionBuilder {
	ab.action.CorrelationID = correlationID
	return ab
}

// WithNotification enables notification for this action
func (ab *ActionBuilder) WithNotification(enabled bool) *ActionBuilder {
	ab.action.RequiresNotification = enabled
	return ab
}

// Build returns the constructed action
func (ab *ActionBuilder) Build() StreamingAction {
	return ab.action
}

// ToJSON converts the action to JSON
func (a StreamingAction) ToJSON() (string, error) {
	bytes, err := json.Marshal(a)
	if err != nil {
		return "", fmt.Errorf("failed to marshal action: %w", err)
	}
	return string(bytes), nil
}

// ParseActionFromJSON parses an action from JSON
func ParseActionFromJSON(data string) (StreamingAction, error) {
	var action StreamingAction
	if err := json.Unmarshal([]byte(data), &action); err != nil {
		return StreamingAction{}, fmt.Errorf("failed to unmarshal action: %w", err)
	}
	return action, nil
}
