package main

import "testing"

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
