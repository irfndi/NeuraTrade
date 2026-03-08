package services

import (
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
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
