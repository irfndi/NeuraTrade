package services

import (
	"encoding/json"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestNewTradingEventBus(t *testing.T) {
	logger := slog.Default()
	bus := NewTradingEventBus(nil, 100, logger)

	if bus == nil {
		t.Fatal("expected non-nil TradingEventBus")
	}

	if bus.subscriptions == nil {
		t.Error("expected subscriptions map to be initialized")
	}

	if bus.channels == nil {
		t.Error("expected channels map to be initialized")
	}

	if bus.bufferSize != 100 {
		t.Errorf("expected bufferSize 100, got %d", bus.bufferSize)
	}
}

func TestTradingEventBus_Subscribe(t *testing.T) {
	bus := NewTradingEventBus(nil, 10, nil)

	var mu sync.Mutex
	handlerCalled := false
	handler := func(event *TradingEvent) {
		mu.Lock()
		handlerCalled = true
		mu.Unlock()
	}

	subID := bus.Subscribe(EventPriceUpdate, handler)
	if subID == "" {
		t.Error("expected non-empty subscription ID")
	}

	count := bus.GetSubscriptionCount(EventPriceUpdate)
	if count != 1 {
		t.Errorf("expected 1 subscription, got %d", count)
	}

	event := &TradingEvent{
		Type:      EventPriceUpdate,
		Timestamp: time.Now(),
	}
	bus.Publish(event)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if !handlerCalled {
		t.Error("expected handler to be called")
	}
	mu.Unlock()

	bus.Close()
}

func TestTradingEventBus_Unsubscribe(t *testing.T) {
	bus := NewTradingEventBus(nil, 10, nil)

	handler := func(event *TradingEvent) {}
	subID := bus.Subscribe(EventPriceUpdate, handler)

	bus.Unsubscribe(EventPriceUpdate, subID)

	count := bus.GetSubscriptionCount(EventPriceUpdate)
	if count != 0 {
		t.Errorf("expected 0 subscriptions after unsubscribe, got %d", count)
	}

	bus.Close()
}

func TestTradingEventBus_MultipleSubscribers(t *testing.T) {
	bus := NewTradingEventBus(nil, 10, nil)

	var mu sync.Mutex
	callCount := 0

	handler1 := func(event *TradingEvent) {
		mu.Lock()
		callCount++
		mu.Unlock()
	}

	handler2 := func(event *TradingEvent) {
		mu.Lock()
		callCount++
		mu.Unlock()
	}

	bus.Subscribe(EventPriceUpdate, handler1)
	bus.Subscribe(EventPriceUpdate, handler2)

	event := &TradingEvent{
		Type:      EventPriceUpdate,
		Timestamp: time.Now(),
	}
	bus.Publish(event)

	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if callCount != 2 {
		t.Errorf("expected 2 handler calls, got %d", callCount)
	}
	mu.Unlock()

	bus.Close()
}

func TestTradingEventBus_PublishToNonSubscribedEvent(t *testing.T) {
	bus := NewTradingEventBus(nil, 10, nil)

	handlerCalled := false
	handler := func(event *TradingEvent) {
		handlerCalled = true
	}

	bus.Subscribe(EventPriceUpdate, handler)

	event := &TradingEvent{
		Type:      EventArbitrageFound,
		Timestamp: time.Now(),
	}
	bus.Publish(event)

	time.Sleep(50 * time.Millisecond)
	if handlerCalled {
		t.Error("handler should not be called for non-subscribed event type")
	}

	bus.Close()
}

func TestTradingEventBus_SubscribeToChannel(t *testing.T) {
	bus := NewTradingEventBus(nil, 10, nil)

	ch, err := bus.SubscribeToChannel(EventPriceUpdate)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	time.Sleep(50 * time.Millisecond)

	bus.Subscribe(EventPriceUpdate, func(e *TradingEvent) {})

	bus.Publish(&TradingEvent{
		Type:      EventPriceUpdate,
		Timestamp: time.Now(),
		Payload: PriceEvent{
			Symbol:    "BTC/USDT",
			Exchange:  "binance",
			Price:     decimal.NewFromFloat(50000),
			Timestamp: time.Now(),
		},
	})

	time.Sleep(100 * time.Millisecond)

	bus.Close()
}

func TestTradingEventBus_PanicRecovery(t *testing.T) {
	logger := slog.Default()
	bus := NewTradingEventBus(nil, 10, logger)

	panicHandler := func(event *TradingEvent) {
		panic("intentional panic")
	}

	bus.Subscribe(EventPriceUpdate, panicHandler)

	event := &TradingEvent{
		Type:      EventPriceUpdate,
		Timestamp: time.Now(),
	}

	bus.Publish(event)

	time.Sleep(50 * time.Millisecond)

	count := bus.GetSubscriptionCount(EventPriceUpdate)
	if count != 1 {
		t.Errorf("expected subscription to still exist after panic, got %d", count)
	}

	bus.Close()
}

func TestTradingEventBus_GetSubscriptionCount(t *testing.T) {
	bus := NewTradingEventBus(nil, 10, nil)

	count := bus.GetSubscriptionCount(EventPriceUpdate)
	if count != 0 {
		t.Errorf("expected 0 for non-existent subscription, got %d", count)
	}

	bus.Subscribe(EventPriceUpdate, func(e *TradingEvent) {})
	bus.Subscribe(EventPriceUpdate, func(e *TradingEvent) {})

	count = bus.GetSubscriptionCount(EventPriceUpdate)
	if count != 2 {
		t.Errorf("expected 2 subscriptions, got %d", count)
	}

	count = bus.GetSubscriptionCount(EventArbitrageFound)
	if count != 0 {
		t.Errorf("expected 0 for non-subscribed event type, got %d", count)
	}

	bus.Close()
}

func TestTradingEventBus_Close(t *testing.T) {
	bus := NewTradingEventBus(nil, 10, nil)

	bus.Subscribe(EventPriceUpdate, func(e *TradingEvent) {})
	bus.Subscribe(EventArbitrageFound, func(e *TradingEvent) {})

	err := bus.Close()
	if err != nil {
		t.Errorf("unexpected error on close: %v", err)
	}

	count := bus.GetSubscriptionCount(EventPriceUpdate)
	if count != 0 {
		t.Errorf("expected 0 subscriptions after close, got %d", count)
	}
}

func TestPriceEventSerialization(t *testing.T) {
	event := PriceEvent{
		Symbol:    "BTC/USDT",
		Exchange:  "binance",
		Price:     decimal.NewFromFloat(50000.12345),
		Volume:    decimal.NewFromFloat(100.5),
		Timestamp: time.Now().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled PriceEvent
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.Symbol != event.Symbol {
		t.Errorf("expected Symbol %s, got %s", event.Symbol, unmarshaled.Symbol)
	}

	if !unmarshaled.Price.Equal(event.Price) {
		t.Errorf("expected Price %s, got %s", event.Price, unmarshaled.Price)
	}
}

func TestSignalEventSerialization(t *testing.T) {
	event := SignalEvent{
		SignalID:   "sig-123",
		Symbol:     "ETH/USDT",
		SignalType: "arbitrage",
		Action:     "buy",
		Confidence: 0.85,
		Timestamp:  time.Now().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled SignalEvent
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.SignalID != event.SignalID {
		t.Errorf("expected SignalID %s, got %s", event.SignalID, unmarshaled.SignalID)
	}

	if unmarshaled.Confidence != event.Confidence {
		t.Errorf("expected Confidence %f, got %f", event.Confidence, unmarshaled.Confidence)
	}
}

func TestArbitrageEventSerialization(t *testing.T) {
	event := ArbitrageEvent{
		OpportunityID: "arb-456",
		Symbol:        "BTC/USDT",
		BuyExchange:   "binance",
		SellExchange:  "kraken",
		ProfitPercent: decimal.NewFromFloat(2.5),
		Volume:        decimal.NewFromFloat(1000),
		Timestamp:     time.Now().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled ArbitrageEvent
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.OpportunityID != event.OpportunityID {
		t.Errorf("expected OpportunityID %s, got %s", event.OpportunityID, unmarshaled.OpportunityID)
	}
}

func TestFillEventSerialization(t *testing.T) {
	event := FillEvent{
		OrderID:   "order-789",
		Symbol:    "BTC/USDT",
		Side:      "buy",
		Price:     decimal.NewFromFloat(50000),
		Quantity:  decimal.NewFromFloat(0.5),
		Fee:       decimal.NewFromFloat(12.5),
		Timestamp: time.Now().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled FillEvent
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.OrderID != event.OrderID {
		t.Errorf("expected OrderID %s, got %s", event.OrderID, unmarshaled.OrderID)
	}
}

func TestRiskEventSerialization(t *testing.T) {
	event := RiskEvent{
		AlertType: "drawdown",
		Severity:  "high",
		Value:     decimal.NewFromFloat(15),
		Threshold: decimal.NewFromFloat(10),
		Message:   "Drawdown exceeded threshold",
		Timestamp: time.Now().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled RiskEvent
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.AlertType != event.AlertType {
		t.Errorf("expected AlertType %s, got %s", event.AlertType, unmarshaled.AlertType)
	}
}

func TestOddsEventSerialization(t *testing.T) {
	event := OddsEvent{
		ConditionID: "cond-123",
		Outcome:     "yes",
		Odds:        decimal.NewFromFloat(0.65),
		Volume:      decimal.NewFromFloat(50000),
		Timestamp:   time.Now().Truncate(time.Millisecond),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var unmarshaled OddsEvent
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if unmarshaled.ConditionID != event.ConditionID {
		t.Errorf("expected ConditionID %s, got %s", event.ConditionID, unmarshaled.ConditionID)
	}
}

func TestTradingEventGenericWrapper(t *testing.T) {
	pricePayload := PriceEvent{
		Symbol:    "BTC/USDT",
		Exchange:  "binance",
		Price:     decimal.NewFromFloat(50000),
		Timestamp: time.Now(),
	}

	event := &TradingEvent{
		Type:      EventPriceUpdate,
		Payload:   pricePayload,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("failed to marshal TradingEvent: %v", err)
	}

	var unmarshaled TradingEvent
	if err := json.Unmarshal(data, &unmarshaled); err != nil {
		t.Fatalf("failed to unmarshal TradingEvent: %v", err)
	}

	if unmarshaled.Type != EventPriceUpdate {
		t.Errorf("expected Type %s, got %s", EventPriceUpdate, unmarshaled.Type)
	}
}

func TestTradingEventBus_ConcurrentSubscribe(t *testing.T) {
	bus := NewTradingEventBus(nil, 10, nil)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Subscribe(EventPriceUpdate, func(e *TradingEvent) {})
		}()
	}

	wg.Wait()

	count := bus.GetSubscriptionCount(EventPriceUpdate)
	if count != 10 {
		t.Errorf("expected 10 subscriptions, got %d", count)
	}

	bus.Close()
}

func TestTradingEventBus_ConcurrentPublish(t *testing.T) {
	bus := NewTradingEventBus(nil, 100, nil)

	var mu sync.Mutex
	publishCount := 0

	bus.Subscribe(EventPriceUpdate, func(e *TradingEvent) {
		mu.Lock()
		publishCount++
		mu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Publish(&TradingEvent{
				Type:      EventPriceUpdate,
				Timestamp: time.Now(),
			})
		}()
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	if publishCount != 50 {
		t.Errorf("expected 50 publish events, got %d", publishCount)
	}
	mu.Unlock()

	bus.Close()
}

func TestEventTypes(t *testing.T) {
	tests := []struct {
		eventType TradingEventType
		expected  string
	}{
		{EventPriceUpdate, "price_update"},
		{EventSignalDetected, "signal_detected"},
		{EventArbitrageFound, "arbitrage_found"},
		{EventOrderFilled, "order_filled"},
		{EventOrderRejected, "order_rejected"},
		{EventStopTriggered, "stop_triggered"},
		{EventDrawdownAlert, "drawdown_alert"},
		{EventEmergencyStop, "emergency_stop"},
		{EventOddsChange, "odds_change"},
		{EventResolved, "event_resolved"},
	}

	for _, tc := range tests {
		if string(tc.eventType) != tc.expected {
			t.Errorf("expected %s, got %s", tc.expected, tc.eventType)
		}
	}
}
