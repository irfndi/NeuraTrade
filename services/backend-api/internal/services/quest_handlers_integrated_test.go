package services

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegratedQuestHandlers_MarketScanWithTA tests market scanning with TA
func TestIntegratedQuestHandlers_MarketScanWithTA(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)

	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, mockNotif, mockMonitoring,
	)

	quest := &Quest{
		ID:           "test-market-scan",
		Name:         "Market Scanner",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Checkpoint:   make(map[string]interface{}),
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := handlers.handleMarketScanWithTA(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_scan_time")
	assert.Contains(t, quest.Checkpoint, "symbols_scanned")
	assert.Contains(t, quest.Checkpoint, "chat_id")
}

// TestIntegratedQuestHandlers_FundingRateScan tests funding rate scanning
func TestIntegratedQuestHandlers_FundingRateScan(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)

	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, mockNotif, mockMonitoring,
	)

	quest := &Quest{
		ID:           "test-funding-scan",
		Name:         "Funding Rate Scanner",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Checkpoint:   make(map[string]interface{}),
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := handlers.handleFundingRateScan(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_funding_scan")
	assert.Contains(t, quest.Checkpoint, "rates_collected")
}

// TestIntegratedQuestHandlers_PortfolioHealthWithRisk tests portfolio health checks
func TestIntegratedQuestHandlers_PortfolioHealthWithRisk(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)

	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, mockNotif, mockMonitoring,
	)

	quest := &Quest{
		ID:           "test-health-check",
		Name:         "Portfolio Health Check",
		Type:         QuestTypeRoutine,
		Status:       QuestStatusActive,
		CurrentCount: 0,
		Checkpoint:   make(map[string]interface{}),
		Metadata:     map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := handlers.handlePortfolioHealthWithRisk(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "last_health_check")
	assert.Contains(t, quest.Checkpoint, "health_status")
	assert.Contains(t, quest.Checkpoint, "checks_passed")
}

// TestIntegratedQuestHandlers_GetMonitoringSnapshot tests monitoring snapshot retrieval
func TestIntegratedQuestHandlers_GetMonitoringSnapshot(t *testing.T) {
	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)

	handlers := NewIntegratedQuestHandlers(
		nil, nil, nil, nil, mockNotif, mockMonitoring,
	)

	snapshot := handlers.GetMonitoringSnapshot("test123")

	assert.NotNil(t, snapshot)
	assert.Contains(t, snapshot, "chat_id")
	assert.Contains(t, snapshot, "uptime_hours")
	assert.Contains(t, snapshot, "total_quests")
}

// TestProductionQuestExecutor tests the production quest executor
func TestProductionQuestExecutor(t *testing.T) {
	mockNotif := &NotificationService{}

	executor := NewProductionQuestExecutor(
		nil, nil, nil, nil, mockNotif,
	)

	assert.NotNil(t, executor)
	assert.NotNil(t, executor.engine)
	assert.NotNil(t, executor.monitoring)

	// Test start
	executor.Start()

	// Test status
	status := executor.GetStatus("test123")
	assert.NotNil(t, status)

	// Test stop
	executor.Stop()
}

// TestQuestEngine_IntegratedHandlerRegistration tests handler registration
func TestQuestEngine_IntegratedHandlerRegistration(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	mockNotif := &NotificationService{}
	mockMonitoring := NewAutonomousMonitorManager(mockNotif)
	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, mockNotif, mockMonitoring)

	// Should not panic
	engine.RegisterIntegratedHandlers(handlers)

	// Verify handlers are registered
	assert.NotNil(t, engine.handlers)
}

// TestQuestEngine_QuestExecutionWithCheckpoint tests quest execution with checkpoints
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

	quest := &Quest{
		ID:         "test-quest",
		Name:       "Test Quest",
		Type:       QuestTypeRoutine,
		Status:     QuestStatusActive,
		Checkpoint: make(map[string]interface{}),
		Metadata:   map[string]string{"chat_id": "test123"},
	}

	ctx := context.Background()
	err := testHandler(ctx, quest)

	assert.NoError(t, err)
	assert.Equal(t, 1, executionCount)
	assert.Equal(t, 1, quest.CurrentCount)
	assert.Contains(t, quest.Checkpoint, "executed_at")
}

// TestQuestEngine_ErrorHandling tests error handling in quest execution
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
}

