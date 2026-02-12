package workerpool

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 5
	cfg.QueueSize = 50

	pool := New(cfg)
	require.NotNil(t, pool)
	assert.Equal(t, 5, pool.workers)
	assert.Equal(t, 50, cap(pool.taskQueue))
	assert.False(t, pool.running)
}

func TestPool_StartStop(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	pool := New(cfg)
	err := pool.Start()
	require.NoError(t, err)
	assert.True(t, pool.IsRunning())

	err = pool.Start()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	err = pool.Stop()
	require.NoError(t, err)
	assert.False(t, pool.IsRunning())
}

func TestPool_Submit(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	pool := New(cfg)
	err := pool.Start()
	require.NoError(t, err)
	defer func() { _ = pool.Stop() }()

	done := make(chan bool, 1)
	task := Task{
		ID: "test-task",
		Execute: func() error {
			done <- true
			return nil
		},
	}

	err = pool.Submit(task)
	require.NoError(t, err)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not execute")
	}
}

func TestPool_Submit_NotRunning(t *testing.T) {
	cfg := DefaultConfig()
	pool := New(cfg)

	task := Task{
		ID:      "test-task",
		Execute: func() error { return nil },
	}

	err := pool.Submit(task)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
}

func TestPool_SubmitAsync(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	pool := New(cfg)
	err := pool.Start()
	require.NoError(t, err)
	defer func() { _ = pool.Stop() }()

	task := Task{
		ID: "async-task",
		Execute: func() error {
			return nil
		},
	}

	resultCh, err := pool.SubmitAsync(task)
	require.NoError(t, err)
	require.NotNil(t, resultCh)

	select {
	case result := <-resultCh:
		assert.Equal(t, "async-task", result.TaskID)
		assert.NoError(t, result.Error)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive result")
	}
}

func TestPool_DropOnFull(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 0
	cfg.QueueSize = 2
	cfg.DropOnFull = true

	pool := New(cfg)
	err := pool.Start()
	require.NoError(t, err)
	defer func() { _ = pool.Stop() }()

	for i := 0; i < 3; i++ {
		err = pool.Submit(Task{
			ID:      string(rune('a' + i)),
			Execute: func() error { return nil },
		})
		if i < 2 {
			require.NoError(t, err)
		}
	}

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dropped")
}

func TestPool_GetQueueDepth(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Workers = 1
	cfg.QueueSize = 10

	pool := New(cfg)
	err := pool.Start()
	require.NoError(t, err)
	defer func() { _ = pool.Stop() }()

	for i := 0; i < 5; i++ {
		err = pool.Submit(Task{
			ID:      string(rune('a' + i)),
			Execute: func() error { return nil },
		})
		require.NoError(t, err)
	}

	depth := pool.GetQueueDepth()
	assert.GreaterOrEqual(t, depth, 0)
}

func TestPoolManager(t *testing.T) {
	pm := NewPoolManager()
	require.NotNil(t, pm)

	cfg := DefaultConfig()
	cfg.Workers = 2
	cfg.QueueSize = 10

	pool, err := pm.CreatePool("test-pool", cfg)
	require.NoError(t, err)
	require.NotNil(t, pool)

	_, exists := pm.GetPool("test-pool")
	assert.True(t, exists)

	_, exists = pm.GetPool("nonexistent")
	assert.False(t, exists)

	names := pm.GetPoolNames()
	assert.Contains(t, names, "test-pool")

	stats := pm.GetPoolStats()
	assert.Contains(t, stats, "test-pool")

	err = pm.StopPool("test-pool")
	require.NoError(t, err)

	_, exists = pm.GetPool("test-pool")
	assert.False(t, exists)
}

func TestPoolManager_CreateDuplicate(t *testing.T) {
	pm := NewPoolManager()
	cfg := DefaultConfig()

	_, err := pm.CreatePool("duplicate", cfg)
	require.NoError(t, err)

	_, err = pm.CreatePool("duplicate", cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestPoolManager_StopAll(t *testing.T) {
	pm := NewPoolManager()
	cfg := DefaultConfig()

	_, err := pm.CreatePool("pool1", cfg)
	require.NoError(t, err)

	_, err = pm.CreatePool("pool2", cfg)
	require.NoError(t, err)

	err = pm.StopAll()
	require.NoError(t, err)

	names := pm.GetPoolNames()
	assert.Empty(t, names)
}
