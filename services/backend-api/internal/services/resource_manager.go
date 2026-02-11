package services

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
)

// ResourceType represents different types of resources.
type ResourceType string

const (
	GoroutineResource  ResourceType = "goroutine"
	ConnectionResource ResourceType = "connection"
	ChannelResource    ResourceType = "channel"
	TimerResource      ResourceType = "timer"
	FileResource       ResourceType = "file"
)

// Resource represents a managed resource.
type Resource struct {
	ID          string
	Type        ResourceType
	CreatedAt   time.Time
	LastUsed    time.Time
	CleanupFunc func() error
	Metadata    map[string]interface{}
}

// ResourceStats holds statistics about resource usage.
type ResourceStats struct {
	TotalCreated    int64     `json:"total_created"`
	TotalCleaned    int64     `json:"total_cleaned"`
	CurrentActive   int64     `json:"current_active"`
	LeaksDetected   int64     `json:"leaks_detected"`
	CleanupErrors   int64     `json:"cleanup_errors"`
	LastCleanupTime time.Time `json:"last_cleanup_time"`
}

// ResourceManager manages system resources and prevents leaks.
type ResourceManager struct {
	logger    *zaplogrus.Logger
	resources map[string]*Resource
	stats     map[ResourceType]*ResourceStats
	mu        sync.RWMutex

	// Configuration
	maxIdleTime     time.Duration
	cleanupInterval time.Duration
	maxResources    int

	// Monitoring
	monitoringCtx    context.Context
	monitoringCancel context.CancelFunc
	shutdownChan     chan struct{}
	shutdownOnce     sync.Once
}

// NewResourceManager creates a new resource manager.
//
// Parameters:
//
//	logger: Logger instance.
//
// Returns:
//
//	*ResourceManager: Initialized manager.
func NewResourceManager(logger *zaplogrus.Logger) *ResourceManager {
	ctx, cancel := context.WithCancel(context.Background())

	rm := &ResourceManager{
		logger:           logger,
		resources:        make(map[string]*Resource),
		stats:            make(map[ResourceType]*ResourceStats),
		maxIdleTime:      5 * time.Minute,
		cleanupInterval:  1 * time.Minute,
		maxResources:     1000,
		monitoringCtx:    ctx,
		monitoringCancel: cancel,
		shutdownChan:     make(chan struct{}),
	}

	// Initialize stats for all resource types
	resourceTypes := []ResourceType{GoroutineResource, ConnectionResource, ChannelResource, TimerResource, FileResource}
	for _, rt := range resourceTypes {
		rm.stats[rt] = &ResourceStats{}
	}

	// Start monitoring
	go rm.startMonitoring()

	return rm
}

// RegisterResource registers a new resource for management.
//
// Parameters:
//
//	id: Unique resource identifier.
//	resourceType: Type of resource.
//	cleanupFunc: Function to clean up the resource.
//	metadata: Additional metadata.
func (rm *ResourceManager) RegisterResource(id string, resourceType ResourceType, cleanupFunc func() error, metadata map[string]interface{}) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	resource := &Resource{
		ID:          id,
		Type:        resourceType,
		CreatedAt:   time.Now(),
		LastUsed:    time.Now(),
		CleanupFunc: cleanupFunc,
		Metadata:    metadata,
	}

	rm.resources[id] = resource
	atomic.AddInt64(&rm.stats[resourceType].TotalCreated, 1)
	atomic.AddInt64(&rm.stats[resourceType].CurrentActive, 1)

	rm.logger.WithFields(zaplogrus.Fields{
		"resource_id":   id,
		"resource_type": resourceType,
		"metadata":      metadata,
	}).Debug("Resource registered")

	// Check if we're approaching resource limits
	if len(rm.resources) > rm.maxResources {
		rm.logger.WithField("resource_count", len(rm.resources)).Warn("High resource count detected")
	}
}

// UpdateResourceUsage updates the last used time for a resource.
//
// Parameters:
//
//	id: Resource identifier.
func (rm *ResourceManager) UpdateResourceUsage(id string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if resource, exists := rm.resources[id]; exists {
		resource.LastUsed = time.Now()
	}
}

