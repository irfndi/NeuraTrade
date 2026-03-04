package execution

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// MockTradingGateway implements ports.TradingGateway for testing
type MockTradingGateway struct {
	mu              sync.RWMutex
	orders          map[string]ports.OrderResult
	canceledOrders  map[string]bool
	placeOrderFunc  func(ctx context.Context, req ports.OrderRequest) (ports.OrderResult, error)
	cancelOrderFunc func(ctx context.Context, exchange, orderID string) error
}

func NewMockTradingGateway() *MockTradingGateway {
	return &MockTradingGateway{
		orders:         make(map[string]ports.OrderResult),
		canceledOrders: make(map[string]bool),
	}
}

func (m *MockTradingGateway) PlaceOrder(ctx context.Context, req ports.OrderRequest) (ports.OrderResult, error) {
	m.mu.RLock()
	placeOrderFn := m.placeOrderFunc
	m.mu.RUnlock()
	if placeOrderFn != nil {
		return placeOrderFn(ctx, req)
	}
	result := ports.OrderResult{
		Exchange:  req.Exchange,
		OrderID:   "order-" + req.ClientID,
		ClientID:  req.ClientID,
		Symbol:    req.Symbol,
		Side:      req.Side,
		Type:      req.Type,
		Amount:    req.Amount,
		Filled:    decimal.Zero,
		Status:    ports.OrderStatusOpen,
		Timestamp: time.Now(),
	}
	m.mu.Lock()
	m.orders[result.OrderID] = result
	m.mu.Unlock()
	return result, nil
}

func (m *MockTradingGateway) CancelOrder(ctx context.Context, exchange, orderID string) error {
	m.mu.RLock()
	cancelOrderFn := m.cancelOrderFunc
	m.mu.RUnlock()
	if cancelOrderFn != nil {
		return cancelOrderFn(ctx, exchange, orderID)
	}
	m.mu.Lock()
	m.canceledOrders[orderID] = true
	m.mu.Unlock()
	return nil
}

func (m *MockTradingGateway) SetPlaceOrderFunc(fn func(ctx context.Context, req ports.OrderRequest) (ports.OrderResult, error)) {
	m.mu.Lock()
	m.placeOrderFunc = fn
	m.mu.Unlock()
}

func (m *MockTradingGateway) SetCancelOrderFunc(fn func(ctx context.Context, exchange, orderID string) error) {
	m.mu.Lock()
	m.cancelOrderFunc = fn
	m.mu.Unlock()
}

func (m *MockTradingGateway) OrdersCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.orders)
}

func (m *MockTradingGateway) CancelAllOrders(ctx context.Context, exchange, symbol string) error {
	return nil
}

func (m *MockTradingGateway) FetchOrder(ctx context.Context, exchange, orderID string) (ports.Order, error) {
	return ports.Order{}, nil
}

func (m *MockTradingGateway) FetchOpenOrders(ctx context.Context, exchange, symbol string) ([]ports.Order, error) {
	return nil, nil
}

func (m *MockTradingGateway) FetchPositions(ctx context.Context, exchange string) ([]ports.Position, error) {
	return nil, nil
}

func (m *MockTradingGateway) FetchBalances(ctx context.Context, exchange string) ([]ports.Balance, error) {
	return nil, nil
}

func (m *MockTradingGateway) IsHealthy(ctx context.Context) bool {
	return true
}

// MockIdempotencyStore implements IdempotencyStore for testing
type MockIdempotencyStore struct {
	mu      sync.RWMutex
	intents map[string]*OrderIntent
}

func NewMockIdempotencyStore() *MockIdempotencyStore {
	return &MockIdempotencyStore{
		intents: make(map[string]*OrderIntent),
	}
}

func (m *MockIdempotencyStore) SaveIntent(ctx context.Context, intent *OrderIntent) error {
	m.mu.Lock()
	m.intents[intent.IntentID] = cloneOrderIntent(intent)
	m.mu.Unlock()
	return nil
}

func (m *MockIdempotencyStore) GetIntent(ctx context.Context, intentID string) (*OrderIntent, error) {
	m.mu.RLock()
	intent, exists := m.intents[intentID]
	m.mu.RUnlock()
	if !exists {
		return nil, nil
	}
	return cloneOrderIntent(intent), nil
}

func (m *MockIdempotencyStore) GetIntentByClientID(ctx context.Context, clientID string) (*OrderIntent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, intent := range m.intents {
		if intent.ClientOrderID == clientID {
			return cloneOrderIntent(intent), nil
		}
	}
	return nil, nil
}

