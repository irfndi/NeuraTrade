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

type reconcilerTestLogger struct {
	infos  []string
	warns  []string
	errors []string
}

func (l *reconcilerTestLogger) WithFields(_ map[string]interface{}) Logger { return l }
func (l *reconcilerTestLogger) Info(msg string)                            { l.infos = append(l.infos, msg) }
func (l *reconcilerTestLogger) Warn(msg string)                            { l.warns = append(l.warns, msg) }
func (l *reconcilerTestLogger) Error(msg string)                           { l.errors = append(l.errors, msg) }

func TestLiveReadinessReconciler_StartStop(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "reconciler.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	logger := &reconcilerTestLogger{}
	config := LiveReadinessReconcilerConfig{
		Interval:       50 * time.Millisecond,
		LookbackWindow: 1 * time.Hour,
		Strategies:     []string{},
		Capital:        decimal.Zero,
	}

	r := NewLiveReadinessReconciler(sqliteDB, store, logger, config)
	require.NoError(t, r.Start(ctx))
	time.Sleep(120 * time.Millisecond)
	r.Stop()

	assert.NotZero(t, r.LastRun())
	assert.Nil(t, r.LastError())
}

func TestLiveReadinessReconciler_DoubleStart(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "reconciler-double.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	ctx := context.Background()
	require.NoError(t, store.InitSchema(ctx))

	config := LiveReadinessReconcilerConfig{Interval: 1 * time.Hour}
	r := NewLiveReadinessReconciler(sqliteDB, store, &reconcilerTestLogger{}, config)
	require.NoError(t, r.Start(ctx))
	defer r.Stop()

	err = r.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestLiveReadinessReconciler_StopNotRunning(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "reconciler-stop.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	config := LiveReadinessReconcilerConfig{Interval: 1 * time.Hour}
	r := NewLiveReadinessReconciler(sqliteDB, store, &reconcilerTestLogger{}, config)

	// Should not panic or block
	r.Stop()
}

func TestLiveReadinessReconciler_DefaultConfig(t *testing.T) {
	defaults := DefaultLiveReadinessReconcilerConfig()
	assert.Equal(t, 1*time.Hour, defaults.Interval)
	assert.Equal(t, 7*24*time.Hour, defaults.LookbackWindow)
	assert.Equal(t, []string{"scalping", "daily_trading", "swing_trading", "arbitrage"}, defaults.Strategies)
}

func TestLiveReadinessReconciler_ConfigFallback(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "reconciler-cfg.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := NewLiveReadinessManifestStore(sqliteDB)
	config := LiveReadinessReconcilerConfig{}
	r := NewLiveReadinessReconciler(sqliteDB, store, &reconcilerTestLogger{}, config)
	assert.Equal(t, DefaultLiveReadinessReconcilerConfig().Interval, r.config.Interval)
	assert.Equal(t, DefaultLiveReadinessReconcilerConfig().LookbackWindow, r.config.LookbackWindow)
	assert.Equal(t, DefaultLiveReadinessReconcilerConfig().Strategies, r.config.Strategies)
}
