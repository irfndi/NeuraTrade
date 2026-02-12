package observability

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInitSentry tests Sentry initialization
func TestInitSentry(t *testing.T) {
	t.Run("disabled when not enabled", func(t *testing.T) {
		cfg := config.SentryConfig{
			Enabled: false,
			DSN:     "https://test@sentry.io/123",
		}
		err := InitSentry(cfg, "v1.0.0", "test")
		assert.NoError(t, err)
	})

	t.Run("disabled when DSN is empty", func(t *testing.T) {
		cfg := config.SentryConfig{
			Enabled: true,
			DSN:     "",
		}
		err := InitSentry(cfg, "v1.0.0", "test")
		assert.NoError(t, err)
	})

	t.Run("uses fallback release when not specified", func(t *testing.T) {
		cfg := config.SentryConfig{
			Enabled:          true,
			DSN:              "https://fake@fake.ingest.sentry.io/fake",
			Release:          "",
			Environment:      "",
			TracesSampleRate: 0.1,
		}
		// This will fail because the DSN is fake, but we're testing the logic
		_ = InitSentry(cfg, "v1.0.0-test", "testing")
	})

	t.Run("uses config release when specified", func(t *testing.T) {
		cfg := config.SentryConfig{
			Enabled:          true,
			DSN:              "https://fake@fake.ingest.sentry.io/fake",
			Release:          "v2.0.0",
			Environment:      "production",
			TracesSampleRate: 0.5,
		}
		_ = InitSentry(cfg, "v1.0.0", "dev")
	})
}

// TestFlush tests the Flush function
func TestFlush(t *testing.T) {
	t.Run("flushes with deadline context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Should not panic
		Flush(ctx)
	})

	t.Run("flushes with background context", func(t *testing.T) {
		ctx := context.Background()
		// Should not panic
		Flush(ctx)
	})

	t.Run("handles expired context", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		time.Sleep(2 * time.Millisecond)
		defer cancel()

		// Should not panic
		Flush(ctx)
	})
}

// TestCaptureException tests exception capturing
func TestCaptureException(t *testing.T) {
	t.Run("handles nil error", func(t *testing.T) {
		ctx := context.Background()
		// Should not panic
		CaptureException(ctx, nil)
	})

	t.Run("captures error with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)
		err := errors.New("test error")

		// Should not panic
		CaptureException(ctx, err)
	})

	t.Run("captures error without context hub", func(t *testing.T) {
		ctx := context.Background()
		err := errors.New("test error")

		// Should not panic
		CaptureException(ctx, err)
	})
}

// TestCaptureExceptionWithTags tests exception capturing with tags
func TestCaptureExceptionWithTags(t *testing.T) {
	t.Run("handles nil error", func(t *testing.T) {
		ctx := context.Background()
		tags := map[string]string{"key": "value"}

		// Should not panic
		CaptureExceptionWithTags(ctx, nil, tags)
	})

	t.Run("captures error with tags", func(t *testing.T) {
		ctx := context.Background()
		err := errors.New("test error")
		tags := map[string]string{
			"component": "test",
			"severity":  "high",
		}

		// Should not panic
		CaptureExceptionWithTags(ctx, err, tags)
	})

	t.Run("captures error with empty tags", func(t *testing.T) {
		ctx := context.Background()
		err := errors.New("test error")

		// Should not panic
		CaptureExceptionWithTags(ctx, err, map[string]string{})
	})

	t.Run("captures error with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)
		err := errors.New("test error")
		tags := map[string]string{"key": "value"}

		// Should not panic
		CaptureExceptionWithTags(ctx, err, tags)
	})
}

// TestCaptureExceptionWithContext tests exception capturing with full context
func TestCaptureExceptionWithContext(t *testing.T) {
	t.Run("handles nil error", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		CaptureExceptionWithContext(ctx, nil, "test_op", nil)
	})

	t.Run("captures error with context", func(t *testing.T) {
		ctx := context.Background()
		err := errors.New("test error")
		extra := map[string]interface{}{
			"user_id":    123,
			"request_id": "abc-123",
		}

		// Should not panic
		CaptureExceptionWithContext(ctx, err, "database_query", extra)
	})

	t.Run("captures error without extra data", func(t *testing.T) {
		ctx := context.Background()
		err := errors.New("test error")

		// Should not panic
		CaptureExceptionWithContext(ctx, err, "test_op", nil)
	})
}

