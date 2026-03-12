package services

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) *sql.DB {
	dbPath := t.TempDir() + "/test_memory.db"
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return db
}

func setupRealizedPnLJournal(t *testing.T, db *sql.DB) {
	t.Helper()
	_, err := db.Exec(`CREATE TABLE realized_pnl_journal (
		id TEXT PRIMARY KEY,
		order_id TEXT NOT NULL UNIQUE,
		chat_id TEXT,
		exchange TEXT NOT NULL,
		symbol TEXT NOT NULL,
		side TEXT NOT NULL,
		filled_amount NUMERIC NOT NULL DEFAULT 0,
		entry_price NUMERIC NOT NULL DEFAULT 0,
		exit_price NUMERIC NOT NULL DEFAULT 0,
		realized_pnl NUMERIC NOT NULL DEFAULT 0,
		fees NUMERIC NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'autonomous',
		closed_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL
	)`)
	require.NoError(t, err)
}

func TestNewTradeMemory(t *testing.T) {
	db := setupTestDB(t)

	tm, err := NewTradeMemory(db)
	require.NoError(t, err)
	assert.NotNil(t, tm)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM ai_trade_memory").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestResolveTradeMemoryConfigFromEnv(t *testing.T) {
	defaults := DefaultTradeMemoryConfig()
	tests := []struct {
		name             string
		lookback         string
		sampleLimit      string
		expectedLookback int
		expectedLimit    int
	}{
		{name: "unset", expectedLookback: defaults.MemoryLookbackHoursDefault, expectedLimit: defaults.MemorySampleLimitDefault},
		{name: "valid", lookback: "168", sampleLimit: "42", expectedLookback: 168, expectedLimit: 42},
		{name: "invalid", lookback: "abc", sampleLimit: "oops", expectedLookback: defaults.MemoryLookbackHoursDefault, expectedLimit: defaults.MemorySampleLimitDefault},
		{name: "zero", lookback: "0", sampleLimit: "0", expectedLookback: defaults.MemoryLookbackHoursDefault, expectedLimit: defaults.MemorySampleLimitDefault},
		{name: "negative", lookback: "-5", sampleLimit: "-10", expectedLookback: defaults.MemoryLookbackHoursDefault, expectedLimit: defaults.MemorySampleLimitDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NEURATRADE_AI_MEMORY_LOOKBACK_HOURS", tt.lookback)
			t.Setenv("NEURATRADE_AI_MEMORY_SAMPLE_LIMIT", tt.sampleLimit)

			cfg := ResolveTradeMemoryConfigFromEnv(DefaultTradeMemoryConfig())

			assert.Equal(t, tt.expectedLookback, cfg.MemoryLookbackHoursDefault)
			assert.Equal(t, tt.expectedLimit, cfg.MemorySampleLimitDefault)
		})
	}
}

func TestTradeMemory_RecordDecision(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	record := AITradeRecord{
		ID:            "test_1",
		Timestamp:     time.Now(),
		Exchange:      "binance",
		Symbol:        "BTC/USDT",
		Action:        "buy",
		SizePercent:   2.5,
		Confidence:    0.85,
		Reasoning:     "Strong bullish momentum with RSI oversold",
		MarketContext: `{"rsi": 30, "volume": "high"}`,
		EntryPrice:    45000.0,
	}

	err = tm.RecordDecision(context.Background(), record)
	require.NoError(t, err)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM ai_trade_memory WHERE id = ?", record.ID).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestTradeMemory_UpdateOutcome(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	record := AITradeRecord{
		ID:         "test_2",
		Timestamp:  time.Now(),
		Exchange:   "binance",
		Symbol:     "ETH/USDT",
		Action:     "buy",
		EntryPrice: 3000.0,
	}
	err = tm.RecordDecision(context.Background(), record)
	require.NoError(t, err)

	pnl := decimal.NewFromFloat(150.0)
	err = tm.UpdateOutcome(context.Background(), "test_2", "win", 3150.0, pnl)
	require.NoError(t, err)

	var outcome string
	var exitPrice float64
	err = db.QueryRow("SELECT outcome, exit_price FROM ai_trade_memory WHERE id = ?", "test_2").Scan(&outcome, &exitPrice)
	require.NoError(t, err)
	assert.Equal(t, "win", outcome)
	assert.Equal(t, 3150.0, exitPrice)
}

