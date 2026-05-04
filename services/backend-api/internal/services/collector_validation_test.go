package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/irfndi/neuratrade/internal/cache"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/test/testmocks"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorServiceSaveBulkTickerDataIgnoresStaleExchangeCache(t *testing.T) {
	collector, db, redisClient := newCollectorWithSQLiteAndRedis(t)
	ctx := context.Background()

	require.NoError(t, redisClient.Set(ctx, "exchange:ccxt_id:bitget", "999", time.Hour).Err())

	err := collector.saveBulkTickerData(models.MarketPrice{
		ExchangeName: "bitget",
		Symbol:       "TRX/USDT",
		Price:        decimal.NewFromFloat(0.291),
		Volume:       decimal.NewFromFloat(125000),
		Timestamp:    time.Now(),
	})

	require.NoError(t, err)
	assertSQLiteScalar(t, db, "SELECT COUNT(*) FROM exchanges WHERE ccxt_id = 'bitget'", 1)
	assertSQLiteScalar(t, db, "SELECT COUNT(*) FROM trading_pairs WHERE symbol = 'TRX/USDT'", 1)
	assertSQLiteScalar(t, db, "SELECT COUNT(*) FROM market_data", 1)

	cachedExchangeID, err := redisClient.Get(ctx, "exchange:ccxt_id:bitget").Result()
	require.NoError(t, err)
	assert.NotEqual(t, "999", cachedExchangeID)
}

func TestCollectorServiceSaveBulkTickerDataIgnoresStaleTradingPairCache(t *testing.T) {
	collector, db, redisClient := newCollectorWithSQLiteAndRedis(t)
	ctx := context.Background()

	_, err := db.Exec(ctx, `
		INSERT INTO exchanges (id, name, display_name, ccxt_id, api_url, status, has_spot, has_futures)
		VALUES (1, 'bitget', 'Bitget', 'bitget', 'https://api.bitget.com', 'active', 1, 1)
	`)
	require.NoError(t, err)
	require.NoError(t, redisClient.Set(ctx, "exchange:ccxt_id:bitget", "1", time.Hour).Err())
	require.NoError(t, redisClient.Set(ctx, "trading_pair:1:LINK/USDT", "999", time.Hour).Err())

	err = collector.saveBulkTickerData(models.MarketPrice{
		ExchangeName: "bitget",
		Symbol:       "LINK/USDT",
		Price:        decimal.NewFromFloat(14.25),
		Volume:       decimal.NewFromFloat(85000),
		Timestamp:    time.Now(),
	})

	require.NoError(t, err)
	assertSQLiteScalar(t, db, "SELECT COUNT(*) FROM trading_pairs WHERE exchange_id = 1 AND symbol = 'LINK/USDT'", 1)
	assertSQLiteScalar(t, db, "SELECT COUNT(*) FROM market_data", 1)

	cachedTradingPairID, err := redisClient.Get(ctx, "trading_pair:1:LINK/USDT").Result()
	require.NoError(t, err)
	assert.NotEqual(t, "999", cachedTradingPairID)
}

func TestCollectorServiceSaveBulkTickerDataReusesCachedExchangeWithMixedCaseName(t *testing.T) {
	collector, db, redisClient := newCollectorWithSQLiteAndRedis(t)
	ctx := context.Background()

	_, err := db.Exec(ctx, `
		INSERT INTO exchanges (id, name, display_name, ccxt_id, api_url, status, has_spot, has_futures)
		VALUES (1, 'Bitget', 'Bitget', 'bitget', 'https://api.bitget.com', 'active', 1, 1)
	`)
	require.NoError(t, err)
	require.NoError(t, redisClient.Set(ctx, "exchange:ccxt_id:bitget", "1", time.Hour).Err())

	err = collector.saveBulkTickerData(models.MarketPrice{
		ExchangeName: "bitget",
		Symbol:       "BTC/USDT",
		Price:        decimal.NewFromFloat(64000),
		Volume:       decimal.NewFromFloat(2100),
		Timestamp:    time.Now(),
	})

	require.NoError(t, err)
	assertSQLiteScalar(t, db, "SELECT COUNT(*) FROM exchanges", 1)
	assertSQLiteScalar(t, db, "SELECT COUNT(*) FROM trading_pairs WHERE exchange_id = 1 AND symbol = 'BTC/USDT'", 1)
	assertSQLiteScalar(t, db, "SELECT COUNT(*) FROM market_data WHERE exchange_id = 1", 1)
}

func newCollectorWithSQLiteAndRedis(t *testing.T) (*CollectorService, *database.SQLiteDB, *redis.Client) {
	t.Helper()

	db, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "collector.db"))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })

	createCollectorSQLiteSchema(t, db)

	redisServer := miniredis.RunT(t)
	t.Cleanup(redisServer.Close)

	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { require.NoError(t, redisClient.Close()) })

	collector := NewCollectorService(
		db,
		&testmocks.MockCCXTService{},
		&config.Config{},
		redisClient,
		cache.NewInMemoryBlacklistCache(),
	)

	return collector, db, redisClient
}

func createCollectorSQLiteSchema(t *testing.T, db *database.SQLiteDB) {
	t.Helper()

	ctx := context.Background()
	statements := []string{
		`CREATE TABLE exchanges (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			ccxt_id TEXT NOT NULL UNIQUE,
			api_url TEXT,
			status TEXT DEFAULT 'active',
			has_spot BOOLEAN DEFAULT 1,
			has_futures BOOLEAN DEFAULT 0,
			is_active BOOLEAN DEFAULT 1,
			priority INTEGER DEFAULT 0,
			last_ping DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE trading_pairs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange_id INTEGER NOT NULL,
			symbol TEXT NOT NULL,
			base_currency TEXT NOT NULL,
			quote_currency TEXT NOT NULL,
			is_active BOOLEAN DEFAULT 1,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
			UNIQUE(exchange_id, symbol)
		)`,
		`CREATE TABLE market_data (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			exchange_id INTEGER NOT NULL,
			trading_pair_id INTEGER NOT NULL,
			bid DECIMAL(20, 8),
			bid_volume DECIMAL(20, 8),
			ask DECIMAL(20, 8),
			ask_volume DECIMAL(20, 8),
			last_price DECIMAL(20, 8) NOT NULL,
			volume_24h DECIMAL(20, 8),
			timestamp DATETIME NOT NULL,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (exchange_id) REFERENCES exchanges(id) ON DELETE CASCADE,
			FOREIGN KEY (trading_pair_id) REFERENCES trading_pairs(id) ON DELETE CASCADE
		)`,
	}

	for _, stmt := range statements {
		_, err := db.Exec(ctx, stmt)
		require.NoError(t, err)
	}
}

func assertSQLiteScalar(t *testing.T, db *database.SQLiteDB, query string, want int) {
	t.Helper()

	var got int
	err := db.QueryRow(context.Background(), query).Scan(&got)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}
