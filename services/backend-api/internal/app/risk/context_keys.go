package risk

import (
	"context"
	"strings"
)

type ctxKey string

const (
	tradingModeKey ctxKey = "trading_mode"
	sourceKey      ctxKey = "source"
	userKey        ctxKey = "user"
)

// WithTradingMode annotates context with the current trading mode.
func WithTradingMode(ctx context.Context, mode string) context.Context {
	return context.WithValue(ctx, tradingModeKey, mode)
}

// WithSource annotates context with an action source identifier.
func WithSource(ctx context.Context, source string) context.Context {
	return context.WithValue(ctx, sourceKey, source)
}

// WithUser annotates context with a user identifier used as source fallback.
func WithUser(ctx context.Context, user string) context.Context {
	return context.WithValue(ctx, userKey, user)
}

func tradingModeFromContext(ctx context.Context) (string, bool) {
	mode, ok := ctx.Value(tradingModeKey).(string)
	return mode, ok
}

func sourceFromContext(ctx context.Context) string {
	if source, ok := ctx.Value(sourceKey).(string); ok {
		trimmed := strings.TrimSpace(source)
		if trimmed != "" {
			return trimmed
		}
	}
	if user, ok := ctx.Value(userKey).(string); ok {
		trimmed := strings.TrimSpace(user)
		if trimmed != "" {
			return trimmed
		}
	}
	return "system"
}
