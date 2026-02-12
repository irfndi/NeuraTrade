package services

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestShouldExecute_MicroCadence(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)

	tests := []struct {
		name           string
		quest          *Quest
		now            time.Time
		expectedResult bool
	}{
		{
			name: "first execution at minute 5",
			quest: &Quest{
				ID:             "test-1",
				Cadence:        CadenceMicro,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC),
			expectedResult: true,
		},
		{
			name: "first execution at minute 0",
			quest: &Quest{
				ID:             "test-2",
				Cadence:        CadenceMicro,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			expectedResult: true,
		},
		{
			name: "skip non-divisible-5 minute",
			quest: &Quest{
				ID:             "test-3",
				Cadence:        CadenceMicro,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 10, 3, 0, 0, time.UTC),
			expectedResult: false,
		},
		{
			name: "too soon since last execution",
			quest: &Quest{
				ID:             "test-4",
				Cadence:        CadenceMicro,
				Status:         QuestStatusActive,
				LastExecutedAt: ptrTime(time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)),
			},
			now:            time.Date(2024, 1, 15, 10, 5, 30, 0, time.UTC),
			expectedResult: false,
		},
		{
			name: "5 minutes since last execution",
			quest: &Quest{
				ID:             "test-5",
				Cadence:        CadenceMicro,
				Status:         QuestStatusActive,
				LastExecutedAt: ptrTime(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)),
			},
			now:            time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC),
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.shouldExecute(tt.quest, tt.now)
			if result != tt.expectedResult {
				t.Errorf("shouldExecute() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestShouldExecute_HourlyCadence(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)

	tests := []struct {
		name           string
		quest          *Quest
		now            time.Time
		expectedResult bool
	}{
		{
			name: "first execution at hour start",
			quest: &Quest{
				ID:             "test-1",
				Cadence:        CadenceHourly,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			expectedResult: true,
		},
		{
			name: "skip non-zero minute",
			quest: &Quest{
				ID:             "test-2",
				Cadence:        CadenceHourly,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedResult: false,
		},
		{
			name: "less than 1 hour since last execution",
			quest: &Quest{
				ID:             "test-3",
				Cadence:        CadenceHourly,
				Status:         QuestStatusActive,
				LastExecutedAt: ptrTime(time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)),
			},
			now:            time.Date(2024, 1, 15, 10, 0, 30, 0, time.UTC),
			expectedResult: false,
		},
		{
			name: "exactly 1 hour since last execution",
			quest: &Quest{
				ID:             "test-4",
				Cadence:        CadenceHourly,
				Status:         QuestStatusActive,
				LastExecutedAt: ptrTime(time.Date(2024, 1, 15, 9, 0, 0, 0, time.UTC)),
			},
			now:            time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.shouldExecute(tt.quest, tt.now)
			if result != tt.expectedResult {
				t.Errorf("shouldExecute() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestShouldExecute_DailyCadence(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)

	tests := []struct {
		name           string
		quest          *Quest
		now            time.Time
		expectedResult bool
	}{
		{
			name: "first execution at midnight",
			quest: &Quest{
				ID:             "test-1",
				Cadence:        CadenceDaily,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expectedResult: true,
		},
		{
			name: "skip non-midnight time",
			quest: &Quest{
				ID:             "test-2",
				Cadence:        CadenceDaily,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			expectedResult: false,
		},
		{
			name: "less than 24 hours since last execution",
			quest: &Quest{
				ID:             "test-3",
				Cadence:        CadenceDaily,
				Status:         QuestStatusActive,
				LastExecutedAt: ptrTime(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)),
			},
			now:            time.Date(2024, 1, 15, 0, 0, 30, 0, time.UTC),
			expectedResult: false,
		},
		{
			name: "24 hours since last execution",
			quest: &Quest{
				ID:             "test-4",
				Cadence:        CadenceDaily,
				Status:         QuestStatusActive,
				LastExecutedAt: ptrTime(time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC)),
			},
			now:            time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.shouldExecute(tt.quest, tt.now)
			if result != tt.expectedResult {
				t.Errorf("shouldExecute() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestShouldExecute_WeeklyCadence(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)

	sunday := time.Date(2024, 1, 14, 0, 0, 0, 0, time.UTC)
	if sunday.Weekday() != time.Sunday {
		t.Fatalf("test setup error: expected Sunday, got %s", sunday.Weekday())
	}

	monday := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		quest          *Quest
		now            time.Time
		expectedResult bool
	}{
		{
			name: "first execution on Sunday midnight",
			quest: &Quest{
				ID:             "test-1",
				Cadence:        CadenceWeekly,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            sunday,
			expectedResult: true,
		},
		{
			name: "skip Monday midnight",
			quest: &Quest{
				ID:             "test-2",
				Cadence:        CadenceWeekly,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            monday,
			expectedResult: false,
		},
		{
			name: "less than 7 days since last execution",
			quest: &Quest{
				ID:             "test-3",
				Cadence:        CadenceWeekly,
				Status:         QuestStatusActive,
				LastExecutedAt: ptrTime(sunday),
			},
			now:            time.Date(2024, 1, 14, 0, 0, 30, 0, time.UTC),
			expectedResult: false,
		},
		{
			name: "7 days since last execution",
			quest: &Quest{
				ID:             "test-4",
				Cadence:        CadenceWeekly,
				Status:         QuestStatusActive,
				LastExecutedAt: ptrTime(sunday),
			},
			now:            time.Date(2024, 1, 21, 0, 0, 0, 0, time.UTC),
			expectedResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := engine.shouldExecute(tt.quest, tt.now)
			if result != tt.expectedResult {
				t.Errorf("shouldExecute() = %v, want %v", result, tt.expectedResult)
			}
		})
	}
}

func TestShouldExecute_OnetimeCadence(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)

	quest := &Quest{
		ID:             "test-1",
		Cadence:        CadenceOnetime,
		Status:         QuestStatusActive,
		LastExecutedAt: nil,
	}

	result := engine.shouldExecute(quest, time.Now())
	if result != false {
		t.Errorf("shouldExecute() for onetime cadence should always return false, got %v", result)
	}
}

func TestAcquireLock_WithRedis(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = client.Close() }()

	store := NewInMemoryQuestStore()
	engine := NewQuestEngineWithRedis(store, client)

	ctx := context.Background()

	ok := engine.acquireLock(ctx, "test:lock:1", 5*time.Minute)
	if !ok {
		t.Error("first lock acquisition should succeed")
	}

	ok = engine.acquireLock(ctx, "test:lock:1", 5*time.Minute)
	if ok {
		t.Error("second lock acquisition should fail (lock already held)")
	}

	ok = engine.acquireLock(ctx, "test:lock:2", 5*time.Minute)
	if !ok {
		t.Error("lock acquisition with different key should succeed")
	}
}

func TestAcquireLock_WithoutRedis(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)

	ctx := context.Background()

	ok := engine.acquireLock(ctx, "test:lock:1", 5*time.Minute)
	if !ok {
		t.Error("lock acquisition without Redis should always succeed")
	}

	ok = engine.acquireLock(ctx, "test:lock:1", 5*time.Minute)
	if !ok {
		t.Error("repeated lock acquisition without Redis should still succeed")
	}
}

func TestReleaseLock_WithRedis(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = client.Close() }()

	store := NewInMemoryQuestStore()
	engine := NewQuestEngineWithRedis(store, client)

	ctx := context.Background()
	lockKey := "test:lock:release"

	ok := engine.acquireLock(ctx, lockKey, 5*time.Minute)
	if !ok {
		t.Fatal("first lock acquisition should succeed")
	}

	engine.releaseLock(ctx, lockKey)

	ok = engine.acquireLock(ctx, lockKey, 5*time.Minute)
	if !ok {
		t.Error("lock acquisition after release should succeed")
	}
}

func TestUpdateLastExecuted(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)

	quest := &Quest{
		ID:             "test-1",
		Name:           "Test Quest",
		Cadence:        CadenceMicro,
		Status:         QuestStatusActive,
		LastExecutedAt: nil,
	}
	engine.quests["test-1"] = quest

	executedAt := time.Date(2024, 1, 15, 10, 5, 0, 0, time.UTC)
	engine.updateLastExecuted("test-1", executedAt)

	if quest.LastExecutedAt == nil {
		t.Fatal("LastExecutedAt should be set")
	}
	if !quest.LastExecutedAt.Equal(executedAt) {
		t.Errorf("LastExecutedAt = %v, want %v", *quest.LastExecutedAt, executedAt)
	}
}

func TestNewQuestEngineWithRedis(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngineWithRedis(store, nil)

	if engine == nil {
		t.Fatal("engine should not be nil")
	}
	if engine.redis != nil {
		t.Error("redis should be nil when nil is passed")
	}
	if engine.store == nil {
		t.Error("store should not be nil")
	}
	if len(engine.definitions) == 0 {
		t.Error("default definitions should be registered")
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