func (m *MockIdempotencyStore) GetIntentByExchangeID(ctx context.Context, exchange, exchangeOrderID string) (*OrderIntent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, intent := range m.intents {
		if intent.Request.Exchange == exchange && intent.ExchangeOrderID == exchangeOrderID {
			return cloneOrderIntent(intent), nil
		}
	}
	return nil, nil
}

func (m *MockIdempotencyStore) UpdateIntent(ctx context.Context, intent *OrderIntent) error {
	m.mu.Lock()
	m.intents[intent.IntentID] = cloneOrderIntent(intent)
	m.mu.Unlock()
	return nil
}

// MockAuditLogger implements AuditLogger for testing
type MockAuditLogger struct {
	mu     sync.RWMutex
	events []OrderAuditEvent
}

func NewMockAuditLogger() *MockAuditLogger {
	return &MockAuditLogger{
		events: make([]OrderAuditEvent, 0),
	}
}

func (m *MockAuditLogger) LogOrderEvent(ctx context.Context, event OrderAuditEvent) error {
	m.mu.Lock()
	m.events = append(m.events, event)
	m.mu.Unlock()
	return nil
}

func (m *MockAuditLogger) GetOrderHistory(ctx context.Context, intentID string) ([]OrderAuditEvent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var history []OrderAuditEvent
	for _, event := range m.events {
		if event.IntentID == intentID {
			history = append(history, event)
		}
	}
	return history, nil
}

func (m *MockAuditLogger) EventsCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.events)
}

// MockEventBus implements ports.EventBus for testing
type MockEventBus struct {
	mu          sync.RWMutex
	subscribers map[string][]ports.EventHandler
	events      []ports.Event
}

func NewMockEventBus() *MockEventBus {
	return &MockEventBus{
		subscribers: make(map[string][]ports.EventHandler),
		events:      make([]ports.Event, 0),
	}
}

func (m *MockEventBus) Publish(ctx context.Context, event ports.Event) error {
	m.mu.Lock()
	m.events = append(m.events, event)
	m.mu.Unlock()
	return nil
}

func (m *MockEventBus) Subscribe(ctx context.Context, eventType string, handler ports.EventHandler) error {
	m.mu.Lock()
	m.subscribers[eventType] = append(m.subscribers[eventType], handler)
	m.mu.Unlock()
	return nil
}

func (m *MockEventBus) SubscribeAll(ctx context.Context, handler ports.EventHandler) error {
	return nil
}

func (m *MockEventBus) Unsubscribe(ctx context.Context, eventType string) error {
	m.mu.Lock()
	delete(m.subscribers, eventType)
	m.mu.Unlock()
	return nil
}

func (m *MockEventBus) EventsCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.events)
}

func TestExecutionActor_ID(t *testing.T) {
	gateway := NewMockTradingGateway()
	eventBus := NewMockEventBus()
	idempotencyStore := NewMockIdempotencyStore()
	auditLog := NewMockAuditLogger()

	actor := NewExecutionActor("test-execution-actor", gateway, eventBus, idempotencyStore, auditLog)

	if actor.ID() != "test-execution-actor" {
		t.Errorf("Expected ID to be 'test-execution-actor', got '%s'", actor.ID())
	}
}

func TestExecutionActor_PlaceOrder_Success(t *testing.T) {
	ctx := context.Background()
	gateway := NewMockTradingGateway()
	eventBus := NewMockEventBus()
	idempotencyStore := NewMockIdempotencyStore()
	auditLog := NewMockAuditLogger()

	execActor := NewExecutionActor("test-actor", gateway, eventBus, idempotencyStore, auditLog)

	msg := PlaceOrderMsg{
		IntentID: "intent-001",
		Request: ports.OrderRequest{
			Exchange: "test-exchange",
			Symbol:   "BTC/USDT",
			Side:     ports.OrderSideBuy,
			Type:     ports.OrderTypeMarket,
			Amount:   decimal.NewFromFloat(1.0),
		},
		RiskApproved: true,
		StrategyID:   "strategy-001",
	}

	env := actor.Envelope{
		Message: msg,
	}

	err := execActor.Receive(ctx, env)
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	// Verify intent was stored
	intent, err := idempotencyStore.GetIntent(ctx, "intent-001")
	if err != nil {
		t.Fatalf("Failed to get intent: %v", err)
	}
	if intent == nil {
		t.Fatal("Intent should be stored")
	}
	if intent.IntentID != "intent-001" {
		t.Errorf("Expected IntentID 'intent-001', got '%s'", intent.IntentID)
	}
	if intent.ClientOrderID == "" {
		t.Error("ClientOrderID should be generated")
	}

	// Verify audit log
	if auditLog.EventsCount() < 2 {
		t.Errorf("Expected at least 2 audit events, got %d", auditLog.EventsCount())
	}

	// Verify event was published
	if eventBus.EventsCount() == 0 {
		t.Error("Expected event to be published")
	}
}

