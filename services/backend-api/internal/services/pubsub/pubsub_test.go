package pubsub

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTestRedis(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	return client, s
}

func TestPublisher_Publish(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()
	pub := NewPublisher(client, zap.NewNop())

	ctx := context.Background()
	sub := client.Subscribe(ctx, "market:ticker:binance:BTC/USDT")
	defer sub.Close()
	_, err := sub.Receive(ctx)
	require.NoError(t, err)

	env := Envelope{
		Type:     MessageTypeTicker,
		Exchange: "binance",
		Symbol:   "BTC/USDT",
		Data:     []byte(`{"bid":"50000"}`),
	}
	err = pub.Publish(ctx, "market:ticker:binance:BTC/USDT", env)
	require.NoError(t, err)

	msg, err := sub.ReceiveMessage(ctx)
	require.NoError(t, err)

	var received Envelope
	err = json.Unmarshal([]byte(msg.Payload), &received)
	require.NoError(t, err)

	assert.Equal(t, MessageTypeTicker, received.Type)
	assert.Equal(t, "binance", received.Exchange)
	assert.Equal(t, "BTC/USDT", received.Symbol)
	assert.Equal(t, "market:ticker:binance:BTC/USDT", received.Channel)
	assert.False(t, received.Timestamp.IsZero())
}

func TestPublisher_PublishEmptyChannel(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()
	pub := NewPublisher(client, zap.NewNop())

	err := pub.Publish(context.Background(), "", Envelope{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "channel cannot be empty")
}

func TestPublisher_PublishTicker(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()
	pub := NewPublisher(client, zap.NewNop())

	ctx := context.Background()
	sub := client.Subscribe(ctx, TickerChannel("binance", "ETH/USDT"))
	defer sub.Close()
	_, err := sub.Receive(ctx)
	require.NoError(t, err)

	payload := TickerPayload{Bid: "3000", Ask: "3001", Last: "3000.5", Volume: "1000"}
	err = pub.PublishTicker(ctx, "binance", "ETH/USDT", payload)
	require.NoError(t, err)

	msg, err := sub.ReceiveMessage(ctx)
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal([]byte(msg.Payload), &env))
	assert.Equal(t, MessageTypeTicker, env.Type)

	var received TickerPayload
	require.NoError(t, json.Unmarshal(env.Data, &received))
	assert.Equal(t, "3000", received.Bid)
	assert.Equal(t, "3001", received.Ask)
}

func TestPublisher_PublishOrderBook(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()
	pub := NewPublisher(client, zap.NewNop())

	ctx := context.Background()
	channel := OrderBookChannel("kraken", "BTC/USD")
	sub := client.Subscribe(ctx, channel)
	defer sub.Close()
	_, err := sub.Receive(ctx)
	require.NoError(t, err)

	payload := OrderBookPayload{
		Bids:      []PriceLevel{{Price: "50000", Amount: "1.5"}},
		Asks:      []PriceLevel{{Price: "50001", Amount: "2.0"}},
		Timestamp: time.Now().UTC(),
	}
	err = pub.PublishOrderBook(ctx, "kraken", "BTC/USD", payload)
	require.NoError(t, err)

	msg, err := sub.ReceiveMessage(ctx)
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal([]byte(msg.Payload), &env))
	assert.Equal(t, MessageTypeOrderBook, env.Type)
	assert.Equal(t, "kraken", env.Exchange)
}

func TestPublisher_PublishTrade(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()
	pub := NewPublisher(client, zap.NewNop())

	ctx := context.Background()
	channel := TradeChannel("coinbase", "SOL/USD")
	sub := client.Subscribe(ctx, channel)
	defer sub.Close()
	_, err := sub.Receive(ctx)
	require.NoError(t, err)

	payload := TradePayload{Price: "150", Amount: "10", Side: "buy", Timestamp: time.Now().Unix()}
	err = pub.PublishTrade(ctx, "coinbase", "SOL/USD", payload)
	require.NoError(t, err)

	msg, err := sub.ReceiveMessage(ctx)
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal([]byte(msg.Payload), &env))
	assert.Equal(t, MessageTypeTrade, env.Type)
}

