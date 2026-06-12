package services

import (
	"testing"
)

func TestResolveSoakRequireRecentMomentum_DefaultsToTrue(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_REQUIRE_RECENT_MOMENTUM", "")
	if !resolveSoakRequireRecentMomentum() {
		t.Error("expected default true when env var unset/empty")
	}
}

func TestResolveSoakRequireRecentMomentum_RespectsFalseOverride(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_REQUIRE_RECENT_MOMENTUM", "false")
	if resolveSoakRequireRecentMomentum() {
		t.Error("expected false when env var set to 'false'")
	}
}

func TestResolveSoakRequireRecentMomentum_RespectsTrueOverride(t *testing.T) {
	t.Setenv("NEURATRADE_SCALPING_REQUIRE_RECENT_MOMENTUM", "true")
	if !resolveSoakRequireRecentMomentum() {
		t.Error("expected true when env var set to 'true'")
	}
}
