package services

import (
	"fmt"
	"log"
	"sync"
	"time"
)

type QuestEventType string

const (
	QuestEventPriceMovement    QuestEventType = "price_movement"
	QuestEventTradeCompleted   QuestEventType = "trade_completed"
	QuestEventArbitrageFound   QuestEventType = "arbitrage_found"
	QuestEventFundingRateShift QuestEventType = "funding_rate_shift"
	QuestEventBalanceChange    QuestEventType = "balance_change"
	QuestEventRiskThreshold    QuestEventType = "risk_threshold"
	QuestEventVolatilitySpike  QuestEventType = "volatility_spike"
	QuestEventPositionOpened   QuestEventType = "position_opened"
	QuestEventPositionClosed   QuestEventType = "position_closed"
)

type QuestEvent struct {
	ID        string                 `json:"id"`
	Type      QuestEventType         `json:"type"`
	Timestamp time.Time              `json:"timestamp"`
	ChatID    string                 `json:"chat_id"`
	Payload   map[string]interface{} `json:"payload"`
}

type QuestTrigger struct {
	DefinitionID string           `json:"definition_id"`
	ChatID       string           `json:"chat_id"`
	EventType    QuestEventType   `json:"event_type"`
	Condition    TriggerCondition `json:"condition"`
	Cooldown     time.Duration    `json:"cooldown"`
	MaxTriggers  int              `json:"max_triggers"`
	TriggerCount int              `json:"trigger_count"`
	LastTrigger  *time.Time       `json:"last_trigger"`
}

type TriggerCondition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"`
	Value    interface{} `json:"value"`
}

type EventDrivenQuestSystem struct {
	mu       sync.RWMutex
	engine   *QuestEngine
	triggers map[string]*QuestTrigger
	events   chan *QuestEvent
	stopCh   chan struct{}
	running  bool
}

func NewEventDrivenQuestSystem(engine *QuestEngine) *EventDrivenQuestSystem {
	return &EventDrivenQuestSystem{
		engine:   engine,
		triggers: make(map[string]*QuestTrigger),
		events:   make(chan *QuestEvent, 1000),
		stopCh:   make(chan struct{}),
	}
}

func (s *EventDrivenQuestSystem) RegisterTrigger(triggerID string, trigger *QuestTrigger) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.triggers[triggerID] = trigger
}

func (s *EventDrivenQuestSystem) UnregisterTrigger(triggerID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.triggers, triggerID)
}

func (s *EventDrivenQuestSystem) GetTrigger(triggerID string) (*QuestTrigger, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trigger, ok := s.triggers[triggerID]
	return trigger, ok
}

func (s *EventDrivenQuestSystem) ListTriggers() []*QuestTrigger {
	s.mu.RLock()
	defer s.mu.RUnlock()

	triggers := make([]*QuestTrigger, 0, len(s.triggers))
	for _, t := range s.triggers {
		triggers = append(triggers, t)
	}
	return triggers
}

func (s *EventDrivenQuestSystem) EmitEvent(event *QuestEvent) {
	select {
	case s.events <- event:
	default:
		log.Printf("Event queue full, dropping event: %s", event.ID)
	}
}

func (s *EventDrivenQuestSystem) EmitEventAsync(eventType QuestEventType, chatID string, payload map[string]interface{}) {
	event := &QuestEvent{
		ID:        generateEventID(),
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		ChatID:    chatID,
		Payload:   payload,
	}
	go s.EmitEvent(event)
}

func (s *EventDrivenQuestSystem) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	go s.eventLoop()
	log.Println("Event-driven quest system started")
}

func (s *EventDrivenQuestSystem) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stopCh)
	s.running = false
	log.Println("Event-driven quest system stopped")
}

func (s *EventDrivenQuestSystem) eventLoop() {
	for {
		select {
		case <-s.stopCh:
			return
		case event := <-s.events:
			s.processEvent(event)
		}
	}
}

func (s *EventDrivenQuestSystem) processEvent(event *QuestEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for triggerID, trigger := range s.triggers {
		if trigger.EventType != event.Type {
			continue
		}

		if trigger.ChatID != "" && trigger.ChatID != event.ChatID {
			continue
		}

		if !s.canTriggerLocked(trigger) {
			continue
		}

		if !s.evaluateCondition(&trigger.Condition, event) {
			continue
		}

		definitionID := trigger.DefinitionID
		chatID := event.ChatID

		trigger.TriggerCount++
		now := time.Now().UTC()
		trigger.LastTrigger = &now

		s.mu.Unlock()

		quest, err := s.engine.CreateQuest(definitionID, chatID)
		if err != nil {
			log.Printf("Failed to create quest from trigger %s: %v", triggerID, err)
			s.mu.Lock()
			continue
		}

		quest.Status = QuestStatusActive
		quest.Metadata["event_id"] = event.ID
		quest.Metadata["event_type"] = string(event.Type)
		quest.Metadata["trigger_id"] = triggerID

		log.Printf("Quest %s triggered by event %s (type: %s)", quest.ID, event.ID, event.Type)

		s.mu.Lock()
	}
}

