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

	currentLossStr, err := d.redis.Get(ctx, key).Result()
	if err != nil && err != redis.Nil {
		return err
	}

	var newLoss decimal.Decimal
	if err == redis.Nil {
		newLoss = loss
	} else {
		currentLoss, parseErr := decimal.NewFromString(currentLossStr)
		if parseErr != nil {
			return parseErr
		}
		newLoss = currentLoss.Add(loss)
	}

	return d.redis.Set(ctx, key, newLoss.String(), dailyLossCapTTL).Err()
}

func (d *DailyLossTracker) GetCurrentLoss(ctx context.Context, userID string) (decimal.Decimal, error) {
	key := fmt.Sprintf(dailyLossCapKey, userID)

	lossStr, err := d.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return decimal.Zero, nil
	}
	if err != nil {
		return decimal.Zero, err
	}

	return decimal.NewFromString(lossStr)
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
