package trading

import (
	"fmt"
	"time"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

// OrderSide is a simple enumeration representing the side of an Order (buy or sell)
type OrderSide int

// BUY and SELL enumerations
const (
	BUY OrderSide = iota
	SELL
)

// OrderType is an enumeration of the types of orders that can be placed
type OrderType int

const (
	MarketOrder OrderType = iota
	LimitOrder
	StopOrder
	StopLimitOrder
	TrailingStopOrder
)

// OrderStatus represents the current state of an order
type OrderStatus int

const (
	OrderStatusNew OrderStatus = iota
	OrderStatusPending
	OrderStatusFilled
	OrderStatusCancelled
	OrderStatusRejected
)

// Order represents a trade execution or request (buy or sell) with associated metadata.
type Order struct {
	ID            string
	Side          OrderSide
	Type          OrderType
	Status        OrderStatus
	Security      string
	Price         decimal.Decimal // Limit price for limit orders
	StopPrice     decimal.Decimal // Stop price for stop orders
	Amount        decimal.Decimal
	FilledPrice   decimal.Decimal
	FilledAmount  decimal.Decimal
	TrailingPct   decimal.Decimal
	ExecutionTime time.Time
	CreationTime  time.Time
}

var orderCounter int64

func NewOrderDetail(side OrderSide, ordType OrderType, security string, amount decimal.Decimal) *Order {
	orderCounter++
	return &Order{
		ID:           fmt.Sprintf("%d-%d", time.Now().UnixNano(), orderCounter),
		Side:         side,
		Type:         ordType,
		Security:     security,
		Amount:       amount,
		Status:       OrderStatusNew,
		CreationTime: time.Now(),
	}
}

func (o *Order) SetLimitPrice(p decimal.Decimal) { o.Price = p }
func (o *Order) SetStopPrice(p decimal.Decimal)  { o.StopPrice = p }
func (o *Order) SetTrailingStop(pct decimal.Decimal) {
	o.Type = TrailingStopOrder
	o.TrailingPct = pct
}
func (o *Order) IsBuy() bool { return o.Side == BUY }
func (o *Order) Fill(price, amount decimal.Decimal) {
	o.Status = OrderStatusFilled
	o.FilledPrice = price
	o.FilledAmount = amount
	o.ExecutionTime = time.Now()
}
func (o *Order) Cancel() { o.Status = OrderStatusCancelled }

// OrderBook manage orders
type OrderBook struct {
	orders map[string]*Order
}

func NewOrderBook() *OrderBook {
	return &OrderBook{orders: make(map[string]*Order)}
}

func (ob *OrderBook) Add(o *Order)     { ob.orders[o.ID] = o }
func (ob *OrderBook) Remove(id string) { delete(ob.orders, id) }
func (ob *OrderBook) Get(id string) (*Order, bool) {
	o, ok := ob.orders[id]
	return o, ok
}
func (ob *OrderBook) GetBySecurity(security string) []*Order {
	var res []*Order
	for _, o := range ob.orders {
		if o.Security == security {
			res = append(res, o)
		}
	}
	return res
}
func (ob *OrderBook) GetPending() []*Order {
	var res []*Order
	for _, o := range ob.orders {
		if o.Status == OrderStatusNew || o.Status == OrderStatusPending {
			res = append(res, o)
		}
	}
	return res
}

// BracketOrder wraps multi orders
type BracketOrder struct {
	Parent     *Order
	TakeProfit *Order
	StopLoss   *Order
}

func NewBracketOrder(parent *Order, tpPrice, slPrice decimal.Decimal) *BracketOrder {
	tp := NewOrderDetail(SELL, LimitOrder, parent.Security, parent.Amount)
	if parent.Side == SELL {
		tp.Side = BUY
	}
	tp.Price = tpPrice

	sl := NewOrderDetail(SELL, StopOrder, parent.Security, parent.Amount)
	if parent.Side == SELL {
		sl.Side = BUY
	}
	sl.StopPrice = slPrice

	return &BracketOrder{
		Parent:     parent,
		TakeProfit: tp,
		StopLoss:   sl,
	}
}

func (bo *BracketOrder) GetAllOrders() []*Order {
	return []*Order{bo.Parent, bo.TakeProfit, bo.StopLoss}
}

type OrderManager struct {
	orderBook *OrderBook
}

func NewOrderManager() *OrderManager {
	return &OrderManager{orderBook: NewOrderBook()}
}

func (om *OrderManager) Submit(o *Order) {
	o.Status = OrderStatusPending
	om.orderBook.Add(o)
}

func (om *OrderManager) SubmitBracket(b *BracketOrder) {
	om.Submit(b.Parent)
	om.Submit(b.TakeProfit)
	om.Submit(b.StopLoss)
}

func (om *OrderManager) ProcessMarketOrder(o *Order, price decimal.Decimal) {
	o.Fill(price, o.Amount)
}

func (om *OrderManager) ProcessLimitOrder(o *Order, price decimal.Decimal) {
	if (o.Side == BUY && price.LTE(o.Price)) || (o.Side == SELL && price.GTE(o.Price)) {
		o.Fill(price, o.Amount)
	}
}

func (om *OrderManager) ProcessStopOrder(o *Order, price decimal.Decimal) {
	if (o.Side == BUY && price.GTE(o.StopPrice)) || (o.Side == SELL && price.LTE(o.StopPrice)) {
		o.Fill(price, o.Amount)
	}
}

func (om *OrderManager) ProcessTrailingStop(o *Order, price decimal.Decimal) {
	// Simple implementation
	o.Fill(price, o.Amount)
}

func (om *OrderManager) CancelPending() {
	for _, o := range om.orderBook.GetPending() {
		o.Cancel()
	}
}

type PriceSource int

const (
	ClosePrice PriceSource = iota
	TypicalPrice
	MedianPrice
)

func GetPriceFromCandle(c *series.Candle, source PriceSource) decimal.Decimal {
	switch source {
	case ClosePrice:
		return c.ClosePrice
	case TypicalPrice:
		return c.MaxPrice.Add(c.MinPrice).Add(c.ClosePrice).Div(decimal.New(3))
	case MedianPrice:
		return c.MaxPrice.Add(c.MinPrice).Div(decimal.New(2))
	default:
		return c.ClosePrice
	}
}
