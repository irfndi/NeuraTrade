package trading

import (
	"testing"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestOrderDetailCreation(t *testing.T) {
	order := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))

	if order.ID == "" {
		t.Error("Order should have an ID")
	}
	if order.Side != BUY {
		t.Errorf("Expected side BUY, got %v", order.Side)
	}
	if order.Type != MarketOrder {
		t.Errorf("Expected type MarketOrder, got %v", order.Type)
	}
	if order.Status != OrderStatusNew {
		t.Errorf("Expected status OrderStatusNew, got %v", order.Status)
	}
	if order.Security != "AAPL" {
		t.Errorf("Expected security AAPL, got %s", order.Security)
	}
	if !order.Amount.EQ(decimal.New(100)) {
		t.Errorf("Expected amount 100, got %v", order.Amount)
	}
}

func TestOrderDetailLimitPrice(t *testing.T) {
	order := NewOrderDetail(SELL, LimitOrder, "AAPL", decimal.New(50))
	order.SetLimitPrice(decimal.New(150))

	if !order.Price.EQ(decimal.New(150)) {
		t.Errorf("Expected price 150, got %v", order.Price)
	}
}

func TestOrderDetailStopPrice(t *testing.T) {
	order := NewOrderDetail(BUY, StopOrder, "AAPL", decimal.New(100))
	order.SetStopPrice(decimal.New(145))

	if !order.StopPrice.EQ(decimal.New(145)) {
		t.Errorf("Expected stop price 145, got %v", order.StopPrice)
	}
}

func TestOrderDetailTrailingStop(t *testing.T) {
	order := NewOrderDetail(SELL, MarketOrder, "AAPL", decimal.New(100))
	order.SetTrailingStop(decimal.New(0.02))

	if order.Type != TrailingStopOrder {
		t.Errorf("Expected type TrailingStopOrder, got %v", order.Type)
	}
	if !order.TrailingPct.EQ(decimal.New(0.02)) {
		t.Errorf("Expected trailing pct 0.02, got %v", order.TrailingPct)
	}
}

func TestOrderDetailIsBuy(t *testing.T) {
	buyOrder := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))
	sellOrder := NewOrderDetail(SELL, MarketOrder, "AAPL", decimal.New(100))

	if !buyOrder.IsBuy() {
		t.Error("Buy order should return true for IsBuy()")
	}
	if sellOrder.IsBuy() {
		t.Error("Sell order should return false for IsBuy()")
	}
}

func TestOrderDetailFill(t *testing.T) {
	order := NewOrderDetail(BUY, LimitOrder, "AAPL", decimal.New(100))
	order.Fill(decimal.New(150), decimal.New(100))

	if order.Status != OrderStatusFilled {
		t.Errorf("Expected status OrderStatusFilled, got %v", order.Status)
	}
	if !order.FilledPrice.EQ(decimal.New(150)) {
		t.Errorf("Expected filled price 150, got %v", order.FilledPrice)
	}
	if !order.FilledAmount.EQ(decimal.New(100)) {
		t.Errorf("Expected filled amount 100, got %v", order.FilledAmount)
	}
}

func TestOrderDetailCancel(t *testing.T) {
	order := NewOrderDetail(BUY, LimitOrder, "AAPL", decimal.New(100))
	order.Cancel()

	if order.Status != OrderStatusCancelled {
		t.Errorf("Expected status OrderStatusCancelled, got %v", order.Status)
	}
}

func TestOrderBookAdd(t *testing.T) {
	book := NewOrderBook()
	order := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))

	book.Add(order)

	_, exists := book.Get(order.ID)
	if !exists {
		t.Error("Order should exist in book")
	}
}

func TestOrderBookRemove(t *testing.T) {
	book := NewOrderBook()
	order := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))

	book.Add(order)
	book.Remove(order.ID)

	_, exists := book.Get(order.ID)
	if exists {
		t.Error("Order should not exist in book after removal")
	}
}

