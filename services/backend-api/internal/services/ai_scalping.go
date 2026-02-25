package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/ai/llm"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/skill"
	"github.com/shopspring/decimal"
)

type AIScalpingConfig struct {
	Exchange          string
	Model             string
	Leverage          int
	MaxTokens         int
	MaxCapitalPct     float64
	MinConfidence     float64
	MaxIterations     int
	Timeout           time.Duration
	AutoExecute       bool
	AllowSpotFallback bool
	MaxPairsToAnalyze int
	MaxCandidatePairs int
	OrderBookPairs    int
}

func DefaultAIScalpingConfig() AIScalpingConfig {
	return AIScalpingConfig{
		Exchange:          "bitget", // Default, will be overridden by user settings
		Model:             "glm-5",
		Leverage:          5,
		MaxTokens:         1200,
		MaxCapitalPct:     5.0,
		MinConfidence:     0.45,
		MaxIterations:     3,
		Timeout:           90 * time.Second,
		AutoExecute:       true,
		AllowSpotFallback: false,
		MaxPairsToAnalyze: 8,
		MaxCandidatePairs: 120,
		OrderBookPairs:    4,
	}
}

func ResolveAIScalpingConfigFromEnv(base AIScalpingConfig) AIScalpingConfig {
	cfg := applyAIScalpingConfigFromFile(base)

	if value := strings.TrimSpace(os.Getenv("NEURATRADE_SCALPING_EXCHANGE")); value != "" {
		cfg.Exchange = strings.ToLower(value)
	}
	if value := strings.TrimSpace(os.Getenv("NEURATRADE_SCALPING_MODEL")); value != "" {
		cfg.Model = value
	}
	if value := getEnvInt("NEURATRADE_SCALPING_LEVERAGE"); value > 0 {
		cfg.Leverage = clampInt(value, 1, 50)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_MAX_TOKENS"); value > 0 {
		cfg.MaxTokens = clampInt(value, 128, 8192)
	}
	if value, ok := getEnvFloat("NEURATRADE_SCALPING_MAX_CAPITAL_PCT"); ok {
		cfg.MaxCapitalPct = clampFloat(value, 0.1, 100)
	}
	if value, ok := getEnvFloat("NEURATRADE_SCALPING_MIN_CONFIDENCE"); ok {
		cfg.MinConfidence = clampFloat(value, 0.05, 0.99)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_MAX_ITERATIONS"); value > 0 {
		cfg.MaxIterations = clampInt(value, 1, 20)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_TIMEOUT_SECONDS"); value > 0 {
		cfg.Timeout = time.Duration(clampInt(value, 10, 600)) * time.Second
	}
	if value, ok := getEnvBool("NEURATRADE_SCALPING_AUTO_EXECUTE"); ok {
		cfg.AutoExecute = value
	}
	if value, ok := getEnvBool("NEURATRADE_SCALPING_ALLOW_SPOT_FALLBACK"); ok {
		cfg.AllowSpotFallback = value
	}
	if value := getEnvInt("NEURATRADE_SCALPING_MAX_PAIRS"); value > 0 {
		cfg.MaxPairsToAnalyze = clampInt(value, 1, 64)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_MAX_CANDIDATES"); value > 0 {
		cfg.MaxCandidatePairs = clampInt(value, cfg.MaxPairsToAnalyze, 2000)
	}
	if value := getEnvInt("NEURATRADE_SCALPING_ORDERBOOK_PAIRS"); value > 0 {
		cfg.OrderBookPairs = clampInt(value, 1, cfg.MaxPairsToAnalyze)
	}

	if cfg.MaxCandidatePairs < cfg.MaxPairsToAnalyze {
		cfg.MaxCandidatePairs = cfg.MaxPairsToAnalyze
	}
	if cfg.OrderBookPairs > cfg.MaxPairsToAnalyze {
		cfg.OrderBookPairs = cfg.MaxPairsToAnalyze
	}
	if cfg.OrderBookPairs <= 0 {
		cfg.OrderBookPairs = 1
	}

	log.Printf(
		"[AI-SCALPING] Runtime config: exchange=%s model=%s leverage=%d max_tokens=%d max_capital_pct=%.2f min_confidence=%.2f timeout=%s auto_execute=%t allow_spot_fallback=%t max_pairs=%d max_candidates=%d orderbook_pairs=%d",
		cfg.Exchange,
		cfg.Model,
		cfg.Leverage,
		cfg.MaxTokens,
		cfg.MaxCapitalPct,
		cfg.MinConfidence,
		cfg.Timeout,
		cfg.AutoExecute,
		cfg.AllowSpotFallback,
		cfg.MaxPairsToAnalyze,
		cfg.MaxCandidatePairs,
		cfg.OrderBookPairs,
	)

	return cfg
}

