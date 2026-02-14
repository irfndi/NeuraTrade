package services

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAlertLevel_Constants(t *testing.T) {
	assert.Equal(t, AlertLevel("info"), AlertLevelInfo)
	assert.Equal(t, AlertLevel("warning"), AlertLevelWarning)
	assert.Equal(t, AlertLevel("error"), AlertLevelError)
	assert.Equal(t, AlertLevel("critical"), AlertLevelCritical)
}

func TestNewAlertService(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)
	assert.NotNil(t, alertService)
	assert.NotNil(t, alertService.handlers)
	assert.NotNil(t, alertService.alertThrottler)
}

func TestNewAlertThrottler(t *testing.T) {
	throttler := NewAlertThrottler(5 * time.Minute)
	assert.NotNil(t, throttler)
	assert.Equal(t, 5*time.Minute, throttler.cooldown)
}

func TestAlertThrottler_ShouldSend(t *testing.T) {
	throttler := NewAlertThrottler(100 * time.Millisecond)

	key := "test-alert"

	assert.True(t, throttler.ShouldSend(key))
	assert.False(t, throttler.ShouldSend(key))

	time.Sleep(150 * time.Millisecond)
	assert.True(t, throttler.ShouldSend(key))
}

func TestAlertThrottler_DifferentKeys(t *testing.T) {
	throttler := NewAlertThrottler(5 * time.Minute)

	assert.True(t, throttler.ShouldSend("alert-1"))
	assert.True(t, throttler.ShouldSend("alert-2"))
	assert.True(t, throttler.ShouldSend("alert-3"))
}

func TestSystemAlert_Struct(t *testing.T) {
	alert := SystemAlert{
		ID:        "alert-123",
		Level:     AlertLevelCritical,
		Source:    "test",
		Message:   "Test message",
		Details:   map[string]any{"key": "value"},
		Timestamp: time.Now(),
		Resolved:  false,
	}

	assert.Equal(t, "alert-123", alert.ID)
	assert.Equal(t, AlertLevelCritical, alert.Level)
	assert.Equal(t, "test", alert.Source)
	assert.Equal(t, "Test message", alert.Message)
	assert.Equal(t, false, alert.Resolved)
}

func TestAlertService_SendAlert(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)

	ctx := context.Background()

	err := alertService.SendAlert(ctx, AlertLevelInfo, "test-source", "Test message", nil)
	assert.NoError(t, err)
}

func TestAlertService_SendAlertWithDetails(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)

	ctx := context.Background()

	details := map[string]any{
		"error": "connection failed",
		"host":  "localhost",
		"port":  5432,
	}

	err := alertService.SendAlert(ctx, AlertLevelError, "database", "Connection failed", details)
	assert.NoError(t, err)
}

func TestAlertService_RegisterHandler(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)

	handlerCalled := false
	handler := func(ctx context.Context, alert SystemAlert) error {
		handlerCalled = true
		return nil
	}

	alertService.RegisterHandler(handler)

	ctx := context.Background()
	alertService.SendAlert(ctx, AlertLevelInfo, "test", "Test", nil)

	assert.True(t, handlerCalled)
}

func TestAlertService_RegisterHandlerError(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)

	handler := func(ctx context.Context, alert SystemAlert) error {
		return assert.AnError
	}

	alertService.RegisterHandler(handler)

	ctx := context.Background()

	err := alertService.SendAlert(ctx, AlertLevelInfo, "test", "Test", nil)
	assert.NoError(t, err)
}

func TestAlertService_SetNotificationService(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)
	notificationService := NewNotificationService(nil, nil, "", "", "")

	alertService.SetNotificationService(notificationService)
}

func TestAlertService_Throttling(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)
	alertService.alertThrottler = NewAlertThrottler(1 * time.Second)

	ctx := context.Background()

	for i := 0; i < 5; i++ {
		err := alertService.SendAlert(ctx, AlertLevelWarning, "test", "Throttled message", nil)
		assert.NoError(t, err)
	}
}

func TestAlertService_ResolveAlert(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)

	ctx := context.Background()

	err := alertService.ResolveAlert(ctx, "alert-123")
	assert.NoError(t, err)
}

func TestAlertService_ResolveAlertNilRedis(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)
	alertService.redis = nil

	ctx := context.Background()

	err := alertService.ResolveAlert(ctx, "alert-123")
	assert.NoError(t, err)
}

func TestAlertService_CheckRedisHealthNilRedis(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)

	ctx := context.Background()

	err := alertService.CheckRedisHealth(ctx)
	assert.NoError(t, err)
}

func TestAlertService_CheckDatabaseHealthNilDB(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)

	ctx := context.Background()

	err := alertService.CheckDatabaseHealth(ctx)
	assert.NoError(t, err)
}

func TestAlertService_RunHealthCheck(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)

	ctx := context.Background()

	config := HealthAlertConfig{
		RedisDown:          false,
		DatabaseDown:       false,
		HighLatency:        false,
		LatencyThresholdMs: 100,
	}

	alertService.RunHealthCheck(ctx, config)
}

func TestAlertService_RunHealthCheckWithAlerts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	alertService := NewAlertService(nil, nil, logger)

	ctx := context.Background()

	config := HealthAlertConfig{
		RedisDown:    true,
		DatabaseDown: true,
	}

	alertService.RunHealthCheck(ctx, config)
}