// CleanupResource manually cleans up a specific resource.
//
// Parameters:
//
//	id: Resource identifier.
//
// Returns:
//
//	error: Error if cleanup fails.
func (rm *ResourceManager) CleanupResource(id string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	resource, exists := rm.resources[id]
	if !exists {
		return nil
	}

	var err error
	if resource.CleanupFunc != nil {
		err = resource.CleanupFunc()
		if err != nil {
			atomic.AddInt64(&rm.stats[resource.Type].CleanupErrors, 1)
			rm.logger.WithFields(zaplogrus.Fields{
				"resource_id":   id,
				"resource_type": resource.Type,
				"error":         err.Error(),
			}).Error("Resource cleanup failed")
		}
	}

	delete(rm.resources, id)
	atomic.AddInt64(&rm.stats[resource.Type].TotalCleaned, 1)
	atomic.AddInt64(&rm.stats[resource.Type].CurrentActive, -1)
	rm.stats[resource.Type].LastCleanupTime = time.Now()

	rm.logger.WithFields(zaplogrus.Fields{
		"resource_id":   id,
		"resource_type": resource.Type,
		"age":           time.Since(resource.CreatedAt),
	}).Debug("Resource cleaned up")

	return err
}

// CleanupIdleResources cleans up resources that have been idle for too long.
func (rm *ResourceManager) CleanupIdleResources() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	now := time.Now()
	var toCleanup []string

	for id, resource := range rm.resources {
		if now.Sub(resource.LastUsed) > rm.maxIdleTime {
			toCleanup = append(toCleanup, id)
		}
	}

	for _, id := range toCleanup {
		resource := rm.resources[id]
		var err error
		if resource.CleanupFunc != nil {
			err = resource.CleanupFunc()
			if err != nil {
				atomic.AddInt64(&rm.stats[resource.Type].CleanupErrors, 1)
				rm.logger.WithFields(zaplogrus.Fields{
					"resource_id":   id,
					"resource_type": resource.Type,
					"error":         err.Error(),
				}).Error("Idle resource cleanup failed")
			}
		}

		delete(rm.resources, id)
		atomic.AddInt64(&rm.stats[resource.Type].TotalCleaned, 1)
		atomic.AddInt64(&rm.stats[resource.Type].CurrentActive, -1)
		rm.stats[resource.Type].LastCleanupTime = now

		rm.logger.WithFields(zaplogrus.Fields{
			"resource_id":   id,
			"resource_type": resource.Type,
			"idle_time":     now.Sub(resource.LastUsed),
		}).Info("Idle resource cleaned up")
	}

	if len(toCleanup) > 0 {
		rm.logger.WithField("cleaned_count", len(toCleanup)).Info("Idle resource cleanup completed")
	}
}

// DetectLeaks detects potential resource leaks.
func (rm *ResourceManager) DetectLeaks() {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	now := time.Now()
	leakThreshold := 10 * time.Minute

	for id, resource := range rm.resources {
		age := now.Sub(resource.CreatedAt)
		idleTime := now.Sub(resource.LastUsed)

		// Consider it a leak if resource is very old and hasn't been used recently
		if age > leakThreshold && idleTime > leakThreshold/2 {
			atomic.AddInt64(&rm.stats[resource.Type].LeaksDetected, 1)
			rm.logger.WithFields(zaplogrus.Fields{
				"resource_id":   id,
				"resource_type": resource.Type,
				"age":           age,
				"idle_time":     idleTime,
				"metadata":      resource.Metadata,
			}).Warn("Potential resource leak detected")
		}
	}
}

// GetResourceStats returns statistics for all resource types.
//
// Returns:
//
//	map[ResourceType]*ResourceStats: Stats per type.
func (rm *ResourceManager) GetResourceStats() map[ResourceType]*ResourceStats {
	rm.mu.RLock()
	defer rm.mu.RUnlock()

	stats := make(map[ResourceType]*ResourceStats)
	for rt, stat := range rm.stats {
		stats[rt] = &ResourceStats{
			TotalCreated:    atomic.LoadInt64(&stat.TotalCreated),
			TotalCleaned:    atomic.LoadInt64(&stat.TotalCleaned),
			CurrentActive:   atomic.LoadInt64(&stat.CurrentActive),
			LeaksDetected:   atomic.LoadInt64(&stat.LeaksDetected),
			CleanupErrors:   atomic.LoadInt64(&stat.CleanupErrors),
			LastCleanupTime: stat.LastCleanupTime,
		}
	}

	return stats
}

