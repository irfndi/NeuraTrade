package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/domain/marketdata"
	"github.com/irfndi/neuratrade/internal/domain/signals"
	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/irfndi/neuratrade/internal/ports"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestStrategyActor_EmitsSignalProposed(t *testing.T) {
	bus := eventbus.New(eventbus.DefaultConfig())
	cfg := DefaultConfig()
	cfg.Signal.Lookback = 3
	cfg.Signal.MinChange = decimal.RequireFromString("0.01")

	a := NewStrategyActor(cfg, bus, nil)
	sys := actor.NewSystem(actor.DefaultConfig())
	ref, err := sys.Spawn(a, actor.DefaultConfig())
	if err != nil {
		t.Fatalf("spawn actor: %v", err)
	}

	cancelRun, done := startActor(t, ref)
	defer stopActor(t, cancelRun, done)

	signalsCh := make(chan signals.SignalProposedEvent, 2)
	sub, err := bus.Subscribe(context.Background(), ports.EventTypeSignalProposed, func(ctx context.Context, event eventbus.Event) error {
		payload, ok := event.Payload.(signals.SignalProposedEvent)
		if ok {
			signalsCh <- payload
		}
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe signal topic: %v", err)
	}
	defer sub.Unsubscribe()

	base := time.Unix(1700000000, 0)
	prices := []string{"100", "101", "102"}
	for i, p := range prices {
		tick := marketdata.MarketTickEvent{
			Exchange:  "binance",
			Symbol:    "BTC/USDT",
			Last:      decimal.RequireFromString(p),
			Bid:       decimal.RequireFromString(p),
			Ask:       decimal.RequireFromString(p),
			Volume:    decimal.RequireFromString("10"),
			Timestamp: base.Add(time.Duration(i) * time.Second),
		}
		err = ref.SendEnvelope(context.Background(), actor.Envelope{
			Message: &IngestMarketTickMessage{Tick: tick},
			TraceID: "trace-btc",
		})
		if err != nil {
			t.Fatalf("send tick: %v", err)
		}
	}

	select {
	case sig := <-signalsCh:
		if sig.Symbol != "BTC/USDT" {
			t.Fatalf("expected BTC/USDT, got %s", sig.Symbol)
		}
		if sig.Side != signals.SideBuy {
			t.Fatalf("expected side %s, got %s", signals.SideBuy, sig.Side)
		}
		if sig.EventType() != ports.EventTypeSignalProposed {
			t.Fatalf("expected event type %s, got %s", ports.EventTypeSignalProposed, sig.EventType())
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for signal")
	}
}

func TestSubscribeMarketTicks_ForwardsEventsToActor(t *testing.T) {
	bus := eventbus.New(eventbus.DefaultConfig())
	cfg := DefaultConfig()
	cfg.Signal.Lookback = 3
	cfg.Signal.MinChange = decimal.RequireFromString("0.01")

	a := NewStrategyActor(cfg, bus, nil)
	sys := actor.NewSystem(actor.DefaultConfig())
	ref, err := sys.Spawn(a, actor.DefaultConfig())
	if err != nil {
		t.Fatalf("spawn actor: %v", err)
	}

	cancelRun, done := startActor(t, ref)
	defer stopActor(t, cancelRun, done)

	bridgeSub, err := SubscribeMarketTicks(context.Background(), bus, ref)
	if err != nil {
		t.Fatalf("subscribe market ticks bridge: %v", err)
	}
	defer bridgeSub.Unsubscribe()

	signalsCh := make(chan signals.SignalProposedEvent, 2)
	signalSub, err := bus.Subscribe(context.Background(), ports.EventTypeSignalProposed, func(ctx context.Context, event eventbus.Event) error {
		payload, ok := event.Payload.(signals.SignalProposedEvent)
		if ok {
			signalsCh <- payload
		}
		return nil
	})
	if err != nil {
		t.Fatalf("subscribe signal topic: %v", err)
	}
	defer signalSub.Unsubscribe()

	base := time.Unix(1700001000, 0)
	prices := []string{"50", "50.8", "51.2"}
	for i, p := range prices {
		tick := marketdata.MarketTickEvent{
			Exchange:  "kraken",
			Symbol:    "ETH/USDT",
			Last:      decimal.RequireFromString(p),
			Bid:       decimal.RequireFromString(p),
			Ask:       decimal.RequireFromString(p),
			Volume:    decimal.RequireFromString("4"),
			Timestamp: base.Add(time.Duration(i) * time.Second),
		}
		err = bus.PublishSync(context.Background(), eventbus.NewEvent(
			ports.EventTypeMarketTick,
			ports.EventTypeMarketTick,
			tick,
		))
		if err != nil {
			t.Fatalf("publish market tick: %v", err)
		}
	}

	select {
	case sig := <-signalsCh:
		if sig.Symbol != "ETH/USDT" {
			t.Fatalf("expected ETH/USDT, got %s", sig.Symbol)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for forwarded signal")
	}
}

func TestStrategyActor_DeterministicReplay(t *testing.T) {
	ticks := []marketdata.MarketTickEvent{
		{
			Exchange:  "binance",
			Symbol:    "BTC/USDT",
			Last:      decimal.RequireFromString("100"),
			Bid:       decimal.RequireFromString("100"),
			Ask:       decimal.RequireFromString("100"),
			Volume:    decimal.RequireFromString("10"),
			Timestamp: time.Unix(1700002000, 0),
		},
		{
			Exchange:  "binance",
			Symbol:    "BTC/USDT",
			Last:      decimal.RequireFromString("101"),
			Bid:       decimal.RequireFromString("101"),
			Ask:       decimal.RequireFromString("101"),
			Volume:    decimal.RequireFromString("11"),
			Timestamp: time.Unix(1700002001, 0),
		},
		{
			Exchange:  "binance",
			Symbol:    "BTC/USDT",
			Last:      decimal.RequireFromString("102"),
			Bid:       decimal.RequireFromString("102"),
			Ask:       decimal.RequireFromString("102"),
			Volume:    decimal.RequireFromString("12"),
			Timestamp: time.Unix(1700002002, 0),
		},
		{
			Exchange:  "binance",
			Symbol:    "ETH/USDT",
			Last:      decimal.RequireFromString("50"),
			Bid:       decimal.RequireFromString("50"),
			Ask:       decimal.RequireFromString("50"),
			Volume:    decimal.RequireFromString("2"),
			Timestamp: time.Unix(1700002003, 0),
		},
		{
			Exchange:  "binance",
			Symbol:    "ETH/USDT",
			Last:      decimal.RequireFromString("49"),
			Bid:       decimal.RequireFromString("49"),
			Ask:       decimal.RequireFromString("49"),
			Volume:    decimal.RequireFromString("2"),
			Timestamp: time.Unix(1700002004, 0),
		},
		{
			Exchange:  "binance",
			Symbol:    "ETH/USDT",
			Last:      decimal.RequireFromString("48"),
			Bid:       decimal.RequireFromString("48"),
			Ask:       decimal.RequireFromString("48"),
			Volume:    decimal.RequireFromString("2"),
			Timestamp: time.Unix(1700002005, 0),
		},
	}

	runA, err := replayWithActor(ticks)
	if err != nil {
		t.Fatalf("runA failed: %v", err)
	}
	runB, err := replayWithActor(ticks)
	if err != nil {
		t.Fatalf("runB failed: %v", err)
	}

	if len(runA) == 0 {
		t.Fatal("expected replay output to contain signals")
	}
	require.Equal(t, len(runA), len(runB), "replay run size mismatch")
	for i := range runA {
		require.Equal(t, runA[i].StrategyID, runB[i].StrategyID, "strategy mismatch at index %d", i)
		require.Equal(t, runA[i].Symbol, runB[i].Symbol, "symbol mismatch at index %d", i)
		require.Equal(t, runA[i].Side, runB[i].Side, "side mismatch at index %d", i)
		require.True(t, runA[i].Confidence.Equal(runB[i].Confidence), "confidence mismatch at index %d", i)
		require.True(t, runA[i].OccurredAt == runB[i].OccurredAt, "occurred_at mismatch at index %d", i)
		require.Equal(t, runA[i].Metadata, runB[i].Metadata, "metadata mismatch at index %d", i)
	}
}

func replayWithActor(ticks []marketdata.MarketTickEvent) ([]signals.SignalProposedEvent, error) {
	bus := eventbus.New(eventbus.DefaultConfig())
	cfg := DefaultConfig()
	cfg.Signal.Lookback = 3
	cfg.Signal.MinChange = decimal.RequireFromString("0.01")

	a := NewStrategyActor(cfg, bus, nil)
	out := make([]signals.SignalProposedEvent, 0)

	sub, err := bus.Subscribe(context.Background(), ports.EventTypeSignalProposed, func(ctx context.Context, event eventbus.Event) error {
		payload, ok := event.Payload.(signals.SignalProposedEvent)
		if ok {
			out = append(out, payload)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	defer sub.Unsubscribe()

	for _, tick := range ticks {
		err = a.Receive(context.Background(), actor.Envelope{
			Message: &IngestMarketTickMessage{Tick: tick},
			TraceID: "replay",
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func startActor(t *testing.T, ref *actor.Ref) (context.CancelFunc, chan error) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- ref.Run(ctx)
	}()

	require.Eventually(t, func() bool {
		return ref.IsRunning()
	}, 2*time.Second, 10*time.Millisecond, "actor did not start")

	return cancel, done
}

func stopActor(t *testing.T, cancel context.CancelFunc, done chan error) {
	t.Helper()
	cancel()
	err := <-done
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("actor stop error: %v", err)
	}
}
