package agentcontrol

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBackendClientConfig(t *testing.T) {
	t.Helper()

	tests := []struct {
		name            string
		config          ClientConfig
		expectedBaseURL string
		expectedTimeout time.Duration
		expectedRetries int
		expectedAPIKey  string
	}{
		{
			name: "keeps configured values",
			config: ClientConfig{
				BaseURL:     "http://localhost:8080",
				Timeout:     30 * time.Second,
				MaxRetries:  3,
				AdminAPIKey: "admin-key",
			},
			expectedBaseURL: "http://localhost:8080",
			expectedTimeout: 30 * time.Second,
			expectedRetries: 3,
			expectedAPIKey:  "admin-key",
		},
		{
			name: "clamps negative retries",
			config: ClientConfig{
				BaseURL:    "http://localhost:8080",
				Timeout:    10 * time.Second,
				MaxRetries: -5,
			},
			expectedBaseURL: "http://localhost:8080",
			expectedTimeout: 10 * time.Second,
			expectedRetries: 0,
		},
		{
			name: "allows zero timeout and retries",
			config: ClientConfig{
				BaseURL:    "http://localhost:8080",
				Timeout:    0,
				MaxRetries: 0,
			},
			expectedBaseURL: "http://localhost:8080",
			expectedTimeout: 0,
			expectedRetries: 0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			client := NewBackendClient(tt.config)

			require.NotNil(t, client)
			assert.Equal(t, tt.expectedBaseURL, client.config.BaseURL)
			assert.Equal(t, tt.expectedTimeout, client.config.Timeout)
			assert.Equal(t, tt.expectedRetries, client.config.MaxRetries)
			assert.Equal(t, tt.expectedAPIKey, client.config.AdminAPIKey)
		})
	}
}

func TestClientMethodsExist(t *testing.T) {
	t.Helper()

	type requestRecord struct {
		Method string
		Path   string
		Body   map[string]any
	}

	var (
		mu       sync.Mutex
		requests []requestRecord
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		record := requestRecord{
			Method: r.Method,
			Path:   r.URL.Path,
		}
		if r.Body != nil {
			err := json.NewDecoder(r.Body).Decode(&record.Body)
			if err != nil && err != io.EOF {
				t.Fatalf("failed to decode request body: %v", err)
			}
		}

		mu.Lock()
		requests = append(requests, record)
		mu.Unlock()

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewBackendClient(ClientConfig{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	})
	ctx := context.Background()

	tests := []struct {
		name         string
		call         func(context.Context) error
		expectedPath string
	}{
		{
			name:         "pause exchange",
			call:         func(ctx context.Context) error { return client.PauseExchange(ctx, "binance") },
			expectedPath: "/api/v1/agent/pause-exchange",
		},
		{
			name:         "resume exchange",
			call:         func(ctx context.Context) error { return client.ResumeExchange(ctx, "binance") },
			expectedPath: "/api/v1/agent/resume-exchange",
		},
		{
			name:         "enable safe mode",
			call:         func(ctx context.Context) error { return client.EnableSafeMode(ctx) },
			expectedPath: "/api/v1/agent/enable-safe-mode",
		},
		{
			name:         "disable safe mode",
			call:         func(ctx context.Context) error { return client.DisableSafeMode(ctx) },
			expectedPath: "/api/v1/agent/disable-safe-mode",
		},
		{
			name:         "engage kill switch",
			call:         func(ctx context.Context) error { return client.EngageKillSwitch(ctx) },
			expectedPath: "/api/v1/agent/kill-switch",
		},
		{
			name:         "disengage kill switch",
			call:         func(ctx context.Context) error { return client.DisengageKillSwitch(ctx) },
			expectedPath: "/api/v1/agent/kill-switch",
		},
		{
			name:         "cancel all orders",
			call:         func(ctx context.Context) error { return client.CancelAllOrders(ctx, "all") },
			expectedPath: "/api/v1/agent/cancel-all-orders",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(ctx)
			require.NoError(t, err)

			mu.Lock()
			defer mu.Unlock()
			require.NotEmpty(t, requests)

			last := requests[len(requests)-1]
			assert.Equal(t, http.MethodPost, last.Method)
			assert.Equal(t, tt.expectedPath, last.Path)
		})
	}

	t.Run("rejects empty exchange id for pause and resume", func(t *testing.T) {
		beforeCount := func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(requests)
		}()

		require.Error(t, client.PauseExchange(ctx, "   "))
		require.Error(t, client.ResumeExchange(ctx, ""))

		afterCount := func() int {
			mu.Lock()
			defer mu.Unlock()
			return len(requests)
		}()
		assert.Equal(t, beforeCount, afterCount, "expected no backend call for invalid exchange IDs")
	})
}
