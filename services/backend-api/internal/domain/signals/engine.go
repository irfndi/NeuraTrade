package signals

import "github.com/shopspring/decimal"

// Engine evaluates tick windows and proposes signals.
type Engine struct {
	cfg Config
}

// NewEngine creates a new signal engine.
func NewEngine(cfg Config) *Engine {
	if cfg.Lookback < 2 {
		cfg.Lookback = DefaultConfig().Lookback
	}
	if !cfg.MinChange.IsPositive() {
		cfg.MinChange = DefaultConfig().MinChange
	}
	return &Engine{cfg: cfg}
}

// Evaluate proposes a signal when a deterministic momentum threshold is crossed.
func (e *Engine) Evaluate(strategyID string, ticks []Tick) (ProposedSignal, bool) {
	if len(ticks) < e.cfg.Lookback {
		return ProposedSignal{}, false
	}
	if strategyID == "" {
		strategyID = "default"
	}

	window := ticks[len(ticks)-e.cfg.Lookback:]
	first := window[0].Last
	last := window[len(window)-1].Last
	if !first.IsPositive() || !last.IsPositive() {
		return ProposedSignal{}, false
	}

	changeRatio := last.Sub(first).Div(first)
	absChange := changeRatio.Abs()
	if absChange.LessThan(e.cfg.MinChange) {
		return ProposedSignal{}, false
	}

	side := SideBuy
	if changeRatio.IsNegative() {
		side = SideSell
	}

	confidence := absChange.Div(e.cfg.MinChange)
	if confidence.GreaterThan(decimal.NewFromInt(1)) {
		confidence = decimal.NewFromInt(1)
	}

	return ProposedSignal{
		StrategyID: strategyID,
		Symbol:     window[len(window)-1].Symbol,
		Side:       side,
		Confidence: confidence,
		Metadata: map[string]string{
			"lookback":     decimal.NewFromInt(int64(e.cfg.Lookback)).String(),
			"change_ratio": changeRatio.String(),
			"threshold":    e.cfg.MinChange.String(),
		},
		Timestamp: window[len(window)-1].Timestamp,
	}, true
}
