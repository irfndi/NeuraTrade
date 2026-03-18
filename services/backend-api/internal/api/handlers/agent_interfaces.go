package handlers

import "context"

type SafeModeController interface {
	Enable(ctx context.Context) error
	Disable(ctx context.Context) error
	IsEnabled() bool
}

type KillSwitchController interface {
	Engage(ctx context.Context, reason string) error
	Disengage(ctx context.Context) error
	IsEngaged() bool
}

type OrderCanceller interface {
	CancelAllOrders(ctx context.Context, exchange, symbol string) error
}

type ExchangeController interface {
	PauseExchange(exchangeID string) error
	ResumeExchange(exchangeID string) error
	IsPaused(exchangeID string) bool
}
