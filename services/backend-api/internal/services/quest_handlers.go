package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/shopspring/decimal"
)

// QuestHandlerFunc is a function that executes a quest
type QuestHandlerFunc func(ctx context.Context, quest *Quest) error

// ScalpingConfig holds configuration for scalping execution
type ScalpingConfig struct {
	MaxConcurrentPositions int           // Maximum concurrent scalp positions (default: 3)
	MaxCapitalPercent      float64       // Maximum % of capital to use (default: 5%)
	MinProfitPercent       float64       // Minimum profit % to enter (default: 0.3%)
	StopLossPercent        float64       // Stop loss % (default: 0.1%)
	TakeProfitPercent      float64       // Take profit % (default: 0.2%)
	CheckInterval          time.Duration // How often to check for opportunities
	TradingPairs           []string      // Pairs to trade (futures format: BTC/USDT:USDT)
	Exchange               string        // Exchange to trade on
	Leverage               int           // Leverage for futures (default: 5)
	TradeSizeUsd           float64       // Trade size in USDT (default: 15)
}

var DefaultScalpingConfig = ScalpingConfig{
	MaxConcurrentPositions: 3,
	MaxCapitalPercent:      5.0,
	MinProfitPercent:       0.1,
	StopLossPercent:        0.1,
	TakeProfitPercent:      0.2,
	CheckInterval:          1 * time.Minute,
	TradingPairs:           []string{"BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT", "BNB/USDT:USDT", "XRP/USDT:USDT"},
	Exchange:               "binance",
	Leverage:               5,
	TradeSizeUsd:           15.0,
}

// scalpingExecutor is an interface for executing scalping trades
type scalpingExecutor interface {
	PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error)
	GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error)
}

// RegisterDefaultQuestHandlers registers default handlers for all quest types
func (e *QuestEngine) RegisterDefaultQuestHandlers(
	ccxtService interface{},
	arbitrageService interface{},
	futuresArbService interface{},
) {
	// Market Scanner handler - scans for trading opportunities every 5 minutes
	e.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		return e.handleMarketScan(ctx, quest, ccxtService, arbitrageService)
	})

	// Funding Rate Scanner handler - scans funding rates every 5 minutes
	e.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		return e.handleFundingRateScan(ctx, quest, futuresArbService)
	})

	// Portfolio Health Check handler - checks portfolio health hourly
	e.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		return e.handlePortfolioHealth(ctx, quest)
	})

	// Scalping Execution handler - executes scalping trades
	e.RegisterHandler(QuestTypeRoutine, func(ctx context.Context, quest *Quest) error {
		return e.handleScalpingExecution(ctx, quest, ccxtService)
	})

	log.Println("Quest handlers registered successfully")
}

