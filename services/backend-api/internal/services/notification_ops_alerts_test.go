package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFormatSystemAlertMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	tests := []struct {
		name     string
		alert    SystemAlert
		contains []string
	}{
		{
			name: "info level",
			alert: SystemAlert{
				ID:        "alert-1",
				Level:     AlertLevelInfo,
				Source:    "test",
				Message:   "Info message",
				Timestamp: time.Now(),
			},
			contains: []string{"ℹ️", "INFO", "test", "Info message"},
		},
		{
			name: "warning level",
			alert: SystemAlert{
				ID:        "alert-2",
				Level:     AlertLevelWarning,
				Source:    "monitor",
				Message:   "Warning message",
				Timestamp: time.Now(),
			},
			contains: []string{"⚠️", "WARNING", "monitor", "Warning message"},
		},
		{
			name: "error level",
			alert: SystemAlert{
				ID:        "alert-3",
				Level:     AlertLevelError,
				Source:    "database",
				Message:   "Error message",
				Timestamp: time.Now(),
			},
			contains: []string{"🔴", "ERROR", "database", "Error message"},
		},
		{
			name: "critical level",
			alert: SystemAlert{
				ID:        "alert-4",
				Level:     AlertLevelCritical,
				Source:    "redis",
				Message:   "Critical message",
				Timestamp: time.Now(),
			},
			contains: []string{"🚨", "CRITICAL", "redis", "Critical message"},
		},
		{
			name: "unknown level defaults to warning emoji",
			alert: SystemAlert{
				ID:        "alert-5",
				Level:     AlertLevel("unknown"),
				Source:    "system",
				Message:   "Unknown level message",
				Timestamp: time.Now(),
			},
			contains: []string{"⚠️", "UNKNOWN", "system", "Unknown level message"},
		},
		{
			name: "with details",
			alert: SystemAlert{
				ID:      "alert-6",
				Level:   AlertLevelError,
				Source:  "database",
				Message: "Connection failed",
				Details: map[string]any{
					"host": "localhost",
					"port": 5432,
				},
				Timestamp: time.Now(),
			},
			contains: []string{"🔴", "Details:", "host: localhost", "port: 5432"},
		},
		{
			name: "without details",
			alert: SystemAlert{
				ID:        "alert-7",
				Level:     AlertLevelInfo,
				Source:    "test",
				Message:   "No details",
				Timestamp: time.Now(),
			},
			contains: []string{"ℹ️", "Source: test", "Message: No details"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := ns.formatSystemAlertMessage(tt.alert)
			for _, expected := range tt.contains {
				assert.Contains(t, message, expected)
			}
		})
	}
}

func TestFormatSystemAlertMessage_Timestamp(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	now := time.Now().UTC()
	alert := SystemAlert{
		ID:        "alert-ts",
		Level:     AlertLevelInfo,
		Source:    "test",
		Message:   "test",
		Timestamp: now,
	}

	message := ns.formatSystemAlertMessage(alert)
	assert.Contains(t, message, now.Format(time.RFC3339))
}

func TestFormatSystemAlertMessage_DetailsSorting(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	alert := SystemAlert{
		ID:      "alert-sort",
		Level:   AlertLevelWarning,
		Source:  "test",
		Message: "test",
		Details: map[string]any{
			"zebra":  "last",
			"alpha":  "first",
			"middle": "mid",
		},
		Timestamp: time.Now(),
	}

	message := ns.formatSystemAlertMessage(alert)
	alphaIdx := strings.Index(message, "alpha: first")
	middleIdx := strings.Index(message, "middle: mid")
	zebraIdx := strings.Index(message, "zebra: last")
	require.NotEqual(t, -1, alphaIdx, "alpha: first not found in message")
	require.NotEqual(t, -1, middleIdx, "middle: mid not found in message")
	require.NotEqual(t, -1, zebraIdx, "zebra: last not found in message")
	assert.Less(t, alphaIdx, middleIdx)
	assert.Less(t, middleIdx, zebraIdx)
}

func TestNotifySystemAlert(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")
	ctx := context.Background()

	alert := SystemAlert{
		ID:        "alert-notify",
		Level:     AlertLevelError,
		Source:    "test",
		Message:   "Test alert",
		Timestamp: time.Now(),
	}

	err := ns.NotifySystemAlert(ctx, 12345, alert)
	assert.Error(t, err)
}

func TestNotifyMonitoringAlert(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")
	ctx := context.Background()

	err := ns.NotifyMonitoringAlert(ctx, 12345, "Monitoring alert message")
	assert.Error(t, err)
}

func TestBroadcastSystemAlert_NilDB(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")
	ctx := context.Background()

	alert := SystemAlert{
		ID:        "alert-broadcast",
		Level:     AlertLevelCritical,
		Source:    "test",
		Message:   "Broadcast alert",
		Timestamp: time.Now(),
	}

	err := ns.BroadcastSystemAlert(ctx, alert)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database not available")
}

func TestFormatMonitoringAlertMessage(t *testing.T) {
	result := formatMonitoringAlertMessage("drawdown exceeded threshold")
	assert.Contains(t, result, "🚨 AUTONOMOUS MONITORING ALERT")
	assert.Contains(t, result, "drawdown exceeded threshold")
	assert.Contains(t, result, "Time:")
}
