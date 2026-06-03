package services

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestAnalyzeStochasticSignal_EmptyInput(t *testing.T) {
	tas := &TechnicalAnalysisService{}
	sig, strength := tas.analyzeStochasticSignal(nil, nil)
	require.Equal(t, "hold", sig)
	require.True(t, strength.Equal(decimal.NewFromFloat(0.5)))
}

func TestAnalyzeStochasticSignal_OversoldLevel(t *testing.T) {
	tas := &TechnicalAnalysisService{}
	k := []float64{15.0, 14.0, 12.0}
	d := []float64{20.0, 18.0, 16.0}
	sig, strength := tas.analyzeStochasticSignal(k, d)
	require.Equal(t, "buy", sig)
	require.True(t, strength.Equal(decimal.NewFromFloat(0.75)))
}

func TestAnalyzeStochasticSignal_OverboughtLevel(t *testing.T) {
	tas := &TechnicalAnalysisService{}
	k := []float64{85.0, 86.0, 88.0}
	d := []float64{80.0, 82.0, 84.0}
	sig, strength := tas.analyzeStochasticSignal(k, d)
	require.Equal(t, "sell", sig)
	require.True(t, strength.Equal(decimal.NewFromFloat(0.75)))
}

func TestAnalyzeStochasticSignal_BullishCrossoverInOversold(t *testing.T) {
	tas := &TechnicalAnalysisService{}
	k := []float64{10.0, 12.0, 18.0}
	d := []float64{20.0, 15.0, 12.0}
	sig, strength := tas.analyzeStochasticSignal(k, d)
	require.Equal(t, "buy", sig)
	require.True(t, strength.Equal(decimal.NewFromFloat(0.85)))
}

func TestAnalyzeStochasticSignal_BearishCrossoverInOverbought(t *testing.T) {
	tas := &TechnicalAnalysisService{}
	k := []float64{90.0, 88.0, 82.0}
	d := []float64{80.0, 82.0, 84.0}
	sig, strength := tas.analyzeStochasticSignal(k, d)
	require.Equal(t, "sell", sig)
	require.True(t, strength.Equal(decimal.NewFromFloat(0.85)))
}

func TestAnalyzeStochasticSignal_NeutralRange(t *testing.T) {
	tas := &TechnicalAnalysisService{}
	k := []float64{40.0, 45.0, 50.0}
	d := []float64{42.0, 44.0, 48.0}
	sig, strength := tas.analyzeStochasticSignal(k, d)
	require.Equal(t, "hold", sig)
	require.True(t, strength.Equal(decimal.NewFromFloat(0.5)))
}
