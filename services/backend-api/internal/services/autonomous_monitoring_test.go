package services

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestAutonomousMonitoring_SendAlert_DispatchesViaNotificationService(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewDecoder(r.Body).Decode(&receivedBody)
		w.Write([]byte(`{"ok":true,"message_id":"1"}`))
	}))
	defer server.Close()

	ns := NewNotificationService(nil, nil, server.URL, "", "")
	monitor := NewAutonomousMonitoring("123456", ns)

	monitor.sendAlert("Drawdown breached: 20%")

	assert.NotNil(t, receivedBody)
	assert.Equal(t, "123456", receivedBody["chatId"])
	assert.Contains(t, receivedBody["text"].(string), "autonomous_monitoring")
	assert.Contains(t, receivedBody["text"].(string), "Drawdown breached")
}

func TestAutonomousMonitoring_SendAlert_NilNotificationService(t *testing.T) {
	monitor := NewAutonomousMonitoring("123456", nil)

	assert.NotPanics(t, func() {
		monitor.sendAlert("test alert")
	})
}

func TestAutonomousMonitoring_SendAlert_InvalidChatID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	ns := NewNotificationService(nil, nil, server.URL, "", "")
	monitor := NewAutonomousMonitoring("not-a-number", ns)

	assert.NotPanics(t, func() {
		monitor.sendAlert("test alert")
	})
}

func TestAutonomousMonitoring_SendAlert_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"ok":false,"error":"fail","errorCode":"INTERNAL_ERROR"}`))
	}))
	defer server.Close()

	ns := NewNotificationService(nil, nil, server.URL, "", "")
	monitor := NewAutonomousMonitoring("123456", ns)

	assert.NotPanics(t, func() {
		monitor.sendAlert("error scenario")
	})
}

func TestAutonomousMonitoring_RecordQuestDispatchesOnThresholdBreach(t *testing.T) {
	dispatchCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dispatchCount++
		w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	ns := NewNotificationService(nil, nil, server.URL, "", "")
	monitor := NewAutonomousMonitoring("123456", ns)
	monitor.alertThresholds.MaxDrawdownPercent = 0.10
	monitor.alertThresholds.MinWinRate = 0
	monitor.alertThresholds.MaxConsecutiveLosses = 99

	monitor.RecordQuestExecution(false, decimal.NewFromFloat(-0.15))

	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, dispatchCount)
}
