package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

type Logger interface {
	WithFields(map[string]interface{}) Logger
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

type IntrusionDetectionConfig struct {
	FailureThreshold int
	BlockDuration    time.Duration
	WindowDuration   time.Duration
}

func DefaultIntrusionDetectionConfig() IntrusionDetectionConfig {
	return IntrusionDetectionConfig{
		FailureThreshold: 5,
		BlockDuration:    15 * time.Minute,
		WindowDuration:   10 * time.Minute,
	}
}

type IntrusionDetectionService struct {
	redisClient *redis.Client
	logger      Logger
	config      IntrusionDetectionConfig
}

func NewIntrusionDetectionService(redisClient *redis.Client, logger Logger, config IntrusionDetectionConfig) *IntrusionDetectionService {
	if config.FailureThreshold == 0 {
		config = DefaultIntrusionDetectionConfig()
	}
	return &IntrusionDetectionService{
		redisClient: redisClient,
		logger:      logger,
		config:      config,
	}
}

func (s *IntrusionDetectionService) RecordFailedAttempt(ctx context.Context, ip string) error {
	key := fmt.Sprintf("intrusion:failed:%s", ip)

	count, err := s.redisClient.Incr(ctx, key).Result()
	if err != nil {
		s.logger.WithFields(map[string]interface{}{"ip": ip}).Error("Failed to record intrusion attempt")
		return fmt.Errorf("failed to record intrusion attempt: %w", err)
	}

	if count == 1 {
		s.redisClient.Expire(ctx, key, s.config.WindowDuration)
	}

	if int(count) >= s.config.FailureThreshold {
		blockKey := fmt.Sprintf("intrusion:blocked:%s", ip)

		pipe := s.redisClient.TxPipeline()
		pipe.Set(ctx, blockKey, "1", s.config.BlockDuration)
		pipe.Del(ctx, key)
		_, err := pipe.Exec(ctx)
		if err != nil {
			s.logger.WithFields(map[string]interface{}{"ip": ip, "error": err}).Error("Failed to execute IP block transaction")
		}

		s.logger.WithFields(map[string]interface{}{
			"ip":       ip,
			"attempts": count,
		}).Warn("IP blocked due to multiple failed attempts")
	}

	return nil
}

func (s *IntrusionDetectionService) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	blockKey := fmt.Sprintf("intrusion:blocked:%s", ip)
	result, err := s.redisClient.Exists(ctx, blockKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check blocked status: %w", err)
	}
	return result > 0, nil
}

func (s *IntrusionDetectionService) GetFailureCount(ctx context.Context, ip string) (int, error) {
	key := fmt.Sprintf("intrusion:failed:%s", ip)
	count, err := s.redisClient.Get(ctx, key).Int()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get failure count: %w", err)
	}
	return count, nil
}

func (s *IntrusionDetectionService) UnblockIP(ctx context.Context, ip string) error {
	blockKey := fmt.Sprintf("intrusion:blocked:%s", ip)
	failKey := fmt.Sprintf("intrusion:failed:%s", ip)

	_, err := s.redisClient.Del(ctx, blockKey, failKey).Result()
	if err != nil {
		return fmt.Errorf("failed to unblock IP: %w", err)
	}

	s.logger.WithFields(map[string]interface{}{"ip": ip}).Info("IP manually unblocked")
	return nil
}

func (s *IntrusionDetectionService) ClearExpiredBlocks(ctx context.Context) (int64, error) {
	return 0, nil
}

type PaperTradingConfig struct {
	MinCapital decimal.Decimal
}

func DefaultPaperTradingConfig() PaperTradingConfig {
	minCap, _ := decimal.NewFromString("10.0")
	return PaperTradingConfig{
		MinCapital: minCap,
	}
}

type PaperTradingService struct {
	redisClient *redis.Client
	logger      Logger
	config      PaperTradingConfig
}

func NewPaperTradingService(redisClient *redis.Client, logger Logger, config PaperTradingConfig) *PaperTradingService {
	if config.MinCapital.IsZero() {
		config = DefaultPaperTradingConfig()
	}
	return &PaperTradingService{
		redisClient: redisClient,
		logger:      logger,
		config:      config,
	}
}

type PaperAccount struct {
	UserID        string          `json:"user_id"`
	Balance       decimal.Decimal `json:"balance"`
	InitialAmount decimal.Decimal `json:"initial_amount"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

func (s *PaperTradingService) InitializePaperAccount(ctx context.Context, userID string, amount decimal.Decimal) (*PaperAccount, error) {
	if amount.LessThan(s.config.MinCapital) {
		return nil, fmt.Errorf("minimum capital is %s USDC", s.config.MinCapital.String())
	}

	key := fmt.Sprintf("paper:account:%s", userID)

	exists, err := s.redisClient.Exists(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to check account existence: %w", err)
	}
	if exists > 0 {
		return nil, fmt.Errorf("paper trading account already exists for user")
	}

	account := &PaperAccount{
		UserID:        userID,
		Balance:       amount,
		InitialAmount: amount,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	jsonData, jsonErr := json.Marshal(account)
	if jsonErr != nil {
		return nil, fmt.Errorf("failed to serialize paper account: %w", jsonErr)
	}

	err = s.redisClient.Set(ctx, key, jsonData, 0).Err()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize paper account: %w", err)
	}

	s.logger.WithFields(map[string]interface{}{
		"user_id": userID,
		"balance": amount.String(),
	}).Info("Paper trading account initialized")

	return account, nil
}

func (s *PaperTradingService) GetPaperAccount(ctx context.Context, userID string) (*PaperAccount, error) {
	key := fmt.Sprintf("paper:account:%s", userID)

	data, err := s.redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get paper account: %w", err)
	}

	var account PaperAccount
	if err := json.Unmarshal([]byte(data), &account); err != nil {
		return nil, fmt.Errorf("failed to deserialize paper account: %w", err)
	}

	return &account, nil
}

func (s *PaperTradingService) UpdatePaperBalance(ctx context.Context, userID string, newBalance decimal.Decimal) error {
	key := fmt.Sprintf("paper:account:%s", userID)

	err := s.redisClient.Watch(ctx, func(tx *redis.Tx) error {
		data, err := tx.Get(ctx, key).Result()
		if err == redis.Nil {
			return fmt.Errorf("paper trading account not found")
		}
		if err != nil {
			return fmt.Errorf("failed to get paper account: %w", err)
		}

		var account PaperAccount
		if err := json.Unmarshal([]byte(data), &account); err != nil {
			return fmt.Errorf("failed to deserialize paper account: %w", err)
		}

		account.Balance = newBalance
		account.UpdatedAt = time.Now()

		jsonData, jsonErr := json.Marshal(account)
		if jsonErr != nil {
			return fmt.Errorf("failed to serialize paper account: %w", jsonErr)
		}

		_, err = tx.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.Set(ctx, key, jsonData, 0)
			return nil
		})
		return err
	}, key)

	if err != nil {
		return fmt.Errorf("failed to update paper account: %w", err)
	}

	return nil
}

func (s *PaperTradingService) DeletePaperAccount(ctx context.Context, userID string) error {
	key := fmt.Sprintf("paper:account:%s", userID)

	_, err := s.redisClient.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to delete paper account: %w", err)
	}

	s.logger.WithFields(map[string]interface{}{"user_id": userID}).Info("Paper trading account deleted")
	return nil
}

func (s *PaperTradingService) GetMinCapital() decimal.Decimal {
	return s.config.MinCapital
}

func (s *PaperTradingService) SetMinCapital(amount decimal.Decimal) {
	s.config.MinCapital = amount
}