type aiScalpingFileConfig struct {
	AI struct {
		MinConfidence *float64 `json:"min_confidence"`
		Scalping      struct {
			Exchange          string   `json:"exchange"`
			Model             string   `json:"model"`
			Leverage          *int     `json:"leverage"`
			MaxTokens         *int     `json:"max_tokens"`
			MaxCapitalPct     *float64 `json:"max_capital_pct"`
			MinConfidence     *float64 `json:"min_confidence"`
			MaxIterations     *int     `json:"max_iterations"`
			TimeoutSeconds    *int     `json:"timeout_seconds"`
			AutoExecute       *bool    `json:"auto_execute"`
			AllowSpotFallback *bool    `json:"allow_spot_fallback"`
			MaxPairs          *int     `json:"max_pairs"`
			MaxCandidates     *int     `json:"max_candidates"`
			OrderBookPairs    *int     `json:"orderbook_pairs"`
		} `json:"scalping"`
	} `json:"ai"`
}

func applyAIScalpingConfigFromFile(base AIScalpingConfig) AIScalpingConfig {
	cfg := base

	configPath := strings.TrimSpace(os.Getenv("NEURATRADE_HOME"))
	if configPath != "" {
		configPath = filepath.Join(configPath, "config.json")
	} else {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return cfg
		}
		configPath = filepath.Join(homeDir, ".neuratrade", "config.json")
	}
	content, err := os.ReadFile(configPath)
	if err != nil {
		return cfg
	}

	var fileConfig aiScalpingFileConfig
	if err := json.Unmarshal(content, &fileConfig); err != nil {
		log.Printf("[AI-SCALPING] Failed to parse %s: %v", configPath, err)
		return cfg
	}

	if fileConfig.AI.MinConfidence != nil {
		cfg.MinConfidence = clampFloat(*fileConfig.AI.MinConfidence, 0.05, 0.99)
	}
	if value := strings.TrimSpace(fileConfig.AI.Scalping.Exchange); value != "" {
		cfg.Exchange = strings.ToLower(value)
	}
	if value := strings.TrimSpace(fileConfig.AI.Scalping.Model); value != "" {
		cfg.Model = value
	}
	if fileConfig.AI.Scalping.Leverage != nil {
		cfg.Leverage = clampInt(*fileConfig.AI.Scalping.Leverage, 1, 50)
	}
	if fileConfig.AI.Scalping.MaxTokens != nil {
		cfg.MaxTokens = clampInt(*fileConfig.AI.Scalping.MaxTokens, 128, 8192)
	}
	if fileConfig.AI.Scalping.MaxCapitalPct != nil {
		cfg.MaxCapitalPct = clampFloat(*fileConfig.AI.Scalping.MaxCapitalPct, 0.1, 100)
	}
	if fileConfig.AI.Scalping.MinConfidence != nil {
		cfg.MinConfidence = clampFloat(*fileConfig.AI.Scalping.MinConfidence, 0.05, 0.99)
	}
	if fileConfig.AI.Scalping.MaxIterations != nil {
		cfg.MaxIterations = clampInt(*fileConfig.AI.Scalping.MaxIterations, 1, 20)
	}
	if fileConfig.AI.Scalping.TimeoutSeconds != nil {
		cfg.Timeout = time.Duration(clampInt(*fileConfig.AI.Scalping.TimeoutSeconds, 10, 600)) * time.Second
	}
	if fileConfig.AI.Scalping.AutoExecute != nil {
		cfg.AutoExecute = *fileConfig.AI.Scalping.AutoExecute
	}
	if fileConfig.AI.Scalping.AllowSpotFallback != nil {
		cfg.AllowSpotFallback = *fileConfig.AI.Scalping.AllowSpotFallback
	}
	if fileConfig.AI.Scalping.MaxPairs != nil {
		cfg.MaxPairsToAnalyze = clampInt(*fileConfig.AI.Scalping.MaxPairs, 1, 64)
	}
	if fileConfig.AI.Scalping.MaxCandidates != nil {
		cfg.MaxCandidatePairs = clampInt(*fileConfig.AI.Scalping.MaxCandidates, cfg.MaxPairsToAnalyze, 2000)
	}
	if fileConfig.AI.Scalping.OrderBookPairs != nil {
		cfg.OrderBookPairs = clampInt(*fileConfig.AI.Scalping.OrderBookPairs, 1, cfg.MaxPairsToAnalyze)
	}

	return cfg
}