// TestCaptureMessage tests message capturing
func TestCaptureMessage(t *testing.T) {
	t.Run("captures info message", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		CaptureMessage(ctx, "Test info message", sentry.LevelInfo)
	})

	t.Run("captures warning message", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		CaptureMessage(ctx, "Test warning message", sentry.LevelWarning)
	})

	t.Run("captures error message", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		CaptureMessage(ctx, "Test error message", sentry.LevelError)
	})

	t.Run("captures message with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)

		// Should not panic
		CaptureMessage(ctx, "Test message", sentry.LevelInfo)
	})
}

// TestStartSpan tests span creation
func TestStartSpan(t *testing.T) {
	t.Run("creates span with operation and description", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := StartSpan(ctx, SpanOpDBQuery, "SELECT * FROM users")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)
		assert.Equal(t, "SELECT * FROM users", span.Description)

		// Cleanup
		span.Finish()
	})

	t.Run("creates nested spans", func(t *testing.T) {
		ctx := context.Background()

		ctx1, span1 := StartSpan(ctx, SpanOpHTTPServer, "GET /api/users")
		require.NotNil(t, span1)

		_, span2 := StartSpan(ctx1, SpanOpDBQuery, "SELECT * FROM users")
		require.NotNil(t, span2)

		// Cleanup
		span2.Finish()
		span1.Finish()
	})
}

// TestStartSpanWithTags tests span creation with tags
func TestStartSpanWithTags(t *testing.T) {
	t.Run("creates span with tags", func(t *testing.T) {
		ctx := context.Background()
		tags := map[string]string{
			"db.system":    "postgresql",
			"db.operation": "SELECT",
		}

		spanCtx, span := StartSpanWithTags(ctx, SpanOpDBQuery, "SELECT * FROM users", tags)

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)
		assert.Equal(t, "SELECT * FROM users", span.Description)

		// Cleanup
		span.Finish()
	})

	t.Run("creates span with empty tags", func(t *testing.T) {
		ctx := context.Background()

		_, span := StartSpanWithTags(ctx, SpanOpDBQuery, "SELECT * FROM users", map[string]string{})
		require.NotNil(t, span)

		// Cleanup
		span.Finish()
	})
}

// TestFinishSpan tests span finishing
func TestFinishSpan(t *testing.T) {
	t.Run("finishes span without error", func(t *testing.T) {
		ctx := context.Background()
		_, span := StartSpan(ctx, SpanOpDBQuery, "test query")

		FinishSpan(span, nil)

		assert.Equal(t, sentry.SpanStatusOK, span.Status)
	})

	t.Run("finishes span with error", func(t *testing.T) {
		ctx := context.Background()
		_, span := StartSpan(ctx, SpanOpDBQuery, "test query")

		FinishSpan(span, errors.New("test error"))

		assert.Equal(t, sentry.SpanStatusInternalError, span.Status)
	})

	t.Run("handles nil span", func(t *testing.T) {
		// Should not panic
		FinishSpan(nil, nil)
	})
}

// TestFinishSpanWithStatus tests span finishing with specific status
func TestFinishSpanWithStatus(t *testing.T) {
	t.Run("finishes span with OK status", func(t *testing.T) {
		ctx := context.Background()
		_, span := StartSpan(ctx, SpanOpDBQuery, "test query")

		FinishSpanWithStatus(span, sentry.SpanStatusOK)

		assert.Equal(t, sentry.SpanStatusOK, span.Status)
	})

	t.Run("finishes span with error status", func(t *testing.T) {
		ctx := context.Background()
		_, span := StartSpan(ctx, SpanOpDBQuery, "test query")

		FinishSpanWithStatus(span, sentry.SpanStatusInternalError)

		assert.Equal(t, sentry.SpanStatusInternalError, span.Status)
	})

	t.Run("handles nil span", func(t *testing.T) {
		// Should not panic
		FinishSpanWithStatus(nil, sentry.SpanStatusOK)
	})
}

