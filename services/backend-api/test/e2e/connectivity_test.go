package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceConnectivity(t *testing.T) {
	// 1. Test CCXT Service Connectivity via HTTP Mock
	// This verifies the CCXT Client correctly handles the ServiceURL and paths.

	mockCCXT := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"ok","version":"1.0.0"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mockCCXT.Close()

	cfg := &config.CCXTConfig{
		ServiceURL: mockCCXT.URL,
		Timeout:    5,
	}

	client := ccxt.NewClient(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	health, err := client.HealthCheck(ctx)
	require.NoError(t, err)
	assert.NotNil(t, health)
	assert.Equal(t, "ok", health.Status)

	// 2. Integration Test for Config URL Handling
	// This ensures that trailing slashes and other URL oddities are handled.

	cfgSlash := &config.CCXTConfig{
		ServiceURL: mockCCXT.URL + "/",
		Timeout:    5,
	}
	clientSlash := ccxt.NewClient(cfgSlash)
	healthSlash, err := clientSlash.HealthCheck(ctx)
	require.NoError(t, err, "Should handle trailing slashes in ServiceURL")
	assert.Equal(t, "ok", healthSlash.Status)
}

func TestConfigValidation(t *testing.T) {
	// Verifies that the config loading logic handles empty or invalid URLs gracefully
	// (This would test the improvements I plan to make to config.go)

	cfg := &config.Config{}
	cfg.CCXT.ServiceURL = "invalid-url"

	// For now, we just verify it doesn't panic when creating a client
	client := ccxt.NewClient(&cfg.CCXT)
	assert.NotNil(t, client)
}
