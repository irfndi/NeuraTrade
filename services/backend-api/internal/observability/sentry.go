package observability

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/irfandi/celebrum-ai-go/internal/config"
)

// SpanOperation constants for consistent span naming
const (
	SpanOpHTTPServer       = "http.server"
	SpanOpHTTPClient       = "http.client"
	SpanOpDBQuery          = "db.query"
	SpanOpDBTransaction    = "db.transaction"
	SpanOpCacheGet         = "cache.get"
	SpanOpCacheSet         = "cache.set"
	SpanOpCacheDelete      = "cache.delete"
	SpanOpArbitrage        = "arbitrage.detection"
	SpanOpSignalProcessing = "signal.processing"
	SpanOpTechnicalAnalys  = "analysis.technical"
	SpanOpMarketData       = "market.data_collection"
	SpanOpNotification     = "notification.send"
	SpanOpGRPC             = "grpc.call"
	SpanOpExternalAPI      = "external.api"
)

// InitSentry configures the Sentry SDK using application config.
//
// Parameters:
//
//	cfg: Sentry configuration.
//	fallbackRelease: Release version if not specified in config.
//	fallbackEnv: Environment if not specified in config.
//
// Returns:
//
//	error: Error if initialization fails.
func InitSentry(cfg config.SentryConfig, fallbackRelease string, fallbackEnv string) error {
	if !cfg.Enabled || cfg.DSN == "" {
		return nil
	}

	release := cfg.Release
	if release == "" {
		release = fallbackRelease
	}

	environment := cfg.Environment
	if environment == "" {
		environment = fallbackEnv
	}

	return sentry.Init(sentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      environment,
		Release:          release,
		EnableTracing:    cfg.TracesSampleRate > 0,
		TracesSampleRate: cfg.TracesSampleRate,
		AttachStacktrace: true,
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			// Add runtime info to all events
			event.Tags["go_version"] = runtime.Version()
			event.Tags["go_os"] = runtime.GOOS
			event.Tags["go_arch"] = runtime.GOARCH
			return event
		},
	})
}

// Flush drains buffered Sentry events within the provided context deadline.
//
// Parameters:
//
//	ctx: Context with optional deadline.
func Flush(ctx context.Context) {
	timeout := 2 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
		if timeout < 0 {
			timeout = 0
		}
	}
	sentry.Flush(timeout)
}

// CaptureException sends an exception to Sentry with context enrichment.
// It uses the hub from the context if available, otherwise uses the global hub.
//
// Parameters:
//
//	ctx: Context.
//	err: Error to capture.
func CaptureException(ctx context.Context, err error) {
	if err == nil {
		return
	}
	if hub := sentry.GetHubFromContext(ctx); hub != nil {
		hub.CaptureException(err)
		return
	}
	sentry.CaptureException(err)
}

// CaptureExceptionWithTags sends an exception to Sentry with additional tags.
//
// Parameters:
//
//	ctx: Context.
//	err: Error to capture.
//	tags: Additional tags to attach to the event.
func CaptureExceptionWithTags(ctx context.Context, err error, tags map[string]string) {
	if err == nil {
		return
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		for k, v := range tags {
			scope.SetTag(k, v)
		}
		hub.CaptureException(err)
	})
}

// CaptureExceptionWithContext sends an exception with full context enrichment.
//
// Parameters:
//
//	ctx: Context.
//	err: Error to capture.
//	operation: The operation that failed.
//	extra: Additional context data.
func CaptureExceptionWithContext(ctx context.Context, err error, operation string, extra map[string]interface{}) {
	if err == nil {
		return
	}

	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub().Clone()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("operation", operation)
		scope.SetLevel(sentry.LevelError)

		for k, v := range extra {
			scope.SetExtra(k, v)
		}

		hub.CaptureException(err)
	})
}

// CaptureMessage sends a message to Sentry.
//
// Parameters:
//
//	ctx: Context.
//	message: Message to send.
//	level: Sentry level (debug, info, warning, error, fatal).
func CaptureMessage(ctx context.Context, message string, level sentry.Level) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetLevel(level)
		hub.CaptureMessage(message)
	})
}

