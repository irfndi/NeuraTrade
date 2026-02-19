package trading

import (
	"github.com/irfndi/goflux/pkg/decimal"
)

type Position struct {
	entryOrder *Order
	exitOrder  *Order
}

func NewPosition(entryOrder Order) *Position {
	return &Position{entryOrder: &entryOrder}
}

func (p *Position) IsNew() bool {
	return p.entryOrder == nil && p.exitOrder == nil
}

func (p *Position) IsOpen() bool {
	return p.entryOrder != nil && p.exitOrder == nil
}

func (p *Position) IsClosed() bool {
	return p.entryOrder != nil && p.exitOrder != nil
}

func (p *Position) EntranceOrder() *Order {
	return p.entryOrder
}

func (p *Position) ExitOrder() *Order {
	return p.exitOrder
}

func (p *Position) Enter(order Order) {
	p.entryOrder = &order
}

func (p *Position) Exit(order Order) {
	p.exitOrder = &order
}

func (p *Position) CostBasis() decimal.Decimal {
	if p.entryOrder == nil {
		return decimal.ZERO
	}
	return p.entryOrder.Amount.Mul(p.entryOrder.Price)
}

func (p *Position) ExitValue() decimal.Decimal {
	if p.exitOrder == nil {
		return decimal.ZERO
	}
	return p.exitOrder.Amount.Mul(p.exitOrder.Price)
}

func (p *Position) IsLong() bool {
	return p.entryOrder != nil && p.entryOrder.Side == BUY
}

func (p *Position) IsShort() bool {
	return p.entryOrder != nil && p.entryOrder.Side == SELL
}
