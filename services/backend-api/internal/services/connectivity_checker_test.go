package services

import (
	"context"
	"testing"
	"time"
)

func TestConnectivityCheckConfig_Defaults(t *testing.T) {
	config := DefaultConnectivityCheckConfig()

	if config.CheckInterval != 30*time.Second {
		t.Errorf("expected CheckInterval to be 30s, got %v", config.CheckInterval)
	}

	if config.Timeout != 5*time.Second {
		t.Errorf("expected Timeout to be 5s, got %v", config.Timeout)
	}
}

func TestConnectivityChecker_NewChecker(t *testing.T) {
	checker := NewConnectivityChecker(DefaultConnectivityCheckConfig())

	if checker == nil {
		t.Fatal("expected checker to not be nil")
	}

	if len(checker.providers) != 0 {
		t.Errorf("expected no providers initially, got %d", len(checker.providers))
	}
}

func TestConnectivityChecker_RegisterProvider(t *testing.T) {
	checker := NewConnectivityChecker(DefaultConnectivityCheckConfig())

	checker.RegisterProvider("binance", ProviderExchange, func(ctx context.Context) (ProviderStatus, time.Duration, error) {
		return ProviderStatusConnected, 10 * time.Millisecond, nil
	})

	if len(checker.providers) != 1 {
		t.Errorf("expected 1 provider, got %d", len(checker.providers))
	}
}

func TestConnectivityChecker_CheckProvider_NotFound(t *testing.T) {
	checker := NewConnectivityChecker(DefaultConnectivityCheckConfig())

	_, err := checker.CheckProvider(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}

func TestConnectivityChecker_CheckProvider_Success(t *testing.T) {
	checker := NewConnectivityChecker(DefaultConnectivityCheckConfig())

	checker.RegisterProvider("test", ProviderExchange, func(ctx context.Context) (ProviderStatus, time.Duration, error) {
		return ProviderStatusConnected, 5 * time.Millisecond, nil
	})

	result, err := checker.CheckProvider(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != ProviderStatusConnected {
		t.Errorf("expected connected status, got %s", result.Status)
	}
}

func TestConnectivityChecker_CheckAll(t *testing.T) {
	checker := NewConnectivityChecker(DefaultConnectivityCheckConfig())

	checker.RegisterProvider("p1", ProviderExchange, func(ctx context.Context) (ProviderStatus, time.Duration, error) {
		return ProviderStatusConnected, 5 * time.Millisecond, nil
	})
	checker.RegisterProvider("p2", ProviderDatabase, func(ctx context.Context) (ProviderStatus, time.Duration, error) {
		return ProviderStatusConnected, 10 * time.Millisecond, nil
	})

	results, err := checker.CheckAll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestConnectivityChecker_IsHealthy(t *testing.T) {
	checker := NewConnectivityChecker(DefaultConnectivityCheckConfig())

	checker.RegisterProvider("healthy", ProviderExchange, func(ctx context.Context) (ProviderStatus, time.Duration, error) {
		return ProviderStatusConnected, 5 * time.Millisecond, nil
	})

	checker.CheckProvider(context.Background(), "healthy")

	if !checker.IsHealthy() {
		t.Error("expected checker to be healthy")
	}
}

func TestConnectivityChecker_Metrics(t *testing.T) {
	checker := NewConnectivityChecker(DefaultConnectivityCheckConfig())

	checker.RegisterProvider("test", ProviderExchange, func(ctx context.Context) (ProviderStatus, time.Duration, error) {
		return ProviderStatusConnected, 5 * time.Millisecond, nil
	})

	checker.CheckProvider(context.Background(), "test")

	metrics := checker.GetMetrics()

	if metrics.TotalChecks != 1 {
		t.Errorf("expected 1 total check, got %d", metrics.TotalChecks)
	}

	if metrics.SuccessfulChecks != 1 {
		t.Errorf("expected 1 successful check, got %d", metrics.SuccessfulChecks)
	}
}
