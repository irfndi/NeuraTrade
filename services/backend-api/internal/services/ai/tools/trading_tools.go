package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/shopspring/decimal"
)

// MarketDataProvider provides market data
type MarketDataProvider interface {
	GetTicker(ctx context.Context, exchange, symbol string) (price, bid, ask, volume float64, err error)
	GetOrderBook(ctx context.Context, exchange, symbol string, depth int) (bids, asks []PriceLevel, err error)
	GetRecentSignals(ctx context.Context, symbol string, limit int) ([]Signal, error)
}

// OrderExecutor executes orders
type OrderExecutor interface {
	PlaceOrder(ctx context.Context, req *OrderRequest) (*OrderResult, error)
	CancelOrder(ctx context.Context, orderID string) error
	GetOpenOrders(ctx context.Context, symbol string) ([]Order, error)
}

// PositionManager manages positions
type PositionManager interface {
	GetPositions(ctx context.Context) ([]Position, error)
	GetPortfolio(ctx context.Context) (*Portfolio, error)
}

// RiskChecker checks risk levels
type RiskChecker interface {
	CheckRisk(ctx context.Context) (*RiskStatus, error)
	IsTradingAllowed() bool
}

// ArbitrageFinder finds arbitrage opportunities
type ArbitrageFinder interface {
	FindOpportunities(ctx context.Context, minProfit float64) ([]ArbitrageOpportunity, error)
}

// PriceLevel represents a price level in order book
type PriceLevel struct {
	Price  float64 `json:"price"`
	Volume float64 `json:"volume"`
}

// Signal represents a trading signal
type Signal struct {
	Symbol      string    `json:"symbol"`
	Exchange    string    `json:"exchange"`
	Type        string    `json:"type"`
	Strength    float64   `json:"strength"`
	Price       float64   `json:"price"`
	Reason      string    `json:"reason"`
	GeneratedAt time.Time `json:"generated_at"`
}

// OrderRequest represents an order request
type OrderRequest struct {
	Symbol   string  `json:"symbol"`
	Exchange string  `json:"exchange"`
	Side     string  `json:"side"` // "buy" or "sell"
	Type     string  `json:"type"` // "market", "limit"
	Size     float64 `json:"size"`
	Price    float64 `json:"price,omitempty"`
}

// OrderResult represents an order result
type OrderResult struct {
	OrderID string  `json:"order_id"`
	Status  string  `json:"status"`
	Price   float64 `json:"price"`
	Size    float64 `json:"size"`
}

// Order represents an open order
type Order struct {
	ID       string    `json:"id"`
	Symbol   string    `json:"symbol"`
	Exchange string    `json:"exchange"`
	Side     string    `json:"side"`
	Type     string    `json:"type"`
	Size     float64   `json:"size"`
	Price    float64   `json:"price"`
	Status   string    `json:"status"`
	Created  time.Time `json:"created"`
}

// Position represents an open position
type Position struct {
	Symbol        string    `json:"symbol"`
	Exchange      string    `json:"exchange"`
	Side          string    `json:"side"`
	Size          float64   `json:"size"`
	EntryPrice    float64   `json:"entry_price"`
	CurrentPrice  float64   `json:"current_price"`
	UnrealizedPnL float64   `json:"unrealized_pnl"`
	OpenedAt      time.Time `json:"opened_at"`
}

// Portfolio represents portfolio state
type Portfolio struct {
	TotalBalance    float64 `json:"total_balance"`
	AvailableFunds  float64 `json:"available_funds"`
	UnrealizedPnL   float64 `json:"unrealized_pnl"`
	RealizedPnL     float64 `json:"realized_pnl"`
	DailyPnL        float64 `json:"daily_pnl"`
	CurrentDrawdown float64 `json:"current_drawdown"`
}

// RiskStatus represents current risk status
type RiskStatus struct {
	CanTrade          bool    `json:"can_trade"`
	RiskLevel         string  `json:"risk_level"` // "low", "medium", "high", "critical"
	DailyLossPercent  float64 `json:"daily_loss_percent"`
	DrawdownPercent   float64 `json:"drawdown_percent"`
	ConsecutiveLosses int     `json:"consecutive_losses"`
	Reason            string  `json:"reason"`
}

