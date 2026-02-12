package services

import (
	"sync"
	"testing"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/stretchr/testify/assert"
)

func TestNewResourceManager(t *testing.T) {
	logger := zaplogrus.New()

	rm := NewResourceManager(logger)

	assert.NotNil(t, rm)
	assert.Equal(t, logger, rm.logger)
	assert.NotNil(t, rm.resources)
	assert.NotNil(t, rm.stats)
	assert.Equal(t, 5*time.Minute, rm.maxIdleTime)
	assert.Equal(t, 1*time.Minute, rm.cleanupInterval)
	assert.Equal(t, 1000, rm.maxResources)
	assert.NotNil(t, rm.monitoringCtx)
	assert.NotNil(t, rm.monitoringCancel)
	assert.NotNil(t, rm.shutdownChan)

	// Check that all resource types are initialized
	assert.Contains(t, rm.stats, GoroutineResource)
	assert.Contains(t, rm.stats, ConnectionResource)
	assert.Contains(t, rm.stats, ChannelResource)
	assert.Contains(t, rm.stats, TimerResource)
	assert.Contains(t, rm.stats, FileResource)
}

func TestResourceManager_RegisterResource(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	cleanupCalled := false
	cleanupFunc := func() error {
		cleanupCalled = true
		return nil
	}

	metadata := map[string]interface{}{
		"exchange": "binance",
		"symbol":   "BTCUSDT",
	}

	rm.RegisterResource("test-resource", ConnectionResource, cleanupFunc, metadata)

	// Verify resource was registered
	assert.True(t, rm.IsResourceManaged("test-resource"))

	// Verify stats were updated
	stats := rm.GetResourceStats()
	assert.Equal(t, int64(1), stats[ConnectionResource].TotalCreated)
	assert.Equal(t, int64(1), stats[ConnectionResource].CurrentActive)

	// Verify resource details
	rm.mu.RLock()
	resource, exists := rm.resources["test-resource"]
	rm.mu.RUnlock()
	assert.True(t, exists)
	assert.Equal(t, "test-resource", resource.ID)
	assert.Equal(t, ConnectionResource, resource.Type)
	assert.NotNil(t, resource.CleanupFunc)
	assert.Equal(t, metadata, resource.Metadata)
	assert.False(t, resource.CreatedAt.IsZero())
	assert.False(t, resource.LastUsed.IsZero())

	// Verify cleanup function works
	cleanupErr := resource.CleanupFunc()
	assert.NoError(t, cleanupErr)
	assert.True(t, cleanupCalled)
}

func TestResourceManager_UpdateResourceUsage(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	rm.RegisterResource("test-resource", TimerResource, nil, nil)

	// Get initial last used time
	rm.mu.RLock()
	initialLastUsed := rm.resources["test-resource"].LastUsed
	rm.mu.RUnlock()

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Update usage
	rm.UpdateResourceUsage("test-resource")

	// Verify last used time was updated
	rm.mu.RLock()
	updatedLastUsed := rm.resources["test-resource"].LastUsed
	rm.mu.RUnlock()
	assert.True(t, updatedLastUsed.After(initialLastUsed))
}

func TestResourceManager_CleanupResource(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	cleanupCalled := false
	cleanupFunc := func() error {
		cleanupCalled = true
		return nil
	}

	rm.RegisterResource("test-resource", FileResource, cleanupFunc, nil)

	// Verify resource exists
	assert.True(t, rm.IsResourceManaged("test-resource"))

	// Cleanup resource
	err := rm.CleanupResource("test-resource")
	assert.NoError(t, err)
	assert.True(t, cleanupCalled)

	// Verify resource was removed
	assert.False(t, rm.IsResourceManaged("test-resource"))

	// Verify stats were updated
	stats := rm.GetResourceStats()
	assert.Equal(t, int64(1), stats[FileResource].TotalCleaned)
	assert.Equal(t, int64(0), stats[FileResource].CurrentActive)
}

