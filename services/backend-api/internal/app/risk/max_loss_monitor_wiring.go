package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/app/execution"
	"github.com/irfndi/neuratrade/internal/ports"
	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

// ExchangePriceProvider fetches mark prices from a TradingGateway-compatible
// exchange registry. Uses MarketDataGateway.FetchTick under the hood.
type ExchangePriceProvider struct {
	registry ports.ExchangeRegistry
	timeout  time.Duration
}

// NewExchangePriceProvider creates a price provider backed by the exchange registry.
func NewExchangePriceProvider(registry ports.ExchangeRegistry) *ExchangePriceProvider {
	return &ExchangePriceProvider{
		registry: registry,
		timeout:  5 * time.Second,
	}
}

// GetPrice implements PriceProvider. Returns the last traded price for the symbol.
func (p *ExchangePriceProvider) GetPrice(ctx context.Context, exchange, symbol string) (decimal.Decimal, error) {
	if p.registry == nil {
		return decimal.Zero, fmt.Errorf("exchange registry is nil")
	}
	gw, err := p.registry.GetMarketDataGateway(exchange)
	if err != nil {
		return decimal.Zero, fmt.Errorf("get market data gateway for %s: %w", exchange, err)
	}
	if gw == nil {
		return decimal.Zero, fmt.Errorf("nil market data gateway for %s", exchange)
	}
	callCtx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	tick, err := gw.FetchTick(callCtx, exchange, symbol)
	if err != nil {
		return decimal.Zero, fmt.Errorf("fetch tick for %s/%s: %w", exchange, symbol, err)
	}
	if tick.Last.IsZero() {
		return decimal.Zero, fmt.Errorf("zero last price for %s/%s", exchange, symbol)
	}
	return tick.Last, nil
}

// GatewayPositionCloser closes positions by placing market orders in the
// opposite direction via the TradingGateway. Used by MaxLossMonitor when a
// position breaches the max-loss threshold.
type GatewayPositionCloser struct {
	registry ports.ExchangeRegistry
	timeout  time.Duration
}

// NewGatewayPositionCloser creates a position closer backed by the exchange registry.
func NewGatewayPositionCloser(registry ports.ExchangeRegistry) *GatewayPositionCloser {
	return &GatewayPositionCloser{
		registry: registry,
		timeout:  10 * time.Second,
	}
}

// ClosePosition implements PositionCloser. Places a market order in the
// opposite direction to flatten the position. Uses ReduceOnly for derivatives.
func (c *GatewayPositionCloser) ClosePosition(ctx context.Context, pos PositionSnapshot, reason string) error {
	if c.registry == nil {
		return fmt.Errorf("exchange registry is nil")
	}
	gw, err := c.registry.GetTradingGateway(pos.Exchange)
	if err != nil {
		return fmt.Errorf("get trading gateway for %s: %w", pos.Exchange, err)
	}
	if gw == nil {
		return fmt.Errorf("nil trading gateway for %s", pos.Exchange)
	}
	oppSide := ports.OrderSideSell
	if pos.Side == "sell" {
		oppSide = ports.OrderSideBuy
	}
	clientID := fmt.Sprintf("maxloss-%d", time.Now().UTC().UnixNano())
	req := ports.OrderRequest{
		Exchange:   pos.Exchange,
		Symbol:     pos.Symbol,
		Side:       oppSide,
		Type:       ports.OrderTypeMarket,
		Amount:     pos.Quantity,
		ClientID:   clientID,
		ReduceOnly: true,
	}
	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	result, err := gw.PlaceOrder(callCtx, req)
	if err != nil {
		return fmt.Errorf("place order %s %s %s qty=%s: %w", pos.Exchange, oppSide, pos.Symbol, pos.Quantity.String(), err)
	}
	zaplogrus.Warnf("[max-loss-monitor] CLOSE order placed: %s/%s side=%s qty=%s orderID=%s reason=%s",
		pos.Exchange, pos.Symbol, oppSide, pos.Quantity.String(), result.OrderID, reason)
	return nil
}

// TrackFromOrderIntent extracts a PositionSnapshot from a filled OrderIntent
// and registers it with the MaxLossMonitor. Idempotent on duplicate fills.
func TrackFromOrderIntent(monitor *MaxLossMonitor, intent *execution.OrderIntent) {
	if monitor == nil || intent == nil {
		return
	}
	if intent.Status != ports.OrderStatusFilled {
		return
	}
	if intent.FilledAmount.IsZero() {
		return
	}
	entryPrice := intent.FillPrice
	if entryPrice.IsZero() {
		entryPrice = intent.Request.Price
	}
	if entryPrice.IsZero() {
		zaplogrus.Infof("[max-loss-monitor] skip track: zero entry price for intent %s", intent.IntentID)
		return
	}
	monitor.TrackPosition(PositionSnapshot{
		Symbol:     intent.Request.Symbol,
		Exchange:   intent.Request.Exchange,
		Side:       string(intent.Request.Side),
		EntryPrice: entryPrice,
		Quantity:   intent.FilledAmount,
		OpenedAt:   intent.UpdatedAt,
	})
}

// UntrackFromBaseEvent extracts exchange/symbol from a position-closed event
// and unregisters the position from the MaxLossMonitor. Best-effort: if the
// event payload can't be parsed, the caller should still treat the position
// as closed at the application level.
func UntrackFromBaseEvent(monitor *MaxLossMonitor, payload []byte) {
	if monitor == nil || len(payload) == 0 {
		return
	}
	var env struct {
		Exchange string `json:"exchange"`
		Symbol   string `json:"symbol"`
	}
	if err := json.Unmarshal(payload, &env); err != nil {
		return
	}
	if env.Exchange != "" && env.Symbol != "" {
		monitor.UntrackPosition(env.Exchange, env.Symbol)
	}
}
