package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
)

type recordingQuestStore struct {
	savedQuest *Quest
}

func (s *recordingQuestStore) SaveQuest(ctx context.Context, quest *Quest) error {
	s.savedQuest = cloneQuestForPersistence(quest)
	return nil
}

func (s *recordingQuestStore) GetQuest(ctx context.Context, id string) (*Quest, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *recordingQuestStore) ListQuests(ctx context.Context, chatID string, status QuestStatus) ([]*Quest, error) {
	return nil, nil
}

func (s *recordingQuestStore) UpdateQuestProgress(ctx context.Context, id string, current int, checkpoint map[string]interface{}) error {
	return nil
}

func (s *recordingQuestStore) UpdateLastExecuted(ctx context.Context, id string, executedAt time.Time) error {
	return nil
}

func (s *recordingQuestStore) SaveAutonomousState(ctx context.Context, state *AutonomousState) error {
	return nil
}

func (s *recordingQuestStore) GetAutonomousState(ctx context.Context, chatID string) (*AutonomousState, error) {
	return &AutonomousState{ChatID: chatID}, nil
}

type contextRecordingQuestStore struct {
	recordingQuestStore
	saveCtx context.Context
}

func (s *contextRecordingQuestStore) SaveQuest(ctx context.Context, quest *Quest) error {
	s.saveCtx = ctx
	s.savedQuest = cloneQuestForPersistence(quest)
	return nil
}

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
			name: "first execution at non-divisible-5 minute",
			quest: &Quest{
				ID:             "test-3",
				Cadence:        CadenceMicro,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 10, 3, 0, 0, time.UTC),
			expectedResult: true,
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
			name: "first execution at non-zero minute",
			quest: &Quest{
				ID:             "test-2",
				Cadence:        CadenceHourly,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
			expectedResult: true,
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
			name: "first execution at non-midnight time",
			quest: &Quest{
				ID:             "test-2",
				Cadence:        CadenceDaily,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            time.Date(2024, 1, 15, 12, 0, 0, 0, time.UTC),
			expectedResult: true,
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
			name: "first execution on Monday midnight",
			quest: &Quest{
				ID:             "test-2",
				Cadence:        CadenceWeekly,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			now:            monday,
			expectedResult: true,
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
	now := time.Now().UTC()

	tests := []struct {
		name     string
		quest    *Quest
		expected bool
	}{
		{
			name: "first onetime execution runs",
			quest: &Quest{
				ID:             "test-1",
				Cadence:        CadenceOnetime,
				Status:         QuestStatusActive,
				LastExecutedAt: nil,
			},
			expected: true,
		},
		{
			name: "already executed onetime quest stays paused",
			quest: &Quest{
				ID:             "test-2",
				Cadence:        CadenceOnetime,
				Status:         QuestStatusActive,
				LastExecutedAt: ptrTime(now),
			},
			expected: false,
		},
		{
			name: "active incomplete goal quest reruns",
			quest: &Quest{
				ID:             "goal-1",
				Type:           QuestTypeGoal,
				Cadence:        CadenceOnetime,
				Status:         QuestStatusActive,
				TargetCount:    100,
				CurrentCount:   40,
				LastExecutedAt: ptrTime(now),
				Checkpoint: map[string]interface{}{
					"goal_reached": false,
				},
			},
			expected: true,
		},
		{
			name: "completed goal quest does not rerun",
			quest: &Quest{
				ID:             "goal-2",
				Type:           QuestTypeGoal,
				Cadence:        CadenceOnetime,
				Status:         QuestStatusActive,
				TargetCount:    100,
				CurrentCount:   100,
				LastExecutedAt: ptrTime(now),
				Checkpoint: map[string]interface{}{
					"goal_reached": true,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, engine.shouldExecute(tt.quest, now))
		})
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

func TestAcquireLock_ReclaimsStaleLockWhenOwnerHeartbeatMissing(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = client.Close() }()

	engine := NewQuestEngineWithRedis(NewInMemoryQuestStore(), client)
	ctx := context.Background()
	lockKey := "test:lock:stale-reclaim"

	if err := client.Set(ctx, lockKey, "stale-owner", 5*time.Minute).Err(); err != nil {
		t.Fatalf("failed to seed stale lock: %v", err)
	}

	ok := engine.acquireLock(ctx, lockKey, 5*time.Minute)
	if !ok {
		t.Fatal("expected stale lock to be reclaimed")
	}

	owner, err := client.Get(ctx, lockKey).Result()
	if err != nil {
		t.Fatalf("failed to read reclaimed lock owner: %v", err)
	}
	if owner != engine.lockOwnerID {
		t.Fatalf("expected reclaimed lock owner %q, got %q", engine.lockOwnerID, owner)
	}
}

func TestAcquireLock_FailsWhenOwnerHeartbeatRefreshFails(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = client.Close() }()

	engine := NewQuestEngineWithRedis(NewInMemoryQuestStore(), client)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lockKey := "test:lock:heartbeat-failure"
	ok := engine.acquireLock(ctx, lockKey, 5*time.Minute)
	if ok {
		t.Fatal("expected lock acquisition to fail when owner heartbeat refresh fails")
	}

	exists, err := client.Exists(context.Background(), lockKey).Result()
	if err != nil {
		t.Fatalf("failed to inspect lock key: %v", err)
	}
	if exists != 0 {
		t.Fatal("expected lock key to remain unset after heartbeat refresh failure")
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

func TestReleaseLock_DoesNotDeleteOtherOwnerLock(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = client.Close() }()

	engine := NewQuestEngineWithRedis(NewInMemoryQuestStore(), client)
	ctx := context.Background()
	lockKey := "test:lock:other-owner"

	if err := client.Set(ctx, lockKey, "different-owner", 5*time.Minute).Err(); err != nil {
		t.Fatalf("failed to seed foreign lock: %v", err)
	}

	engine.releaseLock(ctx, lockKey)

	owner, err := client.Get(ctx, lockKey).Result()
	if err != nil {
		t.Fatalf("expected foreign lock to remain, got error: %v", err)
	}
	if owner != "different-owner" {
		t.Fatalf("expected foreign lock owner to remain unchanged, got %q", owner)
	}
}

func TestStop_KeepsOwnerHeartbeatWhileQuestExecuting(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	defer func() { _ = client.Close() }()

	engine := NewQuestEngineWithRedis(NewInMemoryQuestStore(), client)
	engine.running = true
	engine.executing["quest-1"] = true

	ctx := context.Background()
	if err := engine.refreshQuestLockOwnerHeartbeat(ctx); err != nil {
		t.Fatalf("failed to seed owner heartbeat: %v", err)
	}

	engine.Stop()

	exists, err := client.Exists(ctx, questLockOwnerHeartbeatKey(engine.lockOwnerID)).Result()
	if err != nil {
		t.Fatalf("failed to read owner heartbeat: %v", err)
	}
	if exists != 1 {
		t.Fatal("expected owner heartbeat to remain while a quest is still executing")
	}
}

func TestStart_ReinitializesStopChannelAfterStop(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.Start()
	oldStopCh := engine.stopCh
	engine.Stop()

	engine.Start()
	newStopCh := engine.stopCh
	if newStopCh == nil {
		t.Fatal("expected Start to initialize stopCh")
	}
	if oldStopCh == newStopCh {
		t.Fatal("expected Start to replace a closed stopCh on restart")
	}
	select {
	case <-newStopCh:
		t.Fatal("expected restarted stopCh to remain open")
	default:
	}

	engine.Stop()
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

func TestQuestExecutionStaleAfter(t *testing.T) {
	t.Setenv("NEURATRADE_QUEST_EXECUTION_STALE_SECONDS", "")
	t.Setenv("NEURATRADE_SCALPING_TIMEOUT_SECONDS", "")
	t.Setenv("NEURATRADE_SCALPING_STRUCTURED_RETRIES", "")
	expectedDerived := 90*time.Second + 3*20*time.Second + 45*time.Second
	if got := computeQuestRuntimeBudget().StaleTimeout; got != expectedDerived {
		t.Fatalf("computeQuestRuntimeBudget().StaleTimeout derived default = %s, want %s", got, expectedDerived)
	}

	t.Setenv("NEURATRADE_QUEST_EXECUTION_STALE_SECONDS", "30")
	if got := computeQuestRuntimeBudget().StaleTimeout; got != expectedDerived {
		t.Fatalf("computeQuestRuntimeBudget().StaleTimeout floor clamp = %s, want %s", got, expectedDerived)
	}

	t.Setenv("NEURATRADE_QUEST_EXECUTION_STALE_SECONDS", "480")
	if got := computeQuestRuntimeBudget().StaleTimeout; got != 8*time.Minute {
		t.Fatalf("computeQuestRuntimeBudget().StaleTimeout env = %s, want %s", got, 8*time.Minute)
	}

	t.Setenv("NEURATRADE_QUEST_EXECUTION_STALE_SECONDS", "")
	t.Setenv("NEURATRADE_SCALPING_TIMEOUT_SECONDS", "120")
	t.Setenv("NEURATRADE_SCALPING_STRUCTURED_RETRIES", "3")
	expectedAligned := 120*time.Second + 4*20*time.Second + 45*time.Second
	if got := computeQuestRuntimeBudget().StaleTimeout; got != expectedAligned {
		t.Fatalf("computeQuestRuntimeBudget().StaleTimeout aligned = %s, want %s", got, expectedAligned)
	}
}

func TestTick_ResetsStaleExecutionAndRunsQuest(t *testing.T) {
	t.Setenv("NEURATRADE_QUEST_EXECUTION_STALE_SECONDS", "120")

	engine := NewQuestEngine(NewInMemoryQuestStore())
	executed := make(chan struct{}, 1)
	engine.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		executed <- struct{}{}
		return nil
	})

	quest := &Quest{
		ID:         "stale-quest",
		Name:       "Stale Quest",
		Type:       QuestTypeRoutine,
		Cadence:    CadenceMicro,
		Status:     QuestStatusActive,
		Checkpoint: make(map[string]interface{}),
		Metadata: map[string]string{
			"chat_id":       "123",
			"definition_id": "scalping_execution",
		},
	}
	engine.quests[quest.ID] = quest
	engine.executing[quest.ID] = true
	engine.executionStarts[quest.ID] = time.Now().Add(-10 * time.Minute)

	engine.tick()

	select {
	case <-executed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected stale quest to be executed after stale marker reset")
	}
}

func TestShouldBlockQuestEntryByStateDriftLocked(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	quest := &Quest{
		ID:     "q1",
		Status: QuestStatusActive,
		Metadata: map[string]string{
			"chat_id":       "123",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"state_drift_active": true,
		},
	}
	if !engine.shouldBlockQuestEntryByStateDriftLocked(quest) {
		t.Fatal("expected drift-active checkpoint to block new entries")
	}

	quest.Checkpoint["state_drift_active"] = false
	if engine.shouldBlockQuestEntryByStateDriftLocked(quest) {
		t.Fatal("expected drift-inactive checkpoint to allow entries")
	}
}

func TestGetChatRuntimeDiagnostics_IncludesDriftFields(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.quests["q-drift"] = &Quest{
		ID:     "q-drift",
		Status: QuestStatusActive,
		Metadata: map[string]string{
			"chat_id":       "1082762347",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"state_drift_active":                  true,
			"state_drift_positions":               3,
			"state_drift_last_checked_at":         "2026-02-27T03:31:24Z",
			"state_drift_last_repair_at":          "2026-02-27T03:20:01Z",
			"state_drift_last_clean_reconcile_at": "2026-02-27T03:35:01Z",
			"runtime_entry_gate_reason":           "state drift detected",
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("1082762347")
	active, _ := diag["state_drift_active"].(bool)
	if !active {
		t.Fatal("expected state_drift_active=true in diagnostics")
	}
	count, _ := diag["state_drift_positions"].(int)
	if count != 3 {
		t.Fatalf("expected state_drift_positions=3, got %d", count)
	}
	reason, _ := diag["entry_gate_reason_current"].(string)
	if reason != "state drift detected" {
		t.Fatalf("expected entry_gate_reason_current to be populated, got %q", reason)
	}
	if _, ok := diag["last_drift_repair_at"].(string); !ok {
		t.Fatal("expected last_drift_repair_at in diagnostics")
	}
	if _, ok := diag["last_clean_reconcile_at"].(string); !ok {
		t.Fatal("expected last_clean_reconcile_at in diagnostics")
	}
}

func TestGetChatRuntimeDiagnostics_IncludesRecoveryAndProviderChainFields(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.SetAIProviderChainStats(2, 1)
	engine.quests["q-recovery"] = &Quest{
		ID:     "q-recovery",
		Status: QuestStatusActive,
		Metadata: map[string]string{
			"chat_id":       "chat-recovery",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"recovery_mode":                  "micro_entry",
			"recovery_clean_cycles":          2,
			"recovery_clean_cycles_required": 3,
			"recovery_cycles_to_entry":       1,
			"recovery_gate_eval_at":          "2026-02-27T03:36:01Z",
			"recovery_entry_allowed":         false,
			"entry_gate_type":                "recovery_gate",
			"risk_current_drawdown":          0.08,
			"risk_max_drawdown":              0.41,
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("chat-recovery")
	if mode, _ := diag["recovery_mode"].(string); mode != "micro_entry" {
		t.Fatalf("expected recovery_mode=micro_entry, got %q", mode)
	}
	if cycles, _ := diag["recovery_clean_cycles_current"].(int); cycles != 2 {
		t.Fatalf("expected recovery_clean_cycles_current=2, got %d", cycles)
	}
	if required, _ := diag["recovery_clean_cycles_required"].(int); required != 3 {
		t.Fatalf("expected recovery_clean_cycles_required=3, got %d", required)
	}
	if cyclesToEntry, _ := diag["recovery_cycles_to_entry"].(int); cyclesToEntry != 1 {
		t.Fatalf("expected recovery_cycles_to_entry=1, got %d", cyclesToEntry)
	}
	if evalAt, _ := diag["recovery_gate_eval_at"].(string); evalAt == "" {
		t.Fatal("expected recovery_gate_eval_at to be populated")
	}
	if allowed, _ := diag["recovery_entry_allowed"].(bool); allowed {
		t.Fatal("expected recovery_entry_allowed=false")
	}
	if gateType, _ := diag["entry_gate_type"].(string); gateType != "recovery_gate" {
		t.Fatalf("expected entry_gate_type=recovery_gate, got %q", gateType)
	}
	if drawdown, _ := diag["risk_current_drawdown"].(float64); drawdown != 0.08 {
		t.Fatalf("expected risk_current_drawdown=0.08, got %v", drawdown)
	}
	if usable, _ := diag["provider_chain_usable"].(int); usable != 1 {
		t.Fatalf("expected provider_chain_usable=1, got %d", usable)
	}
	if configured, _ := diag["provider_chain_configured"].(int); configured != 2 {
		t.Fatalf("expected provider_chain_configured=2, got %d", configured)
	}

	aiRuntime, ok := diag["ai_runtime"].(map[string]interface{})
	if !ok {
		t.Fatal("expected ai_runtime map in diagnostics")
	}
	if usable, _ := aiRuntime["provider_chain_usable"].(int); usable != 1 {
		t.Fatalf("expected ai_runtime.provider_chain_usable=1, got %d", usable)
	}
}

func TestGetChatRuntimeDiagnostics_OmitsRecoveryGateEvalAtWhenCheckpointMissing(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.quests["q-recovery"] = &Quest{
		ID:        "q-recovery",
		Status:    QuestStatusActive,
		UpdatedAt: time.Now().UTC(),
		Metadata: map[string]string{
			"chat_id":       "chat-recovery",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"recovery_mode": "micro_entry",
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("chat-recovery")
	if _, exists := diag["recovery_gate_eval_at"]; exists {
		t.Fatalf("expected recovery_gate_eval_at to be omitted when checkpoint field is absent, got %#v", diag["recovery_gate_eval_at"])
	}
}

func TestGetChatRuntimeDiagnostics_OmitsRiskCurrentDrawdownWhenAbsent(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.quests["q-recovery"] = &Quest{
		ID:     "q-recovery",
		Status: QuestStatusActive,
		Metadata: map[string]string{
			"chat_id":       "chat-recovery",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"risk_max_drawdown": 0.41,
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("chat-recovery")
	if _, exists := diag["risk_current_drawdown"]; exists {
		t.Fatalf("expected risk_current_drawdown to be omitted when no checkpoint provides it, got %#v", diag["risk_current_drawdown"])
	}
}

func TestGetChatRuntimeDiagnostics_PrefersActiveScalpingGateFields(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	now := time.Now().UTC()
	engine.quests["q-active"] = &Quest{
		ID:        "q-active",
		Status:    QuestStatusActive,
		UpdatedAt: now.Add(-10 * time.Minute),
		Metadata: map[string]string{
			"chat_id":       "chat-gates",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"recovery_next_condition":        "active recovery",
			"runtime_next_unblock_condition": "active unblock",
			"runtime_entry_gate_reason":      "active reason",
			"entry_gate_type":                "recovery_gate",
		},
	}
	engine.quests["q-paused"] = &Quest{
		ID:        "q-paused",
		Status:    QuestStatusPaused,
		UpdatedAt: now,
		Metadata: map[string]string{
			"chat_id":       "chat-gates",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"recovery_next_condition":        "paused recovery",
			"runtime_next_unblock_condition": "paused unblock",
			"runtime_entry_gate_reason":      "paused reason",
			"entry_gate_type":                "state_drift",
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("chat-gates")
	assert.Equal(t, "active recovery", diag["recovery_next_condition"])
	assert.Equal(t, "active unblock", diag["next_unblock_condition_current"])
	assert.Equal(t, "active reason", diag["entry_gate_reason_current"])
	assert.Equal(t, "recovery_gate", diag["entry_gate_type"])
}

func TestGetChatRuntimeDiagnostics_UsesLatestNonActiveGateFieldsWhenNoActiveScalping(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	now := time.Now().UTC()
	engine.quests["q-older"] = &Quest{
		ID:        "q-older",
		Status:    QuestStatusPaused,
		UpdatedAt: now.Add(-20 * time.Minute),
		Metadata: map[string]string{
			"chat_id":       "chat-gates",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"recovery_next_condition":        "older recovery",
			"runtime_next_unblock_condition": "older unblock",
			"runtime_entry_gate_reason":      "older reason",
			"entry_gate_type":                "recovery_gate",
		},
	}
	engine.quests["q-newer"] = &Quest{
		ID:        "q-newer",
		Status:    QuestStatusPaused,
		UpdatedAt: now,
		Metadata: map[string]string{
			"chat_id":       "chat-gates",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"recovery_next_condition":        "newer recovery",
			"runtime_next_unblock_condition": "newer unblock",
			"runtime_entry_gate_reason":      "newer reason",
			"entry_gate_type":                "runtime_circuit",
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("chat-gates")
	assert.Equal(t, "newer recovery", diag["recovery_next_condition"])
	assert.Equal(t, "newer unblock", diag["next_unblock_condition_current"])
	assert.Equal(t, "newer reason", diag["entry_gate_reason_current"])
	assert.Equal(t, "runtime_circuit", diag["entry_gate_type"])
}

func TestGetChatRuntimeDiagnostics_PrefersNewestExpectancyFields(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	now := time.Now().UTC()
	engine.quests["q-older"] = &Quest{
		ID:        "q-older",
		Status:    QuestStatusPaused,
		UpdatedAt: now.Add(-20 * time.Minute),
		Metadata: map[string]string{
			"chat_id":       "chat-expectancy",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"risk_expectancy":          0.11,
			"risk_expectancy_gross":    0.15,
			"risk_fee_drag_expectancy": 0.04,
		},
	}
	engine.quests["q-newer"] = &Quest{
		ID:        "q-newer",
		Status:    QuestStatusPaused,
		UpdatedAt: now,
		Metadata: map[string]string{
			"chat_id":       "chat-expectancy",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"risk_expectancy":          0.21,
			"risk_expectancy_gross":    0.26,
			"risk_fee_drag_expectancy": 0.05,
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("chat-expectancy")
	assert.Equal(t, 0.21, diag["risk_expectancy"])
	assert.Equal(t, 0.26, diag["risk_expectancy_gross"])
	assert.Equal(t, 0.05, diag["risk_fee_drag_expectancy"])
}

func TestQuestEngine_ExecuteQuestPersistsFinalLocalCheckpointSnapshot(t *testing.T) {
	store := &recordingQuestStore{}
	engine := NewQuestEngine(store)

	staleQuest := &Quest{
		ID:          "q-runtime",
		Name:        "Scalping Executor",
		Type:        QuestTypeRoutine,
		Cadence:     CadenceMicro,
		Status:      QuestStatusActive,
		Checkpoint:  map[string]interface{}{"entry_gate_type": "recovery_gate"},
		Metadata:    map[string]string{"chat_id": "1082762347", "definition_id": "scalping_execution"},
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
		TargetCount: 0,
	}
	liveQuest := &Quest{
		ID:          staleQuest.ID,
		Name:        staleQuest.Name,
		Type:        staleQuest.Type,
		Cadence:     staleQuest.Cadence,
		Status:      staleQuest.Status,
		Checkpoint:  map[string]interface{}{},
		Metadata:    staleQuest.Metadata,
		CreatedAt:   staleQuest.CreatedAt,
		UpdatedAt:   staleQuest.UpdatedAt,
		TargetCount: staleQuest.TargetCount,
	}

	engine.quests[liveQuest.ID] = staleQuest
	engine.executing[liveQuest.ID] = true
	engine.executionStarts[liveQuest.ID] = time.Now().UTC()
	engine.executionLastProgress[liveQuest.ID] = time.Now().UTC()
	engine.executionStage[liveQuest.ID] = questExecutionStageHandler
	engine.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		quest.Checkpoint["entry_gate_type"] = "none"
		quest.Checkpoint["recovery_entry_allowed"] = true
		quest.Checkpoint["risk_current_drawdown"] = 0.0
		return nil
	})

	engine.executeQuest(liveQuest)

	if store.savedQuest == nil {
		t.Fatal("expected final quest snapshot to be persisted")
	}
	if gateType, _ := store.savedQuest.Checkpoint["entry_gate_type"].(string); gateType != "none" {
		t.Fatalf("expected persisted entry_gate_type=none, got %q", gateType)
	}
	if allowed, _ := store.savedQuest.Checkpoint["recovery_entry_allowed"].(bool); !allowed {
		t.Fatal("expected persisted recovery_entry_allowed=true")
	}
	if gateType, _ := engine.quests[liveQuest.ID].Checkpoint["entry_gate_type"].(string); gateType != "none" {
		t.Fatalf("expected in-memory entry_gate_type=none, got %q", gateType)
	}
}

func TestFinalizeQuestExecution_GoalQuestRemainsActiveUntilReached(t *testing.T) {
	store := &recordingQuestStore{}
	engine := NewQuestEngine(store)
	quest := &Quest{
		ID:           "goal-quest",
		Name:         "Fund Growth",
		Type:         QuestTypeGoal,
		Cadence:      CadenceOnetime,
		Status:       QuestStatusActive,
		TargetCount:  100,
		CurrentCount: 40,
		Checkpoint: map[string]interface{}{
			"goal_reached": false,
		},
		Metadata: map[string]string{
			"chat_id":       "chat-goal",
			"definition_id": "fund_growth",
		},
	}

	engine.finalizeQuestExecution(quest, nil)

	if quest.Status != QuestStatusActive {
		t.Fatalf("expected incomplete goal quest to stay active, got %s", quest.Status)
	}
	if quest.CompletedAt != nil {
		t.Fatal("expected incomplete goal quest to remain without completion timestamp")
	}
	if store.savedQuest == nil {
		t.Fatal("expected goal quest snapshot to be persisted")
	}
	if store.savedQuest.Status != QuestStatusActive {
		t.Fatalf("expected persisted goal quest status to remain active, got %s", store.savedQuest.Status)
	}
}

func TestFinalizeQuestExecution_PersistsWithBoundedContext(t *testing.T) {
	store := &contextRecordingQuestStore{}
	engine := NewQuestEngine(store)
	quest := &Quest{
		ID:         "persist-timeout",
		Name:       "Persist Timeout",
		Type:       QuestTypeRoutine,
		Cadence:    CadenceMicro,
		Status:     QuestStatusActive,
		Checkpoint: map[string]interface{}{},
		Metadata: map[string]string{
			"chat_id":       "chat-timeout",
			"definition_id": "scalping_execution",
		},
	}

	before := time.Now()
	engine.finalizeQuestExecution(quest, nil)

	if store.saveCtx == nil {
		t.Fatal("expected SaveQuest context to be captured")
	}
	deadline, ok := store.saveCtx.Deadline()
	if !ok {
		t.Fatal("expected SaveQuest to use a bounded context")
	}
	if deadline.Before(before) || deadline.After(before.Add(defaultQuestStoreWriteTimeout+time.Second)) {
		t.Fatalf("expected SaveQuest deadline near %s, got %s", defaultQuestStoreWriteTimeout, deadline.Sub(before))
	}
}

func TestGetChatRuntimeDiagnostics_IncludesLivenessAndDeadlockFields(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	now := time.Now().UTC()
	engine.quests["q-liveness"] = &Quest{
		ID:     "q-liveness",
		Status: QuestStatusActive,
		Metadata: map[string]string{
			"chat_id":       "chat-liveness",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"runtime_entry_attempts_1h":          2,
			"runtime_entry_attempt_block_reason": "liveness entry-attempt budget reached",
			"runtime_next_unblock_condition":     "Next entry-attempt window opens at 2026-02-27T12:00:00Z",
			"runtime_last_entry_attempt_at":      now.Add(-8 * time.Minute).Format(time.RFC3339),
			"state_drift_signature":              "sync-1|sync-2",
			"state_drift_deadlock_cycles":        6,
			"state_drift_active":                 true,
			"state_drift_positions":              2,
			"runtime_entry_gate_reason":          "state drift detected",
			"entry_gate_type":                    "state_drift",
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("chat-liveness")

	if attempts, _ := diag["entry_attempts_1h"].(int); attempts != 2 {
		t.Fatalf("expected entry_attempts_1h=2, got %d", attempts)
	}
	if blockReason, _ := diag["entry_attempt_block_reason"].(string); blockReason == "" {
		t.Fatal("expected entry_attempt_block_reason to be populated")
	}
	if unblock, _ := diag["next_unblock_condition_current"].(string); unblock == "" {
		t.Fatal("expected next_unblock_condition_current to be populated")
	}
	if signature, _ := diag["drift_signature"].(string); signature != "sync-1|sync-2" {
		t.Fatalf("expected drift_signature=sync-1|sync-2, got %q", signature)
	}
	if deadlockCycles, _ := diag["drift_deadlock_cycles"].(int); deadlockCycles != 6 {
		t.Fatalf("expected drift_deadlock_cycles=6, got %d", deadlockCycles)
	}
	if _, ok := diag["last_entry_attempt_at"].(string); !ok {
		t.Fatal("expected last_entry_attempt_at in diagnostics")
	}
	if minutes, ok := diag["minutes_since_entry_attempt"].(float64); !ok || minutes <= 0 {
		t.Fatalf("expected minutes_since_entry_attempt > 0, got %#v", diag["minutes_since_entry_attempt"])
	}
}

func TestGetChatRuntimeDiagnostics_IncludesScalpingCycleFields(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	lastAttempt := time.Now().UTC().Add(-3 * time.Hour)
	engine.quests["q-cycle"] = &Quest{
		ID:     "q-cycle",
		Status: QuestStatusActive,
		Metadata: map[string]string{
			"chat_id":       "chat-cycle",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"account_tier":                       "micro",
			"effective_min_confidence":           0.65,
			"effective_max_capital_pct":          0.50,
			"effective_max_concurrent_positions": 1,
			"managed_open_positions_effective":   1,
			"candidate_universe_count":           12,
			"candidate_ranked_count":             4,
			"candidate_viable_count":             1,
			"top_candidate_rejections":           []map[string]interface{}{{"symbol": "OPN/USDT", "reason": "confidence_below_effective_threshold", "estimated_confidence": 0.55}},
			"rollout_stage_current":              "shadow",
			"rollout_status_current":             "active",
			"rollout_gate_reason_current":        "strategy_not_live (stage: shadow, status: active)",
			"runtime_last_entry_attempt_at":      lastAttempt.Format(time.RFC3339),
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("chat-cycle")

	if tier, _ := diag["account_tier"].(string); tier != "micro" {
		t.Fatalf("expected account_tier=micro, got %q", tier)
	}
	if minConfidence, _ := diag["effective_min_confidence"].(float64); minConfidence != 0.65 {
		t.Fatalf("expected effective_min_confidence=0.65, got %v", minConfidence)
	}
	if maxCapital, _ := diag["effective_max_capital_pct"].(float64); maxCapital != 0.50 {
		t.Fatalf("expected effective_max_capital_pct=0.50, got %v", maxCapital)
	}
	if maxConcurrent, _ := diag["effective_max_concurrent_positions"].(int); maxConcurrent != 1 {
		t.Fatalf("expected effective_max_concurrent_positions=1, got %d", maxConcurrent)
	}
	if openPositions, _ := diag["managed_open_positions_effective"].(int); openPositions != 1 {
		t.Fatalf("expected managed_open_positions_effective=1, got %d", openPositions)
	}
	if universe, _ := diag["candidate_universe_count"].(int); universe != 12 {
		t.Fatalf("expected candidate_universe_count=12, got %d", universe)
	}
	if viable, _ := diag["candidate_viable_count"].(int); viable != 1 {
		t.Fatalf("expected candidate_viable_count=1, got %d", viable)
	}
	rejections, ok := diag["top_candidate_rejections"].([]map[string]interface{})
	if !ok || len(rejections) != 1 {
		t.Fatalf("expected one top candidate rejection, got %#v", diag["top_candidate_rejections"])
	}
	if reason, _ := rejections[0]["reason"].(string); reason != "confidence_below_effective_threshold" {
		t.Fatalf("expected rejection reason to be propagated, got %q", reason)
	}
	if stage, _ := diag["rollout_stage_current"].(string); stage != "shadow" {
		t.Fatalf("expected rollout_stage_current=shadow, got %q", stage)
	}
	if blocked, _ := diag["progress_blocked"].(bool); !blocked {
		t.Fatal("expected progress_blocked=true")
	}
	if reason, _ := diag["progress_block_reason"].(string); reason == "" {
		t.Fatal("expected progress_block_reason to be populated")
	}
}

func TestGetChatRuntimeDiagnostics_ProgressBlockUsesPolicyEnvOverride(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_PROGRESS_BLOCK_AFTER_MINUTES", "240")

	engine := NewQuestEngine(NewInMemoryQuestStore())
	lastAttempt := time.Now().UTC().Add(-3 * time.Hour)
	engine.quests["q-cycle-progress-env"] = &Quest{
		ID:     "q-cycle-progress-env",
		Status: QuestStatusActive,
		Metadata: map[string]string{
			"chat_id":       "chat-progress-env",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{
			"runtime_last_entry_attempt_at": lastAttempt.Format(time.RFC3339),
		},
	}

	diag := engine.GetChatRuntimeDiagnostics("chat-progress-env")
	if blocked, _ := diag["progress_blocked"].(bool); blocked {
		t.Fatal("expected progress_blocked=false when env override increases block window")
	}
}

func TestSetRiskLockStateWithSource_ExposesSourceInDiagnostics(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.SetRiskLockStateWithSource(true, "portfolio_safety", []string{"portfolio_safety: trading_allowed=false"})

	runtimeDiag := engine.GetRuntimeDiagnostics()
	if source, _ := runtimeDiag["risk_lock_source"].(string); source != "portfolio_safety" {
		t.Fatalf("expected runtime risk_lock_source=portfolio_safety, got %q", source)
	}
	if active, _ := runtimeDiag["risk_lock_active"].(bool); !active {
		t.Fatal("expected runtime risk_lock_active=true")
	}
}

func TestTick_UsesLastProgressForStaleReset(t *testing.T) {
	t.Setenv("NEURATRADE_QUEST_EXECUTION_STALE_SECONDS", "120")

	engine := NewQuestEngine(NewInMemoryQuestStore())
	executed := make(chan struct{}, 1)
	engine.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		executed <- struct{}{}
		return nil
	})

	quest := &Quest{
		ID:         "progress-quest",
		Name:       "Progress Quest",
		Type:       QuestTypeRoutine,
		Cadence:    CadenceMicro,
		Status:     QuestStatusActive,
		Checkpoint: make(map[string]interface{}),
		Metadata: map[string]string{
			"chat_id":       "123",
			"definition_id": "scalping_execution",
		},
	}
	engine.quests[quest.ID] = quest
	engine.executing[quest.ID] = true
	engine.executionStarts[quest.ID] = time.Now().Add(-2 * time.Minute)
	engine.executionLastProgress[quest.ID] = time.Now().Add(-20 * time.Second)
	engine.executionStage[quest.ID] = "handler"

	engine.tick()
	select {
	case <-executed:
		t.Fatal("expected quest to remain in-progress when progress heartbeat is recent")
	case <-time.After(300 * time.Millisecond):
	}
	if !engine.executing[quest.ID] {
		t.Fatal("expected in-progress marker to remain when progress is fresh")
	}

	engine.executionLastProgress[quest.ID] = time.Now().Add(-10 * time.Minute)
	engine.tick()
	select {
	case <-executed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected stale in-progress quest to execute after progress timeout reset")
	}
}

func TestListActiveAutonomousChatIDs(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.autonomousState["chat-a"] = &AutonomousState{ChatID: "chat-a", IsActive: true}
	engine.autonomousState["chat-b"] = &AutonomousState{ChatID: "chat-b", IsActive: false}
	engine.autonomousState["chat-c"] = &AutonomousState{ChatID: "chat-c", IsActive: true}

	ids := engine.ListActiveAutonomousChatIDs()
	if len(ids) != 2 {
		t.Fatalf("expected 2 active chat IDs, got %d", len(ids))
	}
	if ids[0] != "chat-a" || ids[1] != "chat-c" {
		t.Fatalf("unexpected active chat list: %#v", ids)
	}
}

func TestTick_DoesNotResetStaleExecutionWhenLockStillHeld(t *testing.T) {
	t.Setenv("NEURATRADE_QUEST_EXECUTION_STALE_SECONDS", "120")

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	engine := NewQuestEngineWithRedis(NewInMemoryQuestStore(), client)
	executed := make(chan struct{}, 1)
	engine.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		executed <- struct{}{}
		return nil
	})

	quest := &Quest{
		ID:         "lock-held-quest",
		Name:       "Lock Held Quest",
		Type:       QuestTypeRoutine,
		Cadence:    CadenceMicro,
		Status:     QuestStatusActive,
		Checkpoint: make(map[string]interface{}),
		Metadata: map[string]string{
			"chat_id":       "chat-lock",
			"definition_id": "scalping_execution",
		},
	}
	engine.quests[quest.ID] = quest
	engine.executing[quest.ID] = true
	engine.executionStarts[quest.ID] = time.Now().Add(-10 * time.Minute)
	engine.executionLastProgress[quest.ID] = time.Now().Add(-10 * time.Minute)
	engine.executionStage[quest.ID] = questExecutionStageHandler

	lockKey := "quest:lock:" + quest.ID
	if setErr := client.Set(context.Background(), lockKey, "locked", 5*time.Minute).Err(); setErr != nil {
		t.Fatalf("failed to set lock key: %v", setErr)
	}

	engine.tick()

	select {
	case <-executed:
		t.Fatal("expected stale quest not to be rescheduled while lock is still active")
	case <-time.After(300 * time.Millisecond):
	}
	if !engine.executing[quest.ID] {
		t.Fatal("expected in-progress marker to remain when lock is active")
	}
	if held := engine.executionLockHeld[quest.ID]; !held {
		t.Fatal("expected diagnostics to mark lock as held")
	}
}

func TestTick_ResetsExecutionThatExceededTimeoutDespiteFreshHeartbeat(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	executed := make(chan struct{}, 1)
	engine.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		executed <- struct{}{}
		return nil
	})

	quest := &Quest{
		ID:         "timeout-reset-quest",
		Name:       "Timeout Reset Quest",
		Type:       QuestTypeRoutine,
		Cadence:    CadenceMicro,
		Status:     QuestStatusActive,
		Checkpoint: make(map[string]interface{}),
		Metadata: map[string]string{
			"chat_id":       "chat-timeout",
			"definition_id": "scalping_execution",
		},
	}
	engine.quests[quest.ID] = quest
	engine.executing[quest.ID] = true
	engine.executionStarts[quest.ID] = time.Now().Add(-engine.runtimeBudget.ExecutionTimeout - time.Minute)
	engine.executionLastProgress[quest.ID] = time.Now().Add(-10 * time.Second)
	engine.executionStage[quest.ID] = questExecutionStageHandler

	engine.tick()

	select {
	case <-executed:
	case <-time.After(2 * time.Second):
		t.Fatal("expected execution timeout to reset stale quest despite fresh heartbeat")
	}
}

func TestBeginAutonomous_ReusesExistingScalpingQuest(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	existing := &Quest{
		ID:      "existing-scalping-quest",
		Name:    "Scalping Executor",
		Type:    QuestTypeRoutine,
		Cadence: CadenceMicro,
		Status:  QuestStatusPaused,
		Metadata: map[string]string{
			"chat_id":       "chat-777",
			"definition_id": "scalping_execution",
		},
		Checkpoint: map[string]interface{}{},
	}
	engine.quests[existing.ID] = existing

	state, err := engine.BeginAutonomous("chat-777")
	if err != nil {
		t.Fatalf("BeginAutonomous returned error: %v", err)
	}
	if len(state.ActiveQuests) != 1 {
		t.Fatalf("expected 1 active quest, got %d", len(state.ActiveQuests))
	}
	if state.ActiveQuests[0] != existing.ID {
		t.Fatalf("expected reused quest ID %s, got %s", existing.ID, state.ActiveQuests[0])
	}
	if len(engine.quests) != 1 {
		t.Fatalf("expected quest count to stay at 1, got %d", len(engine.quests))
	}
	if engine.quests[existing.ID].Status != QuestStatusActive {
		t.Fatalf("expected reused quest status active, got %s", engine.quests[existing.ID].Status)
	}
}

func TestBeginAutonomous_UsesStoredPaperModeMetadata(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.SetOperationalModeService(&OperationalModeService{
		config: DefaultOperationalModeConfig(),
		states: map[string]*OperationalModeState{
			"paper-chat": {ChatID: "paper-chat", Mode: ModePaper},
		},
	})

	state, err := engine.BeginAutonomous("paper-chat")
	if err != nil {
		t.Fatalf("BeginAutonomous returned error: %v", err)
	}
	quest := engine.quests[state.ActiveQuests[0]]
	if quest.Metadata["execution_mode"] != string(ModePaper) {
		t.Fatalf("expected execution_mode paper, got %q", quest.Metadata["execution_mode"])
	}
	if quest.Metadata["dry_run"] != "true" {
		t.Fatalf("expected dry_run true, got %q", quest.Metadata["dry_run"])
	}
	if quest.Metadata["paper_trading"] != "true" {
		t.Fatalf("expected paper_trading true, got %q", quest.Metadata["paper_trading"])
	}
}

func TestBeginAutonomous_DefaultsToDryWhenModeUnavailable(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())

	state, err := engine.BeginAutonomous("default-chat")
	if err != nil {
		t.Fatalf("BeginAutonomous returned error: %v", err)
	}
	quest := engine.quests[state.ActiveQuests[0]]
	if quest.Metadata["execution_mode"] != string(OpModeDry) {
		t.Fatalf("expected execution_mode dry, got %q", quest.Metadata["execution_mode"])
	}
	if quest.Metadata["dry_run"] != "true" {
		t.Fatalf("expected dry_run true, got %q", quest.Metadata["dry_run"])
	}
	if quest.Metadata["paper_trading"] != "false" {
		t.Fatalf("expected paper_trading false, got %q", quest.Metadata["paper_trading"])
	}
}

func TestBeginAutonomous_PreservesStoredNonLiveModeMetadata(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.SetOperationalModeService(&OperationalModeService{
		config: DefaultOperationalModeConfig(),
		states: map[string]*OperationalModeState{
			"conservative-chat": {ChatID: "conservative-chat", Mode: ModeConservative},
		},
	})

	state, err := engine.BeginAutonomous("conservative-chat")
	if err != nil {
		t.Fatalf("BeginAutonomous returned error: %v", err)
	}
	quest := engine.quests[state.ActiveQuests[0]]
	if quest.Metadata["execution_mode"] != string(ModeConservative) {
		t.Fatalf("expected execution_mode conservative, got %q", quest.Metadata["execution_mode"])
	}
	if quest.Metadata["dry_run"] != "true" {
		t.Fatalf("expected dry_run true, got %q", quest.Metadata["dry_run"])
	}
	if quest.Metadata["paper_trading"] != "false" {
		t.Fatalf("expected paper_trading false, got %q", quest.Metadata["paper_trading"])
	}
}

func TestBeginAutonomous_UsesStoredLiveModeMetadata(t *testing.T) {
	engine := NewQuestEngine(NewInMemoryQuestStore())
	engine.SetOperationalModeService(&OperationalModeService{
		config: DefaultOperationalModeConfig(),
		states: map[string]*OperationalModeState{
			"live-chat": {ChatID: "live-chat", Mode: OpModeLive},
		},
	})

	state, err := engine.BeginAutonomous("live-chat")
	if err != nil {
		t.Fatalf("BeginAutonomous returned error: %v", err)
	}
	quest := engine.quests[state.ActiveQuests[0]]
	if quest.Metadata["execution_mode"] != string(OpModeLive) {
		t.Fatalf("expected execution_mode live, got %q", quest.Metadata["execution_mode"])
	}
	if quest.Metadata["dry_run"] != "false" {
		t.Fatalf("expected dry_run false, got %q", quest.Metadata["dry_run"])
	}
	if quest.Metadata["paper_trading"] != "false" {
		t.Fatalf("expected paper_trading false, got %q", quest.Metadata["paper_trading"])
	}
}

func ptrTime(t time.Time) *time.Time {
	return &t
}