func (s *EventDrivenQuestSystem) canTriggerLocked(trigger *QuestTrigger) bool {
	if trigger.MaxTriggers > 0 && trigger.TriggerCount >= trigger.MaxTriggers {
		return false
	}

	if trigger.Cooldown > 0 && trigger.LastTrigger != nil {
		elapsed := time.Since(*trigger.LastTrigger)
		if elapsed < trigger.Cooldown {
			return false
		}
	}

	return true
}

func (s *EventDrivenQuestSystem) evaluateCondition(condition *TriggerCondition, event *QuestEvent) bool {
	if condition == nil {
		return true
	}

	value, ok := event.Payload[condition.Field]
	if !ok {
		return false
	}

	switch condition.Operator {
	case "eq", "==":
		return value == condition.Value
	case "ne", "!=":
		return value != condition.Value
	case "gt", ">":
		return compareNumbers(value, condition.Value) > 0
	case "gte", ">=":
		return compareNumbers(value, condition.Value) >= 0
	case "lt", "<":
		return compareNumbers(value, condition.Value) < 0
	case "lte", "<=":
		return compareNumbers(value, condition.Value) <= 0
	case "exists":
		return ok
	default:
		return false
	}
}

func (s *EventDrivenQuestSystem) CreatePriceMovementTrigger(chatID string, threshold float64) string {
	triggerID := fmt.Sprintf("price_movement_%s_%d", chatID, time.Now().UnixNano())
	trigger := &QuestTrigger{
		DefinitionID: "volatility_watch",
		ChatID:       chatID,
		EventType:    QuestEventPriceMovement,
		Condition: TriggerCondition{
			Field:    "price_change_pct",
			Operator: ">=",
			Value:    threshold,
		},
		Cooldown: 5 * time.Minute,
	}
	s.RegisterTrigger(triggerID, trigger)
	return triggerID
}

func (s *EventDrivenQuestSystem) CreateArbitrageTrigger(chatID string, minProfit float64) string {
	triggerID := fmt.Sprintf("arbitrage_%s_%d", chatID, time.Now().UnixNano())
	trigger := &QuestTrigger{
		DefinitionID: "market_scan",
		ChatID:       chatID,
		EventType:    QuestEventArbitrageFound,
		Condition: TriggerCondition{
			Field:    "profit_pct",
			Operator: ">=",
			Value:    minProfit,
		},
		Cooldown: 1 * time.Minute,
	}
	s.RegisterTrigger(triggerID, trigger)
	return triggerID
}

func (s *EventDrivenQuestSystem) CreateTradeCompletedTrigger(chatID string) string {
	triggerID := fmt.Sprintf("trade_completed_%s_%d", chatID, time.Now().UnixNano())
	trigger := &QuestTrigger{
		DefinitionID: "portfolio_health",
		ChatID:       chatID,
		EventType:    QuestEventTradeCompleted,
		Condition:    TriggerCondition{},
		Cooldown:     30 * time.Second,
	}
	s.RegisterTrigger(triggerID, trigger)
	return triggerID
}

func (s *EventDrivenQuestSystem) CreateRiskThresholdTrigger(chatID string, maxDrawdown float64) string {
	triggerID := fmt.Sprintf("risk_threshold_%s_%d", chatID, time.Now().UnixNano())
	trigger := &QuestTrigger{
		DefinitionID: "volatility_watch",
		ChatID:       chatID,
		EventType:    QuestEventRiskThreshold,
		Condition: TriggerCondition{
			Field:    "drawdown_pct",
			Operator: ">=",
			Value:    maxDrawdown,
		},
		Cooldown:    10 * time.Minute,
		MaxTriggers: 3,
	}
	s.RegisterTrigger(triggerID, trigger)
	return triggerID
}

func generateEventID() string {
	return fmt.Sprintf("evt_%d", time.Now().UnixNano())
}

func compareNumbers(a, b interface{}) int {
	aFloat := toFloat64(a)
	bFloat := toFloat64(b)

	if aFloat < bFloat {
		return -1
	} else if aFloat > bFloat {
		return 1
	}
	return 0
}

func toFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case float32:
		return float64(val)
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case int32:
		return float64(val)
	default:
		return 0
	}
}
