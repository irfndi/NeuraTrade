package handlers

import "context"

type OrderCanceller interface {
	CancelAllOrders(ctx context.Context, exchange, symbol string) error
}
