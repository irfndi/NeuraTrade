package ai

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

// AISignalProcessor replaces rule-based signal processing with AI-driven decisions
type AISignalProcessor struct {
	brain  *AITradingBrain
	config SignalProcessorConfig

	// State
	symbols     []string
	lastSignals map[string]*Signal
	mu          sync.RWMutex
	logger      *log.Logger

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// SignalProcessorConfig holds configuration
type SignalProcessorConfig struct {
	Enabled         bool          `json:"enabled"`
	ScanInterval    time.Duration `json:"scan_interval"`
	Symbols         []string      `json:"symbols"`
	MinConfidence   float64       `json:"min_confidence"`
	DefaultExchange string        `json:"default_exchange"`
}

// DefaultSignalProcessorConfig returns default config
func DefaultSignalProcessorConfig() SignalProcessorConfig {
	return SignalProcessorConfig{
		Enabled:         true,
		ScanInterval:    30 * time.Second,
		Symbols:         []string{"BTC/USDT", "ETH/USDT", "SOL/USDT"},
		MinConfidence:   0.75,
		DefaultExchange: "binance",
	}
}

// Signal represents an AI-generated trading signal
type Signal struct {
	ID          string    `json:"id"`
	Symbol      string    `json:"symbol"`
	Exchange    string    `json:"exchange"`
	Action      string    `json:"action"` // buy, sell, hold, strong_buy, strong_sell
	Confidence  float64   `json:"confidence"`
	Reasoning   string    `json:"reasoning"`
	Timeframe   string    `json:"timeframe"`
	GeneratedAt time.Time `json:"generated_at"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// NewAISignalProcessor creates AI-driven signal processor
func NewAISignalProcessor(
	brain *AITradingBrain,
	config SignalProcessorConfig,
) *AISignalProcessor {
	ctx, cancel := context.WithCancel(context.Background())

	return &AISignalProcessor{
		brain:       brain,
		config:      config,
		symbols:     config.Symbols,
		lastSignals: make(map[string]*Signal),
		logger:      log.Default(),
		ctx:         ctx,
		cancel:      cancel,
	}
}

// Start begins signal processing
func (sp *AISignalProcessor) Start() error {
	if !sp.config.Enabled {
		sp.logger.Println("[AI Signal Processor] Disabled")
		return nil
	}

	sp.logger.Println("[AI Signal Processor] Starting AI-driven signal generation")

	sp.wg.Add(1)
	go sp.processingLoop()

	return nil
}

// Stop shuts down signal processor
func (sp *AISignalProcessor) Stop() {
	sp.logger.Println("[AI Signal Processor] Stopping")
	sp.cancel()
	sp.wg.Wait()
}

// processingLoop continuously generates signals
func (sp *AISignalProcessor) processingLoop() {
	defer sp.wg.Done()

	ticker := time.NewTicker(sp.config.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sp.ctx.Done():
			return
		case <-ticker.C:
			if err := sp.generateSignals(); err != nil {
				sp.logger.Printf("[AI Signal Processor] Error: %v", err)
			}
		}
	}
}

// generateSignals generates AI-driven signals for all symbols
func (sp *AISignalProcessor) generateSignals() error {
	for _, symbol := range sp.symbols {
		select {
		case <-sp.ctx.Done():
			return nil
		default:
		}

		signal, err := sp.generateSignalForSymbol(symbol)
		if err != nil {
			sp.logger.Printf("[AI Signal Processor] Failed to generate signal for %s: %v", symbol, err)
			continue
		}

		if signal != nil {
			sp.mu.Lock()
			sp.lastSignals[symbol] = signal
			sp.mu.Unlock()

			sp.logger.Printf("[AI Signal Processor] %s: %s (confidence: %.2f) - %s",
				symbol, signal.Action, signal.Confidence, signal.Reasoning)
		}
	}

	return nil
}

// generateSignalForSymbol generates AI signal for single symbol
func (sp *AISignalProcessor) generateSignalForSymbol(symbol string) (*Signal, error) {
	// Get market state
	marketState, err := sp.getMarketState(symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get market state: %w", err)
	}

	// Get portfolio state (simplified)
	portfolioState := &PortfolioState{
		Balance:        1000,
		AvailableFunds: 1000,
	}

	// Create reasoning request
	req := &ReasoningRequest{
		RequestID:      fmt.Sprintf("signal_%s_%d", symbol, time.Now().Unix()),
		Timestamp:      time.Now(),
		Strategy:       "signal_generation",
		MarketState:    *marketState,
		PortfolioState: *portfolioState,
		Context:        fmt.Sprintf("Generate trading signal for %s. Analyze market conditions and provide directional bias with confidence.", symbol),
	}

	// Get AI decision
	resp, err := sp.brain.Reason(sp.ctx, req)
	if err != nil {
		return nil, fmt.Errorf("AI reasoning failed: %w", err)
	}

	// Filter by confidence
	if resp.Confidence < sp.config.MinConfidence {
		return nil, nil // Signal below threshold
	}

	// Map action to signal
	action := sp.mapActionToSignal(resp.Decision.Action)
	if action == "hold" {
		return nil, nil // Don't generate hold signals
	}

	signal := &Signal{
		ID:          fmt.Sprintf("sig_%s_%d", symbol, time.Now().Unix()),
		Symbol:      symbol,
		Exchange:    sp.config.DefaultExchange,
		Action:      action,
		Confidence:  resp.Confidence,
		Reasoning:   resp.Reasoning,
		Timeframe:   "short_term",
		GeneratedAt: time.Now(),
		ExpiresAt:   time.Now().Add(5 * time.Minute),
	}

	return signal, nil
}

// mapActionToSignal maps trading action to signal type
func (sp *AISignalProcessor) mapActionToSignal(action TradingAction) string {
	switch action {
	case ActionBuy:
		return "buy"
	case ActionSell:
		return "sell"
	case ActionScalp:
		if rand.Float64() > 0.5 {
			return "strong_buy"
		}
		return "buy"
	default:
		return "hold"
	}
}

// GetLatestSignal returns latest signal for symbol
func (sp *AISignalProcessor) GetLatestSignal(symbol string) *Signal {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	signal, ok := sp.lastSignals[symbol]
	if !ok {
		return nil
	}

	// Check if expired
	if time.Now().After(signal.ExpiresAt) {
		return nil
	}

	return signal
}

// GetAllSignals returns all current signals
func (sp *AISignalProcessor) GetAllSignals() []*Signal {
	sp.mu.RLock()
	defer sp.mu.RUnlock()

	var signals []*Signal
	for _, signal := range sp.lastSignals {
		if time.Now().Before(signal.ExpiresAt) {
			signals = append(signals, signal)
		}
	}

	return signals
}

// getMarketState retrieves market data
func (sp *AISignalProcessor) getMarketState(symbol string) (*MarketState, error) {
	return &MarketState{
		Symbol:    symbol,
		Exchange:  sp.config.DefaultExchange,
		Price:     0,
		Timestamp: time.Now(),
	}, nil
}