func TestExecutionActor_PlaceOrder_RiskNotApproved(t *testing.T) {
	ctx := context.Background()
	gateway := NewMockTradingGateway()
	eventBus := NewMockEventBus()
	idempotencyStore := NewMockIdempotencyStore()
	auditLog := NewMockAuditLogger()

	execActor := NewExecutionActor("test-actor", gateway, eventBus, idempotencyStore, auditLog)

	msg := PlaceOrderMsg{
		IntentID: "intent-risk-reject-001",
		Request: ports.OrderRequest{
			Exchange: "test-exchange",
			Symbol:   "BTC/USDT",
			Side:     ports.OrderSideBuy,
			Type:     ports.OrderTypeMarket,
			Amount:   decimal.NewFromFloat(1.0),
		},
		RiskApproved: false,
	}

	err := execActor.Receive(ctx, actor.Envelope{Message: msg})
	if !errors.Is(err, ErrRiskNotApproved) {
		t.Fatalf("expected ErrRiskNotApproved, got %v", err)
	}

	intent, getErr := idempotencyStore.GetIntent(ctx, msg.IntentID)
	if getErr != nil {
		t.Fatalf("GetIntent failed: %v", getErr)
	}
	if intent == nil {
		t.Fatalf("expected stored intent for %s", msg.IntentID)
	}
	if intent.Status != ports.OrderStatusRejected {
		t.Fatalf("expected rejected status, got %s", intent.Status)
	}
	if gateway.OrdersCount() != 0 {
		t.Fatalf("gateway should not place any order when risk is not approved")
	}
}

func TestExecutionActor_PlaceOrder_ValidationFailures(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		request ports.OrderRequest
	}{
		{
			name: "empty exchange",
			request: ports.OrderRequest{
				Symbol: "BTC/USDT",
				Side:   ports.OrderSideBuy,
				Type:   ports.OrderTypeMarket,
				Amount: decimal.NewFromFloat(1.0),
			},
		},
		{
			name: "invalid side",
			request: ports.OrderRequest{
				Exchange: "test-exchange",
				Symbol:   "BTC/USDT",
				Side:     ports.OrderSide(""),
				Type:     ports.OrderTypeMarket,
				Amount:   decimal.NewFromFloat(1.0),
			},
		},
		{
			name: "invalid type",
			request: ports.OrderRequest{
				Exchange: "test-exchange",
				Symbol:   "BTC/USDT",
				Side:     ports.OrderSideBuy,
				Type:     ports.OrderType(""),
				Amount:   decimal.NewFromFloat(1.0),
			},
		},
		{
			name: "zero amount",
			request: ports.OrderRequest{
				Exchange: "test-exchange",
				Symbol:   "BTC/USDT",
				Side:     ports.OrderSideBuy,
				Type:     ports.OrderTypeMarket,
				Amount:   decimal.Zero,
			},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gateway := NewMockTradingGateway()
			eventBus := NewMockEventBus()
			idempotencyStore := NewMockIdempotencyStore()
			auditLog := NewMockAuditLogger()
			execActor := NewExecutionActor("test-actor", gateway, eventBus, idempotencyStore, auditLog)

			intentID := fmt.Sprintf("intent-validation-%d", i)
			err := execActor.Receive(ctx, actor.Envelope{
				Message: PlaceOrderMsg{
					IntentID:     intentID,
					Request:      tt.request,
					RiskApproved: true,
				},
			})
			if err == nil {
				t.Fatalf("expected validation error")
			}
			if !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("expected ErrInvalidRequest, got %v", err)
			}
			if gateway.OrdersCount() != 0 {
				t.Fatalf("no gateway order should be placed on validation failure")
			}
		})
	}
}

