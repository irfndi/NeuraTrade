package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/skill"
	"github.com/shopspring/decimal"
)

type AIScalpingConfig struct {
	Exchange          string
	Leverage          int
	MaxCapitalPct     float64
	MinConfidence     float64
	MaxIterations     int
	Timeout           time.Duration
	AutoExecute       bool
	MaxPairsToAnalyze int
	MaxCandidatePairs int
}

func DefaultAIScalpingConfig() AIScalpingConfig {
	return AIScalpingConfig{
		Exchange:          "binance",
		Leverage:          5,
		MaxCapitalPct:     5.0,
		MinConfidence:     0.7,
		MaxIterations:     3,
		Timeout:           180 * time.Second,
		AutoExecute:       true,
		MaxPairsToAnalyze: 10,
		MaxCandidatePairs: 200,
	}
}

type AITradingDecision struct {
	Action      string           `json:"action"`
	Symbol      string           `json:"symbol"`
	SizePercent float64          `json:"size_pct"`
	Confidence  float64          `json:"confidence"`
	Reasoning   string           `json:"reasoning"`
	StopLoss    *decimal.Decimal `json:"stop_loss,omitempty"`
	TakeProfit  *decimal.Decimal `json:"take_profit,omitempty"`
}

type TradingPortfolio struct {
	USDTBalance   float64 `json:"usdt_balance"`
	TotalValue    float64 `json:"total_value"`
	OpenPositions int     `json:"open_positions"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
}

type AIScalpingService struct {
	config        AIScalpingConfig
	llmClient     llm.Client
	skillRegistry *skill.Registry
	ccxtService   ccxt.CCXTService
	orderExecutor ScalpingOrderExecutor
	tradeMemory   *TradeMemory
}

func NewAIScalpingService(
	config AIScalpingConfig,
	llmClient llm.Client,
	skillRegistry *skill.Registry,
	ccxtService ccxt.CCXTService,
	orderExecutor ScalpingOrderExecutor,
	tradeMemory *TradeMemory,
) *AIScalpingService {
	return &AIScalpingService{
		config:        config,
		llmClient:     llmClient,
		skillRegistry: skillRegistry,
		ccxtService:   ccxtService,
		orderExecutor: orderExecutor,
		tradeMemory:   tradeMemory,
	}
}

func (s *AIScalpingService) ExecuteTradingCycle(ctx context.Context, portfolio TradingPortfolio) (*AITradingDecision, error) {
	log.Printf("[AI-SCALPING] Starting trading cycle for portfolio: %.2f USDT", portfolio.USDTBalance)
	ctx, cancel := context.WithTimeout(ctx, s.config.Timeout)
	defer cancel()

	signals, err := s.gatherMarketSignals(ctx)
	if err != nil {
		log.Printf("[AI-SCALPING] Failed to gather signals: %v", err)
		return nil, fmt.Errorf("failed to gather market signals: %w", err)
	}
	log.Printf("[AI-SCALPING] Gathered %d market signals", len(signals))

	decision, err := s.getAIDecision(ctx, signals, portfolio)
	if err != nil {
		log.Printf("[AI-SCALPING] Failed to get AI decision: %v", err)
		return nil, fmt.Errorf("failed to get AI decision: %w", err)
	}

	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	decision.Symbol = normalizeSymbolForComparison(decision.Symbol)

	log.Printf("[AI-SCALPING] AI decision: %s %s (confidence: %.2f)", decision.Action, decision.Symbol, decision.Confidence)

	if err := s.validateDecision(decision, signals); err != nil {
		return nil, fmt.Errorf("invalid AI decision: %w", err)
	}

	effectiveMinConfidence, effectiveMaxCapital := s.dynamicRiskThresholds()
	log.Printf(
		"[AI-SCALPING] Dynamic thresholds: min_confidence=%.2f max_capital_pct=%.2f",
		effectiveMinConfidence,
		effectiveMaxCapital,
	)

	if decision.Action == "hold" {
		log.Printf("[AI-SCALPING] AI decided to hold: %s", decision.Reasoning)
		return decision, nil
	}

	if decision.Confidence < effectiveMinConfidence {
		log.Printf("[AI-SCALPING] Confidence %.2f below minimum %.2f, skipping", decision.Confidence, effectiveMinConfidence)
		return decision, fmt.Errorf("confidence below threshold")
	}

	if s.config.AutoExecute && s.orderExecutor != nil {
		if err := s.executeDecision(ctx, decision, portfolio, effectiveMaxCapital); err != nil {
			return decision, fmt.Errorf("execution failed: %w", err)
		}
	}

	return decision, nil
}

type aiMarketSignal struct {
	Symbol             string  `json:"symbol"`
	Price              float64 `json:"price"`
	High24h            float64 `json:"high_24h"`
	Low24h             float64 `json:"low_24h"`
	Volume24h          float64 `json:"volume_24h"`
	BidAskSpread       float64 `json:"spread_pct"`
	OrderBookImbalance float64 `json:"ob_imbalance"`
	PriceChange24h     float64 `json:"price_change_24h_pct"`
}

func (s *AIScalpingService) discoverTradingPairs(ctx context.Context) ([]string, error) {
	markets, err := s.ccxtService.FetchMarkets(ctx, s.config.Exchange)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch markets: %w", err)
	}

	var candidates []string
	seen := make(map[string]struct{})
	for _, symbol := range markets.Symbols {
		comparison := normalizeSymbolForComparison(symbol)
		if comparison == "" {
			continue
		}
		// Support spot and perp variants like BTC/USDT and BTC/USDT:USDT.
		if !strings.Contains(comparison, "/USDT") {
			continue
		}
		if _, ok := seen[comparison]; ok {
			continue
		}
		seen[comparison] = struct{}{}
		candidates = append(candidates, symbol)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no USDT pairs discovered")
	}

	// Bound the scoring universe to keep the AI loop responsive on exchanges with
	// thousands of pairs.
	maxCandidates := s.config.MaxCandidatePairs
	if maxCandidates <= 0 {
		maxCandidates = 200
	}
	if len(candidates) > maxCandidates {
		candidates = candidates[:maxCandidates]
	}

	// Dynamically rank discovered symbols by liquidity + spread + intraday movement.
	scored, err := s.ccxtService.FetchMarketData(ctx, []string{s.config.Exchange}, candidates)
	if err != nil || len(scored) == 0 {
		limit := s.config.MaxPairsToAnalyze
		if limit > len(candidates) {
			limit = len(candidates)
		}
		log.Printf("[AI-SCALPING] Dynamic pair scoring unavailable (%v), using discovered subset", err)
		return candidates[:limit], nil
	}

	type pairScore struct {
		symbol string
		score  float64
	}
	pairs := make([]pairScore, 0, len(scored))
	for _, t := range scored {
		symbol := t.GetSymbol()
		price := t.GetPrice()
		if symbol == "" || price <= 0 {
			continue
		}
		vol := math.Max(t.GetVolume(), 0)
		spreadPct := 0.0
		if t.GetBid() > 0 && t.GetAsk() > 0 {
			spreadPct = ((t.GetAsk() - t.GetBid()) / price) * 100
		}
		rangePct := 0.0
		if t.GetHigh() > 0 && t.GetLow() > 0 {
			rangePct = ((t.GetHigh() - t.GetLow()) / price) * 100
		}
		liqScore := math.Log1p(vol)
		spreadPenalty := 1.0 / (1.0 + math.Max(spreadPct, 0))
		volatilityBoost := 1.0 + math.Max(rangePct, 0)
		score := liqScore * spreadPenalty * volatilityBoost
		pairs = append(pairs, pairScore{symbol: symbol, score: score})
	}

	sort.Slice(pairs, func(i, j int) bool {
		return pairs[i].score > pairs[j].score
	})

	limit := s.config.MaxPairsToAnalyze
	if limit > len(pairs) {
		limit = len(pairs)
	}
	selected := make([]string, 0, limit)
	for i := 0; i < limit; i++ {
		selected = append(selected, pairs[i].symbol)
	}

	log.Printf("[AI-SCALPING] Dynamically selected %d/%d pairs for AI analysis on %s", len(selected), len(candidates), s.config.Exchange)
	return selected, nil
}

func (s *AIScalpingService) gatherMarketSignals(ctx context.Context) ([]aiMarketSignal, error) {
	var signals []aiMarketSignal

	pairs, err := s.discoverTradingPairs(ctx)
	if err != nil {
		log.Printf("[AI-SCALPING] Failed dynamic pair discovery: %v", err)
		return nil, fmt.Errorf("dynamic pair discovery unavailable: %w", err)
	}
	if len(pairs) == 0 {
		return nil, fmt.Errorf("dynamic pair discovery returned no symbols")
	}

	log.Printf("[AI-SCALPING] Analyzing %d pairs on %s", len(pairs), s.config.Exchange)
	for _, symbol := range pairs {
		ticker, err := s.ccxtService.FetchSingleTicker(ctx, s.config.Exchange, symbol)
		if err != nil {
			log.Printf("[AI-SCALPING] Failed to fetch ticker for %s: %v", symbol, err)
			continue
		}

		obResp, err := s.ccxtService.FetchOrderBook(ctx, s.config.Exchange, symbol, 20)
		if err != nil {
			log.Printf("[AI-SCALPING] Failed to fetch orderbook for %s: %v", symbol, err)
		}

		signal := aiMarketSignal{
			Symbol:    normalizeSymbolForComparison(symbol),
			Price:     ticker.GetPrice(),
			High24h:   ticker.GetHigh(),
			Low24h:    ticker.GetLow(),
			Volume24h: ticker.GetVolume(),
		}

		if signal.High24h > 0 && signal.Low24h > 0 {
			signal.PriceChange24h = (signal.Price - signal.Low24h) / (signal.High24h - signal.Low24h) * 100
		}

		if obResp != nil {
			ob := obResp.OrderBook
			if len(ob.Bids) > 0 && len(ob.Asks) > 0 {
				bidVol := sumDecimalOrderVolume(ob.Bids, 5)
				askVol := sumDecimalOrderVolume(ob.Asks, 5)
				total := bidVol + askVol
				if total > 0 {
					signal.OrderBookImbalance = (bidVol - askVol) / total
				}
				bestBid := ob.Bids[0].Price.InexactFloat64()
				bestAsk := ob.Asks[0].Price.InexactFloat64()
				if signal.Price > 0 {
					signal.BidAskSpread = (bestAsk - bestBid) / signal.Price * 100
				}
			}
		}

		signals = append(signals, signal)
	}

	if len(signals) == 0 {
		return nil, fmt.Errorf("no market signals available from exchange")
	}

	return signals, nil
}

func (s *AIScalpingService) getAIDecision(ctx context.Context, signals []aiMarketSignal, portfolio TradingPortfolio) (*AITradingDecision, error) {
	systemPrompt := s.buildSystemPrompt()
	userPrompt := s.buildUserPrompt(ctx, signals, portfolio)

	log.Printf("[AI-SCALPING] Calling LLM with %d signals", len(signals))

	req := &llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: userPrompt},
		},
		Temperature:    floatPtr(0.3),
		MaxTokens:      1000,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}

	resp, err := s.llmClient.Complete(ctx, req)
	if err != nil {
		log.Printf("[AI-SCALPING] LLM completion failed: %v", err)
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	log.Printf("[AI-SCALPING] LLM response received (latency: %dms)", resp.LatencyMs)

	var decision AITradingDecision
	if err := json.Unmarshal([]byte(resp.Message.Content), &decision); err != nil {
		log.Printf("[AI-SCALPING] Failed to parse AI response: %s", resp.Message.Content)
		return nil, fmt.Errorf("failed to parse AI decision: %w", err)
	}

	return &decision, nil
}

func (s *AIScalpingService) buildSystemPrompt() string {
	skillContent := ""
	if s.skillRegistry != nil {
		if sk, found := s.skillRegistry.Get("scalping"); found {
			skillContent = sk.Content
		}
	}

	return fmt.Sprintf(`You are an autonomous AI trading agent for cryptocurrency futures scalping.

## Your Role
You analyze market data and make trading decisions. You have access to real-time market signals and portfolio state.

## Trading Rules
1. Only trade when you have HIGH confidence (>%.1f)
2. Maximum position size: %.1f%% of portfolio
3. Use futures with %dx leverage
4. Always consider risk: set stop-loss and take-profit levels
5. If uncertain, return action: "hold" with reasoning

## Response Format
Return JSON only:
{
  "action": "buy" | "sell" | "hold",
  "symbol": "SYMBOL/USDT",
  "size_pct": 1-100,
  "confidence": 0.0-1.0,
  "reasoning": "explanation",
  "stop_loss": 123.45,
  "take_profit": 130.00
}

## Skill Guidelines
%s

## Signal Interpretation
- ob_imbalance > 0.2: Strong buy pressure (more bids)
- ob_imbalance < -0.2: Strong sell pressure (more asks)
- spread < 0.1%%: Good liquidity for execution
- price_change_24h > 5%%: Strong momentum (consider direction)
`, s.config.MinConfidence, s.config.MaxCapitalPct, s.config.Leverage, skillContent)
}

func (s *AIScalpingService) buildUserPrompt(ctx context.Context, signals []aiMarketSignal, portfolio TradingPortfolio) string {
	signalsJSON, _ := json.MarshalIndent(signals, "", "  ")

	var memoryContext string
	if s.tradeMemory != nil {
		topSymbol := ""
		if len(signals) > 0 {
			topSymbol = signals[0].Symbol
		}
		currentContext := string(signalsJSON)
		if mem, err := s.tradeMemory.BuildMemoryContext(ctx, topSymbol, currentContext); err == nil {
			memoryContext = "\n" + mem
		}
	}

	return fmt.Sprintf(`Analyze these market signals and make a trading decision.

## Portfolio
- USDT Balance: %.2f
- Total Value: %.2f
- Open Positions: %d

## Market Signals
%s%s

Based on the signals and past trading history, what is your trading decision? Learn from past mistakes. Return only valid JSON.`, portfolio.USDTBalance, portfolio.TotalValue, portfolio.OpenPositions, string(signalsJSON), memoryContext)
}

func (s *AIScalpingService) executeDecision(ctx context.Context, decision *AITradingDecision, portfolio TradingPortfolio, maxCapitalPct float64) error {
	if s.orderExecutor == nil {
		return fmt.Errorf("no order executor configured")
	}

	if maxCapitalPct <= 0 {
		maxCapitalPct = s.config.MaxCapitalPct
	}
	if decision.SizePercent > maxCapitalPct {
		decision.SizePercent = maxCapitalPct
	}
	if decision.SizePercent <= 0 || decision.SizePercent > 100 {
		return fmt.Errorf("invalid size_pct %.4f", decision.SizePercent)
	}

	amount := decimal.NewFromFloat(portfolio.USDTBalance * decision.SizePercent / 100)
	if amount.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("computed order amount is non-positive")
	}

	log.Printf("[AI-SCALPING] Executing: %s %s (%s USDT)", decision.Action, decision.Symbol, amount.String())

	orderID, err := s.orderExecutor.PlaceOrder(ctx, s.config.Exchange, decision.Symbol, decision.Action, "market", amount, nil)
	if err != nil {
		return fmt.Errorf("order failed: %w", err)
	}

	log.Printf("[AI-SCALPING] Order placed: %s", orderID)
	return nil
}

func sumDecimalOrderVolume(orders []ccxt.OrderBookEntry, limit int) float64 {
	var total float64
	for i := 0; i < limit && i < len(orders); i++ {
		total += orders[i].Amount.InexactFloat64()
	}
	return total
}

func floatPtr(v float64) *float64 {
	return &v
}

func (s *AIScalpingService) validateDecision(decision *AITradingDecision, signals []aiMarketSignal) error {
	if decision == nil {
		return fmt.Errorf("decision is nil")
	}
	if decision.Action != "buy" && decision.Action != "sell" && decision.Action != "hold" {
		return fmt.Errorf("unsupported action: %s", decision.Action)
	}
	if decision.Confidence < 0 || decision.Confidence > 1 {
		return fmt.Errorf("confidence out of range: %.4f", decision.Confidence)
	}
	if decision.Action == "hold" {
		return nil
	}
	if decision.Symbol == "" {
		return fmt.Errorf("symbol is required for action %s", decision.Action)
	}
	known := make(map[string]aiMarketSignal, len(signals))
	for _, sig := range signals {
		known[normalizeSymbolForComparison(sig.Symbol)] = sig
	}
	resolved, ok := known[normalizeSymbolForComparison(decision.Symbol)]
	if !ok {
		return fmt.Errorf("symbol %s not in current analyzed universe", decision.Symbol)
	}
	decision.Symbol = resolved.Symbol
	if decision.SizePercent <= 0 {
		return fmt.Errorf("size_pct must be > 0")
	}
	// Loss prevention guardrail: require explicit exit levels for live executions.
	if decision.StopLoss == nil || decision.TakeProfit == nil {
		return fmt.Errorf("stop_loss and take_profit are required for non-hold decisions")
	}
	if decision.StopLoss.LessThanOrEqual(decimal.Zero) || decision.TakeProfit.LessThanOrEqual(decimal.Zero) {
		return fmt.Errorf("stop_loss and take_profit must be positive")
	}
	if resolved.Price <= 0 {
		return fmt.Errorf("invalid market price for symbol %s", resolved.Symbol)
	}
	stopLoss := decision.StopLoss.InexactFloat64()
	takeProfit := decision.TakeProfit.InexactFloat64()
	switch decision.Action {
	case "buy":
		if stopLoss >= resolved.Price || takeProfit <= resolved.Price {
			return fmt.Errorf("buy decision requires stop_loss < price < take_profit")
		}
	case "sell":
		if stopLoss <= resolved.Price || takeProfit >= resolved.Price {
			return fmt.Errorf("sell decision requires stop_loss > price > take_profit")
		}
	}
	return nil
}

func normalizeSymbolForComparison(symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return ""
	}
	normalized = strings.ReplaceAll(normalized, "-", "/")
	if idx := strings.Index(normalized, ":"); idx >= 0 {
		normalized = normalized[:idx]
	}
	return normalized
}

func (s *AIScalpingService) dynamicRiskThresholds() (minConfidence float64, maxCapitalPct float64) {
	minConfidence = s.config.MinConfidence
	maxCapitalPct = s.config.MaxCapitalPct

	adjusted := GetScalpingPerformance().GetAdjustedParameters()
	if adjusted.MaxCapitalPercent > 0 && adjusted.MaxCapitalPercent < maxCapitalPct {
		maxCapitalPct = adjusted.MaxCapitalPercent
	}

	perf := GetScalpingPerformance().GetPerformance()
	consecutiveLosses := readIntMetric(perf["consecutive_losses"])
	if consecutiveLosses >= 2 {
		minConfidence += 0.05 * float64(consecutiveLosses-1)
	}
	if minConfidence > 0.95 {
		minConfidence = 0.95
	}

	return minConfidence, maxCapitalPct
}

func readIntMetric(v interface{}) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}
