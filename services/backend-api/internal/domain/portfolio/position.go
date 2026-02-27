package portfolio

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

const (
	SideBuy  = "buy"
	SideSell = "sell"

	PositionSideLong  = "long"
	PositionSideShort = "short"
	PositionSideFlat  = "flat"
)

type Fill struct {
	TradeID    string
	OrderID    string
	Exchange   string
	Symbol     string
	Side       string
	Quantity   decimal.Decimal
	Price      decimal.Decimal
	Fee        decimal.Decimal
	ExecutedAt time.Time
}

type Position struct {
	Exchange      string
	Symbol        string
	Quantity      decimal.Decimal
	EntryPrice    decimal.Decimal
	MarkPrice     decimal.Decimal
	RealizedPnL   decimal.Decimal
	UnrealizedPnL decimal.Decimal
	LastUpdatedAt time.Time
}

type ApplyResult struct {
	Opened           bool
	Closed           bool
	Reversed         bool
	RealizedPnLDelta decimal.Decimal
	NewQuantity      decimal.Decimal
}

func NewPosition(exchange, symbol string) Position {
	return Position{
		Exchange:      exchange,
		Symbol:        symbol,
		Quantity:      decimal.Zero,
		EntryPrice:    decimal.Zero,
		MarkPrice:     decimal.Zero,
		RealizedPnL:   decimal.Zero,
		UnrealizedPnL: decimal.Zero,
	}
}

func (p Position) Side() string {
	switch p.Quantity.Sign() {
	case 1:
		return PositionSideLong
	case -1:
		return PositionSideShort
	default:
		return PositionSideFlat
	}
}

func (p Position) IsOpen() bool {
	return !p.Quantity.IsZero()
}

func (p Position) Exposure() decimal.Decimal {
	price := p.MarkPrice
	if price.IsZero() {
		price = p.EntryPrice
	}
	if price.IsZero() {
		return decimal.Zero
	}
	return p.Quantity.Abs().Mul(price)
}

func (p *Position) ApplyFill(fill Fill) (ApplyResult, error) {
	if strings.TrimSpace(fill.Exchange) == "" || strings.TrimSpace(fill.Symbol) == "" {
		return ApplyResult{}, fmt.Errorf("exchange and symbol are required")
	}
	if fill.Quantity.LessThanOrEqual(decimal.Zero) {
		return ApplyResult{}, fmt.Errorf("quantity must be positive")
	}
	if fill.Price.LessThanOrEqual(decimal.Zero) {
		return ApplyResult{}, fmt.Errorf("price must be positive")
	}

	sideSign, err := sideToSign(fill.Side)
	if err != nil {
		return ApplyResult{}, err
	}

	if p.Exchange == "" {
		p.Exchange = fill.Exchange
	}
	if p.Symbol == "" {
		p.Symbol = fill.Symbol
	}
	if p.Exchange != fill.Exchange || p.Symbol != fill.Symbol {
		return ApplyResult{}, fmt.Errorf("fill %s/%s does not match position %s/%s", fill.Exchange, fill.Symbol, p.Exchange, p.Symbol)
	}

	oldQty := p.Quantity
	fq := fill.Quantity.Mul(decimal.NewFromInt(int64(sideSign)))
	newQty := oldQty.Add(fq)

	opened := oldQty.IsZero() && !newQty.IsZero()
	closed := !oldQty.IsZero() && newQty.IsZero()
	reversed := oldQty.Sign() != 0 && newQty.Sign() != 0 && oldQty.Sign() != newQty.Sign()

	realizedDelta := decimal.Zero
	if !oldQty.IsZero() && oldQty.Sign() != fq.Sign() {
		closedQty := minDecimal(oldQty.Abs(), fq.Abs())
		direction := decimal.NewFromInt(int64(oldQty.Sign()))
		realizedDelta = fill.Price.Sub(p.EntryPrice).Mul(closedQty).Mul(direction)
	}

	if !fill.Fee.IsZero() {
		realizedDelta = realizedDelta.Sub(fill.Fee)
	}
	p.RealizedPnL = p.RealizedPnL.Add(realizedDelta)

	if oldQty.IsZero() || oldQty.Sign() == fq.Sign() {
		denom := oldQty.Abs().Add(fq.Abs())
		if denom.IsZero() {
			p.EntryPrice = decimal.Zero
		} else {
			weighted := oldQty.Abs().Mul(p.EntryPrice).Add(fq.Abs().Mul(fill.Price))
			p.EntryPrice = weighted.Div(denom)
		}
	} else {
		switch {
		case newQty.IsZero():
			p.EntryPrice = decimal.Zero
		case oldQty.Abs().LessThan(fq.Abs()):
			p.EntryPrice = fill.Price
		}
	}

	p.Quantity = newQty
	p.MarkPrice = fill.Price
	p.LastUpdatedAt = fill.ExecutedAt
	p.RecalculateUnrealizedPnL()

	return ApplyResult{
		Opened:           opened,
		Closed:           closed,
		Reversed:         reversed,
		RealizedPnLDelta: realizedDelta,
		NewQuantity:      newQty,
	}, nil
}

func (p *Position) UpdateMarkPrice(price decimal.Decimal) error {
	if price.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("mark price must be positive")
	}
	p.MarkPrice = price
	p.RecalculateUnrealizedPnL()
	return nil
}

func (p *Position) RecalculateUnrealizedPnL() {
	if p.Quantity.IsZero() || p.EntryPrice.IsZero() || p.MarkPrice.IsZero() {
		p.UnrealizedPnL = decimal.Zero
		return
	}
	p.UnrealizedPnL = p.MarkPrice.Sub(p.EntryPrice).Mul(p.Quantity)
}

func sideToSign(side string) (int, error) {
	switch strings.ToLower(strings.TrimSpace(side)) {
	case SideBuy:
		return 1, nil
	case SideSell:
		return -1, nil
	default:
		return 0, fmt.Errorf("unsupported side: %s", side)
	}
}

func minDecimal(a, b decimal.Decimal) decimal.Decimal {
	if a.LessThan(b) {
		return a
	}
	return b
}
