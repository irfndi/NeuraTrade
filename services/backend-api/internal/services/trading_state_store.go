package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
)

// TradingStateStore manages persistent trading state for crash recovery
type TradingStateStore struct {
	mu     sync.RWMutex
	db     *pgxpool.Pool
	logger *log.Logger
	// In-memory cache for fast access
	openPositions     map[string]*PositionRecord
	pendingOrders     map[string]*OrderRecord
	dailyPnL          decimal.Decimal
	consecutiveLosses int
	emergencyMode     bool
	lastCheckpoint    time.Time
}

// PositionRecord represents a tracked position
type PositionRecord struct {
	PositionID   string          `json:"position_id"`
	Symbol       string          `json:"symbol"`
	Exchange     string          `json:"exchange"`
	Side         string          `json:"side"`
	Size         decimal.Decimal `json:"size"`
	EntryPrice   decimal.Decimal `json:"entry_price"`
	CurrentPrice decimal.Decimal `json:"current_price"`
	Status       string          `json:"status"`
	OpenedAt     time.Time       `json:"opened_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

// OrderRecord represents a pending order
type OrderRecord struct {
	OrderID   string          `json:"order_id"`
	Symbol    string          `json:"symbol"`
	Exchange  string          `json:"exchange"`
	Side      string          `json:"side"`
	Type      string          `json:"type"`
	Size      decimal.Decimal `json:"size"`
	Price     decimal.Decimal `json:"price"`
	Status    string          `json:"status"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// TradingStateStoreInterface defines the interface for trading state persistence
type TradingStateStoreInterface interface {
	// Persist saves the current state to persistent storage
	Persist(ctx context.Context) error
	// Restore loads state from persistent storage
	Restore(ctx context.Context) error
	// Checkpoint creates a periodic snapshot
	Checkpoint(ctx context.Context) error
	// Validate checks state integrity
	Validate(ctx context.Context) error

	// Position operations
	AddPosition(ctx context.Context, pos *PositionRecord) error
	UpdatePosition(ctx context.Context, pos *PositionRecord) error
	RemovePosition(ctx context.Context, positionID string) error
	GetPosition(ctx context.Context, positionID string) (*PositionRecord, error)
	ListOpenPositions(ctx context.Context) ([]*PositionRecord, error)

	// Order operations
	AddOrder(ctx context.Context, order *OrderRecord) error
	UpdateOrder(ctx context.Context, order *OrderRecord) error
	RemoveOrder(ctx context.Context, orderID string) error
	GetOrder(ctx context.Context, orderID string) (*OrderRecord, error)
	ListPendingOrders(ctx context.Context) ([]*OrderRecord, error)

	// State operations
	SetDailyPnL(ctx context.Context, pnl decimal.Decimal) error
	GetDailyPnL(ctx context.Context) decimal.Decimal
	IncrementConsecutiveLosses(ctx context.Context) int
	ResetConsecutiveLosses(ctx context.Context)
	SetEmergencyMode(ctx context.Context, enabled bool) bool
	GetEmergencyMode(ctx context.Context) bool
}

// NewTradingStateStore creates a new trading state store
func NewTradingStateStore(db *pgxpool.Pool) *TradingStateStore {
	return &TradingStateStore{
		db:                db,
		logger:            log.Default(),
		openPositions:     make(map[string]*PositionRecord),
		pendingOrders:     make(map[string]*OrderRecord),
		dailyPnL:          decimal.Zero,
		consecutiveLosses: 0,
		emergencyMode:     false,
	}
}

// Persist saves the current state to persistent storage
func (s *TradingStateStore) Persist(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stateJSON, err := json.Marshal(s.getStateForPersist())
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	// Store in database - using a simple key-value approach
	_, err = s.db.Exec(ctx, `
		INSERT INTO kv_store (key, value, updated_at)
		VALUES ('trading_state', $1, NOW())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = NOW()
	`, stateJSON)

	if err != nil {
		return fmt.Errorf("failed to persist state: %w", err)
	}

	return nil
}

// Restore loads state from persistent storage
func (s *TradingStateStore) Restore(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var stateJSON []byte
	err := s.db.QueryRow(ctx, "SELECT value FROM kv_store WHERE key = 'trading_state'").Scan(&stateJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("failed to restore state: %w", err)
	}

	var state map[string]interface{}
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return fmt.Errorf("failed to unmarshal state: %w", err)
	}

	var restoreErrors []string

	// Restore positions
	if positions, ok := state["open_positions"].(map[string]interface{}); ok {
		for id, v := range positions {
			data, err := json.Marshal(v)
			if err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("position %s: marshal error", id))
				continue
			}
			var pos PositionRecord
			if err := json.Unmarshal(data, &pos); err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("position %s: %v", id, err))
				continue
			}
			s.openPositions[id] = &pos
		}
	}

	// Restore orders
	if orders, ok := state["pending_orders"].(map[string]interface{}); ok {
		for id, v := range orders {
			data, err := json.Marshal(v)
			if err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("order %s: marshal error", id))
				continue
			}
			var order OrderRecord
			if err := json.Unmarshal(data, &order); err != nil {
				restoreErrors = append(restoreErrors, fmt.Sprintf("order %s: %v", id, err))
				continue
			}
			s.pendingOrders[id] = &order
		}
	}

	if len(restoreErrors) > 0 {
		s.logger.Printf("State restoration warnings: %v", restoreErrors)
	}

	// Restore daily PnL
	if pnl, ok := state["daily_pnl"].(string); ok {
		s.dailyPnL, _ = decimal.NewFromString(pnl)
	}

	// Restore consecutive losses
	if cl, ok := state["consecutive_losses"].(float64); ok {
		s.consecutiveLosses = int(cl)
	}

	// Restore emergency mode
	if em, ok := state["emergency_mode"].(bool); ok {
		s.emergencyMode = em
	}

	s.lastCheckpoint = time.Now()
	return nil
}

