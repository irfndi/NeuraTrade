package services

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHeartbeatConfig_Defaults(t *testing.T) {
	config := DefaultHeartbeatConfig()

	assert.Equal(t, 30*time.Minute, config.DefaultInterval)
	assert.Equal(t, 5*time.Minute, config.PositionCheckInterval)
	assert.Equal(t, 1*time.Minute, config.StopLossUpdateInterval)
	assert.Equal(t, 30*time.Second, config.SignalScanInterval)
	assert.Equal(t, 60*time.Minute, config.FundingRateCheckInterval)
	assert.Equal(t, 1*time.Minute, config.ConnectivityCheckInterval)
	assert.Equal(t, 10*time.Minute, config.PolymarketOddsInterval)
	assert.Equal(t, 5*time.Minute, config.CheckpointInterval)
	assert.True(t, config.Enabled)
}

func TestTradingHeartbeat_New(t *testing.T) {
	config := DefaultHeartbeatConfig()

	heartbeat := NewTradingHeartbeat(
		config,
		&mockPositionTracker{},
		&mockStopLossService{},
		&mockSignalProcessor{},
		&mockFundingCollector{},
		&mockConnectivityChecker{},
		nil,
		&mockRiskManager{},
		nil,
	)

	require.NotNil(t, heartbeat)
	assert.False(t, heartbeat.IsRunning())
	assert.Len(t, heartbeat.tasks, 6)
}

func TestTradingHeartbeat_RegisterTasks(t *testing.T) {
	config := DefaultHeartbeatConfig()
	config.PositionCheckInterval = 1 * time.Second

	heartbeat := NewTradingHeartbeat(
		config,
		&mockPositionTracker{},
		&mockStopLossService{},
		&mockSignalProcessor{},
		&mockFundingCollector{},
		&mockConnectivityChecker{},
		nil,
		&mockRiskManager{},
		nil,
	)

	tasks := heartbeat.tasks
	require.NotNil(t, tasks)

	assert.Contains(t, tasks, "position_check")
	assert.Contains(t, tasks, "stop_loss_update")
	assert.Contains(t, tasks, "signal_scan")
	assert.Contains(t, tasks, "funding_check")
	assert.Contains(t, tasks, "connectivity_check")
	assert.Contains(t, tasks, "checkpoint")

	posTask := tasks["position_check"]
	assert.Equal(t, "Position Check", posTask.Name)
	assert.Equal(t, 1*time.Second, posTask.Interval)
	assert.True(t, posTask.Enabled)
}

func TestTradingHeartbeat_StartStop(t *testing.T) {
	config := DefaultHeartbeatConfig()

	heartbeat := NewTradingHeartbeat(
		config,
		&mockPositionTracker{},
		&mockStopLossService{},
		&mockSignalProcessor{},
		&mockFundingCollector{},
		&mockConnectivityChecker{},
		nil,
		&mockRiskManager{},
		nil,
	)

	err := heartbeat.Start(context.Background())
	require.NoError(t, err)
	assert.True(t, heartbeat.IsRunning())

	heartbeat.Stop()
	assert.False(t, heartbeat.IsRunning())
}