func TestExecutionActor_PlaceOrder_GatewayFailure(t *testing.T) {
	ctx := context.Background()
	gateway := NewMockTradingGateway()
	gateway.SetPlaceOrderFunc(func(ctx context.Context, req ports.OrderRequest) (ports.OrderResult, error) {
		return ports.OrderResult{}, fmt.Errorf("exchange temporary failure")
	})
	eventBus := NewMockEventBus()
	idempotencyStore := NewMockIdempotencyStore()
	auditLog := NewMockAuditLogger()

	execActor := NewExecutionActor("test-actor", gateway, eventBus, idempotencyStore, auditLog)
	msg := PlaceOrderMsg{
		IntentID: "intent-gateway-failure-001",
		Request: ports.OrderRequest{
			Exchange: "test-exchange",
			Symbol:   "BTC/USDT",
			Side:     ports.OrderSideBuy,
			Type:     ports.OrderTypeMarket,
			Amount:   decimal.NewFromFloat(1.0),
		},
		RiskApproved: true,
	}

	err := execActor.Receive(ctx, actor.Envelope{Message: msg})
	if !errors.Is(err, ErrExecutionRejected) {
		t.Fatalf("expected ErrExecutionRejected, got %v", err)
	}

	intent, getErr := idempotencyStore.GetIntent(ctx, msg.IntentID)
	if getErr != nil {
		t.Fatalf("GetIntent failed: %v", getErr)
	}
	if intent == nil {
		t.Fatalf("expected stored intent for failed gateway placement")
	}
	if intent.Status != ports.OrderStatusRejected {
		t.Fatalf("expected rejected status after gateway failure, got %s", intent.Status)
	}
	if auditLog.EventsCount() == 0 {
		t.Fatalf("expected audit events for failed gateway placement")
	}
	if eventBus.EventsCount() == 0 {
		t.Fatalf("expected event publication for failed gateway placement")
	}
}

func TestExecutionActor_PlaceOrder_Idempotency(t *testing.T) {
	ctx := context.Background()
	gateway := NewMockTradingGateway()
	eventBus := NewMockEventBus()
	idempotencyStore := NewMockIdempotencyStore()
	auditLog := NewMockAuditLogger()

	execActor := NewExecutionActor("test-actor", gateway, eventBus, idempotencyStore, auditLog)

	msg := PlaceOrderMsg{
		IntentID: "intent-002",
		Request: ports.OrderRequest{
			Exchange: "test-exchange",
			Symbol:   "BTC/USDT",
			Side:     ports.OrderSideBuy,
			Type:     ports.OrderTypeMarket,
			Amount:   decimal.NewFromFloat(1.0),
		},
		RiskApproved: true,
	}

	// First placement
	env := actor.Envelope{Message: msg}
	err := execActor.Receive(ctx, env)
	if err != nil {
		t.Fatalf("First PlaceOrder failed: %v", err)
	}

	// Second placement with same intent ID (should be idempotent)
	err = execActor.Receive(ctx, env)
	if err != nil {
		t.Fatalf("Second PlaceOrder should be idempotent, got error: %v", err)
	}

	// Should only have 2 audit events (not 4)
	if auditLog.EventsCount() != 2 {
		t.Errorf("Expected 2 audit events (idempotent), got %d", auditLog.EventsCount())
	}
}

func TestExecutionActor_CancelOrder(t *testing.T) {
	ctx := context.Background()
	gateway := NewMockTradingGateway()
	eventBus := NewMockEventBus()
	idempotencyStore := NewMockIdempotencyStore()
	auditLog := NewMockAuditLogger()

	execActor := NewExecutionActor("test-actor", gateway, eventBus, idempotencyStore, auditLog)

	// First place an order
	placeMsg := PlaceOrderMsg{
		IntentID: "intent-003",
		Request: ports.OrderRequest{
			Exchange: "test-exchange",
			Symbol:   "BTC/USDT",
			Side:     ports.OrderSideBuy,
			Type:     ports.OrderTypeMarket,
			Amount:   decimal.NewFromFloat(1.0),
		},
		RiskApproved: true,
	}

	err := execActor.Receive(ctx, actor.Envelope{Message: placeMsg})
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	// Get the intent to retrieve order ID
	intent, _ := idempotencyStore.GetIntent(ctx, "intent-003")
	if intent == nil {
		t.Fatal("Intent not found")
	}

	// Now cancel the order
	cancelMsg := CancelOrderMsg{
		IntentID: "intent-003",
		OrderID:  intent.ExchangeOrderID,
		Exchange: "test-exchange",
		Reason:   "user_request",
	}

	err = execActor.Receive(ctx, actor.Envelope{Message: cancelMsg})
	if err != nil {
		t.Fatalf("CancelOrder failed: %v", err)
	}

	// Verify order was canceled
	intent, _ = idempotencyStore.GetIntent(ctx, "intent-003")
	if intent.Status != ports.OrderStatusCancelled {
		t.Errorf("Expected status 'cancelled', got '%s'", intent.Status)
	}
}

