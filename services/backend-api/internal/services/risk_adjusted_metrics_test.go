package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDownsideStdDevFloat64_SingleNegativeSample(t *testing.T) {
	downside := downsideStdDevFloat64([]float64{-0.02})
	assert.InDelta(t, 0.02, downside, 1e-12)
}

func TestComputeRiskAdjustedMetrics_SortinoWithSingleLoss(t *testing.T) {
	metrics := ComputeRiskAdjustedMetrics([]float64{0.02, -0.01, 0.01})
	assert.Greater(t, metrics.Sortino, 0.0)
	assert.Equal(t, 3, metrics.SampleSize)
}