// StartSpan creates a new Sentry span for tracing.
//
// Parameters:
//
//	ctx: Parent context.
//	operation: Span operation name.
//	description: Human-readable description.
//
// Returns:
//
//	context.Context: Context with the span attached.
//	*sentry.Span: The created span (must be finished with span.Finish()).
func StartSpan(ctx context.Context, operation string, description string) (context.Context, *sentry.Span) {
	span := sentry.StartSpan(ctx, operation)
	span.Description = description
	return span.Context(), span
}

// StartSpanWithTags creates a new Sentry span with tags.
//
// Parameters:
//
//	ctx: Parent context.
//	operation: Span operation name.
//	description: Human-readable description.
//	tags: Tags to attach to the span.
//
// Returns:
//
//	context.Context: Context with the span attached.
//	*sentry.Span: The created span.
func StartSpanWithTags(ctx context.Context, operation string, description string, tags map[string]string) (context.Context, *sentry.Span) {
	span := sentry.StartSpan(ctx, operation)
	span.Description = description
	for k, v := range tags {
		span.SetTag(k, v)
	}
	return span.Context(), span
}

// FinishSpan completes a span and optionally records an error.
//
// Parameters:
//
//	span: The span to finish.
//	err: Optional error to record (can be nil).
func FinishSpan(span *sentry.Span, err error) {
	if span == nil {
		return
	}

	if err != nil {
		span.Status = sentry.SpanStatusInternalError
		span.SetTag("error", "true")
		span.SetData("error.message", err.Error())
	} else {
		span.Status = sentry.SpanStatusOK
	}

	span.Finish()
}

// FinishSpanWithStatus completes a span with a specific status.
//
// Parameters:
//
//	span: The span to finish.
//	status: The span status.
func FinishSpanWithStatus(span *sentry.Span, status sentry.SpanStatus) {
	if span == nil {
		return
	}
	span.Status = status
	span.Finish()
}

// AddBreadcrumb adds a breadcrumb to the current scope for debugging.
//
// Parameters:
//
//	ctx: Context.
//	category: Breadcrumb category (e.g., "db", "http", "cache").
//	message: Breadcrumb message.
//	level: Breadcrumb level.
func AddBreadcrumb(ctx context.Context, category string, message string, level sentry.Level) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}

	hub.AddBreadcrumb(&sentry.Breadcrumb{
		Category:  category,
		Message:   message,
		Level:     level,
		Timestamp: time.Now(),
	}, nil)
}

// AddBreadcrumbWithData adds a breadcrumb with additional data.
//
// Parameters:
//
//	ctx: Context.
//	category: Breadcrumb category.
//	message: Breadcrumb message.
//	level: Breadcrumb level.
//	data: Additional data to attach.
func AddBreadcrumbWithData(ctx context.Context, category string, message string, level sentry.Level, data map[string]interface{}) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}

	hub.AddBreadcrumb(&sentry.Breadcrumb{
		Category:  category,
		Message:   message,
		Level:     level,
		Data:      data,
		Timestamp: time.Now(),
	}, nil)
}

// SetUser sets user context for error tracking.
//
// Parameters:
//
//	ctx: Context.
//	userID: User identifier.
//	email: User email (optional).
//	username: Username (optional).
func SetUser(ctx context.Context, userID string, email string, username string) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}

	hub.Scope().SetUser(sentry.User{
		ID:       userID,
		Email:    email,
		Username: username,
	})
}

// SetTag sets a tag on the current scope.
//
// Parameters:
//
//	ctx: Context.
//	key: Tag key.
//	value: Tag value.
func SetTag(ctx context.Context, key string, value string) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub.Scope().SetTag(key, value)
}

// SetTags sets multiple tags on the current scope.
//
// Parameters:
//
//	ctx: Context.
//	tags: Map of tag key-value pairs.
func SetTags(ctx context.Context, tags map[string]string) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub.Scope().SetTags(tags)
}