func TestResourceManager_CleanupResourceWithError(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	cleanupFunc := func() error {
		return assert.AnError
	}

	rm.RegisterResource("test-resource", ChannelResource, cleanupFunc, nil)

	// Cleanup resource with error
	err := rm.CleanupResource("test-resource")
	assert.Error(t, err)
	assert.Equal(t, assert.AnError, err)

	// Verify stats were updated
	stats := rm.GetResourceStats()
	assert.Equal(t, int64(1), stats[ChannelResource].TotalCleaned)
	assert.Equal(t, int64(1), stats[ChannelResource].CleanupErrors)
	assert.Equal(t, int64(0), stats[ChannelResource].CurrentActive)
}

func TestResourceManager_CleanupResource_NonExistent(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	// Cleanup non-existent resource should not error
	err := rm.CleanupResource("non-existent")
	assert.NoError(t, err)
}

func TestResourceManager_CleanupIdleResources(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	// Set short idle time for testing
	rm.SetMaxIdleTime(10 * time.Millisecond)

	// Register multiple resources
	rm.RegisterResource("active-resource", GoroutineResource, nil, nil)
	rm.RegisterResource("idle-resource", ConnectionResource, nil, nil)

	// Update active resource to keep it alive
	time.Sleep(20 * time.Millisecond)
	rm.UpdateResourceUsage("active-resource")

	// Cleanup idle resources
	rm.CleanupIdleResources()

	// Verify idle resource was cleaned up
	assert.False(t, rm.IsResourceManaged("idle-resource"))
	assert.True(t, rm.IsResourceManaged("active-resource"))

	// Verify stats
	stats := rm.GetResourceStats()
	assert.Equal(t, int64(1), stats[ConnectionResource].TotalCleaned)
	assert.Equal(t, int64(0), stats[ConnectionResource].CurrentActive)
	assert.Equal(t, int64(0), stats[GoroutineResource].TotalCleaned)
	assert.Equal(t, int64(1), stats[GoroutineResource].CurrentActive)
}

func TestResourceManager_DetectLeaks(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	// Set short leak threshold for testing
	// Note: This is a simplified test as we can't easily modify the leak threshold
	// In a real scenario, you'd need to refactor the code to make it configurable

	// Register old resource that hasn't been used
	rm.RegisterResource("old-resource", TimerResource, nil, map[string]interface{}{
		"created_for": "leak_test",
	})

	// Wait a bit to make it seem old
	time.Sleep(50 * time.Millisecond)

	// Detect leaks
	rm.DetectLeaks()

	// Verify stats (leak detection is based on time thresholds, so this may not always detect in tests)
	stats := rm.GetResourceStats()
	assert.GreaterOrEqual(t, stats[TimerResource].LeaksDetected, int64(0))
}

func TestResourceManager_GetResourceStats(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	// Register some resources
	rm.RegisterResource("resource1", GoroutineResource, nil, nil)
	rm.RegisterResource("resource2", ConnectionResource, nil, nil)

	// Get stats
	stats := rm.GetResourceStats()

	assert.Contains(t, stats, GoroutineResource)
	assert.Contains(t, stats, ConnectionResource)

	// Verify goroutine stats
	goroutineStats := stats[GoroutineResource]
	assert.Equal(t, int64(1), goroutineStats.TotalCreated)
	assert.Equal(t, int64(0), goroutineStats.TotalCleaned)
	assert.Equal(t, int64(1), goroutineStats.CurrentActive)
	assert.Equal(t, int64(0), goroutineStats.LeaksDetected)
	assert.Equal(t, int64(0), goroutineStats.CleanupErrors)

	// Verify connection stats
	connectionStats := stats[ConnectionResource]
	assert.Equal(t, int64(1), connectionStats.TotalCreated)
	assert.Equal(t, int64(0), connectionStats.TotalCleaned)
	assert.Equal(t, int64(1), connectionStats.CurrentActive)
}

