package services

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDBManifestLiveModeGuard_DBOnly(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "guard-db.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	manifest := &PaperTradingReadinessManifest{
		Timestamp: time.Now().UTC(),
		Strategies: []StrategyEvidence{
			{Strategy: "paper_trading", ClosedTrades: 12, WinningTrades: 8, LosingTrades: 4, OpenPositions: 0, NetPnL: decimal.NewFromFloat(100), AvgNetPnL: decimal.NewFromFloat(10), MaxDrawdown: decimal.NewFromFloat(5)},
			{Strategy: "scalping", ClosedTrades: 25, WinningTrades: 15, LosingTrades: 10, OpenPositions: 0, NetPnL: decimal.NewFromFloat(200), AvgNetPnL: decimal.NewFromFloat(8), MaxDrawdown: decimal.NewFromFloat(3)},
			{Strategy: "daily_trading", ClosedTrades: 15, WinningTrades: 9, LosingTrades: 6, OpenPositions: 0, NetPnL: decimal.NewFromFloat(150), AvgNetPnL: decimal.NewFromFloat(10), MaxDrawdown: decimal.NewFromFloat(4)},
			{Strategy: "swing_trading", ClosedTrades: 10, WinningTrades: 6, LosingTrades: 4, OpenPositions: 0, NetPnL: decimal.NewFromFloat(120), AvgNetPnL: decimal.NewFromFloat(12), MaxDrawdown: decimal.NewFromFloat(6)},
			{Strategy: "arbitrage", ClosedTrades: 5, WinningTrades: 3, LosingTrades: 2, OpenPositions: 0, NetPnL: decimal.NewFromFloat(50), AvgNetPnL: decimal.NewFromFloat(10), MaxDrawdown: decimal.NewFromFloat(2)},
		},
		Acceptance: AcceptanceResult{Ready: true},
	}
	_, err = store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	guard := DBManifestLiveModeGuard(store, "", []string{"paper_trading", "scalping", "daily_trading", "swing_trading", "arbitrage"})
	err = guard(ctx, "")
	require.NoError(t, err)
}

func TestDBManifestLiveModeGuard_FileFallback(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "guard-fallback.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	// No manifest in DB — create a file instead.
	lm := LiveReadinessManifest{
		UpdatedAt: time.Now().UTC(),
		Strategies: map[string]StrategyLiveReadiness{
			"paper_trading": {Ready: true, Evidence: "ok"},
			"scalping": {
				Ready: true, Evidence: "ok",
				EvidenceMetrics: &StrategyReadinessEvidence{
					ClosedTrades: 25, WinningTrades: 15, LosingTrades: 10,
					NetPnL: "10", AvgNetPnL: "5", MaxDrawdownPct: "3",
				},
			},
			"daily_trading": {
				Ready: true, Evidence: "ok",
				EvidenceMetrics: &StrategyReadinessEvidence{
					ClosedTrades: 15, WinningTrades: 9, LosingTrades: 6,
					NetPnL: "10", AvgNetPnL: "5", MaxDrawdownPct: "4",
				},
			},
			"swing_trading": {
				Ready: true, Evidence: "ok",
				EvidenceMetrics: &StrategyReadinessEvidence{
					ClosedTrades: 10, WinningTrades: 6, LosingTrades: 4,
					NetPnL: "10", AvgNetPnL: "5", MaxDrawdownPct: "6",
				},
			},
			"arbitrage": {
				Ready: true, Evidence: "ok", EvidenceMetrics: &StrategyReadinessEvidence{
					ClosedTrades: 5, WinningTrades: 3, LosingTrades: 2,
					NetPnL: "10", AvgNetPnL: "5", MaxDrawdownPct: "2", NoTradeSafety: true, NoTradeReason: "test",
				},
			},
		},
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	data, _ := json.Marshal(lm)
	require.NoError(t, os.WriteFile(path, data, 0600))

	guard := DBManifestLiveModeGuard(store, path, []string{"paper_trading", "scalping", "daily_trading", "swing_trading", "arbitrage"})
	err = guard(ctx, "")
	require.NoError(t, err)
}

func TestDBManifestLiveModeGuard_NoManifest(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "guard-empty.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	guard := DBManifestLiveModeGuard(store, "", []string{"scalping"})
	err = guard(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no readiness manifest available")
}

func TestDBManifestLiveModeGuard_MissingStrategy(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "guard-missing.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	manifest := &PaperTradingReadinessManifest{
		Timestamp: time.Now().UTC(),
		Strategies: []StrategyEvidence{
			{Strategy: "scalping", ClosedTrades: 25, WinningTrades: 15, LosingTrades: 10, OpenPositions: 0, NetPnL: decimal.NewFromFloat(200), AvgNetPnL: decimal.NewFromFloat(8), MaxDrawdown: decimal.NewFromFloat(3)},
		},
		Acceptance: AcceptanceResult{Ready: true},
	}
	_, err = store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	guard := DBManifestLiveModeGuard(store, "", []string{"scalping", "arbitrage"})
	err = guard(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "arbitrage=missing")
}

func TestDBManifestLiveModeGuard_NotReadyStrategy(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "guard-notready.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	manifest := &PaperTradingReadinessManifest{
		Timestamp: time.Now().UTC(),
		Strategies: []StrategyEvidence{
			{Strategy: "scalping", ClosedTrades: 25, WinningTrades: 15, LosingTrades: 10, OpenPositions: 0, NetPnL: decimal.NewFromFloat(200), AvgNetPnL: decimal.NewFromFloat(8), MaxDrawdown: decimal.NewFromFloat(3)},
			{Strategy: "arbitrage", ClosedTrades: 5, WinningTrades: 0, LosingTrades: 2, OpenPositions: 0, NetPnL: decimal.NewFromFloat(50), AvgNetPnL: decimal.NewFromFloat(10), MaxDrawdown: decimal.NewFromFloat(2)},
		},
		Acceptance: AcceptanceResult{Ready: true},
	}
	_, err = store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	guard := DBManifestLiveModeGuard(store, "", []string{"arbitrage"})
	err = guard(ctx, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "arbitrage=no_winning_trades")
}
