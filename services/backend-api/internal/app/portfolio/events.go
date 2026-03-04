package portfolio

import (
	"time"

	domainportfolio "github.com/irfndi/neuratrade/internal/domain/portfolio"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
)

const EventTypePnLUpdated = "portfolio.pnl_updated"

type OrderFilledEvent struct {
	ports.BaseEvent
	TradeID    string
	OrderID    string
	Exchange   string
	Symbol     string
	Side       string
	Quantity   decimal.Decimal
	Price      decimal.Decimal
	Fee        decimal.Decimal
	ExecutedAt time.Time
}

func NewOrderFilledEvent(aggregateID string, fill domainportfolio.Fill) OrderFilledEvent {
	return OrderFilledEvent{
		BaseEvent: ports.BaseEvent{
			Type:       ports.EventTypeOrderFilled,
			Aggregate:  aggregateID,
			OccurredAt: time.Now().UTC().UnixMilli(),
		},
		TradeID:    fill.TradeID,
		OrderID:    fill.OrderID,
		Exchange:   fill.Exchange,
		Symbol:     fill.Symbol,
		Side:       fill.Side,
		Quantity:   fill.Quantity,
		Price:      fill.Price,
		Fee:        fill.Fee,
		ExecutedAt: fill.ExecutedAt,
	}
}

func (e OrderFilledEvent) ToFill() domainportfolio.Fill {
	return domainportfolio.Fill{
		TradeID:    e.TradeID,
		OrderID:    e.OrderID,
		Exchange:   e.Exchange,
		Symbol:     e.Symbol,
		Side:       e.Side,
		Quantity:   e.Quantity,
		Price:      e.Price,
		Fee:        e.Fee,
		ExecutedAt: e.ExecutedAt,
	}
}

type PositionUpdatedEvent struct {
	ports.BaseEvent
	Exchange      string
	Symbol        string
	Side          string
	Quantity      decimal.Decimal
	EntryPrice    decimal.Decimal
	MarkPrice     decimal.Decimal
	Exposure      decimal.Decimal
	RealizedPnL   decimal.Decimal
	UnrealizedPnL decimal.Decimal
}

func NewPositionUpdatedEvent(aggregateID string, state domainportfolio.PositionState) PositionUpdatedEvent {
	return PositionUpdatedEvent{
		BaseEvent: ports.BaseEvent{
			Type:       ports.EventTypePositionUpdated,
			Aggregate:  aggregateID,
			OccurredAt: time.Now().UTC().UnixMilli(),
		},
		Exchange:      state.Exchange,
		Symbol:        state.Symbol,
		Side:          state.Side,
		Quantity:      state.Quantity,
		EntryPrice:    state.EntryPrice,
		MarkPrice:     state.MarkPrice,
		Exposure:      state.Exposure,
		RealizedPnL:   state.RealizedPnL,
		UnrealizedPnL: state.UnrealizedPnL,
	}
}

type PnLUpdatedEvent struct {
	ports.BaseEvent
	Exchange           string
	Symbol             string
	RealizedPnL        decimal.Decimal
	UnrealizedPnL      decimal.Decimal
	TotalRealizedPnL   decimal.Decimal
	TotalUnrealizedPnL decimal.Decimal
}

func NewPnLUpdatedEvent(aggregateID string, state domainportfolio.PositionState, snapshot domainportfolio.Snapshot) PnLUpdatedEvent {
	return PnLUpdatedEvent{
		BaseEvent: ports.BaseEvent{
			Type:       EventTypePnLUpdated,
			Aggregate:  aggregateID,
			OccurredAt: time.Now().UTC().UnixMilli(),
		},
		Exchange:           state.Exchange,
		Symbol:             state.Symbol,
		RealizedPnL:        state.RealizedPnL,
		UnrealizedPnL:      state.UnrealizedPnL,
		TotalRealizedPnL:   snapshot.TotalRealizedPnL,
		TotalUnrealizedPnL: snapshot.TotalUnrealizedPnL,
	}
}

type ProcessOrderFilledMessage struct {
	Event OrderFilledEvent
}

func (m ProcessOrderFilledMessage) MessageType() string { return "portfolio.process_order_filled" }

type UpdateMarkPriceMessage struct {
	Exchange  string
	Symbol    string
	MarkPrice decimal.Decimal
}

func (m UpdateMarkPriceMessage) MessageType() string { return "portfolio.update_mark_price" }

type ReconcileMessage struct {
	Fills []domainportfolio.Fill
}

func (m ReconcileMessage) MessageType() string { return "portfolio.reconcile" }

type GetSnapshotQuery struct{}

func (m GetSnapshotQuery) MessageType() string { return "portfolio.get_snapshot" }

type GetPositionQuery struct {
	Exchange string
	Symbol   string
}

func (m GetPositionQuery) MessageType() string { return "portfolio.get_position" }

type PositionQueryResult struct {
	Position domainportfolio.PositionState
	Found    bool
}