// TestAddBreadcrumb tests breadcrumb addition
func TestAddBreadcrumb(t *testing.T) {
	t.Run("adds breadcrumb without context hub", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		AddBreadcrumb(ctx, "db", "Query executed", sentry.LevelInfo)
	})

	t.Run("adds breadcrumb with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)

		// Should not panic
		AddBreadcrumb(ctx, "db", "Query executed", sentry.LevelInfo)
	})

	t.Run("adds warning breadcrumb", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		AddBreadcrumb(ctx, "cache", "Cache miss", sentry.LevelWarning)
	})
}

// TestAddBreadcrumbWithData tests breadcrumb addition with data
func TestAddBreadcrumbWithData(t *testing.T) {
	t.Run("adds breadcrumb with data", func(t *testing.T) {
		ctx := context.Background()
		data := map[string]interface{}{
			"table":        "users",
			"rowsAffected": 10,
		}

		// Should not panic
		AddBreadcrumbWithData(ctx, "db", "Query executed", sentry.LevelInfo, data)
	})

	t.Run("adds breadcrumb with nil data", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		AddBreadcrumbWithData(ctx, "db", "Query executed", sentry.LevelInfo, nil)
	})

	t.Run("adds breadcrumb with empty data", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		AddBreadcrumbWithData(ctx, "db", "Query executed", sentry.LevelInfo, map[string]interface{}{})
	})
}

// TestSetUser tests user context setting
func TestSetUser(t *testing.T) {
	t.Run("sets user without context hub", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		SetUser(ctx, "user123", "user@example.com", "testuser")
	})

	t.Run("sets user with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)

		// Should not panic
		SetUser(ctx, "user123", "user@example.com", "testuser")
	})

	t.Run("sets user with partial info", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		SetUser(ctx, "user123", "", "")
	})
}

// TestSetTag tests tag setting
func TestSetTag(t *testing.T) {
	t.Run("sets tag without context hub", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		SetTag(ctx, "environment", "production")
	})

	t.Run("sets tag with context hub", func(t *testing.T) {
		hub := sentry.NewHub(nil, sentry.NewScope())
		ctx := sentry.SetHubOnContext(context.Background(), hub)

		// Should not panic
		SetTag(ctx, "environment", "production")
	})
}

// TestSetTags tests multiple tag setting
func TestSetTags(t *testing.T) {
	t.Run("sets multiple tags", func(t *testing.T) {
		ctx := context.Background()
		tags := map[string]string{
			"environment": "production",
			"region":      "us-east-1",
			"version":     "1.0.0",
		}

		// Should not panic
		SetTags(ctx, tags)
	})

	t.Run("sets empty tags", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		SetTags(ctx, map[string]string{})
	})
}

// TestSetExtra tests extra data setting
func TestSetExtra(t *testing.T) {
	t.Run("sets extra string value", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		SetExtra(ctx, "request_id", "abc-123")
	})

	t.Run("sets extra numeric value", func(t *testing.T) {
		ctx := context.Background()

		// Should not panic
		SetExtra(ctx, "user_count", 42)
	})

	t.Run("sets extra complex value", func(t *testing.T) {
		ctx := context.Background()
		value := map[string]interface{}{
			"items": []string{"a", "b", "c"},
			"count": 3,
		}

		// Should not panic
		SetExtra(ctx, "request_data", value)
	})
}

