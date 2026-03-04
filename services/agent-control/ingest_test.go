package agentcontrol

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewIngestor(t *testing.T) {
	t.Helper()

	tests := []struct {
		name                 string
		config               IngestConfig
		expectedBufferSize   int
		expectedReconnectDur time.Duration
	}{
		{
			name: "keeps explicit config",
			config: IngestConfig{
				BackendEventURL: "http://localhost:8080/events",
				BufferSize:      1024,
				ReconnectDelay:  5 * time.Second,
			},
			expectedBufferSize:   1024,
			expectedReconnectDur: 5 * time.Second,
		},
		{
			name: "applies defaults for zero values",
			config: IngestConfig{
				BackendEventURL: "http://localhost:8080/events",
			},
			expectedBufferSize:   1024,
			expectedReconnectDur: 5 * time.Second,
		},
		{
			name: "custom buffer and reconnect delay",
			config: IngestConfig{
				BackendEventURL: "http://localhost:8080/events",
				BufferSize:      512,
				ReconnectDelay:  10 * time.Second,
			},
			expectedBufferSize:   512,
			expectedReconnectDur: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ingestor := NewIngestor(tt.config)

			require.NotNil(t, ingestor)
			assert.Equal(t, tt.expectedBufferSize, ingestor.config.BufferSize)
			assert.Equal(t, tt.expectedReconnectDur, ingestor.config.ReconnectDelay)
		})
	}
}

func TestIngestorLifecycle(t *testing.T) {
	t.Helper()

	tests := []struct {
		name          string
		config        IngestConfig
		validate      func(t *testing.T, ingestor *Ingestor, ctx context.Context)
		expectedStart bool
	}{
		{
			name: "start and stop succeeds",
			config: IngestConfig{
				BackendEventURL: "http://localhost:8080/events",
				BufferSize:      1024,
			},
			expectedStart: true,
			validate: func(t *testing.T, ingestor *Ingestor, ctx context.Context) {
				require.True(t, ingestor.IsRunning())

				stopCtx, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
				defer cancel()
				require.NoError(t, ingestor.Stop(stopCtx))
			},
		},
		{
			name: "cannot start when already running",
			config: IngestConfig{
				BackendEventURL: "http://localhost:8080/events",
			},
			expectedStart: true,
			validate: func(t *testing.T, ingestor *Ingestor, ctx context.Context) {
				_, err := ingestor.Start(ctx)
				require.Error(t, err)
				require.ErrorContains(t, err, "already running")
				require.NoError(t, ingestor.Stop(ctx))
			},
		},
		{
			name: "start fails fast without backend url",
			config: IngestConfig{
				BufferSize: 256,
			},
			expectedStart: false,
			validate: func(t *testing.T, ingestor *Ingestor, ctx context.Context) {
				_ = ctx
				assert.False(t, ingestor.IsRunning())
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			ingestor := NewIngestor(tt.config)
			ctx := context.Background()

			eventChan, err := ingestor.Start(ctx)
			if tt.expectedStart {
				require.NoError(t, err)
				require.NotNil(t, eventChan)
			} else {
				require.Error(t, err)
				require.Nil(t, eventChan)
				require.ErrorContains(t, err, "backend event url is required")
			}

			tt.validate(t, ingestor, ctx)
		})
	}
}