func TestTradeMemory_GetRecentTrades(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		record := AITradeRecord{
			ID:         string(rune('a' + i)),
			Timestamp:  time.Now().Add(-time.Duration(i) * time.Hour),
			Exchange:   "binance",
			Symbol:     "BTC/USDT",
			Action:     "buy",
			EntryPrice: 45000.0 + float64(i*100),
		}
		err = tm.RecordDecision(context.Background(), record)
		require.NoError(t, err)
	}

	trades, err := tm.GetRecentTrades(context.Background(), 3)
	require.NoError(t, err)
	assert.Len(t, trades, 3)
	assert.Equal(t, "a", trades[0].ID)
}

func TestTradeMemory_FindSimilarPatterns(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	records := []AITradeRecord{
		{
			ID: "sim_1", Timestamp: time.Now(), Exchange: "binance", Symbol: "BTC/USDT",
			Action: "buy", Reasoning: "RSI oversold, bullish momentum", MarketContext: "rsi:30,volume:high",
			EntryPrice: 45000, Outcome: "win", PnL: decimal.NewFromFloat(100),
		},
		{
			ID: "sim_2", Timestamp: time.Now(), Exchange: "binance", Symbol: "BTC/USDT",
			Action: "buy", Reasoning: "RSI overbought, bearish", MarketContext: "rsi:70,volume:low",
			EntryPrice: 46000, Outcome: "loss", PnL: decimal.NewFromFloat(-50),
		},
		{
			ID: "sim_3", Timestamp: time.Now(), Exchange: "binance", Symbol: "ETH/USDT",
			Action: "sell", Reasoning: "Support breakout", MarketContext: "price:below_support",
			EntryPrice: 3000, Outcome: "win", PnL: decimal.NewFromFloat(75),
		},
	}

	for _, r := range records {
		_, err = db.Exec(`INSERT INTO ai_trade_memory (id, timestamp, exchange, symbol, action, reasoning, market_context, entry_price, outcome, pnl)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, r.ID, r.Timestamp, r.Exchange, r.Symbol, r.Action, r.Reasoning, r.MarketContext, r.EntryPrice, r.Outcome, r.PnL)
		require.NoError(t, err)
	}

	similar, err := tm.FindSimilarPatterns(context.Background(), "BTC/USDT", "RSI oversold condition")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(similar), 1)
	assert.Equal(t, "BTC/USDT", similar[0].Symbol)
}

func TestTradeMemory_GetLessonsLearned(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO ai_lessons (category, pattern, lesson, weight) VALUES
		('momentum', 'RSI oversold', 'Wait for confirmation before entry', 1.0)`)
	require.NoError(t, err)

	lessons, err := tm.GetLessonsLearned(context.Background())
	require.NoError(t, err)
	assert.Contains(t, lessons, "RSI oversold")
	assert.Contains(t, lessons, "Wait for confirmation")
}

func TestTradeMemory_GetPerformanceStats(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO ai_trade_memory (id, timestamp, exchange, symbol, action, outcome, pnl, confidence) VALUES
		('p1', datetime('now'), 'binance', 'BTC/USDT', 'buy', 'win', 100, 0.85),
		('p2', datetime('now'), 'binance', 'ETH/USDT', 'buy', 'win', 50, 0.75),
		('p3', datetime('now'), 'binance', 'SOL/USDT', 'sell', 'loss', -30, 0.65)`)
	require.NoError(t, err)

	stats, err := tm.GetPerformanceStats(context.Background())
	require.NoError(t, err)

	require.NotNil(t, stats["total_trades"])
	require.NotNil(t, stats["wins"])
	require.NotNil(t, stats["losses"])
	assert.Equal(t, 3, stats["total_trades"])
	assert.Equal(t, 2, stats["wins"])
	assert.Equal(t, 1, stats["losses"])
}

func TestTradeMemory_BuildMemoryContext(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO ai_trade_memory (id, timestamp, exchange, symbol, action, outcome, pnl, pnl_percent, confidence) VALUES
		('ctx_1', datetime('now'), 'binance', 'BTC/USDT', 'buy', 'win', 100, 2.5, 0.85)`)
	require.NoError(t, err)

	context, err := tm.BuildMemoryContext(context.Background(), "BTC/USDT", "current market conditions")
	require.NoError(t, err)
	assert.Contains(t, context, "Past Trading History")
	assert.Contains(t, context, "Performance Stats")
}

