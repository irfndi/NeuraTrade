package services

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewRuntimeOptimizer(t *testing.T) {
	config := DefaultRuntimeConfig()
	optimizer := NewRuntimeOptimizer(config)

	assert.NotNil(t, optimizer)
	assert.Equal(t, config, optimizer.config)
}

func TestDefaultRuntimeConfig(t *testing.T) {
	config := DefaultRuntimeConfig()

	assert.Equal(t, 0, config.MaxProcs)
	assert.Equal(t, 100, config.GCPercent)
	assert.Equal(t, int64(0), config.MemoryLimit)
	assert.False(t, config.TraceEnabled)
	assert.Equal(t, 0, config.BlockProfileRate)
	assert.Equal(t, 0, config.MutexProfileRate)
}

func TestRuntimeOptimizer_Apply(t *testing.T) {
	originalMaxProcs := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(originalMaxProcs)

	config := RuntimeConfig{
		MaxProcs:    0,
		GCPercent:   100,
		MemoryLimit: 0,
	}

	optimizer := NewRuntimeOptimizer(config)
	assert.NotPanics(t, func() {
		optimizer.Apply()
	})
}

func TestRuntimeOptimizer_GetRuntimeStats(t *testing.T) {
	config := DefaultRuntimeConfig()
	optimizer := NewRuntimeOptimizer(config)

	stats := optimizer.GetRuntimeStats()

	assert.Equal(t, runtime.GOMAXPROCS(0), stats.GOMAXPROCS)
	assert.Equal(t, runtime.NumCPU(), stats.NumCPU)
	assert.Equal(t, runtime.Version(), stats.GoVersion)
	assert.GreaterOrEqual(t, stats.NumGoroutine, 1)
}

func TestGetMemoryPressure(t *testing.T) {
	pressure := GetMemoryPressure()

	assert.GreaterOrEqual(t, pressure, 0.0)
	assert.LessOrEqual(t, pressure, 1.0)
}

func TestGetRecommendedWorkerCount(t *testing.T) {
	count := GetRecommendedWorkerCount()

	assert.GreaterOrEqual(t, count, 1)
	assert.LessOrEqual(t, count, 32)
}

func TestForceGC(t *testing.T) {
	assert.NotPanics(t, func() {
		ForceGC()
	})
}

func TestMin(t *testing.T) {
	assert.Equal(t, 3, min(3, 5))
	assert.Equal(t, 2, min(5, 2))
	assert.Equal(t, 5, min(5, 5))
}

func TestMax(t *testing.T) {
	assert.Equal(t, 5, max(3, 5))
	assert.Equal(t, 5, max(5, 2))
	assert.Equal(t, 5, max(5, 5))
}
