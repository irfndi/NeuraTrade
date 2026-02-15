package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

type TradingEventType string

const (
	EventPriceUpdate    TradingEventType = "price_update"
	EventSignalDetected TradingEventType = "signal_detected"
	EventArbitrageFound TradingEventType = "arbitrage_found"
	EventOrderFilled    TradingEventType = "order_filled"
	EventOrderRejected  TradingEventType = "order_rejected"
	EventStopTriggered  TradingEventType = "stop_triggered"
	EventDrawdownAlert  TradingEventType = "drawdown_alert"
	EventEmergencyStop  TradingEventType = "emergency_stop"
	EventOddsChange     TradingEventType = "odds_change"
	EventResolved       TradingEventType = "event_resolved"
)

type PriceEvent struct {
	Symbol    string          `json:"symbol"`
	Exchange  string          `json:"exchange"`
	Price     decimal.Decimal `json:"price"`
	Volume    decimal.Decimal `json:"volume"`
	Timestamp time.Time       `json:"timestamp"`
}

type SignalEvent struct {
	SignalID   string    `json:"signal_id"`
	Symbol     string    `json:"symbol"`
	SignalType string    `json:"signal_type"`
	Action     string    `json:"action"`
	Confidence float64   `json:"confidence"`
	Timestamp  time.Time `json:"timestamp"`
}

// ArbitrageEvent represents an arbitrage opportunity event.
type ArbitrageEvent struct {
	OpportunityID string          `json:"opportunity_id"`
	Symbol        string          `json:"symbol"`
	BuyExchange   string          `json:"buy_exchange"`
	SellExchange  string          `json:"sell_exchange"`
	ProfitPercent decimal.Decimal `json:"profit_percent"`
	Volume        decimal.Decimal `json:"volume"`
	Timestamp     time.Time       `json:"timestamp"`
}

// FillEvent represents an order fill event.
type FillEvent struct {
	OrderID   string          `json:"order_id"`
	Symbol    string          `json:"symbol"`
	Side      string          `json:"side"`
	Price     decimal.Decimal `json:"price"`
	Quantity  decimal.Decimal `json:"quantity"`
	Fee       decimal.Decimal `json:"fee"`
	Timestamp time.Time       `json:"timestamp"`
}

// RejectEvent represents an order rejection event.
type RejectEvent struct {
	OrderID   string    `json:"order_id"`
	Symbol    string    `json:"symbol"`
	Reason    string    `json:"reason"`
	Timestamp time.Time `json:"timestamp"`
}

// StopEvent represents a stop-loss trigger event.
type StopEvent struct {
	PositionID string          `json:"position_id"`
	Symbol     string          `json:"symbol"`
	StopPrice  decimal.Decimal `json:"stop_price"`
	ExitPrice  decimal.Decimal `json:"exit_price"`
	PnL        decimal.Decimal `json:"pnl"`
	Timestamp  time.Time       `json:"timestamp"`
}

// RiskEvent represents a risk threshold alert event.
type RiskEvent struct {
	AlertType string          `json:"alert_type"`
	Severity  string          `json:"severity"`
	Value     decimal.Decimal `json:"value"`
	Threshold decimal.Decimal `json:"threshold"`
	Message   string          `json:"message"`
	Timestamp time.Time       `json:"timestamp"`
}

// EmergencyEvent represents an emergency stop event.
type EmergencyEvent struct {
	Reason    string    `json:"reason"`
	Source    string    `json:"source"`
	Timestamp time.Time `json:"timestamp"`
}

// OddsEvent represents Polymarket odds change event.
type OddsEvent struct {
	ConditionID string          `json:"condition_id"`
	Outcome     string          `json:"outcome"`
	Odds        decimal.Decimal `json:"odds"`
	Volume      decimal.Decimal `json:"volume"`
	Timestamp   time.Time       `json:"timestamp"`
}

// ResolutionEvent represents a Polymarket event resolution.
type ResolutionEvent struct {
	ConditionID string    `json:"condition_id"`
	Outcome     string    `json:"outcome"`
	Result      string    `json:"result"`
	Timestamp   time.Time `json:"timestamp"`
}

// TradingEvent is a generic event wrapper.
type TradingEvent struct {
	Type      TradingEventType `json:"type"`
	Payload   interface{}      `json:"payload"`
	Timestamp time.Time        `json:"timestamp"`
}

// EventHandler is a function type that handles trading events.
type EventHandler func(event *TradingEvent)

// Subscription represents an event subscription.
type Subscription struct {
	ID      string
	Event   TradingEventType
	Handler EventHandler
}

// TradingEventBus manages event subscriptions and dispatching.
type TradingEventBus struct {
	mu            sync.RWMutex
	subscriptions map[TradingEventType]map[string]*Subscription
	channels      map[TradingEventType]chan *TradingEvent
	redis         *database.RedisClient
	logger        *slog.Logger
	bufferSize    int
	stopCh        chan struct{}
}

// NewTradingEventBus creates a new TradingEventBus.
func NewTradingEventBus(redisClient *database.RedisClient, bufferSize int, logger *slog.Logger) *TradingEventBus {
	return &TradingEventBus{
		subscriptions: make(map[TradingEventType]map[string]*Subscription),
		channels:      make(map[TradingEventType]chan *TradingEvent),
		redis:         redisClient,
		logger:        logger,
		bufferSize:    bufferSize,
		stopCh:        make(chan struct{}),
	}
}