func TestExecutionActor_HandleFillUpdate(t *testing.T) {
	ctx := context.Background()
	gateway := NewMockTradingGateway()
	eventBus := NewMockEventBus()
	idempotencyStore := NewMockIdempotencyStore()
	auditLog := NewMockAuditLogger()

	execActor := NewExecutionActor("test-actor", gateway, eventBus, idempotencyStore, auditLog)

	// Place an order
	placeMsg := PlaceOrderMsg{
		IntentID: "intent-004",
		Request: ports.OrderRequest{
			Exchange: "test-exchange",
			Symbol:   "BTC/USDT",
			Side:     ports.OrderSideBuy,
			Type:     ports.OrderTypeMarket,
			Amount:   decimal.NewFromFloat(1.0),
		},
		RiskApproved: true,
	}

	err := execActor.Receive(ctx, actor.Envelope{Message: placeMsg})
	if err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	// Get the order ID
	intent, _ := idempotencyStore.GetIntent(ctx, "intent-004")

	// Simulate a fill update
	fillMsg := OrderFillUpdateMsg{
		OrderID:      intent.ExchangeOrderID,
		Exchange:     "test-exchange",
		FilledAmount: decimal.NewFromFloat(1.0),
		FillPrice:    decimal.NewFromFloat(50000.0),
		Timestamp:    time.Now(),
	}

	err = execActor.Receive(ctx, actor.Envelope{Message: fillMsg})
	if err != nil {
		t.Fatalf("HandleFillUpdate failed: %v", err)
	}

	// Verify intent was updated
	intent, _ = idempotencyStore.GetIntent(ctx, "intent-004")
	if intent.Status != ports.OrderStatusFilled {
		t.Errorf("Expected status 'filled', got '%s'", intent.Status)
	}
	if !intent.FilledAmount.Equal(decimal.NewFromFloat(1.0)) {
		t.Errorf("Expected filled amount 1.0, got %s", intent.FilledAmount)
	}
}

func TestExecutionActor_HandleFillUpdate_AfterRestart(t *testing.T) {
	ctx := context.Background()
	gateway := NewMockTradingGateway()
	idempotencyStore := NewMockIdempotencyStore()
	auditLog := NewMockAuditLogger()

	firstEventBus := NewMockEventBus()
	actorBeforeRestart := NewExecutionActor("test-actor-before", gateway, firstEventBus, idempotencyStore, auditLog)

	placeMsg := PlaceOrderMsg{
		IntentID: "intent-restart-001",
		Request: ports.OrderRequest{
			Exchange: "test-exchange",
			Symbol:   "BTC/USDT",
			Side:     ports.OrderSideBuy,
			Type:     ports.OrderTypeMarket,
			Amount:   decimal.NewFromFloat(1.0),
		},
		RiskApproved: true,
	}

	if err := actorBeforeRestart.Receive(ctx, actor.Envelope{Message: placeMsg}); err != nil {
		t.Fatalf("PlaceOrder failed: %v", err)
	}

	storedIntent, _ := idempotencyStore.GetIntent(ctx, "intent-restart-001")
	if storedIntent == nil || storedIntent.ExchangeOrderID == "" {
		t.Fatal("expected persisted intent with exchange order ID")
	}

	// Simulate restart with a fresh actor instance and empty in-memory maps.
	secondEventBus := NewMockEventBus()
	actorAfterRestart := NewExecutionActor("test-actor-after", gateway, secondEventBus, idempotencyStore, auditLog)

	fillMsg := OrderFillUpdateMsg{
		OrderID:      storedIntent.ExchangeOrderID,
		Exchange:     "test-exchange",
		FilledAmount: decimal.NewFromFloat(1.0),
		FillPrice:    decimal.NewFromFloat(50000.0),
		Timestamp:    time.Now(),
	}

	if err := actorAfterRestart.Receive(ctx, actor.Envelope{Message: fillMsg}); err != nil {
		t.Fatalf("HandleFillUpdate after restart failed: %v", err)
	}

	updatedIntent, _ := idempotencyStore.GetIntent(ctx, "intent-restart-001")
	if updatedIntent == nil {
		t.Fatal("expected persisted intent after fill update")
	}
	if updatedIntent.Status != ports.OrderStatusFilled {
		t.Errorf("expected status 'filled', got '%s'", updatedIntent.Status)
	}
}

