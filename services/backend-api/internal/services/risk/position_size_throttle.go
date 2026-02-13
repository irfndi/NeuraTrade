package risk

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

const (
	throttleKeyPrefix = "risk:position_throttle:"
	throttleTTL       = 24 * time.Hour
)

type PositionSizeThrottleConfig struct {
	Enabled               bool            `json:"enabled"`
	ReductionFactor       decimal.Decimal `json:"reduction_factor"`
	MinPositionMultiplier decimal.Decimal `json:"min_position_multiplier"`
	LossThreshold         int             `json:"loss_threshold"`
	RecoveryFactor        decimal.Decimal `json:"recovery_factor"`
}

func DefaultPositionSizeThrottleConfig() PositionSizeThrottleConfig {
	return PositionSizeThrottleConfig{
		Enabled:               true,
		ReductionFactor:       decimal.NewFromFloat(0.7),
		MinPositionMultiplier: decimal.NewFromFloat(0.1),
		LossThreshold:         1,
		RecoveryFactor:        decimal.NewFromFloat(1.5),
	}
}

type PositionSizeThrottle struct {
	redis  *redis.Client
	config PositionSizeThrottleConfig
}

func NewPositionSizeThrottle(redisClient *redis.Client, config PositionSizeThrottleConfig) *PositionSizeThrottle {
	return &PositionSizeThrottle{
		redis:  redisClient,
		config: config,
	}
}

func (t *PositionSizeThrottle) Config() PositionSizeThrottleConfig {
	return t.config
}

func (t *PositionSizeThrottle) SetConfig(config PositionSizeThrottleConfig) {
	t.config = config
}

func (t *PositionSizeThrottle) GetThrottleMultiplier(ctx context.Context, userID string) (decimal.Decimal, error) {
	if !t.config.Enabled {
		return decimal.NewFromInt(1), nil
	}

	key := fmt.Sprintf("%s%s", throttleKeyPrefix, userID)

	multiplierStr, err := t.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return decimal.NewFromInt(1), nil
	}
	if err != nil {
		return decimal.NewFromInt(1), fmt.Errorf("failed to get throttle multiplier: %w", err)
	}

	multiplier, err := decimal.NewFromString(multiplierStr)
	if err != nil {
		return decimal.NewFromInt(1), nil
	}

	return multiplier, nil
}

func (t *PositionSizeThrottle) ApplyThrottle(ctx context.Context, userID string, requestedSize decimal.Decimal) (decimal.Decimal, error) {
	multiplier, err := t.GetThrottleMultiplier(ctx, userID)
	if err != nil {
		return requestedSize, err
	}

	return requestedSize.Mul(multiplier), nil
}

func (t *PositionSizeThrottle) RecordLoss(ctx context.Context, userID string, consecutiveLosses int) (decimal.Decimal, error) {
	if !t.config.Enabled {
		return decimal.NewFromInt(1), nil
	}

	key := fmt.Sprintf("%s%s", throttleKeyPrefix, userID)

	if consecutiveLosses < t.config.LossThreshold {
		return decimal.NewFromInt(1), nil
	}

	effectiveLosses := consecutiveLosses - t.config.LossThreshold + 1
	reductionFloat, _ := t.config.ReductionFactor.Float64()
	newMultiplier := decimal.NewFromFloat(math.Pow(reductionFloat, float64(effectiveLosses)))

	if newMultiplier.LessThan(t.config.MinPositionMultiplier) {
		newMultiplier = t.config.MinPositionMultiplier
	}

	if err := t.redis.Set(ctx, key, newMultiplier.String(), throttleTTL).Err(); err != nil {
		return decimal.NewFromInt(1), fmt.Errorf("failed to update throttle multiplier: %w", err)
	}

	return newMultiplier, nil
}

func (t *PositionSizeThrottle) RecordWin(ctx context.Context, userID string) (decimal.Decimal, error) {
	if !t.config.Enabled {
		return decimal.NewFromInt(1), nil
	}

	key := fmt.Sprintf("%s%s", throttleKeyPrefix, userID)

	currentMultiplier, err := t.GetThrottleMultiplier(ctx, userID)
	if err != nil {
		return decimal.NewFromInt(1), err
	}

	if currentMultiplier.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		return decimal.NewFromInt(1), nil
	}

	newMultiplier := currentMultiplier.Mul(t.config.RecoveryFactor)

	if newMultiplier.GreaterThan(decimal.NewFromInt(1)) {
		newMultiplier = decimal.NewFromInt(1)
	}

	if err := t.redis.Set(ctx, key, newMultiplier.String(), throttleTTL).Err(); err != nil {
		return currentMultiplier, fmt.Errorf("failed to update throttle multiplier: %w", err)
	}

	if newMultiplier.GreaterThanOrEqual(decimal.NewFromInt(1)) {
		t.redis.Del(ctx, key)
	}

	return newMultiplier, nil
}

func (t *PositionSizeThrottle) IsThrottled(ctx context.Context, userID string) (bool, error) {
	multiplier, err := t.GetThrottleMultiplier(ctx, userID)
	if err != nil {
		return false, err
	}

	return multiplier.LessThan(decimal.NewFromInt(1)), nil
}

func (t *PositionSizeThrottle) Reset(ctx context.Context, userID string) error {
	key := fmt.Sprintf("%s%s", throttleKeyPrefix, userID)
	return t.redis.Del(ctx, key).Err()
}

type ThrottleStatus struct {
	Multiplier      decimal.Decimal `json:"multiplier"`
	IsThrottled     bool            `json:"is_throttled"`
	EffectiveLosses int             `json:"effective_losses,omitempty"`
}

func (t *PositionSizeThrottle) GetStatus(ctx context.Context, userID string) (ThrottleStatus, error) {
	multiplier, err := t.GetThrottleMultiplier(ctx, userID)
	if err != nil {
		return ThrottleStatus{}, err
	}

	status := ThrottleStatus{
		Multiplier:  multiplier,
		IsThrottled: multiplier.LessThan(decimal.NewFromInt(1)),
	}

	if status.IsThrottled && !t.config.ReductionFactor.IsZero() && !multiplier.IsZero() {
		reductionFloat, _ := t.config.ReductionFactor.Float64()
		multiplierFloat, _ := multiplier.Float64()
		if reductionFloat > 0 && multiplierFloat > 0 {
			effectiveLosses := math.Log(multiplierFloat) / math.Log(reductionFloat)
			if effectiveLosses > 0 {
				status.EffectiveLosses = int(effectiveLosses)
			}
		}
	}

	return status, nil
}

func (t *PositionSizeThrottle) AdjustPositionSize(
	ctx context.Context,
	userID string,
	consecutiveLosses int,
	requestedSize decimal.Decimal,
) (decimal.Decimal, string, error) {
	if !t.config.Enabled {
		return requestedSize, "", nil
	}

	if consecutiveLosses >= t.config.LossThreshold {
		_, err := t.RecordLoss(ctx, userID, consecutiveLosses)
		if err != nil {
			return requestedSize, "", err
		}
	}

	throttledSize, err := t.ApplyThrottle(ctx, userID, requestedSize)
	if err != nil {
		return requestedSize, "", err
	}

	var message string
	if throttledSize.LessThan(requestedSize) {
		reductionPct := decimal.NewFromInt(1).Sub(throttledSize.Div(requestedSize)).Mul(decimal.NewFromInt(100))
		message = fmt.Sprintf("Position size reduced by %s%% due to recent losses", reductionPct.StringFixed(1))
	}

	return throttledSize, message, nil
}
