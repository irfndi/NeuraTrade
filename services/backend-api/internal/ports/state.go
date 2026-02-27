// Package ports defines the application's port interfaces.
package ports

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// ============================================================
// State Repositories - Persistence contracts
// ============================================================

// Position represents a stored position.
type StoredPosition struct {
	ID            string
	Exchange      string
	Symbol        string
	Side          string
	Amount        decimal.Decimal
	EntryPrice    decimal.Decimal
	CurrentPrice  decimal.Decimal
	UnrealizedPnL decimal.Decimal
	RealizedPnL   decimal.Decimal
	OpenedAt      time.Time
	UpdatedAt     time.Time
	ClosedAt      *time.Time
	Status        string
	StrategyID    string
	Metadata      map[string]any
}

// Order represents a stored order.
type StoredOrder struct {
	ID              string
	Exchange        string
	Symbol          string
	ExchangeOrderID string
	ClientOrderID   string
	Side            string
	Type            string
	Amount          decimal.Decimal
	FilledAmount    decimal.Decimal
	Price           decimal.Decimal
	AveragePrice    decimal.Decimal
	Status          string
	StrategyID      string
	SignalID        string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ClosedAt        *time.Time
	Metadata        map[string]any
}

// Trade represents a stored trade (fill).
type StoredTrade struct {
	ID              string
	Exchange        string
	Symbol          string
	OrderID         string
	ExchangeOrderID string
	Side            string
	Amount          decimal.Decimal
	Price           decimal.Decimal
	Fee             decimal.Decimal
	FeeCurrency     string
	PnL             decimal.Decimal
	PositionID      string
	ExecutedAt      time.Time
	Metadata        map[string]any
}

// Signal represents a stored trading signal.
type StoredSignal struct {
	ID          string
	Exchange    string
	Symbol      string
	StrategyID  string
	Side        string
	Confidence  decimal.Decimal
	Price       decimal.Decimal
	StopLoss    decimal.Decimal
	TakeProfit  decimal.Decimal
	Status      string
	GeneratedAt time.Time
	ProcessedAt *time.Time
	Metadata    map[string]any
}

// PositionsRepository manages position persistence.
type PositionsRepository interface {
	// Create creates a new position.
	Create(ctx context.Context, pos StoredPosition) (StoredPosition, error)

	// Update updates a position.
	Update(ctx context.Context, pos StoredPosition) error

	// GetByID fetches a position by ID.
	GetByID(ctx context.Context, id string) (StoredPosition, error)

	// GetOpen fetches all open positions.
	GetOpen(ctx context.Context) ([]StoredPosition, error)

	// GetOpenBySymbol fetches open positions for a symbol.
	GetOpenBySymbol(ctx context.Context, exchange, symbol string) ([]StoredPosition, error)

	// Close closes a position.
	Close(ctx context.Context, id string, closedAt time.Time, realizedPnL decimal.Decimal) error
}

// OrdersRepository manages order persistence.
type OrdersRepository interface {
	// Create creates a new order.
	Create(ctx context.Context, order StoredOrder) (StoredOrder, error)

	// Update updates an order.
	Update(ctx context.Context, order StoredOrder) error

	// GetByID fetches an order by ID.
	GetByID(ctx context.Context, id string) (StoredOrder, error)

	// GetByExchangeOrderID fetches an order by exchange order ID.
	GetByExchangeOrderID(ctx context.Context, exchange, exchangeOrderID string) (StoredOrder, error)

	// GetByClientOrderID fetches an order by client order ID.
	GetByClientOrderID(ctx context.Context, exchange, clientOrderID string) (StoredOrder, error)

	// GetOpen fetches all open orders.
	GetOpen(ctx context.Context) ([]StoredOrder, error)

	// GetRecent fetches recent orders.
	GetRecent(ctx context.Context, limit int) ([]StoredOrder, error)
}

// TradesRepository manages trade persistence.
type TradesRepository interface {
	// Create creates a new trade.
	Create(ctx context.Context, trade StoredTrade) (StoredTrade, error)

	// GetByID fetches a trade by ID.
	GetByID(ctx context.Context, id string) (StoredTrade, error)

	// GetByOrderID fetches trades for an order.
	GetByOrderID(ctx context.Context, orderID string) ([]StoredTrade, error)

	// GetRecent fetches recent trades.
	GetRecent(ctx context.Context, limit int) ([]StoredTrade, error)

	// GetByPositionID fetches trades for a position.
	GetByPositionID(ctx context.Context, positionID string) ([]StoredTrade, error)
}

// SignalsRepository manages signal persistence.
type SignalsRepository interface {
	// Create creates a new signal.
	Create(ctx context.Context, signal StoredSignal) (StoredSignal, error)

	// Update updates a signal.
	Update(ctx context.Context, signal StoredSignal) error

	// GetByID fetches a signal by ID.
	GetByID(ctx context.Context, id string) (StoredSignal, error)

	// GetPending fetches pending signals.
	GetPending(ctx context.Context, limit int) ([]StoredSignal, error)

	// MarkProcessed marks a signal as processed.
	MarkProcessed(ctx context.Context, id string, processedAt time.Time) error
}

// ============================================================
// Configuration Repository
// ============================================================

// StrategyConfig represents a strategy configuration.
type StrategyConfig struct {
	ID        string
	Name      string
	Enabled   bool
	Config    map[string]any
	UpdatedAt time.Time
}

// RiskConfig represents risk configuration.
type RiskConfig struct {
	MaxPositionSize  decimal.Decimal
	MaxDailyLoss     decimal.Decimal
	MaxDrawdown      decimal.Decimal
	MaxLeverage      decimal.Decimal
	AllowedSymbols   []string
	AllowedExchanges []string
	SafeMode         bool
	KillSwitch       bool
}

// ConfigRepository manages configuration persistence.
type ConfigRepository interface {
	// GetStrategyConfig fetches strategy configuration.
	GetStrategyConfig(ctx context.Context, strategyID string) (StrategyConfig, error)

	// UpdateStrategyConfig updates strategy configuration.
	UpdateStrategyConfig(ctx context.Context, config StrategyConfig) error

	// GetRiskConfig fetches risk configuration.
	GetRiskConfig(ctx context.Context) (RiskConfig, error)

	// UpdateRiskConfig updates risk configuration.
	UpdateRiskConfig(ctx context.Context, config RiskConfig) error

	// GetFeatureFlag fetches a feature flag.
	GetFeatureFlag(ctx context.Context, key string) (bool, error)

	// SetFeatureFlag sets a feature flag.
	SetFeatureFlag(ctx context.Context, key string, value bool) error
}

// ============================================================
// State Store - Unified interface for state management
// ============================================================

// StateStore provides unified access to all state repositories.
type StateStore interface {
	Positions() PositionsRepository
	Orders() OrdersRepository
	Trades() TradesRepository
	Signals() SignalsRepository
	Config() ConfigRepository

	// Health checks if the state store is healthy.
	Health(ctx context.Context) error
}

// ============================================================
// Cache Store
// ============================================================

// CacheStore provides caching capabilities.
type CacheStore interface {
	// Get fetches a value from the cache.
	Get(ctx context.Context, key string) ([]byte, error)

	// Set stores a value in the cache.
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Delete deletes a value from the cache.
	Delete(ctx context.Context, key string) error

	// Exists checks if a key exists.
	Exists(ctx context.Context, key string) (bool, error)

	// Health checks if the cache is healthy.
	Health(ctx context.Context) error
}
