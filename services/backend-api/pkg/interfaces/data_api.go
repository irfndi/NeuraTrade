package interfaces

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// PositionStatus represents the current state of a position.
type PositionStatus string

const (
	// PositionStatusOpen means the position is currently active
	PositionStatusOpen PositionStatus = "OPEN"
	// PositionStatusClosed means the position has been closed
	PositionStatusClosed PositionStatus = "CLOSED"
	// PositionStatusLiquidated means the position was force-closed by the exchange
	PositionStatusLiquidated PositionStatus = "LIQUIDATED"
)

// Position represents a trading position with entry and current price info.
type Position struct {
	// PositionID is the unique identifier for this position
	PositionID string `json:"position_id"`
	// OrderID is the associated order that opened this position
	OrderID string `json:"order_id"`
	// Exchange is the exchange where the position is held
	Exchange string `json:"exchange"`
	// Symbol is the trading pair (e.g., "BTC/USDT")
	Symbol string `json:"symbol"`
	// Side is either "BUY" or "SELL"
	Side string `json:"side"`
	// Size is the amount of the position
	Size decimal.Decimal `json:"size"`
	// EntryPrice is the price at which the position was opened
	EntryPrice decimal.Decimal `json:"entry_price"`
	// CurrentPrice is the current market price
	CurrentPrice decimal.Decimal `json:"current_price"`
	// UnrealizedPL is the profit/loss if closed at current price
	UnrealizedPL decimal.Decimal `json:"unrealized_pl"`
	// Status is the current state of the position
	Status PositionStatus `json:"status"`
	// OpenedAt is when the position was opened
	OpenedAt time.Time `json:"opened_at"`
	// UpdatedAt is the last time the position was updated
	UpdatedAt time.Time `json:"updated_at"`
}

// PositionInterface defines the contract for accessing position data.
type PositionInterface interface {
	// GetPositionID returns the unique position identifier
	GetPositionID() string
	// GetOrderID returns the associated order ID
	GetOrderID() string
	// GetExchange returns the exchange name
	GetExchange() string
	// GetSymbol returns the trading pair
	GetSymbol() string
	// GetSide returns the position side (BUY/SELL)
	GetSide() string
	// GetSize returns the position size
	GetSize() decimal.Decimal
	// GetEntryPrice returns the entry price
	GetEntryPrice() decimal.Decimal
	// GetCurrentPrice returns the current market price
	GetCurrentPrice() decimal.Decimal
	// GetUnrealizedPL returns the unrealized profit/loss
	GetUnrealizedPL() decimal.Decimal
	// GetStatus returns the position status
	GetStatus() PositionStatus
	// GetOpenedAt returns when the position was opened
	GetOpenedAt() time.Time
	// GetUpdatedAt returns the last update time
	GetUpdatedAt() time.Time
}

func (p *Position) GetPositionID() string            { return p.PositionID }
func (p *Position) GetOrderID() string               { return p.OrderID }
func (p *Position) GetExchange() string              { return p.Exchange }
func (p *Position) GetSymbol() string                { return p.Symbol }
func (p *Position) GetSide() string                  { return p.Side }
func (p *Position) GetSize() decimal.Decimal         { return p.Size }
func (p *Position) GetEntryPrice() decimal.Decimal   { return p.EntryPrice }
func (p *Position) GetCurrentPrice() decimal.Decimal { return p.CurrentPrice }
func (p *Position) GetUnrealizedPL() decimal.Decimal { return p.UnrealizedPL }
func (p *Position) GetStatus() PositionStatus        { return p.Status }
func (p *Position) GetOpenedAt() time.Time           { return p.OpenedAt }
func (p *Position) GetUpdatedAt() time.Time          { return p.UpdatedAt }

