package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
)

// IntrusionDetectionService monitors and detects potential security threats
type IntrusionDetectionService struct {
	redisClient      *redis.Client
	logger           Logger
	failureThreshold int           // Max failures before blocking
	blockDuration    time.Duration // How long to block after threshold
	windowDuration   time.Duration // Time window to count failures
}

// Logger interface for intrusion detection
type Logger interface {
	WithFields(map[string]interface{}) Logger
	Info(msg string)
	Warn(msg string)
	Error(msg string)
}

// NewIntrusionDetectionService creates a new intrusion detection service
func NewIntrusionDetectionService(redisClient *redis.Client, logger Logger) *IntrusionDetectionService {
	return &IntrusionDetectionService{
		redisClient:      redisClient,
		logger:           logger,
		failureThreshold: 5,                // Block after 5 failures
		blockDuration:    15 * time.Minute, // Block for 15 minutes
		windowDuration:   10 * time.Minute, // Count failures within 10 minutes
	}
}

// RecordFailedAttempt records a failed authentication attempt from an IP
func (s *IntrusionDetectionService) RecordFailedAttempt(ctx context.Context, ip string) error {
	key := fmt.Sprintf("intrusion:failed:%s", ip)

	// Increment the failure count
	count, err := s.redisClient.Incr(ctx, key).Result()
	if err != nil {
		s.logger.WithFields(map[string]interface{}{"ip": ip}).Error("Failed to record intrusion attempt")
		return fmt.Errorf("failed to record intrusion attempt: %w", err)
	}

	// Set expiry on first failure
	if count == 1 {
		s.redisClient.Expire(ctx, key, s.windowDuration)
	}

	// Check if threshold exceeded
	if int(count) >= s.failureThreshold {
		blockKey := fmt.Sprintf("intrusion:blocked:%s", ip)
		s.redisClient.Set(ctx, blockKey, "1", s.blockDuration)
		s.logger.WithFields(map[string]interface{}{
			"ip":       ip,
			"attempts": count,
		}).Warn("IP blocked due to multiple failed attempts")

		// Remove the failure counter after blocking
		s.redisClient.Del(ctx, key)
	}

	return nil
}

// IsIPBlocked checks if an IP is currently blocked
func (s *IntrusionDetectionService) IsIPBlocked(ctx context.Context, ip string) (bool, error) {
	blockKey := fmt.Sprintf("intrusion:blocked:%s", ip)
	result, err := s.redisClient.Exists(ctx, blockKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check blocked status: %w", err)
	}
	return result > 0, nil
}

// GetFailureCount gets the current failure count for an IP
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

// UnblockIP manually unblocks an IP (admin function)
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

// ClearExpiredBlocks cleans up expired block entries (for periodic cleanup)
func (s *IntrusionDetectionService) ClearExpiredBlocks(ctx context.Context) (int64, error) {
	// This is handled automatically by Redis TTL
	// Could be used for metrics/logging
	return 0, nil
}

// PaperTradingService manages virtual trading accounts for paper trading
type PaperTradingService struct {
	redisClient *redis.Client
	logger      Logger
	minCapital  decimal.Decimal // Minimum capital in USDC
}

// NewPaperTradingService creates a new paper trading service
func NewPaperTradingService(redisClient *redis.Client, logger Logger) *PaperTradingService {
	return &PaperTradingService{
		redisClient: redisClient,
		logger:      logger,
		minCapital:  decimal.NewFromFloat(10.0), // Default minimum $10 USDC
	}
}

// PaperAccount represents a paper trading account
type PaperAccount struct {
	UserID        string          `json:"user_id"`
	Balance       decimal.Decimal `json:"balance"`
	InitialAmount decimal.Decimal `json:"initial_amount"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
}

// InitializePaperAccount initializes a new paper trading account with the specified amount
func (s *PaperTradingService) InitializePaperAccount(ctx context.Context, userID string, amount decimal.Decimal) (*PaperAccount, error) {
	// Validate minimum capital
	if amount.LessThan(s.minCapital) {
		return nil, fmt.Errorf("minimum capital is %s USDC", s.minCapital.String())
	}

	key := fmt.Sprintf("paper:account:%s", userID)

	// Check if account already exists
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

	// Store account data
	data := fmt.Sprintf("%s|%s|%s|%s|%s",
		account.UserID,
		account.Balance.String(),
		account.InitialAmount.String(),
		account.CreatedAt.Format(time.RFC3339),
		account.UpdatedAt.Format(time.RFC3339),
	)

	err = s.redisClient.Set(ctx, key, data, 0).Err()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize paper account: %w", err)
	}

	s.logger.WithFields(map[string]interface{}{
		"user_id": userID,
		"balance": amount.String(),
	}).Info("Paper trading account initialized")

	return account, nil
}

// GetPaperAccount gets the paper trading account for a user
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
	parts := strings.Split(data, "|")
	if len(parts) != 5 {
		return nil, fmt.Errorf("invalid account data format")
	}

	account.UserID = parts[0]
	account.Balance, _ = decimal.NewFromString(parts[1])
	account.InitialAmount, _ = decimal.NewFromString(parts[2])
	account.CreatedAt, _ = time.Parse(time.RFC3339, parts[3])
	account.UpdatedAt, _ = time.Parse(time.RFC3339, parts[4])

	return &account, nil
}

// UpdatePaperBalance updates the balance of a paper trading account
func (s *PaperTradingService) UpdatePaperBalance(ctx context.Context, userID string, newBalance decimal.Decimal) error {
	key := fmt.Sprintf("paper:account:%s", userID)

	// Get existing account
	account, err := s.GetPaperAccount(ctx, userID)
	if err != nil {
		return err
	}
	if account == nil {
		return fmt.Errorf("paper trading account not found")
	}

	account.Balance = newBalance
	account.UpdatedAt = time.Now()

	// Store updated account
	data := fmt.Sprintf("%s|%s|%s|%s|%s",
		account.UserID,
		account.Balance.String(),
		account.InitialAmount.String(),
		account.CreatedAt.Format(time.RFC3339),
		account.UpdatedAt.Format(time.RFC3339),
	)

	err = s.redisClient.Set(ctx, key, data, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to update paper account: %w", err)
	}

	return nil
}

// DeletePaperAccount deletes a paper trading account
func (s *PaperTradingService) DeletePaperAccount(ctx context.Context, userID string) error {
	key := fmt.Sprintf("paper:account:%s", userID)

	_, err := s.redisClient.Del(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("failed to delete paper account: %w", err)
	}

	s.logger.WithFields(map[string]interface{}{"user_id": userID}).Info("Paper trading account deleted")
	return nil
}

// GetMinCapital returns the minimum capital required for paper trading
func (s *PaperTradingService) GetMinCapital() decimal.Decimal {
	return s.minCapital
}

// SetMinCapital sets the minimum capital requirement
func (s *PaperTradingService) SetMinCapital(amount decimal.Decimal) {
	s.minCapital = amount
}
