package services

import (
	"context"
	"sync"
	"testing"
	"time"

	appautonomy "github.com/irfndi/neuratrade/internal/app/autonomy"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestShadowEvaluationCoordinatorUpsertAndDeleteVariant(t *testing.T) {
	coordinator := NewShadowEvaluationCoordinator(nil, nil, nil, nil, nil)
	_, err := coordinator.UpsertVariant(ShadowVariantConfig{VariantID: "test", Name: "Test Variant"})
	if err != nil {
		t.Fatalf("expected upsert success, got error: %v", err)
	}
	if !coordinator.DeleteVariant("test") {
		t.Fatalf("expected delete success")
	}
}

func TestShadowEvaluationCoordinatorMirrorAndCompare(t *testing.T) {
	coordinator := NewShadowEvaluationCoordinator(nil, nil, nil, nil, []ShadowVariantConfig{
		NewDefaultShadowVariant(),
		{
			VariantID: "high-risk",
			Name:      "High Risk",
			PolicyOverrides: map[string]interface{}{
				ShadowPolicyMinConfidence: 0.55,
				ShadowPolicyMaxCapitalPct: 2.0,
			},
		},
	})
	entry := decimal.NewFromInt(100)
	live := &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		Confidence:  0.7,
		SizePercent: 1.5,
		EntryPrice:  &entry,
	}
	policy := appautonomy.ScalpingCyclePolicy{
		EffectiveMinConfidence: 0.65,
		EffectiveMaxCapitalPct: 1.0,
	}
	portfolio := TradingPortfolio{USDTBalanceDecimal: decimal.NewFromInt(1000)}
	mirrored, err := coordinator.MirrorDecision(context.Background(), live, portfolio, policy)
	if err != nil {
		t.Fatalf("expected mirror success, got error: %v", err)
	}
	if len(mirrored) < 2 {
		t.Fatalf("expected at least 2 mirrored decisions, got %d", len(mirrored))
	}
	report, err := coordinator.CompareLiveVsShadow(context.Background(), time.Now().UTC().Add(-time.Hour), time.Now().UTC())
	if err != nil {
		t.Fatalf("expected compare success, got error: %v", err)
	}
	if len(report.Comparisons) == 0 {
		t.Fatalf("expected at least one variant comparison")
	}
}

func TestShadowEvaluationCoordinatorConcurrentAccess(t *testing.T) {
	coordinator := NewShadowEvaluationCoordinator(nil, nil, nil, nil, []ShadowVariantConfig{
		NewDefaultShadowVariant(),
	})
	entry := decimal.NewFromInt(100)
	live := &AITradingDecision{
		Action:      "buy",
		Symbol:      "ETH/USDT",
		Confidence:  0.7,
		SizePercent: 1.0,
		EntryPrice:  &entry,
	}
	policy := appautonomy.ScalpingCyclePolicy{
		EffectiveMinConfidence: 0.65,
		EffectiveMaxCapitalPct: 1.0,
	}
	portfolio := TradingPortfolio{USDTBalanceDecimal: decimal.NewFromInt(1000)}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = coordinator.MirrorDecision(context.Background(), live, portfolio, policy)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			prices := map[string]decimal.Decimal{"ETH/USDT": decimal.NewFromInt(105)}
			sell := &AITradingDecision{Action: "sell", Symbol: "ETH/USDT"}
			coordinator.RecordShadowOutcome(context.Background(), sell, prices)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = coordinator.shadowMetricsSnapshot()
		}()
	}
	wg.Wait()
}

func TestShadowEvaluationCoordinatorCloseStalePositions(t *testing.T) {
	deterministic := DefaultPaperExecutionConfig()
	deterministic.EnableRandomness = false
	coordinator := NewShadowEvaluationCoordinator(nil, nil, NewPaperExecutionSimulator(deterministic), nil, []ShadowVariantConfig{
		NewDefaultShadowVariant(),
	})
	entry := decimal.NewFromInt(100)
	live := &AITradingDecision{
		Action:      "buy",
		Symbol:      "BTC/USDT",
		Confidence:  0.7,
		SizePercent: 1.0,
		EntryPrice:  &entry,
	}
	policy := appautonomy.ScalpingCyclePolicy{
		EffectiveMinConfidence: 0.65,
		EffectiveMaxCapitalPct: 1.0,
	}
	portfolio := TradingPortfolio{USDTBalanceDecimal: decimal.NewFromInt(1000)}

	_, _ = coordinator.MirrorDecision(context.Background(), live, portfolio, policy)

	runtime := coordinator.runtimeForVariant("baseline")
	runtime.mu.Lock()
	if _, ok := runtime.openDecisions["BTC/USDT"]; !ok {
		runtime.mu.Unlock()
		t.Fatalf("expected open decision for BTC/USDT after buy")
	}
	runtime.mu.Unlock()

	prices := map[string]decimal.Decimal{"BTC/USDT": decimal.NewFromInt(105)}
	maxAge := time.Now().UTC().Add(-time.Nanosecond)
	coordinator.CloseStaleShadowPositions(context.Background(), prices, maxAge)

	runtime.mu.Lock()
	if _, ok := runtime.openDecisions["BTC/USDT"]; ok {
		runtime.mu.Unlock()
		t.Fatalf("expected stale position to be closed")
	}
	runtime.mu.Unlock()
}