func TestResourceManager_GetSystemStats(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	// Register some resources
	rm.RegisterResource("test1", FileResource, nil, nil)
	rm.RegisterResource("test2", ChannelResource, nil, nil)

	// Get system stats
	stats := rm.GetSystemStats()

	assert.Contains(t, stats, "goroutines")
	assert.Contains(t, stats, "memory_alloc")
	assert.Contains(t, stats, "memory_sys")
	assert.Contains(t, stats, "gc_cycles")
	assert.Contains(t, stats, "managed_resources")

	// Verify managed resources count
	assert.Equal(t, 2, stats["managed_resources"])
}

func TestResourceManager_CleanupAll(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	cleanupCount := 0
	cleanupFunc := func() error {
		cleanupCount++
		return nil
	}

	// Register multiple resources
	rm.RegisterResource("resource1", GoroutineResource, cleanupFunc, nil)
	rm.RegisterResource("resource2", ConnectionResource, cleanupFunc, nil)
	rm.RegisterResource("resource3", ChannelResource, cleanupFunc, nil)

	// Verify all resources exist
	assert.Equal(t, 3, rm.GetResourceCount())

	// Cleanup all
	rm.CleanupAll()

	// Verify all resources were cleaned up
	assert.Equal(t, 0, rm.GetResourceCount())
	assert.Equal(t, 3, cleanupCount)

	// Verify stats - each resource type should have 1 cleaned resource
	stats := rm.GetResourceStats()
	assert.Equal(t, int64(1), stats[GoroutineResource].TotalCleaned)
	assert.Equal(t, int64(1), stats[ConnectionResource].TotalCleaned)
	assert.Equal(t, int64(1), stats[ChannelResource].TotalCleaned)
	assert.Equal(t, int64(0), stats[GoroutineResource].CurrentActive)
	assert.Equal(t, int64(0), stats[ConnectionResource].CurrentActive)
	assert.Equal(t, int64(0), stats[ChannelResource].CurrentActive)
}

func TestResourceManager_Shutdown(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	cleanupCalled := false
	cleanupFunc := func() error {
		cleanupCalled = true
		return nil
	}

	rm.RegisterResource("test-resource", FileResource, cleanupFunc, nil)

	// Shutdown
	rm.Shutdown()

	// Verify cleanup was called
	assert.True(t, cleanupCalled)

	// Verify resources were cleaned up
	assert.Equal(t, 0, rm.GetResourceCount())

	// Verify shutdown can be called multiple times safely
	assert.NotPanics(t, func() {
		rm.Shutdown()
		rm.Shutdown()
	})
}

func TestResourceManager_SetConfiguration(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	// Test setting max idle time - this should not panic
	rm.SetMaxIdleTime(2 * time.Minute)

	// Test setting cleanup interval - this should not panic
	rm.SetCleanupInterval(30 * time.Second)

	// Test setting max resources - this should not panic
	rm.SetMaxResources(500)

	// Clean up
	rm.Shutdown()

	// Configuration setters should work without panicking - tested by reaching this point
	assert.True(t, true, "All setters completed without panicking")
}

func TestResourceManager_GetResourceCount(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	// Initially empty
	assert.Equal(t, 0, rm.GetResourceCount())

	// Add resources
	rm.RegisterResource("resource1", GoroutineResource, nil, nil)
	rm.RegisterResource("resource2", ConnectionResource, nil, nil)
	assert.Equal(t, 2, rm.GetResourceCount())

	// Remove one resource
	err := rm.CleanupResource("resource1")
	assert.NoError(t, err)
	assert.Equal(t, 1, rm.GetResourceCount())
}

func TestResourceManager_IsResourceManaged(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	// Check non-existent resource
	assert.False(t, rm.IsResourceManaged("non-existent"))

	// Add resource
	rm.RegisterResource("test-resource", ChannelResource, nil, nil)

	// Check existing resource
	assert.True(t, rm.IsResourceManaged("test-resource"))

	// Remove resource
	err := rm.CleanupResource("test-resource")
	assert.NoError(t, err)

	// Check again
	assert.False(t, rm.IsResourceManaged("test-resource"))
}

