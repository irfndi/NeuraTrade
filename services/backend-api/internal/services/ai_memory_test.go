package services

import (
	"context"
	"database/sql"
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
	assert.Equal(t, int64(3), stats["total_trades"])
	assert.Equal(t, int64(2), stats["wins"])
	assert.Equal(t, int64(1), stats["losses"])
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
