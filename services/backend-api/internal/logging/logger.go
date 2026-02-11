package logging

import (
	"log/slog"
	"os"
	"strings"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
)

// Logger interface defines the common logging methods.
type Logger interface {
	// WithService adds service name to the log context.
	WithService(serviceName string) *slog.Logger
	// WithComponent adds component name to the log context.
	WithComponent(componentName string) *slog.Logger
	// WithOperation adds operation name to the log context.
	WithOperation(operationName string) *slog.Logger
	// WithRequestID adds request ID to the log context.
	WithRequestID(requestID string) *slog.Logger
	// WithUserID adds user ID to the log context.
	WithUserID(userID string) *slog.Logger
	// WithExchange adds exchange name to the log context.
	WithExchange(exchange string) *slog.Logger
	// WithSymbol adds symbol to the log context.
	WithSymbol(symbol string) *slog.Logger
	// WithError adds error details to the log context.
	WithError(err error) *slog.Logger
	// WithMetrics adds metrics map to the log context.
	WithMetrics(metrics map[string]interface{}) *slog.Logger
	// LogStartup logs application startup information.
	LogStartup(serviceName string, version string, port int)
	// LogShutdown logs application shutdown information.
	LogShutdown(serviceName string, reason string)
	// LogPerformanceMetrics logs performance metrics in a standardized format.
	LogPerformanceMetrics(serviceName string, metrics map[string]interface{})
	// LogResourceStats logs resource statistics in a standardized format.
	LogResourceStats(serviceName string, stats map[string]interface{})
	// LogCacheOperation logs cache operations in a standardized format.
	LogCacheOperation(operation string, key string, hit bool, duration int64)
	// LogDatabaseOperation logs database operations in a standardized format.
	LogDatabaseOperation(operation string, table string, duration int64, rowsAffected int64)
	// LogAPIRequest logs API requests in a standardized format.
	LogAPIRequest(method string, path string, statusCode int, duration int64, userID string)
	// LogBusinessEvent logs business events in a standardized format.
	LogBusinessEvent(eventType string, details map[string]interface{})
	// Logger returns the underlying *slog.Logger.
	Logger() *slog.Logger
}

// StandardLogger provides a standardized logging interface.
type StandardLogger struct {
	logger Logger
}

// NewStandardLogger creates a new standardized logger based on configuration.
//
// Parameters:
//
//	logLevel: The log level (debug, info, warn, error).
//	environment: The environment (development, production).
//
// Returns:
//
//	*StandardLogger: The initialized logger.
func NewStandardLogger(logLevel string, environment string) *StandardLogger {
	// For now, return a basic logger - we'll integrate with Sentry in the main initialization
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: getSlogLevel(logLevel),
	}))

	return &StandardLogger{
		logger: &fallbackLogger{logger: logger},
	}
}

// SetLogger sets the underlying logger implementation.
//
// Parameters:
//
//	logger: The logger implementation to use.
func (l *StandardLogger) SetLogger(logger Logger) {
	l.logger = logger
}

// WithService creates a logger with service context.
//
// Parameters:
//
//	serviceName: The service name.
//
// Returns:
//
//	*slog.Logger: Logger with context.
func (l *StandardLogger) WithService(serviceName string) *slog.Logger {
	return l.logger.WithService(serviceName)
}

// WithComponent creates a logger with component context.
//
// Parameters:
//
//	componentName: The component name.
//
// Returns:
//
//	*slog.Logger: Logger with context.
func (l *StandardLogger) WithComponent(componentName string) *slog.Logger {
	return l.logger.WithComponent(componentName)
}

// WithOperation creates a logger with operation context.
//
// Parameters:
//
//	operationName: The operation name.
//
// Returns:
//
//	*slog.Logger: Logger with context.
func (l *StandardLogger) WithOperation(operationName string) *slog.Logger {
	return l.logger.WithOperation(operationName)
}

// WithRequestID creates a logger with request ID context.
//
// Parameters:
//
//	requestID: The request identifier.
//
// Returns:
//
//	*slog.Logger: Logger with context.
func (l *StandardLogger) WithRequestID(requestID string) *slog.Logger {
	return l.logger.WithRequestID(requestID)
}

