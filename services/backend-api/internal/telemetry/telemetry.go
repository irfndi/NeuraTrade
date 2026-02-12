package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/getsentry/sentry-go"
)

const (
	// ServiceName is the identifier for the application service.
	ServiceName = "github.com/irfndi/neuratrade"
	// ServiceVersion indicates the current version of the service.
	ServiceVersion = "1.0.0"
)

// TelemetryConfig holds configuration settings for the telemetry system (Sentry).
type TelemetryConfig struct {
	Enabled     bool
	DSN         string
	Environment string
	Release     string
	SampleRate  float64
}

// DefaultConfig returns a TelemetryConfig with default settings.
func DefaultConfig() *TelemetryConfig {
	return &TelemetryConfig{
		Enabled:     true,
		DSN:         "", // Should be provided via env
		Environment: "development",
		Release:     ServiceVersion,
		SampleRate:  0.2,
	}
}

// InitTelemetry initializes the Sentry client with the provided configuration.
//
// Parameters:
//   - config: The configuration for telemetry.
//
// Returns:
//   - An error if initialization fails.
func InitTelemetry(config TelemetryConfig) error {
	if !config.Enabled {
		return nil
	}

	err := sentry.Init(sentry.ClientOptions{
		Dsn:              config.DSN,
		Environment:      config.Environment,
		Release:          config.Release,
		TracesSampleRate: config.SampleRate,
		AttachStacktrace: true,
	})
	if err != nil {
		return fmt.Errorf("sentry init: %w", err)
	}

	return nil
}

// Flush sends any buffered events to the telemetry backend, blocking up to the timeout duration.
//
// Parameters:
//   - timeout: The maximum time to wait for events to be sent.
func Flush(timeout time.Duration) {
	sentry.Flush(timeout)
}

// Provider represents a telemetry service provider interface.
type Provider struct {
	Shutdown func(context.Context) error
}

// Shutdown safely shuts down the global telemetry provider, flushing remaining events.
//
// Returns:
//   - An error (currently always nil).
func Shutdown() error {
	sentry.Flush(2 * time.Second)
	return nil
}

// Logger returns the global slog.Logger instance for structured logging in the application.
func Logger() *slog.Logger {
	return slog.Default()
}

// GetLogger is an alias for Logger, maintained for backward compatibility.
func GetLogger() *slog.Logger {
	return Logger()
}
