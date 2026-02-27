// Package execution provides the ExecutionService facade for external consumers.
// This service wraps the ExecutionActor and provides a simpler API for the rest of the application.
package execution

import (
	"context"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// ExecutionService provides a high-level API for order execution
type ExecutionService struct {
	actorRef *actor.Ref
	actor    *ExecutionActor
}

// ServiceConfig holds configuration for the execution service
type ServiceConfig struct {
	ActorConfig      actor.Config
	Gateway          ports.TradingGateway
	EventBus         ports.EventBus
	IdempotencyStore IdempotencyStore
	AuditLog         AuditLogger
}

// NewExecutionService creates and initializes the execution service
func NewExecutionService(config ServiceConfig) (*ExecutionService, error) {
	if config.Gateway == nil {
		return nil, fmt.Errorf("trading gateway is required")
	}

	// Create the execution actor
	execActor := NewExecutionActor(
		"execution-actor",
		config.Gateway,
		config.EventBus,
		config.IdempotencyStore,
		config.AuditLog,
	)

	// Create actor reference with mailbox
	actorRef := actor.NewRef(execActor, config.ActorConfig)

	return &ExecutionService{
		actorRef: actorRef,
		actor:    execActor,
	}, nil
}

// Start starts the execution service and its actor
func (s *ExecutionService) Start(ctx context.Context) error {
	// Start the actor's message processing loop
	go func() {
		if err := s.actorRef.Run(ctx); err != nil && err != context.Canceled {
			fmt.Printf("[ExecutionService] Actor stopped with error: %v\n", err)
		}
	}()

	// Wait for actor to be running
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return fmt.Errorf("timeout waiting for execution actor to start")
		case <-ticker.C:
			if s.actorRef.IsRunning() {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Stop gracefully stops the execution service
func (s *ExecutionService) Stop() error {
	s.actorRef.Stop()
	return nil
}

// IsRunning returns whether the service is running
func (s *ExecutionService) IsRunning() bool {
	return s.actorRef.IsRunning()
}

// PlaceOrder submits an order with idempotency guarantees
func (s *ExecutionService) PlaceOrder(ctx context.Context, req PlaceOrderRequest) (*PlaceOrderResponse, error) {
	if !s.actorRef.IsRunning() {
		return nil, ErrActorStopped
	}

	msg := PlaceOrderMsg{
		IntentID:     req.IntentID,
		Request:      req.OrderRequest,
		RiskApproved: req.RiskApproved,
		StrategyID:   req.StrategyID,
		SignalID:     req.SignalID,
		Metadata:     req.Metadata,
	}

	if err := s.actorRef.Send(ctx, msg); err != nil {
		return nil, fmt.Errorf("failed to send place order message: %w", err)
	}

	return &PlaceOrderResponse{
		IntentID: req.IntentID,
		Status:   "submitted",
	}, nil
}

// PlaceOrderRequest represents a request to place an order
type PlaceOrderRequest struct {
	IntentID     string                 // Unique ID for this intent (idempotency key)
	OrderRequest ports.OrderRequest     // The actual order request
	RiskApproved bool                   // Whether risk checks passed
	StrategyID   string                 // Optional: originating strategy
	SignalID     string                 // Optional: originating signal
	Metadata     map[string]interface{} // Additional context
}

// PlaceOrderResponse represents the response from placing an order
type PlaceOrderResponse struct {
	IntentID string `json:"intent_id"`
	Status   string `json:"status"`
}

// CancelOrder cancels an existing order
func (s *ExecutionService) CancelOrder(ctx context.Context, req CancelOrderRequest) error {
	if !s.actorRef.IsRunning() {
		return ErrActorStopped
	}

	msg := CancelOrderMsg{
		IntentID: req.IntentID,
		OrderID:  req.OrderID,
		Exchange: req.Exchange,
		Reason:   req.Reason,
	}

	if err := s.actorRef.Send(ctx, msg); err != nil {
		return fmt.Errorf("failed to send cancel order message: %w", err)
	}

	return nil
}

// CancelOrderRequest represents a request to cancel an order
type CancelOrderRequest struct {
	IntentID string // Original intent ID
	OrderID  string // Exchange order ID
	Exchange string
	Reason   string
}

// GetOrderStatus retrieves the current status of an order
func (s *ExecutionService) GetOrderStatus(ctx context.Context, intentID string) (*OrderStatusResponse, error) {
	if !s.actorRef.IsRunning() {
		return nil, ErrActorStopped
	}

	msg := GetOrderStatusMsg{
		IntentID: intentID,
	}

	result, err := s.actorRef.Ask(ctx, msg)
	if err != nil {
		return nil, fmt.Errorf("failed to get order status: %w", err)
	}

	intent, ok := result.(*OrderIntent)
	if !ok {
		return nil, fmt.Errorf("unexpected response type from actor")
	}

	return &OrderStatusResponse{
		IntentID:        intent.IntentID,
		ClientOrderID:   intent.ClientOrderID,
		ExchangeOrderID: intent.ExchangeOrderID,
		Status:          string(intent.Status),
		Exchange:        intent.Request.Exchange,
		Symbol:          intent.Request.Symbol,
		Side:            string(intent.Request.Side),
		Amount:          intent.Request.Amount,
		FilledAmount:    intent.FilledAmount,
		FillPrice:       intent.FillPrice,
		SubmittedAt:     intent.SubmittedAt,
		UpdatedAt:       intent.UpdatedAt,
		IsTerminal:      intent.IsTerminal(),
	}, nil
}

// OrderStatusResponse represents the current status of an order
type OrderStatusResponse struct {
	IntentID        string          `json:"intent_id"`
	ClientOrderID   string          `json:"client_order_id"`
	ExchangeOrderID string          `json:"exchange_order_id,omitempty"`
	Status          string          `json:"status"`
	Exchange        string          `json:"exchange"`
	Symbol          string          `json:"symbol"`
	Side            string          `json:"side"`
	Amount          decimal.Decimal `json:"amount"`
	FilledAmount    decimal.Decimal `json:"filled_amount"`
	FillPrice       decimal.Decimal `json:"fill_price,omitempty"`
	SubmittedAt     time.Time       `json:"submitted_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	IsTerminal      bool            `json:"is_terminal"`
}

// GetAuditHistory retrieves the audit trail for an order
func (s *ExecutionService) GetAuditHistory(ctx context.Context, intentID string) ([]OrderAuditEvent, error) {
	return s.actor.auditLog.GetOrderHistory(ctx, intentID)
}

// WaitForTerminalState waits for an order to reach a terminal state
func (s *ExecutionService) WaitForTerminalState(ctx context.Context, intentID string, timeout time.Duration) (*OrderStatusResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for terminal state: %w", ctx.Err())
		case <-ticker.C:
			status, err := s.GetOrderStatus(ctx, intentID)
			if err != nil {
				return nil, err
			}
			if status.IsTerminal {
				return status, nil
			}
		}
	}
}

// Helper types and errors
var (
	ErrActorStopped = fmt.Errorf("execution actor is stopped")
)
