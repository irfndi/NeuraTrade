package agentcontrol

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewRegistry(t *testing.T) {
	registry := NewRegistry()
	if registry == nil {
		t.Fatal("NewRegistry returned nil")
	}
	if registry.Count() != 0 {
		t.Errorf("Expected 0 playbooks, got %d", registry.Count())
	}
}

func TestRegistryRegister(t *testing.T) {
	registry := NewRegistry()
	playbook := Playbook{
		Name:        "test_playbook",
		Description: "Test playbook",
		Execute: func(ctx context.Context, event any) error {
			return nil
		},
	}

	registry.Register("test_playbook", playbook)
	if registry.Count() != 1 {
		t.Errorf("Expected 1 playbook, got %d", registry.Count())
	}
}

func TestRegistryGet(t *testing.T) {
	registry := NewRegistry()
	playbook := Playbook{
		Name:        "test_playbook",
		Description: "Test playbook",
		Execute: func(ctx context.Context, event any) error {
			return nil
		},
	}

	registry.Register("test_playbook", playbook)

	retrieved, ok := registry.Get("test_playbook")
	if !ok {
		t.Fatal("Expected to find playbook")
	}
	if retrieved.Name != "test_playbook" {
		t.Errorf("Expected name 'test_playbook', got '%s'", retrieved.Name)
	}
}

func TestRegistryGetNotFound(t *testing.T) {
	registry := NewRegistry()

	_, ok := registry.Get("nonexistent")
	if ok {
		t.Error("Expected to not find nonexistent playbook")
	}
}

func TestRegistryList(t *testing.T) {
	registry := NewRegistry()
	registry.Register("playbook1", Playbook{Name: "playbook1"})
	registry.Register("playbook2", Playbook{Name: "playbook2"})

	playbooks := registry.List()
	if len(playbooks) != 2 {
		t.Errorf("Expected 2 playbooks, got %d", len(playbooks))
	}
}

func TestRegistryExecute(t *testing.T) {
	registry := NewRegistry()
	executed := false

	playbook := Playbook{
		Name:        "test_playbook",
		Description: "Test playbook",
		Execute: func(ctx context.Context, event any) error {
			executed = true
			return nil
		},
	}

	registry.Register("test_playbook", playbook)

	ctx := context.Background()
	err := registry.Execute(ctx, "test_playbook", nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !executed {
		t.Error("Expected playbook to be executed")
	}
}

func TestRegistryExecuteNotFound(t *testing.T) {
	registry := NewRegistry()

	ctx := context.Background()
	err := registry.Execute(ctx, "nonexistent", nil)
	if err == nil {
		t.Error("Expected error for nonexistent playbook")
	}
}

func TestRegistryExecuteTimeout(t *testing.T) {
	registry := NewRegistry()

	playbook := Playbook{
		Name:        "slow_playbook",
		Description: "Slow playbook",
		Execute: func(ctx context.Context, event any) error {
			time.Sleep(2 * time.Second)
			return nil
		},
		Timeout: 100 * time.Millisecond,
	}

	registry.Register("slow_playbook", playbook)

	ctx := context.Background()
	err := registry.Execute(ctx, "slow_playbook", nil)
	if err == nil {
		t.Error("Expected timeout error")
	}
}

func TestPlaybookDefaultTimeout(t *testing.T) {
	registry := NewRegistry()
	playbook := Playbook{
		Name:        "test",
		Description: "Test",
		Execute: func(ctx context.Context, event any) error {
			return nil
		},
	}

	registry.Register("test", playbook)

	retrieved, _ := registry.Get("test")
	if retrieved.Timeout <= 0 {
		t.Error("Expected default timeout to be set")
	}
}

func TestRegistryExecuteWithError(t *testing.T) {
	registry := NewRegistry()
	testError := errors.New("test error")

	playbook := Playbook{
		Name:        "error_playbook",
		Description: "Error playbook",
		Execute: func(ctx context.Context, event any) error {
			return testError
		},
	}

	registry.Register("error_playbook", playbook)

	ctx := context.Background()
	err := registry.Execute(ctx, "error_playbook", nil)
	if err != testError {
		t.Errorf("Expected test error, got %v", err)
	}
}
