// Package eventbus provides adapters for the event bus port interface.
// This adapter bridges the platform eventbus to the ports.EventBus interface.
package eventbus

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
)

// PlatformEventBusAdapter adapts the platform eventbus.Bus to the ports.EventBus interface.
// This allows services to use the typed ports.Event interface while the platform
// handles the underlying pub/sub mechanics.
type PlatformEventBusAdapter struct {
	bus   *eventbus.Bus
	mu    sync.RWMutex
	subs  map[string][]*eventbus.Subscription
	actor *actor.Ref
}

// NewPlatformEventBusAdapter creates a new adapter wrapping the platform event bus.
func NewPlatformEventBusAdapter(bus *eventbus.Bus) *PlatformEventBusAdapter {
	return &PlatformEventBusAdapter{
		bus:  bus,
		subs: make(map[string][]*eventbus.Subscription),
	}
}

// WithActor sets an actor reference for message processing (optional).
func (a *PlatformEventBusAdapter) WithActor(ref *actor.Ref) *PlatformEventBusAdapter {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.actor = ref
	return a
}

// Publish publishes an event to the bus.
func (a *PlatformEventBusAdapter) Publish(ctx context.Context, event ports.Event) error {
	if a.bus == nil {
		return fmt.Errorf("eventbus: bus not initialized")
	}

	// Use eventType as topic for consistent routing
	pe := eventbus.NewEvent(event.EventType(), event.EventType(), event)
	pe.Timestamp = time.Unix(event.Timestamp(), 0)
	return a.bus.Publish(ctx, pe)
}

// Subscribe subscribes to events of a specific type.
func (a *PlatformEventBusAdapter) Subscribe(ctx context.Context, eventType string, handler ports.EventHandler) error {
	if a.bus == nil {
		return fmt.Errorf("eventbus: bus not initialized")
	}

	// Wrap the handler to convert between event types
	wrappedHandler := func(ctx context.Context, e eventbus.Event) error {
		// Try to extract the ports.Event from the payload
		if pe, ok := e.Payload.(ports.Event); ok {
			return handler(ctx, pe)
		}
		// Create a wrapper event if payload is not a ports.Event
		wrapper := &eventWrapper{
			eventType:   e.Type,
			aggregateID: e.Topic,
			timestamp:   e.Timestamp.Unix(),
			payload:     e.Payload,
		}
		return handler(ctx, wrapper)
	}

	sub, err := a.bus.Subscribe(ctx, eventType, wrappedHandler)
	if err != nil {
		return err
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	a.subs[eventType] = append(a.subs[eventType], sub)

	return nil
}

// SubscribeAll subscribes to all events using wildcard pattern.
func (a *PlatformEventBusAdapter) SubscribeAll(ctx context.Context, handler ports.EventHandler) error {
	if a.bus == nil {
		return fmt.Errorf("eventbus: bus not initialized")
	}

	// Use wildcard to match all events
	return a.Subscribe(ctx, "*", handler)
}

// Unsubscribe removes all subscriptions for an event type.
func (a *PlatformEventBusAdapter) Unsubscribe(ctx context.Context, eventType string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	subs, ok := a.subs[eventType]
	if !ok {
		return nil
	}

	for _, sub := range subs {
		sub.Unsubscribe()
	}
	delete(a.subs, eventType)

	return nil
}

// Stop gracefully stops the adapter and all subscriptions.
func (a *PlatformEventBusAdapter) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()

	for _, subs := range a.subs {
		for _, sub := range subs {
			sub.Unsubscribe()
		}
	}
	a.subs = make(map[string][]*eventbus.Subscription)
}

// eventWrapper wraps a platform event to implement ports.Event interface.
type eventWrapper struct {
	eventType   string
	aggregateID string
	timestamp   int64
	payload     any
}

func (e *eventWrapper) EventType() string   { return e.eventType }
func (e *eventWrapper) AggregateID() string { return e.aggregateID }
func (e *eventWrapper) Timestamp() int64    { return e.timestamp }
func (e *eventWrapper) Payload() any        { return e.payload }

// TradingEventAdapter wraps trading events to implement ports.Event.
type TradingEventAdapter struct {
	BaseEvent
	Payload any
}

// NewTradingEvent creates a new trading event adapter.
func NewTradingEvent(eventType, aggregateID string, payload any) *TradingEventAdapter {
	return &TradingEventAdapter{
		BaseEvent: BaseEvent{
			Type:       eventType,
			Aggregate:  aggregateID,
			OccurredAt: time.Now().Unix(),
		},
		Payload: payload,
	}
}

// BaseEvent provides a base implementation for trading events.
type BaseEvent struct {
	Type       string
	Aggregate  string
	OccurredAt int64
}

func (e BaseEvent) EventType() string   { return e.Type }
func (e BaseEvent) AggregateID() string { return e.Aggregate }
func (e BaseEvent) Timestamp() int64    { return e.OccurredAt }