// WithUserID creates a logger with user ID context.
//
// Parameters:
//
//	userID: The user identifier.
//
// Returns:
//
//	*slog.Logger: Logger with context.
func (l *StandardLogger) WithUserID(userID string) *slog.Logger {
	return l.logger.WithUserID(userID)
}

// WithExchange creates a logger with exchange context.
//
// Parameters:
//
//	exchange: The exchange name.
//
// Returns:
//
//	*slog.Logger: Logger with context.
func (l *StandardLogger) WithExchange(exchange string) *slog.Logger {
	return l.logger.WithExchange(exchange)
}

// WithSymbol creates a logger with symbol context.
//
// Parameters:
//
//	symbol: The trading symbol.
//
// Returns:
//
//	*slog.Logger: Logger with context.
func (l *StandardLogger) WithSymbol(symbol string) *slog.Logger {
	return l.logger.WithSymbol(symbol)
}

// WithError creates a logger with error context.
//
// Parameters:
//
//	err: The error to log.
//
// Returns:
//
//	*slog.Logger: Logger with context.
func (l *StandardLogger) WithError(err error) *slog.Logger {
	return l.logger.WithError(err)
}

// WithMetrics creates a logger with metrics context.
//
// Parameters:
//
//	metrics: Map of metrics.
//
// Returns:
//
//	*slog.Logger: Logger with context.
func (l *StandardLogger) WithMetrics(metrics map[string]interface{}) *slog.Logger {
	return l.logger.WithMetrics(metrics)
}

// LogStartup logs application startup information.
//
// Parameters:
//
//	serviceName: Name of the service.
//	version: Service version.
//	port: Port number.
func (l *StandardLogger) LogStartup(serviceName string, version string, port int) {
	l.logger.LogStartup(serviceName, version, port)
}

// LogShutdown logs application shutdown information.
//
// Parameters:
//
//	serviceName: Name of the service.
//	reason: Reason for shutdown.
func (l *StandardLogger) LogShutdown(serviceName string, reason string) {
	l.logger.LogShutdown(serviceName, reason)
}

// LogPerformanceMetrics logs performance metrics in a standardized format.
//
// Parameters:
//
//	serviceName: Name of the service.
//	metrics: Map of performance metrics.
func (l *StandardLogger) LogPerformanceMetrics(serviceName string, metrics map[string]interface{}) {
	l.logger.LogPerformanceMetrics(serviceName, metrics)
}

// LogResourceStats logs resource statistics in a standardized format.
//
// Parameters:
//
//	serviceName: Name of the service.
//	stats: Map of resource statistics.
func (l *StandardLogger) LogResourceStats(serviceName string, stats map[string]interface{}) {
	l.logger.LogResourceStats(serviceName, stats)
}

// LogCacheOperation logs cache operations in a standardized format.
//
// Parameters:
//
//	operation: Cache operation (e.g., "get", "set").
//	key: Cache key.
//	hit: Whether it was a cache hit.
//	duration: Duration in milliseconds.
func (l *StandardLogger) LogCacheOperation(operation string, key string, hit bool, duration int64) {
	l.logger.LogCacheOperation(operation, key, hit, duration)
}

// LogDatabaseOperation logs database operations in a standardized format.
//
// Parameters:
//
//	operation: DB operation (e.g., "select", "insert").
//	table: Table name.
//	duration: Duration in milliseconds.
//	rowsAffected: Number of rows affected.
func (l *StandardLogger) LogDatabaseOperation(operation string, table string, duration int64, rowsAffected int64) {
	l.logger.LogDatabaseOperation(operation, table, duration, rowsAffected)
}

// LogAPIRequest logs API requests in a standardized format.
//
// Parameters:
//
//	method: HTTP method.
//	path: Request path.
//	statusCode: HTTP status code.
//	duration: Duration in milliseconds.
//	userID: User identifier (if authenticated).
func (l *StandardLogger) LogAPIRequest(method string, path string, statusCode int, duration int64, userID string) {
	l.logger.LogAPIRequest(method, path, statusCode, duration, userID)
}

// LogBusinessEvent logs business events in a standardized format.
//
// Parameters:
//
//	eventType: Type of business event.
//	details: Event details.
func (l *StandardLogger) LogBusinessEvent(eventType string, details map[string]interface{}) {
	l.logger.LogBusinessEvent(eventType, details)
}