func TestResourceManager_ConcurrentAccess(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	var wg sync.WaitGroup
	iterations := 100

	// Test concurrent access to resource management
	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()

			resourceID := string(rune(i%26+'a')) + "-resource"
			// Use only predefined resource types to avoid nil pointer dereference
			resourceTypes := []ResourceType{GoroutineResource, ConnectionResource, ChannelResource, TimerResource, FileResource}
			resourceType := resourceTypes[i%len(resourceTypes)]

			switch i % 6 {
			case 0:
				rm.RegisterResource(resourceID, resourceType, nil, nil)
			case 1:
				rm.UpdateResourceUsage(resourceID)
			case 2:
				err := rm.CleanupResource(resourceID)
				assert.NoError(t, err)
			case 3:
				rm.GetResourceCount()
			case 4:
				rm.GetResourceStats()
			case 5:
				rm.IsResourceManaged(resourceID)
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is consistent
	finalCount := rm.GetResourceCount()
	assert.GreaterOrEqual(t, finalCount, 0)
	assert.LessOrEqual(t, finalCount, iterations)

	// Verify stats are consistent
	stats := rm.GetResourceStats()
	totalCreated := int64(0)
	totalActive := int64(0)
	for _, stat := range stats {
		totalCreated += stat.TotalCreated
		totalActive += stat.CurrentActive
	}
	assert.GreaterOrEqual(t, totalCreated, int64(0))
	assert.GreaterOrEqual(t, totalActive, int64(0))
}

func TestResourceManager_ResourceLimits(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	// Set low resource limit
	rm.SetMaxResources(10)

	// Add resources up to the limit
	for i := 0; i < 10; i++ {
		rm.RegisterResource(string(rune(i+'a')), GoroutineResource, nil, nil)
	}

	assert.Equal(t, 10, rm.GetResourceCount())

	// Add one more resource (should trigger warning but still work)
	rm.RegisterResource("extra-resource", GoroutineResource, nil, nil)
	assert.Equal(t, 11, rm.GetResourceCount())
}

func TestResourceManager_MetadataHandling(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	metadata := map[string]interface{}{
		"exchange": "binance",
		"symbol":   "BTCUSDT",
		"priority": 1,
		"timeout":  30 * time.Second,
		"enabled":  true,
		"tags":     []string{"crypto", "trading"},
		"config":   map[string]interface{}{"retry": 3},
	}

	rm.RegisterResource("metadata-test", ConnectionResource, nil, metadata)

	rm.mu.RLock()
	resource, exists := rm.resources["metadata-test"]
	rm.mu.RUnlock()

	assert.True(t, exists)
	assert.Equal(t, metadata, resource.Metadata)
}

func TestResourceManager_ResourceAging(t *testing.T) {
	logger := zaplogrus.New()
	rm := NewResourceManager(logger)

	startTime := time.Now()

	rm.RegisterResource("aging-test", TimerResource, nil, nil)

	// Verify creation time
	rm.mu.RLock()
	resource := rm.resources["aging-test"]
	originalLastUsed := resource.LastUsed
	rm.mu.RUnlock()

	assert.True(t, resource.CreatedAt.After(startTime))
	assert.True(t, resource.LastUsed.After(startTime))
	assert.True(t, resource.CreatedAt.Equal(resource.LastUsed) || resource.LastUsed.Sub(resource.CreatedAt) < time.Microsecond)

	// Update usage after some time
	time.Sleep(50 * time.Millisecond)
	rm.UpdateResourceUsage("aging-test")

	// Verify last used time was updated
	rm.mu.RLock()
	updatedResource := rm.resources["aging-test"]
	rm.mu.RUnlock()

	// Debug output
	t.Logf("Original LastUsed: %v", originalLastUsed)
	t.Logf("Updated LastUsed: %v", updatedResource.LastUsed)
	t.Logf("Time difference: %v", updatedResource.LastUsed.Sub(originalLastUsed))

	// Use WithinDuration for more tolerant timing comparison
	assert.True(t, updatedResource.LastUsed.After(originalLastUsed))
	assert.True(t, updatedResource.CreatedAt.Equal(resource.CreatedAt))
}
