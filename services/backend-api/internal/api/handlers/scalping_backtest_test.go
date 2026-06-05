package handlers

import (
	"database/sql"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseToServiceConfig_UsesSpreadMultiplier(t *testing.T) {
	spreadMultiplier := 12.5
	feeRate := "0.002"
	req := RunScalpingBacktestRequest{
		StartTime:          time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		EndTime:            time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Symbols:            []string{"btc/usdt", "BTC/USDT"},
		Exchange:           "Bybit",
		InitialCapital:     "25000",
		MinConfidence:      floatPtr(0.72),
		MaxBidAskSpreadPct: floatPtr(0.09),
		SpreadMultiplier:   &spreadMultiplier,
		FeeRate:            &feeRate,
	}

	cfg, err := parseToServiceConfig(req)
	require.NoError(t, err)
	assert.Equal(t, "bybit", cfg.Exchange)
	assert.Equal(t, 0.72, cfg.MinConfidence)
	assert.Equal(t, 0.09, cfg.MaxBidAskSpreadPct)
	assert.Equal(t, spreadMultiplier, cfg.SpreadMultiplier)
	assert.Equal(t, 0.002, cfg.FeeRate.InexactFloat64())
	assert.Equal(t, services.DefaultScalpingBacktestSlippage, cfg.SlippagePct.InexactFloat64())
	assert.Equal(t, []string{"BTC/USDT"}, cfg.Symbols)
}

func TestDecodeScalpingBacktestRow_DefaultsZeroSpreadMultiplier(t *testing.T) {
	configRaw := []byte(`{"spread_multiplier":0}`)
	run, err := decodeScalpingBacktestRow("run-1", "completed", configRaw, nil, time.Unix(0, 0).UTC(), sql.NullTime{})
	require.NoError(t, err)
	assert.Equal(t, float64(services.DefaultScalpingBacktestSpreadMultiplier), run.Config.SpreadMultiplier)
}

func TestParseToServiceConfig_RejectsNonPositiveSpreadMultiplier(t *testing.T) {
	spreadMultiplier := 0.0
	req := RunScalpingBacktestRequest{
		StartTime:        time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		EndTime:          time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		SpreadMultiplier: &spreadMultiplier,
	}

	_, err := parseToServiceConfig(req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "spread_multiplier must be greater than zero")
}

func TestParseToServiceConfig_Accepts5YearRange(t *testing.T) {
	req := RunScalpingBacktestRequest{
		StartTime:      time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		EndTime:        time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		InitialCapital: "10000",
	}
	cfg, err := parseToServiceConfig(req)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC), cfg.StartTime)
	assert.Equal(t, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), cfg.EndTime)
}

func TestParseToServiceConfig_EnvVarOverrideEmptyReq(t *testing.T) {
	t.Setenv("NEURATRADE_BACKTEST_SYMBOLS", "BTC/USDT,ETH/USDT")
	req := RunScalpingBacktestRequest{
		StartTime:      time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		EndTime:        time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		InitialCapital: "10000",
	}
	cfg, err := parseToServiceConfig(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"BTC/USDT", "ETH/USDT"}, cfg.Symbols)
}

func TestParseToServiceConfig_ReqSymbolsOverrideEnvVar(t *testing.T) {
	t.Setenv("NEURATRADE_BACKTEST_SYMBOLS", "BTC/USDT")
	req := RunScalpingBacktestRequest{
		StartTime:      time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		EndTime:        time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
		Symbols:        []string{"ETH/USDT"},
		InitialCapital: "10000",
	}
	cfg, err := parseToServiceConfig(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"ETH/USDT"}, cfg.Symbols)
}

func floatPtr(v float64) *float64 {
	return &v
}