// Checkpoint creates a periodic snapshot (alias for Persist for clarity)
func (s *TradingStateStore) Checkpoint(ctx context.Context) error {
	return s.Persist(ctx)
}

// Validate checks state integrity
func (s *TradingStateStore) Validate(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check for duplicate position IDs
	seen := make(map[string]bool)
	for id := range s.openPositions {
		if seen[id] {
			return fmt.Errorf("duplicate position ID: %s", id)
		}
		seen[id] = true
	}

	// Check for duplicate order IDs
	seen = make(map[string]bool)
	for id := range s.pendingOrders {
		if seen[id] {
			return fmt.Errorf("duplicate order ID: %s", id)
		}
		seen[id] = true
	}

	return nil
}

// getStateForPersist returns a map representation of the state
func (s *TradingStateStore) getStateForPersist() map[string]interface{} {
	return map[string]interface{}{
		"open_positions":     s.openPositions,
		"pending_orders":     s.pendingOrders,
		"daily_pnl":          s.dailyPnL.String(),
		"consecutive_losses": s.consecutiveLosses,
		"emergency_mode":     s.emergencyMode,
		"last_checkpoint":    s.lastCheckpoint,
	}
}

// Position operations

// AddPosition adds a new position to the state
func (s *TradingStateStore) AddPosition(ctx context.Context, pos *PositionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.openPositions[pos.PositionID]; exists {
		return fmt.Errorf("position already exists: %s", pos.PositionID)
	}

	pos.UpdatedAt = time.Now()
	s.openPositions[pos.PositionID] = pos
	return nil
}

// UpdatePosition updates an existing position
func (s *TradingStateStore) UpdatePosition(ctx context.Context, pos *PositionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.openPositions[pos.PositionID]; !exists {
		return fmt.Errorf("position not found: %s", pos.PositionID)
	}

	pos.UpdatedAt = time.Now()
	s.openPositions[pos.PositionID] = pos
	return nil
}

