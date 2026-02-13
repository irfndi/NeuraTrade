package phase_management

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestPhase_String(t *testing.T) {
	tests := []struct {
		name     string
		phase    Phase
		expected string
	}{
		{"bootstrap", PhaseBootstrap, "bootstrap"},
		{"growth", PhaseGrowth, "growth"},
		{"scale", PhaseScale, "scale"},
		{"mature", PhaseMature, "mature"},
		{"unknown", Phase(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.phase.String())
		})
	}
}

func TestParsePhase(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expected    Phase
		expectError bool
	}{
		{"bootstrap", "bootstrap", PhaseBootstrap, false},
		{"growth", "growth", PhaseGrowth, false},
		{"scale", "scale", PhaseScale, false},
		{"mature", "mature", PhaseMature, false},
		{"unknown", "unknown", PhaseBootstrap, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phase, err := ParsePhase(tt.input)
			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, phase)
			}
		})
	}
}

func TestDefaultPhaseThresholds(t *testing.T) {
	thresholds := DefaultPhaseThresholds()

	assert.Equal(t, decimal.NewFromFloat(10000.0), thresholds.BootstrapMax)
	assert.Equal(t, decimal.NewFromFloat(50000.0), thresholds.GrowthMax)
	assert.Equal(t, decimal.NewFromFloat(200000.0), thresholds.ScaleMax)
}

func TestPhaseDetector_DetectPhase(t *testing.T) {
	config := DefaultPhaseDetectorConfig()
	detector := NewPhaseDetector(config, nil)

	tests := []struct {
		name     string
		value    decimal.Decimal
		expected Phase
	}{
		{"bootstrap phase - 5k", decimal.NewFromFloat(5000), PhaseBootstrap},
		{"bootstrap phase - 10k", decimal.NewFromFloat(10000), PhaseBootstrap},
		{"growth phase - 25k", decimal.NewFromFloat(25000), PhaseGrowth},
		{"growth phase - 50k", decimal.NewFromFloat(50000), PhaseGrowth},
		{"scale phase - 100k", decimal.NewFromFloat(100000), PhaseScale},
		{"scale phase - 200k", decimal.NewFromFloat(200000), PhaseScale},
		{"mature phase - 250k", decimal.NewFromFloat(250000), PhaseMature},
		{"mature phase - 1M", decimal.NewFromFloat(1000000), PhaseMature},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			phase := detector.DetectPhase(tt.value)
			assert.Equal(t, tt.expected, phase)
		})
	}
}

func TestPhaseDetector_ShouldTransition(t *testing.T) {
	config := DefaultPhaseDetectorConfig()
	config.MinPhaseDuration = 0
	detector := NewPhaseDetector(config, nil)

	tests := []struct {
		name           string
		currentPhase   Phase
		newPhase       Phase
		portfolioValue decimal.Decimal
		expected       bool
	}{
		{"no change", PhaseBootstrap, PhaseBootstrap, decimal.NewFromFloat(5000), false},
		{"bootstrap to growth - not enough", PhaseBootstrap, PhaseGrowth, decimal.NewFromFloat(10000), false},
		{"bootstrap to growth - enough", PhaseBootstrap, PhaseGrowth, decimal.NewFromFloat(10500), true},
		{"growth to bootstrap - too high", PhaseGrowth, PhaseBootstrap, decimal.NewFromFloat(10000), false},
		{"growth to bootstrap - low enough", PhaseGrowth, PhaseBootstrap, decimal.NewFromFloat(9500), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detector.ShouldTransition(tt.currentPhase, tt.newPhase, tt.portfolioValue)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPhaseDetector_AttemptTransition(t *testing.T) {
	config := DefaultPhaseDetectorConfig()
	config.MinPhaseDuration = 0
	detector := NewPhaseDetector(config, nil)

	event, transitioned := detector.AttemptTransition(decimal.NewFromFloat(5000), "test")
	assert.False(t, transitioned)
	assert.Empty(t, event.FromPhase)

	event, transitioned = detector.AttemptTransition(decimal.NewFromFloat(10500), "growth transition")
	assert.True(t, transitioned)
	assert.Equal(t, PhaseBootstrap, event.FromPhase)
	assert.Equal(t, PhaseGrowth, event.ToPhase)
	assert.Equal(t, "growth transition", event.Reason)
}

func TestPhaseDetector_RegisterTransitionHandler(t *testing.T) {
	config := DefaultPhaseDetectorConfig()
	config.MinPhaseDuration = 0
	detector := NewPhaseDetector(config, nil)

	handlerCalled := false
	detector.RegisterTransitionHandler(func(event PhaseTransitionEvent) {
		handlerCalled = true
	})

	detector.AttemptTransition(decimal.NewFromFloat(10500), "test")

	time.Sleep(100 * time.Millisecond)
	assert.True(t, handlerCalled)
}

func TestPhaseDetector_CurrentPhase(t *testing.T) {
	config := DefaultPhaseDetectorConfig()
	detector := NewPhaseDetector(config, nil)

	assert.Equal(t, PhaseBootstrap, detector.CurrentPhase())
}

func TestPhaseDetector_PhaseDuration(t *testing.T) {
	config := DefaultPhaseDetectorConfig()
	detector := NewPhaseDetector(config, nil)

	duration := detector.PhaseDuration()
	assert.Greater(t, duration, time.Duration(0))
}

func TestPhaseDetector_TransitionHistory(t *testing.T) {
	config := DefaultPhaseDetectorConfig()
	config.MinPhaseDuration = 0
	detector := NewPhaseDetector(config, nil)

	history := detector.TransitionHistory()
	assert.Empty(t, history)

	detector.AttemptTransition(decimal.NewFromFloat(10500), "test")

	history = detector.TransitionHistory()
	assert.Len(t, history, 1)
}

func TestPhaseDetector_SetPhase(t *testing.T) {
	config := DefaultPhaseDetectorConfig()
	detector := NewPhaseDetector(config, nil)

	err := detector.SetPhase(PhaseGrowth, "manual test")
	assert.NoError(t, err)
	assert.Equal(t, PhaseGrowth, detector.CurrentPhase())

	err = detector.SetPhase(Phase(-1), "invalid")
	assert.Error(t, err)
}

func TestPhaseDetector_GetPhaseForValue(t *testing.T) {
	config := DefaultPhaseDetectorConfig()
	detector := NewPhaseDetector(config, nil)

	phase := detector.GetPhaseForValue(decimal.NewFromFloat(25000))
	assert.Equal(t, PhaseGrowth, phase)
}
