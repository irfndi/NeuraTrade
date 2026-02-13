package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type MessageHandler func(ctx context.Context, envelope Envelope) error

type Subscriber struct {
	client        *redis.Client
	logger        *zap.Logger
	handlers      map[string]MessageHandler
	mu            sync.RWMutex
	subscriptions []*activeSubscription
	received      atomic.Int64
	errors        atomic.Int64
}

type activeSubscription struct {
	pubsub *redis.PubSub
	cancel context.CancelFunc
	done   chan struct{}
}

func NewSubscriber(client *redis.Client, logger *zap.Logger) *Subscriber {
	return &Subscriber{
		client:   client,
		logger:   logger,
		handlers: make(map[string]MessageHandler),
	}
}

func (s *Subscriber) Handle(channel string, handler MessageHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[channel] = handler
}

func (s *Subscriber) HandleFunc(msgType MessageType, handler MessageHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[string(msgType)] = handler
}

func (s *Subscriber) Subscribe(ctx context.Context, channels ...string) error {
	if len(channels) == 0 {
		return fmt.Errorf("pubsub: at least one channel required")
	}

	pubsub := s.client.Subscribe(ctx, channels...)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return fmt.Errorf("pubsub: subscribe to %v: %w", channels, err)
	}

	subCtx, cancel := context.WithCancel(ctx)
	sub := &activeSubscription{
		pubsub: pubsub,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	s.mu.Lock()
	s.subscriptions = append(s.subscriptions, sub)
	s.mu.Unlock()

	go s.listen(subCtx, sub)
	return nil
}

func (s *Subscriber) PSubscribe(ctx context.Context, patterns ...string) error {
	if len(patterns) == 0 {
		return fmt.Errorf("pubsub: at least one pattern required")
	}

	pubsub := s.client.PSubscribe(ctx, patterns...)
	if _, err := pubsub.Receive(ctx); err != nil {
		_ = pubsub.Close()
		return fmt.Errorf("pubsub: psubscribe to %v: %w", patterns, err)
	}

	subCtx, cancel := context.WithCancel(ctx)
	sub := &activeSubscription{
		pubsub: pubsub,
		cancel: cancel,
		done:   make(chan struct{}),
	}

	s.mu.Lock()
	s.subscriptions = append(s.subscriptions, sub)
	s.mu.Unlock()

	go s.listen(subCtx, sub)
	return nil
}

func (s *Subscriber) listen(ctx context.Context, sub *activeSubscription) {
	defer close(sub.done)
	ch := sub.pubsub.Channel()

	for {
		select {
		case <-ctx.Done():
			_ = sub.pubsub.Close()
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			s.dispatch(ctx, msg)
		}
	}
}

func (s *Subscriber) dispatch(ctx context.Context, msg *redis.Message) {
	var envelope Envelope
	if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
		s.errors.Add(1)
		s.logger.Warn("pubsub: unmarshal message failed",
			zap.String("channel", msg.Channel),
			zap.Error(err),
		)
		return
	}

	s.received.Add(1)

	s.mu.RLock()
	handler, ok := s.handlers[msg.Channel]
	if !ok {
		handler, ok = s.handlers[string(envelope.Type)]
	}
	s.mu.RUnlock()

	if !ok {
		return
	}

	handlerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := handler(handlerCtx, envelope); err != nil {
		s.errors.Add(1)
		s.logger.Error("pubsub: handler error",
			zap.String("channel", msg.Channel),
			zap.String("type", string(envelope.Type)),
			zap.Error(err),
		)
	}
}

func (s *Subscriber) Close() error {
	s.mu.Lock()
	subs := make([]*activeSubscription, len(s.subscriptions))
	copy(subs, s.subscriptions)
	s.subscriptions = nil
	s.mu.Unlock()

	for _, sub := range subs {
		sub.cancel()
		<-sub.done
	}
	return nil
}

type SubscriberStats struct {
	Received      int64 `json:"received"`
	Errors        int64 `json:"errors"`
	Subscriptions int   `json:"subscriptions"`
}

func (s *Subscriber) Stats() SubscriberStats {
	s.mu.RLock()
	subCount := len(s.subscriptions)
	s.mu.RUnlock()
	return SubscriberStats{
		Received:      s.received.Load(),
		Errors:        s.errors.Load(),
		Subscriptions: subCount,
	}
}
