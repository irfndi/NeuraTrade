package eventbus

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/eventbus"
)

func TestTradingEventBusBridge_BridgeEvent(t *testing.T) {
	platformBus := eventbus.New(eventbus.DefaultConfig())
	bridge := NewTradingEventBusBridge(platformBus, slog.Default())

	// Subscribe to the mapped topic on platform bus
	received := make(chan any, 1)
	_, err := platformBus.Subscribe(context.Background(), "market.tick", func(ctx context.Context, e eventbus.Event) error {
		received <- e.Payload
		return nil
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// Start bridge
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("failed to start bridge: %v", err)
	}
	defer bridge.Stop()

	// Bridge a legacy event
	payload := map[string]float64{"price": 100.0}
	err = bridge.BridgeEvent(context.Background(), "price_update", payload)
	if err != nil {
		t.Fatalf("failed to bridge event: %v", err)
	}

	// Wait for event
	select {
	case p := <-received:
		if p == nil {
			t.Error("received nil payload")
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for bridged event")
	}
}

func TestTradingEventBusBridge_LegacyEventHandler(t *testing.T) {
	platformBus := eventbus.New(eventbus.DefaultConfig())
	bridge := NewTradingEventBusBridge(platformBus, slog.Default())

	// Subscribe on platform bus
	received := make(chan any, 1)
	_, _ = platformBus.Subscribe(context.Background(), "order.filled", func(ctx context.Context, e eventbus.Event) error {
		received <- e.Payload
		return nil
	})

	// Get legacy handler
	handler := bridge.LegacyEventHandler("order_filled")

	// Call handler
	handler(map[string]string{"order_id": "123"})

	// Wait for event
	select {
	case p := <-received:
		if p == nil {
			t.Error("received nil payload")
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for bridged event")
	}
}

func TestTradingEventBusBridge_SubscribeToPlatform(t *testing.T) {
	platformBus := eventbus.New(eventbus.DefaultConfig())
	bridge := NewTradingEventBusBridge(platformBus, slog.Default())

	// Subscribe through bridge
	received := make(chan any, 1)
	_, err := bridge.SubscribeToPlatform(context.Background(), "test.topic", func(p any) {
		received <- p
	})
	if err != nil {
		t.Fatalf("failed to subscribe: %v", err)
	}

	// Publish directly on platform bus
	_ = platformBus.Publish(context.Background(), eventbus.NewEvent("test.topic", "test", "hello"))

	// Wait for event
	select {
	case p := <-received:
		if p != "hello" {
			t.Errorf("expected 'hello', got %v", p)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}
}

func TestTradingEventBusBridge_StartStop(t *testing.T) {
	platformBus := eventbus.New(eventbus.DefaultConfig())
	bridge := NewTradingEventBusBridge(platformBus, slog.Default())

	// Start
	if err := bridge.Start(context.Background()); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	if !bridge.IsRunning() {
		t.Error("expected bridge to be running")
	}

	// Double start should fail
	if err := bridge.Start(context.Background()); err == nil {
		t.Error("expected error on double start")
	}

	// Stop
	bridge.Stop()
	if bridge.IsRunning() {
		t.Error("expected bridge to be stopped")
	}

	// Double stop should be safe
	bridge.Stop()
}

func TestTradingEventBusBridge_NilBus(t *testing.T) {
	bridge := NewTradingEventBusBridge(nil, slog.Default())

	err := bridge.BridgeEvent(context.Background(), "test", "payload")
	if err == nil {
		t.Error("expected error with nil bus")
	}

	_, err = bridge.SubscribeToPlatform(context.Background(), "test", func(p any) {})
	if err == nil {
		t.Error("expected error with nil bus")
	}
}

func TestTradingEventTypeMapping(t *testing.T) {
	tests := []struct {
		legacy string
		want   string
	}{
		{"price_update", "market.tick"},
		{"signal_detected", "signal.proposed"},
		{"arbitrage_found", "trade.arbitrage"},
		{"order_filled", "order.filled"},
		{"unknown_type", "trading.unknown_type"},
	}

	for _, tt := range tests {
		t.Run(tt.legacy, func(t *testing.T) {
			got, ok := TradingEventTypeMapping[tt.legacy]
			if ok {
				if got != tt.want {
					t.Errorf("mapping[%s] = %s, want %s", tt.legacy, got, tt.want)
				}
			} else {
				// Unknown types should use fallback
				expected := "trading." + tt.legacy
				if tt.want != expected {
					t.Errorf("unknown type should map to %s", expected)
				}
			}
		})
	}
}

func TestTradingEventBusBridge_BatchBridgeEvents(t *testing.T) {
	platformBus := eventbus.New(eventbus.DefaultConfig())
	bridge := NewTradingEventBusBridge(platformBus, slog.Default())

	received := make(chan any, 3)

	// Subscribe to specific topics that will be published
	_, _ = platformBus.Subscribe(context.Background(), "market.tick", func(ctx context.Context, e eventbus.Event) error {
		received <- e.Payload
		return nil
	})
	_, _ = platformBus.Subscribe(context.Background(), "signal.proposed", func(ctx context.Context, e eventbus.Event) error {
		received <- e.Payload
		return nil
	})
	_, _ = platformBus.Subscribe(context.Background(), "order.filled", func(ctx context.Context, e eventbus.Event) error {
		received <- e.Payload
		return nil
	})

	events := []struct {
		Type    string
		Payload any
	}{
		{"price_update", 1},
		{"signal_detected", 2},
		{"order_filled", 3},
	}

	err := bridge.BatchBridgeEvents(context.Background(), events)
	if err != nil {
		t.Fatalf("failed to batch bridge: %v", err)
	}

	// Wait for all events
	for i := 0; i < 3; i++ {
		select {
		case <-received:
		case <-time.After(time.Second):
			t.Errorf("timeout waiting for event %d", i)
		}
	}
}
