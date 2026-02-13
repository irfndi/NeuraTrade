package interfaces

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestBlacklistCacheEntry(t *testing.T) {
	// Test BlacklistCacheEntry struct
	entry := BlacklistCacheEntry{
		Symbol:    "TEST/USDT",
		Reason:    "Test blacklist",
		CreatedAt: time.Now(),
	}

	assert.Equal(t, "TEST/USDT", entry.Symbol)
	assert.Equal(t, "Test blacklist", entry.Reason)
	assert.False(t, entry.CreatedAt.IsZero())
}

func TestExchangeBlacklistEntry(t *testing.T) {
	// Test ExchangeBlacklistEntry struct
	entry := ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "test_exchange",
		Reason:       "Test reason",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	assert.Equal(t, int64(1), entry.ID)
	assert.Equal(t, "test_exchange", entry.ExchangeName)
	assert.Equal(t, "Test reason", entry.Reason)
}

func TestSymbolCacheEntry(t *testing.T) {
	// Test SymbolCacheEntry struct
	entry := SymbolCacheEntry{
		Symbols:   []string{"BTC/USDT", "ETH/USDT"},
		CachedAt:  time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}

	assert.Len(t, entry.Symbols, 2)
	assert.Equal(t, "BTC/USDT", entry.Symbols[0])
	assert.Equal(t, "ETH/USDT", entry.Symbols[1])
	assert.False(t, entry.CachedAt.IsZero())
	assert.False(t, entry.ExpiresAt.IsZero())
}

func TestBlacklistCacheStats(t *testing.T) {
	// Test BlacklistCacheStats struct
	stats := BlacklistCacheStats{
		TotalEntries:   10,
		ExpiredEntries: 2,
		Hits:           100,
		Misses:         20,
		Adds:           15,
	}

	assert.Equal(t, int64(10), stats.TotalEntries)
	assert.Equal(t, int64(2), stats.ExpiredEntries)
	assert.Equal(t, int64(100), stats.Hits)
	assert.Equal(t, int64(20), stats.Misses)
	assert.Equal(t, int64(15), stats.Adds)
}

func TestSymbolCacheStats(t *testing.T) {
	// Test SymbolCacheStats struct
	stats := SymbolCacheStats{
		Hits:   50,
		Misses: 10,
		Sets:   25,
	}

	assert.Equal(t, int64(50), stats.Hits)
	assert.Equal(t, int64(10), stats.Misses)
	assert.Equal(t, int64(25), stats.Sets)
}

func TestPosition(t *testing.T) {
	now := time.Now()
	pos := Position{
		PositionID:   "pos-123",
		OrderID:      "order-456",
		Exchange:     "binance",
		Symbol:       "BTC/USDT",
		Side:         "BUY",
		Size:         decimal.NewFromFloat(0.5),
		EntryPrice:   decimal.NewFromFloat(50000),
		CurrentPrice: decimal.NewFromFloat(51000),
		UnrealizedPL: decimal.NewFromFloat(500),
		Status:       PositionStatusOpen,
		OpenedAt:     now,
		UpdatedAt:    now,
	}

	assert.Equal(t, "pos-123", pos.GetPositionID())
	assert.Equal(t, "order-456", pos.GetOrderID())
	assert.Equal(t, "binance", pos.GetExchange())
	assert.Equal(t, "BTC/USDT", pos.GetSymbol())
	assert.Equal(t, "BUY", pos.GetSide())
	assert.True(t, pos.GetSize().Equal(decimal.NewFromFloat(0.5)))
	assert.True(t, pos.GetEntryPrice().Equal(decimal.NewFromFloat(50000)))
	assert.True(t, pos.GetCurrentPrice().Equal(decimal.NewFromFloat(51000)))
	assert.True(t, pos.GetUnrealizedPL().Equal(decimal.NewFromFloat(500)))
	assert.Equal(t, PositionStatusOpen, pos.GetStatus())
	assert.False(t, pos.GetOpenedAt().IsZero())
	assert.False(t, pos.GetUpdatedAt().IsZero())
}

func TestBalance(t *testing.T) {
	bal := Balance{
		Asset:    "USDC",
		Free:     decimal.NewFromFloat(1000),
		Locked:   decimal.NewFromFloat(500),
		Total:    decimal.NewFromFloat(1500),
		USDValue: decimal.NewFromFloat(1500),
	}

	assert.Equal(t, "USDC", bal.GetAsset())
	assert.True(t, bal.GetFree().Equal(decimal.NewFromFloat(1000)))
	assert.True(t, bal.GetLocked().Equal(decimal.NewFromFloat(500)))
	assert.True(t, bal.GetTotal().Equal(decimal.NewFromFloat(1500)))
	assert.True(t, bal.GetUSDValue().Equal(decimal.NewFromFloat(1500)))
}

func TestPortfolio(t *testing.T) {
	now := time.Now()
	positions := []Position{
		{PositionID: "pos-1", Symbol: "BTC/USDT", Size: decimal.NewFromFloat(0.5), Status: PositionStatusOpen},
		{PositionID: "pos-2", Symbol: "ETH/USDT", Size: decimal.NewFromFloat(2), Status: PositionStatusOpen},
	}
	balances := []Balance{
		{Asset: "USDC", Total: decimal.NewFromFloat(10000)},
		{Asset: "BTC", Total: decimal.NewFromFloat(0.5)},
	}

	pf := Portfolio{
		TotalValue: decimal.NewFromFloat(50000),
		Positions:  positions,
		Balances:   balances,
		UpdatedAt:  now,
	}

	assert.True(t, pf.GetTotalValue().Equal(decimal.NewFromFloat(50000)))
	assert.Len(t, pf.GetPositions(), 2)
	assert.Len(t, pf.GetBalances(), 2)
	assert.False(t, pf.GetUpdatedAt().IsZero())
}
