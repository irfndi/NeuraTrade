package logging

import (
	"os"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Logger interface defines the common logging methods.
type Logger interface {
	// WithService adds service name to the log context.
	WithService(serviceName string) Logger
	// WithComponent adds component name to the log context.
	WithComponent(componentName string) Logger
	// WithOperation adds operation name to the log context.
	WithOperation(operationName string) Logger
	// WithRequestID adds request ID to the log context.
	WithRequestID(requestID string) Logger
	// WithUserID adds user ID to the log context.
	WithUserID(userID string) Logger
	// WithExchange adds exchange name to the log context.
	WithExchange(exchange string) Logger
	// WithSymbol adds symbol to the log context.
	WithSymbol(symbol string) Logger
	// WithError adds error details to the log context.
	WithError(err error) Logger
	// WithMetrics adds metrics map to the log context.
	WithMetrics(metrics map[string]interface{}) Logger
	// WithFields adds multiple fields to the log context and returns a Logger for chaining.
	WithFields(fields map[string]interface{}) Logger

	// Info logs an info-level message with optional arguments.
	Info(msg string, args ...interface{})
	// Warn logs a warning-level message with optional arguments.
	Warn(msg string, args ...interface{})
	// Error logs an error-level message with optional arguments.
	Error(msg string, args ...interface{})
	// Debug logs a debug-level message with optional arguments.
	Debug(msg string, args ...interface{})
	// Fatal logs a fatal-level message and exits.
	Fatal(msg string, args ...interface{})

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

	// Logger returns the underlying *zap.Logger.
	Logger() *zap.Logger

	// SetLevel sets the log level.
	SetLevel(level string)
}

// StandardLogger provides a standardized logging interface.
type StandardLogger struct {
	logger      *zap.Logger
	atomicLevel zap.AtomicLevel
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
	// Use AtomicLevel for dynamic level changes without recreating the logger
	atomicLevel := zap.NewAtomicLevelAt(getZapLevel(logLevel))

	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder

	var core zapcore.Core

	if environment == "development" {
		encoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		consoleEncoder := zapcore.NewConsoleEncoder(encoderConfig)
		core = zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), atomicLevel)
	} else {
		jsonEncoder := zapcore.NewJSONEncoder(encoderConfig)
		core = zapcore.NewCore(jsonEncoder, zapcore.AddSync(os.Stdout), atomicLevel)
	}

	// Sentry Integration
	// We assume Sentry is initialized globally via observability.InitSentry
	// We add a core that sends Error and Fatal logs to Sentry
	sentryCore := newSentryCore(atomicLevel)
	core = zapcore.NewTee(core, sentryCore)

	logger := zap.New(core, zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel))

	return &StandardLogger{
		logger:      logger,
		atomicLevel: atomicLevel,
	}
}

// SetLogger sets the underlying logger implementation.
//
// Parameters:
//
//	logger: The logger implementation to use.
func (l *StandardLogger) SetLogger(logger *zap.Logger) {
	l.logger = logger
}

func (l *StandardLogger) Logger() *zap.Logger {
	return l.logger
}

// SetLevel sets the log level for the logger dynamically.
// This uses AtomicLevel for efficient level changes without recreating the logger.
// Valid levels: "debug", "info", "warn", "error"
func (l *StandardLogger) SetLevel(level string) {
	l.atomicLevel.SetLevel(getZapLevel(level))
}

// WithService creates a logger with service context.
func (l *StandardLogger) WithService(serviceName string) Logger {
	return &StandardLogger{logger: l.logger.With(zap.String("service", serviceName)), atomicLevel: l.atomicLevel}
}

// WithComponent creates a logger with component context.
func (l *StandardLogger) WithComponent(componentName string) Logger {
	return &StandardLogger{logger: l.logger.With(zap.String("component", componentName)), atomicLevel: l.atomicLevel}
}

