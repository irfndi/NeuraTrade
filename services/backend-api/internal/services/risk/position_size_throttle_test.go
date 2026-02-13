package risk

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func setupTestThrottle(t *testing.T) (*PositionSizeThrottle, *miniredis.Miniredis) {
	mr, err := miniredis.Run()
	assert.NoError(t, err)

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	throttle := NewPositionSizeThrottle(client, DefaultPositionSizeThrottleConfig())
	return throttle, mr
}

func TestDefaultPositionSizeThrottleConfig(t *testing.T) {
	config := DefaultPositionSizeThrottleConfig()

	assert.True(t, config.Enabled)
	assert.True(t, config.ReductionFactor.Equal(decimal.NewFromFloat(0.7)))
	assert.True(t, config.MinPositionMultiplier.Equal(decimal.NewFromFloat(0.1)))
	assert.Equal(t, 1, config.LossThreshold)
	assert.True(t, config.RecoveryFactor.Equal(decimal.NewFromFloat(1.5)))
}

func TestPositionSizeThrottle_GetThrottleMultiplier_NoThrottle(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	multiplier, err := throttle.GetThrottleMultiplier(context.Background(), "user1")
	assert.NoError(t, err)
	assert.True(t, multiplier.Equal(decimal.NewFromInt(1)))
}

func TestPositionSizeThrottle_RecordLoss_BelowThreshold(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	throttle.config.LossThreshold = 3

	multiplier, err := throttle.RecordLoss(context.Background(), "user1", 2)
	assert.NoError(t, err)
	assert.True(t, multiplier.Equal(decimal.NewFromInt(1)))
}

func TestPositionSizeThrottle_RecordLoss_AtThreshold(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	throttle.config.LossThreshold = 1

	multiplier, err := throttle.RecordLoss(context.Background(), "user1", 1)
	assert.NoError(t, err)
	assert.True(t, multiplier.Equal(decimal.NewFromFloat(0.7)))
}

func TestPositionSizeThrottle_RecordLoss_MultipleLosses(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	throttle.config.LossThreshold = 1

	multiplier, err := throttle.RecordLoss(context.Background(), "user1", 3)
	assert.NoError(t, err)
	expected := decimal.NewFromFloat(0.343)
	assert.True(t, multiplier.Sub(expected).Abs().LessThan(decimal.NewFromFloat(0.001)))
}

func TestPositionSizeThrottle_RecordLoss_RespectsMinimum(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	throttle.config.LossThreshold = 1
	throttle.config.MinPositionMultiplier = decimal.NewFromFloat(0.5)

	multiplier, err := throttle.RecordLoss(context.Background(), "user1", 10)
	assert.NoError(t, err)
	assert.True(t, multiplier.Equal(decimal.NewFromFloat(0.5)))
}

func TestPositionSizeThrottle_RecordWin(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	throttle.config.LossThreshold = 1

	_, err := throttle.RecordLoss(context.Background(), "user1", 1)
	assert.NoError(t, err)

	multiplier, err := throttle.RecordWin(context.Background(), "user1")
	assert.NoError(t, err)
	assert.True(t, multiplier.Equal(decimal.NewFromInt(1)))
}

func TestPositionSizeThrottle_RecordWin_PartialRecovery(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	throttle.config.LossThreshold = 1

	_, err := throttle.RecordLoss(context.Background(), "user1", 3)
	assert.NoError(t, err)

	beforeRecovery, err := throttle.GetThrottleMultiplier(context.Background(), "user1")
	assert.NoError(t, err)

	multiplier, err := throttle.RecordWin(context.Background(), "user1")
	assert.NoError(t, err)
	expected := beforeRecovery.Mul(throttle.config.RecoveryFactor)
	assert.True(t, multiplier.Equal(expected))
	assert.True(t, multiplier.LessThan(decimal.NewFromInt(1)))
}

func TestPositionSizeThrottle_RecordWin_CapAtOne(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	_, err := throttle.redis.Set(context.Background(), "risk:position_throttle:user1", "0.9", 0).Result()
	assert.NoError(t, err)

	multiplier, err := throttle.RecordWin(context.Background(), "user1")
	assert.NoError(t, err)
	assert.True(t, multiplier.Equal(decimal.NewFromInt(1)))
}

func TestPositionSizeThrottle_RecordWin_NoThrottle(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	multiplier, err := throttle.RecordWin(context.Background(), "user1")
	assert.NoError(t, err)
	assert.True(t, multiplier.Equal(decimal.NewFromInt(1)))
}