func TestTradingHeartbeat_StartWhenRunning(t *testing.T) {
	heartbeat := NewTradingHeartbeat(
		DefaultHeartbeatConfig(),
		&mockPositionTracker{},
		&mockStopLossService{},
		&mockSignalProcessor{},
		&mockFundingCollector{},
		&mockConnectivityChecker{},
		nil,
		&mockRiskManager{},
		nil,
	)

	heartbeat.Start(context.Background())
	defer heartbeat.Stop()

	err := heartbeat.Start(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")
}

func TestTradingHeartbeat_StopWhenNotRunning(t *testing.T) {
	heartbeat := NewTradingHeartbeat(
		DefaultHeartbeatConfig(),
		&mockPositionTracker{},
		&mockStopLossService{},
		&mockSignalProcessor{},
		&mockFundingCollector{},
		&mockConnectivityChecker{},
		nil,
		&mockRiskManager{},
		nil,
	)

	heartbeat.Stop()
	assert.False(t, heartbeat.IsRunning())
}

func TestTradingHeartbeat_GetTaskStatus(t *testing.T) {
	config := DefaultHeartbeatConfig()
	heartbeat := NewTradingHeartbeat(
		config,
		&mockPositionTracker{},
		&mockStopLossService{},
		&mockSignalProcessor{},
		&mockFundingCollector{},
		&mockConnectivityChecker{},
		nil,
		&mockRiskManager{},
		nil,
	)

	status := heartbeat.GetTaskStatus()
	assert.Len(t, status, 6)

	assert.Contains(t, status, "position_check")
	posStatus := status["position_check"]
	assert.Equal(t, "Position Check", posStatus.Name)
	assert.True(t, posStatus.Enabled)
}

func TestTradingHeartbeat_DisabledTasks(t *testing.T) {
	config := DefaultHeartbeatConfig()
	config.PositionCheckInterval = 50 * time.Millisecond

	heartbeat := NewTradingHeartbeat(
		config,
		&mockPositionTracker{},
		&mockStopLossService{},
		&mockSignalProcessor{},
		&mockFundingCollector{},
		&mockConnectivityChecker{},
		nil,
		&mockRiskManager{},
		nil,
	)

	heartbeat.tasks["position_check"].Enabled = false

	callCount := atomic.Int32{}
	heartbeat.tasks["position_check"].Handler = func(ctx context.Context) error {
		callCount.Add(1)
		return nil
	}

	heartbeat.Start(context.Background())
	defer heartbeat.Stop()

	time.Sleep(150 * time.Millisecond)

	assert.Equal(t, int32(0), callCount.Load())
}

func TestTradingHeartbeat_TaskErrorTracking(t *testing.T) {
	config := DefaultHeartbeatConfig()

	mockTracker := &mockPositionTrackerWithError{}

	heartbeat := NewTradingHeartbeat(
		config,
		mockTracker,
		&mockStopLossService{},
		&mockSignalProcessor{},
		&mockFundingCollector{},
		&mockConnectivityChecker{},
		nil,
		&mockRiskManager{},
		nil,
	)

	heartbeat.runTask(context.Background(), "position_check", heartbeat.tasks["position_check"])

	status := heartbeat.GetTaskStatus()
	posStatus := status["position_check"]
	assert.Equal(t, 1, posStatus.ErrorCount)
	assert.NotNil(t, posStatus.LastError)
}

type mockPositionTracker struct{}

func (m *mockPositionTracker) SyncPositions(ctx context.Context) error {
	return nil
}

type mockPositionTrackerWithCounter struct {
	callCount *atomic.Int32
}

func (m *mockPositionTrackerWithCounter) SyncPositions(ctx context.Context) error {
	m.callCount.Add(1)
	return nil
}

type mockPositionTrackerWithError struct{}

func (m *mockPositionTrackerWithError) SyncPositions(ctx context.Context) error {
	return errors.New("forced error")
}

type mockStopLossService struct{}

func (m *mockStopLossService) UpdateAllStopLosses(ctx context.Context) error {
	return nil
}

type mockSignalProcessor struct{}

func (m *mockSignalProcessor) ScanForSignals(ctx context.Context) error {
	return nil
}

type mockSignalProcessorWithCounter struct {
	callCount *atomic.Int32
}

func (m *mockSignalProcessorWithCounter) ScanForSignals(ctx context.Context) error {
	m.callCount.Add(1)
	return nil
}

type mockFundingCollector struct{}

func (m *mockFundingCollector) CheckFundingRates(ctx context.Context) error {
	return nil
}

type mockConnectivityChecker struct{}

func (m *mockConnectivityChecker) CheckConnectivity(ctx context.Context) error {
	return nil
}

type mockRiskManager struct{}

func (m *mockRiskManager) CheckRiskLimits(ctx context.Context) interface{} {
	return nil
}
