package signals

import (
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

// SignalProposedEvent is published when strategy logic proposes a new trade intent.
type SignalProposedEvent struct {
	ports.BaseEvent
	StrategyID string
	Symbol     string
	Side       Side
	Confidence decimal.Decimal
	Metadata   map[string]string
}

// NewSignalProposedEvent builds a port-compatible event from a domain signal.
func NewSignalProposedEvent(signal ProposedSignal) SignalProposedEvent {
	return SignalProposedEvent{
		BaseEvent: ports.BaseEvent{
			Type:       ports.EventTypeSignalProposed,
			Aggregate:  signal.StrategyID,
			OccurredAt: signal.Timestamp.Unix(),
		},
		StrategyID: signal.StrategyID,
		Symbol:     signal.Symbol,
		Side:       signal.Side,
		Confidence: signal.Confidence,
		Metadata:   signal.Metadata,
	}
}
