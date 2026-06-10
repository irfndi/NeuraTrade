// Package ccxt provides an adapter that wraps the existing CCXT service
// to implement the ports.ExchangeGateway interface.
package ccxt

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// Adapter wraps the existing CCXT service to implement ExchangeGateway.
type Adapter struct {
	service ccxt.CCXTService
}

// NewAdapter creates a new CCXT adapter.
func NewAdapter(service ccxt.CCXTService) *Adapter {
	return &Adapter{service: service}
}

// ============================================================
// MarketDataGateway Implementation
// ============================================================

// FetchTick fetches the current tick for a symbol.
func (a *Adapter) FetchTick(ctx context.Context, exchange, symbol string) (ports.Tick, error) {
	ticker, err := a.service.FetchSingleTicker(ctx, exchange, symbol)
	if err != nil {
		return ports.Tick{}, err
	}

	return ports.Tick{
		Exchange:  ticker.GetExchangeName(),
		Symbol:    ticker.GetSymbol(),
		Bid:       ticker.GetBid(),
		Ask:       ticker.GetAsk(),
		Last:      ticker.GetPrice(),
		Volume:    ticker.GetVolume(),
		Timestamp: ticker.GetTimestamp(),
	}, nil
}

func (a *Adapter) FetchOHLCV(ctx context.Context, exchange, symbol, timeframe string, since time.Time, limit int) ([]ports.Candle, error) {
	resp, err := a.service.FetchOHLCV(ctx, exchange, symbol, timeframe, limit)
	if err != nil {
		return nil, err
	}

	candles := make([]ports.Candle, 0, len(resp.OHLCV))
	for _, ohlcv := range resp.OHLCV {
		candles = append(candles, ports.Candle{
			Exchange:  exchange,
			Symbol:    symbol,
			Timeframe: timeframe,
			Open:      ohlcv.Open,
			High:      ohlcv.High,
			Low:       ohlcv.Low,
			Close:     ohlcv.Close,
			Volume:    ohlcv.Volume,
			Timestamp: ohlcv.Timestamp,
		})
	}

	return candles, nil
}

func (a *Adapter) FetchOrderBook(ctx context.Context, exchange, symbol string, depth int) (ports.OrderBook, error) {
	resp, err := a.service.FetchOrderBook(ctx, exchange, symbol, depth)
	if err != nil {
		return ports.OrderBook{}, err
	}

	bids := make([]ports.PriceLevel, 0, len(resp.OrderBook.Bids))
	for _, bid := range resp.OrderBook.Bids {
		bids = append(bids, ports.PriceLevel{
			Price:  bid.Price,
			Amount: bid.Amount,
		})
	}

	asks := make([]ports.PriceLevel, 0, len(resp.OrderBook.Asks))
	for _, ask := range resp.OrderBook.Asks {
		asks = append(asks, ports.PriceLevel{
			Price:  ask.Price,
			Amount: ask.Amount,
		})
	}

	return ports.OrderBook{
		Exchange:  exchange,
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: resp.OrderBook.Timestamp,
	}, nil
}

func (a *Adapter) FetchFundingRate(ctx context.Context, exchange, symbol string) (ports.FundingRate, error) {
	rate, err := a.service.FetchFundingRate(ctx, exchange, symbol)
	if err != nil {
		return ports.FundingRate{}, err
	}

	return ports.FundingRate{
		Exchange:        exchange,
		Symbol:          symbol,
		Rate:            decimal.NewFromFloat(rate.FundingRate),
		NextFundingTime: time.Time(rate.NextFundingTime),
		Timestamp:       time.Time(rate.Timestamp),
	}, nil
}

func (a *Adapter) FetchFundingRates(ctx context.Context, exchange string, symbols []string) ([]ports.FundingRate, error) {
	rates, err := a.service.FetchFundingRates(ctx, exchange, symbols)
	if err != nil {
		return nil, err
	}

	result := make([]ports.FundingRate, 0, len(rates))
	for _, rate := range rates {
		result = append(result, ports.FundingRate{
			Exchange:        exchange,
			Symbol:          rate.Symbol,
			Rate:            decimal.NewFromFloat(rate.FundingRate),
			NextFundingTime: time.Time(rate.NextFundingTime),
			Timestamp:       time.Time(rate.Timestamp),
		})
	}

	return result, nil
}

// IsHealthy checks if the gateway is healthy.
func (a *Adapter) IsHealthy(ctx context.Context) bool {
	return a.service.IsHealthy(ctx)
}

// ============================================================
// TradingGateway Implementation
// ============================================================

// PlaceOrder places a new order.
func (a *Adapter) PlaceOrder(ctx context.Context, req ports.OrderRequest) (ports.OrderResult, error) {
	// Note: The existing CCXT service may not have a PlaceOrder method.
	// This is a placeholder that should be implemented when trading is enabled.
	return ports.OrderResult{}, errors.New("PlaceOrder not implemented: trading not enabled")
}

// CancelOrder cancels an existing order.
func (a *Adapter) CancelOrder(ctx context.Context, exchange, orderID string) error {
	return a.service.CancelOrder(ctx, exchange, orderID, "")
}

