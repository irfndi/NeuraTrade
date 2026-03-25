package handlers

import (
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

func floatPtr(v float64) *float64 {
	return &v
}
