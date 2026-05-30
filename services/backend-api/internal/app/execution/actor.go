// Package execution provides the ExecutionActor for order execution with idempotency and audit trail.
// This is PR6 of the actor-based platform refactor.
package execution

import (
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// Errors
var (
	ErrDuplicateIntent    = errors.New("duplicate intent: order already placed")
	ErrExecutionTimeout   = errors.New("execution timeout")
	ErrGatewayUnavailable = errors.New("trading gateway unavailable")
	ErrInvalidRequest     = errors.New("invalid order request")
	ErrExecutionRejected  = errors.New("order execution rejected")
	ErrRiskNotApproved    = errors.New("risk approval required")
)

// Message types for ExecutionActor
type (
	// PlaceOrderMsg requests order placement with idempotency
	PlaceOrderMsg struct {
		IntentID     string                 // Unique intent ID for idempotency
		AttemptCount int                    // Optional retry attempt start (defaults to 1)
		Request      ports.OrderRequest     // The order request
		RiskApproved bool                   // Whether risk has approved this
		StrategyID   string                 // Optional: originating strategy
		SignalID     string                 // Optional: originating signal
		Metadata     map[string]interface{} // Additional context
	}

	// CancelOrderMsg requests order cancellation
	CancelOrderMsg struct {
		IntentID string // Original intent ID
		OrderID  string // Exchange order ID
		Exchange string
		Reason   string
	}

	// GetOrderStatusMsg requests order status
	GetOrderStatusMsg struct {
		IntentID string
		OrderID  string
		Exchange string
	}

	// OrderFillUpdateMsg is sent when an order fill is detected
	OrderFillUpdateMsg struct {
		OrderID      string
		Exchange     string
		FilledAmount decimal.Decimal
		FillPrice    decimal.Decimal
		Timestamp    time.Time
	}

	// OrderRejectedMsg is sent when an order is rejected
	OrderRejectedMsg struct {
		OrderID   string
		Exchange  string
		Reason    string
		Timestamp time.Time
	}
)

// MessageType implementations
func (m PlaceOrderMsg) MessageType() string      { return "execution.place_order" }
func (m CancelOrderMsg) MessageType() string     { return "execution.cancel_order" }
func (m GetOrderStatusMsg) MessageType() string  { return "execution.get_status" }
func (m OrderFillUpdateMsg) MessageType() string { return "execution.fill_update" }
func (m OrderRejectedMsg) MessageType() string   { return "execution.rejected" }

// OrderIntent represents a submitted order intent with idempotency tracking
type OrderIntent struct {
	IntentID        string
	ClientOrderID   string
	ExchangeOrderID string
	Status          ports.OrderStatus
	Request         ports.OrderRequest
	SubmittedAt     time.Time
	UpdatedAt       time.Time
	FilledAmount    decimal.Decimal
	FillPrice       decimal.Decimal
	RejectReason    string
	AttemptCount    int
	LastAuditHash   string
}

// IsTerminal returns true if the order has reached a terminal state
func (o *OrderIntent) IsTerminal() bool {
	switch o.Status {
	case ports.OrderStatusFilled, ports.OrderStatusCancelled, ports.OrderStatusRejected:
		return true
	default:
		return false
	}
}

// ExecutionActor handles order execution with idempotency and audit trail
type ExecutionActor struct {
	id               string
	gateway          ports.TradingGateway
	eventBus         ports.EventBus
	idempotencyStore IdempotencyStore
	auditLog         AuditLogger

	// In-memory state (actor-owned, single-writer)
	intents               map[string]*OrderIntent // IntentID -> Intent
	clientIDToIntent      map[string]string       // ClientOrderID -> IntentID
	exchangeOrderToIntent map[string]string       // exchange:orderID -> IntentID
	lastAuditHash         map[string]string       // IntentID -> hash of most recent audit event
}

// IdempotencyStore persists intent mappings for restart safety
type IdempotencyStore interface {
	// SaveIntent persists an order intent
	SaveIntent(ctx context.Context, intent *OrderIntent) error
	// GetIntent retrieves an intent by IntentID
	GetIntent(ctx context.Context, intentID string) (*OrderIntent, error)
	// GetIntentByClientID retrieves an intent by ClientOrderID
	GetIntentByClientID(ctx context.Context, clientID string) (*OrderIntent, error)
	// GetIntentByExchangeID retrieves an intent by ExchangeOrderID
	GetIntentByExchangeID(ctx context.Context, exchange, exchangeOrderID string) (*OrderIntent, error)
	// UpdateIntent updates an existing intent
	UpdateIntent(ctx context.Context, intent *OrderIntent) error
}

// AuditLogger records order lifecycle events
type AuditLogger interface {
	// LogOrderEvent records an order lifecycle event
	LogOrderEvent(ctx context.Context, event OrderAuditEvent) error
	// GetOrderHistory retrieves audit history for an intent
	GetOrderHistory(ctx context.Context, intentID string) ([]OrderAuditEvent, error)
}

// OrderAuditEvent represents an auditable order event
type OrderAuditEvent struct {
	EventID         string                 `json:"event_id"`
	IntentID        string                 `json:"intent_id"`
	ClientOrderID   string                 `json:"client_order_id"`
	ExchangeOrderID string                 `json:"exchange_order_id,omitempty"`
	EventType       string                 `json:"event_type"` // submitted, filled, rejected, cancelled
	Exchange        string                 `json:"exchange"`
	Symbol          string                 `json:"symbol"`
	Side            string                 `json:"side"`
	Amount          decimal.Decimal        `json:"amount"`
	Price           decimal.Decimal        `json:"price"`
	FilledAmount    decimal.Decimal        `json:"filled_amount"`
	FillPrice       decimal.Decimal        `json:"fill_price,omitempty"`
	Reason          string                 `json:"reason,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
	Timestamp       time.Time              `json:"timestamp"`
	HashChain       string                 `json:"hash_chain,omitempty"` // Optional: for tamper detection
}

// Config holds ExecutionActor configuration
type Config struct {
	ActorConfig    actor.Config
	DefaultTimeout time.Duration
	MaxRetries     int
}

// DefaultConfig returns sensible defaults
func DefaultConfig() Config {
	return Config{
		ActorConfig:    actor.DefaultConfig(),
		DefaultTimeout: 30 * time.Second,
		MaxRetries:     3,
	}
}

// NewExecutionActor creates a new ExecutionActor
func NewExecutionActor(
	id string,
	gateway ports.TradingGateway,
	eventBus ports.EventBus,
	idempotencyStore IdempotencyStore,
	auditLog AuditLogger,
) *ExecutionActor {
	return &ExecutionActor{
		id:                    id,
		gateway:               gateway,
		eventBus:              eventBus,
		idempotencyStore:      idempotencyStore,
		auditLog:              auditLog,
		intents:               make(map[string]*OrderIntent),
		clientIDToIntent:      make(map[string]string),
		exchangeOrderToIntent: make(map[string]string),
		lastAuditHash:         make(map[string]string),
	}
}

// ID implements actor.Actor
func (a *ExecutionActor) ID() string {
	return a.id
}

// Receive implements actor.Actor - processes messages sequentially
func (a *ExecutionActor) Receive(ctx context.Context, env actor.Envelope) error {
	switch msg := env.Message.(type) {
	case PlaceOrderMsg:
		return a.handlePlaceOrder(ctx, msg)
	case CancelOrderMsg:
		return a.handleCancelOrder(ctx, msg)
	case GetOrderStatusMsg:
		return a.handleGetOrderStatus(ctx, env)
	case OrderFillUpdateMsg:
		return a.handleFillUpdate(ctx, msg)
	case OrderRejectedMsg:
		return a.handleRejected(ctx, msg)
	default:
		return actor.ErrInvalidMessage
	}
}

// handlePlaceOrder processes a new order placement with idempotency
func (a *ExecutionActor) handlePlaceOrder(ctx context.Context, msg PlaceOrderMsg) error {
	// Check for duplicate intent
	existing, err := a.idempotencyStore.GetIntent(ctx, msg.IntentID)
	if err != nil {
		return fmt.Errorf("load intent %s: %w", msg.IntentID, err)
	}
	if existing != nil {
		// Intent already exists - this is a retry
		if existing.IsTerminal() {
			// Order already in terminal state, return success without re-executing.
			return nil
		}
		// Order in flight, increment attempt count
		existing.AttemptCount++
		if err := a.idempotencyStore.UpdateIntent(ctx, existing); err != nil {
			return fmt.Errorf("update intent attempt count: %w", err)
		}
		a.intents[msg.IntentID] = existing
		if existing.ClientOrderID != "" {
			a.clientIDToIntent[existing.ClientOrderID] = existing.IntentID
		}
		if existing.ExchangeOrderID != "" {
			a.exchangeOrderToIntent[exchangeOrderKey(existing.Request.Exchange, existing.ExchangeOrderID)] = existing.IntentID
		}
		return nil
	}

	// Validate request
	if err := a.validateRequest(&msg.Request); err != nil {
		a.logAuditEvent(ctx, msg.IntentID, "validation_failed", msg.Request.Exchange, msg.Request.Symbol, err.Error(), nil)
		return fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}

	// Resolve deterministic client order ID while handling collisions.
	attempt := msg.AttemptCount
	if attempt <= 0 {
		attempt = 1
	}
	var clientOrderID string
	for {
		clientOrderID = generateClientOrderID(msg.IntentID, attempt)
		existingByClientID, lookupErr := a.idempotencyStore.GetIntentByClientID(ctx, clientOrderID)
		if lookupErr != nil {
			return fmt.Errorf("lookup intent by client order ID %s: %w", clientOrderID, lookupErr)
		}

		if existingByClientID == nil {
			break
		}

		// Existing mapping belongs to the same intent: reuse and keep idempotent behavior.
		if existingByClientID.IntentID == msg.IntentID {
			existingByClientID.AttemptCount++
			if err := a.idempotencyStore.UpdateIntent(ctx, existingByClientID); err != nil {
				return fmt.Errorf("update intent attempt count: %w", err)
			}

			a.intents[msg.IntentID] = existingByClientID
			a.clientIDToIntent[clientOrderID] = existingByClientID.IntentID
			if existingByClientID.ExchangeOrderID != "" {
				a.exchangeOrderToIntent[exchangeOrderKey(existingByClientID.Request.Exchange, existingByClientID.ExchangeOrderID)] = existingByClientID.IntentID
			}
			if existingByClientID.IsTerminal() {
				return nil
			}
			a.publishEvent(ctx, ports.EventTypeOrderPlaced, existingByClientID)
			return nil
		}

		// Collision with a different intent: advance attempt and retry.
		attempt++
	}

	// Create intent record
	intent := &OrderIntent{
		IntentID:      msg.IntentID,
		ClientOrderID: clientOrderID,
		Status:        ports.OrderStatusPending,
		Request:       msg.Request,
		SubmittedAt:   time.Now(),
		UpdatedAt:     time.Now(),
		AttemptCount:  attempt,
	}

	// Persist intent before execution (for crash recovery)
	if err := a.idempotencyStore.SaveIntent(ctx, intent); err != nil {
		return fmt.Errorf("save intent: %w", err)
	}

	// Update in-memory state
	a.intents[msg.IntentID] = intent
	a.clientIDToIntent[clientOrderID] = msg.IntentID

	// Audit log: submission
	a.logAuditEvent(ctx, msg.IntentID, "submitted", msg.Request.Exchange, msg.Request.Symbol, "", map[string]interface{}{
		"client_order_id": clientOrderID,
		"risk_approved":   msg.RiskApproved,
		"strategy_id":     msg.StrategyID,
		"signal_id":       msg.SignalID,
	})

	if !msg.RiskApproved {
		intent.Status = ports.OrderStatusRejected
		intent.RejectReason = ErrRiskNotApproved.Error()
		intent.UpdatedAt = time.Now()
		updateErr := a.idempotencyStore.UpdateIntent(ctx, intent)

		a.logAuditEvent(ctx, msg.IntentID, "rejected", msg.Request.Exchange, msg.Request.Symbol, ErrRiskNotApproved.Error(), map[string]interface{}{
			"rejection_source": "execution_actor",
		})
		a.publishEvent(ctx, ports.EventTypeOrderRejected, intent)
		if updateErr != nil {
			return errors.Join(
				ErrRiskNotApproved,
				fmt.Errorf("persist rejected intent: %w", updateErr),
			)
		}
		return ErrRiskNotApproved
	}

	// Execute order via gateway
	req := msg.Request
	req.ClientID = clientOrderID

	result, err := a.gateway.PlaceOrder(ctx, req)
	if err != nil {
		reason := sanitizeExternalError(err)
		intent.Status = ports.OrderStatusRejected
		intent.RejectReason = reason
		intent.UpdatedAt = time.Now()
		updateErr := a.idempotencyStore.UpdateIntent(ctx, intent)

		a.logAuditEvent(ctx, msg.IntentID, "rejected", req.Exchange, req.Symbol, reason, nil)
		a.publishEvent(ctx, ports.EventTypeOrderRejected, intent)
		if updateErr != nil {
			return errors.Join(
				fmt.Errorf("%w: %s", ErrExecutionRejected, reason),
				fmt.Errorf("persist rejected intent: %w", updateErr),
			)
		}
		return fmt.Errorf("%w: %s", ErrExecutionRejected, reason)
	}

	// Update intent with result
	intent.ExchangeOrderID = result.OrderID
	intent.Status = result.Status
	intent.FilledAmount = result.Filled
	intent.FillPrice = result.AveragePrice
	intent.UpdatedAt = time.Now()

	// Persist updated intent
	if err := a.idempotencyStore.UpdateIntent(ctx, intent); err != nil {
		return fmt.Errorf("update intent with result: %w", err)
	}

	// Update mappings
	a.exchangeOrderToIntent[exchangeOrderKey(req.Exchange, result.OrderID)] = msg.IntentID

	// Audit log: placed
	a.logAuditEvent(ctx, msg.IntentID, "placed", req.Exchange, req.Symbol, "", map[string]interface{}{
		"exchange_order_id": result.OrderID,
		"status":            result.Status,
	})

	// Publish event
	a.publishEvent(ctx, ports.EventTypeOrderPlaced, intent)

	// If already filled, emit fill event
	if result.Status == ports.OrderStatusFilled {
		a.logAuditEvent(ctx, msg.IntentID, "filled", req.Exchange, req.Symbol, "", map[string]interface{}{
			"filled_amount": result.Filled,
			"fill_price":    result.AveragePrice,
		})
		a.publishEvent(ctx, ports.EventTypeOrderFilled, intent)
	}

	return nil
}

// handleCancelOrder processes order cancellation
func (a *ExecutionActor) handleCancelOrder(ctx context.Context, msg CancelOrderMsg) error {
	intent, exists := a.intents[msg.IntentID]
	if !exists {
		// Try to load from store
		loaded, err := a.idempotencyStore.GetIntent(ctx, msg.IntentID)
		if err != nil {
			return fmt.Errorf("load intent %s: %w", msg.IntentID, err)
		}
		if loaded == nil {
			return fmt.Errorf("intent not found: %s", msg.IntentID)
		}
		intent = loaded
		a.intents[msg.IntentID] = intent
	}

	if intent.IsTerminal() {
		// Already in terminal state, nothing to cancel
		return nil
	}

	// Use persisted identifiers as canonical values for cancellation.
	exchange := intent.Request.Exchange
	if exchange == "" {
		exchange = msg.Exchange
	}
	orderID := intent.ExchangeOrderID
	if orderID == "" {
		orderID = msg.OrderID
	}
	if exchange == "" || orderID == "" {
		return fmt.Errorf("cancel order intent %s: missing exchange/order identifier", msg.IntentID)
	}

	// Execute cancellation via gateway
	if err := a.gateway.CancelOrder(ctx, exchange, orderID); err != nil {
		a.logAuditEvent(ctx, msg.IntentID, "cancel_failed", exchange, intent.Request.Symbol, sanitizeExternalError(err), nil)
		return fmt.Errorf("cancel order exchange=%s orderID=%s: %w", exchange, orderID, err)
	}

	// Update intent
	intent.Status = ports.OrderStatusCancelled
	intent.UpdatedAt = time.Now()
	if err := a.idempotencyStore.UpdateIntent(ctx, intent); err != nil {
		return fmt.Errorf("update intent after cancel: %w", err)
	}

	// Audit log
	a.logAuditEvent(ctx, msg.IntentID, "cancelled", exchange, intent.Request.Symbol, msg.Reason, nil)

	// Publish event
	a.publishEvent(ctx, ports.EventTypeOrderCancelled, intent)

	return nil
}

// handleGetOrderStatus processes status requests (via Ask pattern)
func (a *ExecutionActor) handleGetOrderStatus(ctx context.Context, env actor.Envelope) error {
	msg, ok := env.Message.(GetOrderStatusMsg)
	if !ok {
		return actor.ErrInvalidMessage
	}

	intent, exists := a.intents[msg.IntentID]
	if !exists {
		loaded, err := a.idempotencyStore.GetIntent(ctx, msg.IntentID)
		if err != nil {
			return fmt.Errorf("load intent %s: %w", msg.IntentID, err)
		}
		if loaded == nil {
			return fmt.Errorf("intent not found: %s", msg.IntentID)
		}
		intent = loaded
	}

	// Reply with status if reply channel exists
	if env.Reply != nil {
		env.Reply <- intent
	}

	return nil
}

// handleFillUpdate processes order fill updates
func (a *ExecutionActor) handleFillUpdate(ctx context.Context, msg OrderFillUpdateMsg) error {
	intentID, intent, err := a.resolveIntentByExchangeAndOrderID(ctx, msg.Exchange, msg.OrderID)
	if err != nil {
		return err
	}

	// Update fill information
	intent.FilledAmount = msg.FilledAmount
	intent.FillPrice = msg.FillPrice
	intent.UpdatedAt = time.Now()

	// Check if fully filled
	if intent.FilledAmount.GreaterThanOrEqual(intent.Request.Amount) {
		intent.Status = ports.OrderStatusFilled
	}

	// Persist update
	if err := a.idempotencyStore.UpdateIntent(ctx, intent); err != nil {
		return fmt.Errorf("update intent after fill: %w", err)
	}

	// Audit log
	a.logAuditEvent(ctx, intentID, "filled", msg.Exchange, intent.Request.Symbol, "", map[string]interface{}{
		"filled_amount": msg.FilledAmount,
		"fill_price":    msg.FillPrice,
	})

	// Publish event
	a.publishEvent(ctx, ports.EventTypeOrderFilled, intent)

	return nil
}

// handleRejected processes order rejection updates
func (a *ExecutionActor) handleRejected(ctx context.Context, msg OrderRejectedMsg) error {
	intentID, intent, err := a.resolveIntentByExchangeAndOrderID(ctx, msg.Exchange, msg.OrderID)
	if err != nil {
		return err
	}

	// Update status
	intent.Status = ports.OrderStatusRejected
	intent.RejectReason = msg.Reason
	intent.UpdatedAt = time.Now()

	// Persist update
	if err := a.idempotencyStore.UpdateIntent(ctx, intent); err != nil {
		return fmt.Errorf("update intent after reject: %w", err)
	}

	// Audit log
	a.logAuditEvent(ctx, intentID, "rejected", msg.Exchange, intent.Request.Symbol, msg.Reason, nil)

	// Publish event
	a.publishEvent(ctx, ports.EventTypeOrderRejected, intent)

	return nil
}

func (a *ExecutionActor) resolveIntentByExchangeAndOrderID(
	ctx context.Context,
	exchange string,
	exchangeOrderID string,
) (string, *OrderIntent, error) {
	key := exchangeOrderKey(exchange, exchangeOrderID)
	if intentID, exists := a.exchangeOrderToIntent[key]; exists {
		if intent, ok := a.intents[intentID]; ok {
			return intentID, intent, nil
		}

		loaded, err := a.idempotencyStore.GetIntent(ctx, intentID)
		if err != nil {
			return "", nil, fmt.Errorf("load intent %s: %w", intentID, err)
		}
		if loaded == nil {
			return "", nil, fmt.Errorf("intent not found: %s", intentID)
		}

		a.intents[intentID] = loaded
		if loaded.ClientOrderID != "" {
			a.clientIDToIntent[loaded.ClientOrderID] = intentID
		}
		if loaded.ExchangeOrderID != "" {
			a.exchangeOrderToIntent[exchangeOrderKey(loaded.Request.Exchange, loaded.ExchangeOrderID)] = intentID
		}
		return intentID, loaded, nil
	}

	loaded, err := a.idempotencyStore.GetIntentByExchangeID(ctx, exchange, exchangeOrderID)
	if err != nil {
		return "", nil, fmt.Errorf("lookup intent by exchange=%s orderID=%s: %w", exchange, exchangeOrderID, err)
	}
	if loaded == nil {
		return "", nil, fmt.Errorf("unknown order exchange=%s orderID=%s", exchange, exchangeOrderID)
	}

	intentID := loaded.IntentID
	a.intents[intentID] = loaded
	if loaded.ClientOrderID != "" {
		a.clientIDToIntent[loaded.ClientOrderID] = intentID
	}
	if loaded.ExchangeOrderID != "" {
		a.exchangeOrderToIntent[exchangeOrderKey(loaded.Request.Exchange, loaded.ExchangeOrderID)] = intentID
	}
	return intentID, loaded, nil
}

// validateRequest validates an order request
func (a *ExecutionActor) validateRequest(req *ports.OrderRequest) error {
	if req.Exchange == "" {
		return errors.New("exchange is required")
	}
	if req.Symbol == "" {
		return errors.New("symbol is required")
	}
	if req.Side != ports.OrderSideBuy && req.Side != ports.OrderSideSell {
		return errors.New("side must be buy or sell")
	}
	if req.Type != ports.OrderTypeMarket && req.Type != ports.OrderTypeLimit {
		return errors.New("order type must be market or limit")
	}
	if req.Amount.LessThanOrEqual(decimal.Zero) {
		return errors.New("amount must be greater than zero")
	}
	if req.Type == ports.OrderTypeLimit && req.Price.LessThanOrEqual(decimal.Zero) {
		return errors.New("limit orders require a positive price")
	}
	return nil
}

// logAuditEvent creates and persists an audit event
func (a *ExecutionActor) logAuditEvent(ctx context.Context, intentID, eventType, exchange, symbol, reason string, metadata map[string]interface{}) {
	if a.auditLog == nil {
		zaplogrus.Warnf("[AUDIT] logger unavailable for intent %s event %s", intentID, eventType)
		return
	}

	event := OrderAuditEvent{
		EventID:   generateEventID(),
		IntentID:  intentID,
		EventType: eventType,
		Exchange:  exchange,
		Symbol:    symbol,
		Reason:    sanitizeMessage(reason),
		Metadata:  metadata,
		Timestamp: time.Now(),
	}

	// Derive hash chain from cached prior hash when available.
	previousHash := a.lastAuditHash[intentID]
	if previousHash == "" {
		if history, err := a.auditLog.GetOrderHistory(ctx, intentID); err == nil && len(history) > 0 {
			lastEvent := history[len(history)-1]
			hash, hashErr := calculateHash(lastEvent)
			if hashErr != nil {
				zaplogrus.Warnf("[AUDIT] failed to hash previous event intent=%s type=%s: %s", intentID, eventType, sanitizeExternalError(hashErr))
			} else {
				previousHash = hash
			}
		}
	}
	if previousHash != "" {
		event.HashChain = previousHash
	}

	if intent, exists := a.intents[intentID]; exists {
		event.ClientOrderID = intent.ClientOrderID
		event.ExchangeOrderID = intent.ExchangeOrderID
		event.Side = string(intent.Request.Side)
		event.Amount = intent.Request.Amount
		event.Price = intent.Request.Price
		event.FilledAmount = intent.FilledAmount
		event.FillPrice = intent.FillPrice
	}

	if err := a.auditLog.LogOrderEvent(ctx, event); err != nil {
		// Audit logging is best-effort by design.
		zaplogrus.Warnf("[AUDIT] failed to log event intent=%s type=%s: %s", intentID, eventType, sanitizeExternalError(err))
		return
	}

	eventHash, err := calculateHash(event)
	if err != nil {
		zaplogrus.Warnf("[AUDIT] failed to hash event intent=%s type=%s: %s", intentID, eventType, sanitizeExternalError(err))
		return
	}
	a.lastAuditHash[intentID] = eventHash
	if intent, exists := a.intents[intentID]; exists {
		intent.LastAuditHash = eventHash
	}
}

// publishEvent publishes an event to the event bus
func (a *ExecutionActor) publishEvent(ctx context.Context, eventType string, intent *OrderIntent) {
	if a.eventBus == nil {
		return
	}

	event := ports.BaseEvent{
		Type:       eventType,
		Aggregate:  intent.IntentID,
		OccurredAt: time.Now().Unix(),
	}

	// Wrap with order-specific data
	orderEvent := OrderEvent{
		BaseEvent:       event,
		IntentID:        intent.IntentID,
		ClientOrderID:   intent.ClientOrderID,
		ExchangeOrderID: intent.ExchangeOrderID,
		Exchange:        intent.Request.Exchange,
		Symbol:          intent.Request.Symbol,
		Side:            string(intent.Request.Side),
		Amount:          intent.Request.Amount,
		FilledAmount:    intent.FilledAmount,
		Status:          string(intent.Status),
	}

	if err := a.eventBus.Publish(ctx, orderEvent); err != nil {
		zaplogrus.Warnf("[EVENT] failed to publish intent=%s type=%s: %s", intent.IntentID, eventType, sanitizeExternalError(err))
	}
}

// OrderEvent wraps domain events with order data
type OrderEvent struct {
	ports.BaseEvent
	IntentID        string          `json:"intent_id"`
	ClientOrderID   string          `json:"client_order_id"`
	ExchangeOrderID string          `json:"exchange_order_id,omitempty"`
	Exchange        string          `json:"exchange"`
	Symbol          string          `json:"symbol"`
	Side            string          `json:"side"`
	Amount          decimal.Decimal `json:"amount"`
	FilledAmount    decimal.Decimal `json:"filled_amount"`
	Status          string          `json:"status"`
}

// generateClientOrderID creates a deterministic client order ID from intent ID
func generateClientOrderID(intentID string, attempt int) string {
	data := fmt.Sprintf("%s:%d", intentID, attempt)
	hash := sha256.Sum256([]byte(data))
	// Use first 16 chars of hex for exchange compatibility
	return "NT" + hex.EncodeToString(hash[:])[:16]
}

// generateEventID creates a unique event ID
func generateEventID() string {
	return fmt.Sprintf("evt_%s", uuid.NewString())
}

// calculateHash computes a hash of an audit event for tamper detection
func calculateHash(event OrderAuditEvent) (string, error) {
	data, err := json.Marshal(event)
	if err != nil {
		return "", fmt.Errorf("marshal audit event: %w", err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:]), nil
}

func exchangeOrderKey(exchange, orderID string) string {
	return strings.ToLower(strings.TrimSpace(exchange)) + ":" + strings.TrimSpace(orderID)
}

func sanitizeExternalError(err error) string {
	if err == nil {
		return ""
	}

	return sanitizeMessage(err.Error())
}

func sanitizeMessage(message string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(message, "\n", " "))
	lower := strings.ToLower(normalized)
	for _, marker := range []string{"api key", "secret", "token", "authorization", "passphrase", "signature"} {
		if strings.Contains(lower, marker) {
			return "external provider error (redacted)"
		}
	}

	if len(normalized) > 256 {
		return normalized[:256]
	}
	return normalized
}

// Compile-time interface check
var _ actor.Actor = (*ExecutionActor)(nil)
