package portfolio

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

type PositionState struct {
	Exchange      string
	Symbol        string
	Side          string
	Quantity      decimal.Decimal
	EntryPrice    decimal.Decimal
	MarkPrice     decimal.Decimal
	Exposure      decimal.Decimal
	RealizedPnL   decimal.Decimal
	UnrealizedPnL decimal.Decimal
	UpdatedAt     time.Time
}

type Snapshot struct {
	Positions          []PositionState
	TotalExposure      decimal.Decimal
	TotalRealizedPnL   decimal.Decimal
	TotalUnrealizedPnL decimal.Decimal
	GeneratedAt        time.Time
}

type Totals struct {
	TotalExposure      decimal.Decimal
	TotalRealizedPnL   decimal.Decimal
	TotalUnrealizedPnL decimal.Decimal
}

type Change struct {
	Key              string
	Position         PositionState
	Opened           bool
	Closed           bool
	Reversed         bool
	RealizedPnLDelta decimal.Decimal
}

type Portfolio struct {
	positions map[string]Position
	totals    Totals
}

func NewPortfolio() *Portfolio {
	return &Portfolio{positions: make(map[string]Position)}
}

func PositionKey(exchange, symbol string) string {
	return strings.ToLower(strings.TrimSpace(exchange)) + "|" + strings.ToUpper(strings.TrimSpace(symbol))
}

func (p *Portfolio) ApplyFill(fill Fill) (Change, error) {
	key := PositionKey(fill.Exchange, fill.Symbol)
	pos, ok := p.positions[key]
	before := PositionState{}
	if !ok {
		pos = NewPosition(fill.Exchange, fill.Symbol)
	} else {
		before = positionState(pos)
	}

	res, err := pos.ApplyFill(fill)
	if err != nil {
		return Change{}, err
	}

	if pos.IsOpen() {
		p.positions[key] = pos
	} else {
		delete(p.positions, key)
	}

	after := PositionState{}
	if pos.IsOpen() {
		after = positionState(pos)
	}
	p.totals.TotalExposure = p.totals.TotalExposure.Sub(before.Exposure).Add(after.Exposure)
	p.totals.TotalRealizedPnL = p.totals.TotalRealizedPnL.Sub(before.RealizedPnL).Add(after.RealizedPnL)
	p.totals.TotalUnrealizedPnL = p.totals.TotalUnrealizedPnL.Sub(before.UnrealizedPnL).Add(after.UnrealizedPnL)

	return Change{
		Key:              key,
		Position:         positionState(pos),
		Opened:           res.Opened,
		Closed:           res.Closed,
		Reversed:         res.Reversed,
		RealizedPnLDelta: res.RealizedPnLDelta,
	}, nil
}

func (p *Portfolio) UpdateMarkPrice(exchange, symbol string, markPrice decimal.Decimal) (PositionState, bool, error) {
	key := PositionKey(exchange, symbol)
	pos, ok := p.positions[key]
	if !ok {
		return PositionState{}, false, nil
	}
	before := positionState(pos)
	if err := pos.UpdateMarkPrice(markPrice); err != nil {
		return PositionState{}, false, err
	}
	p.positions[key] = pos
	after := positionState(pos)
	p.totals.TotalExposure = p.totals.TotalExposure.Sub(before.Exposure).Add(after.Exposure)
	p.totals.TotalRealizedPnL = p.totals.TotalRealizedPnL.Sub(before.RealizedPnL).Add(after.RealizedPnL)
	p.totals.TotalUnrealizedPnL = p.totals.TotalUnrealizedPnL.Sub(before.UnrealizedPnL).Add(after.UnrealizedPnL)
	return after, true, nil
}

func (p *Portfolio) GetPosition(exchange, symbol string) (PositionState, bool) {
	pos, ok := p.positions[PositionKey(exchange, symbol)]
	if !ok {
		return PositionState{}, false
	}
	return positionState(pos), true
}

func (p *Portfolio) Snapshot() Snapshot {
	states := make([]PositionState, 0, len(p.positions))

	for _, pos := range p.positions {
		state := positionState(pos)
		states = append(states, state)
	}

	sort.Slice(states, func(i, j int) bool {
		if states[i].Exchange == states[j].Exchange {
			return states[i].Symbol < states[j].Symbol
		}
		return states[i].Exchange < states[j].Exchange
	})

	return Snapshot{
		Positions:          states,
		TotalExposure:      p.totals.TotalExposure,
		TotalRealizedPnL:   p.totals.TotalRealizedPnL,
		TotalUnrealizedPnL: p.totals.TotalUnrealizedPnL,
		GeneratedAt:        time.Now().UTC(),
	}
}

func (p *Portfolio) Totals() Totals {
	return p.totals
}

func (p *Portfolio) Reconcile(fills []Fill) ([]Change, error) {
	working := NewPortfolio()
	ordered := append([]Fill(nil), fills...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].ExecutedAt.Equal(ordered[j].ExecutedAt) {
			if ordered[i].Exchange == ordered[j].Exchange {
				if ordered[i].Symbol == ordered[j].Symbol {
					return ordered[i].TradeID < ordered[j].TradeID
				}
				return ordered[i].Symbol < ordered[j].Symbol
			}
			return ordered[i].Exchange < ordered[j].Exchange
		}
		return ordered[i].ExecutedAt.Before(ordered[j].ExecutedAt)
	})

	changes := make([]Change, 0, len(ordered))
	for _, fill := range ordered {
		change, err := working.ApplyFill(fill)
		if err != nil {
			return nil, fmt.Errorf("reconcile apply fill: %w", err)
		}
		changes = append(changes, change)
	}

	p.positions = working.positions
	p.totals = working.totals
	if err := p.Validate(); err != nil {
		return nil, err
	}

	return changes, nil
}

func (p *Portfolio) Validate() error {
	for key, pos := range p.positions {
		if strings.TrimSpace(pos.Exchange) == "" || strings.TrimSpace(pos.Symbol) == "" {
			return fmt.Errorf("invalid position key %s: missing exchange/symbol", key)
		}
		if pos.Quantity.IsZero() {
			return fmt.Errorf("invalid position key %s: zero quantity should not be stored", key)
		}
		if pos.EntryPrice.LessThan(decimal.Zero) || pos.MarkPrice.LessThan(decimal.Zero) {
			return fmt.Errorf("invalid position key %s: negative price", key)
		}
	}
	return nil
}

func positionState(pos Position) PositionState {
	return PositionState{
		Exchange:      pos.Exchange,
		Symbol:        pos.Symbol,
		Side:          pos.Side(),
		Quantity:      pos.Quantity,
		EntryPrice:    pos.EntryPrice,
		MarkPrice:     pos.MarkPrice,
		Exposure:      pos.Exposure(),
		RealizedPnL:   pos.RealizedPnL,
		UnrealizedPnL: pos.UnrealizedPnL,
		UpdatedAt:     pos.LastUpdatedAt,
	}
}
