package services

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestAutonomousMonitoring_RecordQuestExecution_DoesNotDeadlock(t *testing.T) {
	monitor := NewAutonomousMonitoring("chat-1", nil)

	done := make(chan struct{})
	go func() {
		monitor.RecordQuestExecution(true, decimal.Zero)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("RecordQuestExecution deadlocked")
	}

	snapshot := monitor.GetSnapshot()
	if snapshot.TotalQuests != 1 {
		t.Fatalf("expected 1 recorded quest, got %d", snapshot.TotalQuests)
	}
	if snapshot.SuccessRate != 1 {
		t.Fatalf("expected success rate 1, got %f", snapshot.SuccessRate)
	}
}

func TestAutonomousMonitoring_ComputeAlertsLocked_DrawdownThreshold(t *testing.T) {
	monitor := NewAutonomousMonitoring("chat-1", nil)
	monitor.alertThresholds.MaxDrawdownPercent = 0.10
	monitor.alertThresholds.MinWinRate = 0
	monitor.alertThresholds.MaxConsecutiveLosses = 99

	monitor.RecordQuestExecution(false, decimal.NewFromFloat(-0.15))

	monitor.mu.Lock()
	alerts := monitor.computeAlertsLocked()
	monitor.mu.Unlock()

	found := false
	for _, alert := range alerts {
		if strings.Contains(alert, "Max drawdown breached") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected drawdown alert, got %#v", alerts)
	}
}

func TestAutonomousMonitoring_SendAlert(t *testing.T) {
	t.Run("nil notification service does not panic", func(t *testing.T) {
		monitor := NewAutonomousMonitoring("12345", nil)
		assert.NotPanics(t, func() {
			monitor.sendAlert("test alert message")
		})
	})

	t.Run("invalid chatID logs error and does not panic", func(t *testing.T) {
		ns := NewNotificationService(nil, nil, "", "", "")
		monitor := NewAutonomousMonitoring("not-a-number", ns)
		assert.NotPanics(t, func() {
			monitor.sendAlert("test alert message")
		})
	})

	t.Run("valid chatID with nil deps does not panic", func(t *testing.T) {
		ns := NewNotificationService(nil, nil, "", "", "")
		monitor := NewAutonomousMonitoring("12345", ns)
		assert.NotPanics(t, func() {
			monitor.sendAlert("test alert message")
		})
	})
}

func TestAutonomousMonitoring_ComputeAlertsLocked_LowWinRateThreshold(t *testing.T) {
	monitor := NewAutonomousMonitoring("chat-2", nil)
	monitor.alertThresholds.MaxDrawdownPercent = 1.0
	monitor.alertThresholds.MinWinRate = 0.50
	monitor.alertThresholds.MaxConsecutiveLosses = 99

	monitor.RecordTrade(false, decimal.NewFromFloat(-0.01))
	monitor.RecordTrade(false, decimal.NewFromFloat(-0.01))
	monitor.RecordTrade(false, decimal.NewFromFloat(-0.01))
	monitor.RecordTrade(false, decimal.NewFromFloat(-0.01))
	monitor.RecordTrade(false, decimal.NewFromFloat(-0.01))
	monitor.RecordTrade(true, decimal.NewFromFloat(0.01))

	monitor.mu.Lock()
	alerts := monitor.computeAlertsLocked()
	monitor.mu.Unlock()

	found := false
	for _, alert := range alerts {
		if strings.Contains(alert, "Win rate below threshold") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected low win-rate alert, got %#v", alerts)
	}
}
