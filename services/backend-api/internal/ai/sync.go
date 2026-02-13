package ai

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

type SyncConfig struct {
	SyncInterval     time.Duration
	RefreshOnStartup bool
	CacheTTL         time.Duration
	EnableMetrics    bool
}

func DefaultSyncConfig() SyncConfig {
	return SyncConfig{
		SyncInterval:     6 * time.Hour,
		RefreshOnStartup: true,
		CacheTTL:         CacheTTL,
		EnableMetrics:    true,
	}
}

type SyncMetrics struct {
	mu               sync.RWMutex
	LastSyncTime     time.Time
	LastSyncDuration time.Duration
	TotalSyncs       int64
	FailedSyncs      int64
	ModelsCount      int
	ProvidersCount   int
}

func (m *SyncMetrics) RecordSync(duration time.Duration, models, providers int, failed bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.LastSyncTime = time.Now()
	m.LastSyncDuration = duration
	m.TotalSyncs++
	if failed {
		m.FailedSyncs++
	}
	m.ModelsCount = models
	m.ProvidersCount = providers
}

func (m *SyncMetrics) GetSnapshot() SyncMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return *m
}

type SyncService struct {
	registry *Registry
	redis    *redis.Client
	logger   *zap.Logger
	config   SyncConfig
	metrics  SyncMetrics

	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

func NewSyncService(registry *Registry, redisClient *redis.Client, logger *zap.Logger, config SyncConfig) *SyncService {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &SyncService{
		registry: registry,
		redis:    redisClient,
		logger:   logger,
		config:   config,
		stopCh:   make(chan struct{}),
	}
}

func (s *SyncService) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = true
	s.mu.Unlock()

	if s.config.RefreshOnStartup {
		if err := s.syncOnce(ctx); err != nil {
			s.logger.Error("Initial sync failed", zap.Error(err))
		}
	}

	s.wg.Add(1)
	go s.runBackgroundSync()

	s.logger.Info("Registry sync service started",
		zap.Duration("interval", s.config.SyncInterval),
	)

	return nil
}

func (s *SyncService) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	close(s.stopCh)
	s.wg.Wait()

	s.logger.Info("Registry sync service stopped")
}

func (s *SyncService) runBackgroundSync() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := s.syncOnce(ctx); err != nil {
				s.logger.Error("Background sync failed", zap.Error(err))
			}
			cancel()
		}
	}
}

func (s *SyncService) syncOnce(ctx context.Context) error {
	startTime := time.Now()

	registry, err := s.registry.FetchModels(ctx)
	if err != nil {
		s.metrics.RecordSync(0, 0, 0, true)
		return err
	}

	duration := time.Since(startTime)
	s.metrics.RecordSync(duration, len(registry.Models), len(registry.Providers), false)

	s.logger.Info("Registry sync completed",
		zap.Duration("duration", duration),
		zap.Int("models", len(registry.Models)),
		zap.Int("providers", len(registry.Providers)),
	)

	return nil
}

func (s *SyncService) ForceSync(ctx context.Context) error {
	return s.syncOnce(ctx)
}

func (s *SyncService) GetMetrics() SyncMetrics {
	return s.metrics.GetSnapshot()
}

func (s *SyncService) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *SyncService) GetStatus() map[string]interface{} {
	metrics := s.metrics.GetSnapshot()

	return map[string]interface{}{
		"running":            s.IsRunning(),
		"sync_interval":      s.config.SyncInterval.String(),
		"last_sync_time":     metrics.LastSyncTime.Format(time.RFC3339),
		"last_sync_duration": metrics.LastSyncDuration.String(),
		"total_syncs":        metrics.TotalSyncs,
		"failed_syncs":       metrics.FailedSyncs,
		"models_count":       metrics.ModelsCount,
		"providers_count":    metrics.ProvidersCount,
	}
}
