package agentcontrol

import (
	"context"
	"strings"
	"sync"
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
		{
			name: "start fails when context already canceled",
			config: IngestConfig{
				BackendEventURL: "http://localhost:8080/events",
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
			if tt.name == "start fails when context already canceled" {
				canceledCtx, cancel := context.WithCancel(context.Background())
				cancel()
				ctx = canceledCtx
			}

			eventChan, err := ingestor.Start(ctx)
			if tt.expectedStart {
				require.NoError(t, err)
				require.NotNil(t, eventChan)
			} else {
				require.Error(t, err)
				require.Nil(t, eventChan)
				if tt.name == "start fails when context already canceled" {
					require.ErrorContains(t, err, "context canceled")
				} else {
					require.ErrorContains(t, err, "backend event url is required")
				}
			}

			tt.validate(t, ingestor, ctx)
		})
	}
}

func TestIngestorConcurrentLifecycle(t *testing.T) {
	t.Helper()

	ingestor := NewIngestor(IngestConfig{
		BackendEventURL: "http://localhost:8080/events",
		BufferSize:      256,
		ReconnectDelay:  10 * time.Millisecond,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const goroutines = 8
	const iterations = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				switch (worker + i) % 3 {
				case 0:
					_, err := ingestor.Start(ctx)
					if err != nil {
						errStr := err.Error()
						if !strings.Contains(errStr, "already running") &&
							!strings.Contains(errStr, "context canceled") &&
							!strings.Contains(errStr, "deadline exceeded") {
							t.Errorf("unexpected start error: %v", err)
						}
					}
				case 1:
					stopCtx, stopCancel := context.WithTimeout(context.Background(), 2*time.Second)
					err := ingestor.Stop(stopCtx)
					stopCancel()
					if err != nil {
						t.Errorf("unexpected stop error: %v", err)
					}
				default:
					_ = ingestor.IsRunning()
				}
			}
		}(g)
	}

	wg.Wait()

	finalStopCtx, finalStopCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer finalStopCancel()
	require.NoError(t, ingestor.Stop(finalStopCtx))
}
