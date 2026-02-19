package ai

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// AIArbitrageDetector replaces rule-based arbitrage detection with AI
type AIArbitrageDetector struct {
	brain  *AITradingBrain
	config ArbitrageConfig

	// State
	exchanges     []string
	opportunities []*ArbitrageOpportunity
	mu            sync.RWMutex
	logger        *log.Logger

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// ArbitrageConfig holds configuration
type ArbitrageConfig struct {
	Enabled       bool          `json:"enabled"`
	ScanInterval  time.Duration `json:"scan_interval"`
	Symbols       []string      `json:"symbols"`
	Exchanges     []string      `json:"exchanges"`
	MinConfidence float64       `json:"min_confidence"`
	MaxHoldTime   time.Duration `json:"max_hold_time"`
}

// DefaultArbitrageConfig returns default config
func DefaultArbitrageConfig() ArbitrageConfig {
	return ArbitrageConfig{
		Enabled:       true,
		ScanInterval:  30 * time.Second,
		Symbols:       []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"},
		Exchanges:     []string{"binance", "bybit", "okx"},
		MinConfidence: 0.80,
		MaxHoldTime:   5 * time.Minute,
	}
}

// ArbitrageOpportunity represents AI-detected arbitrage
type ArbitrageOpportunity struct {
	ID              string    `json:"id"`
	Symbol          string    `json:"symbol"`
	BuyExchange     string    `json:"buy_exchange"`
	SellExchange    string    `json:"sell_exchange"`
	BuyPrice        float64   `json:"buy_price"`
	SellPrice       float64   `json:"sell_price"`
	SpreadPercent   float64   `json:"spread_percent"`
	PotentialProfit float64   `json:"potential_profit"`
	Confidence      float64   `json:"confidence"`
	Reasoning       string    `json:"reasoning"`
	DetectedAt      time.Time `json:"detected_at"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// NewAIArbitrageDetector creates AI-driven arbitrage detector
func NewAIArbitrageDetector(
	brain *AITradingBrain,
	config ArbitrageConfig,
) *AIArbitrageDetector {
	ctx, cancel := context.WithCancel(context.Background())

	return &AIArbitrageDetector{
		brain:         brain,
		config:        config,
		exchanges:     config.Exchanges,
		opportunities: make([]*ArbitrageOpportunity, 0),
		logger:        log.Default(),
		ctx:           ctx,
		cancel:        cancel,
	}
}

// Start begins arbitrage detection
func (ad *AIArbitrageDetector) Start() error {
	if !ad.config.Enabled {
		ad.logger.Println("[AI Arbitrage] Disabled")
		return nil
	}

	ad.logger.Println("[AI Arbitrage] Starting AI-driven arbitrage detection")

	ad.wg.Add(1)
	go ad.detectionLoop()

	return nil
}

// Stop shuts down arbitrage detector
func (ad *AIArbitrageDetector) Stop() {
	ad.logger.Println("[AI Arbitrage] Stopping")
	ad.cancel()
	ad.wg.Wait()
}

// detectionLoop continuously scans for arbitrage
func (ad *AIArbitrageDetector) detectionLoop() {
	defer ad.wg.Done()

	ticker := time.NewTicker(ad.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ad.ctx.Done():
			return
		case <-ticker.C:
			if err := ad.detectOpportunities(); err != nil {
				ad.logger.Printf("[AI Arbitrage] Error: %v", err)
			}
		}
	}
}

// detectOpportunities scans all symbols for arbitrage
func (ad *AIArbitrageDetector) detectOpportunities() error {
	for _, symbol := range ad.config.Symbols {
		select {
		case <-ad.ctx.Done():
			return nil
		default:
		}

		opps, err := ad.detectForSymbol(symbol)
		if err != nil {
			ad.logger.Printf("[AI Arbitrage] Failed to detect for %s: %v", symbol, err)
			continue
		}

		if len(opps) > 0 {
			ad.mu.Lock()
			ad.opportunities = append(ad.opportunities, opps...)
			ad.mu.Unlock()

			for _, opp := range opps {
				ad.logger.Printf("[AI Arbitrage] %s: Buy %s @ %.2f, Sell %s @ %.2f, Profit: %.2f%% (confidence: %.2f)",
					opp.Symbol, opp.BuyExchange, opp.BuyPrice, opp.SellExchange, opp.SellPrice, opp.SpreadPercent, opp.Confidence)
			}
		}
	}

	// Clean expired opportunities
	ad.cleanExpiredOpportunities()

	return nil
}

// detectForSymbol detects arbitrage for single symbol
func (ad *AIArbitrageDetector) detectForSymbol(symbol string) ([]*ArbitrageOpportunity, error) {
	// Get prices from all exchanges
	prices, err := ad.getPricesFromExchanges(symbol)
	if err != nil {
		return nil, err
	}

	if len(prices) < 2 {
		return nil, nil // Need at least 2 exchanges
	}

	var opportunities []*ArbitrageOpportunity

	// Compare all exchange pairs
	for buyEx, buyPrice := range prices {
		for sellEx, sellPrice := range prices {
			if buyEx == sellEx {
				continue
			}

			spread := ((sellPrice - buyPrice) / buyPrice) * 100

			// Use AI to evaluate opportunity
			req := &ReasoningRequest{
				RequestID: fmt.Sprintf("arb_%s_%s_%s_%d", symbol, buyEx, sellEx, time.Now().Unix()),
				Timestamp: time.Now(),
				Strategy:  "arbitrage",
				MarketState: MarketState{
					Symbol:   symbol,
					Price:    buyPrice,
					Exchange: buyEx,
				},
				Context: fmt.Sprintf("Arbitrage opportunity: Buy on %s at %.2f, Sell on %s at %.2f, Spread: %.2f%%. Evaluate if this is viable considering fees, slippage, and execution risk.",
					buyEx, buyPrice, sellEx, sellPrice, spread),
			}

			resp, err := ad.brain.Reason(ad.ctx, req)
			if err != nil {
				continue
			}

			if resp.Confidence < ad.config.MinConfidence {
				continue
			}

			// Only create opportunity if AI approves
			if resp.Decision.Action == ActionArbitrage || resp.Decision.Action == ActionBuy {
				opportunity := &ArbitrageOpportunity{
					ID:              fmt.Sprintf("arb_%s_%s_%s_%d", symbol, buyEx, sellEx, time.Now().Unix()),
					Symbol:          symbol,
					BuyExchange:     buyEx,
					SellExchange:    sellEx,
					BuyPrice:        buyPrice,
					SellPrice:       sellPrice,
					SpreadPercent:   spread,
					PotentialProfit: spread * 0.5, // Conservative estimate after fees
					Confidence:      resp.Confidence,
					Reasoning:       resp.Reasoning,
					DetectedAt:      time.Now(),
					ExpiresAt:       time.Now().Add(2 * time.Minute),
				}
				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities, nil
}

// getPricesFromExchanges fetches prices from all exchanges
func (ad *AIArbitrageDetector) getPricesFromExchanges(symbol string) (map[string]float64, error) {
	// Placeholder - would fetch from actual exchanges
	prices := make(map[string]float64)

	for _, ex := range ad.exchanges {
		prices[ex] = 0 // Placeholder
	}

	return prices, nil
}

// cleanExpiredOpportunities removes old opportunities
func (ad *AIArbitrageDetector) cleanExpiredOpportunities() {
	ad.mu.Lock()
	defer ad.mu.Unlock()

	var active []*ArbitrageOpportunity
	now := time.Now()

	for _, opp := range ad.opportunities {
		if now.Before(opp.ExpiresAt) {
			active = append(active, opp)
		}
	}

	ad.opportunities = active
}

// GetActiveOpportunities returns current opportunities
func (ad *AIArbitrageDetector) GetActiveOpportunities() []*ArbitrageOpportunity {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	result := make([]*ArbitrageOpportunity, len(ad.opportunities))
	copy(result, ad.opportunities)
	return result
}

// GetBestOpportunity returns best opportunity for symbol
func (ad *AIArbitrageDetector) GetBestOpportunity(symbol string) *ArbitrageOpportunity {
	ad.mu.RLock()
	defer ad.mu.RUnlock()

	var best *ArbitrageOpportunity
	for _, opp := range ad.opportunities {
		if opp.Symbol == symbol && time.Now().Before(opp.ExpiresAt) {
			if best == nil || opp.SpreadPercent > best.SpreadPercent {
				best = opp
			}
		}
	}

	return best
}