// ArbitrageOpportunity represents an arbitrage opportunity
type ArbitrageOpportunity struct {
	ID              string    `json:"id"`
	Symbol          string    `json:"symbol"`
	BuyExchange     string    `json:"buy_exchange"`
	SellExchange    string    `json:"sell_exchange"`
	BuyPrice        float64   `json:"buy_price"`
	SellPrice       float64   `json:"sell_price"`
	ProfitPercent   float64   `json:"profit_percent"`
	EstimatedProfit float64   `json:"estimated_profit"`
	Volume          float64   `json:"volume"`
	DetectedAt      time.Time `json:"detected_at"`
}

// TradingToolsRegistry holds all trading tools
type TradingToolsRegistry struct {
	*Registry
	marketData  MarketDataProvider
	orderExec   OrderExecutor
	positionMgr PositionManager
	riskChecker RiskChecker
	arbFinder   ArbitrageFinder
}

// NewTradingToolsRegistry creates a new trading tools registry
func NewTradingToolsRegistry() *TradingToolsRegistry {
	return &TradingToolsRegistry{
		Registry: NewRegistry(),
	}
}

// SetMarketDataProvider sets the market data provider
func (r *TradingToolsRegistry) SetMarketDataProvider(p MarketDataProvider) {
	r.marketData = p
}

// SetOrderExecutor sets the order executor
func (r *TradingToolsRegistry) SetOrderExecutor(e OrderExecutor) {
	r.orderExec = e
}

// SetPositionManager sets the position manager
func (r *TradingToolsRegistry) SetPositionManager(m PositionManager) {
	r.positionMgr = m
}

// SetRiskChecker sets the risk checker
func (r *TradingToolsRegistry) SetRiskChecker(c RiskChecker) {
	r.riskChecker = c
}

// SetArbitrageFinder sets the arbitrage finder
func (r *TradingToolsRegistry) SetArbitrageFinder(f ArbitrageFinder) {
	r.arbFinder = f
}

// RegisterAllTools registers all available trading tools
func (r *TradingToolsRegistry) RegisterAllTools() {
	r.Register(&GetMarketDataTool{provider: r.marketData})
	r.Register(&AnalyzeSignalsTool{provider: r.marketData})
	r.Register(&PlaceOrderTool{executor: r.orderExec, riskChecker: r.riskChecker})
	r.Register(&CancelOrderTool{executor: r.orderExec})
	r.Register(&GetPositionsTool{manager: r.positionMgr})
	r.Register(&GetPortfolioTool{manager: r.positionMgr})
	r.Register(&CheckRiskTool{checker: r.riskChecker})
	r.Register(&FindArbitrageTool{finder: r.arbFinder})
}

// GetToolDefinitions returns LLM tool definitions for all registered tools
func (r *TradingToolsRegistry) GetToolDefinitions() []llm.ToolDefinition {
	var defs []llm.ToolDefinition
	for _, tool := range r.tools {
		if td, ok := tool.(ToolWithDefinition); ok {
			defs = append(defs, td.GetDefinition())
		}
	}
	return defs
}

// GetMarketDataTool gets current market data for a symbol
type GetMarketDataTool struct {
	provider MarketDataProvider
}

func (t *GetMarketDataTool) Name() string { return "get_market_data" }
func (t *GetMarketDataTool) Description() string {
	return "Get current market data including price, orderbook, and volume for a trading pair"
}
func makeToolDef(name, desc string, params map[string]interface{}) llm.ToolDefinition {
	return llm.ToolDefinition{
		Type: "function",
		Function: llm.FunctionDefinition{
			Name:        name,
			Description: desc,
			Parameters:  params,
		},
	}
}

