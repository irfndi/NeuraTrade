package services

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTradingStateStore_PositionOperations(t *testing.T) {
	store := NewTradingStateStore(nil)

	ctx := context.Background()

	t.Run("AddPosition", func(t *testing.T) {
		pos := &PositionRecord{
			PositionID:   "pos_001",
			Symbol:       "BTC/USDT",
			Exchange:     "binance",
			Side:         "BUY",
			Size:         decimal.NewFromFloat(0.5),
			EntryPrice:   decimal.NewFromFloat(50000),
			CurrentPrice: decimal.NewFromFloat(51000),
			Status:       "OPEN",
			OpenedAt:     time.Now(),
			UpdatedAt:    time.Now(),
		}

		err := store.AddPosition(ctx, pos)
		require.NoError(t, err)

		retrieved, err := store.GetPosition(ctx, "pos_001")
		require.NoError(t, err)
		assert.Equal(t, "BTC/USDT", retrieved.Symbol)
		assert.Equal(t, "binance", retrieved.Exchange)
	})

	t.Run("AddDuplicatePosition", func(t *testing.T) {
		pos := &PositionRecord{
			PositionID: "pos_001",
			Symbol:     "ETH/USDT",
		}

		err := store.AddPosition(ctx, pos)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})

	t.Run("UpdatePosition", func(t *testing.T) {
		pos := &PositionRecord{
			PositionID:   "pos_001",
			Symbol:       "BTC/USDT",
			CurrentPrice: decimal.NewFromFloat(52000),
		}

		err := store.UpdatePosition(ctx, pos)
		require.NoError(t, err)

		retrieved, err := store.GetPosition(ctx, "pos_001")
		require.NoError(t, err)
		assert.Equal(t, "52000", retrieved.CurrentPrice.String())
	})

	t.Run("RemovePosition", func(t *testing.T) {
		err := store.RemovePosition(ctx, "pos_001")
		require.NoError(t, err)

		_, err = store.GetPosition(ctx, "pos_001")
		assert.Error(t, err)
	})

	t.Run("GetNonExistentPosition", func(t *testing.T) {
		_, err := store.GetPosition(ctx, "nonexistent")
		assert.Error(t, err)
	})
}

func TestTradingStateStore_OrderOperations(t *testing.T) {
	store := NewTradingStateStore(nil)
	ctx := context.Background()

	t.Run("AddOrder", func(t *testing.T) {
		order := &OrderRecord{
			OrderID:   "order_001",
			Symbol:    "BTC/USDT",
			Exchange:  "binance",
			Side:      "BUY",
			Type:      "LIMIT",
			Size:      decimal.NewFromFloat(0.1),
			Price:     decimal.NewFromFloat(49000),
			Status:    "PENDING",
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		err := store.AddOrder(ctx, order)
		require.NoError(t, err)

		retrieved, err := store.GetOrder(ctx, "order_001")
		require.NoError(t, err)
		assert.Equal(t, "LIMIT", retrieved.Type)
		assert.Equal(t, "PENDING", retrieved.Status)
	})

	t.Run("UpdateOrder", func(t *testing.T) {
		order := &OrderRecord{
			OrderID: "order_001",
			Status:  "FILLED",
		}

		err := store.UpdateOrder(ctx, order)
		require.NoError(t, err)

		retrieved, err := store.GetOrder(ctx, "order_001")
		require.NoError(t, err)
		assert.Equal(t, "FILLED", retrieved.Status)
	})

	t.Run("RemoveOrder", func(t *testing.T) {
		err := store.RemoveOrder(ctx, "order_001")
		require.NoError(t, err)

		_, err = store.GetOrder(ctx, "order_001")
		assert.Error(t, err)
	})
}

func TestTradingStateStore_ListPendingOrders(t *testing.T) {
	store := NewTradingStateStore(nil)
	ctx := context.Background()

	store.AddOrder(ctx, &OrderRecord{OrderID: "order_1", Status: "PENDING"})
	store.AddOrder(ctx, &OrderRecord{OrderID: "order_2", Status: "PENDING"})
	store.AddOrder(ctx, &OrderRecord{OrderID: "order_3", Status: "FILLED"})

	orders, err := store.ListPendingOrders(ctx)
	require.NoError(t, err)
	assert.Len(t, orders, 2)
}

func TestTradingStateStore_ListOpenPositions(t *testing.T) {
	store := NewTradingStateStore(nil)
	ctx := context.Background()

	store.AddPosition(ctx, &PositionRecord{PositionID: "pos_1", Symbol: "BTC/USDT"})
	store.AddPosition(ctx, &PositionRecord{PositionID: "pos_2", Symbol: "ETH/USDT"})
	store.AddPosition(ctx, &PositionRecord{PositionID: "pos_3", Symbol: "SOL/USDT"})

	positions, err := store.ListOpenPositions(ctx)
	require.NoError(t, err)
	assert.Len(t, positions, 3)
}

func TestTradingStateStore_DailyPnL(t *testing.T) {
	store := NewTradingStateStore(nil)
	ctx := context.Background()

	pnl := decimal.NewFromFloat(1500.50)
	err := store.SetDailyPnL(ctx, pnl)
	require.NoError(t, err)

	retrieved := store.GetDailyPnL(ctx)
	assert.Equal(t, pnl, retrieved)
}

func TestTradingStateStore_ConsecutiveLosses(t *testing.T) {
	store := NewTradingStateStore(nil)
	ctx := context.Background()

	count1 := store.IncrementConsecutiveLosses(ctx)
	assert.Equal(t, 1, count1)

	count2 := store.IncrementConsecutiveLosses(ctx)
	assert.Equal(t, 2, count2)

	store.ResetConsecutiveLosses(ctx)
	count3 := store.IncrementConsecutiveLosses(ctx)
	assert.Equal(t, 1, count3)
}

func TestTradingStateStore_EmergencyMode(t *testing.T) {
	store := NewTradingStateStore(nil)
	ctx := context.Background()

	enabled := store.SetEmergencyMode(ctx, true)
	assert.True(t, enabled)

	emergency := store.GetEmergencyMode(ctx)
	assert.True(t, emergency)

	disabled := store.SetEmergencyMode(ctx, false)
	assert.False(t, disabled)

	emergency = store.GetEmergencyMode(ctx)
	assert.False(t, emergency)
}

func TestTradingStateStore_Validate(t *testing.T) {
	store := NewTradingStateStore(nil)
	ctx := context.Background()

	store.AddPosition(ctx, &PositionRecord{PositionID: "pos_1", Symbol: "BTC/USDT"})
	store.AddOrder(ctx, &OrderRecord{OrderID: "order_1", Status: "PENDING"})

	err := store.Validate(ctx)
	require.NoError(t, err)
}

func TestTradingStateStore_ValidateDuplicates(t *testing.T) {
	store := NewTradingStateStore(nil)
	ctx := context.Background()

	store.AddPosition(ctx, &PositionRecord{PositionID: "pos_1", Symbol: "BTC/USDT"})

	err := store.Validate(ctx)
	require.NoError(t, err)
}

func TestTradingStateStore_ConcurrentAccess(t *testing.T) {
	store := NewTradingStateStore(nil)
	ctx := context.Background()

	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(n int) {
			store.mu.Lock()
			store.openPositions["concurrent_pos"] = &PositionRecord{
				PositionID: "concurrent_pos",
				Symbol:     "BTC/USDT",
			}
			store.mu.Unlock()
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	pos, err := store.GetPosition(ctx, "concurrent_pos")
	require.NoError(t, err)
	assert.NotNil(t, pos)
}