// RemovePosition removes a position from the state
func (s *TradingStateStore) RemovePosition(ctx context.Context, positionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.openPositions[positionID]; !exists {
		return fmt.Errorf("position not found: %s", positionID)
	}

	delete(s.openPositions, positionID)
	return nil
}

// GetPosition retrieves a position by ID
func (s *TradingStateStore) GetPosition(ctx context.Context, positionID string) (*PositionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	pos, exists := s.openPositions[positionID]
	if !exists {
		return nil, fmt.Errorf("position not found: %s", positionID)
	}

	return pos, nil
}

// ListOpenPositions returns all open positions
func (s *TradingStateStore) ListOpenPositions(ctx context.Context) ([]*PositionRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	positions := make([]*PositionRecord, 0, len(s.openPositions))
	for _, pos := range s.openPositions {
		positions = append(positions, pos)
	}

	return positions, nil
}

// Order operations

// AddOrder adds a new order to the state
func (s *TradingStateStore) AddOrder(ctx context.Context, order *OrderRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.pendingOrders[order.OrderID]; exists {
		return fmt.Errorf("order already exists: %s", order.OrderID)
	}

	order.UpdatedAt = time.Now()
	s.pendingOrders[order.OrderID] = order
	return nil
}

// UpdateOrder updates an existing order
func (s *TradingStateStore) UpdateOrder(ctx context.Context, order *OrderRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.pendingOrders[order.OrderID]; !exists {
		return fmt.Errorf("order not found: %s", order.OrderID)
	}

	order.UpdatedAt = time.Now()
	s.pendingOrders[order.OrderID] = order
	return nil
}

// RemoveOrder removes an order from the state
func (s *TradingStateStore) RemoveOrder(ctx context.Context, orderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.pendingOrders[orderID]; !exists {
		return fmt.Errorf("order not found: %s", orderID)
	}

	delete(s.pendingOrders, orderID)
	return nil
}

// GetOrder retrieves an order by ID
func (s *TradingStateStore) GetOrder(ctx context.Context, orderID string) (*OrderRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	order, exists := s.pendingOrders[orderID]
	if !exists {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	return order, nil
}

// ListPendingOrders returns all pending orders
func (s *TradingStateStore) ListPendingOrders(ctx context.Context) ([]*OrderRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orders := make([]*OrderRecord, 0)
	for _, order := range s.pendingOrders {
		if order.Status == "PENDING" {
			orders = append(orders, order)
		}
	}

	return orders, nil
}

// State operations

// SetDailyPnL sets the daily profit/loss
func (s *TradingStateStore) SetDailyPnL(ctx context.Context, pnl decimal.Decimal) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dailyPnL = pnl
	return nil
}

// GetDailyPnL returns the daily profit/loss
func (s *TradingStateStore) GetDailyPnL(ctx context.Context) decimal.Decimal {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dailyPnL
}

// IncrementConsecutiveLosses increments the consecutive losses counter
func (s *TradingStateStore) IncrementConsecutiveLosses(ctx context.Context) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveLosses++
	return s.consecutiveLosses
}

// ResetConsecutiveLosses resets the consecutive losses counter
func (s *TradingStateStore) ResetConsecutiveLosses(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecutiveLosses = 0
}

// SetEmergencyMode enables or disables emergency mode
func (s *TradingStateStore) SetEmergencyMode(ctx context.Context, enabled bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emergencyMode = enabled
	return s.emergencyMode
}

// GetEmergencyMode returns the current emergency mode status
func (s *TradingStateStore) GetEmergencyMode(ctx context.Context) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.emergencyMode
}

// Ensure TradingStateStore implements TradingStateStoreInterface
var _ TradingStateStoreInterface = (*TradingStateStore)(nil)

// DBPool interface compatibility - ensure database package is used
func init() {
	_ = database.DBPool(nil)
}
