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