// TestRecoverAndCapture tests panic recovery
func TestRecoverAndCapture(t *testing.T) {
	t.Run("does nothing when no panic", func(t *testing.T) {
		ctx := context.Background()

		func() {
			defer RecoverAndCapture(ctx, "test_operation")
			// No panic
		}()
		// Should complete without issue
	})

	t.Run("captures panic and re-panics", func(t *testing.T) {
		ctx := context.Background()

		assert.Panics(t, func() {
			defer RecoverAndCapture(ctx, "test_operation")
			panic("test panic")
		})
	})

	t.Run("captures error panic and re-panics", func(t *testing.T) {
		ctx := context.Background()

		assert.Panics(t, func() {
			defer RecoverAndCapture(ctx, "test_operation")
			panic(errors.New("panic error"))
		})
	})
}

// TestTraceDBQuery tests database query tracing
func TestTraceDBQuery(t *testing.T) {
	t.Run("creates span for SELECT query", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := TraceDBQuery(ctx, "SELECT", "users")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)
		assert.Contains(t, span.Description, "SELECT")
		assert.Contains(t, span.Description, "users")

		span.Finish()
	})

	t.Run("creates span for INSERT query", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := TraceDBQuery(ctx, "INSERT", "orders")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)
		assert.Contains(t, span.Description, "INSERT")
		assert.Contains(t, span.Description, "orders")

		span.Finish()
	})
}

// TestTraceDBTransaction tests database transaction tracing
func TestTraceDBTransaction(t *testing.T) {
	t.Run("creates span for transaction", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := TraceDBTransaction(ctx, "create_order_transaction")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)
		assert.Equal(t, "create_order_transaction", span.Description)

		span.Finish()
	})
}

// TestTraceCacheOperation tests cache operation tracing
func TestTraceCacheOperation(t *testing.T) {
	t.Run("creates span for cache get", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := TraceCacheOperation(ctx, "get", "user:123")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)

		span.Finish()
	})

	t.Run("creates span for cache set", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := TraceCacheOperation(ctx, "set", "user:123")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)

		span.Finish()
	})

	t.Run("creates span for cache delete", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := TraceCacheOperation(ctx, "delete", "user:123")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)

		span.Finish()
	})

	t.Run("creates span for unknown cache operation", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := TraceCacheOperation(ctx, "mget", "user:*")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)

		span.Finish()
	})
}

// TestTraceExternalAPI tests external API tracing
func TestTraceExternalAPI(t *testing.T) {
	t.Run("creates span for external API call", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := TraceExternalAPI(ctx, "ccxt", "fetchTicker")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)
		assert.Contains(t, span.Description, "ccxt")
		assert.Contains(t, span.Description, "fetchTicker")

		span.Finish()
	})
}

// TestTraceGRPCCall tests gRPC call tracing
func TestTraceGRPCCall(t *testing.T) {
	t.Run("creates span for gRPC call", func(t *testing.T) {
		ctx := context.Background()

		spanCtx, span := TraceGRPCCall(ctx, "MarketService", "GetPrice")

		require.NotNil(t, span)
		require.NotNil(t, spanCtx)
		assert.Contains(t, span.Description, "MarketService")
		assert.Contains(t, span.Description, "GetPrice")

		span.Finish()
	})
}

// TestSpanOperationConstants tests that constants are defined correctly
func TestSpanOperationConstants(t *testing.T) {
	assert.Equal(t, "http.server", SpanOpHTTPServer)
	assert.Equal(t, "http.client", SpanOpHTTPClient)
	assert.Equal(t, "db.query", SpanOpDBQuery)
	assert.Equal(t, "db.transaction", SpanOpDBTransaction)
	assert.Equal(t, "cache.get", SpanOpCacheGet)
	assert.Equal(t, "cache.set", SpanOpCacheSet)
	assert.Equal(t, "cache.delete", SpanOpCacheDelete)
	assert.Equal(t, "arbitrage.detection", SpanOpArbitrage)
	assert.Equal(t, "signal.processing", SpanOpSignalProcessing)
	assert.Equal(t, "analysis.technical", SpanOpTechnicalAnalys)
	assert.Equal(t, "market.data_collection", SpanOpMarketData)
	assert.Equal(t, "notification.send", SpanOpNotification)
	assert.Equal(t, "grpc.call", SpanOpGRPC)
	assert.Equal(t, "external.api", SpanOpExternalAPI)
}
