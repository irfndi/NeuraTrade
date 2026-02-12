package services

import (
	"testing"

	"github.com/irfndi/neuratrade/internal/logging"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewControlledLiquidationService(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	killSwitch := NewKillSwitchMonitor(nil, nil, nil, logger)

	t.Run("creates service with defaults", func(t *testing.T) {
		service := NewControlledLiquidationService(nil, nil, nil, killSwitch, nil, logger)
		require.NotNil(t, service)
		assert.Equal(t, decimal.NewFromInt(100000), service.limits.MaxPositionSize)
		assert.Equal(t, 50, service.limits.MaxDailyLiquidations)
		assert.NotNil(t, service.dailyStats)
	})

	t.Run("creates service with custom config", func(t *testing.T) {
		config := &ControlledLiquidationConfig{
			Limits: LiquidationLimits{
				MaxPositionSize:      decimal.NewFromInt(50000),
				MaxDailyLiquidations: 25,
			},
		}
		service := NewControlledLiquidationService(nil, nil, nil, killSwitch, config, logger)
		require.NotNil(t, service)
		assert.Equal(t, decimal.NewFromInt(50000), service.limits.MaxPositionSize)
		assert.Equal(t, 25, service.limits.MaxDailyLiquidations)
	})
}

func TestControlledLiquidationService_ValidateRequest(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	service := NewControlledLiquidationService(nil, nil, nil, nil, nil, logger)

	t.Run("requires position_id or symbol", func(t *testing.T) {
		req := &LiquidationRequest{}
		err := service.validateRequest(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "position_id or symbol is required")
	})

	t.Run("defaults percentage to 100", func(t *testing.T) {
		req := &LiquidationRequest{PositionID: "pos-123"}
		err := service.validateRequest(req)
		require.NoError(t, err)
		assert.Equal(t, decimal.NewFromInt(100), req.Percentage)
	})

	t.Run("rejects invalid percentage", func(t *testing.T) {
		req := &LiquidationRequest{
			PositionID: "pos-123",
			Percentage: decimal.NewFromInt(150),
		}
		err := service.validateRequest(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "percentage must be between 0 and 100")
	})

	t.Run("accepts valid partial percentage", func(t *testing.T) {
		req := &LiquidationRequest{
			PositionID: "pos-123",
			Percentage: decimal.NewFromInt(50),
		}
		err := service.validateRequest(req)
		require.NoError(t, err)
		assert.Equal(t, decimal.NewFromInt(50), req.Percentage)
	})
}

func TestControlledLiquidationService_ValidateRiskLimits(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")

	t.Run("rejects when kill switch is active", func(t *testing.T) {
		killSwitch := NewKillSwitchMonitor(nil, nil, nil, logger)
		killSwitch.Trigger(KillSwitchTriggerDrawdown, "test", "test trigger")

		service := NewControlledLiquidationService(nil, nil, nil, killSwitch, nil, logger)

		req := &LiquidationRequest{
			PositionID: "pos-123",
			Percentage: decimal.NewFromInt(100),
		}
		_, err := service.validateRiskLimits(req)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "kill switch is active")
	})

	t.Run("allows when kill switch is off", func(t *testing.T) {
		killSwitch := NewKillSwitchMonitor(nil, nil, nil, logger)

		service := NewControlledLiquidationService(nil, nil, nil, killSwitch, nil, logger)

		req := &LiquidationRequest{
			PositionID: "pos-123",
			Percentage: decimal.NewFromInt(100),
		}
		_, err := service.validateRiskLimits(req)
		require.NoError(t, err)
	})
}

func TestControlledLiquidationService_StartStop(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	service := NewControlledLiquidationService(nil, nil, nil, nil, nil, logger)

	t.Run("starts and stops successfully", func(t *testing.T) {
		require.NoError(t, service.Start())
		assert.True(t, service.IsRunning())

		service.Stop()
		assert.False(t, service.IsRunning())
	})

	t.Run("cannot start twice", func(t *testing.T) {
		require.NoError(t, service.Start())

		err := service.Start()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already running")

		service.Stop()
	})
}