func TestPublisher_PublishSignal(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()
	pub := NewPublisher(client, zap.NewNop())

	ctx := context.Background()
	channel := SignalChannel(QualifierAggregated)
	sub := client.Subscribe(ctx, channel)
	defer sub.Close()
	_, err := sub.Receive(ctx)
	require.NoError(t, err)

	payload := SignalPayload{
		SignalType: "momentum",
		Direction:  "long",
		Strength:   0.85,
		Confidence: 0.92,
		Source:     "analyst_agent",
	}
	err = pub.PublishSignal(ctx, QualifierAggregated, payload)
	require.NoError(t, err)

	msg, err := sub.ReceiveMessage(ctx)
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal([]byte(msg.Payload), &env))
	assert.Equal(t, MessageTypeSignal, env.Type)
}

func TestPublisher_PublishFunding(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()
	pub := NewPublisher(client, zap.NewNop())

	ctx := context.Background()
	channel := FundingChannel("binance", "BTC/USDT")
	sub := client.Subscribe(ctx, channel)
	defer sub.Close()
	_, err := sub.Receive(ctx)
	require.NoError(t, err)

	payload := FundingPayload{Rate: "0.0001", NextFundingAt: time.Now().Add(8 * time.Hour).Unix(), IntervalHours: 8}
	err = pub.PublishFunding(ctx, "binance", "BTC/USDT", payload)
	require.NoError(t, err)

	msg, err := sub.ReceiveMessage(ctx)
	require.NoError(t, err)

	var env Envelope
	require.NoError(t, json.Unmarshal([]byte(msg.Payload), &env))
	assert.Equal(t, MessageTypeFunding, env.Type)
}

func TestPublisher_Stats(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()
	pub := NewPublisher(client, zap.NewNop())

	stats := pub.Stats()
	assert.Equal(t, int64(0), stats.Published)
	assert.Equal(t, int64(0), stats.Errors)

	ctx := context.Background()
	_ = pub.Publish(ctx, "test", Envelope{Type: MessageTypeTicker, Data: []byte(`{}`)})
	_ = pub.Publish(ctx, "test2", Envelope{Type: MessageTypeTicker, Data: []byte(`{}`)})

	stats = pub.Stats()
	assert.Equal(t, int64(2), stats.Published)
}

func TestSubscriber_HandleByChannel(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	pub := NewPublisher(client, zap.NewNop())
	sub := NewSubscriber(client, zap.NewNop())
	defer sub.Close()

	var received Envelope
	var wg sync.WaitGroup
	wg.Add(1)

	channel := TickerChannel("binance", "BTC/USDT")
	sub.Handle(channel, func(_ context.Context, env Envelope) error {
		received = env
		wg.Done()
		return nil
	})

	ctx := context.Background()
	err := sub.Subscribe(ctx, channel)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	err = pub.PublishTicker(ctx, "binance", "BTC/USDT", TickerPayload{Bid: "50000", Ask: "50001"})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for message")
	}

	assert.Equal(t, MessageTypeTicker, received.Type)
	assert.Equal(t, "binance", received.Exchange)
}

func TestSubscriber_HandleFuncByType(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	pub := NewPublisher(client, zap.NewNop())
	sub := NewSubscriber(client, zap.NewNop())
	defer sub.Close()

	var received Envelope
	var wg sync.WaitGroup
	wg.Add(1)

	sub.HandleFunc(MessageTypeSignal, func(_ context.Context, env Envelope) error {
		received = env
		wg.Done()
		return nil
	})

	channel := SignalChannel(QualifierTechnical)
	ctx := context.Background()
	err := sub.Subscribe(ctx, channel)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)

	err = pub.PublishSignal(ctx, QualifierTechnical, SignalPayload{
		SignalType: "rsi", Direction: "long", Strength: 0.7, Confidence: 0.8, Source: "ta",
	})
	require.NoError(t, err)

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for message")
	}

	assert.Equal(t, MessageTypeSignal, received.Type)
}