// GetSystemStats returns system-level resource statistics.
//
// Returns:
//
//	map[string]interface{}: System stats.
func (rm *ResourceManager) GetSystemStats() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"goroutines":        runtime.NumGoroutine(),
		"memory_alloc":      m.Alloc,
		"memory_sys":        m.Sys,
		"gc_cycles":         m.NumGC,
		"managed_resources": len(rm.resources),
	}
}

// startMonitoring starts the background monitoring goroutine
func (rm *ResourceManager) startMonitoring() {
	rm.mu.RLock()
	ticker := time.NewTicker(rm.cleanupInterval)
	rm.mu.RUnlock()
	defer ticker.Stop()

	leakDetectionTicker := time.NewTicker(5 * time.Minute)
	defer leakDetectionTicker.Stop()

	for {
		select {
		case <-rm.monitoringCtx.Done():
			return
		case <-rm.shutdownChan:
			return
		case <-ticker.C:
			rm.CleanupIdleResources()
		case <-leakDetectionTicker.C:
			rm.DetectLeaks()
			rm.logResourceStats()
		}
	}
}

// logResourceStats logs current resource statistics
func (rm *ResourceManager) logResourceStats() {
	stats := rm.GetResourceStats()
	systemStats := rm.GetSystemStats()

	rm.logger.WithFields(zaplogrus.Fields{
		"resource_stats": stats,
		"system_stats":   systemStats,
	}).Info("Resource manager statistics")
}

// CleanupAll cleans up all managed resources.
func (rm *ResourceManager) CleanupAll() {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	rm.logger.Info("Cleaning up all managed resources")

	for id, resource := range rm.resources {
		if resource.CleanupFunc != nil {
			if err := resource.CleanupFunc(); err != nil {
				atomic.AddInt64(&rm.stats[resource.Type].CleanupErrors, 1)
				rm.logger.WithFields(zaplogrus.Fields{
					"resource_id":   id,
					"resource_type": resource.Type,
					"error":         err.Error(),
				}).Error("Resource cleanup failed during shutdown")
			} else {
				atomic.AddInt64(&rm.stats[resource.Type].TotalCleaned, 1)
			}
		}
		atomic.AddInt64(&rm.stats[resource.Type].CurrentActive, -1)
	}

	rm.resources = make(map[string]*Resource)
	rm.logger.Info("All managed resources cleaned up")
}

// Shutdown gracefully shuts down the resource manager.
func (rm *ResourceManager) Shutdown() {
	rm.shutdownOnce.Do(func() {
		rm.logger.Info("Shutting down resource manager")

		// Stop monitoring
		rm.monitoringCancel()
		close(rm.shutdownChan)

		// Cleanup all resources
		rm.CleanupAll()

		rm.logger.Info("Resource manager shutdown complete")
	})
}

// SetMaxIdleTime sets the maximum idle time before cleanup.
//
// Parameters:
//
//	duration: Idle duration.
func (rm *ResourceManager) SetMaxIdleTime(duration time.Duration) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.maxIdleTime = duration
}

// SetCleanupInterval sets the cleanup interval.
//
// Parameters:
//
//	duration: Interval duration.
func (rm *ResourceManager) SetCleanupInterval(duration time.Duration) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.cleanupInterval = duration
}

// SetMaxResources sets the maximum number of resources.
//
// Parameters:
//
//	max: Maximum count.
func (rm *ResourceManager) SetMaxResources(max int) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.maxResources = max
}

// GetResourceCount returns the current number of managed resources.
//
// Returns:
//
//	int: Count.
func (rm *ResourceManager) GetResourceCount() int {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return len(rm.resources)
}

// IsResourceManaged checks if a resource is currently managed.
//
// Parameters:
//
//	id: Resource identifier.
//
// Returns:
//
//	bool: True if managed.
func (rm *ResourceManager) IsResourceManaged(id string) bool {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	_, exists := rm.resources[id]
	return exists
}
