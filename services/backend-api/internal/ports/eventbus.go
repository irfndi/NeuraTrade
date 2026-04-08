// Package ports defines the application's port interfaces.
package ports

import (
	"context"
)

// ============================================================
// Event Bus - Pub/Sub contract
// ============================================================

// Event represents a domain event.
type Event interface {
	// EventType returns the type of the event.
	EventType() string

	// AggregateID returns the aggregate that produced the event.
	AggregateID() string

	// Timestamp returns when the event occurred.
	Timestamp() int64
}

// EventHandler handles events.
type EventHandler func(ctx context.Context, event Event) error

// EventBus provides pub/sub capabilities.
type EventBus interface {
	// Publish publishes an event.
	Publish(ctx context.Context, event Event) error

	// Subscribe subscribes to events of a specific type.
	Subscribe(ctx context.Context, eventType string, handler EventHandler) error

	// SubscribeAll subscribes to all events.
	SubscribeAll(ctx context.Context, handler EventHandler) error

	// Unsubscribe removes a subscription.
	Unsubscribe(ctx context.Context, eventType string) error
}

// ============================================================
// Common Event Types
// ============================================================

// BaseEvent provides a base implementation for events.
type BaseEvent struct {
	Type       string
	Aggregate  string
	OccurredAt int64
}

func (e BaseEvent) EventType() string   { return e.Type }
func (e BaseEvent) AggregateID() string { return e.Aggregate }
func (e BaseEvent) Timestamp() int64    { return e.OccurredAt }

// Market Events
const (
	EventTypeMarketTick   = "market.tick"
	EventTypeMarketCandle = "market.candle"
	EventTypeOrderBook    = "market.orderbook"
	EventTypeFundingRate  = "market.funding_rate"
)

// Signal Events
const (
	EventTypeSignalProposed         = "signal.proposed"
	EventTypeSignalApproved         = "signal.approved"
	EventTypeSignalRejected         = "signal.rejected"
	EventTypeSignalSkipped          = "signal.skipped"
	EventTypeScalpingSignalProposed = "signal.scalping_proposed"
)

// Order Events
const (
	EventTypeOrderIntentApproved = "order.intent_approved"
	EventTypeOrderIntentRejected = "order.intent_rejected"
	EventTypeOrderPlaced         = "order.placed"
	EventTypeOrderFilled         = "order.filled"
	EventTypeOrderCancelled      = "order.cancelled"
	EventTypeOrderRejected       = "order.rejected"
)

// Position Events
const (
	EventTypePositionOpened     = "position.opened"
	EventTypePositionUpdated    = "position.updated"
	EventTypePositionClosed     = "position.closed"
	EventTypePositionLiquidated = "position.liquidated"
)

// Risk Events
const (
	EventTypeRiskLimitBreached = "risk.limit_breached"
	EventTypeSafeModeEnabled   = "risk.safe_mode_enabled"
	EventTypeKillSwitchEngaged = "risk.kill_switch_engaged"
	EventTypeDailyLossCapHit   = "risk.daily_loss_cap"
	EventTypeMaxDrawdownHit    = "risk.max_drawdown"
)

// System Events
const (
	EventTypeCollectorDegraded    = "system.collector_degraded"
	EventTypeCollectorRecovered   = "system.collector_recovered"
	EventTypeExchangeConnected    = "system.exchange_connected"
	EventTypeExchangeDisconnected = "system.exchange_disconnected"
	EventTypePluginLoaded         = "system.plugin_loaded"
	EventTypePluginUnloaded       = "system.plugin_unloaded"
)
