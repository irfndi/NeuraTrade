package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockKillSwitchNotifier struct {
	mu               sync.Mutex
	triggeredCount   int
	recoveredCount   int
	lastTriggerState *KillSwitchState
	lastRecoverState *KillSwitchState
}

func (m *mockKillSwitchNotifier) NotifyKillSwitchTriggered(ctx context.Context, state *KillSwitchState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggeredCount++
	m.lastTriggerState = state
	return nil
}

func (m *mockKillSwitchNotifier) NotifyKillSwitchRecovered(ctx context.Context, state *KillSwitchState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.recoveredCount++
	m.lastRecoverState = state
	return nil
}

func (m *mockKillSwitchNotifier) GetTriggeredCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.triggeredCount
}

func (m *mockKillSwitchNotifier) GetRecoveredCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.recoveredCount
}

func (m *mockKillSwitchNotifier) GetLastTriggerState() *KillSwitchState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastTriggerState
}

func (m *mockKillSwitchNotifier) GetLastRecoverState() *KillSwitchState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastRecoverState
}

func TestNewKillSwitchMonitor(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	notifier := &mockKillSwitchNotifier{}

	t.Run("creates monitor with defaults", func(t *testing.T) {
		monitor := NewKillSwitchMonitor(nil, notifier, nil, logger)
		require.NotNil(t, monitor)
		assert.Equal(t, KillSwitchStatusActive, monitor.GetState().Status)
		assert.NotNil(t, monitor.conditions)
		assert.Len(t, monitor.GetConditions(), 4)
	})

	t.Run("creates monitor with custom config", func(t *testing.T) {
		config := &KillSwitchMonitorConfig{
			CheckInterval:  10 * time.Second,
			CooldownPeriod: 2 * time.Minute,
		}
		monitor := NewKillSwitchMonitor(nil, notifier, config, logger)
		require.NotNil(t, monitor)
		assert.Equal(t, 10*time.Second, monitor.checkInterval)
		assert.Equal(t, 2*time.Minute, monitor.cooldownPeriod)
	})
}

func TestKillSwitchMonitor_Trigger(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	notifier := &mockKillSwitchNotifier{}
	monitor := NewKillSwitchMonitor(nil, notifier, nil, logger)

	t.Run("triggers successfully", func(t *testing.T) {
		err := monitor.Trigger(KillSwitchTriggerDrawdown, "test", "drawdown exceeded 20%")
		require.NoError(t, err)
		assert.Equal(t, KillSwitchStatusTriggered, monitor.GetState().Status)
		assert.Equal(t, KillSwitchTriggerDrawdown, monitor.GetState().Trigger)
		assert.Equal(t, "test", monitor.GetState().TriggeredBy)
		assert.Equal(t, "drawdown exceeded 20%", monitor.GetState().Reason)
		assert.NotNil(t, monitor.GetState().TriggeredAt)
		assert.NotNil(t, monitor.GetState().AutoRecoverAt)
	})

	t.Run("cannot trigger when already triggered", func(t *testing.T) {
		err := monitor.Trigger(KillSwitchTriggerLossCap, "test2", "should fail")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already triggered")
	})
}

func TestKillSwitchMonitor_Recover(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	notifier := &mockKillSwitchNotifier{}
	monitor := NewKillSwitchMonitor(nil, notifier, nil, logger)

	t.Run("cannot recover when not triggered", func(t *testing.T) {
		err := monitor.Recover("test")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not triggered")
	})

	t.Run("recovers successfully after trigger", func(t *testing.T) {
		require.NoError(t, monitor.Trigger(KillSwitchTriggerDrawdown, "test", "test trigger"))

		err := monitor.Recover("admin")
		require.NoError(t, err)
		assert.Equal(t, KillSwitchStatusActive, monitor.GetState().Status)
		assert.Equal(t, KillSwitchTrigger(""), monitor.GetState().Trigger)
		assert.Nil(t, monitor.GetState().TriggeredAt)
		assert.Equal(t, "admin", monitor.GetState().RecoveredBy)
		assert.NotNil(t, monitor.GetState().RecoveredAt)
	})
}

func TestKillSwitchMonitor_PauseResume(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	notifier := &mockKillSwitchNotifier{}
	monitor := NewKillSwitchMonitor(nil, notifier, nil, logger)

	t.Run("pauses successfully", func(t *testing.T) {
		err := monitor.Pause("manual pause")
		require.NoError(t, err)
		assert.Equal(t, KillSwitchStatusPaused, monitor.GetState().Status)
		assert.Equal(t, "manual pause", monitor.GetState().Reason)
	})

	t.Run("cannot pause when triggered", func(t *testing.T) {
		monitor2 := NewKillSwitchMonitor(nil, notifier, nil, logger)
		require.NoError(t, monitor2.Trigger(KillSwitchTriggerDrawdown, "test", "test"))

		err := monitor2.Pause("should fail")
		assert.Error(t, err)
	})

	t.Run("resumes successfully", func(t *testing.T) {
		require.NoError(t, monitor.Pause("pause test"))

		err := monitor.Resume()
		require.NoError(t, err)
		assert.Equal(t, KillSwitchStatusActive, monitor.GetState().Status)
		assert.Equal(t, "", monitor.GetState().Reason)
	})

	t.Run("cannot resume when not paused", func(t *testing.T) {
		err := monitor.Resume()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not paused")
	})
}

