package risk

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

const (
	dailyLossCapKey = "risk:daily_loss:%s"
	dailyLossCapTTL = 24 * time.Hour
)

type DailyLossCapConfig struct {
	MaxDailyLoss decimal.Decimal
}

type DailyLossTracker struct {
	redis  *redis.Client
	config DailyLossCapConfig
}

func NewDailyLossTracker(redisClient *redis.Client, config DailyLossCapConfig) *DailyLossTracker {
	return &DailyLossTracker{
		redis:  redisClient,
		config: config,
	}
}

func (d *DailyLossTracker) Config() DailyLossCapConfig {
	return d.config
}

func (d *DailyLossTracker) RecordLoss(ctx context.Context, userID string, loss decimal.Decimal) error {
	return d.recordLoss(ctx, userID, loss)
}

func (d *DailyLossTracker) recordLoss(ctx context.Context, userID string, loss decimal.Decimal) error {
	key := fmt.Sprintf(dailyLossCapKey, userID)

	currentLoss, err := d.redis.Get(ctx, key).Float64()
	if err != nil && err != redis.Nil {
		return err
	}

	newLoss := currentLoss + loss.InexactFloat64()

	return d.redis.Set(ctx, key, newLoss, dailyLossCapTTL).Err()
}

func (d *DailyLossTracker) GetCurrentLoss(ctx context.Context, userID string) (decimal.Decimal, error) {
	key := fmt.Sprintf(dailyLossCapKey, userID)

	loss, err := d.redis.Get(ctx, key).Float64()
	if err == redis.Nil {
		return decimal.Zero, nil
	}
	if err != nil {
		return decimal.Zero, err
	}

	return decimal.NewFromFloat(loss), nil
}

func (d *DailyLossTracker) CheckLossLimit(ctx context.Context, userID string) (bool, decimal.Decimal, error) {
	currentLoss, err := d.GetCurrentLoss(ctx, userID)
	if err != nil {
		return false, decimal.Zero, err
	}

	exceeded := currentLoss.GreaterThanOrEqual(d.config.MaxDailyLoss)
	return exceeded, currentLoss, nil
}

func (d *DailyLossTracker) ResetDailyLoss(ctx context.Context, userID string) error {
	key := fmt.Sprintf(dailyLossCapKey, userID)
	return d.redis.Del(ctx, key).Err()
}
