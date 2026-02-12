package services

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionStreaming_NewActionStreamer(t *testing.T) {
	streamer := NewActionStreamer(nil)
	assert.NotNil(t, streamer)
	assert.NotNil(t, streamer.subscribers)
	assert.Equal(t, 0, streamer.GetSubscriberCount())
}

func TestActionStreaming_SubscribeUnsubscribe(t *testing.T) {
	streamer := NewActionStreamer(nil)

	ch := streamer.Subscribe("test-subscriber")
	assert.NotNil(t, ch)
	assert.Equal(t, 1, streamer.GetSubscriberCount())

	streamer.Unsubscribe("test-subscriber")
	assert.Equal(t, 0, streamer.GetSubscriberCount())
}

func TestActionStreaming_Publish(t *testing.T) {
	streamer := NewActionStreamer(nil)
	ch := streamer.Subscribe("test-subscriber")

	action := StreamingAction{
		ID:        "test-action-1",
		Type:      ActionTypeTrade,
		Title:     "Test Action",
		Timestamp: time.Now().UTC(),
	}

	err := streamer.Publish(context.Background(), action)
	require.NoError(t, err)

	select {
	case received := <-ch:
		assert.Equal(t, action.ID, received.ID)
		assert.Equal(t, action.Type, received.Type)
		assert.Equal(t, action.Title, received.Title)
	case <-time.After(time.Second):
		t.Fatal("Expected to receive action but timed out")
	}
}

func TestActionStreaming_PublishToMultipleSubscribers(t *testing.T) {
	streamer := NewActionStreamer(nil)

	ch1 := streamer.Subscribe("sub1")
	ch2 := streamer.Subscribe("sub2")
	ch3 := streamer.Subscribe("sub3")

	action := StreamingAction{
		ID:    "multi-action",
		Type:  ActionTypeQuestProgress,
		Title: "Multi Subscriber Test",
	}

	err := streamer.Publish(context.Background(), action)
	require.NoError(t, err)

	for i, ch := range []<-chan StreamingAction{ch1, ch2, ch3} {
		select {
		case received := <-ch:
			assert.Equal(t, action.ID, received.ID, "Subscriber %d should receive action", i+1)
		case <-time.After(time.Second):
			t.Fatalf("Subscriber %d timed out", i+1)
		}
	}
}

func TestActionStreaming_GetHistory(t *testing.T) {
	streamer := NewActionStreamer(nil)

	for i := 0; i < 5; i++ {
		action := StreamingAction{
			ID:    string(rune('a' + i)),
			Type:  ActionTypeTrade,
			Title: "History Test",
		}
		err := streamer.Publish(context.Background(), action)
		require.NoError(t, err)
	}

	history := streamer.GetHistory(3)
	assert.Len(t, history, 3)
	assert.Equal(t, "e", history[0].ID)
	assert.Equal(t, "d", history[1].ID)
	assert.Equal(t, "c", history[2].ID)
}

func TestActionStreaming_GetHistoryWithTypeFilter(t *testing.T) {
	streamer := NewActionStreamer(nil)

	actions := []StreamingAction{
		{ID: "trade-1", Type: ActionTypeTrade, Title: "Trade 1"},
		{ID: "quest-1", Type: ActionTypeQuestProgress, Title: "Quest 1"},
		{ID: "trade-2", Type: ActionTypeTrade, Title: "Trade 2"},
		{ID: "risk-1", Type: ActionTypeRiskEvent, Title: "Risk 1"},
		{ID: "trade-3", Type: ActionTypeTrade, Title: "Trade 3"},
	}

	for _, action := range actions {
		err := streamer.Publish(context.Background(), action)
		require.NoError(t, err)
	}

	tradeHistory := streamer.GetHistory(10, ActionTypeTrade)
	assert.Len(t, tradeHistory, 3)
	for _, a := range tradeHistory {
		assert.Equal(t, ActionTypeTrade, a.Type)
	}

	questHistory := streamer.GetHistory(10, ActionTypeQuestProgress)
	assert.Len(t, questHistory, 1)
	assert.Equal(t, "quest-1", questHistory[0].ID)
}

func TestActionStreaming_GetActionByID(t *testing.T) {
	streamer := NewActionStreamer(nil)

	action := StreamingAction{
		ID:    "find-me",
		Type:  ActionTypeArbitrage,
		Title: "Find This Action",
	}

	err := streamer.Publish(context.Background(), action)
	require.NoError(t, err)

	found := streamer.GetActionByID("find-me")
	require.NotNil(t, found)
	assert.Equal(t, "find-me", found.ID)
	assert.Equal(t, ActionTypeArbitrage, found.Type)

	notFound := streamer.GetActionByID("nonexistent")
	assert.Nil(t, notFound)
}

