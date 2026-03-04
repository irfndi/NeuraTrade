// Package agentcontrol provides comprehensive audit logging for agent actions.
package agentcontrol

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// LogLevel represents the severity of an audit log entry.
type LogLevel int

const (
	LevelDebug LogLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

// ActionType represents the type of auditable action.
type ActionType string

const (
	ActionAgentStarted      ActionType = "agent.started"
	ActionAgentStopping     ActionType = "agent.stopping"
	ActionEventReceived     ActionType = "event.received"
	ActionPolicyEvaluated   ActionType = "policy.evaluated"
	ActionPolicyApproved    ActionType = "policy.approved"
	ActionPolicyRejected    ActionType = "policy.rejected"
	ActionPlaybookExecuted  ActionType = "playbook.executed"
	ActionPlaybookCompleted ActionType = "playbook.completed"
	ActionPlaybookFailed    ActionType = "playbook.failed"
	ActionCommandSent       ActionType = "command.sent"
	ActionCommandFailed     ActionType = "command.failed"
)

// AuditConfig holds audit logger configuration.
type AuditConfig struct {
	Level      LogLevel
	OutputPath string
	MaxEntries int // Maximum entries to keep in memory (0 = unlimited)
}

// Entry represents a single audit log entry.
type Entry struct {
	Timestamp  time.Time      `json:"timestamp"`
	Level      LogLevel       `json:"level"`
	ActionType ActionType     `json:"action_type"`
	Component  string         `json:"component"`
	Data       map[string]any `json:"data"`
	TraceID    string         `json:"trace_id,omitempty"`
}

// Logger provides audit logging functionality.
type Logger struct {
	config  AuditConfig
	mu      sync.Mutex
	entries []Entry

	writeMu    sync.Mutex
	output     io.Writer
	outputFile *os.File
}

// NewLogger creates a new audit logger.
func NewLogger(config AuditConfig) *Logger {
	// Set default max entries if not specified
	if config.MaxEntries <= 0 {
		config.MaxEntries = 10000 // Default limit to prevent memory leak
	}

	logger := &Logger{
		config:  config,
		entries: make([]Entry, 0, config.MaxEntries),
		output:  os.Stdout,
	}

	if config.OutputPath != "" {
		f, err := os.OpenFile(config.OutputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open audit log file %q: %v\n", config.OutputPath, err)
		} else {
			logger.outputFile = f
			logger.output = f
		}
	}

	return logger
}

// Log records an audit entry.
func (l *Logger) Log(ctx context.Context, actionType ActionType, component string, data map[string]any) {
	entry := Entry{
		Timestamp:  time.Now().UTC(),
		Level:      LevelInfo,
		ActionType: actionType,
		Component:  component,
		Data:       data,
	}

	if traceID, ok := ctx.Value("trace_id").(string); ok {
		entry.TraceID = traceID
	}

	l.mu.Lock()
	l.entries = append(l.entries, entry)

	// Prevent memory leak by limiting entries (circular buffer behavior)
	if l.config.MaxEntries > 0 && len(l.entries) > l.config.MaxEntries {
		// Remove oldest 10% of entries when limit exceeded
		cutIndex := len(l.entries) / 10
		l.entries = l.entries[cutIndex:]
	}
	l.mu.Unlock()

	// File/stdout write is intentionally outside the entries lock to minimize contention.
	l.writeEntry(entry)
}

func (l *Logger) writeEntry(entry Entry) {
	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal audit entry: %v\n", err)
		return
	}

	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	if _, err := l.output.Write(append(data, '\n')); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to write audit entry: %v\n", err)
	}
}

// GetEntries returns all audit entries.
func (l *Logger) GetEntries() []Entry {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]Entry, len(l.entries))
	copy(result, l.entries)
	return result
}

// Clear removes all audit entries.
func (l *Logger) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = l.entries[:0]
}

// Count returns the number of entries currently in memory.
func (l *Logger) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}

// Close closes any owned file handle used for audit output.
func (l *Logger) Close() error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()

	if l.outputFile == nil {
		return nil
	}

	file := l.outputFile
	l.outputFile = nil
	l.output = os.Stdout

	return file.Close()
}
