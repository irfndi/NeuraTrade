package interfaces

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// OrderExecutionType defines the type of order execution.
type OrderExecutionType string

const (
	// OrderExecutionMarket executes a market order (immediate execution at best available price)
	OrderExecutionMarket OrderExecutionType = "MARKET"
	// OrderExecutionLimit executes a limit order (execute only at specified price or better)
	OrderExecutionLimit OrderExecutionType = "LIMIT"
	// OrderExecutionFOK executes a Fill-Or-Kill order (must fill completely immediately or cancel)
	OrderExecutionFOK OrderExecutionType = "FOK"
	// OrderExecutionGTC executes a Good-Till-Cancel order (remains active until filled or cancelled)
	OrderExecutionGTC OrderExecutionType = "GTC"
	// OrderExecutionGTD executes a Good-Till-Date order (remains active until filled, cancelled, or expiration)
	OrderExecutionGTD OrderExecutionType = "GTD"
	// OrderExecutionFAK executes a Fill-And-Kill order (partial fill allowed, remaining cancelled)
	OrderExecutionFAK OrderExecutionType = "FAK"
)

// OrderSide defines the direction of the order.
type OrderSide string

const (
	// OrderSideBuy represents a buy order
	OrderSideBuy OrderSide = "BUY"
	// OrderSideSell represents a sell order
	OrderSideSell OrderSide = "SELL"
)

// OrderStatus represents the current state of an order.
type OrderStatus string

const (
	// OrderStatusPending order is pending execution
	OrderStatusPending OrderStatus = "PENDING"
	// OrderStatusOpen order is open and waiting for fill
	OrderStatusOpen OrderStatus = "OPEN"
	// OrderStatusFilled order has been completely filled
	OrderStatusFilled OrderStatus = "FILLED"
	// OrderStatusPartiallyFilled order has been partially filled
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	// OrderStatusCancelled order has been cancelled
	OrderStatusCancelled OrderStatus = "CANCELLED"
	// OrderStatusRejected order was rejected by the exchange
	OrderStatusRejected OrderStatus = "REJECTED"
	// OrderStatusExpired order has expired
	OrderStatusExpired OrderStatus = "EXPIRED"
)

// OrderExecutionRequest represents a request to execute an order.
type OrderExecutionRequest struct {
	// TokenID is the Polymarket token ID for the market
	TokenID string `json:"tokenId"`
	// Side is the order side (BUY or SELL)
	Side OrderSide `json:"side"`
	// Size is the amount to trade
	Size decimal.Decimal `json:"size"`
	// Price is the limit price (required for LIMIT orders)
	Price decimal.Decimal `json:"price,omitempty"`
	// MaxPrice is the maximum acceptable price for market orders
	MaxPrice decimal.Decimal `json:"maxPrice,omitempty"`
	// OrderType is the type of order execution
	OrderType OrderExecutionType `json:"orderType"`
	// PostOnly indicates if the order should only place as maker
	PostOnly bool `json:"postOnly,omitempty"`
	// Expiration is the Unix timestamp when the order expires (for GTD orders)
	Expiration int64 `json:"expiration,omitempty"`
}

// OrderExecutionResult represents the result of an order execution.
type OrderExecutionResult struct {
	// OrderID is the unique identifier of the placed order
	OrderID string `json:"orderId"`
	// Status is the current status of the order
	Status OrderStatus `json:"status"`
	// Size is the total size of the order
	Size decimal.Decimal `json:"size"`
	// FilledSize is the amount that has been filled
	FilledSize decimal.Decimal `json:"filledSize"`
	// RemainingSize is the amount remaining to be filled
	RemainingSize decimal.Decimal `json:"remainingSize"`
	// Price is the execution price
	Price decimal.Decimal `json:"price"`
	// CreatedAt is when the order was created
	CreatedAt time.Time `json:"createdAt"`
	// TokenID is the token this order is for
	TokenID string `json:"tokenId"`
	// Side is the order side
	Side OrderSide `json:"side"`
}

// OrderBookEntry represents a single entry in an order book.
type OrderBookEntry struct {
	Price decimal.Decimal `json:"price"`
	Size  decimal.Decimal `json:"size"`
}

// OrderBook represents the current order book for a token.
type OrderBook struct {
	// TokenID is the token this order book is for
	TokenID string `json:"tokenId"`
	// Bids are the buy orders (price, size pairs)
	Bids []OrderBookEntry `json:"bids"`
	// Asks are the sell orders (price, size pairs)
	Asks []OrderBookEntry `json:"asks"`
	// Timestamp is when the order book was captured
	Timestamp time.Time `json:"timestamp"`
}

// OrderExecutionInterface defines the contract for order execution services.
// Any implementation can be used for placing orders on exchanges (Polymarket, etc.).
type OrderExecutionInterface interface {
	// PlaceOrder places an order on the exchange and returns the result.
	//
	// Parameters:
	//   - ctx: Context for the request
	//   - req: The order execution request
	//
	// Returns:
	//   - *OrderExecutionResult: The result of the order placement
	//   - error: Any error that occurred
	PlaceOrder(ctx context.Context, req OrderExecutionRequest) (*OrderExecutionResult, error)

	// GetOrder retrieves an order by its ID.
	//
	// Parameters:
	//   - ctx: Context for the request
	//   - orderID: The order ID to retrieve
	//
	// Returns:
	//   - *OrderExecutionResult: The order details
	//   - error: Any error that occurred
	GetOrder(ctx context.Context, orderID string) (*OrderExecutionResult, error)

	// CancelOrder cancels an existing order.
	//
	// Parameters:
	//   - ctx: Context for the request
	//   - orderID: The order ID to cancel
	//
	// Returns:
	//   - error: Any error that occurred
	CancelOrder(ctx context.Context, orderID string) error

	// GetOpenOrders retrieves all open orders.
	//
	// Parameters:
	//   - ctx: Context for the request
	//
	// Returns:
	//   - []*OrderExecutionResult: List of open orders
	//   - error: Any error that occurred
	GetOpenOrders(ctx context.Context) ([]*OrderExecutionResult, error)

	// GetOrderBook retrieves the current order book for a token.
	//
	// Parameters:
	//   - ctx: Context for the request
	//   - tokenID: The token ID to get the order book for
	//
	// Returns:
	//   - *OrderBook: The current order book
	//   - error: Any error that occurred
	GetOrderBook(ctx context.Context, tokenID string) (*OrderBook, error)

	// GetBestPrice retrieves the best available price for a token and side.
	//
	// Parameters:
	//   - ctx: Context for the request
	//   - tokenID: The token ID
	//   - side: The order side (BUY or SELL)
	//
	// Returns:
	//   - decimal.Decimal: The best price
	//   - error: Any error that occurred
	GetBestPrice(ctx context.Context, tokenID string, side OrderSide) (decimal.Decimal, error)
}