func TestActionStreaming_AutoGenerateID(t *testing.T) {
	streamer := NewActionStreamer(nil)
	ch := streamer.Subscribe("test")

	action := StreamingAction{
		Type:  ActionTypeTrade,
		Title: "No ID Action",
	}

	err := streamer.Publish(context.Background(), action)
	require.NoError(t, err)

	select {
	case received := <-ch:
		assert.NotEmpty(t, received.ID)
		assert.Contains(t, received.ID, "trade-")
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for action")
	}
}

func TestActionStreaming_AutoGenerateTimestamp(t *testing.T) {
	streamer := NewActionStreamer(nil)
	ch := streamer.Subscribe("test")

	before := time.Now().UTC()
	action := StreamingAction{
		ID:    "time-test",
		Type:  ActionTypeTrade,
		Title: "Time Test",
	}

	err := streamer.Publish(context.Background(), action)
	require.NoError(t, err)

	select {
	case received := <-ch:
		assert.False(t, received.Timestamp.IsZero())
		assert.True(t, received.Timestamp.After(before) || received.Timestamp.Equal(before))
	case <-time.After(time.Second):
		t.Fatal("Timed out waiting for action")
	}
}

func TestActionBuilder_FluentInterface(t *testing.T) {
	action := NewActionBuilder(ActionTypeTrade).
		WithID("builder-test").
		WithPriority(PriorityHigh).
		WithStatus(StatusExecuted).
		WithUserID("user-123").
		WithChatID("chat-456").
		WithTitle("Builder Test Action").
		WithDescription("Testing the builder pattern").
		WithData("price", 100.50).
		WithData("symbol", "BTC/USDT").
		WithMetadata("source", "test").
		WithSource("test_suite").
		WithCorrelationID("corr-789").
		WithNotification(true).
		Build()

	assert.Equal(t, "builder-test", action.ID)
	assert.Equal(t, ActionTypeTrade, action.Type)
	assert.Equal(t, PriorityHigh, action.Priority)
	assert.Equal(t, StatusExecuted, action.Status)
	assert.Equal(t, "user-123", action.UserID)
	assert.Equal(t, "chat-456", action.ChatID)
	assert.Equal(t, "Builder Test Action", action.Title)
	assert.Equal(t, "Testing the builder pattern", action.Description)
	assert.Equal(t, 100.50, action.Data["price"])
	assert.Equal(t, "BTC/USDT", action.Data["symbol"])
	assert.Equal(t, "test", action.Metadata["source"])
	assert.Equal(t, "test_suite", action.Source)
	assert.Equal(t, "corr-789", action.CorrelationID)
	assert.True(t, action.RequiresNotification)
}

func TestActionBuilder_Defaults(t *testing.T) {
	action := NewActionBuilder(ActionTypeQuestProgress).Build()

	assert.NotEmpty(t, action.ID)
	assert.Equal(t, ActionTypeQuestProgress, action.Type)
	assert.Equal(t, PriorityNormal, action.Priority)
	assert.Equal(t, StatusExecuted, action.Status)
	assert.False(t, action.RequiresNotification)
	assert.NotNil(t, action.Data)
	assert.NotNil(t, action.Metadata)
	assert.False(t, action.Timestamp.IsZero())
}

func TestActionStreaming_JSON(t *testing.T) {
	action := StreamingAction{
		ID:                   "json-test",
		Type:                 ActionTypeRiskEvent,
		Priority:             PriorityCritical,
		Status:               StatusPending,
		Timestamp:            time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		UserID:               "user-1",
		ChatID:               "chat-1",
		Title:                "Risk Alert",
		Description:          "High risk detected",
		Data:                 map[string]interface{}{"risk_score": 85.5},
		Metadata:             map[string]string{"alert_type": "margin_call"},
		Source:               "risk_monitor",
		CorrelationID:        "corr-1",
		RequiresNotification: true,
		NotificationSent:     false,
	}

	jsonStr, err := action.ToJSON()
	require.NoError(t, err)
	assert.Contains(t, jsonStr, `"id":"json-test"`)
	assert.Contains(t, jsonStr, `"type":"risk_event"`)
	assert.Contains(t, jsonStr, `"priority":"critical"`)
	assert.Contains(t, jsonStr, `"status":"pending"`)
	assert.Contains(t, jsonStr, `"title":"Risk Alert"`)

	parsed, err := ParseActionFromJSON(jsonStr)
	require.NoError(t, err)
	assert.Equal(t, action.ID, parsed.ID)
	assert.Equal(t, action.Type, parsed.Type)
	assert.Equal(t, action.Priority, parsed.Priority)
	assert.Equal(t, action.Title, parsed.Title)
	assert.Equal(t, action.Description, parsed.Description)
}

