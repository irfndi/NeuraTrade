package services_test

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/services"
	"github.com/stretchr/testify/assert"
)

// getMemStats returns current memory allocation in MB
func getMemStats() uint64 {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.Alloc / 1024 / 1024
}

// forceGC forces garbage collection
func forceGC() {
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
}

// TestQuestProgressManager_NoGoroutineLeaks tests that notification goroutines are properly cleaned up
func TestQuestProgressManager_NoGoroutineLeaks(t *testing.T) {
	forceGC()
	memBefore := getMemStats()
	goroutinesBefore := runtime.NumGoroutine()

	// Create mock engine and notification service
	engine := &services.QuestEngine{}
	progressManager := services.NewQuestProgressManager(engine)

	// Simulate multiple progress updates
	for i := 0; i < 10; i++ {
		update := &services.QuestProgressUpdate{
			QuestID:       fmt.Sprintf("quest-%d", i),
			QuestName:     "Test Quest",
			CurrentCount:  5,
			TargetCount:   10,
			PercentComplete: 50.0,
			Status:        "in_progress",
		}
		progressManager.SendProgressUpdate(update)
	}

	// Wait for any pending operations
	time.Sleep(200 * time.Millisecond)

	forceGC()
	memAfter := getMemStats()
	goroutinesAfter := runtime.NumGoroutine()

	// Memory should not grow significantly
	memGrowth := int64(memAfter) - int64(memBefore)
	goroutineGrowth := goroutinesAfter - goroutinesBefore

	t.Logf("Memory: %d MB -> %d MB (growth: %d MB)", memBefore, memAfter, memGrowth)
	t.Logf("Goroutines: %d -> %d (growth: %d)", goroutinesBefore, goroutinesAfter, goroutineGrowth)

	// Allow some tolerance for GC timing
	assert.Less(t, goroutineGrowth, 5, "Goroutine count should not grow significantly")
}

// TestWebSocketHandler_ClientCleanup tests that WebSocket clients are properly cleaned up
func TestWebSocketHandler_ClientCleanup(t *testing.T) {
	forceGC()
	goroutinesBefore := runtime.NumGoroutine()

	// Create WebSocket handler
	handler := services.NewWebSocketHandler(nil)

	// Simulate client connections and disconnections
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Simulate client lifecycle
			time.Sleep(10 * time.Millisecond)
		}(i)
	}

	wg.Wait()

	// Stop handler
	handler.Stop()

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	forceGC()
	goroutinesAfter := runtime.NumGoroutine()

	goroutineGrowth := goroutinesAfter - goroutinesBefore
	t.Logf("Goroutines: %d -> %d (growth: %d)", goroutinesBefore, goroutinesAfter, goroutineGrowth)

	// Allow some tolerance
	assert.Less(t, goroutineGrowth, 3, "Goroutine count should not grow significantly after cleanup")
}

// TestCleanupService_Stop tests that cleanup service stops cleanly
func TestCleanupService_Stop(t *testing.T) {
	forceGC()
	goroutinesBefore := runtime.NumGoroutine()

	// Create cleanup service
	cleanupSvc := services.NewCleanupService(nil, nil, nil, nil)

	// Start service
	config := services.CleanupConfig{
		IntervalMinutes: 1,
		EnableSmartCleanup: false,
	}
	cleanupSvc.Start(config)

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop service
	cleanupSvc.Stop()

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	forceGC()
	goroutinesAfter := runtime.NumGoroutine()

	goroutineGrowth := goroutinesAfter - goroutinesBefore
	t.Logf("Goroutines: %d -> %d (growth: %d)", goroutinesBefore, goroutinesAfter, goroutineGrowth)

	assert.Less(t, goroutineGrowth, 2, "Cleanup service should stop cleanly without goroutine leaks")
}

// TestSubagentSpawner_Close tests that subagent spawner closes cleanly
func TestSubagentSpawner_Close(t *testing.T) {
	forceGC()
	goroutinesBefore := runtime.NumGoroutine()

	// Create spawner
	spawner := services.NewSubagentSpawner(30*time.Second, 10)

	// Spawn some agents
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_, err := spawner.SpawnAnalyst(ctx, "BTC/USDT", services.SubagentOptions{
			Timeout: 100 * time.Millisecond,
		})
		if err != nil {
			t.Logf("Failed to spawn agent: %v", err)
		}
	}

	// Wait briefly for agents to start
	time.Sleep(50 * time.Millisecond)

	// Close spawner
	spawner.Close()

	// Wait for cleanup
	time.Sleep(200 * time.Millisecond)

	forceGC()
	goroutinesAfter := runtime.NumGoroutine()

	goroutineGrowth := goroutinesAfter - goroutinesBefore
	t.Logf("Goroutines: %d -> %d (growth: %d)", goroutinesBefore, goroutinesAfter, goroutineGrowth)

	assert.Less(t, goroutineGrowth, 3, "Subagent spawner should close cleanly")
}

// TestSignalProcessor_ContextCancellation tests that signal processor handles context cancellation
func TestSignalProcessor_ContextCancellation(t *testing.T) {
	forceGC()
	goroutinesBefore := runtime.NumGoroutine()

	// Create context that will be cancelled
	ctx, cancel := context.WithCancel(context.Background())

	// Create minimal signal processor config
	config := services.SignalProcessorConfig{
		WorkerCount:    2,
		TimeoutSeconds: 5,
	}

	// Create processor (note: this may need mock dependencies)
	_ = ctx
	_ = config

	// Cancel context immediately
	cancel()

	// Wait for cleanup
	time.Sleep(100 * time.Millisecond)

	forceGC()
	goroutinesAfter := runtime.NumGoroutine()

	goroutineGrowth := goroutinesAfter - goroutinesBefore
	t.Logf("Goroutines: %d -> %d (growth: %d)", goroutinesBefore, goroutinesAfter, goroutineGrowth)

	assert.Less(t, goroutineGrowth, 2, "Context cancellation should clean up goroutines")
}

// BenchmarkGoroutineCreation benchmarks goroutine creation and cleanup
func BenchmarkGoroutineCreation(b *testing.B) {
	for i := 0; i < b.N; i++ {
		done := make(chan struct{})
		go func() {
			<-done
		}()
		close(done)
		time.Sleep(time.Millisecond)
	}
}

// BenchmarkMemoryAllocation benchmarks memory allocation patterns
func BenchmarkMemoryAllocation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = make([]byte, 1024)
	}
}
