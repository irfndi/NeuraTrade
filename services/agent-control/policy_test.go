package agentcontrol

import (
	"context"
	"testing"
)

func TestNewEngine(t *testing.T) {
	engine := NewEngine(PolicyConfig{
		MaxOrderSize: 1.0,
	})
	if engine == nil {
		t.Fatal("NewEngine returned nil")
	}
}

func TestEngineValidateKillSwitch(t *testing.T) {
	engine := NewEngine(PolicyConfig{
		KillSwitchActive: true,
	})

	ctx := context.Background()
	result := engine.Validate(ctx, Action{
		Type:     ActionPlaybookExecution,
		Playbook: "test_playbook",
	})

	if result.Approved {
		t.Error("Expected action to be rejected when kill switch is active")
	}
	if result.Reason != "kill_switch_active" {
		t.Errorf("Expected reason 'kill_switch_active', got '%s'", result.Reason)
	}
}

func TestEngineValidateSafeMode(t *testing.T) {
	engine := NewEngine(PolicyConfig{
		SafeModeEnabled: true,
	})

	ctx := context.Background()
	result := engine.Validate(ctx, Action{
		Type:     ActionPlaybookExecution,
		Playbook: "enable_strategy",
	})

	if result.Approved {
		t.Error("Expected trading playbook to be rejected in safe mode")
	}
	if result.Reason != "safe_mode_enabled" {
		t.Errorf("Expected reason 'safe_mode_enabled', got '%s'", result.Reason)
	}
}

func TestEngineValidateSafeModeNonTrading(t *testing.T) {
	engine := NewEngine(PolicyConfig{
		SafeModeEnabled: true,
	})

	ctx := context.Background()
	result := engine.Validate(ctx, Action{
		Type:     ActionPlaybookExecution,
		Playbook: "pause_exchange_on_errors",
	})

	if !result.Approved {
		t.Error("Expected non-trading playbook to be approved in safe mode")
	}
}

func TestEngineValidateExchangeAllowlist(t *testing.T) {
	engine := NewEngine(PolicyConfig{
		AllowedExchanges: []string{"binance", "okx"},
	})

	ctx := context.Background()
	result := engine.Validate(ctx, Action{
		Type:     ActionPlaybookExecution,
		Playbook: "test",
		Event: Event{
			Payload: map[string]any{
				"exchange_id": "bybit",
			},
		},
	})

	if result.Approved {
		t.Error("Expected action to be rejected for non-allowed exchange")
	}
	if result.Reason != "exchange_not_allowed" {
		t.Errorf("Expected reason 'exchange_not_allowed', got '%s'", result.Reason)
	}
}

func TestEngineValidateAllowedExchange(t *testing.T) {
	engine := NewEngine(PolicyConfig{
		AllowedExchanges: []string{"binance", "okx"},
	})

	ctx := context.Background()
	result := engine.Validate(ctx, Action{
		Type:     ActionPlaybookExecution,
		Playbook: "test",
		Event: Event{
			Payload: map[string]any{
				"exchange_id": "binance",
			},
		},
	})

	if !result.Approved {
		t.Error("Expected action to be approved for allowed exchange")
	}
}

func TestEngineValidateAllPoliciesPassed(t *testing.T) {
	engine := NewEngine(PolicyConfig{
		KillSwitchActive: false,
		SafeModeEnabled:  false,
	})

	ctx := context.Background()
	result := engine.Validate(ctx, Action{
		Type:     ActionPlaybookExecution,
		Playbook: "test",
	})

	if !result.Approved {
		t.Error("Expected action to be approved when all policies pass")
	}
	if result.Reason != "all_policies_passed" {
		t.Errorf("Expected reason 'all_policies_passed', got '%s'", result.Reason)
	}
}

func TestEngineUpdateConfig(t *testing.T) {
	engine := NewEngine(PolicyConfig{
		MaxOrderSize: 1.0,
	})

	newConfig := PolicyConfig{
		MaxOrderSize: 2.0,
	}
	engine.UpdateConfig(newConfig)

	// Config should be updated (no direct way to verify without exposing config)
}
