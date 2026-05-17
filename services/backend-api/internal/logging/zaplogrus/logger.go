package logrus

import (
	"fmt"
	"os"
	"sync"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Level int8

const (
	PanicLevel Level = iota
	FatalLevel
	ErrorLevel
	WarnLevel
	InfoLevel
	DebugLevel
	TraceLevel
)

type Fields map[string]interface{}

type JSONFormatter struct{}

type Logger struct {
	mu          sync.RWMutex
	base        *zap.Logger
	atomicLevel zap.AtomicLevel
	level       Level
	Formatter   interface{}
}

type Entry struct {
	logger *Logger
	fields []zap.Field
}

var std = New()

func New() *Logger {
	atomicLevel := zap.NewAtomicLevelAt(zapcore.InfoLevel)
	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "time"
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderCfg),
		zapcore.Lock(os.Stdout),
		atomicLevel,
	)

	base := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1), zap.AddStacktrace(zapcore.ErrorLevel))
	return &Logger{base: base, atomicLevel: atomicLevel, level: InfoLevel, Formatter: &JSONFormatter{}}
}

func (l *Logger) SetLevel(level Level) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.level = level
	l.atomicLevel.SetLevel(toZapLevel(level))
}

func (l *Logger) GetLevel() Level {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.level
}

func (l *Logger) SetFormatter(formatter interface{}) { l.Formatter = formatter }

func (l *Logger) WithField(key string, value interface{}) *Entry {
	return &Entry{logger: l, fields: []zap.Field{zap.Any(key, value)}}
}

func (l *Logger) WithFields(fields Fields) *Entry {
	return &Entry{logger: l, fields: toZapFields(fields)}
}

func (l *Logger) WithError(err error) *Entry {
	return &Entry{logger: l, fields: []zap.Field{zap.Error(err)}}
}

func (l *Logger) Debug(args ...interface{}) { l.base.Debug(fmt.Sprint(args...)) }
func (l *Logger) Info(args ...interface{})  { l.base.Info(fmt.Sprint(args...)) }
func (l *Logger) Warn(args ...interface{})  { l.base.Warn(fmt.Sprint(args...)) }
func (l *Logger) Error(args ...interface{}) { l.base.Error(fmt.Sprint(args...)) }
func (l *Logger) Fatal(args ...interface{}) { l.base.Fatal(fmt.Sprint(args...)) }
func (l *Logger) Panic(args ...interface{}) { l.base.Panic(fmt.Sprint(args...)) }

func (l *Logger) Debugf(format string, args ...interface{}) {
	l.base.Debug(fmt.Sprintf(format, args...))
}
func (l *Logger) Infof(format string, args ...interface{}) { l.base.Info(fmt.Sprintf(format, args...)) }
func (l *Logger) Warnf(format string, args ...interface{}) { l.base.Warn(fmt.Sprintf(format, args...)) }
func (l *Logger) Errorf(format string, args ...interface{}) {
	l.base.Error(fmt.Sprintf(format, args...))
}
func (l *Logger) Fatalf(format string, args ...interface{}) {
	l.base.Fatal(fmt.Sprintf(format, args...))
}
func (l *Logger) Panicf(format string, args ...interface{}) {
	l.base.Panic(fmt.Sprintf(format, args...))
}

func (l *Logger) Sync() error { return l.base.Sync() }

func (e *Entry) WithField(key string, value interface{}) *Entry {
	newFields := append(copyFields(e.fields), zap.Any(key, value))
	return &Entry{logger: e.logger, fields: newFields}
}

func (e *Entry) WithFields(fields Fields) *Entry {
	newFields := append(copyFields(e.fields), toZapFields(fields)...)
	return &Entry{logger: e.logger, fields: newFields}
}

func (e *Entry) WithError(err error) *Entry {
	newFields := append(copyFields(e.fields), zap.Error(err))
	return &Entry{logger: e.logger, fields: newFields}
}

func (e *Entry) Debug(args ...interface{}) {
	e.logger.base.With(e.fields...).Debug(fmt.Sprint(args...))
}

