package ports

import (
	"context"
	"time"

	"github.com/shopspring/decimal"
)

// OrderBookSnapshot represents a stored order book metrics snapshot.
type OrderBookSnapshot struct {
	ID              string
	Exchange        string
	Symbol          string
	BestBid         decimal.Decimal
	BestAsk         decimal.Decimal
	MidPrice        decimal.Decimal
	BidAskSpreadPct decimal.Decimal
	BidDepth1Pct    decimal.Decimal
	AskDepth1Pct    decimal.Decimal
	BidDepth2Pct    decimal.Decimal
	AskDepth2Pct    decimal.Decimal
	Imbalance1Pct   decimal.Decimal
	Imbalance2Pct   decimal.Decimal
	BidLevels       int
	AskLevels       int
	LiquidityScore  decimal.Decimal
	SnapshotAt      time.Time
}

// OrderBookSnapshotRepository manages order book metrics persistence.
type OrderBookSnapshotRepository interface {
	// SaveSnapshot persists an order book metrics snapshot.
	SaveSnapshot(ctx context.Context, snapshot OrderBookSnapshot) error

	// GetLatestSnapshot retrieves the most recent snapshot for an exchange/symbol pair.
	GetLatestSnapshot(ctx context.Context, exchange, symbol string) (*OrderBookSnapshot, error)

	// GetSnapshotsInRange retrieves snapshots within a time range for an exchange/symbol.
	GetSnapshotsInRange(ctx context.Context, exchange, symbol string, from, to time.Time, limit int) ([]OrderBookSnapshot, error)
}

// SignalOutcomeRecorder records trade outcomes for signal expectancy feedback.
type SignalOutcomeRecorder interface {
	// RecordOutcome records the outcome of a signal-driven trade.
	RecordOutcome(ctx context.Context, signalID string, outcome SignalOutcome) error
}

// SignalOutcome represents the result of a signal-driven trade.
type SignalOutcome struct {
	SignalID      string          `json:"signal_id"`
	Exchange      string          `json:"exchange"`
	Symbol        string          `json:"symbol"`
	Direction     string          `json:"direction"`
	EntryPrice    decimal.Decimal `json:"entry_price"`
	ExitPrice     decimal.Decimal `json:"exit_price"`
	PnL           decimal.Decimal `json:"pnl"`
	PnLPercent    decimal.Decimal `json:"pnl_percent"`
	HoldDuration  time.Duration   `json:"hold_duration"`
	OutcomeReason string          `json:"outcome_reason"`
	RecordedAt    time.Time       `json:"recorded_at"`
}
