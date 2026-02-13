package pubsub

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type Publisher struct {
	client    *redis.Client
	logger    *zap.Logger
	published atomic.Int64
	errors    atomic.Int64
}

func NewPublisher(client *redis.Client, logger *zap.Logger) *Publisher {
	return &Publisher{
		client: client,
		logger: logger,
	}
}

func (p *Publisher) Publish(ctx context.Context, channel string, envelope Envelope) error {
	if channel == "" {
		return fmt.Errorf("pubsub: channel cannot be empty")
	}

	envelope.Channel = channel
	if envelope.Timestamp.IsZero() {
		envelope.Timestamp = time.Now().UTC()
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		p.errors.Add(1)
		return fmt.Errorf("pubsub: marshal envelope: %w", err)
	}

	if err := p.client.Publish(ctx, channel, data).Err(); err != nil {
		p.errors.Add(1)
		p.logger.Error("pubsub: publish failed",
			zap.String("channel", channel),
			zap.Error(err),
		)
		return fmt.Errorf("pubsub: publish to %s: %w", channel, err)
	}

	p.published.Add(1)
	return nil
}

func (p *Publisher) PublishTicker(ctx context.Context, exchange, symbol string, payload TickerPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pubsub: marshal ticker payload: %w", err)
	}
	return p.Publish(ctx, TickerChannel(exchange, symbol), Envelope{
		Type:     MessageTypeTicker,
		Exchange: exchange,
		Symbol:   symbol,
		Data:     data,
	})
}

func (p *Publisher) PublishOrderBook(ctx context.Context, exchange, symbol string, payload OrderBookPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pubsub: marshal orderbook payload: %w", err)
	}
	return p.Publish(ctx, OrderBookChannel(exchange, symbol), Envelope{
		Type:     MessageTypeOrderBook,
		Exchange: exchange,
		Symbol:   symbol,
		Data:     data,
	})
}

func (p *Publisher) PublishTrade(ctx context.Context, exchange, symbol string, payload TradePayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pubsub: marshal trade payload: %w", err)
	}
	return p.Publish(ctx, TradeChannel(exchange, symbol), Envelope{
		Type:     MessageTypeTrade,
		Exchange: exchange,
		Symbol:   symbol,
		Data:     data,
	})
}

func (p *Publisher) PublishSignal(ctx context.Context, qualifier string, payload SignalPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pubsub: marshal signal payload: %w", err)
	}
	return p.Publish(ctx, SignalChannel(qualifier), Envelope{
		Type: MessageTypeSignal,
		Data: data,
	})
}

func (p *Publisher) PublishFunding(ctx context.Context, exchange, symbol string, payload FundingPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("pubsub: marshal funding payload: %w", err)
	}
	return p.Publish(ctx, FundingChannel(exchange, symbol), Envelope{
		Type:     MessageTypeFunding,
		Exchange: exchange,
		Symbol:   symbol,
		Data:     data,
	})
}

type PublisherStats struct {
	Published int64 `json:"published"`
	Errors    int64 `json:"errors"`
}

func (p *Publisher) Stats() PublisherStats {
	return PublisherStats{
		Published: p.published.Load(),
		Errors:    p.errors.Load(),
	}
}