func TestKillSwitchMonitor_IsTradingAllowed(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	notifier := &mockKillSwitchNotifier{}
	monitor := NewKillSwitchMonitor(nil, notifier, nil, logger)

	t.Run("trading allowed when active", func(t *testing.T) {
		assert.True(t, monitor.IsTradingAllowed())
	})

	t.Run("trading not allowed when triggered", func(t *testing.T) {
		require.NoError(t, monitor.Trigger(KillSwitchTriggerDrawdown, "test", "test"))
		assert.False(t, monitor.IsTradingAllowed())
	})

	t.Run("trading not allowed when paused", func(t *testing.T) {
		monitor2 := NewKillSwitchMonitor(nil, notifier, nil, logger)
		require.NoError(t, monitor2.Pause("test"))
		assert.False(t, monitor2.IsTradingAllowed())
	})
}

func TestKillSwitchMonitor_Conditions(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	notifier := &mockKillSwitchNotifier{}
	monitor := NewKillSwitchMonitor(nil, notifier, nil, logger)

	t.Run("registers custom condition", func(t *testing.T) {
		condition := &KillSwitchCondition{
			ID:          "custom_test",
			Name:        "Custom Test",
			Description: "Test condition",
			Trigger:     KillSwitchTriggerManual,
			Threshold:   0.5,
			IsActive:    true,
		}
		monitor.RegisterCondition(condition)

		conditions := monitor.GetConditions()
		var found bool
		for _, c := range conditions {
			if c.ID == "custom_test" {
				found = true
				assert.Equal(t, 0.5, c.Threshold)
				break
			}
		}
		assert.True(t, found)
	})

	t.Run("updates existing condition", func(t *testing.T) {
		err := monitor.UpdateCondition("max_drawdown", 0.3, true)
		require.NoError(t, err)

		conditions := monitor.GetConditions()
		for _, c := range conditions {
			if c.ID == "max_drawdown" {
				assert.Equal(t, 0.3, c.Threshold)
				break
			}
		}
	})

	t.Run("fails to update non-existent condition", func(t *testing.T) {
		err := monitor.UpdateCondition("non_existent", 0.5, true)
		assert.Error(t, err)
	})

	t.Run("checks condition", func(t *testing.T) {
		triggered, err := monitor.CheckCondition("max_drawdown", 0.25)
		require.NoError(t, err)
		assert.False(t, triggered) // 0.25 < 0.3 threshold

		triggered, err = monitor.CheckCondition("max_drawdown", 0.35)
		require.NoError(t, err)
		assert.True(t, triggered) // 0.35 > 0.3 threshold
	})
}

func TestKillSwitchMonitor_Notifications(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	notifier := &mockKillSwitchNotifier{}
	monitor := NewKillSwitchMonitor(nil, notifier, nil, logger)

	t.Run("sends notification on trigger", func(t *testing.T) {
		require.NoError(t, monitor.Trigger(KillSwitchTriggerDrawdown, "test", "test trigger"))

		time.Sleep(100 * time.Millisecond) // Wait for async notification

		assert.Equal(t, 1, notifier.GetTriggeredCount())
		assert.NotNil(t, notifier.GetLastTriggerState())
		assert.Equal(t, KillSwitchStatusTriggered, notifier.GetLastTriggerState().Status)
	})

	t.Run("sends notification on recover", func(t *testing.T) {
		require.NoError(t, monitor.Recover("admin"))

		time.Sleep(100 * time.Millisecond) // Wait for async notification

		assert.Equal(t, 1, notifier.GetRecoveredCount())
		assert.NotNil(t, notifier.GetLastRecoverState())
		assert.Equal(t, KillSwitchStatusActive, notifier.GetLastRecoverState().Status)
	})
}

func TestKillSwitchMonitor_StartStop(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	notifier := &mockKillSwitchNotifier{}
	monitor := NewKillSwitchMonitor(nil, notifier, &KillSwitchMonitorConfig{
		CheckInterval:  100 * time.Millisecond,
		CooldownPeriod: 1 * time.Second,
	}, logger)

	t.Run("starts and stops successfully", func(t *testing.T) {
		require.NoError(t, monitor.Start())
		assert.True(t, monitor.IsRunning())

		monitor.Stop()
		assert.False(t, monitor.IsRunning())
	})

	t.Run("cannot start twice", func(t *testing.T) {
		require.NoError(t, monitor.Start())

		err := monitor.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")

		monitor.Stop()
	})
}

func TestKillSwitchMonitor_WithRedis(t *testing.T) {
	t.Skip("requires Redis instance")

	logger := logging.NewStandardLogger("info", "test")
	notifier := &mockKillSwitchNotifier{}

	redisClient := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	defer func() { _ = redisClient.Close() }()

	monitor := NewKillSwitchMonitor(redisClient, notifier, nil, logger)

	t.Run("saves and loads state", func(t *testing.T) {
		ctx := context.Background()
		redisClient.Del(ctx, "kill_switch:state")

		require.NoError(t, monitor.Start())

		require.NoError(t, monitor.Trigger(KillSwitchTriggerDrawdown, "test", "test"))

		monitor.Stop()

		monitor2 := NewKillSwitchMonitor(redisClient, notifier, nil, logger)
		require.NoError(t, monitor2.Start())

		// State should be loaded from Redis
		assert.Equal(t, KillSwitchStatusTriggered, monitor2.GetState().Status)

		monitor2.Stop()
	})
}