type AITradingDecision struct {
	Action      string           `json:"action"`
	Symbol      string           `json:"symbol"`
	SizePercent float64          `json:"size_pct"`
	Confidence  float64          `json:"confidence"`
	Reasoning   string           `json:"reasoning"`
	StopLoss    *decimal.Decimal `json:"stop_loss,omitempty"`
	TakeProfit  *decimal.Decimal `json:"take_profit,omitempty"`
	OrderID     string           `json:"order_id,omitempty"`
	EntryPrice  *decimal.Decimal `json:"-"`
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
	pairCacheMu   sync.RWMutex
	cachedPairs   []string
	cacheExchange string
	cacheUpdated  time.Time
}

// SetExchange updates the exchange for scalping (called dynamically based on user wallet)
func (s *AIScalpingService) SetExchange(exchange string) {
	s.config.Exchange = exchange
	log.Printf("[AI-SCALPING] Exchange set to: %s", exchange)
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
		return fallbackHoldDecision(err.Error(), err), nil
	}

	decision.Action = strings.ToLower(strings.TrimSpace(decision.Action))
	decision.Symbol = normalizeSymbolForComparison(decision.Symbol)

	log.Printf("[AI-SCALPING] AI decision: %s %s (confidence: %.2f)", decision.Action, decision.Symbol, decision.Confidence)

	if err := s.validateDecision(decision, signals); err != nil {
		return fallbackHoldDecision(err.Error(), err), nil
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
			if shouldDowngradeExecutionErrorToHold(err) {
				decision.Action = "hold"
				decision.Confidence = 0
				decision.OrderID = ""
				decision.Reasoning = buildExecutionFallbackReason(err)
				log.Printf("[AI-SCALPING] Downgrading execution issue to HOLD: %v", err)
				return decision, nil
			}
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
	if cached := s.getCachedPairs(); len(cached) > 0 {
		return cached, nil
	}

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

	// The executor is futures-first. Prefer symbols that are present in funding-rate
	// universe so trade candidates map to futures contracts.
	if s.config.Exchange == "bitget" {
		if filtered := s.filterFuturesSymbols(ctx, candidates); len(filtered) > 0 {
			candidates = filtered
		}
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
		selected := candidates[:limit]
		s.updatePairCache(selected)
		return selected, nil
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
	s.updatePairCache(selected)
	return selected, nil
}

func (s *AIScalpingService) getCachedPairs() []string {
	s.pairCacheMu.RLock()
	defer s.pairCacheMu.RUnlock()

	if len(s.cachedPairs) == 0 {
		return nil
	}
	if s.cacheExchange != s.config.Exchange {
		return nil
	}
	if time.Since(s.cacheUpdated) > 2*time.Minute {
		return nil
	}

	result := make([]string, len(s.cachedPairs))
	copy(result, s.cachedPairs)
	return result
}

func (s *AIScalpingService) updatePairCache(pairs []string) {
	if len(pairs) == 0 {
		return
	}
	s.pairCacheMu.Lock()
	defer s.pairCacheMu.Unlock()

	s.cachedPairs = append(s.cachedPairs[:0], pairs...)
	s.cacheExchange = s.config.Exchange
	s.cacheUpdated = time.Now()
}

func (s *AIScalpingService) filterFuturesSymbols(ctx context.Context, symbols []string) []string {
	rates, err := s.ccxtService.FetchAllFundingRates(ctx, s.config.Exchange)
	if err != nil || len(rates) == 0 {
		if err != nil {
			log.Printf("[AI-SCALPING] Futures universe unavailable on %s: %v", s.config.Exchange, err)
		}
		return nil
	}

	allowed := make(map[string]struct{}, len(rates)*2)
	for _, rate := range rates {
		normalized := normalizeFuturesSymbol(rate.Symbol)
		if normalized == "" {
			continue
		}
		allowed[normalized] = struct{}{}
		allowed[strings.ReplaceAll(normalized, "/", "")] = struct{}{}
	}

	filtered := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		normalized := normalizeSymbolForComparison(symbol)
		if normalized == "" {
			continue
		}
		if _, ok := allowed[normalized]; ok {
			filtered = append(filtered, symbol)
			continue
		}
		if _, ok := allowed[strings.ReplaceAll(normalized, "/", "")]; ok {
			filtered = append(filtered, symbol)
		}
	}

	if len(filtered) == 0 {
		log.Printf("[AI-SCALPING] Futures universe filter returned no overlap on %s; using discovered pairs", s.config.Exchange)
		return nil
	}

	log.Printf("[AI-SCALPING] Futures universe filtered %d -> %d symbols on %s", len(symbols), len(filtered), s.config.Exchange)
	return filtered
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

	// Bulk ticker fetch keeps the cycle responsive under high symbol counts.
	tickerBySymbol := make(map[string]ccxt.MarketPriceInterface, len(pairs))
	if marketData, bulkErr := s.ccxtService.FetchMarketData(ctx, []string{s.config.Exchange}, pairs); bulkErr == nil {
		for _, t := range marketData {
			if t == nil {
				continue
			}
			key := normalizeSymbolForComparison(t.GetSymbol())
			if key == "" {
				continue
			}
			tickerBySymbol[key] = t
		}
	} else {
		log.Printf("[AI-SCALPING] Bulk ticker fetch unavailable: %v", bulkErr)
	}

	orderBookPairs := s.config.OrderBookPairs
	if orderBookPairs <= 0 {
		orderBookPairs = 4
	}

	log.Printf("[AI-SCALPING] Analyzing %d pairs on %s", len(pairs), s.config.Exchange)
	for idx, symbol := range pairs {
		normalizedSymbol := normalizeSymbolForComparison(symbol)
		tickerData, ok := tickerBySymbol[normalizedSymbol]
		if !ok {
			tickerData, err = s.ccxtService.FetchSingleTicker(ctx, s.config.Exchange, symbol)
			if err != nil {
				log.Printf("[AI-SCALPING] Failed to fetch ticker for %s: %v", symbol, err)
				continue
			}
		}

		var obResp *ccxt.OrderBookResponse
		if idx < orderBookPairs {
			obResp, err = s.ccxtService.FetchOrderBook(ctx, s.config.Exchange, symbol, 20)
			if err != nil {
				log.Printf("[AI-SCALPING] Failed to fetch orderbook for %s: %v", symbol, err)
			}
		}

		signal := aiMarketSignal{
			Symbol:    normalizeSymbolForComparison(tickerData.GetSymbol()),
			Price:     tickerData.GetPrice(),
			High24h:   tickerData.GetHigh(),
			Low24h:    tickerData.GetLow(),
			Volume24h: tickerData.GetVolume(),
		}
		if signal.Symbol == "" {
			signal.Symbol = normalizedSymbol
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
	log.Printf("[AI-SCALPING] === SYSTEM PROMPT ===\n%s", systemPrompt)
	log.Printf("[AI-SCALPING] === USER PROMPT ===\nPortfolio: %.2f USDT, Signals: %d", portfolio.USDTBalance, len(signals))

	req := &llm.CompletionRequest{
		Model: strings.TrimSpace(s.config.Model),
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: systemPrompt},
			{Role: llm.RoleUser, Content: userPrompt},
		},
		Temperature:    floatPtr(0.3),
		MaxTokens:      s.config.MaxTokens,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}
	if req.Model == "" {
		req.Model = "glm-5"
	}
	if req.MaxTokens <= 0 {
		req.MaxTokens = 1200
	}

	log.Printf("[AI-SCALPING] Sending LLM request...")

	resp, err := s.llmClient.Complete(ctx, req)
	if err != nil {
		log.Printf("[AI-SCALPING] LLM completion failed: %v", err)
		return nil, fmt.Errorf("LLM completion failed: %w", err)
	}

	log.Printf("[AI-SCALPING] === LLM RESPONSE ===\nLatency: %dms\nRaw: %s", resp.LatencyMs, resp.Message.Content)

	decision, err := parseAIDecisionPayload(resp.Message.Content)
	if err != nil {
		log.Printf("[AI-SCALPING] Failed to parse AI response: %s", resp.Message.Content)
		repaired, repairErr := s.repairDecisionJSON(ctx, resp.Message.Content)
		if repairErr != nil {
			log.Printf("[AI-SCALPING] Failed to repair AI response: %v", repairErr)
			return fallbackHoldDecision(resp.Message.Content, err), nil
		}
		decision, err = parseAIDecisionPayload(repaired)
		if err != nil {
			log.Printf("[AI-SCALPING] Failed to parse repaired AI response: %s", repaired)
			return fallbackHoldDecision(repaired, err), nil
		}
	}

	stopLossFloat := 0.0
	takeProfitFloat := 0.0
	if decision.StopLoss != nil {
		stopLossFloat = decision.StopLoss.InexactFloat64()
	}
	if decision.TakeProfit != nil {
		takeProfitFloat = decision.TakeProfit.InexactFloat64()
	}
	log.Printf("[AI-SCALPING] === AI DECISION PARSED ===\nAction: %s, Symbol: %s, Confidence: %.0f%%, Size: %.1f%%, SL: %.4f, TP: %.4f\nReasoning: %s",
		decision.Action, decision.Symbol, decision.Confidence*100, decision.SizePercent,
		stopLossFloat, takeProfitFloat, decision.Reasoning)

	return decision, nil
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

	// Build detailed trade info for rich notification
	details := TradeDetails{
		Exchange:          s.config.Exchange,
		Symbol:            decision.Symbol,
		Side:              decision.Action,
		OrderType:         "market",
		MarketType:        "futures",
		AllowSpotFallback: s.config.AllowSpotFallback,
		Leverage:          s.config.Leverage,
		AmountUSDT:        amount,
		WalletPercent:     decision.SizePercent,
		TakeProfit:        decision.TakeProfit,
		StopLoss:          decision.StopLoss,
		TradeType:         "scalping",
		Confidence:        decision.Confidence,
		Reasoning:         decision.Reasoning,
		EntryPrice:        decision.EntryPrice,
		IsPaperTrade:      false, // Real trading mode
	}

	// Use PlaceOrderWithDetails for rich notifications
	orderID, err := s.orderExecutor.PlaceOrderWithDetails(ctx, details)
	if err != nil {
		return fmt.Errorf("order failed: %w", err)
	}

	decision.OrderID = strings.TrimSpace(orderID)
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
	resolved, ok := resolveDecisionSymbol(decision.Symbol, known)
	if !ok {
		return fmt.Errorf("symbol %s not in current analyzed universe", decision.Symbol)
	}
	decision.Symbol = resolved.Symbol
	if resolved.Price > 0 {
		entry := decimal.NewFromFloat(resolved.Price)
		decision.EntryPrice = &entry
	}
	if decision.SizePercent <= 0 {
		return fmt.Errorf("size_pct must be > 0")
	}
	if decision.StopLoss == nil || decision.TakeProfit == nil {
		defaultSL, defaultTP := defaultExitLevels(resolved.Price, decision.Action)
		if decision.StopLoss == nil {
			decision.StopLoss = &defaultSL
		}
		if decision.TakeProfit == nil {
			decision.TakeProfit = &defaultTP
		}
		log.Printf("[AI-SCALPING] Applied default SL/TP for %s %s due to incomplete model output", decision.Action, decision.Symbol)
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

func normalizeFuturesSymbol(symbol string) string {
	normalized := strings.ToUpper(strings.TrimSpace(symbol))
	if normalized == "" {
		return ""
	}
	if idx := strings.Index(normalized, ":"); idx >= 0 {
		normalized = normalized[:idx]
	}
	normalized = strings.ReplaceAll(normalized, "-", "")
	normalized = strings.ReplaceAll(normalized, "_", "")
	normalized = strings.ReplaceAll(normalized, "/", "")
	if strings.HasSuffix(normalized, "USDT") && len(normalized) > len("USDT") {
		base := normalized[:len(normalized)-len("USDT")]
		return base + "/USDT"
	}
	return normalized
}

func resolveDecisionSymbol(raw string, known map[string]aiMarketSignal) (aiMarketSignal, bool) {
	normalized := normalizeSymbolForComparison(raw)
	if sig, ok := known[normalized]; ok {
		return sig, true
	}

	compact := strings.ReplaceAll(normalized, "/", "")
	compact = strings.TrimSuffix(compact, "USDT")
	compact = strings.TrimSpace(compact)
	if compact == "" {
		return aiMarketSignal{}, false
	}

	matches := make([]aiMarketSignal, 0, 1)
	for key, sig := range known {
		base := strings.TrimSuffix(strings.ReplaceAll(key, "/", ""), "USDT")
		if strings.HasPrefix(base, compact) || strings.HasPrefix(compact, base) {
			matches = append(matches, sig)
		}
	}

	if len(matches) == 1 {
		return matches[0], true
	}
	return aiMarketSignal{}, false
}

func parseAIDecisionPayload(content string) (*AITradingDecision, error) {
	var payload struct {
		Action      string          `json:"action"`
		Symbol      string          `json:"symbol"`
		SizePercent float64         `json:"size_pct"`
		Confidence  float64         `json:"confidence"`
		Reasoning   string          `json:"reasoning"`
		StopLoss    json.RawMessage `json:"stop_loss"`
		TakeProfit  json.RawMessage `json:"take_profit"`
	}

	raw := strings.TrimSpace(content)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		start := strings.Index(raw, "{")
		end := strings.LastIndex(raw, "}")
		if start >= 0 && end > start {
			extracted := raw[start : end+1]
			if extractErr := json.Unmarshal([]byte(extracted), &payload); extractErr != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}

	stopLoss, err := parseOptionalDecimal(payload.StopLoss)
	if err != nil {
		return nil, fmt.Errorf("invalid stop_loss: %w", err)
	}
	takeProfit, err := parseOptionalDecimal(payload.TakeProfit)
	if err != nil {
		return nil, fmt.Errorf("invalid take_profit: %w", err)
	}

	return &AITradingDecision{
		Action:      payload.Action,
		Symbol:      payload.Symbol,
		SizePercent: payload.SizePercent,
		Confidence:  payload.Confidence,
		Reasoning:   payload.Reasoning,
		StopLoss:    stopLoss,
		TakeProfit:  takeProfit,
	}, nil
}

func fallbackHoldDecision(content string, parseErr error) *AITradingDecision {
	sanitized := strings.Join(strings.Fields(strings.TrimSpace(content)), " ")
	if len(sanitized) > 180 {
		sanitized = sanitized[:177] + "..."
	}
	if sanitized == "" {
		sanitized = "model response was empty"
	}

	reason := fmt.Sprintf("model response parse fallback: %s", sanitized)
	if parseErr != nil {
		reason = fmt.Sprintf("model response parse fallback (%v): %s", parseErr, sanitized)
	}

	return &AITradingDecision{
		Action:      "hold",
		Confidence:  0,
		Reasoning:   reason,
		SizePercent: 0,
	}
}

func shouldDowngradeExecutionErrorToHold(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "futures-only mode prevented spot fallback") ||
		strings.Contains(msg, "parameter") && strings.Contains(msg, "does not exist") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "request failed") ||
		strings.Contains(msg, "failed to get ticker")
}