func TestActionStreaming_HistoryLimit(t *testing.T) {
	streamer := NewActionStreamer(nil)
	streamer.maxHistorySize = 5

	for i := 0; i < 10; i++ {
		action := StreamingAction{
			ID:    string(rune('a' + i)),
			Type:  ActionTypeTrade,
			Title: "Overflow Test",
		}
		err := streamer.Publish(context.Background(), action)
		require.NoError(t, err)
	}

	history := streamer.GetHistory(100)
	assert.Len(t, history, 5)
	assert.Equal(t, "j", history[0].ID)
}

func TestActionStreaming_ConcurrentPublish(t *testing.T) {
	streamer := NewActionStreamer(nil)
	ch := streamer.Subscribe("concurrent-test")

	var wg sync.WaitGroup
	numGoroutines := 5
	numActionsPerRoutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(routineID int) {
			defer wg.Done()
			for j := 0; j < numActionsPerRoutine; j++ {
				action := StreamingAction{
					ID:    string(rune('A'+routineID)) + string(rune('0'+j%10)),
					Type:  ActionTypeTrade,
					Title: "Concurrent Test",
				}
				_ = streamer.Publish(context.Background(), action)
			}
		}(i)
	}

	wg.Wait()

	receivedCount := 0
	timeout := time.After(time.Second)
	for {
		select {
		case <-ch:
			receivedCount++
		case <-timeout:
			goto done
		}
	}
done:

	expected := numGoroutines * numActionsPerRoutine
	assert.GreaterOrEqual(t, receivedCount, 1, "Should receive at least one action in concurrent test")
	assert.LessOrEqual(t, receivedCount, expected, "Should not receive more than total published")
}

func TestActionStreaming_BufferFullHandling(t *testing.T) {
	err := sentry.Init(sentry.ClientOptions{
		Dsn: "",
	})
	require.NoError(t, err)

	streamer := NewActionStreamer(nil)

	smallBufferCh := make(chan StreamingAction, 2)
	streamer.mu.Lock()
	streamer.subscribers["small-buffer"] = smallBufferCh
	streamer.mu.Unlock()

	for i := 0; i < 5; i++ {
		action := StreamingAction{
			ID:    string(rune('A' + i)),
			Type:  ActionTypeTrade,
			Title: "Buffer Overflow Test",
		}
		err := streamer.Publish(context.Background(), action)
		require.NoError(t, err)
	}

	select {
	case <-smallBufferCh:
	default:
	}
}

func TestActionType_Constants(t *testing.T) {
	assert.Equal(t, ActionType("trade"), ActionTypeTrade)
	assert.Equal(t, ActionType("quest_progress"), ActionTypeQuestProgress)
	assert.Equal(t, ActionType("risk_event"), ActionTypeRiskEvent)
	assert.Equal(t, ActionType("fund_milestone"), ActionTypeFundMilestone)
	assert.Equal(t, ActionType("ai_reasoning"), ActionTypeAIReasoning)
	assert.Equal(t, ActionType("arbitrage"), ActionTypeArbitrage)
	assert.Equal(t, ActionType("system_alert"), ActionTypeSystemAlert)
	assert.Equal(t, ActionType("position_update"), ActionTypePositionUpdate)
}

func TestActionPriority_Constants(t *testing.T) {
	assert.Equal(t, ActionPriority("low"), PriorityLow)
	assert.Equal(t, ActionPriority("normal"), PriorityNormal)
	assert.Equal(t, ActionPriority("high"), PriorityHigh)
	assert.Equal(t, ActionPriority("critical"), PriorityCritical)
}

func TestActionStatus_Constants(t *testing.T) {
	assert.Equal(t, ActionStatus("pending"), StatusPending)
	assert.Equal(t, ActionStatus("executed"), StatusExecuted)
	assert.Equal(t, ActionStatus("failed"), StatusFailed)
	assert.Equal(t, ActionStatus("cancelled"), StatusCancelled)
}