// Subscribe registers a handler for a specific event type.
// Returns a subscription ID that can be used to unsubscribe.
func (b *TradingEventBus) Subscribe(eventType TradingEventType, handler EventHandler) string {
	b.mu.Lock()
	defer b.mu.Unlock()

	subID := fmt.Sprintf("%s_%d", eventType, time.Now().UnixNano())

	if _, ok := b.subscriptions[eventType]; !ok {
		b.subscriptions[eventType] = make(map[string]*Subscription)
		b.channels[eventType] = make(chan *TradingEvent, b.bufferSize)
		go b.startEventDispatcher(eventType)
	}

	sub := &Subscription{
		ID:      subID,
		Event:   eventType,
		Handler: handler,
	}
	b.subscriptions[eventType][subID] = sub

	return subID
}

// Unsubscribe removes a subscription by ID.
func (b *TradingEventBus) Unsubscribe(eventType TradingEventType, subscriptionID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subs, ok := b.subscriptions[eventType]; ok {
		delete(subs, subscriptionID)
		if len(subs) == 0 {
			close(b.channels[eventType])
			delete(b.channels, eventType)
			delete(b.subscriptions, eventType)
		}
	}
}

// Publish sends an event to all subscribers.
func (b *TradingEventBus) Publish(event *TradingEvent) {
	b.mu.RLock()
	ch, ok := b.channels[event.Type]
	b.mu.RUnlock()

	if !ok {
		return
	}

	select {
	case ch <- event:
	case <-time.After(100 * time.Millisecond):
		b.logger.Warn("event channel full, dropping event", "type", event.Type)
	}

	// Also publish to Redis if available
	if b.redis != nil {
		b.publishToRedis(event)
	}
}

// SubscribeToChannel returns a read-only channel for a specific event type.
func (b *TradingEventBus) SubscribeToChannel(eventType TradingEventType) (<-chan *TradingEvent, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, ok := b.subscriptions[eventType]; !ok {
		b.subscriptions[eventType] = make(map[string]*Subscription)
		b.channels[eventType] = make(chan *TradingEvent, b.bufferSize)
	}

	return b.channels[eventType], nil
}

// startEventDispatcher runs a goroutine to dispatch events to handlers.
func (b *TradingEventBus) startEventDispatcher(eventType TradingEventType) {
	ch := b.channels[eventType]
	for {
		select {
		case event, ok := <-ch:
			if !ok {
				return
			}
			b.mu.RLock()
			subs := make([]*Subscription, 0)
			for _, sub := range b.subscriptions[eventType] {
				subs = append(subs, sub)
			}
			b.mu.RUnlock()

			for _, sub := range subs {
				func() {
					defer func() {
						if r := recover(); r != nil {
							b.logger.Error("handler panicked",
								"subscription", sub.ID,
								"error", r)
						}
					}()
					sub.Handler(event)
				}()
			}
		case <-b.stopCh:
			return
		}
	}
}

// publishToRedis sends event to Redis Pub/Sub for distributed systems.
func (b *TradingEventBus) publishToRedis(event *TradingEvent) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	data, err := json.Marshal(event)
	if err != nil {
		b.logger.Error("failed to marshal event", "error", err)
		return
	}

	channel := fmt.Sprintf("trading:events:%s", event.Type)
	b.redis.Publish(ctx, channel, data)
}

// SubscribeToRedis subscribes to Redis Pub/Sub events.
func (b *TradingEventBus) SubscribeToRedis(ctx context.Context, eventType TradingEventType) error {
	if b.redis == nil {
		return nil
	}

	channel := fmt.Sprintf("trading:events:%s", eventType)
	pubsub, err := b.redis.Subscribe(ctx, channel)
	if err != nil {
		return fmt.Errorf("failed to subscribe to redis channel %s: %w", channel, err)
	}

	go func() {
		ch := pubsub.Channel()
		for msg := range ch {
			var event TradingEvent
			if err := json.Unmarshal([]byte(msg.Payload), &event); err != nil {
				b.logger.Error("failed to unmarshal redis event", "error", err)
				continue
			}

			b.mu.RLock()
			ch, ok := b.channels[event.Type]
			b.mu.RUnlock()

			if ok {
				select {
				case ch <- &event:
				case <-time.After(100 * time.Millisecond):
					b.logger.Warn("redis event channel full", "type", event.Type)
				}
			}
		}
	}()

	return nil
}

// Close shuts down the event bus.
func (b *TradingEventBus) Close() error {
	close(b.stopCh)

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, ch := range b.channels {
		close(ch)
	}

	b.subscriptions = make(map[TradingEventType]map[string]*Subscription)
	b.channels = make(map[TradingEventType]chan *TradingEvent)

	return nil
}

// GetSubscriptionCount returns the number of active subscriptions for an event type.
func (b *TradingEventBus) GetSubscriptionCount(eventType TradingEventType) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if subs, ok := b.subscriptions[eventType]; ok {
		return len(subs)
	}
	return 0
}
