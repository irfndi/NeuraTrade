package signals

import (
	"time"

	"github.com/shopspring/decimal"
)

// Side is the directional intent of a trading signal.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// Tick is the market data snapshot consumed by signal logic.
type Tick struct {
	Exchange  string
	Symbol    string
	Bid       decimal.Decimal
	Ask       decimal.Decimal
	Last      decimal.Decimal
	Volume    decimal.Decimal
	Timestamp time.Time
}

// ProposedSignal is the domain-level output from signal evaluation.
type ProposedSignal struct {
	StrategyID string
	Symbol     string
	Side       Side
	Confidence decimal.Decimal
	Metadata   map[string]string
	Timestamp  time.Time
}

// Config controls signal generation behavior.
type Config struct {
	Lookback  int
	MinChange decimal.Decimal
}

// DefaultConfig returns deterministic defaults for momentum-based signaling.
func DefaultConfig() Config {
	return Config{
		Lookback:  3,
		MinChange: decimal.RequireFromString("0.001"),
	}
}