func (t *GetMarketDataTool) GetDefinition() llm.ToolDefinition {
	return makeToolDef(t.Name(), t.Description(), map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"exchange": map[string]interface{}{
				"type":        "string",
				"description": "Exchange name (e.g., binance, coinbase)",
			},
			"symbol": map[string]interface{}{
				"type":        "string",
				"description": "Trading pair symbol (e.g., BTC/USDT)",
			},
			"depth": map[string]interface{}{
				"type":        "integer",
				"description": "Order book depth (default 5)",
				"default":     5,
			},
		},
		"required": []string{"exchange", "symbol"},
	})
}
func (t *GetMarketDataTool) Execute(params map[string]interface{}) (interface{}, error) {
	if t.provider == nil {
		return nil, fmt.Errorf("market data provider not configured")
	}
	ctx := context.Background()
	exchange := params["exchange"].(string)
	symbol := params["symbol"].(string)
	depth := 5
	if d, ok := params["depth"].(float64); ok {
		depth = int(d)
	}

	price, bid, ask, volume, err := t.provider.GetTicker(ctx, exchange, symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get ticker: %w", err)
	}

	bids, asks, err := t.provider.GetOrderBook(ctx, exchange, symbol, depth)
	if err != nil {
		return nil, fmt.Errorf("failed to get orderbook: %w", err)
	}

	spread := decimal.NewFromFloat(ask).Sub(decimal.NewFromFloat(bid)).Div(decimal.NewFromFloat(bid)).Mul(decimal.NewFromFloat(100))

	return map[string]interface{}{
		"symbol":     symbol,
		"exchange":   exchange,
		"price":      price,
		"bid":        bid,
		"ask":        ask,
		"spread_pct": spread.InexactFloat64(),
		"volume_24h": volume,
		"orderbook": map[string]interface{}{
			"bids":      bids,
			"asks":      asks,
			"bid_depth": sumVolume(bids),
			"ask_depth": sumVolume(asks),
			"imbalance": calculateImbalance(bids, asks),
		},
		"timestamp": time.Now().Unix(),
	}, nil
}

// AnalyzeSignalsTool analyzes trading signals for a symbol
type AnalyzeSignalsTool struct {
	provider MarketDataProvider
}

func (t *AnalyzeSignalsTool) Name() string { return "analyze_signals" }
func (t *AnalyzeSignalsTool) Description() string {
	return "Get and analyze recent trading signals for a symbol to understand market sentiment"
}
func (t *AnalyzeSignalsTool) GetDefinition() llm.ToolDefinition {
	return makeToolDef(t.Name(), t.Description(), map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"symbol": map[string]interface{}{
				"type":        "string",
				"description": "Trading pair symbol",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Number of signals to analyze",
				"default":     10,
			},
		},
		"required": []string{"symbol"},
	})
}
func (t *AnalyzeSignalsTool) Execute(params map[string]interface{}) (interface{}, error) {
	if t.provider == nil {
		return nil, fmt.Errorf("market data provider not configured")
	}
	ctx := context.Background()
	symbol := params["symbol"].(string)
	limit := 10
	if l, ok := params["limit"].(float64); ok {
		limit = int(l)
	}

	signals, err := t.provider.GetRecentSignals(ctx, symbol, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get signals: %w", err)
	}

	buyCount := 0
	sellCount := 0
	neutralCount := 0
	avgStrength := 0.0

	for _, s := range signals {
		switch s.Type {
		case "buy":
			buyCount++
		case "sell":
			sellCount++
		default:
			neutralCount++
		}
		avgStrength += s.Strength
	}

	if len(signals) > 0 {
		avgStrength /= float64(len(signals))
	}

	sentiment := "neutral"
	if buyCount > sellCount*2 {
		sentiment = "bullish"
	} else if sellCount > buyCount*2 {
		sentiment = "bearish"
	}

	return map[string]interface{}{
		"symbol":        symbol,
		"signals_count": len(signals),
		"buy_signals":   buyCount,
		"sell_signals":  sellCount,
		"neutral_count": neutralCount,
		"avg_strength":  avgStrength,
		"sentiment":     sentiment,
		"signals":       signals,
		"timestamp":     time.Now().Unix(),
	}, nil
}

// PlaceOrderTool places a trade order
type PlaceOrderTool struct {
	executor    OrderExecutor
	riskChecker RiskChecker
}

