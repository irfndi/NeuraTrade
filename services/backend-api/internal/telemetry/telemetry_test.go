package telemetry

import (
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	assert.NotNil(t, config)
	assert.True(t, config.Enabled)
	assert.Equal(t, "", config.DSN)
	assert.Equal(t, "development", config.Environment)
	assert.Equal(t, ServiceVersion, config.Release)
	assert.Equal(t, 0.2, config.SampleRate)
}

func TestInitTelemetryDisabled(t *testing.T) {
	config := TelemetryConfig{
		Enabled: false,
	}

	err := InitTelemetry(config)
	assert.NoError(t, err)
}

func TestInitTelemetryEnabled(t *testing.T) {
	config := TelemetryConfig{
		Enabled:     true,
		DSN:         "",
		Environment: "test",
		Release:     "1.0.0",
		SampleRate:  1.0,
	}

	err := InitTelemetry(config)
	assert.NoError(t, err)
}

func TestShutdown(t *testing.T) {
	err := Shutdown()
	assert.NoError(t, err)
}

func TestLogger(t *testing.T) {
	logger := Logger()
	assert.NotNil(t, logger)
	assert.Equal(t, slog.Default(), logger)
}

func TestGetLogger(t *testing.T) {
	logger := GetLogger()
	assert.NotNil(t, logger)
	assert.Equal(t, slog.Default(), logger)
}

func TestFlush(t *testing.T) {
	Flush(100 * time.Millisecond)
}
