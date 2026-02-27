package timeout

import (
	"context"
	"testing"
	"time"
)

func TestContext(t *testing.T) {
	parent := context.Background()
	ctx, cancel := Context(parent, 100*time.Millisecond)
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Error("expected deadline to be set")
	}

	if time.Until(deadline) > 200*time.Millisecond {
		t.Error("deadline too far in the future")
	}
}

func TestDo(t *testing.T) {
	parent := context.Background()
	called := false

	err := Do(parent, 100*time.Millisecond, func(ctx context.Context) error {
		called = true
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !called {
		t.Error("function was not called")
	}
}

func TestDoTimeout(t *testing.T) {
	parent := context.Background()

	err := Do(parent, 50*time.Millisecond, func(ctx context.Context) error {
		select {
		case <-time.After(200 * time.Millisecond):
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded, got %v", err)
	}
}

func TestDoWithResult(t *testing.T) {
	parent := context.Background()

	result, err := DoWithResult(parent, 100*time.Millisecond, func(ctx context.Context) (string, error) {
		return "success", nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("expected 'success', got %s", result)
	}
}

func TestConfigDefaults(t *testing.T) {
	config := DefaultConfig()

	if config.Database != Normal {
		t.Errorf("expected Database to be %v, got %v", Normal, config.Database)
	}
	if config.Exchange != Slow {
		t.Errorf("expected Exchange to be %v, got %v", Slow, config.Exchange)
	}
}

func TestConfigMethods(t *testing.T) {
	config := DefaultConfig()
	ctx := context.Background()

	tests := []struct {
		name string
		fn   func(context.Context) (context.Context, context.CancelFunc)
	}{
		{"Database", config.WithDatabase},
		{"Redis", config.WithRedis},
		{"Exchange", config.WithExchange},
		{"Telegram", config.WithTelegram},
		{"Plugin", config.WithPlugin},
		{"Default", config.WithDefault},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := tt.fn(ctx)
			defer cancel()

			deadline, ok := ctx.Deadline()
			if !ok {
				t.Error("expected deadline to be set")
			}
			if deadline.Before(time.Now()) {
				t.Error("deadline should be in the future")
			}
		})
	}
}
