package portfolio

import (
	"context"
	"sync"
	"testing"
	"time"

	domainportfolio "github.com/irfndi/neuratrade/internal/domain/portfolio"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type mockEventBus struct {
	mu        sync.RWMutex
	handlers  map[string][]ports.EventHandler
	published []ports.Event
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{handlers: make(map[string][]ports.EventHandler)}
}

func (m *mockEventBus) Publish(ctx context.Context, event ports.Event) error {
	m.mu.Lock()
	m.published = append(m.published, event)
	handlers := append([]ports.EventHandler(nil), m.handlers[event.EventType()]...)
	allHandlers := append([]ports.EventHandler(nil), m.handlers["*"]...)
	m.mu.Unlock()

	for _, h := range handlers {
		if err := h(ctx, event); err != nil {
			return err
		}
	}
	for _, h := range allHandlers {
		if err := h(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (m *mockEventBus) Subscribe(ctx context.Context, eventType string, handler ports.EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[eventType] = append(m.handlers[eventType], handler)
	return nil
}

func (m *mockEventBus) SubscribeAll(ctx context.Context, handler ports.EventHandler) error {
	return m.Subscribe(ctx, "*", handler)
}

func (m *mockEventBus) Unsubscribe(ctx context.Context, eventType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.handlers, eventType)
	return nil
}

func (m *mockEventBus) PublishedTypes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	types := make([]string, 0, len(m.published))
	for _, e := range m.published {
		types = append(types, e.EventType())
	}
	return types
}

func TestPortfolioActor_OrderFilledEventFlow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := newMockEventBus()
	pa := NewPortfolioActor("portfolio-test", b)
	ref := actor.NewRef(pa, actor.DefaultConfig())
	pa.BindRef(ref)
	require.NoError(t, pa.SubscribeOrderFilled(ctx))

	go ref.Run(ctx)
	t.Cleanup(ref.Stop)
	time.Sleep(20 * time.Millisecond)

	err := b.Publish(ctx, NewOrderFilledEvent("trade-aggregate", domainportfolio.Fill{
		TradeID:    "t-1",
		OrderID:    "o-1",
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Side:       domainportfolio.SideBuy,
		Quantity:   decimal.NewFromInt(2),
		Price:      decimal.NewFromInt(100),
		ExecutedAt: time.Now().UTC(),
	}))
	require.NoError(t, err)

	err = b.Publish(ctx, NewOrderFilledEvent("trade-aggregate", domainportfolio.Fill{
		TradeID:    "t-2",
		OrderID:    "o-1",
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Side:       domainportfolio.SideSell,
		Quantity:   decimal.NewFromInt(1),
		Price:      decimal.NewFromInt(110),
		ExecutedAt: time.Now().UTC(),
	}))
	require.NoError(t, err)

	snapshot := askSnapshot(t, ref)
	require.Len(t, snapshot.Positions, 1)
	pos := snapshot.Positions[0]

	assert.Equal(t, "binance", pos.Exchange)
	assert.Equal(t, "BTC/USDT", pos.Symbol)
	assert.True(t, pos.Quantity.Equal(decimal.NewFromInt(1)))
	assert.True(t, pos.RealizedPnL.Equal(decimal.NewFromInt(10)))
	assert.True(t, pos.UnrealizedPnL.Equal(decimal.NewFromInt(10)))

	publishedTypes := b.PublishedTypes()
	assert.Contains(t, publishedTypes, ports.EventTypePositionOpened)
	assert.Contains(t, publishedTypes, ports.EventTypePositionUpdated)
	assert.Contains(t, publishedTypes, EventTypePnLUpdated)
}

func TestPortfolioActor_ReconcileAndQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := newMockEventBus()
	pa := NewPortfolioActor("portfolio-test", b)
	ref := actor.NewRef(pa, actor.DefaultConfig())
	pa.BindRef(ref)
	go ref.Run(ctx)
	t.Cleanup(ref.Stop)
	time.Sleep(20 * time.Millisecond)

	baseTime := time.Now().UTC()
	resp, err := ref.Ask(ctx, ReconcileMessage{Fills: []domainportfolio.Fill{
		{
			TradeID:    "3",
			Exchange:   "binance",
			Symbol:     "ETH/USDT",
			Side:       domainportfolio.SideBuy,
			Quantity:   decimal.NewFromInt(1),
			Price:      decimal.NewFromInt(220),
			ExecutedAt: baseTime.Add(2 * time.Minute),
		},
		{
			TradeID:    "1",
			Exchange:   "binance",
			Symbol:     "BTC/USDT",
			Side:       domainportfolio.SideBuy,
			Quantity:   decimal.NewFromInt(2),
			Price:      decimal.NewFromInt(100),
			ExecutedAt: baseTime,
		},
		{
			TradeID:    "2",
			Exchange:   "binance",
			Symbol:     "BTC/USDT",
			Side:       domainportfolio.SideSell,
			Quantity:   decimal.NewFromInt(1),
			Price:      decimal.NewFromInt(120),
			ExecutedAt: baseTime.Add(time.Minute),
		},
	}})
	require.NoError(t, err)
	changes, ok := resp.([]domainportfolio.Change)
	require.True(t, ok)
	require.Len(t, changes, 3)

	snapshot := askSnapshot(t, ref)
	require.Len(t, snapshot.Positions, 2)
	assert.True(t, snapshot.TotalRealizedPnL.Equal(decimal.NewFromInt(20)))

	posResp, err := ref.Ask(ctx, GetPositionQuery{Exchange: "binance", Symbol: "BTC/USDT"})
	require.NoError(t, err)
	positionResult, ok := posResp.(PositionQueryResult)
	require.True(t, ok)
	require.True(t, positionResult.Found)
	assert.True(t, positionResult.Position.Quantity.Equal(decimal.NewFromInt(1)))
}

