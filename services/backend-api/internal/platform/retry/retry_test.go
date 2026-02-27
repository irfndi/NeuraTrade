package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestDoSuccess(t *testing.T) {
	config := DefaultConfig()
	ctx := context.Background()

	err := Do(ctx, config, func(ctx context.Context) error {
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDoRetry(t *testing.T) {
	config := NewConfig(3, 10*time.Millisecond, 100*time.Millisecond)
	ctx := context.Background()

	var attempts atomic.Int32
	err := Do(ctx, config, func(ctx context.Context) error {
		attempts.Add(1)
		if attempts.Load() < 3 {
			return MarkRetryable(errors.New("temporary error"))
		}
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if attempts.Load() != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestDoMaxRetriesExceeded(t *testing.T) {
	config := NewConfig(2, 10*time.Millisecond, 50*time.Millisecond)
	ctx := context.Background()

	var attempts atomic.Int32
	err := Do(ctx, config, func(ctx context.Context) error {
		attempts.Add(1)
		return MarkRetryable(errors.New("always fails"))
	})

	if !errors.Is(err, ErrMaxRetries) {
		t.Errorf("expected ErrMaxRetries, got %v", err)
	}
	if attempts.Load() != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts.Load())
	}
}

func TestDoNonRetryable(t *testing.T) {
	config := DefaultConfig()
	config.IsRetryable = func(err error) bool {
		return false // Make all errors non-retryable
	}
	ctx := context.Background()

	var attempts atomic.Int32
	nonRetryable := errors.New("non-retryable")

	err := Do(ctx, config, func(ctx context.Context) error {
		attempts.Add(1)
		return nonRetryable
	})

	if !errors.Is(err, nonRetryable) {
		t.Errorf("expected non-retryable error, got %v", err)
	}
	if attempts.Load() != 1 {
		t.Errorf("expected 1 attempt (non-retryable), got %d", attempts.Load())
	}
}

func TestDoContextCancelled(t *testing.T) {
	config := NewConfig(10, 100*time.Millisecond, time.Second)
	ctx, cancel := context.WithCancel(context.Background())

	var attempts atomic.Int32
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Do(ctx, config, func(ctx context.Context) error {
		attempts.Add(1)
		return MarkRetryable(errors.New("fail"))
	})

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	if attempts.Load() > 2 {
		t.Errorf("should not retry many times after cancel, got %d attempts", attempts.Load())
	}
}

func TestDoWithResult(t *testing.T) {
	config := NewConfig(3, 10*time.Millisecond, 100*time.Millisecond)
	ctx := context.Background()

	var attempts atomic.Int32
	result, err := DoWithResult(ctx, config, func(ctx context.Context) (string, error) {
		attempts.Add(1)
		if attempts.Load() < 2 {
			return "", MarkRetryable(errors.New("temporary"))
		}
		return "success", nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != "success" {
		t.Errorf("expected 'success', got %s", result)
	}
	if attempts.Load() != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts.Load())
	}
}

func TestPolicy(t *testing.T) {
	policy := NewPolicy(DefaultConfig())
	ctx := context.Background()

	var attempts atomic.Int32
	err := policy.Do(ctx, func(ctx context.Context) error {
		attempts.Add(1)
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPolicyChaining(t *testing.T) {
	policy := Quick.WithMaxAttempts(5).WithDelay(5*time.Millisecond, 50*time.Millisecond)
	ctx := context.Background()

	var attempts atomic.Int32
	err := policy.Do(ctx, func(ctx context.Context) error {
		attempts.Add(1)
		if attempts.Load() < 4 {
			return MarkRetryable(errors.New("fail"))
		}
		return nil
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if attempts.Load() != 4 {
		t.Errorf("expected 4 attempts, got %d", attempts.Load())
	}
}

func TestPolicies(t *testing.T) {
	policies := []*Policy{Quick, Standard, Slow}
	ctx := context.Background()

	for i, policy := range policies {
		t.Run(string(rune('A'+i)), func(t *testing.T) {
			err := policy.Do(ctx, func(ctx context.Context) error {
				return nil
			})
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestRetryableError(t *testing.T) {
	inner := errors.New("inner error")
	retryable := MarkRetryable(inner)

	if retryable.Error() != inner.Error() {
		t.Errorf("expected same error message")
	}

	if !errors.Is(retryable, inner) {
		t.Error("should wrap inner error")
	}
}

func TestCalculateDelay(t *testing.T) {
	config := Config{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     time.Second,
		Multiplier:   2.0,
		Jitter:       0, // No jitter for predictable testing
	}

	tests := []struct {
		attempt int
		min     time.Duration
		max     time.Duration
	}{
		{0, 90 * time.Millisecond, 110 * time.Millisecond},
		{1, 180 * time.Millisecond, 220 * time.Millisecond},
		{2, 360 * time.Millisecond, 440 * time.Millisecond},
	}

	for _, tt := range tests {
		delay := calculateDelay(config, tt.attempt)
		if delay < tt.min || delay > tt.max {
			t.Errorf("attempt %d: expected delay between %v and %v, got %v", tt.attempt, tt.min, tt.max, delay)
		}
	}
}
