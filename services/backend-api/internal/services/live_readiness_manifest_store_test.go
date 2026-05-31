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

func TestLiveReadinessManifestStore_NilDatabase(t *testing.T) {
	store := NewLiveReadinessManifestStore(nil)
	ctx := context.Background()

	err := store.InitSchema(ctx)
	require.Error(t, err)

	_, err = store.SaveManifest(ctx, &PaperTradingReadinessManifest{})
	require.Error(t, err)

	_, err = store.GetLatestManifest(ctx)
	require.Error(t, err)

	_, err = store.GetLatestReadyManifest(ctx)
	require.Error(t, err)

	_, err = store.GetManifestByID(ctx, "test-id")
	require.Error(t, err)

	_, err = store.ListManifests(ctx, 10)
	require.Error(t, err)

	_, err = store.CountManifests(ctx)
	require.Error(t, err)

	_, err = store.HasReadyManifest(ctx)
	require.Error(t, err)
}

func TestLiveReadinessManifestStore_RoundTrip(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "manifest-store.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	manifest := &PaperTradingReadinessManifest{
		Timestamp:                  time.Now().UTC(),
		ContinuousValidationHours:  decimal.NewFromFloat(169.5),
		StrategyCount:              2,
		TotalTrades:                15,
		ClosedTrades:               12,
		OpenPositions:              1,
		NetPnL:                     decimal.NewFromFloat(123.45),
		OverallWinRate:             decimal.NewFromFloat(0.65),
		RiskLimitsEnforced:         true,
		BacktestComparisonVerified: true,
		DiagnosticOnly:             false,
		Strategies: []StrategyEvidence{
			{
				Strategy:      "scalping",
				TotalTrades:   10,
				ClosedTrades:  8,
				OpenPositions: 1,
				WinningTrades: 6,
				LosingTrades:  2,
				NetPnL:        decimal.NewFromFloat(80.0),
				AvgNetPnL:     decimal.NewFromFloat(10.0),
				WinRate:       decimal.NewFromFloat(0.75),
				MaxDrawdown:   decimal.NewFromFloat(5.5),
			},
			{
				Strategy:      "arbitrage",
				TotalTrades:   5,
				ClosedTrades:  4,
				OpenPositions: 0,
				WinningTrades: 3,
				LosingTrades:  1,
				NetPnL:        decimal.NewFromFloat(43.45),
				AvgNetPnL:     decimal.NewFromFloat(10.86),
				WinRate:       decimal.NewFromFloat(0.6),
				MaxDrawdown:   decimal.NewFromFloat(2.1),
			},
		},
		Acceptance: AcceptanceResult{
			Ready:            true,
			MinHoursMet:      true,
			MinTradesMet:     true,
			MinStrategiesMet: true,
			RiskLimitsMet:    true,
			BacktestMet:      true,
			Failures:         nil,
		},
	}

	id, err := store.SaveManifest(ctx, manifest)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	latest, err := store.GetLatestManifest(ctx)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.Equal(t, manifest.StrategyCount, latest.StrategyCount)
	assert.Equal(t, manifest.ClosedTrades, latest.ClosedTrades)
	assert.True(t, manifest.NetPnL.Equal(latest.NetPnL))
	assert.True(t, latest.Acceptance.Ready)

	ready, err := store.GetLatestReadyManifest(ctx)
	require.NoError(t, err)
	require.NotNil(t, ready)
	assert.True(t, ready.Acceptance.Ready)

	byID, err := store.GetManifestByID(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, byID)
	assert.Equal(t, manifest.StrategyCount, byID.StrategyCount)

	hasReady, err := store.HasReadyManifest(ctx)
	require.NoError(t, err)
	assert.True(t, hasReady)

	count, err := store.CountManifests(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)

	list, err := store.ListManifests(ctx, 10)
	require.NoError(t, err)
	require.Len(t, list, 1)
	assert.Equal(t, manifest.StrategyCount, list[0].StrategyCount)
}

func TestLiveReadinessManifestStore_NotReadyManifest(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "manifest-not-ready.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	manifest := &PaperTradingReadinessManifest{
		Timestamp:                 time.Now().UTC(),
		ContinuousValidationHours: decimal.NewFromFloat(24.0),
		StrategyCount:             1,
		ClosedTrades:              2,
		Acceptance: AcceptanceResult{
			Ready:        false,
			MinHoursMet:  false,
			MinTradesMet: false,
			Failures:     []string{"hours too low"},
		},
	}

	id, err := store.SaveManifest(ctx, manifest)
	require.NoError(t, err)
	require.NotEmpty(t, id)

	latest, err := store.GetLatestManifest(ctx)
	require.NoError(t, err)
	require.NotNil(t, latest)
	assert.False(t, latest.Acceptance.Ready)

	ready, err := store.GetLatestReadyManifest(ctx)
	require.NoError(t, err)
	assert.Nil(t, ready)

	hasReady, err := store.HasReadyManifest(ctx)
	require.NoError(t, err)
	assert.False(t, hasReady)
}

func TestLiveReadinessManifestStore_ListLimit(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "manifest-list.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	for i := 0; i < 5; i++ {
		m := &PaperTradingReadinessManifest{
			Timestamp:    time.Now().UTC().Add(time.Duration(i) * time.Minute),
			ClosedTrades: i + 1,
			Acceptance:   AcceptanceResult{Ready: i%2 == 0},
		}
		_, err := store.SaveManifest(ctx, m)
		require.NoError(t, err)
	}

	list, err := store.ListManifests(ctx, 3)
	require.NoError(t, err)
	require.Len(t, list, 3)

	count, err := store.CountManifests(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, count)
}

func TestLiveReadinessManifestStore_GetManifestByID_Missing(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "manifest-missing.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	byID, err := store.GetManifestByID(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, byID)
}

func TestLiveReadinessManifestStore_GetManifestByID_EmptyID(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "manifest-empty.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	_, err = store.GetManifestByID(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "id is required")
}