// WithOperation creates a logger with operation context.
func (l *StandardLogger) WithOperation(operationName string) Logger {
	return &StandardLogger{logger: l.logger.With(zap.String("operation", operationName)), atomicLevel: l.atomicLevel}
}

// WithRequestID creates a logger with request ID context.
func (l *StandardLogger) WithRequestID(requestID string) Logger {
	return &StandardLogger{logger: l.logger.With(zap.String("request_id", requestID)), atomicLevel: l.atomicLevel}
}

// WithUserID creates a logger with user ID context.
func (l *StandardLogger) WithUserID(userID string) Logger {
	return &StandardLogger{logger: l.logger.With(zap.String("user_id", userID)), atomicLevel: l.atomicLevel}
}

// WithExchange creates a logger with exchange context.
func (l *StandardLogger) WithExchange(exchange string) Logger {
	return &StandardLogger{logger: l.logger.With(zap.String("exchange", exchange)), atomicLevel: l.atomicLevel}
}

// WithSymbol creates a logger with symbol context.
func (l *StandardLogger) WithSymbol(symbol string) Logger {
	return &StandardLogger{logger: l.logger.With(zap.String("symbol", symbol)), atomicLevel: l.atomicLevel}
}

// WithError creates a logger with error context.
func (l *StandardLogger) WithError(err error) Logger {
	return &StandardLogger{logger: l.logger.With(zap.Error(err)), atomicLevel: l.atomicLevel}
}

// WithMetrics creates a logger with metrics context.
func (l *StandardLogger) WithMetrics(metrics map[string]interface{}) Logger {
	return &StandardLogger{logger: l.logger.With(zap.Any("metrics", metrics)), atomicLevel: l.atomicLevel}
}

// WithFields adds multiple fields to the log context.
func (l *StandardLogger) WithFields(fields map[string]interface{}) Logger {
	if len(fields) == 0 {
		return l
	}
	zapFields := make([]zap.Field, 0, len(fields))
	for k, v := range fields {
		zapFields = append(zapFields, zap.Any(k, v))
	}
	return &StandardLogger{logger: l.logger.With(zapFields...), atomicLevel: l.atomicLevel}
}

// Info logs an info-level message.
func (l *StandardLogger) Info(msg string, args ...interface{}) {
	if len(args) > 0 {
		// Use Sugar for formatting if args are present, but we prefer structured fields
		l.logger.Sugar().Infof(msg, args...)
	} else {
		l.logger.Info(msg)
	}
}

// Warn logs a warning-level message.
func (l *StandardLogger) Warn(msg string, args ...interface{}) {
	if len(args) > 0 {
		l.logger.Sugar().Warnf(msg, args...)
	} else {
		l.logger.Warn(msg)
	}
}

// Error logs an error-level message.
func (l *StandardLogger) Error(msg string, args ...interface{}) {
	if len(args) > 0 {
		l.logger.Sugar().Errorf(msg, args...)
	} else {
		l.logger.Error(msg)
	}
}

// Debug logs a debug-level message.
func (l *StandardLogger) Debug(msg string, args ...interface{}) {
	if len(args) > 0 {
		l.logger.Sugar().Debugf(msg, args...)
	} else {
		l.logger.Debug(msg)
	}
}

// Fatal logs a fatal-level message.
func (l *StandardLogger) Fatal(msg string, args ...interface{}) {
	if len(args) > 0 {
		l.logger.Sugar().Fatalf(msg, args...)
	} else {
		l.logger.Fatal(msg)
	}
}

// LogStartup logs application startup information.
func (l *StandardLogger) LogStartup(serviceName string, version string, port int) {
	l.logger.Info("Service starting",
		zap.String("service", serviceName),
		zap.String("version", version),
		zap.Int("port", port),
		zap.String("event", "startup"),
	)
}