func TestPositionSizeThrottle_IsThrottled(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	throttled, err := throttle.IsThrottled(context.Background(), "user1")
	assert.NoError(t, err)
	assert.False(t, throttled)

	_, err = throttle.RecordLoss(context.Background(), "user1", 1)
	assert.NoError(t, err)

	throttled, err = throttle.IsThrottled(context.Background(), "user1")
	assert.NoError(t, err)
	assert.True(t, throttled)
}

func TestPositionSizeThrottle_ApplyThrottle(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	requestedSize := decimal.NewFromFloat(1000)
	throttledSize, err := throttle.ApplyThrottle(context.Background(), "user1", requestedSize)
	assert.NoError(t, err)
	assert.True(t, throttledSize.Equal(requestedSize))

	_, err = throttle.RecordLoss(context.Background(), "user1", 1)
	assert.NoError(t, err)

	throttledSize, err = throttle.ApplyThrottle(context.Background(), "user1", requestedSize)
	assert.NoError(t, err)
	expected := decimal.NewFromFloat(700)
	assert.True(t, throttledSize.Equal(expected))
}

func TestPositionSizeThrottle_Reset(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	_, err := throttle.RecordLoss(context.Background(), "user1", 2)
	assert.NoError(t, err)

	throttled, err := throttle.IsThrottled(context.Background(), "user1")
	assert.NoError(t, err)
	assert.True(t, throttled)

	err = throttle.Reset(context.Background(), "user1")
	assert.NoError(t, err)

	throttled, err = throttle.IsThrottled(context.Background(), "user1")
	assert.NoError(t, err)
	assert.False(t, throttled)
}

func TestPositionSizeThrottle_GetStatus(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	status, err := throttle.GetStatus(context.Background(), "user1")
	assert.NoError(t, err)
	assert.False(t, status.IsThrottled)
	assert.True(t, status.Multiplier.Equal(decimal.NewFromInt(1)))

	_, err = throttle.RecordLoss(context.Background(), "user1", 2)
	assert.NoError(t, err)

	status, err = throttle.GetStatus(context.Background(), "user1")
	assert.NoError(t, err)
	assert.True(t, status.IsThrottled)
	assert.True(t, status.Multiplier.LessThan(decimal.NewFromInt(1)))
}

func TestPositionSizeThrottle_AdjustPositionSize(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	requestedSize := decimal.NewFromFloat(1000)

	adjustedSize, message, err := throttle.AdjustPositionSize(context.Background(), "user1", 0, requestedSize)
	assert.NoError(t, err)
	assert.True(t, adjustedSize.Equal(requestedSize))
	assert.Empty(t, message)

	adjustedSize, message, err = throttle.AdjustPositionSize(context.Background(), "user1", 1, requestedSize)
	assert.NoError(t, err)
	assert.True(t, adjustedSize.Equal(decimal.NewFromFloat(700)))
	assert.NotEmpty(t, message)
}

func TestPositionSizeThrottle_Disabled(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	throttle.config.Enabled = false

	_, err := throttle.RecordLoss(context.Background(), "user1", 5)
	assert.NoError(t, err)

	multiplier, err := throttle.GetThrottleMultiplier(context.Background(), "user1")
	assert.NoError(t, err)
	assert.True(t, multiplier.Equal(decimal.NewFromInt(1)))
}

func TestPositionSizeThrottle_SetConfig(t *testing.T) {
	throttle, mr := setupTestThrottle(t)
	defer mr.Close()

	newConfig := PositionSizeThrottleConfig{
		Enabled:               false,
		ReductionFactor:       decimal.NewFromFloat(0.5),
		MinPositionMultiplier: decimal.NewFromFloat(0.2),
		LossThreshold:         2,
		RecoveryFactor:        decimal.NewFromFloat(2.0),
	}

	throttle.SetConfig(newConfig)
	config := throttle.Config()

	assert.False(t, config.Enabled)
	assert.True(t, config.ReductionFactor.Equal(decimal.NewFromFloat(0.5)))
	assert.True(t, config.MinPositionMultiplier.Equal(decimal.NewFromFloat(0.2)))
	assert.Equal(t, 2, config.LossThreshold)
	assert.True(t, config.RecoveryFactor.Equal(decimal.NewFromFloat(2.0)))
}