// Balance represents an asset balance in an account.
type Balance struct {
	// Asset is the currency symbol (e.g., "USDC", "BTC")
	Asset string `json:"asset"`
	// Free is the amount available for trading
	Free decimal.Decimal `json:"free"`
	// Locked is the amount locked in orders
	Locked decimal.Decimal `json:"locked"`
	// Total is the total balance (free + locked)
	Total decimal.Decimal `json:"total"`
	// USDValue is the value in USD
	USDValue decimal.Decimal `json:"usd_value"`
}

// BalanceInterface defines the contract for accessing balance data.
type BalanceInterface interface {
	// GetAsset returns the currency symbol
	GetAsset() string
	// GetFree returns the available balance
	GetFree() decimal.Decimal
	// GetLocked returns the locked balance
	GetLocked() decimal.Decimal
	// GetTotal returns the total balance
	GetTotal() decimal.Decimal
	// GetUSDValue returns the USD equivalent
	GetUSDValue() decimal.Decimal
}

func (b *Balance) GetAsset() string             { return b.Asset }
func (b *Balance) GetFree() decimal.Decimal     { return b.Free }
func (b *Balance) GetLocked() decimal.Decimal   { return b.Locked }
func (b *Balance) GetTotal() decimal.Decimal    { return b.Total }
func (b *Balance) GetUSDValue() decimal.Decimal { return b.USDValue }

// Portfolio represents a collection of positions and balances.
type Portfolio struct {
	// TotalValue is the total portfolio value in USD
	TotalValue decimal.Decimal `json:"total_value"`
	// Positions holds all open positions
	Positions []Position `json:"positions"`
	// Balances holds all asset balances
	Balances []Balance `json:"balances"`
	// UpdatedAt is when this snapshot was taken
	UpdatedAt time.Time `json:"updated_at"`
}

// PortfolioInterface defines the contract for accessing portfolio data.
type PortfolioInterface interface {
	// GetTotalValue returns the total portfolio value
	GetTotalValue() decimal.Decimal
	// GetPositions returns all positions
	GetPositions() []Position
	// GetBalances returns all balances
	GetBalances() []Balance
	// GetUpdatedAt returns the snapshot timestamp
	GetUpdatedAt() time.Time
}

func (pf *Portfolio) GetTotalValue() decimal.Decimal { return pf.TotalValue }
func (pf *Portfolio) GetPositions() []Position       { return pf.Positions }
func (pf *Portfolio) GetBalances() []Balance         { return pf.Balances }
func (pf *Portfolio) GetUpdatedAt() time.Time        { return pf.UpdatedAt }

// DataAPIInterface defines the contract for accessing trading data (positions and balances).
type DataAPIInterface interface {
	// GetPositions retrieves positions with optional status filter.
	//
	// Parameters:
	//   - ctx: Context for the request
	//   - filter: Status filter ("OPEN", "CLOSED", "LIQUIDATED", or empty for all)
	//
	// Returns:
	//   - []Position: List of positions
	//   - error: Any error that occurred
	GetPositions(ctx context.Context, filter string) ([]Position, error)

	// GetPosition retrieves a single position by ID.
	//
	// Parameters:
	//   - ctx: Context for the request
	//   - positionID: The position ID to retrieve
	//
	// Returns:
	//   - *Position: The position details
	//   - error: Any error that occurred
	GetPosition(ctx context.Context, positionID string) (*Position, error)

	// GetBalances retrieves all account balances.
	//
	// Parameters:
	//   - ctx: Context for the request
	//
	// Returns:
	//   - map[string]decimal.Decimal: Map of asset to balance
	//   - error: Any error that occurred
	GetBalances(ctx context.Context) (map[string]decimal.Decimal, error)

	// GetPortfolio retrieves the complete portfolio snapshot.
	//
	// Parameters:
	//   - ctx: Context for the request
	//
	// Returns:
	//   - *Portfolio: Portfolio with positions and balances
	//   - error: Any error that occurred
	GetPortfolio(ctx context.Context) (*Portfolio, error)
}