func TestTradeMemory_GetPerformanceStatsWindow_UsesScopedJournalOnlyWhenScopeKeysExist(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	setupRealizedPnLJournal(t, db)

	_, err = db.Exec(`INSERT INTO ai_trade_memory (id, timestamp, exchange, symbol, action, outcome, pnl, confidence) VALUES
		('mem_1', datetime('now'), 'bitget', 'BTC/USDT', 'buy', 'win', 2, 0.90),
		('mem_2', datetime('now'), 'bitget', 'ETH/USDT', 'buy', 'loss', -1, 0.60)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('rp_1', 'ord_1', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 101, 1, 0, 'autonomous', datetime('now'), datetime('now')),
		('rp_2', 'ord_2', 'chat-1', 'bitget', 'ETH/USDT', 'sell', 1, 100, 99, -1, 0, 'autonomous', datetime('now'), datetime('now')),
		('rp_3', 'ord_3', 'chat-1', 'bitget', 'SOL/USDT', 'buy', 1, 100, 100, 0, 0, 'autonomous', datetime('now'), datetime('now')),
		('rp_4', 'ord_4', 'chat-foreign', 'bitget', 'XRP/USDT', 'buy', 1, 100, 105, 5, 0, 'autonomous', datetime('now'), datetime('now')),
		('rp_5', 'ord_5', 'chat-1', 'binance', 'BNB/USDT', 'buy', 1, 100, 104, 4, 0, 'autonomous', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	tests := []struct {
		name           string
		scope          ScalpingAutonomyScope
		expectedTrades int
		expectedWins   int
		expectedLosses int
		expectedBreak  int
		expectedDecis  int
		expectedPnL    decimal.Decimal
	}{
		{
			name: "full_scope_prefers_realized_journal",
			scope: ScalpingAutonomyScope{
				ChatID:   "chat-1",
				Exchange: "bitget",
			},
			expectedTrades: 3,
			expectedWins:   1,
			expectedLosses: 1,
			expectedBreak:  1,
			expectedDecis:  2,
			expectedPnL:    decimal.Zero,
		},
		{
			name: "exchange_only_scope_prefers_realized_journal",
			scope: ScalpingAutonomyScope{
				Exchange: "bitget",
			},
			expectedTrades: 4,
			expectedWins:   2,
			expectedLosses: 1,
			expectedBreak:  1,
			expectedDecis:  3,
			expectedPnL:    decimal.NewFromInt(5),
		},
		{
			name: "scoped_no_match_returns_empty_scoped_stats",
			scope: ScalpingAutonomyScope{
				ChatID:   "chat-1",
				Exchange: "kraken",
			},
			expectedTrades: 0,
			expectedWins:   0,
			expectedLosses: 0,
			expectedBreak:  0,
			expectedDecis:  0,
			expectedPnL:    decimal.Zero,
		},
		{
			name:           "empty_scope_falls_back_to_legacy_memory",
			scope:          ScalpingAutonomyScope{},
			expectedTrades: 2,
			expectedWins:   1,
			expectedLosses: 1,
			expectedBreak:  0,
			expectedDecis:  2,
			expectedPnL:    decimal.NewFromInt(1),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := WithScalpingAutonomyScope(context.Background(), tt.scope)
			stats, err := tm.GetPerformanceStatsWindow(ctx, 24*30)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedTrades, stats.TotalTrades)
			assert.Equal(t, tt.expectedWins, stats.Wins)
			assert.Equal(t, tt.expectedLosses, stats.Losses)
			assert.Equal(t, tt.expectedBreak, stats.Breakeven)
			assert.Equal(t, tt.expectedDecis, stats.DecisiveTrades)
			assert.True(t, stats.TotalPnL.Equal(tt.expectedPnL), "expected pnl %s, got %s", tt.expectedPnL.String(), stats.TotalPnL.String())
		})
	}
}

func TestTradeMemory_GetPerformanceStatsWindow_UsesConfiguredDefaultLookback(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemoryWithConfig(db, TradeMemoryConfig{
		MemoryLookbackHoursDefault: 24,
		MemorySampleLimitDefault:   250,
	})
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO ai_trade_memory (id, timestamp, exchange, symbol, action, outcome, pnl, confidence) VALUES
		('recent_1', ?, 'bitget', 'BTC/USDT', 'buy', 'win', 2, 0.90),
		('old_1', ?, 'bitget', 'ETH/USDT', 'buy', 'loss', -1, 0.60)`,
		time.Now().UTC().Add(-6*time.Hour),
		time.Now().UTC().Add(-48*time.Hour),
	)
	require.NoError(t, err)

	stats, err := tm.GetPerformanceStatsWindow(context.Background(), 0)
	require.NoError(t, err)

	assert.Equal(t, 24, stats.LookbackHours)
	assert.Equal(t, 1, stats.TotalTrades)
	assert.Equal(t, 1, stats.Wins)
	assert.Zero(t, stats.Losses)
	assert.True(t, stats.TotalPnL.Equal(decimal.NewFromInt(2)))
}

