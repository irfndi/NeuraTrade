package risk

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestPosition(entryPrice string, side string) PositionSnapshot {
	return PositionSnapshot{
		Symbol:     "BTC/USDT",
		Exchange:   "binance",
		Side:       side,
		EntryPrice: decimal.RequireFromString(entryPrice),
		Quantity:   decimal.NewFromInt(1),
		OpenedAt:   time.Now().UTC(),
	}
}

func TestMaxLossMonitor_FiresOnBuyBreach(t *testing.T) {
	pos := newTestPosition("100", "buy")
	var mu sync.Mutex
	var closed PositionSnapshot
	var closeReason string
	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 10 * time.Millisecond,
	}, func(_ context.Context, _ string, _ string) (decimal.Decimal, error) {
		return decimal.RequireFromString("98"), nil
	}, func(_ context.Context, p PositionSnapshot, reason string) error {
		mu.Lock()
		defer mu.Unlock()
		closed = p
		closeReason = reason
		return nil
	})
	monitor.TrackPosition(pos)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return closed.Symbol != ""
	}, 500*time.Millisecond, 10*time.Millisecond)

	cancel()
	<-errCh
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "BTC/USDT", closed.Symbol)
	assert.Contains(t, closeReason, "max_loss")
	assert.Equal(t, 0, monitor.TrackedCount(), "position should be untracked after close")
}

func TestMaxLossMonitor_FiresOnSellBreach(t *testing.T) {
	pos := newTestPosition("100", "sell")
	var mu sync.Mutex
	var closed PositionSnapshot
	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 10 * time.Millisecond,
	}, func(_ context.Context, _ string, _ string) (decimal.Decimal, error) {
		return decimal.RequireFromString("102"), nil
	}, func(_ context.Context, p PositionSnapshot, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		closed = p
		return nil
	})
	monitor.TrackPosition(pos)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return closed.Symbol != ""
	}, 500*time.Millisecond, 10*time.Millisecond)

	cancel()
	<-errCh
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "BTC/USDT", closed.Symbol)
}

func TestMaxLossMonitor_DoesNotFireWithinThreshold(t *testing.T) {
	pos := newTestPosition("100", "buy")
	var closed atomic.Bool
	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 10 * time.Millisecond,
	}, func(_ context.Context, _ string, _ string) (decimal.Decimal, error) {
		return decimal.RequireFromString("99"), nil
	}, func(_ context.Context, _ PositionSnapshot, _ string) error {
		closed.Store(true)
		return nil
	})
	monitor.TrackPosition(pos)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-errCh

	assert.False(t, closed.Load(), "closer should not be called when loss is within threshold")
	assert.Equal(t, 1, monitor.TrackedCount(), "position should remain tracked")
}

func TestMaxLossMonitor_SkipsOnPriceError(t *testing.T) {
	pos := newTestPosition("100", "buy")
	var closed atomic.Bool
	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 10 * time.Millisecond,
	}, func(_ context.Context, _, _ string) (decimal.Decimal, error) {
		return decimal.Zero, errors.New("network failure")
	}, func(_ context.Context, _ PositionSnapshot, _ string) error {
		closed.Store(true)
		return nil
	})
	monitor.TrackPosition(pos)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-errCh

	assert.False(t, closed.Load(), "closer should not be called when price fetch fails")
}

func TestMaxLossMonitor_KeepsPositionOnCloserError(t *testing.T) {
	pos := newTestPosition("100", "buy")
	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 10 * time.Millisecond,
	}, func(_ context.Context, _ string, _ string) (decimal.Decimal, error) {
		return decimal.RequireFromString("98"), nil
	}, func(_ context.Context, _ PositionSnapshot, _ string) error {
		return errors.New("exchange rejected order")
	})
	monitor.TrackPosition(pos)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	time.Sleep(100 * time.Millisecond)
	cancel()
	<-errCh

	assert.Equal(t, 1, monitor.TrackedCount(), "position should remain tracked when closer fails")
}

func TestMaxLossMonitor_UntrackPosition(t *testing.T) {
	pos := newTestPosition("100", "buy")
	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 10 * time.Millisecond,
	}, func(_ context.Context, _, _ string) (decimal.Decimal, error) {
		return decimal.Zero, nil
	}, func(_ context.Context, _ PositionSnapshot, _ string) error {
		return nil
	})
	monitor.TrackPosition(pos)
	assert.Equal(t, 1, monitor.TrackedCount())

	monitor.UntrackPosition("binance", "BTC/USDT")
	assert.Equal(t, 0, monitor.TrackedCount())
}

func TestMaxLossMonitor_DefaultConfig(t *testing.T) {
	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{}, nil, nil)
	assert.InDelta(t, MaxLossPctDefault, monitor.config.MaxLossPct, 0.0001)
	assert.Equal(t, 1*time.Second, monitor.config.PollInterval)
	assert.Equal(t, 10*time.Second, monitor.config.MaxStalePrice)
}

func TestMaxLossMonitor_MultiplePositionsConcurrent(t *testing.T) {
	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{
		MaxLossPct:   0.015,
		PollInterval: 10 * time.Millisecond,
	}, func(_ context.Context, exchange, symbol string) (decimal.Decimal, error) {
		if symbol == "BREACH/USDT" {
			return decimal.RequireFromString("98"), nil
		}
		return decimal.RequireFromString("100"), nil
	}, func(_ context.Context, _ PositionSnapshot, _ string) error {
		return nil
	})

	monitor.TrackPosition(newTestPosition("100", "buy"))
	pos2 := newTestPosition("100", "buy")
	pos2.Symbol = "ETH/USDT"
	monitor.TrackPosition(pos2)
	pos3 := newTestPosition("100", "buy")
	pos3.Symbol = "BREACH/USDT"
	monitor.TrackPosition(pos3)

	var mu sync.Mutex
	closed := []string{}
	closer := func(_ context.Context, p PositionSnapshot, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		closed = append(closed, p.Symbol)
		return nil
	}
	monitor.closer = closer

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- monitor.Run(ctx) }()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		for _, s := range closed {
			if s == "BREACH/USDT" {
				return true
			}
		}
		return false
	}, 500*time.Millisecond, 10*time.Millisecond)

	cancel()
	<-errCh
}

func TestMaxLossMonitor_ComputeMaxLossPrice(t *testing.T) {
	monitor := NewMaxLossMonitor(MaxLossMonitorConfig{MaxLossPct: 0.015}, nil, nil)

	buyPos := newTestPosition("100", "buy")
	buyMaxLoss := monitor.computeMaxLossPrice(buyPos)
	expected := decimal.RequireFromString("98.5")
	assert.True(t, buyMaxLoss.Equal(expected), "buy max loss = 100 * 0.985 = 98.5, got %s", buyMaxLoss.String())

	sellPos := newTestPosition("100", "sell")
	sellMaxLoss := monitor.computeMaxLossPrice(sellPos)
	expectedSell := decimal.RequireFromString("101.5")
	assert.True(t, sellMaxLoss.Equal(expectedSell), "sell max loss = 100 * 1.015 = 101.5, got %s", sellMaxLoss.String())
}
