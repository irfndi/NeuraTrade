package services

import (
	"context"
	"database/sql"
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/stretchr/testify/require"
)

func TestScalpingTelemetryStore_InsertCycleRecordPersistsSignalQuality(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-telemetry.db"))
	require.NoError(t, err)
	defer sqliteDB.Close()

	store := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, store.EnsureSchema(ctx))

	cycleID, err := store.InsertCycleRecord(ctx, CycleRecord{
		ID:                     "cycle-quality",
		ChatID:                 "chat-1",
		Exchange:               "bitget",
		CycleAt:                time.Date(2026, 5, 11, 9, 0, 0, 0, time.UTC),
		Symbol:                 "BTC/USDT",
		Action:                 "buy",
		Confidence:             0.81,
		BidAskSpreadPct:        floatPtr(0.027),
		OrderBookImbalance:     floatPtr(0.46),
		RangePosition24h:       floatPtr(41.5),
		PriceChange24hPct:      floatPtr(-0.64),
		EffectiveMinConfidence: 0.65,
		EffectiveMaxCapitalPct: 0.50,
		PolicyAdjustmentsJSON:  "[]",
	})
	require.NoError(t, err)
	require.Equal(t, "cycle-quality", cycleID)

	var spread, imbalance, rangePos, priceChange float64
	err = sqliteDB.DB.QueryRowContext(ctx, `
		SELECT bid_ask_spread_pct, order_book_imbalance, range_position_24h, price_change_24h_pct
		FROM scalping_cycle_telemetry
		WHERE id = ?
	`, cycleID).Scan(&spread, &imbalance, &rangePos, &priceChange)
	require.NoError(t, err)

	require.InDelta(t, 0.027, spread, 1e-9)
	require.InDelta(t, 0.46, imbalance, 1e-9)
	require.InDelta(t, 41.5, rangePos, 1e-9)
	require.InDelta(t, -0.64, priceChange, 1e-9)
}

func TestScalpingTelemetryStore_InsertCycleRecordSanitizesNonFiniteSignalQuality(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "scalping-telemetry-nonfinite.db"))
	require.NoError(t, err)
	defer sqliteDB.Close()

	store := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, store.EnsureSchema(ctx))

	cycleID, err := store.InsertCycleRecord(ctx, CycleRecord{
		ID:                 "cycle-quality-nonfinite",
		ChatID:             "chat-1",
		Exchange:           "bitget",
		CycleAt:            time.Date(2026, 5, 11, 9, 5, 0, 0, time.UTC),
		Symbol:             "BTC/USDT",
		Action:             "buy",
		BidAskSpreadPct:    floatPtr(math.NaN()),
		OrderBookImbalance: floatPtr(math.Inf(1)),
		RangePosition24h:   floatPtr(math.Inf(-1)),
		PriceChange24hPct:  floatPtr(math.NaN()),
	})
	require.NoError(t, err)
	require.Equal(t, "cycle-quality-nonfinite", cycleID)

	var spread, imbalance, rangePos, priceChange sql.NullFloat64
	err = sqliteDB.DB.QueryRowContext(ctx, `
		SELECT bid_ask_spread_pct, order_book_imbalance, range_position_24h, price_change_24h_pct
		FROM scalping_cycle_telemetry
		WHERE id = ?
	`, cycleID).Scan(&spread, &imbalance, &rangePos, &priceChange)
	require.NoError(t, err)

	require.False(t, spread.Valid)
	require.False(t, imbalance.Valid)
	require.False(t, rangePos.Valid)
	require.False(t, priceChange.Valid)
}

func TestScalpingTelemetryStore_EnsureSchemaAddsSignalQualityColumnsToLegacyTable(t *testing.T) {
	ctx := context.Background()
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "legacy-scalping-telemetry.db"))
	require.NoError(t, err)
	defer sqliteDB.Close()

	_, err = sqliteDB.DB.ExecContext(ctx, `
		CREATE TABLE scalping_cycle_telemetry (
			id TEXT PRIMARY KEY,
			chat_id TEXT,
			exchange TEXT,
			order_id TEXT,
			cycle_at TIMESTAMP,
			symbol TEXT,
			action TEXT,
			confidence REAL,
			universe_count INT,
			ranked_count INT,
			viable_count INT,
			rejection_counts TEXT,
			regime TEXT,
			expectancy REAL,
			expectancy_sample_size INT,
			gate_block_code TEXT,
			gate_block_reason TEXT,
			account_tier TEXT,
			effective_min_confidence REAL,
			effective_max_capital_pct REAL,
			policy_adjustments TEXT,
			outcome TEXT,
			pnl NUMERIC,
			hold_duration_seconds INT,
			closed_at TIMESTAMP
		)
	`)
	require.NoError(t, err)

	store := NewScalpingTelemetryStore(sqliteDB, nil)
	require.NoError(t, store.EnsureSchema(ctx))

	for _, column := range []string{
		"bid_ask_spread_pct",
		"order_book_imbalance",
		"range_position_24h",
		"price_change_24h_pct",
	} {
		var count int
		err = sqliteDB.DB.QueryRowContext(ctx, `
			SELECT COUNT(*)
			FROM pragma_table_info('scalping_cycle_telemetry')
			WHERE name = ?
		`, column).Scan(&count)
		require.NoError(t, err)
		require.Equal(t, 1, count, "expected column %s", column)
	}
}
