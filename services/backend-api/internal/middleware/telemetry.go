// Package middleware provides HTTP middleware components for NeuraTrade.
package middleware

import (
	"fmt"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-gonic/gin"
)

// TelemetryMiddleware creates Gin middleware for Sentry tracing.
// Initializes Sentry hub for each request to enable error tracking.
//
// Returns:
//   - gin.HandlerFunc: Gin middleware handler.
func TelemetryMiddleware() gin.HandlerFunc {
	return sentrygin.New(sentrygin.Options{
		Repanic: true,
	})
}

// HealthCheckTelemetryMiddleware creates Gin middleware for health check endpoints.
// Tags transactions as health checks for monitoring purposes.
//
// Returns:
//   - gin.HandlerFunc: Gin middleware handler.
func HealthCheckTelemetryMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// For now, we just pass through as we want to monitor health checks too
		// But we can tag them if needed
		if hub := sentrygin.GetHubFromContext(c); hub != nil {
			hub.Scope().SetTag("transaction_type", "health_check")
		}
		c.Next()
	}
}

// RecordError records an error on the current Sentry span/hub.
// Captures the error for tracking and sets span status to error.
//
// Parameters:
//   - c: Gin context.
//   - err: Error to record.
//   - description: Description of the error context.
func RecordError(c *gin.Context, err error, description string) {
	if hub := sentrygin.GetHubFromContext(c); hub != nil {
		hub.CaptureException(err)
		if span := sentry.TransactionFromContext(c.Request.Context()); span != nil {
			span.Status = sentry.SpanStatusInternalError
		}
	}
}

// StartSpan starts a new span or adds a breadcrumb for tracing.
// Wrapper for Sentry span management with backward compatibility.
//
// Parameters:
//   - c: Gin context.
//   - name: Span name or operation description.
func StartSpan(c *gin.Context, name string) {
	if hub := sentrygin.GetHubFromContext(c); hub != nil {
		// Sentry Gin middleware starts the transaction automatically.
		// If we want custom spans, we can use sentry.StartSpan but it requires managing the span lifecycle.
		// For now, we can just add breadcrumbs or do nothing as the transaction is already running.
		hub.AddBreadcrumb(&sentry.Breadcrumb{
			Category: "span",
			Message:  name,
			Level:    sentry.LevelInfo,
		}, nil)
	}
}

// AddSpanAttribute adds an attribute to the current Sentry span.
// Sets a tag on the scope for filtering and searching in Sentry.
//
// Parameters:
//   - c: Gin context.
//   - key: Attribute key name.
//   - value: Attribute value (converted to string).
func AddSpanAttribute(c *gin.Context, key string, value interface{}) {
	if hub := sentrygin.GetHubFromContext(c); hub != nil {
		hub.Scope().SetTag(key, fmt.Sprint(value))
	}
}
