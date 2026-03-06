package services

import (
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
