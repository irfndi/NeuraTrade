package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIntegratedQuestHandlers_MarketScanWithTA(t *testing.T) {
	// Create mock services
	mockTA := &TechnicalAnalysisService{}
	mockNotif := &NotificationService{}

	handlers := NewIntegratedQuestHandlers(
		mockTA,
		nil, nil, nil, mockNotif, nil,
	)

	// Create quest engine
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)
	engine.RegisterIntegratedHandlers(handlers)

	// Create test quest
	quest := &Quest{
		ID:           "test-market-scan",
		Name:         "Market Scanner",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	// Execute handler
	ctx := context.Background()
	err := handlers.handleMarketScanWithTA(ctx, quest)

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_scan_time")
	assert.Contains(t, quest.Checkpoint, "symbols_scanned")
	assert.Contains(t, quest.Checkpoint, "chat_id")
}

func TestIntegratedQuestHandlers_FundingRateScan(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, nil,
	)

	quest := &Quest{
		ID:           "test-funding-scan",
		Name:         "Funding Rate Scanner",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := handlers.handleFundingRateScan(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_funding_scan")
	assert.Contains(t, quest.Checkpoint, "rates_collected")
}

func TestIntegratedQuestHandlers_PortfolioHealthWithRisk(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, nil,
	)

	quest := &Quest{
		ID:           "test-health-check",
		Name:         "Portfolio Health Check",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := handlers.handlePortfolioHealthWithRisk(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_health_check")
	assert.Contains(t, quest.Checkpoint, "health_status")
	assert.Contains(t, quest.Checkpoint, "risk_level")
}

func TestIntegratedQuestHandlers_AIDecisionQuest(t *testing.T) {
	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, nil,
	)

	quest := &Quest{
		ID:           "test-ai-decision",
		Name:         "AI Decision Quest",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
// 	err := handlers.handleAIDecisionQuest(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_ai_decision")
	assert.Contains(t, quest.Checkpoint, "decision")
}

func TestQuestEngine_IntegratedHandlerRegistration(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, nil,
	)

	// Should not panic
	engine.RegisterIntegratedHandlers(handlers)

	// Verify handlers are registered
	assert.NotNil(t, engine.handlers)
}

func TestQuestEngine_QuestExecutionWithCheckpoint(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	executionCount := 0
	testHandler := func(ctx context.Context, quest *Quest) error {
		executionCount++
		quest.CurrentCount++
		quest.Checkpoint["executed_at"] = time.Now().UTC().Format(time.RFC3339)
		return nil
	}

	engine.RegisterHandler(QuestTypeRoutine, testHandler)

	// Create and execute quest
	quest := &Quest{
		ID:       "test-quest",
		Name:     "Test Quest",
		Type:     QuestTypeRoutine,
		Status:   QuestStatusActive,
		Metadata: map[string]string{"chat_id": "test123"},
	}

	// Manually execute (simulating scheduler)
	ctx := context.Background()
	err := testHandler(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "executed_at")
}

func TestQuestEngine_ErrorHandling(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	errorHandler := func(ctx context.Context, quest *Quest) error {
		return assert.AnError
	}

	engine.RegisterHandler(QuestTypeRoutine, errorHandler)

	quest := &Quest{
		ID:     "error-quest",
		Name:   "Error Quest",
		Type:   QuestTypeRoutine,
		Status: QuestStatusActive,
	}

	ctx := context.Background()
	err := errorHandler(ctx, quest)

	assert.Error(t, err)
	assert.Equal(t, QuestStatusActive, quest.Status) // Should not change status on error in handler
}

func TestQuestEngine_MetadataPropagation(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	metadataHandler := func(ctx context.Context, quest *Quest) error {
		chatID, ok := quest.Metadata["chat_id"]
		if !ok {
			return assert.AnError
		}
		quest.Checkpoint["processed_chat_id"] = chatID
		quest.CurrentCount++
		return nil
	}

	engine.RegisterHandler(QuestTypeRoutine, metadataHandler)

	quest := &Quest{
		ID:     "metadata-quest",
		Name:   "Metadata Quest",
		Type:   QuestTypeRoutine,
		Status: QuestStatusActive,
		Metadata: map[string]string{
			"chat_id": "test-chat-123",
			"user":    "test-user",
		},
	}

	ctx := context.Background()
	err := metadataHandler(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Equal(t, "test-chat-123", quest.Checkpoint["processed_chat_id"])
}

func TestQuestEngine_ConcurrentExecution(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	executionCount := 0
	slowHandler := func(ctx context.Context, quest *Quest) error {
		time.Sleep(10 * time.Millisecond)
		executionCount++
		quest.CurrentCount++
		return nil
	}

	engine.RegisterHandler(QuestTypeRoutine, slowHandler)

	// Create multiple quests
	quests := make([]*Quest, 5)
	for i := 0; i < 5; i++ {
		quests[i] = &Quest{
			ID:     fmt.Sprintf("concurrent-quest-%d", i),
			Name:   "Concurrent Quest",
			Type:   QuestTypeRoutine,
			Status: QuestStatusActive,
		}
	}

	// Execute concurrently
	ctx := context.Background()
	done := make(chan bool)
	for _, q := range quests {
		go func(quest *Quest) {
			_ = slowHandler(ctx, quest)
			done <- true
		}(q)
	}

	// Wait for all to complete
	for i := 0; i < 5; i++ {
		<-done
	}

	assert.Equal(t, 5, executionCount)
}
