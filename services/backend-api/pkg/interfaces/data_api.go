package interfaces

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

type PositionStatus string

const (
	PositionStatusOpen       PositionStatus = "OPEN"
	PositionStatusClosed     PositionStatus = "CLOSED"
	PositionStatusLiquidated PositionStatus = "LIQUIDATED"
)

type Position struct {
	PositionID   string          `json:"position_id"`
	OrderID      string          `json:"order_id"`
	Exchange     string          `json:"exchange"`
	Symbol       string          `json:"symbol"`
	Side         string          `json:"side"`
	Size         decimal.Decimal `json:"size"`
	EntryPrice   decimal.Decimal `json:"entry_price"`
	CurrentPrice decimal.Decimal `json:"current_price"`
	UnrealizedPL decimal.Decimal `json:"unrealized_pl"`
	Status       PositionStatus  `json:"status"`
	OpenedAt     time.Time       `json:"opened_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

type PositionInterface interface {
	GetPositionID() string
	GetOrderID() string
	GetExchange() string
	GetSymbol() string
	GetSide() string
	GetSize() decimal.Decimal
	GetEntryPrice() decimal.Decimal
	GetCurrentPrice() decimal.Decimal
	GetUnrealizedPL() decimal.Decimal
	GetStatus() PositionStatus
	GetOpenedAt() time.Time
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

type Balance struct {
	Asset    string          `json:"asset"`
	Free     decimal.Decimal `json:"free"`
	Locked   decimal.Decimal `json:"locked"`
	Total    decimal.Decimal `json:"total"`
	USDValue decimal.Decimal `json:"usd_value"`
}

type BalanceInterface interface {
	GetAsset() string
	GetFree() decimal.Decimal
	GetLocked() decimal.Decimal
	GetTotal() decimal.Decimal
	GetUSDValue() decimal.Decimal
}

func (b *Balance) GetAsset() string             { return b.Asset }
func (b *Balance) GetFree() decimal.Decimal     { return b.Free }
func (b *Balance) GetLocked() decimal.Decimal   { return b.Locked }
func (b *Balance) GetTotal() decimal.Decimal    { return b.Total }
func (b *Balance) GetUSDValue() decimal.Decimal { return b.USDValue }

type Portfolio struct {
	TotalValue decimal.Decimal `json:"total_value"`
	Positions  []Position      `json:"positions"`
	Balances   []Balance       `json:"balances"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type PortfolioInterface interface {
	GetTotalValue() decimal.Decimal
	GetPositions() []Position
	GetBalances() []Balance
	GetUpdatedAt() time.Time
}

func (pf *Portfolio) GetTotalValue() decimal.Decimal { return pf.TotalValue }
func (pf *Portfolio) GetPositions() []Position       { return pf.Positions }
func (pf *Portfolio) GetBalances() []Balance         { return pf.Balances }
func (pf *Portfolio) GetUpdatedAt() time.Time        { return pf.UpdatedAt }

type DataAPIInterface interface {
	GetPositions(ctx context.Context, filter string) ([]Position, error)
	GetPosition(ctx context.Context, positionID string) (*Position, error)
	GetBalances(ctx context.Context) (map[string]decimal.Decimal, error)
	GetPortfolio(ctx context.Context) (*Portfolio, error)
}