func (t *PlaceOrderTool) Name() string { return "place_order" }
func (t *PlaceOrderTool) Description() string {
	return "Place a buy or sell order for a trading pair. USE CAREFULLY - this executes real trades."
}
func (t *PlaceOrderTool) GetDefinition() llm.ToolDefinition {
	return makeToolDef(t.Name(), t.Description(), map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"exchange": map[string]interface{}{
				"type":        "string",
				"description": "Exchange to trade on",
			},
			"symbol": map[string]interface{}{
				"type":        "string",
				"description": "Trading pair symbol",
			},
			"side": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"buy", "sell"},
				"description": "Order side",
			},
			"type": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"market", "limit"},
				"description": "Order type",
			},
			"size": map[string]interface{}{
				"type":        "number",
				"description": "Position size",
			},
			"price": map[string]interface{}{
				"type":        "number",
				"description": "Limit price (required for limit orders)",
			},
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Your reasoning for this trade",
			},
		},
		"required": []string{"exchange", "symbol", "side", "size", "reason"},
	})
}
func (t *PlaceOrderTool) Execute(params map[string]interface{}) (interface{}, error) {
	if t.executor == nil {
		return nil, fmt.Errorf("order executor not configured")
	}

	if t.riskChecker != nil && !t.riskChecker.IsTradingAllowed() {
		return map[string]interface{}{
			"success": false,
			"error":   "Trading not allowed - risk limits exceeded",
		}, nil
	}

	ctx := context.Background()
	req := &OrderRequest{
		Exchange: params["exchange"].(string),
		Symbol:   params["symbol"].(string),
		Side:     params["side"].(string),
		Size:     params["size"].(float64),
	}
	if t, ok := params["type"].(string); ok {
		req.Type = t
	} else {
		req.Type = "market"
	}
	if p, ok := params["price"].(float64); ok {
		req.Price = p
	}

	result, err := t.executor.PlaceOrder(ctx, req)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"success":  true,
		"order_id": result.OrderID,
		"status":   result.Status,
		"price":    result.Price,
		"size":     result.Size,
		"message":  fmt.Sprintf("Order placed successfully: %s %s %s @ %f", req.Side, req.Symbol, result.OrderID, result.Price),
	}, nil
}

// CancelOrderTool cancels an open order
type CancelOrderTool struct {
	executor OrderExecutor
}

func (t *CancelOrderTool) Name() string { return "cancel_order" }
func (t *CancelOrderTool) Description() string {
	return "Cancel an open order by order ID"
}
func (t *CancelOrderTool) GetDefinition() llm.ToolDefinition {
	return makeToolDef(t.Name(), t.Description(), map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"order_id": map[string]interface{}{
				"type":        "string",
				"description": "Order ID to cancel",
			},
		},
		"required": []string{"order_id"},
	})
}
func (t *CancelOrderTool) Execute(params map[string]interface{}) (interface{}, error) {
	if t.executor == nil {
		return nil, fmt.Errorf("order executor not configured")
	}
	ctx := context.Background()
	orderID := params["order_id"].(string)

	err := t.executor.CancelOrder(ctx, orderID)
	if err != nil {
		return map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}, nil
	}

	return map[string]interface{}{
		"success":  true,
		"order_id": orderID,
		"message":  fmt.Sprintf("Order %s cancelled successfully", orderID),
	}, nil
}

// GetPositionsTool gets open positions
type GetPositionsTool struct {
	manager PositionManager
}

func (t *GetPositionsTool) Name() string { return "get_positions" }
func (t *GetPositionsTool) Description() string {
	return "Get all open trading positions"
}
func (t *GetPositionsTool) GetDefinition() llm.ToolDefinition {
	return makeToolDef(t.Name(), t.Description(), map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	})
}
func (t *GetPositionsTool) Execute(params map[string]interface{}) (interface{}, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("position manager not configured")
	}
	ctx := context.Background()

	positions, err := t.manager.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get positions: %w", err)
	}

	totalPnL := 0.0
	for _, p := range positions {
		totalPnL += p.UnrealizedPnL
	}

	return map[string]interface{}{
		"positions":            positions,
		"count":                len(positions),
		"total_unrealized_pnl": totalPnL,
		"timestamp":            time.Now().Unix(),
	}, nil
}

// GetPortfolioTool gets portfolio state
type GetPortfolioTool struct {
	manager PositionManager
}

