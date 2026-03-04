package risk

import "context"

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
		return source
	}
	if user, ok := ctx.Value(userKey).(string); ok {
		return user
	}
	return "system"
}