func buildExecutionFallbackReason(err error) string {
	if err == nil {
		return "execution unavailable, held this cycle"
	}
	sanitized := strings.Join(strings.Fields(err.Error()), " ")
	if len(sanitized) > 220 {
		sanitized = sanitized[:217] + "..."
	}
	return fmt.Sprintf("execution unavailable, held this cycle: %s", sanitized)
}

func (s *AIScalpingService) repairDecisionJSON(ctx context.Context, raw string) (string, error) {
	modelID := strings.TrimSpace(s.config.Model)
	if modelID == "" {
		modelID = "glm-5"
	}

	maxTokens := 320
	if s.config.MaxTokens > 0 && s.config.MaxTokens < maxTokens {
		maxTokens = s.config.MaxTokens
	}

	repairCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req := &llm.CompletionRequest{
		Model: modelID,
		Messages: []llm.Message{
			{
				Role: llm.RoleSystem,
				Content: `Convert the provided trading analysis into strict JSON only.
Schema:
{
  "action": "buy" | "sell" | "hold",
  "symbol": "SYMBOL/USDT",
  "size_pct": number,
  "confidence": number,
  "reasoning": "text",
  "stop_loss": number|null,
  "take_profit": number|null
}
Do not include markdown or extra text.`,
			},
			{
				Role:    llm.RoleUser,
				Content: raw,
			},
		},
		Temperature:    floatPtr(0),
		MaxTokens:      maxTokens,
		ResponseFormat: &llm.ResponseFormat{Type: "json_object"},
	}

	resp, err := s.llmClient.Complete(repairCtx, req)
	if err != nil {
		return "", err
	}

	content := strings.TrimSpace(resp.Message.Content)
	if content == "" {
		return "", fmt.Errorf("empty repair response")
	}
	log.Printf("[AI-SCALPING] Repaired non-JSON decision payload")
	return content, nil
}

