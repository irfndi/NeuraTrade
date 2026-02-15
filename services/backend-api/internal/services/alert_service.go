package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
)

type AlertLevel string

const (
	AlertLevelInfo     AlertLevel = "info"
	AlertLevelWarning  AlertLevel = "warning"
	AlertLevelError    AlertLevel = "error"
	AlertLevelCritical AlertLevel = "critical"
)

type SystemAlert struct {
	ID        string         `json:"id"`
	Level     AlertLevel     `json:"level"`
	Source    string         `json:"source"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	Resolved  bool           `json:"resolved"`
}

type AlertHandler func(ctx context.Context, alert SystemAlert) error

type AlertService struct {
	db                  DBPool
	redis               *database.RedisClient
	logger              *slog.Logger
	handlers            []AlertHandler
	alertThrottler      *AlertThrottler
	notificationService *NotificationService
	mu                  sync.RWMutex
}

type AlertThrottler struct {
	alerts   map[string]time.Time
	mu       sync.Mutex
	cooldown time.Duration
}

func NewAlertThrottler(cooldown time.Duration) *AlertThrottler {
	return &AlertThrottler{
		alerts:   make(map[string]time.Time),
		cooldown: cooldown,
	}
}

func (t *AlertThrottler) ShouldSend(alertKey string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	lastSent, exists := t.alerts[alertKey]
	if !exists {
		t.alerts[alertKey] = time.Now()
		return true
	}

	if time.Since(lastSent) > t.cooldown {
		t.alerts[alertKey] = time.Now()
		return true
	}

	return false
}

func NewAlertService(db DBPool, redis *database.RedisClient, logger *slog.Logger) *AlertService {
	return &AlertService{
		db:             db,
		redis:          redis,
		logger:         logger,
		handlers:       make([]AlertHandler, 0),
		alertThrottler: NewAlertThrottler(5 * time.Minute),
	}
}

func (s *AlertService) RegisterHandler(handler AlertHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, handler)
}

func (s *AlertService) SetNotificationService(ns *NotificationService) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notificationService = ns
}

func (s *AlertService) SendAlert(ctx context.Context, level AlertLevel, source, message string, details map[string]any) error {
	alert := SystemAlert{
		ID:        fmt.Sprintf("alert-%d", time.Now().UnixNano()),
		Level:     level,
		Source:    source,
		Message:   message,
		Details:   details,
		Timestamp: time.Now(),
		Resolved:  false,
	}

	alertKey := fmt.Sprintf("%s:%s:%s", source, level, message)
	if !s.alertThrottler.ShouldSend(alertKey) {
		s.logger.Debug("Alert throttled", "alert_key", alertKey)
		return nil
	}

	s.logger.Info("Sending system alert",
		"level", level,
		"source", source,
		"message", message)

	s.mu.RLock()
	handlers := make([]AlertHandler, len(s.handlers))
	copy(handlers, s.handlers)
	ns := s.notificationService
	s.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler(ctx, alert); err != nil {
			s.logger.Error("Alert handler error", "error", err)
		}
	}

	if ns != nil && (level == AlertLevelError || level == AlertLevelCritical) {
		go func() {
			// TODO: Implement actual notification dispatch via NotificationService
			// Currently logs only - integrate with Telegram or other notification channels
			s.logger.Info(fmt.Sprintf("[%s] %s: %s", level, source, message))
		}()
	}

	return s.persistAlert(ctx, alert)
}

func (s *AlertService) persistAlert(ctx context.Context, alert SystemAlert) error {
	if s.redis == nil {
		return nil
	}

	key := fmt.Sprintf("alerts:%s", alert.ID)
	data, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("failed to marshal alert: %w", err)
	}

	return s.redis.Set(ctx, key, data, 24*time.Hour)
}

func (s *AlertService) GetActiveAlerts(ctx context.Context) ([]SystemAlert, error) {
	if s.redis == nil {
		return nil, nil
	}

	pattern := "alerts:*"
	iter := s.redis.Client.Scan(ctx, 0, pattern, 100).Iterator()

	var alerts []SystemAlert
	for iter.Next(ctx) {
		key := iter.Val()
		data, err := s.redis.Get(ctx, key)
		if err != nil {
			continue
		}
		var alert SystemAlert
		if err := json.Unmarshal([]byte(data), &alert); err != nil {
			continue
		}
		if !alert.Resolved {
			alerts = append(alerts, alert)
		}
	}

	return alerts, nil
}

func (s *AlertService) ResolveAlert(ctx context.Context, alertID string) error {
	if s.redis == nil {
		return nil
	}

	key := fmt.Sprintf("alerts:%s", alertID)
	data, err := s.redis.Get(ctx, key)
	if err != nil {
		return err
	}

	var alert SystemAlert
	if err := json.Unmarshal([]byte(data), &alert); err != nil {
		return err
	}

	alert.Resolved = true
	alert.Timestamp = time.Now()

	newData, err := json.Marshal(alert)
	if err != nil {
		return err
	}

	return s.redis.Set(ctx, key, newData, 24*time.Hour)
}

type HealthAlertConfig struct {
	RedisDown            bool
	DatabaseDown         bool
	HighLatency          bool
	LatencyThresholdMs   int
	ConnectionErrorCount int
	ErrorThreshold       int
}

func (s *AlertService) CheckRedisHealth(ctx context.Context) error {
	if s.redis == nil {
		return nil
	}

	err := s.redis.HealthCheck(ctx)
	if err != nil {
		_ = s.SendAlert(ctx, AlertLevelCritical, "redis", "Redis connection failed",
			map[string]any{"error": err.Error()})
		return err
	}
	return nil
}

func (s *AlertService) CheckDatabaseHealth(ctx context.Context) error {
	if s.db == nil {
		return nil
	}

	_, err := s.db.Exec(ctx, "SELECT 1")
	return err
}

func (s *AlertService) RunHealthCheck(ctx context.Context, config HealthAlertConfig) {
	if err := s.CheckRedisHealth(ctx); err != nil && config.RedisDown {
		s.logger.Error("Redis health check failed", "error", err)
	}

	if err := s.CheckDatabaseHealth(ctx); err != nil && config.DatabaseDown {
		_ = s.SendAlert(ctx, AlertLevelCritical, "database", "Database connection failed",
			map[string]any{"error": err.Error()})
		s.logger.Error("Database health check failed", "error", err)
	}
}