func TestSubscriber_Close(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	sub := NewSubscriber(client, zap.NewNop())

	ctx := context.Background()
	err := sub.Subscribe(ctx, "test:channel")
	require.NoError(t, err)

	stats := sub.Stats()
	assert.Equal(t, 1, stats.Subscriptions)

	err = sub.Close()
	require.NoError(t, err)

	stats = sub.Stats()
	assert.Equal(t, 0, stats.Subscriptions)
}

func TestSubscriber_SubscribeEmptyChannels(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	sub := NewSubscriber(client, zap.NewNop())
	defer sub.Close()

	err := sub.Subscribe(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one channel required")
}

func TestSubscriber_PSubscribeEmptyPatterns(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	sub := NewSubscriber(client, zap.NewNop())
	defer sub.Close()

	err := sub.PSubscribe(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one pattern required")
}

func TestSubscriber_Stats(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	sub := NewSubscriber(client, zap.NewNop())

	stats := sub.Stats()
	assert.Equal(t, int64(0), stats.Received)
	assert.Equal(t, int64(0), stats.Errors)
	assert.Equal(t, 0, stats.Subscriptions)

	ctx := context.Background()
	err := sub.Subscribe(ctx, "ch1")
	require.NoError(t, err)
	err = sub.Subscribe(ctx, "ch2")
	require.NoError(t, err)

	stats = sub.Stats()
	assert.Equal(t, 2, stats.Subscriptions)

	sub.Close()
}

func TestEndToEnd_PublisherSubscriberRoundTrip(t *testing.T) {
	client, _ := setupTestRedis(t)
	defer client.Close()

	pub := NewPublisher(client, zap.NewNop())
	sub := NewSubscriber(client, zap.NewNop())
	defer sub.Close()

	results := make(chan Envelope, 5)

	sub.Handle(TickerChannel("binance", "BTC/USDT"), func(_ context.Context, env Envelope) error {
		results <- env
		return nil
	})
	sub.Handle(FundingChannel("binance", "BTC/USDT"), func(_ context.Context, env Envelope) error {
		results <- env
		return nil
	})

	ctx := context.Background()
	err := sub.Subscribe(ctx, TickerChannel("binance", "BTC/USDT"), FundingChannel("binance", "BTC/USDT"))
	require.NoError(t, err)
	time.Sleep(50 * time.Millisecond)

	err = pub.PublishTicker(ctx, "binance", "BTC/USDT", TickerPayload{Bid: "60000", Ask: "60001"})
	require.NoError(t, err)

	err = pub.PublishFunding(ctx, "binance", "BTC/USDT", FundingPayload{Rate: "0.0003", IntervalHours: 8})
	require.NoError(t, err)

	received := make(map[MessageType]Envelope)
	timeout := time.After(3 * time.Second)
	for i := 0; i < 2; i++ {
		select {
		case env := <-results:
			received[env.Type] = env
		case <-timeout:
			t.Fatalf("timed out after receiving %d of 2 messages", i)
		}
	}

	assert.Contains(t, received, MessageTypeTicker)
	assert.Contains(t, received, MessageTypeFunding)

	tickerEnv := received[MessageTypeTicker]
	var tickerData TickerPayload
	require.NoError(t, json.Unmarshal(tickerEnv.Data, &tickerData))
	assert.Equal(t, "60000", tickerData.Bid)

	pubStats := pub.Stats()
	assert.Equal(t, int64(2), pubStats.Published)

	subStats := sub.Stats()
	assert.Equal(t, int64(2), subStats.Received)
}
