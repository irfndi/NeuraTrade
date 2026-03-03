package marketdata

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/platform/actor"
	"github.com/irfndi/neuratrade/internal/platform/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectorServiceWrapper_Behavior(t *testing.T) {
	testCases := []struct {
		name string
		run  func(t *testing.T, wrapper *CollectorServiceWrapper)
	}{
		{
			name: "start health stop flow",
			run: func(t *testing.T, wrapper *CollectorServiceWrapper) {
				ctx := context.Background()
				require.NoError(t, wrapper.StartExchange(ctx, "binance", []string{"BTC/USDT"}, time.Second))

				healthy, err := wrapper.IsExchangeHealthy(ctx, "binance")
				require.NoError(t, err)
				require.True(t, healthy)

				require.NoError(t, wrapper.StopExchange(ctx, "binance"))

				healthy, err = wrapper.IsExchangeHealthy(ctx, "binance")
				require.NoError(t, err)
				require.False(t, healthy)
			},
		},
		{
			name: "stop is idempotent",
			run: func(t *testing.T, wrapper *CollectorServiceWrapper) {
				wrapper.Stop()
				wrapper.Stop()

				err := wrapper.StartExchange(context.Background(), "binance", []string{"BTC/USDT"}, time.Second)
				assert.True(t, errors.Is(err, actor.ErrActorStopped))
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			bus := eventbus.New(eventbus.DefaultConfig())
			wrapper, err := NewCollectorServiceWrapper(nil, nil, bus, DefaultConfig())
			require.NoError(t, err)
			require.NotNil(t, wrapper)
			defer wrapper.Stop()

			tc.run(t, wrapper)
		})
	}
}
