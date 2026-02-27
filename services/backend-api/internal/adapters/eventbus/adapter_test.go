package eventbus

import (
	"context"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
)

func TestPlatformEventBusAdapter_Publish(t *testing.T) {
	bus := eventbus.New(eventbus.DefaultConfig())
	adapter := NewPlatformEventBusAdapter(bus)

	event := &testEvent{
		eventType:   "test.event",
		aggregateID: "test-agg",
		timestamp:   time.Now().Unix(),
	}

	// Subscribe first
	received := make(chan ports.Event, 1)
	err := adapter.Subscribe(context.Background(), "test.event", func(ctx context.Context, e ports.Event) error {
		received <- e
		return nil
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// Publish
	err = adapter.Publish(context.Background(), event)
	if err != nil {
		t.Fatalf("failed to publish: %v", err)
	}

	// Wait for event
	select {
	case e := <-received:
		if e.EventType() != event.eventType {
			t.Errorf("expected event type %s, got %s", event.eventType, e.EventType())
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}
}

func TestPlatformEventBusAdapter_Unsubscribe(t *testing.T) {
	bus := eventbus.New(eventbus.DefaultConfig())
	adapter := NewPlatformEventBusAdapter(bus)

	callCount := 0
	err := adapter.Subscribe(context.Background(), "test.event", func(ctx context.Context, e ports.Event) error {
		callCount++
		return nil
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	event := &testEvent{eventType: "test.event", aggregateID: "test"}

	// Publish first event
	_ = adapter.Publish(context.Background(), event)
	time.Sleep(10 * time.Millisecond)

	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}

	// Unsubscribe
	err = adapter.Unsubscribe(context.Background(), "test.event")
	if err != nil {
		t.Fatalf("failed to unsubscribe: %v", err)
	}

	// Publish second event (should not be received)
	_ = adapter.Publish(context.Background(), event)
	time.Sleep(10 * time.Millisecond)

	if callCount != 1 {
		t.Errorf("expected still 1 call after unsubscribe, got %d", callCount)
	}
}

func TestPlatformEventBusAdapter_Stop(t *testing.T) {
	bus := eventbus.New(eventbus.DefaultConfig())
	adapter := NewPlatformEventBusAdapter(bus)

	// Subscribe to multiple event types
	for i := 0; i < 3; i++ {
		_ = adapter.Subscribe(context.Background(), "test.event", func(ctx context.Context, e ports.Event) error {
			return nil
		})
	}

	adapter.Stop()

	// After stop, subscriptions should be cleared
	adapter.mu.RLock()
	if len(adapter.subs) != 0 {
		t.Errorf("expected 0 subscriptions after stop, got %d", len(adapter.subs))
	}
	adapter.mu.RUnlock()
}

func TestTradingEventAdapter(t *testing.T) {
	payload := map[string]string{"key": "value"}
	event := NewTradingEvent("order.filled", "order-123", payload)

	if event.EventType() != "order.filled" {
		t.Errorf("expected EventType 'order.filled', got %s", event.EventType())
	}

	if event.AggregateID() != "order-123" {
		t.Errorf("expected AggregateID 'order-123', got %s", event.AggregateID())
	}

	if event.Timestamp() == 0 {
		t.Error("expected non-zero timestamp")
	}

	if event.Payload == nil {
		t.Error("expected non-nil payload")
	}
}

func TestEventWrapper(t *testing.T) {
	wrapper := &eventWrapper{
		eventType:   "test.type",
		aggregateID: "test-agg",
		timestamp:   12345,
		payload:     "test-payload",
	}

	if wrapper.EventType() != "test.type" {
		t.Errorf("expected EventType 'test.type', got %s", wrapper.EventType())
	}

	if wrapper.AggregateID() != "test-agg" {
		t.Errorf("expected AggregateID 'test-agg', got %s", wrapper.AggregateID())
	}

	if wrapper.Timestamp() != 12345 {
		t.Errorf("expected Timestamp 12345, got %d", wrapper.Timestamp())
	}

	if wrapper.Payload() != "test-payload" {
		t.Errorf("expected Payload 'test-payload', got %v", wrapper.Payload())
	}
}

// testEvent implements ports.Event for testing.
type testEvent struct {
	eventType   string
	aggregateID string
	timestamp   int64
}

func (e *testEvent) EventType() string   { return e.eventType }
func (e *testEvent) AggregateID() string { return e.aggregateID }
func (e *testEvent) Timestamp() int64    { return e.timestamp }
