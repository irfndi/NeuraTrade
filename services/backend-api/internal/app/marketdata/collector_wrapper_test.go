package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
)

func TestCollectorServiceWrapper_StartsActorAndProcessesCommands(t *testing.T) {
	bus := eventbus.New(eventbus.DefaultConfig())
	wrapper, err := NewCollectorServiceWrapper(nil, nil, bus, DefaultConfig())
	if err != nil {
		t.Fatalf("new wrapper failed: %v", err)
	}
	defer wrapper.Stop()

	ctx := context.Background()
	if err := wrapper.StartExchange(ctx, "binance", []string{"BTC/USDT"}, time.Second); err != nil {
		t.Fatalf("start exchange failed: %v", err)
	}

	healthy, err := wrapper.IsExchangeHealthy(ctx, "binance")
	if err != nil {
		t.Fatalf("health check failed: %v", err)
	}
	if !healthy {
		t.Fatal("expected started exchange to be healthy")
	}

	if err := wrapper.StopExchange(ctx, "binance"); err != nil {
		t.Fatalf("stop exchange failed: %v", err)
	}

	healthy, err = wrapper.IsExchangeHealthy(ctx, "binance")
	if err != nil {
		t.Fatalf("health check after stop failed: %v", err)
	}
	if healthy {
		t.Fatal("expected stopped exchange to be unhealthy")
	}
}

func TestCollectorServiceWrapper_StopIsIdempotent(t *testing.T) {
	bus := eventbus.New(eventbus.DefaultConfig())
	wrapper, err := NewCollectorServiceWrapper(nil, nil, bus, DefaultConfig())
	if err != nil {
		t.Fatalf("new wrapper failed: %v", err)
	}

	wrapper.Stop()
	wrapper.Stop()

	err = wrapper.StartExchange(context.Background(), "binance", []string{"BTC/USDT"}, time.Second)
	if !errors.Is(err, actor.ErrActorStopped) {
		t.Fatalf("expected ErrActorStopped after stop, got %v", err)
	}
}
