// Package ports defines the application's port interfaces.
// These interfaces define what the application depends on, not how it's implemented.
// Adapters implement these interfaces for specific infrastructure.
package ports

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// ============================================================
// Market Data Gateway - Read-only market data access
// ============================================================

// Tick represents a single market tick.
type Tick struct {
	Exchange  string
	Symbol    string
	Bid       decimal.Decimal
	Ask       decimal.Decimal
	Last      decimal.Decimal
	Volume    decimal.Decimal
	Timestamp time.Time
}

// Candle represents an OHLCV candle.
type Candle struct {
	Exchange  string
	Symbol    string
	Timeframe string
	Open      decimal.Decimal
	High      decimal.Decimal
	Low       decimal.Decimal
	Close     decimal.Decimal
	Volume    decimal.Decimal
	Timestamp time.Time
}

// OrderBook represents a snapshot of the order book.
type OrderBook struct {
	Exchange  string
	Symbol    string
	Bids      []PriceLevel
	Asks      []PriceLevel
	Timestamp time.Time
}

// PriceLevel represents a price level in the order book.
type PriceLevel struct {
	Price  decimal.Decimal
	Amount decimal.Decimal
}

// FundingRate represents a funding rate for perpetual futures.
type FundingRate struct {
	Exchange        string
	Symbol          string
	Rate            decimal.Decimal
	NextFundingTime time.Time
	Timestamp       time.Time
}

// MarketDataGateway provides read-only access to market data.
type MarketDataGateway interface {
	// FetchTick fetches the current tick for a symbol.
	FetchTick(ctx context.Context, exchange, symbol string) (Tick, error)

	// FetchOHLCV fetches historical candles.
	FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, since time.Time, limit int) ([]Candle, error)

	// FetchOrderBook fetches the order book for a symbol.
	FetchOrderBook(ctx context.Context, exchange, symbol string, depth int) (OrderBook, error)

	// FetchFundingRate fetches the current funding rate for a symbol.
	FetchFundingRate(ctx context.Context, exchange, symbol string) (FundingRate, error)

	// FetchFundingRates fetches funding rates for multiple symbols.
	FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]FundingRate, error)

	// IsHealthy checks if the gateway is healthy.
	IsHealthy(ctx context.Context) bool
}

// ============================================================
// Trading Gateway - Order execution
// ============================================================

// OrderSide represents the side of an order.
type OrderSide string

const (
	OrderSideBuy  OrderSide = "buy"
	OrderSideSell OrderSide = "sell"
)

// OrderType represents the type of an order.
type OrderType string

const (
	OrderTypeMarket OrderType = "market"
	OrderTypeLimit  OrderType = "limit"
)

// OrderStatus represents the status of an order.
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusOpen      OrderStatus = "open"
	OrderStatusFilled    OrderStatus = "filled"
	OrderStatusPartial   OrderStatus = "partial"
	OrderStatusCancelled OrderStatus = "cancelled"
	OrderStatusRejected  OrderStatus = "rejected"
)

// OrderRequest represents a request to place an order.
type OrderRequest struct {
	Exchange   string
	Symbol     string
	Side       OrderSide
	Type       OrderType
	Amount     decimal.Decimal
	Price      decimal.Decimal // For limit orders
	ClientID   string          // Idempotency key
	StopPrice  decimal.Decimal // For stop orders
	TakeProfit decimal.Decimal // For TP/SL orders
	ReduceOnly bool
	PostOnly   bool
	Leverage   decimal.Decimal
	ChatID     string
}

// OrderResult represents the result of placing an order.
type OrderResult struct {
	Exchange     string
	OrderID      string
	ClientID     string
	Symbol       string
	Side         OrderSide
	Type         OrderType
	Amount       decimal.Decimal
	Filled       decimal.Decimal
	Price        decimal.Decimal
	AveragePrice decimal.Decimal
	Fee          decimal.Decimal
	Status       OrderStatus
	Timestamp    time.Time
}

// Order represents an order.
type Order struct {
	Exchange     string
	OrderID      string
	ClientID     string
	Symbol       string
	Side         OrderSide
	Type         OrderType
	Amount       decimal.Decimal
	Filled       decimal.Decimal
	Price        decimal.Decimal
	AveragePrice decimal.Decimal
	Status       OrderStatus
	Timestamp    time.Time
}

// Position represents a trading position.
type Position struct {
	Exchange         string
	Symbol           string
	Side             OrderSide
	Amount           decimal.Decimal
	EntryPrice       decimal.Decimal
	MarkPrice        decimal.Decimal
	UnrealizedPnL    decimal.Decimal
	Leverage         decimal.Decimal
	LiquidationPrice decimal.Decimal
}

// Balance represents an account balance.
type Balance struct {
	Exchange string
	Currency string
	Total    decimal.Decimal
	Free     decimal.Decimal
	Used     decimal.Decimal
}

// TradingGateway provides order execution capabilities.
type TradingGateway interface {
	// PlaceOrder places a new order.
	PlaceOrder(ctx context.Context, req OrderRequest) (OrderResult, error)

	// CancelOrder cancels an existing order.
	CancelOrder(ctx context.Context, exchange, orderID string) error

	// CancelAllOrders cancels all orders for a symbol.
	CancelAllOrders(ctx context.Context, exchange, symbol string) error

	// FetchOrder fetches an order by ID.
	FetchOrder(ctx context.Context, exchange, orderID string) (Order, error)

	// FetchOpenOrders fetches all open orders for a symbol.
	FetchOpenOrders(ctx context.Context, exchange, symbol string) ([]Order, error)

	// FetchPositions fetches all positions.
	FetchPositions(ctx context.Context, exchange string) ([]Position, error)

	// FetchBalances fetches account balances.
	FetchBalances(ctx context.Context, exchange string) ([]Balance, error)

	// IsHealthy checks if the gateway is healthy.
	IsHealthy(ctx context.Context) bool
}

// ExchangeGateway combines market data and trading capabilities.
type ExchangeGateway interface {
	MarketDataGateway
	TradingGateway
}

// ============================================================
// Exchange Registry
// ============================================================

// ExchangeInfo represents information about an exchange.
type ExchangeInfo struct {
	ID         string
	Name       string
	Enabled    bool
	Symbols    []string
	HasSpot    bool
	HasFutures bool
	HasMargin  bool
}

// ExchangeRegistry provides access to configured exchanges.
type ExchangeRegistry interface {
	// GetGateway returns the gateway for an exchange.
	GetGateway(exchange string) (ExchangeGateway, error)

	// GetMarketDataGateway returns the market data gateway for an exchange.
	GetMarketDataGateway(exchange string) (MarketDataGateway, error)

	// GetTradingGateway returns the trading gateway for an exchange.
	GetTradingGateway(exchange string) (TradingGateway, error)

	// ListExchanges returns all configured exchanges.
	ListExchanges(ctx context.Context) ([]ExchangeInfo, error)

	// IsExchangeEnabled checks if an exchange is enabled.
	IsExchangeEnabled(exchange string) bool
}