// TestShadowEvaluationCoordinator_PaperTradeReconciler covers the
// production orphan-paper-trade problem: the live path opens paper_trades
// rows via recordPaperTradeOpen but only the backfill path closes them,
// so rows accumulate unboundedly and break PnL accounting.
//
// The reconciler must:
//  1. Close any 'open' paper_trades row older than the configured cutoff.
//  2. Leave 'open' rows newer than the cutoff alone.
//  3. Be idempotent (re-running does nothing on the second pass).
//  4. Stop cleanly when ctx is cancelled.
//
// Run with: go test -v -count=1 -run TestShadowEvaluationCoordinator_PaperTradeReconciler ./internal/services/
func TestShadowEvaluationCoordinator_PaperTradeReconciler(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(":memory:")
	require.NoError(t, err)
	defer sqliteDB.Close()
	pool := readOnlyDBPoolAdapter{pool: sqliteDB}
	recorder := NewPaperTradeRecorder(pool, noopPaperDryRunLogger{})

	// Seed paper_trades schema.
	_, err = pool.Exec(context.Background(), `
		CREATE TABLE paper_trades (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id TEXT NOT NULL,
			quest_id INTEGER,
			strategy_id TEXT NOT NULL,
			exchange TEXT NOT NULL,
			symbol TEXT NOT NULL,
			side TEXT NOT NULL,
			entry_price DECIMAL(20, 8) NOT NULL,
			exit_price DECIMAL(20, 8) NOT NULL DEFAULT 0,
			size DECIMAL(20, 8) NOT NULL,
			fees DECIMAL(20, 8) NOT NULL DEFAULT 0,
			pnl DECIMAL(20, 8) NOT NULL DEFAULT 0,
			cost_basis DECIMAL(20, 8) NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'open',
			opened_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			closed_at DATETIME,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	// Insert one stale row and one fresh row.
	staleOpenedAt := time.Now().UTC().Add(-6 * time.Hour)
	freshOpenedAt := time.Now().UTC().Add(-1 * time.Minute)
	insertTrade := func(openedAt time.Time, symbol string) int64 {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO paper_trades
				(user_id, strategy_id, exchange, symbol, side, entry_price, size, cost_basis, status, opened_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'open', ?)
		`,
			shadowRecorderUserID, "baseline", "binance", symbol, "buy",
			"100.0", "1.0", "100.0", openedAt,
		)
		require.NoError(t, err)
		var id int64
		err = pool.QueryRow(context.Background(),
			"SELECT id FROM paper_trades WHERE symbol = ? ORDER BY id DESC LIMIT 1", symbol,
		).Scan(&id)
		require.NoError(t, err)
		return id
	}
	staleID := insertTrade(staleOpenedAt, "STALE/USDT")
	_ = insertTrade(freshOpenedAt, "FRESH/USDT")

	// Wire the coordinator with the recorder.
	coordinator := NewShadowEvaluationCoordinator(pool, zap.NewNop(), nil, recorder, nil)

	// Run a single sweep with a 4h cutoff: the stale row (6h old) must close,
	// the fresh row (1m old) must remain open.
	cfg := ReconciliationConfig{MaxAge: 4 * time.Hour, Interval: time.Hour}
	coordinator.runReconcileOnce(context.Background(), cfg)

	var staleStatus, freshStatus string
	require.NoError(t, pool.QueryRow(context.Background(),
		"SELECT status FROM paper_trades WHERE id = ?", staleID,
	).Scan(&staleStatus))
	require.NoError(t, pool.QueryRow(context.Background(),
		"SELECT status FROM paper_trades WHERE symbol = 'FRESH/USDT'",
	).Scan(&freshStatus))
	assert.Equal(t, "closed", staleStatus, "6h-old row must be closed by reconciler")
	assert.Equal(t, "open", freshStatus, "1m-old row must remain open")

	// Idempotency: re-running the sweep does not re-close anything (and
	// does not error on the already-closed row).
	coordinator.runReconcileOnce(context.Background(), cfg)

	// Start/Stop lifecycle: a long-running sweep should terminate cleanly.
	coordinator.Start(context.Background(), ReconciliationConfig{
		MaxAge:   4 * time.Hour,
		Interval: 50 * time.Millisecond,
	})
	time.Sleep(120 * time.Millisecond)
	coordinator.Stop()
	// Second Stop must be safe (no panic, no deadlock).
	coordinator.Stop()
}