// SetExtra sets extra context data on the current scope.
//
// Parameters:
//
//	ctx: Context.
//	key: Extra key.
//	value: Extra value.
func SetExtra(ctx context.Context, key string, value interface{}) {
	hub := sentry.GetHubFromContext(ctx)
	if hub == nil {
		hub = sentry.CurrentHub()
	}
	hub.Scope().SetExtra(key, value)
}

// RecoverAndCapture recovers from a panic and reports it to Sentry.
// Should be used with defer.
//
// Parameters:
//
//	ctx: Context.
//	operation: The operation where panic occurred.
func RecoverAndCapture(ctx context.Context, operation string) {
	if r := recover(); r != nil {
		err := fmt.Errorf("panic in %s: %v", operation, r)

		hub := sentry.GetHubFromContext(ctx)
		if hub == nil {
			hub = sentry.CurrentHub()
		}

		hub.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("panic", "true")
			scope.SetTag("operation", operation)
			scope.SetLevel(sentry.LevelFatal)
			hub.CaptureException(err)
		})

		// Re-panic after capturing
		panic(r)
	}
}

// TraceDBQuery creates a span for database queries.
//
// Parameters:
//
//	ctx: Parent context.
//	operation: Query operation (e.g., "SELECT", "INSERT").
//	table: Table name.
//
// Returns:
//
//	context.Context: Context with span.
//	*sentry.Span: The span.
func TraceDBQuery(ctx context.Context, operation string, table string) (context.Context, *sentry.Span) {
	description := fmt.Sprintf("%s %s", operation, table)
	spanCtx, span := StartSpan(ctx, SpanOpDBQuery, description)
	span.SetTag("db.operation", operation)
	span.SetTag("db.table", table)
	return spanCtx, span
}

// TraceDBTransaction creates a span for database transactions.
//
// Parameters:
//
//	ctx: Parent context.
//	name: Transaction name.
//
// Returns:
//
//	context.Context: Context with span.
//	*sentry.Span: The span.
func TraceDBTransaction(ctx context.Context, name string) (context.Context, *sentry.Span) {
	return StartSpan(ctx, SpanOpDBTransaction, name)
}

// TraceCacheOperation creates a span for cache operations.
//
// Parameters:
//
//	ctx: Parent context.
//	operation: Cache operation (get, set, delete).
//	key: Cache key.
//
// Returns:
//
//	context.Context: Context with span.
//	*sentry.Span: The span.
func TraceCacheOperation(ctx context.Context, operation string, key string) (context.Context, *sentry.Span) {
	var spanOp string
	switch operation {
	case "get":
		spanOp = SpanOpCacheGet
	case "set":
		spanOp = SpanOpCacheSet
	case "delete":
		spanOp = SpanOpCacheDelete
	default:
		spanOp = "cache." + operation
	}

	description := fmt.Sprintf("%s %s", operation, key)
	spanCtx, span := StartSpan(ctx, spanOp, description)
	span.SetTag("cache.operation", operation)
	span.SetTag("cache.key", key)
	return spanCtx, span
}

// TraceExternalAPI creates a span for external API calls.
//
// Parameters:
//
//	ctx: Parent context.
//	service: External service name.
//	operation: API operation.
//
// Returns:
//
//	context.Context: Context with span.
//	*sentry.Span: The span.
func TraceExternalAPI(ctx context.Context, service string, operation string) (context.Context, *sentry.Span) {
	description := fmt.Sprintf("%s.%s", service, operation)
	spanCtx, span := StartSpan(ctx, SpanOpExternalAPI, description)
	span.SetTag("external.service", service)
	span.SetTag("external.operation", operation)
	return spanCtx, span
}

// TraceGRPCCall creates a span for gRPC calls.
//
// Parameters:
//
//	ctx: Parent context.
//	service: gRPC service name.
//	method: gRPC method name.
//
// Returns:
//
//	context.Context: Context with span.
//	*sentry.Span: The span.
func TraceGRPCCall(ctx context.Context, service string, method string) (context.Context, *sentry.Span) {
	description := fmt.Sprintf("%s/%s", service, method)
	spanCtx, span := StartSpan(ctx, SpanOpGRPC, description)
	span.SetTag("grpc.service", service)
	span.SetTag("grpc.method", method)
	return spanCtx, span
}
