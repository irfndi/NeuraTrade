package signals

// Replay executes deterministic signal generation on a fixed tick stream.
func Replay(strategyID string, cfg Config, ticks []Tick) []ProposedSignal {
	engine := NewEngine(cfg)
	lookback := engine.cfg.Lookback
	if strategyID == "" {
		strategyID = "default"
	}

	windows := make(map[string][]Tick)
	out := make([]ProposedSignal, 0)

	for _, tick := range ticks {
		symbolWindow := append(windows[tick.Symbol], tick)
		if len(symbolWindow) > lookback {
			symbolWindow = symbolWindow[len(symbolWindow)-lookback:]
		}
		windows[tick.Symbol] = symbolWindow

		signal, ok := engine.Evaluate(strategyID, symbolWindow)
		if ok {
			out = append(out, signal)
		}
	}

	return out
}