func TestOrderBookGetBySecurity(t *testing.T) {
	book := NewOrderBook()

	book.Add(NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100)))
	book.Add(NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(50)))
	book.Add(NewOrderDetail(SELL, MarketOrder, "GOOGL", decimal.New(75)))

	aaplOrders := book.GetBySecurity("AAPL")
	if len(aaplOrders) != 2 {
		t.Errorf("Expected 2 AAPL orders, got %d", len(aaplOrders))
	}

	googlOrders := book.GetBySecurity("GOOGL")
	if len(googlOrders) != 1 {
		t.Errorf("Expected 1 GOOGL order, got %d", len(googlOrders))
	}
}

func TestOrderBookGetPending(t *testing.T) {
	book := NewOrderBook()

	order1 := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))
	order2 := NewOrderDetail(SELL, MarketOrder, "AAPL", decimal.New(50))
	order3 := NewOrderDetail(BUY, LimitOrder, "GOOGL", decimal.New(75))

	book.Add(order1)
	book.Add(order2)
	book.Add(order3)

	order1.Fill(decimal.New(150), decimal.New(100))

	pending := book.GetPending()
	if len(pending) != 2 {
		t.Errorf("Expected 2 pending orders, got %d", len(pending))
	}
}

func TestBracketOrderCreation(t *testing.T) {
	parent := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))
	bracket := NewBracketOrder(parent, decimal.New(160), decimal.New(140))

	if bracket.Parent != parent {
		t.Error("Bracket should have parent order")
	}
	if bracket.TakeProfit == nil {
		t.Error("Bracket should have take-profit order")
	}
	if bracket.StopLoss == nil {
		t.Error("Bracket should have stop-loss order")
	}
	if !bracket.TakeProfit.Price.EQ(decimal.New(160)) {
		t.Errorf("Expected take-profit price 160, got %v", bracket.TakeProfit.Price)
	}
	if !bracket.StopLoss.StopPrice.EQ(decimal.New(140)) {
		t.Errorf("Expected stop-loss price 140, got %v", bracket.StopLoss.StopPrice)
	}
}

func TestBracketOrderGetAllOrders(t *testing.T) {
	parent := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))
	bracket := NewBracketOrder(parent, decimal.New(160), decimal.New(140))

	orders := bracket.GetAllOrders()
	if len(orders) != 3 {
		t.Errorf("Expected 3 orders in bracket, got %d", len(orders))
	}
}

func TestOrderManagerSubmit(t *testing.T) {
	manager := NewOrderManager()
	order := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))

	manager.Submit(order)

	if order.Status != OrderStatusPending {
		t.Errorf("Expected status OrderStatusPending, got %v", order.Status)
	}

	_, exists := manager.orderBook.Get(order.ID)
	if !exists {
		t.Error("Order should exist in order book")
	}
}

func TestOrderManagerSubmitBracket(t *testing.T) {
	manager := NewOrderManager()
	parent := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))
	bracket := NewBracketOrder(parent, decimal.New(160), decimal.New(140))

	manager.SubmitBracket(bracket)

	if parent.Status != OrderStatusPending {
		t.Errorf("Expected parent status OrderStatusPending, got %v", parent.Status)
	}
	if bracket.TakeProfit.Status != OrderStatusPending {
		t.Errorf("Expected take-profit status OrderStatusPending, got %v", bracket.TakeProfit.Status)
	}
	if bracket.StopLoss.Status != OrderStatusPending {
		t.Errorf("Expected stop-loss status OrderStatusPending, got %v", bracket.StopLoss.Status)
	}
}

func TestOrderManagerProcessMarketOrder(t *testing.T) {
	manager := NewOrderManager()
	order := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))
	manager.Submit(order)

	manager.ProcessMarketOrder(order, decimal.New(150))

	if order.Status != OrderStatusFilled {
		t.Errorf("Expected status OrderStatusFilled, got %v", order.Status)
	}
	if !order.FilledPrice.EQ(decimal.New(150)) {
		t.Errorf("Expected filled price 150, got %v", order.FilledPrice)
	}
}