func parseOptionalDecimal(raw json.RawMessage) (*decimal.Decimal, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	// Handle numeric JSON values first.
	var numeric float64
	if err := json.Unmarshal(raw, &numeric); err == nil {
		dec := decimal.NewFromFloat(numeric)
		return &dec, nil
	}

	// Handle quoted values (including empty string placeholders).
	var text string
	if err := json.Unmarshal(raw, &text); err == nil {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return nil, nil
		}
		dec, err := decimal.NewFromString(trimmed)
		if err != nil {
			return nil, err
		}
		return &dec, nil
	}

	return nil, fmt.Errorf("unsupported value %s", string(raw))
}

func defaultExitLevels(price float64, action string) (decimal.Decimal, decimal.Decimal) {
	entry := decimal.NewFromFloat(price)
	stopPct := decimal.NewFromFloat(0.008) // 0.8%
	takePct := decimal.NewFromFloat(0.012) // 1.2%

	switch strings.ToLower(strings.TrimSpace(action)) {
	case "sell":
		stopLoss := entry.Mul(decimal.NewFromInt(1).Add(stopPct))
		takeProfit := entry.Mul(decimal.NewFromInt(1).Sub(takePct))
		return stopLoss, takeProfit
	default:
		stopLoss := entry.Mul(decimal.NewFromInt(1).Sub(stopPct))
		takeProfit := entry.Mul(decimal.NewFromInt(1).Add(takePct))
		return stopLoss, takeProfit
	}
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

func getEnvInt(key string) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		log.Printf("[AI-SCALPING] Invalid integer %s=%q", key, raw)
		return 0
	}
	return value
}

func getEnvFloat(key string) (float64, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return 0, false
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		log.Printf("[AI-SCALPING] Invalid float %s=%q", key, raw)
		return 0, false
	}
	return value, true
}

func getEnvBool(key string) (bool, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return false, false
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		log.Printf("[AI-SCALPING] Invalid boolean %s=%q", key, raw)
		return false, false
	}
}

func clampInt(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

func clampFloat(value, min, max float64) float64 {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}
