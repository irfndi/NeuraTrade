package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/app/execution"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

type liveFuturesOrderRequest struct {
	IntentID        string `json:"intent_id"`
	ChatID          string `json:"chat_id"`
	Exchange        string `json:"exchange"`
	Symbol          string `json:"symbol"`
	Side            string `json:"side"`
	OrderType       string `json:"order_type"`
	Size            string `json:"size"`
	Price           string `json:"price"`
	ProductType     string `json:"product_type"`
	MarginMode      string `json:"margin_mode"`
	Leverage        string `json:"leverage"`
	ReduceOnly      bool   `json:"reduce_only"`
	PortfolioValue  string `json:"portfolio_value"`
	CurrentPosition string `json:"current_position"`
	Confidence      string `json:"confidence"`
	StopLoss        string `json:"stop_loss"`
	TakeProfit      string `json:"take_profit"`
}

type liveFuturesOrderResponse struct {
	IntentID    string `json:"intent_id"`
	OrderID     string `json:"order_id"`
	ClientID    string `json:"client_id"`
	Exchange    string `json:"exchange"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	FilledQty   string `json:"filled_qty"`
	FilledPrice string `json:"filled_price"`
	Fee         string `json:"fee"`
	Status      string `json:"status"`
	Timestamp   string `json:"timestamp"`
}

type liveFuturesPositionResponse struct {
	ID               string `json:"id"`
	Symbol           string `json:"symbol"`
	Side             string `json:"side"`
	ProductType      string `json:"product_type"`
	MarginMode       string `json:"margin_mode"`
	Leverage         int    `json:"leverage"`
	Quantity         string `json:"quantity"`
	Available        string `json:"available"`
	EntryPrice       string `json:"entry_price"`
	LiquidationPrice string `json:"liquidation_price"`
	UnrealizedPnl    string `json:"unrealized_pnl"`
	MarginCoin       string `json:"margin_coin"`
}

type liveFuturesPositionsResponse struct {
	Exchange  string                        `json:"exchange"`
	Positions []liveFuturesPositionResponse `json:"positions"`
	Count     int                           `json:"count"`
	Timestamp string                        `json:"timestamp"`
}

func (e *riskGatedLiveExecution) getFuturesPositions(c *gin.Context) {
	exchange := liveLookupExchange(c.Query("exchange"))
	if exchange == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange is required"})
		return
	}
	positions, err := e.orderLookup.FetchPositions(c.Request.Context(), exchange)
	if err != nil || positions == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live positions unavailable"})
		return
	}

	response := liveFuturesPositionsResponse{
		Exchange:  positions.Exchange,
		Positions: make([]liveFuturesPositionResponse, 0, len(positions.Positions)),
		Count:     len(positions.Positions),
		Timestamp: positions.Timestamp,
	}
	for _, position := range positions.Positions {
		response.Positions = append(response.Positions, liveFuturesPositionResponse{
			ID:               position.ID,
			Symbol:           position.Symbol,
			Side:             position.Side,
			ProductType:      "USDT-FUTURES",
			MarginMode:       position.MarginMode,
			Leverage:         position.Leverage,
			Quantity:         position.Size.String(),
			Available:        position.Size.String(),
			EntryPrice:       position.EntryPrice.String(),
			LiquidationPrice: position.LiquidationPrice.String(),
			UnrealizedPnl:    position.UnrealizedPnl.String(),
			MarginCoin:       "USDT",
		})
	}
	c.JSON(http.StatusOK, response)
}

func (e *riskGatedLiveExecution) placeFuturesOrder(c *gin.Context) {
	var request liveFuturesOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid order request"})
		return
	}
	if request.IntentID == "" {
		request.IntentID = uuid.NewString()
	}
	if request.Exchange == "" || request.Symbol == "" || request.ChatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange, symbol, and chat_id are required"})
		return
	}
	if request.Side != "buy" && request.Side != "sell" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "side must be buy or sell"})
		return
	}
	if request.OrderType == "" {
		request.OrderType = "market"
	}
	if request.OrderType != "market" && request.OrderType != "limit" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_type must be market or limit"})
		return
	}
	if request.ProductType == "" {
		request.ProductType = "USDT-FUTURES"
	}
	if request.MarginMode == "" {
		request.MarginMode = "crossed"
	}

	amount, price, err := parseRequiredOrderDecimals(request)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if amount.IsNegative() || amount.IsZero() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "size must be positive"})
		return
	}
	if price.IsNegative() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "price must not be negative"})
		return
	}
	// Market orders may omit the price; limit orders always require one.
	if request.OrderType == "limit" && !price.IsPositive() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "limit orders require a positive price"})
		return
	}

	// Validate against the live MARKET price, never the client-supplied
	// price: a manipulated price must not be able to fake a small notional
	// and bypass the max-notional / max-leverage gates.
	marketPrice, err := e.liveMarketPrice(c, request.Exchange, request.Symbol)
	if err != nil || !marketPrice.IsPositive() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live market price unavailable"})
		return
	}
	// Market orders fill at the live market price, so their notional MUST
	// be computed from the market price — a fake low client price must not
	// bypass the max-notional / max-leverage gates. A supplied market price
	// must also not deviate beyond a tolerance from the fetched price.
	notionalPrice := marketPrice
	if price.IsPositive() && request.OrderType == "market" {
		deviation := price.Sub(marketPrice).Abs().Div(marketPrice)
		if deviation.GreaterThan(decimal.NewFromFloat(0.05)) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "order price deviates more than 5% from live market price"})
			return
		}
	}
	// Limit orders are bounded by their limit price (they fill at or better
	// than the limit), so their notional uses the client price; legitimately
	// far-from-market limit strategies are not rejected.
	if request.OrderType == "limit" && price.IsPositive() {
		notionalPrice = price
	}

	portfolioValue, err := e.livePortfolioValue(c, request.Exchange)
	if err != nil || !portfolioValue.IsPositive() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "live account balance unavailable"})
		return
	}
	notional := amount.Mul(notionalPrice).Abs()
	if notional.GreaterThan(e.maxNotional) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order exceeds configured live notional limit"})
		return
	}

	currentPosition := decimal.Zero
	if request.CurrentPosition != "" {
		currentPosition, err = decimal.NewFromString(request.CurrentPosition)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid current_position"})
			return
		}
	}
	confidence := 1.0
	if request.Confidence != "" {
		if _, err := fmt.Sscanf(request.Confidence, "%f", &confidence); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid confidence"})
			return
		}
	}
	stopLoss, err := optionalDecimal(request.StopLoss)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid stop_loss"})
		return
	}
	takeProfit, err := optionalDecimal(request.TakeProfit)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid take_profit"})
		return
	}
	leverage := decimal.NewFromInt(1)
	if request.Leverage != "" {
		leverage, err = decimal.NewFromString(request.Leverage)
		if err != nil || !leverage.IsPositive() {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid leverage"})
			return
		}
	}

	// The risk intent carries the notional reference price (market price for
	// market orders, limit price for limit orders), so the policy engine's
	// max-notional / max-leverage rules gate on real exposure rather than a
	// client-supplied price that could fake a small notional.
	intent := ports.OrderIntent{
		IntentID:        request.IntentID,
		Exchange:        request.Exchange,
		Symbol:          request.Symbol,
		Side:            ports.OrderSide(request.Side),
		Type:            ports.OrderType(request.OrderType),
		Amount:          amount,
		Price:           notionalPrice,
		Confidence:      confidence,
		StopLoss:        decimalOrZero(stopLoss),
		TakeProfit:      decimalOrZero(takeProfit),
		CurrentPosition: currentPosition,
		PortfolioValue:  portfolioValue,
	}

	// Decouple risk evaluation, placement, and status from the HTTP request
	// context: a client disconnect mid-request must not orphan or cancel a
	// live exchange order. Gateway calls use a server-side context with a
	// fixed timeout; a cancellation then surfaces as "unknown status" so the
	// client reconciles with the exchange instead of assuming rejection.
	execCtx, execCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer execCancel()

	decision, err := e.riskRef.EvaluateIntent(execCtx, intent)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "risk evaluation failed"})
		return
	}
	if !decision.Approved {
		c.JSON(http.StatusForbidden, gin.H{"error": decision.Reason, "rule": decision.RuleName})
		return
	}

	requestBody := ports.OrderRequest{
		Exchange:   request.Exchange,
		Symbol:     request.Symbol,
		Side:       ports.OrderSide(request.Side),
		Type:       ports.OrderType(request.OrderType),
		Amount:     amount,
		Price:      price,
		ClientID:   request.IntentID,
		StopPrice:  decimalOrZero(stopLoss),
		TakeProfit: decimalOrZero(takeProfit),
		ReduceOnly: request.ReduceOnly,
		Leverage:   leverage,
		ChatID:     request.ChatID,
	}
	if _, err := e.executionRef.Ask(execCtx, execution.PlaceOrderMsg{
		IntentID:     request.IntentID,
		Request:      requestBody,
		RiskApproved: true,
		StrategyID:   "neuratrade-cli-ts",
		Metadata:     map[string]interface{}{"chat_id": request.ChatID, "product_type": request.ProductType, "margin_mode": request.MarginMode},
	}); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			c.JSON(http.StatusAccepted, gin.H{
				"intent_id": request.IntentID,
				"status":    "unknown",
				"error":     "placement in flight; reconcile with the exchange",
			})
			return
		}
		if errors.Is(err, execution.ErrIntentConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "execution rejected"})
		return
	}
	statusValue, err := e.executionRef.Ask(execCtx, execution.GetOrderStatusMsg{IntentID: request.IntentID})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			c.JSON(http.StatusAccepted, gin.H{
				"intent_id": request.IntentID,
				"status":    "unknown",
				"error":     "placement in flight; reconcile with the exchange",
			})
			return
		}
		c.JSON(http.StatusBadGateway, gin.H{"error": "execution status unavailable"})
		return
	}
	executionIntent, ok := statusValue.(*execution.OrderIntent)
	if !ok || executionIntent == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "execution returned invalid status"})
		return
	}

	statusCode := http.StatusOK
	if executionIntent.Status != ports.OrderStatusFilled {
		statusCode = http.StatusAccepted
	}
	c.JSON(statusCode, liveFuturesOrderResponse{
		IntentID:    request.IntentID,
		OrderID:     executionIntent.ExchangeOrderID,
		ClientID:    executionIntent.ClientOrderID,
		Exchange:    request.Exchange,
		Symbol:      request.Symbol,
		Side:        request.Side,
		FilledQty:   executionIntent.FilledAmount.String(),
		FilledPrice: executionIntent.FillPrice.String(),
		Fee:         executionIntent.Fee.String(),
		Status:      string(executionIntent.Status),
		Timestamp:   time.Now().UTC().Format(time.RFC3339Nano),
	})
}

func parseRequiredOrderDecimals(request liveFuturesOrderRequest) (decimal.Decimal, decimal.Decimal, error) {
	amount, err := decimal.NewFromString(request.Size)
	if err != nil {
		return decimal.Zero, decimal.Zero, errors.New("invalid size")
	}
	// Market orders may omit the price; the notional gate then uses the
	// live market price. Limit orders must supply one (validated later).
	price := decimal.Zero
	if strings.TrimSpace(request.Price) != "" {
		price, err = decimal.NewFromString(request.Price)
		if err != nil {
			return decimal.Zero, decimal.Zero, errors.New("invalid price")
		}
	}
	return amount, price, nil
}

func optionalDecimal(raw string) (*decimal.Decimal, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	value, err := decimal.NewFromString(raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func decimalOrZero(value *decimal.Decimal) decimal.Decimal {
	if value == nil {
		return decimal.Zero
	}
	return *value
}

func (e *riskGatedLiveExecution) livePortfolioValue(c *gin.Context, exchange string) (decimal.Decimal, error) {
	response, err := e.orderLookup.FetchBalance(c.Request.Context(), liveLookupExchange(exchange))
	if err != nil || response == nil {
		return decimal.Zero, err
	}
	if value := response.Free["USDT"]; value.IsPositive() {
		return value, nil
	}
	if value := response.Total["USDT"]; value.IsPositive() {
		return value, nil
	}
	return decimal.Zero, errors.New("USDT account balance is empty")
}

// liveMarketPrice fetches the current market price for the order's symbol.
// It is the authoritative price source for the notional gate so that a
// client-supplied price cannot bypass the max-notional limit.
func (e *riskGatedLiveExecution) liveMarketPrice(c *gin.Context, exchange, symbol string) (decimal.Decimal, error) {
	tickCtx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	ticker, err := e.orderLookup.FetchSingleTicker(tickCtx, liveLookupExchange(exchange), symbol)
	if err != nil || ticker == nil {
		return decimal.Zero, err
	}
	price := ticker.GetPrice()
	if price.IsZero() {
		price = ticker.GetAsk()
	}
	if price.IsZero() {
		price = ticker.GetBid()
	}
	if price.IsZero() {
		return decimal.Zero, errors.New("market price is zero")
	}
	return price, nil
}