func (t *GetPortfolioTool) Name() string { return "get_portfolio" }
func (t *GetPortfolioTool) Description() string {
	return "Get current portfolio balance and P&L summary"
}
func (t *GetPortfolioTool) GetDefinition() llm.ToolDefinition {
	return makeToolDef(t.Name(), t.Description(), map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	})
}
func (t *GetPortfolioTool) Execute(params map[string]interface{}) (interface{}, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("position manager not configured")
	}
	ctx := context.Background()

	portfolio, err := t.manager.GetPortfolio(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get portfolio: %w", err)
	}

	return map[string]interface{}{
		"total_balance":    portfolio.TotalBalance,
		"available_funds":  portfolio.AvailableFunds,
		"unrealized_pnl":   portfolio.UnrealizedPnL,
		"realized_pnl":     portfolio.RealizedPnL,
		"daily_pnl":        portfolio.DailyPnL,
		"current_drawdown": portfolio.CurrentDrawdown,
		"timestamp":        time.Now().Unix(),
	}, nil
}

// CheckRiskTool checks current risk status
type CheckRiskTool struct {
	checker RiskChecker
}

func (t *CheckRiskTool) Name() string { return "check_risk" }
func (t *CheckRiskTool) Description() string {
	return "Check current risk levels and whether trading is allowed"
}
func (t *CheckRiskTool) GetDefinition() llm.ToolDefinition {
	return makeToolDef(t.Name(), t.Description(), map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	})
}
func (t *CheckRiskTool) Execute(params map[string]interface{}) (interface{}, error) {
	if t.checker == nil {
		return nil, fmt.Errorf("risk checker not configured")
	}
	ctx := context.Background()

	status, err := t.checker.CheckRisk(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to check risk: %w", err)
	}

	return map[string]interface{}{
		"can_trade":          status.CanTrade,
		"risk_level":         status.RiskLevel,
		"daily_loss_percent": status.DailyLossPercent,
		"drawdown_percent":   status.DrawdownPercent,
		"consecutive_losses": status.ConsecutiveLosses,
		"reason":             status.Reason,
		"timestamp":          time.Now().Unix(),
	}, nil
}

// FindArbitrageTool finds arbitrage opportunities
type FindArbitrageTool struct {
	finder ArbitrageFinder
}

func (t *FindArbitrageTool) Name() string { return "find_arbitrage" }
func (t *FindArbitrageTool) Description() string {
	return "Find current arbitrage opportunities across exchanges"
}
func (t *FindArbitrageTool) GetDefinition() llm.ToolDefinition {
	return makeToolDef(t.Name(), t.Description(), map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"min_profit": map[string]interface{}{
				"type":        "number",
				"description": "Minimum profit percentage to consider",
				"default":     0.1,
			},
		},
	})
}
func (t *FindArbitrageTool) Execute(params map[string]interface{}) (interface{}, error) {
	if t.finder == nil {
		return nil, fmt.Errorf("arbitrage finder not configured")
	}
	ctx := context.Background()
	minProfit := 0.1
	if m, ok := params["min_profit"].(float64); ok {
		minProfit = m
	}

	opportunities, err := t.finder.FindOpportunities(ctx, minProfit)
	if err != nil {
		return nil, fmt.Errorf("failed to find opportunities: %w", err)
	}

	bestProfit := 0.0
	if len(opportunities) > 0 {
		for _, o := range opportunities {
			if o.ProfitPercent > bestProfit {
				bestProfit = o.ProfitPercent
			}
		}
	}

	return map[string]interface{}{
		"opportunities":  opportunities,
		"count":          len(opportunities),
		"best_profit":    bestProfit,
		"min_profit_req": minProfit,
		"timestamp":      time.Now().Unix(),
	}, nil
}

// ============== HELPER FUNCTIONS ==============

func sumVolume(levels []PriceLevel) float64 {
	total := 0.0
	for _, l := range levels {
		total += l.Volume
	}
	return total
}

func calculateImbalance(bids, asks []PriceLevel) float64 {
	bidVol := sumVolume(bids)
	askVol := sumVolume(asks)
	total := bidVol + askVol
	if total == 0 {
		return 0
	}
	return (bidVol - askVol) / total
}