// CancelAllOrders cancels all orders for a symbol.
func (a *Adapter) CancelAllOrders(ctx context.Context, exchange, symbol string) error {
	// Fetch all open orders and cancel them
	resp, err := a.service.FetchOpenOrdersForSymbol(ctx, exchange, symbol)
	if err != nil {
		return err
	}

	var errs []error
	for _, order := range resp.Orders {
		if err := a.service.CancelOrder(ctx, exchange, order.ID, symbol); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func (a *Adapter) FetchOrder(ctx context.Context, exchange, orderID string) (ports.Order, error) {
	resp, err := a.service.FetchOrder(ctx, exchange, orderID, "")
	if err != nil {
		return ports.Order{}, err
	}

	avgPrice := decimal.Zero
	if resp.Order.Filled.GreaterThan(decimal.Zero) {
		avgPrice = resp.Order.Cost.Div(resp.Order.Filled)
	}

	return ports.Order{
		Exchange:     exchange,
		OrderID:      resp.Order.ID,
		ClientID:     resp.Order.ClientOrderID,
		Symbol:       resp.Order.Symbol,
		Side:         ports.OrderSide(resp.Order.Side),
		Type:         ports.OrderType(resp.Order.Type),
		Amount:       resp.Order.Amount,
		Filled:       resp.Order.Filled,
		Price:        resp.Order.Price,
		AveragePrice: avgPrice,
		Status:       ports.OrderStatus(resp.Order.Status),
		Timestamp:    resp.Order.CreatedAt,
	}, nil
}

func (a *Adapter) FetchOpenOrders(ctx context.Context, exchange, symbol string) ([]ports.Order, error) {
	var resp *ccxt.OpenOrdersResponse
	var err error

	if symbol != "" {
		resp, err = a.service.FetchOpenOrdersForSymbol(ctx, exchange, symbol)
	} else {
		resp, err = a.service.FetchOpenOrders(ctx, exchange)
	}

	if err != nil {
		return nil, err
	}

	orders := make([]ports.Order, 0, len(resp.Orders))
	for _, o := range resp.Orders {
		avgPrice := decimal.Zero
		if o.Filled.GreaterThan(decimal.Zero) {
			avgPrice = o.Cost.Div(o.Filled)
		}
		orders = append(orders, ports.Order{
			Exchange:     exchange,
			OrderID:      o.ID,
			ClientID:     o.ClientOrderID,
			Symbol:       o.Symbol,
			Side:         ports.OrderSide(o.Side),
			Type:         ports.OrderType(o.Type),
			Amount:       o.Amount,
			Filled:       o.Filled,
			Price:        o.Price,
			AveragePrice: avgPrice,
			Status:       ports.OrderStatus(o.Status),
			Timestamp:    o.CreatedAt,
		})
	}

	return orders, nil
}

func (a *Adapter) FetchPositions(ctx context.Context, exchange string) ([]ports.Position, error) {
	resp, err := a.service.FetchPositions(ctx, exchange)
	if err != nil {
		return nil, err
	}

	positions := make([]ports.Position, 0, len(resp.Positions))
	for _, p := range resp.Positions {
		positions = append(positions, ports.Position{
			Exchange:         exchange,
			Symbol:           p.Symbol,
			Side:             ports.OrderSide(p.Side),
			Amount:           p.Size,
			EntryPrice:       p.EntryPrice,
			MarkPrice:        p.MarkPrice,
			UnrealizedPnL:    p.UnrealizedPnl,
			Leverage:         decimal.NewFromFloat(float64(p.Leverage)),
			LiquidationPrice: p.LiquidationPrice,
		})
	}

	return positions, nil
}

func (a *Adapter) FetchBalances(ctx context.Context, exchange string) ([]ports.Balance, error) {
	resp, err := a.service.FetchBalance(ctx, exchange)
	if err != nil {
		return nil, err
	}

	balances := make([]ports.Balance, 0, len(resp.Total))
	for currency, total := range resp.Total {
		free := resp.Free[currency]
		used := resp.Used[currency]
		balances = append(balances, ports.Balance{
			Exchange: exchange,
			Currency: currency,
			Total:    total,
			Free:     free,
			Used:     used,
		})
	}

	return balances, nil
}

// ============================================================
// ExchangeRegistry Implementation
// ============================================================

// Registry manages exchange gateways.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]*Adapter
}

// NewRegistry creates a new exchange registry.
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]*Adapter),
	}
}

// Register registers an exchange adapter.
func (r *Registry) Register(exchange string, adapter *Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[exchange] = adapter
}

// ErrExchangeNotFound is returned when an exchange is not registered.
var ErrExchangeNotFound = errors.New("exchange not found")

// GetGateway returns the gateway for an exchange.
func (r *Registry) GetGateway(exchange string) (ports.ExchangeGateway, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[exchange]
	if !ok {
		return nil, ErrExchangeNotFound
	}
	return adapter, nil
}

// GetMarketDataGateway returns the market data gateway for an exchange.
func (r *Registry) GetMarketDataGateway(exchange string) (ports.MarketDataGateway, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[exchange]
	if !ok {
		return nil, ErrExchangeNotFound
	}
	return adapter, nil
}

// GetTradingGateway returns the trading gateway for an exchange.
func (r *Registry) GetTradingGateway(exchange string) (ports.TradingGateway, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	adapter, ok := r.adapters[exchange]
	if !ok {
		return nil, ErrExchangeNotFound
	}
	return adapter, nil
}

// ListExchanges returns all configured exchanges.
func (r *Registry) ListExchanges(ctx context.Context) ([]ports.ExchangeInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	exchanges := make([]ports.ExchangeInfo, 0, len(r.adapters))
	for id := range r.adapters {
		exchanges = append(exchanges, ports.ExchangeInfo{
			ID:      id,
			Name:    id,
			Enabled: true,
		})
	}
	return exchanges, nil
}

// IsExchangeEnabled checks if an exchange is enabled.
func (r *Registry) IsExchangeEnabled(exchange string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.adapters[exchange]
	return ok
}