func TestPortfolioActor_MarkPriceUpdateRecomputesPnL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b := newMockEventBus()
	pa := NewPortfolioActor("portfolio-test", b)
	ref := actor.NewRef(pa, actor.DefaultConfig())
	pa.BindRef(ref)
	go ref.Run(ctx)
	t.Cleanup(ref.Stop)
	time.Sleep(20 * time.Millisecond)

	require.NoError(t, ref.Send(ctx, ProcessOrderFilledMessage{Event: NewOrderFilledEvent("agg", domainportfolio.Fill{
		TradeID:    "x",
		Exchange:   "binance",
		Symbol:     "BTC/USDT",
		Side:       domainportfolio.SideBuy,
		Quantity:   decimal.NewFromInt(1),
		Price:      decimal.NewFromInt(100),
		ExecutedAt: time.Now().UTC(),
	})}))

	require.NoError(t, ref.Send(ctx, UpdateMarkPriceMessage{
		Exchange:  "binance",
		Symbol:    "BTC/USDT",
		MarkPrice: decimal.NewFromInt(130),
	}))

	position := askPosition(t, ref, "binance", "BTC/USDT")
	require.True(t, position.Found)
	assert.True(t, position.Position.UnrealizedPnL.Equal(decimal.NewFromInt(30)))
}

func askSnapshot(t *testing.T, ref *actor.Ref) domainportfolio.Snapshot {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := ref.Ask(ctx, GetSnapshotQuery{})
	require.NoError(t, err)
	snap, ok := resp.(domainportfolio.Snapshot)
	require.True(t, ok)
	return snap
}

func askPosition(t *testing.T, ref *actor.Ref, exchange, symbol string) PositionQueryResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	resp, err := ref.Ask(ctx, GetPositionQuery{Exchange: exchange, Symbol: symbol})
	require.NoError(t, err)
	out, ok := resp.(PositionQueryResult)
	require.True(t, ok)
	return out
}