func TestTradeMemory_GetScopedExpectancyStats(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	setupRealizedPnLJournal(t, db)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('rp_a', 'ord_a', 'chat-1', 'bitget', 'btc/usdt', 'buy', 1, 100, 102, 2, 0, 'autonomous', datetime('now'), datetime('now')),
		('rp_b', 'ord_b', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 99, -1, 0, 'autonomous', datetime('now'), datetime('now')),
		('rp_suffix', 'ord_suffix', 'chat-1', 'bitget', 'BTC-USDT:USDT', 'buy', 1, 100, 103, 3, 0, 'autonomous', datetime('now'), datetime('now')),
		-- rp_future uses datetime('now', '+2 hours') intentionally to verify future rows are excluded from expectancy calculations.
		('rp_future', 'ord_future', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 140, 40, 0, 'autonomous', datetime('now', '+2 hours'), datetime('now', '+2 hours')),
		('rp_c', 'ord_c', 'chat-2', 'bitget', 'BTC/USDT', 'buy', 1, 100, 109, 9, 0, 'autonomous', datetime('now'), datetime('now')),
		('rp_d', 'ord_d', 'chat-1', 'binance', 'BTC/USDT', 'buy', 1, 100, 108, 8, 0, 'autonomous', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	t.Run("scoped_match", func(t *testing.T) {
		ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
			ChatID:   "chat-1",
			Exchange: "bitget",
		})

		stats, found, err := tm.GetScopedExpectancyStats(ctx, "BTC/USDT", "buy", 24*30, 50)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, stats)
		assert.Equal(t, 3, stats.SampleSize)
		assert.Equal(t, 2, stats.Wins)
		assert.Equal(t, 1, stats.Losses)
		assert.InDelta(t, 1.3333333, stats.NetExpectancy, 0.0001)
	})

	t.Run("scoped_no_match_returns_false", func(t *testing.T) {
		ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
			ChatID:   "chat-1",
			Exchange: "kraken",
		})

		stats, found, err := tm.GetScopedExpectancyStats(ctx, "BTC/USDT", "buy", 24*30, 50)
		require.NoError(t, err)
		assert.False(t, found)
		assert.Nil(t, stats)
	})

	t.Run("breakeven_only_rows_return_scoped_zero_sample", func(t *testing.T) {
		_, err = db.Exec(`INSERT INTO realized_pnl_journal (
			id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
		) VALUES
			('rp_flat', 'ord_flat', 'chat-flat', 'bitget', 'BTC/USDT', 'buy', 1, 100, 100, 0, 0, 'autonomous', datetime('now'), datetime('now'))`)
		require.NoError(t, err)

		ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
			ChatID:   "chat-flat",
			Exchange: "bitget",
		})

		stats, found, err := tm.GetScopedExpectancyStats(ctx, "BTC/USDT", "buy", 24*30, 50)
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, stats)
		assert.Zero(t, stats.SampleSize)
		assert.Zero(t, stats.Wins)
		assert.Zero(t, stats.Losses)
		assert.Zero(t, stats.NetExpectancy)
	})
}

