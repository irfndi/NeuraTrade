package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/shopspring/decimal"
)

type scalpingTradingGateway struct {
	executor    services.ScalpingOrderExecutor
	orderLookup liveOrderLookup
}

func (g *scalpingTradingGateway) PlaceOrder(ctx context.Context, request ports.OrderRequest) (ports.OrderResult, error) {
	notional := request.Amount.Mul(request.Price).Abs()
	details := services.TradeDetails{
		Exchange:      request.Exchange,
		Symbol:        request.Symbol,
		Side:          string(request.Side),
		OrderType:     string(request.Type),
		MarketType:    "futures",
		Leverage:      int(request.Leverage.IntPart()),
		Amount:        request.Amount,
		AmountUSDT:    notional,
		EntryPrice:    &request.Price,
		TakeProfit:    decimalPointer(request.TakeProfit),
		StopLoss:      decimalPointer(request.StopPrice),
		ReduceOnly:    request.ReduceOnly,
		IntentID:      request.ClientID,
		ClientOrderID: request.ClientID,
		TradeType:     "neuratrade-cli-ts",
	}
	executionCtx := services.WithScalpingAutonomyScope(ctx, services.ScalpingAutonomyScope{
		ChatID:     request.ChatID,
		Exchange:   request.Exchange,
		MarketType: "futures",
		Leverage:   int(request.Leverage.IntPart()),
	})
	executionCtx = services.WithOperationalMode(executionCtx, services.OpModeLive)
	orderID, err := g.executor.PlaceOrderWithDetails(executionCtx, details)
	if err != nil {
		return ports.OrderResult{}, err
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	observed, err := g.orderLookup.FetchOrder(lookupCtx, liveLookupExchange(request.Exchange), orderID, request.Symbol)
	if err != nil || observed == nil {
		return ports.OrderResult{
			Exchange:  request.Exchange,
			OrderID:   orderID,
			ClientID:  request.ClientID,
			Symbol:    request.Symbol,
			Side:      request.Side,
			Type:      request.Type,
			Amount:    request.Amount,
			Price:     request.Price,
			Status:    ports.OrderStatusOpen,
			Timestamp: time.Now().UTC(),
		}, nil
	}
	return orderResultFromObservedOrder(request.Exchange, request.Symbol, request.Side, request.Type, observed.Order), nil
}

func liveLookupExchange(exchange string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(exchange)), "bitget") {
		return "bitget"
	}
	return strings.TrimSpace(exchange)
}

func orderResultFromObservedOrder(exchange, symbol string, side ports.OrderSide, orderType ports.OrderType, observed ccxt.Order) ports.OrderResult {
	filled := observed.Filled
	averagePrice := decimal.Zero
	if filled.IsPositive() && observed.Cost.IsPositive() {
		averagePrice = observed.Cost.Div(filled)
	}
	status := observedOrderStatus(observed.Status, filled, observed.Amount)
	if observed.Symbol == "" {
		observed.Symbol = symbol
	}
	if observed.Side == "" {
		observed.Side = string(side)
	}
	if observed.Type == "" {
		observed.Type = string(orderType)
	}
	if observed.CreatedAt.IsZero() {
		observed.CreatedAt = time.Now().UTC()
	}
	return ports.OrderResult{
		Exchange:     exchange,
		OrderID:      observed.ID,
		ClientID:     observed.ClientOrderID,
		Symbol:       observed.Symbol,
		Side:         ports.OrderSide(observed.Side),
		Type:         ports.OrderType(observed.Type),
		Amount:       observed.Amount,
		Filled:       filled,
		Price:        observed.Price,
		AveragePrice: averagePrice,
		Fee:          observed.Fee,
		Status:       status,
		Timestamp:    observed.CreatedAt,
	}
}

func observedOrderStatus(raw string, filled, amount decimal.Decimal) ports.OrderStatus {
	status := strings.ToLower(strings.TrimSpace(raw))
	if status == "closed" || status == "filled" {
		if filled.GreaterThanOrEqual(amount) && filled.IsPositive() {
			return ports.OrderStatusFilled
		}
		if filled.IsPositive() {
			return ports.OrderStatusPartial
		}
		return ports.OrderStatusOpen
	}
	switch status {
	case "cancelled", "canceled":
		return ports.OrderStatusCancelled
	case "rejected":
		return ports.OrderStatusRejected
	case "partial", "partially_filled", "partial_fill":
		return ports.OrderStatusPartial
	case "pending", "new":
		return ports.OrderStatusPending
	default:
		return ports.OrderStatusOpen
	}
}

func (g *scalpingTradingGateway) CancelOrder(context.Context, string, string) error {
	return errors.New("live actor gateway does not support cancellation")
}

func (g *scalpingTradingGateway) CancelAllOrders(context.Context, string, string) error {
	return errors.New("live actor gateway does not support cancellation")
}

func (g *scalpingTradingGateway) FetchOrder(context.Context, string, string) (ports.Order, error) {
	return ports.Order{}, errors.New("live actor gateway does not support order lookup")
}

func (g *scalpingTradingGateway) FetchOpenOrders(context.Context, string, string) ([]ports.Order, error) {
	return nil, errors.New("live actor gateway does not support order lookup")
}

func (g *scalpingTradingGateway) FetchPositions(context.Context, string) ([]ports.Position, error) {
	return nil, errors.New("live actor gateway does not support position lookup")
}

func (g *scalpingTradingGateway) FetchBalances(context.Context, string) ([]ports.Balance, error) {
	return nil, errors.New("live actor gateway does not support balance lookup")
}

func (g *scalpingTradingGateway) IsHealthy(context.Context) bool {
	return g.executor != nil && !g.executor.IsPaperTrading()
}

func decimalPointer(value decimal.Decimal) *decimal.Decimal {
	if value.IsZero() {
		return nil
	}
	return &value
}