func TestControlledLiquidationService_Limits(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	service := NewControlledLiquidationService(nil, nil, nil, nil, nil, logger)

	t.Run("gets default limits", func(t *testing.T) {
		limits := service.GetLimits()
		assert.Equal(t, decimal.NewFromInt(100000), limits.MaxPositionSize)
	})

	t.Run("sets new limits", func(t *testing.T) {
		newLimits := LiquidationLimits{
			MaxPositionSize:      decimal.NewFromInt(25000),
			MaxDailyLiquidations: 10,
		}
		service.SetLimits(newLimits)

		limits := service.GetLimits()
		assert.Equal(t, decimal.NewFromInt(25000), limits.MaxPositionSize)
		assert.Equal(t, 10, limits.MaxDailyLiquidations)
	})
}

func TestControlledLiquidationService_DailyStats(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	service := NewControlledLiquidationService(nil, nil, nil, nil, nil, logger)

	t.Run("returns initial stats", func(t *testing.T) {
		count, loss := service.GetDailyStats()
		assert.Equal(t, 0, count)
		assert.True(t, loss.IsZero())
	})

	t.Run("updates stats after liquidation", func(t *testing.T) {
		result := &LiquidationResult{
			ID:     "liq-test",
			NetPnL: decimal.NewFromInt(-100),
			Status: LiquidationStatusCompleted,
		}
		service.updateDailyStats(result)

		count, loss := service.GetDailyStats()
		assert.Equal(t, 1, count)
		assert.Equal(t, decimal.NewFromInt(100), loss)
	})
}

func TestControlledLiquidationService_PendingCount(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	service := NewControlledLiquidationService(nil, nil, nil, nil, nil, logger)

	t.Run("returns zero initially", func(t *testing.T) {
		assert.Equal(t, 0, service.GetPendingCount())
	})

	t.Run("increases when request queued", func(t *testing.T) {
		service.mu.Lock()
		service.pendingQueue = append(service.pendingQueue, &LiquidationRequest{
			PositionID: "pos-123",
		})
		service.mu.Unlock()

		assert.Equal(t, 1, service.GetPendingCount())
	})
}

func TestControlledLiquidationService_Results(t *testing.T) {
	logger := logging.NewStandardLogger("info", "test")
	service := NewControlledLiquidationService(nil, nil, nil, nil, nil, logger)

	t.Run("returns error for non-existent result", func(t *testing.T) {
		_, err := service.GetLiquidationResult("non-existent")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("returns stored result", func(t *testing.T) {
		result := &LiquidationResult{
			ID:     "liq-test-123",
			Status: LiquidationStatusCompleted,
		}
		service.mu.Lock()
		service.results["liq-test-123"] = result
		service.mu.Unlock()

		got, err := service.GetLiquidationResult("liq-test-123")
		require.NoError(t, err)
		assert.Equal(t, LiquidationStatusCompleted, got.Status)
	})
}

func TestGenerateLiquidationID(t *testing.T) {
	id1 := generateLiquidationID()
	id2 := generateLiquidationID()

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Contains(t, id1, "liq-")
	assert.Contains(t, id2, "liq-")
}

func TestLiquidationStatus_Constants(t *testing.T) {
	assert.Equal(t, LiquidationStatus("pending"), LiquidationStatusPending)
	assert.Equal(t, LiquidationStatus("processing"), LiquidationStatusProcessing)
	assert.Equal(t, LiquidationStatus("completed"), LiquidationStatusCompleted)
	assert.Equal(t, LiquidationStatus("partial"), LiquidationStatusPartial)
	assert.Equal(t, LiquidationStatus("failed"), LiquidationStatusFailed)
	assert.Equal(t, LiquidationStatus("rejected"), LiquidationStatusRejected)
}

func TestLiquidationReason_Constants(t *testing.T) {
	assert.Equal(t, LiquidationReason("manual"), LiquidationReasonManual)
	assert.Equal(t, LiquidationReason("kill_switch"), LiquidationReasonKillSwitch)
	assert.Equal(t, LiquidationReason("risk_limit"), LiquidationReasonRiskLimit)
	assert.Equal(t, LiquidationReason("stop_loss"), LiquidationReasonStopLoss)
	assert.Equal(t, LiquidationReason("drawdown"), LiquidationReasonDrawdown)
	assert.Equal(t, LiquidationReason("margin_call"), LiquidationReasonMargin)
	assert.Equal(t, LiquidationReason("emergency"), LiquidationReasonEmergency)
}