func (e *Entry) Info(args ...interface{}) {
	e.logger.base.With(e.fields...).Info(fmt.Sprint(args...))
}

func (e *Entry) Warn(args ...interface{}) {
	e.logger.base.With(e.fields...).Warn(fmt.Sprint(args...))
}

func (e *Entry) Error(args ...interface{}) {
	e.logger.base.With(e.fields...).Error(fmt.Sprint(args...))
}

func (e *Entry) Fatal(args ...interface{}) {
	e.logger.base.With(e.fields...).Fatal(fmt.Sprint(args...))
}

func (e *Entry) Panic(args ...interface{}) {
	e.logger.base.With(e.fields...).Panic(fmt.Sprint(args...))
}

func (e *Entry) Debugf(format string, args ...interface{}) {
	e.logger.base.With(e.fields...).Debug(fmt.Sprintf(format, args...))
}

func (e *Entry) Infof(format string, args ...interface{}) {
	e.logger.base.With(e.fields...).Info(fmt.Sprintf(format, args...))
}

func (e *Entry) Warnf(format string, args ...interface{}) {
	e.logger.base.With(e.fields...).Warn(fmt.Sprintf(format, args...))
}

func (e *Entry) Errorf(format string, args ...interface{}) {
	e.logger.base.With(e.fields...).Error(fmt.Sprintf(format, args...))
}

func (e *Entry) Fatalf(format string, args ...interface{}) {
	e.logger.base.With(e.fields...).Fatal(fmt.Sprintf(format, args...))
}

func (e *Entry) Panicf(format string, args ...interface{}) {
	e.logger.base.With(e.fields...).Panic(fmt.Sprintf(format, args...))
}

func Debug(args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Debug(fmt.Sprint(args...))
}
func Info(args ...interface{}) { std.base.WithOptions(zap.AddCallerSkip(1)).Info(fmt.Sprint(args...)) }
func Warn(args ...interface{}) { std.base.WithOptions(zap.AddCallerSkip(1)).Warn(fmt.Sprint(args...)) }
func Error(args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Error(fmt.Sprint(args...))
}
func Fatal(args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Fatal(fmt.Sprint(args...))
}
func Panic(args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Panic(fmt.Sprint(args...))
}

func Debugf(format string, args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Debug(fmt.Sprintf(format, args...))
}
func Infof(format string, args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Info(fmt.Sprintf(format, args...))
}
func Warnf(format string, args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Warn(fmt.Sprintf(format, args...))
}
func Errorf(format string, args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Error(fmt.Sprintf(format, args...))
}
func Fatalf(format string, args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Fatal(fmt.Sprintf(format, args...))
}
func Panicf(format string, args ...interface{}) {
	std.base.WithOptions(zap.AddCallerSkip(1)).Panic(fmt.Sprintf(format, args...))
}

func WithField(key string, value interface{}) *Entry { return std.WithField(key, value) }
func WithFields(fields Fields) *Entry                { return std.WithFields(fields) }
func WithError(err error) *Entry                     { return std.WithError(err) }

func toZapFields(fields Fields) []zap.Field {
	out := make([]zap.Field, 0, len(fields))
	for key, value := range fields {
		out = append(out, zap.Any(key, value))
	}
	return out
}

func copyFields(in []zap.Field) []zap.Field {
	out := make([]zap.Field, len(in))
	copy(out, in)
	return out
}

func toZapLevel(level Level) zapcore.Level {
	switch level {
	case PanicLevel:
		return zapcore.PanicLevel
	case FatalLevel:
		return zapcore.FatalLevel
	case ErrorLevel:
		return zapcore.ErrorLevel
	case WarnLevel:
		return zapcore.WarnLevel
	case InfoLevel:
		return zapcore.InfoLevel
	case DebugLevel:
		return zapcore.DebugLevel
	case TraceLevel:
		return zapcore.DebugLevel
	default:
		return zapcore.InfoLevel
	}
}
