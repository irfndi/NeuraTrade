package db

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const positionsSchema = `
CREATE TABLE positions (
	id TEXT PRIMARY KEY,
	exchange TEXT NOT NULL,
	symbol TEXT NOT NULL,
	side TEXT NOT NULL,
	amount NUMERIC NOT NULL,
	entry_price NUMERIC NOT NULL,
	current_price NUMERIC NOT NULL,
	unrealized_pnl NUMERIC NOT NULL,
	realized_pnl NUMERIC NOT NULL,
	opened_at TIMESTAMP NOT NULL,
	updated_at TIMESTAMP NOT NULL,
	closed_at TIMESTAMP NULL,
	status TEXT NOT NULL,
	strategy_id TEXT NOT NULL,
	metadata BLOB NOT NULL
);`

const riskConfigSchema = `
CREATE TABLE risk_config (
	id INTEGER PRIMARY KEY,
	max_position_size NUMERIC NOT NULL,
	max_daily_loss NUMERIC NOT NULL,
	max_drawdown NUMERIC NOT NULL,
	max_leverage NUMERIC NOT NULL,
	allowed_symbols BLOB NOT NULL,
	allowed_exchanges BLOB NOT NULL,
	safe_mode BOOLEAN NOT NULL,
	kill_switch BOOLEAN NOT NULL
);`

const featureFlagsSchema = `
CREATE TABLE feature_flags (
	key TEXT PRIMARY KEY,
	value BOOLEAN NOT NULL,
	updated_at TIMESTAMP NOT NULL
);`

func newSQLiteAdapter(t *testing.T, schema ...string) *Adapter {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "adapter.db")
	dbConn, err := database.NewSQLiteConnection(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = dbConn.Close()
	})

	for _, stmt := range schema {
		_, err := dbConn.Exec(context.Background(), stmt)
		require.NoError(t, err)
	}

	return NewAdapter(dbConn)
}

func TestAdapter_HealthWithNilDB(t *testing.T) {
	adapter := NewAdapter(nil)

	err := adapter.Health(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database connection is nil")
}

func TestPositionsRepo_CreateAndGetByID_Smoke(t *testing.T) {
	adapter := newSQLiteAdapter(t, positionsSchema)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	position := ports.StoredPosition{
		ID:            "pos-1",
		Exchange:      "binance",
		Symbol:        "BTC/USDT",
		Side:          "long",
		Amount:        decimal.NewFromFloat(0.5),
		EntryPrice:    decimal.NewFromInt(100000),
		CurrentPrice:  decimal.NewFromInt(101500),
		UnrealizedPnL: decimal.NewFromInt(750),
		RealizedPnL:   decimal.Zero,
		OpenedAt:      now,
		UpdatedAt:     now,
		Status:        "open",
		StrategyID:    "strat-1",
		Metadata: map[string]any{
			"source": "smoke",
		},
	}

	created, err := adapter.Positions().Create(ctx, position)
	require.NoError(t, err)
	assert.Equal(t, position.ID, created.ID)

	stored, err := adapter.Positions().GetByID(ctx, position.ID)
	require.NoError(t, err)
	assert.Equal(t, position.ID, stored.ID)
	assert.Equal(t, position.Exchange, stored.Exchange)
	assert.True(t, position.Amount.Equal(stored.Amount))
	assert.Equal(t, "smoke", stored.Metadata["source"])
}

func TestConfigRepo_GetRiskConfig_DefaultAndErrorMapping(t *testing.T) {
	t.Run("returns defaults on sql.ErrNoRows", func(t *testing.T) {
		adapter := newSQLiteAdapter(t, riskConfigSchema)

		cfg, err := adapter.Config().GetRiskConfig(context.Background())
		require.NoError(t, err)
		assert.Equal(t, defaultRiskConfig(), cfg)
	})

	t.Run("wraps database errors that are not sql.ErrNoRows", func(t *testing.T) {
		adapter := newSQLiteAdapter(t)

		_, err := adapter.Config().GetRiskConfig(context.Background())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get risk config")
	})

	t.Run("persists and reloads config", func(t *testing.T) {
		adapter := newSQLiteAdapter(t, riskConfigSchema)
		expected := ports.RiskConfig{
			MaxPositionSize:  decimal.RequireFromString("0.25"),
			MaxDailyLoss:     decimal.RequireFromString("0.05"),
			MaxDrawdown:      decimal.RequireFromString("0.10"),
			MaxLeverage:      decimal.NewFromInt(3),
			AllowedSymbols:   []string{"BTC/USDT", "ETH/USDT"},
			AllowedExchanges: []string{"binance", "bitget"},
			SafeMode:         true,
			KillSwitch:       false,
		}

		err := adapter.Config().UpdateRiskConfig(context.Background(), expected)
		require.NoError(t, err)

		actual, err := adapter.Config().GetRiskConfig(context.Background())
		require.NoError(t, err)
		assert.True(t, expected.MaxPositionSize.Equal(actual.MaxPositionSize))
		assert.True(t, expected.MaxDailyLoss.Equal(actual.MaxDailyLoss))
		assert.True(t, expected.MaxDrawdown.Equal(actual.MaxDrawdown))
		assert.True(t, expected.MaxLeverage.Equal(actual.MaxLeverage))
		assert.Equal(t, expected.AllowedSymbols, actual.AllowedSymbols)
		assert.Equal(t, expected.AllowedExchanges, actual.AllowedExchanges)
		assert.Equal(t, expected.SafeMode, actual.SafeMode)
		assert.Equal(t, expected.KillSwitch, actual.KillSwitch)
	})
}

func TestConfigRepo_FeatureFlag_DefaultAndErrorMapping(t *testing.T) {
	t.Run("returns false when flag does not exist", func(t *testing.T) {
		adapter := newSQLiteAdapter(t, featureFlagsSchema)

		enabled, err := adapter.Config().GetFeatureFlag(context.Background(), "new-ui")
		require.NoError(t, err)
		assert.False(t, enabled)
	})

	t.Run("wraps database errors when table is missing", func(t *testing.T) {
		adapter := newSQLiteAdapter(t)

		_, err := adapter.Config().GetFeatureFlag(context.Background(), "new-ui")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to get feature flag")
	})

	t.Run("persists and reloads feature flag", func(t *testing.T) {
		adapter := newSQLiteAdapter(t, featureFlagsSchema)
		ctx := context.Background()

		err := adapter.Config().SetFeatureFlag(ctx, "new-ui", true)
		require.NoError(t, err)

		enabled, err := adapter.Config().GetFeatureFlag(ctx, "new-ui")
		require.NoError(t, err)
		assert.True(t, enabled)
	})
}
