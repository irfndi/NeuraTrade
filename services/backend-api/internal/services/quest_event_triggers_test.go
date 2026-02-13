package services

import (
	"testing"
	"time"
)

func TestEventDrivenQuestSystem_NewSystem(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	if system == nil {
		t.Fatal("expected system to not be nil")
	}

	if len(system.triggers) != 0 {
		t.Errorf("expected no triggers initially, got %d", len(system.triggers))
	}
}

func TestEventDrivenQuestSystem_RegisterTrigger(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	trigger := &QuestTrigger{
		DefinitionID: "test_quest",
		EventType:    QuestEventPriceMovement,
		Cooldown:     time.Minute,
	}

	system.RegisterTrigger("test_trigger", trigger)

	retrieved, ok := system.GetTrigger("test_trigger")
	if !ok {
		t.Fatal("expected trigger to be registered")
	}

	if retrieved.DefinitionID != "test_quest" {
		t.Errorf("expected DefinitionID to be test_quest, got %s", retrieved.DefinitionID)
	}
}

func TestEventDrivenQuestSystem_UnregisterTrigger(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	trigger := &QuestTrigger{
		DefinitionID: "test_quest",
		EventType:    QuestEventPriceMovement,
	}

	system.RegisterTrigger("test_trigger", trigger)
	system.UnregisterTrigger("test_trigger")

	_, ok := system.GetTrigger("test_trigger")
	if ok {
		t.Error("expected trigger to be unregistered")
	}
}

func TestEventDrivenQuestSystem_ListTriggers(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	trigger1 := &QuestTrigger{DefinitionID: "quest1", EventType: QuestEventPriceMovement}
	trigger2 := &QuestTrigger{DefinitionID: "quest2", EventType: QuestEventArbitrageFound}

	system.RegisterTrigger("trigger1", trigger1)
	system.RegisterTrigger("trigger2", trigger2)

	triggers := system.ListTriggers()
	if len(triggers) != 2 {
		t.Errorf("expected 2 triggers, got %d", len(triggers))
	}
}

func TestEventDrivenQuestSystem_EmitEventAsync(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	system.EmitEventAsync(QuestEventPriceMovement, "test-chat", map[string]interface{}{
		"symbol":           "BTC/USDT",
		"price_change_pct": 5.0,
	})

	time.Sleep(100 * time.Millisecond)

	select {
	case event := <-system.events:
		if event == nil {
			t.Error("expected non-nil event")
		}
	default:
	}
}

func TestEventDrivenQuestSystem_CanTrigger_Cooldown(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	trigger := &QuestTrigger{
		DefinitionID: "test_quest",
		EventType:    QuestEventPriceMovement,
		Cooldown:     time.Hour,
		TriggerCount: 0,
	}

	if !system.canTriggerLocked(trigger) {
		t.Error("expected trigger to be allowed with no last trigger")
	}

	now := time.Now().UTC()
	trigger.LastTrigger = &now

	if system.canTriggerLocked(trigger) {
		t.Error("expected trigger to be blocked due to cooldown")
	}
}

func TestEventDrivenQuestSystem_CanTrigger_MaxTriggers(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	trigger := &QuestTrigger{
		DefinitionID: "test_quest",
		EventType:    QuestEventPriceMovement,
		MaxTriggers:  2,
		TriggerCount: 2,
	}

	if system.canTriggerLocked(trigger) {
		t.Error("expected trigger to be blocked due to max triggers")
	}
}

func TestEventDrivenQuestSystem_EvaluateCondition_Equal(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	condition := &TriggerCondition{
		Field:    "symbol",
		Operator: "eq",
		Value:    "BTC/USDT",
	}

	event := &QuestEvent{
		Payload: map[string]interface{}{
			"symbol": "BTC/USDT",
		},
	}

	if !system.evaluateCondition(condition, event) {
		t.Error("expected condition to match")
	}
}

func TestEventDrivenQuestSystem_EvaluateCondition_GreaterThan(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	condition := &TriggerCondition{
		Field:    "price_change_pct",
		Operator: ">=",
		Value:    5.0,
	}

	event := &QuestEvent{
		Payload: map[string]interface{}{
			"price_change_pct": 10.0,
		},
	}

	if !system.evaluateCondition(condition, event) {
		t.Error("expected condition to match")
	}
}