// Logger returns the underlying *slog.Logger.
//
// Returns:
//
//	*slog.Logger: The underlying logger.
func (l *StandardLogger) Logger() *slog.Logger {
	return l.logger.Logger()
}

// getSlogLevel converts string level to slog.Level.
func getSlogLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// ParseLogrusLevel converts string level to zaplogrus.Level.
// This helper is useful for integrations that use Logrus.
func ParseLogrusLevel(level string) zaplogrus.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zaplogrus.DebugLevel
	case "warn", "warning":
		return zaplogrus.WarnLevel
	case "error":
		return zaplogrus.ErrorLevel
	default:
		return zaplogrus.InfoLevel
	}
}

// fallbackLogger is a simple implementation that uses slog directly.
// This is used as a fallback when telemetry is not configured.
type fallbackLogger struct {
	logger *slog.Logger
}

func (f *fallbackLogger) WithService(serviceName string) *slog.Logger {
	return f.logger.With("service", serviceName)
}

func (f *fallbackLogger) WithComponent(componentName string) *slog.Logger {
	return f.logger.With("component", componentName)
}

func (f *fallbackLogger) WithOperation(operationName string) *slog.Logger {
	return f.logger.With("operation", operationName)
}

func (f *fallbackLogger) WithRequestID(requestID string) *slog.Logger {
	return f.logger.With("request_id", requestID)
}

func (f *fallbackLogger) WithUserID(userID string) *slog.Logger {
	return f.logger.With("user_id", userID)
}

func (f *fallbackLogger) WithExchange(exchange string) *slog.Logger {
	return f.logger.With("exchange", exchange)
}

func (f *fallbackLogger) WithSymbol(symbol string) *slog.Logger {
	return f.logger.With("symbol", symbol)
}

func (f *fallbackLogger) WithError(err error) *slog.Logger {
	return f.logger.With("error", err.Error())
}

func (f *fallbackLogger) WithMetrics(metrics map[string]interface{}) *slog.Logger {
	return f.logger.With("metrics", metrics)
}

func (f *fallbackLogger) LogStartup(serviceName string, version string, port int) {
	f.logger.Info("Application startup",
		"service", serviceName,
		"version", version,
		"port", port,
		"event", "startup",
	)
}

func (f *fallbackLogger) LogShutdown(serviceName string, reason string) {
	f.logger.Info("Application shutdown",
		"service", serviceName,
		"reason", reason,
		"event", "shutdown",
	)
}

func (f *fallbackLogger) LogPerformanceMetrics(serviceName string, metrics map[string]interface{}) {
	f.logger.Info("Performance metrics",
		"service", serviceName,
		"metrics", metrics,
		"event", "performance",
	)
}

func (f *fallbackLogger) LogResourceStats(serviceName string, stats map[string]interface{}) {
	f.logger.Info("Resource statistics",
		"service", serviceName,
		"stats", stats,
		"event", "resource",
	)
}

func (f *fallbackLogger) LogCacheOperation(operation string, key string, hit bool, duration int64) {
	f.logger.Info("Cache operation",
		"operation", operation,
		"key", key,
		"hit", hit,
		"duration_ms", duration,
		"event", "cache",
	)
}

func (f *fallbackLogger) LogDatabaseOperation(operation string, table string, duration int64, rowsAffected int64) {
	f.logger.Info("Database operation",
		"operation", operation,
		"table", table,
		"duration_ms", duration,
		"rows_affected", rowsAffected,
		"event", "database",
	)
}

func (f *fallbackLogger) LogAPIRequest(method string, path string, statusCode int, duration int64, userID string) {
	f.logger.Info("API request",
		"method", method,
		"path", path,
		"status", statusCode,
		"duration_ms", duration,
		"user_id", userID,
		"event", "api",
	)
}

func (f *fallbackLogger) LogBusinessEvent(eventType string, details map[string]interface{}) {
	f.logger.Info("Business event",
		"event_type", eventType,
		"details", details,
		"event", "business",
	)
}

func (f *fallbackLogger) Logger() *slog.Logger {
	return f.logger
}
