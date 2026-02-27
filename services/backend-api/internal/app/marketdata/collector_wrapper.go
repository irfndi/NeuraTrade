// Package marketdata provides compatibility wrapper for existing CollectorService.
package marketdata

import (
	"context"
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
	md "github.com/irfndi/neuratrade/internal/domain/marketdata"
)

// CollectorServiceWrapper provides backward-compatible API for existing CollectorService callers.
// It delegates all operations to the underlying CollectorActor.
type CollectorServiceWrapper struct {
	actorRef *actor.Ref
	timeout  time.Duration
}

// NewCollectorServiceWrapper creates a new compatibility wrapper.
func NewCollectorServiceWrapper(
	db database.DBPool,
	exchange ports.ExchangeRegistry,
	eventBus *eventbus.Bus,
	config Config,
) (*CollectorServiceWrapper, error) {
	collectorActor := NewCollectorActor(db, exchange, eventBus, config)

	// Create actor system and spawn collector
	sys := actor.NewSystem(actor.DefaultConfig())
	ref, err := sys.Spawn(collectorActor, actor.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to spawn collector actor: %w", err)
	}

	return &CollectorServiceWrapper{
		actorRef: ref,
		timeout:  10 * time.Second,
	}, nil
}

// StartExchange starts collection for an exchange (backward compatible).
func (w *CollectorServiceWrapper) StartExchange(ctx context.Context, exchangeID string, symbols []string, interval time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	cmd := md.StartExchangeCommand{
		ExchangeID: exchangeID,
		Symbols:    symbols,
		Interval:   interval,
	}

	return w.actorRef.Send(ctx, cmd)
}

// StopExchange stops collection for an exchange (backward compatible).
func (w *CollectorServiceWrapper) StopExchange(ctx context.Context, exchangeID string) error {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	cmd := md.StopExchangeCommand{
		ExchangeID: exchangeID,
	}

	return w.actorRef.Send(ctx, cmd)
}

// PauseExchange pauses collection for an exchange (backward compatible).
func (w *CollectorServiceWrapper) PauseExchange(ctx context.Context, exchangeID string) error {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	cmd := md.PauseExchangeCommand{
		ExchangeID: exchangeID,
	}

	return w.actorRef.Send(ctx, cmd)
}

// ResumeExchange resumes collection for an exchange (backward compatible).
func (w *CollectorServiceWrapper) ResumeExchange(ctx context.Context, exchangeID string) error {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	cmd := md.ResumeExchangeCommand{
		ExchangeID: exchangeID,
	}

	return w.actorRef.Send(ctx, cmd)
}

// IsExchangeHealthy checks if an exchange is healthy (backward compatible).
func (w *CollectorServiceWrapper) IsExchangeHealthy(ctx context.Context, exchangeID string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	reply := make(chan interface{}, 1)
	cmd := md.HealthCheckCommand{
		ExchangeID: exchangeID,
	}

	env := actor.Envelope{
		Message: cmd,
		Reply:   reply,
	}

	if err := w.actorRef.SendEnvelope(ctx, env); err != nil {
		return false, err
	}

	select {
	case resp := <-reply:
		if healthResp, ok := resp.(md.HealthCheckResponse); ok {
			return healthResp.Healthy, nil
		}
		return false, fmt.Errorf("unexpected response type")
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// GetExchangeStats gets collection statistics for an exchange (backward compatible).
func (w *CollectorServiceWrapper) GetExchangeStats(ctx context.Context, exchangeID string) (map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	reply := make(chan interface{}, 1)
	cmd := md.GetStatsCommand{
		ExchangeID: exchangeID,
	}

	env := actor.Envelope{
		Message: cmd,
		Reply:   reply,
	}

	if err := w.actorRef.SendEnvelope(ctx, env); err != nil {
		return nil, err
	}

	select {
	case resp := <-reply:
		if statsResp, ok := resp.(md.GetStatsResponse); ok {
			return map[string]interface{}{
				"exchange_id":      statsResp.ExchangeID,
				"symbols_count":    statsResp.SymbolsCount,
				"last_collection":  statsResp.LastCollection,
				"collection_count": statsResp.CollectionCount,
				"error_count":      statsResp.ErrorCount,
				"avg_latency_ms":   statsResp.AvgLatencyMs,
			}, nil
		}
		return nil, fmt.Errorf("unexpected response type")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Stop stops the collector service (backward compatible).
func (w *CollectorServiceWrapper) Stop() {
	if w.actorRef != nil {
		w.actorRef.Stop()
	}
}