func TestEventDrivenQuestSystem_EvaluateCondition_LessThan(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	condition := &TriggerCondition{
		Field:    "price_change_pct",
		Operator: "<",
		Value:    5.0,
	}

	event := &QuestEvent{
		Payload: map[string]interface{}{
			"price_change_pct": 3.0,
		},
	}

	if !system.evaluateCondition(condition, event) {
		t.Error("expected condition to match")
	}
}

func TestEventDrivenQuestSystem_CreatePriceMovementTrigger(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	triggerID := system.CreatePriceMovementTrigger("test-chat", 5.0)

	if triggerID == "" {
		t.Fatal("expected trigger ID to be returned")
	}

	trigger, ok := system.GetTrigger(triggerID)
	if !ok {
		t.Fatal("expected trigger to be registered")
	}

	if trigger.EventType != QuestEventPriceMovement {
		t.Errorf("expected EventType to be price_movement, got %s", trigger.EventType)
	}

	if trigger.Cooldown != 5*time.Minute {
		t.Errorf("expected Cooldown to be 5m, got %v", trigger.Cooldown)
	}
}

func TestEventDrivenQuestSystem_CreateArbitrageTrigger(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	triggerID := system.CreateArbitrageTrigger("test-chat", 1.5)

	if triggerID == "" {
		t.Fatal("expected trigger ID to be returned")
	}

	trigger, ok := system.GetTrigger(triggerID)
	if !ok {
		t.Fatal("expected trigger to be registered")
	}

	if trigger.EventType != QuestEventArbitrageFound {
		t.Errorf("expected EventType to be arbitrage_found, got %s", trigger.EventType)
	}
}

func TestEventDrivenQuestSystem_CreateTradeCompletedTrigger(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	triggerID := system.CreateTradeCompletedTrigger("test-chat")

	trigger, ok := system.GetTrigger(triggerID)
	if !ok {
		t.Fatal("expected trigger to be registered")
	}

	if trigger.EventType != QuestEventTradeCompleted {
		t.Errorf("expected EventType to be trade_completed, got %s", trigger.EventType)
	}
}

func TestEventDrivenQuestSystem_CreateRiskThresholdTrigger(t *testing.T) {
	store := NewInMemoryQuestStore()
	engine := NewQuestEngine(store)
	system := NewEventDrivenQuestSystem(engine)

	triggerID := system.CreateRiskThresholdTrigger("test-chat", 10.0)

	trigger, ok := system.GetTrigger(triggerID)
	if !ok {
		t.Fatal("expected trigger to be registered")
	}

	if trigger.EventType != QuestEventRiskThreshold {
		t.Errorf("expected EventType to be risk_threshold, got %s", trigger.EventType)
	}

	if trigger.MaxTriggers != 3 {
		t.Errorf("expected MaxTriggers to be 3, got %d", trigger.MaxTriggers)
	}
}

func TestCompareNumbers(t *testing.T) {
	tests := []struct {
		a, b     interface{}
		expected int
	}{
		{5.0, 3.0, 1},
		{3.0, 5.0, -1},
		{5.0, 5.0, 0},
		{5, 3, 1},
		{int64(5), int64(3), 1},
	}

	for _, tt := range tests {
		result := compareNumbers(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("compareNumbers(%v, %v) = %d, expected %d", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input    interface{}
		expected float64
	}{
		{float64(5.5), 5.5},
		{float32(5.5), 5.5},
		{5, 5.0},
		{int64(5), 5.0},
		{int32(5), 5.0},
		{"string", 0.0},
	}

	for _, tt := range tests {
		result := toFloat64(tt.input)
		if result != tt.expected {
			t.Errorf("toFloat64(%v) = %f, expected %f", tt.input, result, tt.expected)
		}
	}
}

func TestQuestEvent_Defaults(t *testing.T) {
	event := &QuestEvent{
		ID:        "test-event",
		Type:      QuestEventPriceMovement,
		Timestamp: time.Now().UTC(),
		ChatID:    "test-chat",
		Payload:   map[string]interface{}{},
	}

	if event.ID != "test-event" {
		t.Errorf("expected ID to be test-event, got %s", event.ID)
	}

	if event.Type != QuestEventPriceMovement {
		t.Errorf("expected Type to be price_movement, got %s", event.Type)
	}
}
