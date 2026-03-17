package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBackendPort_Priority(t *testing.T) {
	t.Setenv("SERVER_PORT", "8080")
	t.Setenv("PORT", "58080")
	t.Setenv("BACKEND_HOST_PORT", "18080")

	got := resolveBackendPort(nil)
	if got != "8080" {
		t.Fatalf("expected SERVER_PORT to win, got %s", got)
	}
}

func TestResolveBackendPort_FallbackToConfig(t *testing.T) {
	t.Setenv("SERVER_PORT", "")
	t.Setenv("PORT", "")
	t.Setenv("BACKEND_HOST_PORT", "")

	cfg := &localConfig{}
	cfg.Server.Port = 9090
	got := resolveBackendPort(cfg)
	if got != "9090" {
		t.Fatalf("expected config server.port fallback, got %s", got)
	}
}

func TestResolveBackendPort_Default(t *testing.T) {
	t.Setenv("SERVER_PORT", "")
	t.Setenv("PORT", "")
	t.Setenv("BACKEND_HOST_PORT", "")

	got := resolveBackendPort(nil)
	if got != "8080" {
		t.Fatalf("expected default 8080, got %s", got)
	}
}

func TestNormalizeAdminAPIKey(t *testing.T) {
	valid := "abcdefghijklmnopqrstuvwxyz123456"
	if got := normalizeAdminAPIKey(valid); got != valid {
		t.Fatalf("expected valid key to remain unchanged, got %s", got)
	}

	short := "short-key"
	generated := normalizeAdminAPIKey(short)
	if len(generated) < 32 {
		t.Fatalf("expected generated key length >= 32, got %d", len(generated))
	}
}

func TestConfigAdminAPIKey_PrefersSecurityThenTopLevelThenCCXT(t *testing.T) {
	cfg := &localConfig{}
	cfg.CCXT.AdminAPIKey = "ccxt-admin-key-1234567890123456789012"
	if got := configAdminAPIKey(cfg); got != cfg.CCXT.AdminAPIKey {
		t.Fatalf("expected ccxt key fallback, got %q", got)
	}

	cfg.AdminAPIKey = "top-level-admin-key-123456789012345"
	if got := configAdminAPIKey(cfg); got != cfg.AdminAPIKey {
		t.Fatalf("expected top-level key fallback, got %q", got)
	}

	cfg.Security.AdminAPIKey = "security-admin-key-12345678901234"
	if got := configAdminAPIKey(cfg); got != cfg.Security.AdminAPIKey {
		t.Fatalf("expected security key to win, got %q", got)
	}
}

func TestConfigJWTSecret_PrefersSecurityThenAuth(t *testing.T) {
	cfg := &localConfig{}
	cfg.Auth.JWTSecret = "auth-jwt-secret-12345678901234567890"
	if got := configJWTSecret(cfg); got != cfg.Auth.JWTSecret {
		t.Fatalf("expected auth jwt fallback, got %q", got)
	}

	cfg.Security.JWTSecret = "security-jwt-secret-123456789012345678"
	if got := configJWTSecret(cfg); got != cfg.Security.JWTSecret {
		t.Fatalf("expected security jwt to win, got %q", got)
	}
}

func TestNormalizeJWTSecret(t *testing.T) {
	valid := "abcdefghijklmnopqrstuvwxyz123456"
	if got := normalizeJWTSecret(valid); got != valid {
		t.Fatalf("expected valid jwt secret to remain unchanged, got %s", got)
	}

	short := "short-secret"
	generated := normalizeJWTSecret(short)
	if len(generated) < 32 {
		t.Fatalf("expected generated secret length >= 32, got %d", len(generated))
	}
}

func TestDeriveGatewayMode(t *testing.T) {
	tests := []struct {
		name            string
		backendUp       bool
		telegramUp      bool
		ccxtUp          bool
		backendHealthy  bool
		telegramHealthy bool
		want            string
	}{
		{
			name: "all down",
			want: "down",
		},
		{
			name:            "core healthy",
			backendUp:       true,
			telegramUp:      true,
			ccxtUp:          true,
			backendHealthy:  true,
			telegramHealthy: true,
			want:            "healthy",
		},
		{
			name:            "core processes up but warming",
			backendUp:       true,
			telegramUp:      true,
			backendHealthy:  false,
			telegramHealthy: false,
			want:            "warming",
		},
		{
			name:            "partial outage degrades",
			backendUp:       true,
			telegramUp:      false,
			ccxtUp:          true,
			backendHealthy:  true,
			telegramHealthy: false,
			want:            "degraded",
		},
		{
			name:   "ccxt only is degraded",
			ccxtUp: true,
			want:   "degraded",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveGatewayMode(tc.backendUp, tc.telegramUp, tc.ccxtUp, tc.backendHealthy, tc.telegramHealthy)
			if got != tc.want {
				t.Fatalf("unexpected mode: got %s want %s", got, tc.want)
			}
		})
	}
}

func TestCleanupGatewayRuntimeArtifacts_RemovesPIDFilesAndMarksStateDown(t *testing.T) {
	tempDir := t.TempDir()
	statePath := filepath.Join(tempDir, "gateway-state.json")
	backendPID := filepath.Join(tempDir, "backend.pid")
	ccxtPID := filepath.Join(tempDir, "ccxt.pid")
	telegramPID := filepath.Join(tempDir, "telegram.pid")

	for _, pidFile := range []string{backendPID, ccxtPID, telegramPID} {
		err := os.WriteFile(pidFile, []byte("123"), 0644)
		require.NoError(t, err, "write %s", pidFile)
	}

	writeGatewayState(statePath, gatewayRuntimeState{
		Mode: "healthy",
		Services: map[string]gatewayServiceRuntime{
			"backend":  {Status: "healthy", Endpoint: "http://127.0.0.1:8080/health"},
			"ccxt":     {Status: "healthy"},
			"telegram": {Status: "healthy", Endpoint: "http://127.0.0.1:3002/health"},
		},
	})

	cleanupGatewayRuntimeArtifacts(statePath, "gateway stopped", backendPID, ccxtPID, telegramPID)

	for _, pidFile := range []string{backendPID, ccxtPID, telegramPID} {
		_, err := os.Stat(pidFile)
		require.True(t, os.IsNotExist(err), "expected %s to be removed, got err=%v", pidFile, err)
	}

	state, ok := readGatewayState(statePath)
	require.True(t, ok, "expected gateway state to be readable")
	require.Equal(t, "down", state.Mode)
	for _, serviceName := range []string{"gateway", "backend", "ccxt", "telegram"} {
		service, exists := state.Services[serviceName]
		require.True(t, exists, "expected service %s in gateway state", serviceName)
		require.Equal(t, "down", service.Status, "expected %s status down", serviceName)
		require.Equal(t, "gateway stopped", service.Detail, "expected %s detail to be propagated", serviceName)
	}
}
