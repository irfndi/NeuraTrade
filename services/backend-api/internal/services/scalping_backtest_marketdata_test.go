package services

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSignalsFromMarketData_LegacyLastPriceSchema(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "legacy-market-data.db"))
	require.NoError(t, err)
	defer sqliteDB.Close()

	ctx := context.Background()
	_, err = sqliteDB.DB.ExecContext(ctx, `
		CREATE TABLE exchanges (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			ccxt_id TEXT NOT NULL
		);
		CREATE TABLE ccxt_exchanges (
			id INTEGER PRIMARY KEY,
			exchange_id INTEGER NOT NULL,
			ccxt_id TEXT NOT NULL
		);
		CREATE TABLE trading_pairs (
			id INTEGER PRIMARY KEY,
			exchange_id INTEGER NOT NULL,
			symbol TEXT NOT NULL
		);
		CREATE TABLE market_data (
			id INTEGER PRIMARY KEY,
			exchange_id INTEGER NOT NULL,
			trading_pair_id INTEGER NOT NULL,
			bid DECIMAL(20, 8),
			ask DECIMAL(20, 8),
			last_price DECIMAL(20, 8) NOT NULL,
			volume_24h DECIMAL(20, 8),
			timestamp DATETIME NOT NULL
		);
		INSERT INTO exchanges (id, name, ccxt_id) VALUES (1, 'bitget', 'bitget');
		INSERT INTO ccxt_exchanges (exchange_id, ccxt_id) VALUES (1, 'bitget');
		INSERT INTO trading_pairs (id, exchange_id, symbol) VALUES (10, 1, 'TEST/USDT');
		INSERT INTO market_data (exchange_id, trading_pair_id, bid, ask, last_price, volume_24h, timestamp)
		VALUES
			(1, 10, 99, 101, 100, 1000, '2026-05-04 20:15:45+07:00'),
			(1, 10, 100, 103, 102, 1100, '2026-05-04 20:20:45+07:00');
	`)
	require.NoError(t, err)

	jakarta := time.FixedZone("WIB", 7*60*60)
	engine := NewScalpingBacktestEngine(sqliteDB, ScalpingBacktestConfig{
		StartTime:          time.Date(2026, 5, 4, 20, 0, 0, 0, jakarta),
		EndTime:            time.Date(2026, 5, 4, 21, 0, 0, 0, jakarta),
		Symbols:            []string{"TEST/USDT"},
		Exchange:           "bitget",
		InitialCapital:     decimal.NewFromInt(48),
		FeeRate:            decimal.NewFromFloat(0.0006),
		SlippagePct:        decimal.NewFromFloat(DefaultScalpingBacktestSlippage),
		MaxBidAskSpreadPct: 99,
		MinConfidence:      0.55,
		MinExpectancyN:     1,
		MaxCapitalPct:      5,
		DefaultHoldPeriod:  DefaultScalpingBacktestHoldPeriod,
		SpreadMultiplier:   DefaultScalpingBacktestSpreadMultiplier,
	})

	signals, err := engine.loadHistoricalSignals(ctx, engine.config.StartTime, engine.config.EndTime)
	require.NoError(t, err)
	require.Len(t, signals, 2)
	assert.Equal(t, "TEST/USDT", signals[0].Symbol)
	assert.Equal(t, "bitget", signals[0].Exchange)
	assert.Equal(t, 100.0, signals[0].Signal.Price)
	assert.Equal(t, 101.0, signals[0].Signal.High24h)
	assert.Equal(t, 99.0, signals[0].Signal.Low24h)
	assert.Greater(t, signals[0].Signal.BidAskSpread, 0.0)
}