func TestTradeMemory_GetScopedExpectancyStats_BitgetExchangeReconciliationSubtractsFees(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	setupRealizedPnLJournal(t, db)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('rp_net_a', 'ord_net_a', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 105, 5, 1, 'exchange_reconciliation', datetime('now'), datetime('now')),
		('rp_net_b', 'ord_net_b', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 95, -2, 0.5, 'exchange_reconciliation', datetime('now'), datetime('now'))`)
	require.NoError(t, err)

	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	stats, found, err := tm.GetScopedExpectancyStats(ctx, "BTC/USDT", "buy", 24*30, 50)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, stats)
	assert.Equal(t, 2, stats.SampleSize)
	assert.Equal(t, 1, stats.Wins)
	assert.Equal(t, 1, stats.Losses)
	assert.InDelta(t, 0.75, stats.NetExpectancy, 0.0001)
}

func TestTradeMemory_GetScopedExpectancyStats_MissingTableReturnsNoData(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	stats, found, err := tm.GetScopedExpectancyStats(ctx, "BTC/USDT", "buy", 24, 10)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, stats)
}

func TestTradeMemory_GetScopedExpectancyStats_DefaultLookbackAndLimit(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	setupRealizedPnLJournal(t, db)

	for i := 0; i < 300; i++ {
		_, err = db.Exec(`INSERT INTO realized_pnl_journal (
			id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("rp_%03d", i),
			fmt.Sprintf("ord_%03d", i),
			"chat-1",
			"bitget",
			"BTC/USDT",
			"buy",
			1,
			100,
			101,
			1,
			0,
			"autonomous",
			time.Now().UTC(),
			time.Now().UTC(),
		)
		require.NoError(t, err)
	}

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"rp_old",
		"ord_old",
		"chat-1",
		"bitget",
		"BTC/USDT",
		"buy",
		1,
		100,
		101,
		5,
		0,
		"autonomous",
		time.Now().UTC().Add(-31*24*time.Hour),
		time.Now().UTC().Add(-31*24*time.Hour),
	)
	require.NoError(t, err)

	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	stats, found, err := tm.GetScopedExpectancyStats(ctx, "BTC/USDT", "buy", 0, 0)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, stats)
	defaults := DefaultTradeMemoryConfig()
	assert.Equal(t, defaults.MemorySampleLimitDefault, stats.SampleSize)
	assert.Equal(t, defaults.MemorySampleLimitDefault, stats.Wins)
	assert.Zero(t, stats.Losses)
}