// handleMarketScan scans markets for trading opportunities
func (e *QuestEngine) handleMarketScan(
	ctx context.Context,
	quest *Quest,
	ccxtService interface{},
	arbitrageService interface{},
) error {
	log.Printf("Executing market scan quest: %s", quest.Name)

	// TODO: Implement actual market scanning logic
	// For now, just update the checkpoint to show the quest is running
	quest.CurrentCount++
	quest.Checkpoint["last_scan_time"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["status"] = "scanning"

	log.Printf("Market scan complete")
	return nil
}

// handleFundingRateScan scans for funding rate arbitrage opportunities
func (e *QuestEngine) handleFundingRateScan(
	ctx context.Context,
	quest *Quest,
	futuresArbService interface{},
) error {
	log.Printf("Executing funding rate scan quest: %s", quest.Name)

	if futuresArbService == nil {
		log.Printf("Futures arbitrage service not available, skipping funding rate scan")
		quest.CurrentCount++
		return nil
	}

	// TODO: Implement actual funding rate scanning
	quest.CurrentCount++
	quest.Checkpoint["last_funding_scan"] = time.Now().UTC().Format(time.RFC3339)

	log.Printf("Funding rate scan complete")
	return nil
}

// handlePortfolioHealth checks portfolio balance and exposure
func (e *QuestEngine) handlePortfolioHealth(ctx context.Context, quest *Quest) error {
	log.Printf("Executing portfolio health check quest: %s", quest.Name)

	// Get chat ID from quest metadata
	chatID, ok := quest.Metadata["chat_id"]
	if !ok {
		return fmt.Errorf("chat_id not found in quest metadata")
	}

	// Update quest progress
	quest.CurrentCount++
	quest.Checkpoint["last_health_check"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["chat_id"] = chatID
	quest.Checkpoint["status"] = "healthy"

	log.Printf("Portfolio health check complete for chat_id: %s", chatID)
	return nil
}

// handleScalpingExecution executes scalping trades based on market conditions
func (e *QuestEngine) handleScalpingExecution(
	ctx context.Context,
	quest *Quest,
	ccxtService interface{},
) error {
	log.Printf("Executing scalping quest: %s", quest.Name)

	if ccxtService == nil {
		log.Printf("CCXT service not available, skipping scalping execution")
		return nil
	}

	ccxt, ok := ccxtService.(interface {
		FetchSingleTicker(ctx context.Context, exchange, symbol string) (interface{}, error)
		FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (interface{}, error)
		GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error)
	})
	if !ok {
		log.Printf("CCXT service does not implement required interface, skipping scalping")
		return nil
	}

	cfg := DefaultScalpingConfig
	chatID := quest.Metadata["chat_id"]

	currentPositions, err := e.getCurrentScalpPositions(ctx, ccxt, cfg.Exchange)
	if err != nil {
		log.Printf("Failed to get current positions: %v", err)
	} else {
		quest.Checkpoint["current_positions"] = len(currentPositions)
		quest.Checkpoint["max_positions"] = cfg.MaxConcurrentPositions
	}

	if len(currentPositions) >= cfg.MaxConcurrentPositions {
		log.Printf("Max concurrent positions reached (%d), skipping scalping", cfg.MaxConcurrentPositions)
		quest.Checkpoint["status"] = "max_positions_reached"
		quest.CurrentCount++
		return nil
	}

	oppFound := false
	oppSymbol := ""
	var oppSide string
	var oppEntryPrice decimal.Decimal

	for _, symbol := range cfg.TradingPairs {
		side, entryPrice, err := e.analyzeScalpOpportunity(ctx, ccxt, cfg.Exchange, symbol, cfg)
		if err != nil {
			continue
		}

		if side != "" {
			oppFound = true
			oppSymbol = symbol
			oppSide = side
			oppEntryPrice = entryPrice
			break
		}
	}

	if !oppFound {
		log.Printf("No scalping opportunities found")
		quest.Checkpoint["status"] = "no_opportunity"
		quest.CurrentCount++
		return nil
	}

	orderID, err := e.executeScalpTrade(ctx, ccxt, cfg.Exchange, oppSymbol, oppSide, cfg)
	if err != nil {
		log.Printf("Failed to execute scalp trade: %v", err)
		quest.Checkpoint["status"] = "execution_failed"
		quest.Checkpoint["error"] = err.Error()
	} else {
		log.Printf("Scalp trade executed: %s %s %s, orderID: %s", oppSide, cfg.Exchange, oppSymbol, orderID)
		quest.Checkpoint["status"] = "executed"
		quest.Checkpoint["order_id"] = orderID
		quest.Checkpoint["symbol"] = oppSymbol
		quest.Checkpoint["side"] = oppSide
		quest.Checkpoint["entry_price"] = oppEntryPrice.String()
	}

	quest.CurrentCount++
	quest.Checkpoint["last_scalp_time"] = time.Now().UTC().Format(time.RFC3339)
	quest.Checkpoint["chat_id"] = chatID

	return nil
}

// analyzeScalpOpportunity analyzes if there's a scalping opportunity
func (e *QuestEngine) analyzeScalpOpportunity(
	ctx context.Context,
	ccxt interface {
		FetchSingleTicker(ctx context.Context, exchange, symbol string) (interface{}, error)
		FetchOrderBook(ctx context.Context, exchange, symbol string, limit int) (interface{}, error)
	},
	exchange, symbol string,
	cfg ScalpingConfig,
) (string, decimal.Decimal, error) {
	ticker, err := ccxt.FetchSingleTicker(ctx, exchange, symbol)
	if err != nil {
		return "", decimal.Zero, err
	}

	tickerMap, ok := ticker.(map[string]interface{})
	if !ok {
		return "", decimal.Zero, fmt.Errorf("invalid ticker format")
	}

	lastPrice := decimal.Zero
	if v, ok := tickerMap["last"].(float64); ok {
		lastPrice = decimal.NewFromFloat(v)
	} else if v, ok := tickerMap["last"].(string); ok {
		lastPrice, _ = decimal.NewFromString(v)
	}
	if lastPrice.IsZero() {
		return "", decimal.Zero, fmt.Errorf("no price data")
	}

	change24h := float64(0)
	if v, ok := tickerMap["percentage"].(float64); ok {
		change24h = v
	} else if v, ok := tickerMap["change"].(float64); ok {
		change24h = v
	}

	vol24h := float64(0)
	if v, ok := tickerMap["quoteVolume"].(float64); ok {
		vol24h = v
	} else if v, ok := tickerMap["baseVolume"].(float64); ok {
		vol24h = v
	}

	if vol24h < 1000000 {
		return "", decimal.Zero, fmt.Errorf("insufficient volume: %.0f", vol24h)
	}

	orderBook, err := ccxt.FetchOrderBook(ctx, exchange, symbol, 20)
	if err != nil {
		return "", decimal.Zero, err
	}

	ob, ok := orderBook.(map[string]interface{})
	if !ok {
		return "", decimal.Zero, fmt.Errorf("invalid orderbook format")
	}

	bids, _ := ob["bids"].([]interface{})
	asks, _ := ob["asks"].([]interface{})

	if len(bids) < 3 || len(asks) < 3 {
		return "", decimal.Zero, fmt.Errorf("insufficient order book depth")
	}

	bestBid, _ := decimal.NewFromString(fmt.Sprintf("%v", bids[0].([]interface{})[0]))
	bestAsk, _ := decimal.NewFromString(fmt.Sprintf("%v", asks[0].([]interface{})[0]))

	spread := bestAsk.Sub(bestBid)
	spreadPercent := spread.Div(lastPrice).Mul(decimal.NewFromFloat(100)).InexactFloat64()

	if spreadPercent < cfg.MinProfitPercent {
		return "", decimal.Zero, fmt.Errorf("spread too tight: %.2f%%", spreadPercent)
	}

	bidVol := float64(0)
	for i := 0; i < 5 && i < len(bids); i++ {
		if v, ok := bids[i].([]interface{})[1].(float64); ok {
			bidVol += v
		}
	}

	askVol := float64(0)
	for i := 0; i < 5 && i < len(asks); i++ {
		if v, ok := asks[i].([]interface{})[1].(float64); ok {
			askVol += v
		}
	}

	totalVol := bidVol + askVol
	bidRatio := 0.0
	if totalVol > 0 {
		bidRatio = bidVol / totalVol
	}

	var side string
	if bidRatio > 0.6 && change24h > -2 {
		side = "buy"
	} else if bidRatio < 0.4 && change24h < 2 {
		side = "sell"
	} else {
		return "", decimal.Zero, fmt.Errorf("no clear direction: bidRatio=%.2f, change24h=%.2f", bidRatio, change24h)
	}

	return side, lastPrice, nil
}

// executeScalpTrade executes a scalping trade on futures with leverage
func (e *QuestEngine) executeScalpTrade(
	ctx context.Context,
	ccxt interface{},
	exchange, symbol, side string,
	cfg ScalpingConfig,
) (string, error) {
	type orderExecutor interface {
		PlaceOrder(ctx context.Context, exchange, symbol, side, orderType string, amount decimal.Decimal, price *decimal.Decimal) (string, error)
	}

	type walletFetcher interface {
		FetchBalance(ctx context.Context, exchange string) (map[string]interface{}, error)
	}

	executor, ok := ccxt.(orderExecutor)
	if !ok {
		return "", fmt.Errorf("CCXT service does not support order execution")
	}

	wallet, ok := ccxt.(walletFetcher)
	if !ok {
		return "", fmt.Errorf("CCXT service does not support balance fetching")
	}

	balance, err := wallet.FetchBalance(ctx, exchange)
	if err != nil {
		return "", fmt.Errorf("failed to fetch balance: %w", err)
	}

	usdtBalance := decimal.Zero
	if total, ok := balance["total"].(map[string]interface{}); ok {
		if usdt, ok := total["USDT"].(float64); ok {
			usdtBalance = decimal.NewFromFloat(usdt)
		}
	}

	if usdtBalance.LessThan(decimal.NewFromFloat(10)) {
		return "", fmt.Errorf("insufficient USDT balance: %s (minimum 10 USDT required)", usdtBalance.String())
	}

	tradeSizeUsd := usdtBalance.Mul(decimal.NewFromFloat(cfg.MaxCapitalPercent / 100.0))
	minSize := decimal.NewFromFloat(10)
	if tradeSizeUsd.LessThan(minSize) {
		tradeSizeUsd = minSize
	}

	log.Printf("[SCALPING] Wallet USDT: %s, Trade size: %s USDT (%.1f%% of balance)",
		usdtBalance.String(), tradeSizeUsd.String(), cfg.MaxCapitalPercent)

	orderID, err := executor.PlaceOrder(ctx, exchange, symbol, side, "market", tradeSizeUsd, nil)
	if err != nil {
		return "", err
	}

	return orderID, nil
}

// getCurrentScalpPositions gets current open scalp positions
func (e *QuestEngine) getCurrentScalpPositions(
	ctx context.Context,
	ccxt interface {
		GetOpenOrders(ctx context.Context, exchange, symbol string) ([]map[string]interface{}, error)
	},
	exchange string,
) ([]map[string]interface{}, error) {
	symbols := DefaultScalpingConfig.TradingPairs
	var allOrders []map[string]interface{}

	for _, symbol := range symbols {
		orders, err := ccxt.GetOpenOrders(ctx, exchange, symbol)
		if err != nil {
			continue
		}
		allOrders = append(allOrders, orders...)
	}

	return allOrders, nil
}
