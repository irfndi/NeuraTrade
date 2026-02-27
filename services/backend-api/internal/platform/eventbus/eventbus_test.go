package eventbus

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestNewEvent(t *testing.T) {
	e := NewEvent("market", "tick", map[string]any{"price": 100.0})

	if e.Topic != "market" {
		t.Errorf("expected topic 'market', got %s", e.Topic)
	}
	if e.Type != "tick" {
		t.Errorf("expected type 'tick', got %s", e.Type)
	}
	if e.Timestamp.IsZero() {
		t.Error("timestamp should be set")
	}
}

func TestEventWithMethods(t *testing.T) {
	e := NewEvent("test", "test", nil).
		WithTraceID("trace-123").
		WithSource("test-source").
		WithMetadata("key", "value")

	if e.TraceID != "trace-123" {
		t.Errorf("expected trace ID 'trace-123', got %s", e.TraceID)
	}
	if e.Source != "test-source" {
		t.Errorf("expected source 'test-source', got %s", e.Source)
	}
	if e.Metadata["key"] != "value" {
		t.Errorf("expected metadata key=value, got %v", e.Metadata)
	}
}

func TestBusSubscribe(t *testing.T) {
	bus := New(DefaultConfig())
	defer bus.Stop()
	ctx := context.Background()

	var received atomic.Int32
	sub, err := bus.Subscribe(ctx, "test", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})

	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	if sub == nil {
		t.Fatal("subscription should not be nil")
	}

	// Give subscriber time to start
	time.Sleep(10 * time.Millisecond)

	if bus.SubscriberCount("test") != 1 {
		t.Errorf("expected 1 subscriber, got %d", bus.SubscriberCount("test"))
	}
}

func TestBusPublish(t *testing.T) {
	bus := New(DefaultConfig())
	defer bus.Stop()
	ctx := context.Background()

	var received atomic.Int32
	_, err := bus.Subscribe(ctx, "test", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	event := NewEvent("test", "test", nil)
	if err := bus.Publish(ctx, event); err != nil {
		t.Fatalf("Publish failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	if received.Load() != 1 {
		t.Errorf("expected 1 event, got %d", received.Load())
	}
}

func TestBusPublishToWrongTopic(t *testing.T) {
	bus := New(DefaultConfig())
	defer bus.Stop()
	ctx := context.Background()

	var received atomic.Int32
	_, err := bus.Subscribe(ctx, "topic-a", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Publish to different topic
	event := NewEvent("topic-b", "test", nil)
	bus.Publish(ctx, event)

	time.Sleep(50 * time.Millisecond)

	if received.Load() != 0 {
		t.Errorf("should not receive event on wrong topic, got %d", received.Load())
	}
}

func TestBusUnsubscribe(t *testing.T) {
	bus := New(DefaultConfig())
	defer bus.Stop()
	ctx := context.Background()

	sub, err := bus.Subscribe(ctx, "test", func(ctx context.Context, event Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if bus.SubscriberCount("test") != 1 {
		t.Errorf("expected 1 subscriber, got %d", bus.SubscriberCount("test"))
	}

	sub.Unsubscribe()
	time.Sleep(10 * time.Millisecond)

	if bus.SubscriberCount("test") != 0 {
		t.Errorf("expected 0 subscribers after unsubscribe, got %d", bus.SubscriberCount("test"))
	}
}

func TestBusWildcard(t *testing.T) {
	config := DefaultConfig()
	bus := New(config)
	defer bus.Stop()
	ctx := context.Background()

	var received atomic.Int32

	// Subscribe with wildcard
	_, err := bus.Subscribe(ctx, "market.*", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Publish to matching topics
	bus.Publish(ctx, NewEvent("market.tick", "test", nil))
	bus.Publish(ctx, NewEvent("market.candle", "test", nil))
	bus.Publish(ctx, NewEvent("other.tick", "test", nil)) // Should not match

	time.Sleep(100 * time.Millisecond)

	if received.Load() != 2 {
		t.Errorf("expected 2 events (market.*), got %d", received.Load())
	}
}

func TestBusMultipleSubscribers(t *testing.T) {
	bus := New(DefaultConfig())
	defer bus.Stop()
	ctx := context.Background()

	var received atomic.Int32

	// Multiple subscribers on same topic
	for i := 0; i < 3; i++ {
		_, err := bus.Subscribe(ctx, "test", func(ctx context.Context, event Event) error {
			received.Add(1)
			return nil
		})
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}
	}

	time.Sleep(10 * time.Millisecond)

	bus.Publish(ctx, NewEvent("test", "test", nil))

	time.Sleep(100 * time.Millisecond)

	if received.Load() != 3 {
		t.Errorf("expected 3 events (3 subscribers), got %d", received.Load())
	}
}

func TestBusPublishSync(t *testing.T) {
	bus := New(DefaultConfig())
	defer bus.Stop()
	ctx := context.Background()

	var received atomic.Int32
	_, err := bus.Subscribe(ctx, "test", func(ctx context.Context, event Event) error {
		received.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	event := NewEvent("test", "test", nil)
	if err := bus.PublishSync(ctx, event); err != nil {
		t.Fatalf("PublishSync failed: %v", err)
	}

	// Sync publish should have processed immediately
	if received.Load() != 1 {
		t.Errorf("expected 1 event (sync), got %d", received.Load())
	}
}

func TestBusStop(t *testing.T) {
	bus := New(DefaultConfig())
	ctx := context.Background()

	_, err := bus.Subscribe(ctx, "test", func(ctx context.Context, event Event) error {
		return nil
	})
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	bus.Stop()

	if !bus.stopped.Load() {
		t.Error("bus should be stopped")
	}

	// Should fail to subscribe after stop
	_, err = bus.Subscribe(ctx, "test2", func(ctx context.Context, event Event) error {
		return nil
	})
	if err != ErrBusStopped {
		t.Errorf("expected ErrBusStopped, got %v", err)
	}

	// Should fail to publish after stop
	err = bus.Publish(ctx, NewEvent("test", "test", nil))
	if err != ErrBusStopped {
		t.Errorf("expected ErrBusStopped, got %v", err)
	}
}

func TestBusTotalSubscribers(t *testing.T) {
	bus := New(DefaultConfig())
	defer bus.Stop()
	ctx := context.Background()

	if bus.TotalSubscribers() != 0 {
		t.Errorf("expected 0 total subscribers, got %d", bus.TotalSubscribers())
	}

	for i := 0; i < 3; i++ {
		_, err := bus.Subscribe(ctx, "test", func(ctx context.Context, event Event) error {
			return nil
		})
		if err != nil {
			t.Fatalf("Subscribe failed: %v", err)
		}
	}

	time.Sleep(10 * time.Millisecond)

	if bus.TotalSubscribers() != 3 {
		t.Errorf("expected 3 total subscribers, got %d", bus.TotalSubscribers())
	}
}

func TestBusEmptyTopic(t *testing.T) {
	bus := New(DefaultConfig())
	ctx := context.Background()

	_, err := bus.Subscribe(ctx, "", func(ctx context.Context, event Event) error {
		return nil
	})
	if err != ErrTopicEmpty {
		t.Errorf("expected ErrTopicEmpty, got %v", err)
	}
}