func TestOrderManagerProcessLimitOrderBuy(t *testing.T) {
	manager := NewOrderManager()
	order := NewOrderDetail(BUY, LimitOrder, "AAPL", decimal.New(100))
	order.SetLimitPrice(decimal.New(145))
	manager.Submit(order)

	manager.ProcessLimitOrder(order, decimal.New(144))

	if order.Status != OrderStatusFilled {
		t.Errorf("Expected status OrderStatusFilled, got %v", order.Status)
	}
}

func TestOrderManagerProcessLimitOrderNotFilled(t *testing.T) {
	manager := NewOrderManager()
	order := NewOrderDetail(BUY, LimitOrder, "AAPL", decimal.New(100))
	order.SetLimitPrice(decimal.New(145))
	manager.Submit(order)

	manager.ProcessLimitOrder(order, decimal.New(146))

	if order.Status == OrderStatusFilled {
		t.Error("Order should not be filled when price is above limit")
	}
}

func TestOrderManagerProcessLimitOrderSell(t *testing.T) {
	manager := NewOrderManager()
	order := NewOrderDetail(SELL, LimitOrder, "AAPL", decimal.New(100))
	order.SetLimitPrice(decimal.New(155))
	manager.Submit(order)

	manager.ProcessLimitOrder(order, decimal.New(156))

	if order.Status != OrderStatusFilled {
		t.Errorf("Expected status OrderStatusFilled, got %v", order.Status)
	}
}

func TestOrderManagerProcessStopOrder(t *testing.T) {
	manager := NewOrderManager()
	order := NewOrderDetail(SELL, StopOrder, "AAPL", decimal.New(100))
	order.SetStopPrice(decimal.New(140))
	manager.Submit(order)

	manager.ProcessStopOrder(order, decimal.New(139))

	if order.Status != OrderStatusFilled {
		t.Errorf("Expected status OrderStatusFilled, got %v", order.Status)
	}
}

func TestOrderManagerProcessTrailingStop(t *testing.T) {
	manager := NewOrderManager()
	order := NewOrderDetail(SELL, TrailingStopOrder, "AAPL", decimal.New(100))
	order.SetTrailingStop(decimal.New(0.05))
	manager.Submit(order)

	manager.ProcessTrailingStop(order, decimal.New(200))
	manager.ProcessTrailingStop(order, decimal.New(195))

	if order.Status != OrderStatusFilled {
		t.Errorf("Expected status OrderStatusFilled, got %v", order.Status)
	}
}

func TestOrderManagerCancelPending(t *testing.T) {
	manager := NewOrderManager()
	order1 := NewOrderDetail(BUY, MarketOrder, "AAPL", decimal.New(100))
	order2 := NewOrderDetail(SELL, LimitOrder, "GOOGL", decimal.New(50))
	manager.Submit(order1)
	manager.Submit(order2)

	manager.CancelPending()

	if order1.Status != OrderStatusCancelled {
		t.Errorf("Expected status OrderStatusCancelled, got %v", order1.Status)
	}
	if order2.Status != OrderStatusCancelled {
		t.Errorf("Expected status OrderStatusCancelled, got %v", order2.Status)
	}
}

func TestGetPriceFromCandle(t *testing.T) {
	candle := &series.Candle{
		OpenPrice:  decimal.New(100),
		MaxPrice:   decimal.New(110),
		MinPrice:   decimal.New(90),
		ClosePrice: decimal.New(105),
		Volume:     decimal.New(1000),
	}

	closePrice := GetPriceFromCandle(candle, ClosePrice)
	if !closePrice.EQ(decimal.New(105)) {
		t.Errorf("Expected close price 105, got %v", closePrice)
	}

	typicalPrice := GetPriceFromCandle(candle, TypicalPrice)
	expectedTypical := decimal.New(110).Add(decimal.New(90)).Add(decimal.New(105)).Div(decimal.New(3))
	if !typicalPrice.EQ(expectedTypical) {
		t.Errorf("Expected typical price %v, got %v", expectedTypical, typicalPrice)
	}

	medianPrice := GetPriceFromCandle(candle, MedianPrice)
	expectedMedian := decimal.New(110).Add(decimal.New(90)).Div(decimal.New(2))
	if !medianPrice.EQ(expectedMedian) {
		t.Errorf("Expected median price %v, got %v", expectedMedian, medianPrice)
	}
}
