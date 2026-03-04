package portfolio

import (
	"context"
	"fmt"
	"sync"
	"time"

	domainportfolio "github.com/irfndi/neuratrade/internal/domain/portfolio"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/ports"
)

type PortfolioActor struct {
	id          string
	eventBus    ports.EventBus
	portfolio   *domainportfolio.Portfolio
	sendTimeout time.Duration
	mu          sync.RWMutex
	ref         *actor.Ref
}

func NewPortfolioActor(id string, eventBus ports.EventBus) *PortfolioActor {
	if id == "" {
		id = "portfolio-actor"
	}
	return &PortfolioActor{
		id:          id,
		eventBus:    eventBus,
		portfolio:   domainportfolio.NewPortfolio(),
		sendTimeout: 2 * time.Second,
	}
}

func (a *PortfolioActor) ID() string {
	return a.id
}

func (a *PortfolioActor) BindRef(ref *actor.Ref) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ref = ref
}

func (a *PortfolioActor) SubscribeOrderFilled(ctx context.Context) error {
	if a.eventBus == nil {
		return nil
	}
	return a.eventBus.Subscribe(ctx, ports.EventTypeOrderFilled, func(handlerCtx context.Context, event ports.Event) error {
		var orderFilled OrderFilledEvent
		switch e := event.(type) {
		case OrderFilledEvent:
			orderFilled = e
		case *OrderFilledEvent:
			if e == nil {
				return nil
			}
			orderFilled = *e
		default:
			return nil
		}

		ref, found := a.getRef()
		if !found {
			return actor.ErrActorStopped
		}

		sendCtx, cancel := context.WithTimeout(handlerCtx, a.sendTimeout)
		defer cancel()
		return ref.Send(sendCtx, ProcessOrderFilledMessage{Event: orderFilled})
	})
}

func (a *PortfolioActor) Receive(ctx context.Context, env actor.Envelope) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	switch msg := env.Message.(type) {
	case ProcessOrderFilledMessage:
		return a.handleOrderFilled(ctx, msg.Event)
	case UpdateMarkPriceMessage:
		return a.handleMarkPriceUpdate(ctx, msg)
	case ReconcileMessage:
		changes, err := a.portfolio.Reconcile(msg.Fills)
		if err != nil {
			return err
		}
		if env.Reply != nil {
			env.Reply <- changes
		}
		return nil
	case GetSnapshotQuery:
		if env.Reply == nil {
			return actor.ErrNoReplyChannel
		}
		env.Reply <- a.portfolio.Snapshot()
		return nil
	case GetPositionQuery:
		if env.Reply == nil {
			return actor.ErrNoReplyChannel
		}
		position, found := a.portfolio.GetPosition(msg.Exchange, msg.Symbol)
		env.Reply <- PositionQueryResult{Position: position, Found: found}
		return nil
	default:
		return fmt.Errorf("%w: %T", actor.ErrInvalidMessage, msg)
	}
}

func (a *PortfolioActor) handleOrderFilled(ctx context.Context, event OrderFilledEvent) error {
	change, err := a.portfolio.ApplyFill(event.ToFill())
	if err != nil {
		return err
	}

	if a.eventBus == nil {
		return nil
	}

	if err := a.publishWithTimeout(ctx, NewPositionUpdatedEvent(a.id, change.Position)); err != nil {
		return err
	}

	if change.Opened {
		if err := a.publishWithTimeout(ctx, ports.BaseEvent{
			Type:       ports.EventTypePositionOpened,
			Aggregate:  a.id,
			OccurredAt: time.Now().UTC().UnixMilli(),
		}); err != nil {
			return err
		}
	}

	if change.Closed {
		if err := a.publishWithTimeout(ctx, ports.BaseEvent{
			Type:       ports.EventTypePositionClosed,
			Aggregate:  a.id,
			OccurredAt: time.Now().UTC().UnixMilli(),
		}); err != nil {
			return err
		}
	}

	totals := a.portfolio.Totals()
	return a.publishWithTimeout(
		ctx,
		NewPnLUpdatedEvent(a.id, change.Position, totals.TotalRealizedPnL, totals.TotalUnrealizedPnL),
	)
}

func (a *PortfolioActor) handleMarkPriceUpdate(ctx context.Context, msg UpdateMarkPriceMessage) error {
	position, found, err := a.portfolio.UpdateMarkPrice(msg.Exchange, msg.Symbol, msg.MarkPrice)
	if err != nil {
		return err
	}
	if !found || a.eventBus == nil {
		return nil
	}

	if err := a.publishWithTimeout(ctx, NewPositionUpdatedEvent(a.id, position)); err != nil {
		return err
	}

	totals := a.portfolio.Totals()
	return a.publishWithTimeout(
		ctx,
		NewPnLUpdatedEvent(a.id, position, totals.TotalRealizedPnL, totals.TotalUnrealizedPnL),
	)
}

func (a *PortfolioActor) publishWithTimeout(ctx context.Context, event ports.Event) error {
	if a.eventBus == nil {
		return nil
	}
	pubCtx, cancel := context.WithTimeout(ctx, a.sendTimeout)
	defer cancel()
	return a.eventBus.Publish(pubCtx, event)
}

func (a *PortfolioActor) getRef() (*actor.Ref, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.ref == nil {
		return nil, false
	}
	return a.ref, true
}
