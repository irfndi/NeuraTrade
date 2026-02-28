// Package eventbus provides an in-process publish/subscribe event system.
// It supports:
//   - Typed events with metadata
//   - Wildcard topic subscriptions (e.g., "market.*")
//   - Multiple subscribers per topic
//   - Non-blocking publish with optional buffering
package eventbus

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Errors
var (
	ErrTopicEmpty    = errors.New("topic cannot be empty")
	ErrNotSubscribed = errors.New("not subscribed")
	ErrBusStopped    = errors.New("event bus stopped")
)

// Event represents a published event with metadata.
type Event struct {
	// Topic is the event topic/channel.
	Topic string

	// Type is the specific event type within the topic.
	Type string

	// Payload is the event data.
	Payload any

	// TraceID for distributed tracing.
	TraceID string

	// Timestamp when the event was created.
	Timestamp time.Time

	// Source identifies the component that published the event.
	Source string

	// Metadata contains additional key-value pairs.
	Metadata map[string]any
}

// NewEvent creates a new Event with defaults.
func NewEvent(topic, eventType string, payload any) Event {
	return Event{
		Topic:     topic,
		Type:      eventType,
		Payload:   payload,
		Timestamp: time.Now(),
		Metadata:  make(map[string]any),
	}
}

// WithTraceID sets the trace ID.
func (e Event) WithTraceID(traceID string) Event {
	e.TraceID = traceID
	return e
}

// WithSource sets the source.
func (e Event) WithSource(source string) Event {
	e.Source = source
	return e
}

// WithMetadata adds metadata.
func (e Event) WithMetadata(key string, value any) Event {
	if e.Metadata == nil {
		e.Metadata = make(map[string]any)
	}
	e.Metadata[key] = value
	return e
}

// Handler processes events.
type Handler func(ctx context.Context, event Event) error

// Subscription represents an active subscription.
type Subscription struct {
	id          string
	topic       string
	handler     Handler
	unsubscribe func()
}

// ID returns the subscription ID.
func (s *Subscription) ID() string {
	return s.id
}

// Topic returns the subscribed topic.
func (s *Subscription) Topic() string {
	return s.topic
}

// Unsubscribe cancels the subscription.
func (s *Subscription) Unsubscribe() {
	if s.unsubscribe != nil {
		s.unsubscribe()
	}
}

// Config holds EventBus configuration.
type Config struct {
	// BufferSize is the buffer size for subscriber channels.
	// Default: 256
	BufferSize int

	// WildcardChar is the character used for wildcards.
	// Default: "*"
	WildcardChar string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		BufferSize:   256,
		WildcardChar: "*",
	}
}

// Bus is the in-process event bus.
type Bus struct {
	config  Config
	mu      sync.RWMutex
	subs    map[string]map[string]*subscriber // topic -> subscriberID -> subscriber
	nextID  atomic.Uint64
	stopped atomic.Bool
	wg      sync.WaitGroup
}

type subscriber struct {
	id      string
	topic   string
	handler Handler
	buffer  chan Event
	ctx     context.Context
	cancel  context.CancelFunc
}

// New creates a new EventBus.
func New(config Config) *Bus {
	if config.BufferSize <= 0 {
		config.BufferSize = DefaultConfig().BufferSize
	}
	if config.WildcardChar == "" {
		config.WildcardChar = DefaultConfig().WildcardChar
	}
	return &Bus{
		config: config,
		subs:   make(map[string]map[string]*subscriber),
	}
}

