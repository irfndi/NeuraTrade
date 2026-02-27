package agentcontrol

import (
	"context"
	"testing"
)

func TestNewLogger(t *testing.T) {
	logger := NewLogger(AuditConfig{
		Level: LevelInfo,
	})
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}
	if len(logger.entries) != 0 {
		t.Errorf("Expected 0 entries, got %d", len(logger.entries))
	}
}

func TestLoggerLog(t *testing.T) {
	logger := NewLogger(AuditConfig{
		Level:      LevelInfo,
		OutputPath: "",
	})

	ctx := context.Background()
	logger.Log(ctx, ActionAgentStarted, "test_component", map[string]any{
		"test_key": "test_value",
	})

	entries := logger.GetEntries()
	if len(entries) != 1 {
		t.Fatalf("Expected 1 entry, got %d", len(entries))
	}

	entry := entries[0]
	if entry.ActionType != ActionAgentStarted {
		t.Errorf("Expected ActionType %s, got %s", ActionAgentStarted, entry.ActionType)
	}
	if entry.Component != "test_component" {
		t.Errorf("Expected Component test_component, got %s", entry.Component)
	}
	if entry.Data["test_key"] != "test_value" {
		t.Errorf("Expected test_value, got %v", entry.Data["test_key"])
	}
}

func TestLoggerClear(t *testing.T) {
	logger := NewLogger(AuditConfig{Level: LevelInfo})
	ctx := context.Background()

	logger.Log(ctx, ActionAgentStarted, "test", map[string]any{})
	logger.Log(ctx, ActionAgentStopping, "test", map[string]any{})

	if len(logger.GetEntries()) != 2 {
		t.Fatal("Expected 2 entries before clear")
	}

	logger.Clear()
	if len(logger.GetEntries()) != 0 {
		t.Error("Expected 0 entries after clear")
	}
}

func TestLogLevelConstants(t *testing.T) {
	if LevelDebug >= LevelInfo {
		t.Error("LevelDebug should be less than LevelInfo")
	}
	if LevelInfo >= LevelWarn {
		t.Error("LevelInfo should be less than LevelWarn")
	}
	if LevelWarn >= LevelError {
		t.Error("LevelWarn should be less than LevelError")
	}
}