func TestGenerateClientOrderID(t *testing.T) {
	// Test deterministic generation
	id1 := generateClientOrderID("intent-001", 1)
	id2 := generateClientOrderID("intent-001", 1)

	if id1 != id2 {
		t.Error("ClientOrderID should be deterministic")
	}

	// Test different intents produce different IDs
	id3 := generateClientOrderID("intent-002", 1)
	if id1 == id3 {
		t.Error("Different intents should produce different ClientOrderIDs")
	}

	// Test different attempts produce different IDs
	id4 := generateClientOrderID("intent-001", 2)
	if id1 == id4 {
		t.Error("Different attempts should produce different ClientOrderIDs")
	}

	// Test format
	if len(id1) != 18 { // "NT" + 16 chars
		t.Errorf("Expected ClientOrderID length 18, got %d", len(id1))
	}
	if id1[:2] != "NT" {
		t.Errorf("Expected ClientOrderID to start with 'NT', got '%s'", id1[:2])
	}
}

func TestOrderIntent_IsTerminal(t *testing.T) {
	tests := []struct {
		status   ports.OrderStatus
		expected bool
	}{
		{ports.OrderStatusFilled, true},
		{ports.OrderStatusCancelled, true},
		{ports.OrderStatusRejected, true},
		{ports.OrderStatusPending, false},
		{ports.OrderStatusOpen, false},
		{ports.OrderStatusPartial, false},
	}

	for _, test := range tests {
		intent := &OrderIntent{Status: test.status}
		if intent.IsTerminal() != test.expected {
			t.Errorf("IsTerminal() for status %s: expected %v, got %v",
				test.status, test.expected, intent.IsTerminal())
		}
	}
}

func BenchmarkPlaceOrder(b *testing.B) {
	ctx := context.Background()
	gateway := NewMockTradingGateway()
	eventBus := NewMockEventBus()
	idempotencyStore := NewMockIdempotencyStore()
	auditLog := NewMockAuditLogger()

	execActor := NewExecutionActor("bench-actor", gateway, eventBus, idempotencyStore, auditLog)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		msg := PlaceOrderMsg{
			IntentID: fmt.Sprintf("benchmark-intent-%d", i),
			Request: ports.OrderRequest{
				Exchange: "test-exchange",
				Symbol:   "BTC/USDT",
				Side:     ports.OrderSideBuy,
				Type:     ports.OrderTypeMarket,
				Amount:   decimal.NewFromFloat(1.0),
			},
			RiskApproved: true,
		}
		if err := execActor.Receive(ctx, actor.Envelope{Message: msg}); err != nil {
			b.Fatalf("PlaceOrder failed: %v", err)
		}
	}
}

func cloneOrderIntent(intent *OrderIntent) *OrderIntent {
	if intent == nil {
		return nil
	}
	return &OrderIntent{
		IntentID:        intent.IntentID,
		ClientOrderID:   intent.ClientOrderID,
		ExchangeOrderID: intent.ExchangeOrderID,
		Status:          intent.Status,
		Request:         cloneOrderRequest(intent.Request),
		SubmittedAt:     intent.SubmittedAt,
		UpdatedAt:       intent.UpdatedAt,
		FilledAmount:    intent.FilledAmount,
		FillPrice:       intent.FillPrice,
		RejectReason:    intent.RejectReason,
		AttemptCount:    intent.AttemptCount,
		LastAuditHash:   intent.LastAuditHash,
	}
}

func cloneOrderRequest(req ports.OrderRequest) ports.OrderRequest {
	// Explicit field mapping keeps cloning behavior stable if mutable nested fields are added later.
	return ports.OrderRequest{
		ClientID:   req.ClientID,
		Exchange:   req.Exchange,
		Symbol:     req.Symbol,
		Side:       req.Side,
		Type:       req.Type,
		Amount:     req.Amount,
		Price:      req.Price,
		StopPrice:  req.StopPrice,
		TakeProfit: req.TakeProfit,
		ReduceOnly: req.ReduceOnly,
		PostOnly:   req.PostOnly,
	}
}