// Subscribe registers a handler for a topic.
// Returns a Subscription that can be used to unsubscribe.
func (b *Bus) Subscribe(ctx context.Context, topic string, handler Handler) (*Subscription, error) {
	if topic == "" {
		return nil, ErrTopicEmpty
	}
	if b.stopped.Load() {
		return nil, ErrBusStopped
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	subID := fmt.Sprintf("sub-%d", b.nextID.Add(1))
	subCtx, cancel := context.WithCancel(ctx)

	sub := &subscriber{
		id:      subID,
		topic:   topic,
		handler: handler,
		buffer:  make(chan Event, b.config.BufferSize),
		ctx:     subCtx,
		cancel:  cancel,
	}

	if b.subs[topic] == nil {
		b.subs[topic] = make(map[string]*subscriber)
	}
	b.subs[topic][subID] = sub

	// Start processing goroutine
	b.wg.Add(1)
	go b.processSubscriber(sub)

	return &Subscription{
		id:      subID,
		topic:   topic,
		handler: handler,
		unsubscribe: func() {
			b.unsubscribe(topic, subID)
		},
	}, nil
}

func (b *Bus) unsubscribe(topic, subID string) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if topicSubs, ok := b.subs[topic]; ok {
		if sub, ok := topicSubs[subID]; ok {
			sub.cancel()
			close(sub.buffer)
			delete(topicSubs, subID)
		}
		if len(topicSubs) == 0 {
			delete(b.subs, topic)
		}
	}
}

func (b *Bus) processSubscriber(sub *subscriber) {
	defer b.wg.Done()

	for {
		select {
		case <-sub.ctx.Done():
			return
		case event, ok := <-sub.buffer:
			if !ok {
				return
			}
			// Handler errors are intentionally not propagated to avoid affecting other subscribers.
			// In a production system, these errors should be logged or emitted as metrics.
			_ = sub.handler(sub.ctx, event)
		}
	}
}

// Publish sends an event to all matching subscribers.
// This is non-blocking; events are buffered.
func (b *Bus) Publish(ctx context.Context, event Event) error {
	if b.stopped.Load() {
		return ErrBusStopped
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	// Find matching topics
	topics := b.matchingTopics(event.Topic)

	for _, topic := range topics {
		if topicSubs, ok := b.subs[topic]; ok {
			for _, sub := range topicSubs {
				select {
				case sub.buffer <- event:
					// Event sent
				default:
					// Buffer full - drop event (backpressure)
					// In production, this would emit a metric
				}
			}
		}
	}

	return nil
}

// PublishSync sends an event and waits for all handlers to process it.
func (b *Bus) PublishSync(ctx context.Context, event Event) error {
	if b.stopped.Load() {
		return ErrBusStopped
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	topics := b.matchingTopics(event.Topic)

	for _, topic := range topics {
		if topicSubs, ok := b.subs[topic]; ok {
			for _, sub := range topicSubs {
				_ = sub.handler(ctx, event) // Handler errors ignored
			}
		}

	}

	return nil
}

// matchingTopics returns all topics that match the given topic (including wildcards).
	// Caller must hold b.mu.RLock() before calling.
	func (b *Bus) matchingTopics(topic string) []string {
	// Exact match
	topics := []string{topic}

	// Wildcard matching
	for t := range b.subs {
		if b.isWildcardMatch(topic, t) {
			topics = append(topics, t)
		}
	}

	return topics
}

func (b *Bus) isWildcardMatch(topic, pattern string) bool {
	if !strings.Contains(pattern, b.config.WildcardChar) {
		return false
	}

	parts := strings.Split(pattern, ".")
	topicParts := strings.Split(topic, ".")

	if len(parts) != len(topicParts) {
		return false
	}

	for i, p := range parts {
		if p != b.config.WildcardChar && p != topicParts[i] {
			return false
		}
	}

	return true
}

// Stop gracefully stops the event bus.
func (b *Bus) Stop() {
	b.stopped.Store(true)

	b.mu.Lock()
	defer b.mu.Unlock()

	for _, topicSubs := range b.subs {
		for _, sub := range topicSubs {
			sub.cancel()
			close(sub.buffer)
		}
	}
	b.subs = make(map[string]map[string]*subscriber)
	b.wg.Wait()
}

// SubscriberCount returns the number of subscribers for a topic.
func (b *Bus) SubscriberCount(topic string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := 0
	if topicSubs, ok := b.subs[topic]; ok {
		count = len(topicSubs)
	}
	return count
}

// TotalSubscribers returns the total number of subscribers.
func (b *Bus) TotalSubscribers() int {
	b.mu.RLock()
	defer b.mu.RUnlock()

	count := 0
	for _, topicSubs := range b.subs {
		count += len(topicSubs)
	}
	return count
}