// LogShutdown logs application shutdown information.
func (l *StandardLogger) LogShutdown(serviceName string, reason string) {
	l.logger.Info("Service shutting down",
		zap.String("service", serviceName),
		zap.String("reason", reason),
		zap.String("event", "shutdown"),
	)
}

// LogPerformanceMetrics logs performance metrics.
func (l *StandardLogger) LogPerformanceMetrics(serviceName string, metrics map[string]interface{}) {
	l.logger.Info("Performance metrics",
		zap.String("service", serviceName),
		zap.Any("metrics", metrics),
		zap.String("event", "performance_metrics"),
	)
}

// LogResourceStats logs resource statistics.
func (l *StandardLogger) LogResourceStats(serviceName string, stats map[string]interface{}) {
	l.logger.Info("Resource statistics",
		zap.String("service", serviceName),
		zap.Any("stats", stats),
		zap.String("event", "resource_stats"),
	)
}

// LogCacheOperation logs cache operations.
func (l *StandardLogger) LogCacheOperation(operation string, key string, hit bool, duration int64) {
	l.logger.Info("Cache operation",
		zap.String("operation", operation),
		zap.String("key", key),
		zap.Bool("hit", hit),
		zap.Int64("duration_ms", duration),
		zap.String("event", "cache_operation"),
	)
}

// LogDatabaseOperation logs database operations.
func (l *StandardLogger) LogDatabaseOperation(operation string, table string, duration int64, rowsAffected int64) {
	l.logger.Info("Database operation",
		zap.String("operation", operation),
		zap.String("table", table),
		zap.Int64("duration_ms", duration),
		zap.Int64("rows_affected", rowsAffected),
		zap.String("event", "database_operation"),
	)
}

// LogAPIRequest logs API requests.
func (l *StandardLogger) LogAPIRequest(method string, path string, statusCode int, duration int64, userID string) {
	l.logger.Info("API request",
		zap.String("method", method),
		zap.String("path", path),
		zap.Int("status_code", statusCode),
		zap.Int64("duration_ms", duration),
		zap.String("user_id", userID),
		zap.String("event", "api_request"),
	)
}

// LogBusinessEvent logs business events.
func (l *StandardLogger) LogBusinessEvent(eventType string, details map[string]interface{}) {
	l.logger.Info("Business event",
		zap.String("type", eventType),
		zap.Any("details", details),
		zap.String("event", "business_event"),
	)
}

// getZapLevel converts string level to zapcore.Level.
func getZapLevel(level string) zapcore.Level {
	switch strings.ToLower(level) {
	case "debug":
		return zapcore.DebugLevel
	case "warn", "warning":
		return zapcore.WarnLevel
	case "error":
		return zapcore.ErrorLevel
	default:
		return zapcore.InfoLevel
	}
}

type sentryCore struct {
	zapcore.LevelEnabler
}

func newSentryCore(levelEnabler zapcore.LevelEnabler) *sentryCore {
	return &sentryCore{
		LevelEnabler: levelEnabler,
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

func (s *sentryCore) Enabled(level zapcore.Level) bool {
	return s.LevelEnabler.Enabled(level)
}

func (s *sentryCore) With(fields []zapcore.Field) zapcore.Core {
	return s // We don't need to accumulate fields here, we just intercept Check/Write
}

func (s *sentryCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if s.Enabled(ent.Level) && (ent.Level == zapcore.ErrorLevel || ent.Level == zapcore.FatalLevel) {
		return ce.AddCore(ent, s)
	}
	return ce
}

func (s *sentryCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	// Send to Sentry
	// Capture message
	sentry.CaptureMessage(ent.Message)

	// If there are error fields, capture exception
	for _, f := range fields {
		if f.Type == zapcore.ErrorType && f.Interface != nil {
			if err, ok := f.Interface.(error); ok {
				sentry.CaptureException(err)
			}
		}
	}
	return nil
}

func (s *sentryCore) Sync() error {
	sentry.Flush(2 * time.Second) // Wait up to 2 seconds
	return nil
}
