// Package eventbus provides adapters for the event bus port interface.
// This file provides a bridge between the legacy TradingEventBus and the platform EventBus.
package eventbus

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/eventbus"
)

// TradingEventBusBridge bridges the legacy services.TradingEventBus to the platform EventBus.
// This enables gradual migration - services can emit to either bus and events flow to both.
type TradingEventBusBridge struct {
	platformBus *eventbus.Bus
	logger      *slog.Logger
	mu          sync.RWMutex
	running     bool
	stopCh      chan struct{}
}

// NewTradingEventBusBridge creates a new bridge between legacy and platform event buses.
func NewTradingEventBusBridge(platformBus *eventbus.Bus, logger *slog.Logger) *TradingEventBusBridge {
	if logger == nil {
		logger = slog.Default()
	}
	return &TradingEventBusBridge{
		platformBus: platformBus,
		logger:      logger,
		stopCh:      make(chan struct{}),
	}
}

// TradingEventTypeMapping maps legacy event types to platform topics.
var TradingEventTypeMapping = map[string]string{
	"price_update":    "market.tick",
	"signal_detected": "signal.proposed",
	"arbitrage_found": "trade.arbitrage",
	"order_filled":    "order.filled",
	"order_rejected":  "order.rejected",
	"stop_triggered":  "position.stop_triggered",
	"drawdown_alert":  "risk.drawdown_alert",
	"emergency_stop":  "risk.emergency_stop",
	"odds_change":     "market.odds_change",
	"event_resolved":  "market.resolved",
}

// BridgeEvent converts a legacy TradingEvent to a platform Event and publishes it.
func (b *TradingEventBusBridge) BridgeEvent(ctx context.Context, legacyType string, payload any) error {
	if b.platformBus == nil {
		return fmt.Errorf("trading_event_bridge: platform bus not initialized")
	}

	// Map legacy type to platform topic
	topic, ok := TradingEventTypeMapping[legacyType]
	if !ok {
		topic = "trading." + legacyType
	}

	event := eventbus.NewEvent(topic, legacyType, payload).WithSource("trading_event_bridge")
	event.Timestamp = time.Now()
	if err := b.platformBus.Publish(ctx, event); err != nil {
		b.logger.Warn("failed to bridge event to platform bus",
			"legacy_type", legacyType,
			"topic", topic,
			"error", err)
		return err
	}

	b.logger.Debug("bridged event to platform",
		"legacy_type", legacyType,
		"topic", topic)

	return nil
}

// Start begins the bridge (currently a no-op as the bridge is passive).
func (b *TradingEventBusBridge) Start(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.running {
		return fmt.Errorf("trading_event_bridge: already running")
	}

	b.running = true
	b.logger.Info("trading event bus bridge started")

	return nil
}

// Stop gracefully stops the bridge.
func (b *TradingEventBusBridge) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if !b.running {
		return
	}

	close(b.stopCh)
	b.running = false
	b.logger.Info("trading event bus bridge stopped")
}

// IsRunning returns whether the bridge is currently running.
func (b *TradingEventBusBridge) IsRunning() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.running
}

// LegacyEventHandler is a handler function that can be registered with the legacy bus
// and forwards events to the platform bus.
func (b *TradingEventBusBridge) LegacyEventHandler(eventType string) func(any) {
	return func(payload any) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := b.BridgeEvent(ctx, eventType, payload); err != nil {
			b.logger.Error("failed to bridge legacy event",
				"event_type", eventType,
				"error", err)
		}
	}
}

// SubscribeToPlatform subscribes to events on the platform bus with a legacy-style handler.
// This enables legacy code to receive events from the platform bus.
func (b *TradingEventBusBridge) SubscribeToPlatform(ctx context.Context, topic string, handler func(any)) (*eventbus.Subscription, error) {
	if b.platformBus == nil {
		return nil, fmt.Errorf("trading_event_bridge: platform bus not initialized")
	}

	wrappedHandler := func(ctx context.Context, event eventbus.Event) error {
		handler(event.Payload)
		return nil
	}

	return b.platformBus.Subscribe(ctx, topic, wrappedHandler)
}

// BatchBridgeEvents bridges multiple events at once for efficiency.
func (b *TradingEventBusBridge) BatchBridgeEvents(ctx context.Context, events []struct {
	Type    string
	Payload any
}) error {
	for _, e := range events {
		if err := b.BridgeEvent(ctx, e.Type, e.Payload); err != nil {
			return err
		}
	}
	return nil
}