func TestTradeMemory_GetScopedExpectancyStats_UsesConfiguredDefaultsAndDeterministicOrdering(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemoryWithConfig(db, TradeMemoryConfig{
		MemoryLookbackHoursDefault: 24,
		MemorySampleLimitDefault:   1,
	})
	require.NoError(t, err)

	setupRealizedPnLJournal(t, db)

	closedAt := time.Now().UTC().Truncate(time.Second)
	olderCreatedAt := closedAt.Add(-2 * time.Minute)
	newerCreatedAt := closedAt.Add(-1 * time.Minute)
	outsideLookback := closedAt.Add(-48 * time.Hour)

	_, err = db.Exec(`INSERT INTO realized_pnl_journal (
		id, order_id, chat_id, exchange, symbol, side, filled_amount, entry_price, exit_price, realized_pnl, fees, source, closed_at, created_at
	) VALUES
		('rp_oldest', 'ord_oldest', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 98, -2, 0, 'autonomous', ?, ?),
		('rp_newest', 'ord_newest', 'chat-1', 'bitget', 'BTC-USDT:USDT', 'buy', 1, 100, 103, 3, 0, 'autonomous', ?, ?),
		('rp_outside', 'ord_outside', 'chat-1', 'bitget', 'BTC/USDT', 'buy', 1, 100, 110, 10, 0, 'autonomous', ?, ?)`,
		closedAt, olderCreatedAt,
		closedAt, newerCreatedAt,
		outsideLookback, outsideLookback,
	)
	require.NoError(t, err)

	ctx := WithScalpingAutonomyScope(context.Background(), ScalpingAutonomyScope{
		ChatID:   "chat-1",
		Exchange: "bitget",
	})

	stats, found, err := tm.GetScopedExpectancyStats(ctx, "BTC/USDT", "buy", 0, 0)
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, stats)
	assert.Equal(t, 1, stats.SampleSize)
	assert.Equal(t, 1, stats.Wins)
	assert.Zero(t, stats.Losses)
	assert.InDelta(t, 3.0, stats.NetExpectancy, 0.0001)
}

func TestTradeMemory_RecordLesson(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	err = tm.RecordLesson(context.Background(), "risk", "high volatility", "reduce position size", "trade_123")
	require.NoError(t, err)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM ai_lessons WHERE pattern = 'high volatility'").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestTradeMemory_RecordTradeDecisionJSON(t *testing.T) {
	db := setupTestDB(t)
	tm, err := NewTradeMemory(db)
	require.NoError(t, err)

	decisionJSON := `{"action":"buy","symbol":"BTC/USDT","size_pct":2.5,"confidence":0.9,"reasoning":"Strong uptrend"}`

	err = tm.RecordTradeDecisionJSON(decisionJSON)
	require.NoError(t, err)

	var action, symbol string
	err = db.QueryRow("SELECT action, symbol FROM ai_trade_memory").Scan(&action, &symbol)
	require.NoError(t, err)
	assert.Equal(t, "buy", action)
	assert.Equal(t, "BTC/USDT", symbol)
}

func TestExtractKeywords(t *testing.T) {
	keywords := extractKeywords("RSI is oversold with bullish momentum and high volume")
	assert.Contains(t, keywords, "oversold")
	assert.Contains(t, keywords, "bullish")
	assert.Contains(t, keywords, "volume")
}

func TestCalculateSimilarity(t *testing.T) {
	keywords := []string{"oversold", "bullish", "momentum"}

	score := calculateSimilarity(keywords, "RSI is oversold with bullish momentum")
	assert.GreaterOrEqual(t, score, 0.6)

	score = calculateSimilarity(keywords, "bearish trend with low volume")
	assert.Less(t, score, 0.4)

	score = calculateSimilarity([]string{}, "any text")
	assert.Equal(t, 0.0, score)
}

func TestTruncate(t *testing.T) {
	result := truncate("short", 10)
	assert.Equal(t, "short", result)

	result = truncate("this is a very long string", 10)
	assert.Equal(t, "this is a ...", result)
}
