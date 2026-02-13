package risk

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	consecutiveLossKey = "risk:consecutive_loss:%s"
	consecutiveLossTTL = 24 * time.Hour
	pauseDuration      = 15 * time.Minute
	pauseKey           = "risk:paused:%s"
)

// ConsecutiveLossConfig holds configuration for the consecutive loss tracker.
type ConsecutiveLossConfig struct {
	MaxConsecutiveLosses int
	PauseDuration        time.Duration
}

// DefaultConsecutiveLossConfig returns the default configuration for tracking consecutive losses.
func DefaultConsecutiveLossConfig() ConsecutiveLossConfig {
	return ConsecutiveLossConfig{
		MaxConsecutiveLosses: 3,
		PauseDuration:        pauseDuration,
	}
}

// ConsecutiveLossTracker tracks consecutive trading losses and enforces pause periods.
type ConsecutiveLossTracker struct {
	redis  *redis.Client
	config ConsecutiveLossConfig
}

func NewConsecutiveLossTracker(redisClient *redis.Client, config ConsecutiveLossConfig) *ConsecutiveLossTracker {
	return &ConsecutiveLossTracker{
		redis:  redisClient,
		config: config,
	}
}

func (c *ConsecutiveLossTracker) Config() ConsecutiveLossConfig {
	return c.config
}

func (c *ConsecutiveLossTracker) RecordWin(ctx context.Context, userID string) error {
	key := fmt.Sprintf(consecutiveLossKey, userID)
	return c.redis.Del(ctx, key).Err()
}

func (c *ConsecutiveLossTracker) RecordLoss(ctx context.Context, userID string) error {
	key := fmt.Sprintf(consecutiveLossKey, userID)

	count, err := c.redis.Incr(ctx, key).Result()
	if err != nil {
		return err
	}

	if err := c.redis.Expire(ctx, key, consecutiveLossTTL).Err(); err != nil {
		return err
	}

	if int(count) >= c.config.MaxConsecutiveLosses {
		if err := c.SetPaused(ctx, userID, true); err != nil {
			return err
		}
	}

	return nil
}

func (c *ConsecutiveLossTracker) GetConsecutiveLosses(ctx context.Context, userID string) (int, error) {
	key := fmt.Sprintf(consecutiveLossKey, userID)

	count, err := c.redis.Get(ctx, key).Int()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	return count, nil
}

func (c *ConsecutiveLossTracker) IsPaused(ctx context.Context, userID string) (bool, time.Time, error) {
	key := fmt.Sprintf(pauseKey, userID)

	pausedAtStr, err := c.redis.Get(ctx, key).Result()
	if err == redis.Nil {
		return false, time.Time{}, nil
	}
	if err != nil {
		return false, time.Time{}, err
	}

	pausedAt, err := time.Parse(time.RFC3339, pausedAtStr)
	if err != nil {
		return false, time.Time{}, err
	}

	elapsed := time.Since(pausedAt)
	if elapsed >= c.config.PauseDuration {
		return false, time.Time{}, nil
	}

	return true, pausedAt, nil
}

func (c *ConsecutiveLossTracker) SetPaused(ctx context.Context, userID string, paused bool) error {
	key := fmt.Sprintf(pauseKey, userID)

	if !paused {
		return c.redis.Del(ctx, key).Err()
	}

	return c.redis.Set(ctx, key, time.Now().Format(time.RFC3339), c.config.PauseDuration).Err()
}

func (c *ConsecutiveLossTracker) Check(ctx context.Context, userID string) (bool, int, time.Time, error) {
	losses, err := c.GetConsecutiveLosses(ctx, userID)
	if err != nil {
		return false, 0, time.Time{}, err
	}

	isPaused, pausedAt, err := c.IsPaused(ctx, userID)
	if err != nil {
		return false, 0, time.Time{}, err
	}

	return isPaused, losses, pausedAt, nil
}

func (c *ConsecutiveLossTracker) CanTrade(ctx context.Context, userID string) (bool, string, error) {
	isPaused, losses, pausedAt, err := c.Check(ctx, userID)
	if err != nil {
		return false, "", err
	}

	if isPaused {
		remaining := c.config.PauseDuration - time.Since(pausedAt)
		return false, fmt.Sprintf("Trading paused due to %d consecutive losses. Try again in %v", losses, remaining), nil
	}

	if losses > 0 {
		remaining := c.config.MaxConsecutiveLosses - losses
		return true, fmt.Sprintf("Warning: %d consecutive losses recorded. %d more loss(es) will pause trading.", losses, remaining), nil
	}

	return true, "", nil
}

func (c *ConsecutiveLossTracker) Reset(ctx context.Context, userID string) error {
	lossKey := fmt.Sprintf(consecutiveLossKey, userID)
	pauseKey := fmt.Sprintf(pauseKey, userID)

	if err := c.redis.Del(ctx, lossKey, pauseKey).Err(); err != nil {
		return err
	}

	return nil
}

func (c *ConsecutiveLossTracker) GetStats(ctx context.Context, userID string) (int, bool, time.Duration, error) {
	losses, err := c.GetConsecutiveLosses(ctx, userID)
	if err != nil {
		return 0, false, 0, err
	}

	isPaused, pausedAt, err := c.IsPaused(ctx, userID)
	if err != nil {
		return 0, false, 0, err
	}

	var remaining time.Duration
	if isPaused {
		remaining = c.config.PauseDuration - time.Since(pausedAt)
	}

	return losses, isPaused, remaining, nil
}