// TestQuestEngine_MetadataPropagation tests metadata propagation
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
		ID:         "metadata-quest",
		Name:       "Metadata Quest",
		Type:       QuestTypeRoutine,
		Status:     QuestStatusActive,
		Checkpoint: make(map[string]interface{}),
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

// TestQuestEngine_ConcurrentExecution tests concurrent quest execution
func TestQuestEngine_ConcurrentExecution(t *testing.T) {
	engine := NewQuestEngineWithNotification(NewInMemoryQuestStore(), nil, nil)

	var executionCount atomic.Int32
	slowHandler := func(ctx context.Context, quest *Quest) error {
		time.Sleep(10 * time.Millisecond)
		executionCount.Add(1)
		quest.CurrentCount++
		return nil
	}

	engine.RegisterHandler(QuestTypeRoutine, slowHandler)

	quests := make([]*Quest, 5)
	for i := 0; i < 5; i++ {
		quests[i] = &Quest{
			ID:     "concurrent-quest-" + string(rune(i)),
			Name:   "Concurrent Quest",
			Type:   QuestTypeRoutine,
			Status: QuestStatusActive,
		}
	}

	ctx := context.Background()
	done := make(chan bool)
	for _, q := range quests {
		go func(quest *Quest) {
			_ = slowHandler(ctx, quest)
			done <- true
		}(q)
	}

	for i := 0; i < 5; i++ {
		<-done
	}

	assert.Equal(t, int32(5), executionCount.Load())
}

// TestHasExchange tests the hasExchange helper function
func TestHasExchange(t *testing.T) {
	exchanges := []string{"binance", "bybit", "okx"}

	assert.True(t, hasExchange(exchanges, "binance"))
	assert.True(t, hasExchange(exchanges, "bybit"))
	assert.True(t, hasExchange(exchanges, "okx"))
	assert.False(t, hasExchange(exchanges, "kraken"))
	assert.False(t, hasExchange(exchanges, ""))
	assert.False(t, hasExchange([]string{}, "binance"))
}

func TestIntegratedQuestHandlers_GetUserExchange_SQLiteLookup(t *testing.T) {
	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "quest-user-exchange.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	ctx := context.Background()
	_, err = sqliteDB.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS telegram_operator_wallets (
			id TEXT PRIMARY KEY,
			chat_id TEXT NOT NULL,
			provider TEXT NOT NULL,
			status TEXT NOT NULL,
			created_at TIMESTAMP NOT NULL
		)
	`)
	require.NoError(t, err)
	_, err = sqliteDB.Exec(ctx, `
		INSERT INTO telegram_operator_wallets (id, chat_id, provider, status, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`, "w1", "chat-1", "bitget", "connected", time.Now().UTC())
	require.NoError(t, err)

	handlers := NewIntegratedQuestHandlers(nil, nil, nil, nil, nil, nil)
	handlers.SetDB(sqliteDB.DB)

	assert.Equal(t, "bitget", handlers.getUserExchange("chat-1"))
	assert.Equal(t, "bitget", handlers.getUserExchange("chat-missing"))
}

func TestShouldSendScalpingDecisionNotification_DefaultActionableOnly(t *testing.T) {
	t.Setenv("NEURATRADE_TELEGRAM_NOTIFY_AI_DECISIONS", "")
	t.Setenv("NEURATRADE_TELEGRAM_ACTIONABLE_ONLY", "")

	assert.False(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "scalping_cycle",
		Action:       "hold",
	}))

	assert.False(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "scalping",
		Action:       "buy",
	}))

	assert.True(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "pnl_reconciliation",
		Action:       "record",
	}))
}

func TestShouldSendScalpingDecisionNotification_EnvOverrides(t *testing.T) {
	t.Setenv("NEURATRADE_TELEGRAM_ACTIONABLE_ONLY", "false")
	t.Setenv("NEURATRADE_TELEGRAM_NOTIFY_AI_DECISIONS", "")

	assert.True(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "scalping_cycle",
		Action:       "hold",
	}))

	t.Setenv("NEURATRADE_TELEGRAM_ACTIONABLE_ONLY", "true")
	t.Setenv("NEURATRADE_TELEGRAM_NOTIFY_AI_DECISIONS", "true")
	assert.True(t, shouldSendScalpingDecisionNotification(AIReasoningNotification{
		DecisionType: "scalping_cycle",
		Action:       "hold",
	}))
}

// hasExchange checks if a specific exchange exists in the list
func hasExchange(exchanges []string, exchangeName string) bool {
	for _, ex := range exchanges {
		if ex == exchangeName {
			return true
		}
	}
	return false
}
