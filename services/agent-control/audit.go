// Package agentcontrol provides comprehensive audit logging for agent actions.
package agentcontrol

import (
	"context"
	"encoding/json"
	"fmt"
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
}

// NewLogger creates a new audit logger.
func NewLogger(config AuditConfig) *Logger {
	// Set default max entries if not specified
	if config.MaxEntries <= 0 {
		config.MaxEntries = 10000 // Default limit to prevent memory leak
	}
	return &Logger{
		config:  config,
		entries: make([]Entry, 0, config.MaxEntries),
	}
}

// Log records an audit entry.
func (l *Logger) Log(ctx context.Context, actionType ActionType, component string, data map[string]any) {
	l.mu.Lock()
	defer l.mu.Unlock()

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

	l.entries = append(l.entries, entry)

	// Prevent memory leak by limiting entries (circular buffer behavior)
	if l.config.MaxEntries > 0 && len(l.entries) > l.config.MaxEntries {
		// Remove oldest 10% of entries when limit exceeded
		cutIndex := len(l.entries) / 10
		l.entries = l.entries[cutIndex:]
	}

	l.writeEntry(entry)
}

func (l *Logger) writeEntry(entry Entry) {
	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to marshal audit entry: %v\n", err)
		return
	}

	if l.config.OutputPath != "" {
		f, err := os.OpenFile(l.config.OutputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open audit log file: %v\n", err)
			return
		}
		defer f.Close()
		if _, err := f.Write(append(data, '\n')); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to write audit entry: %v\n", err)
		}
	} else {
		fmt.Println(string(data))
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
	l.entries = nil
}

// Count returns the number of entries currently in memory.
func (l *Logger) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.entries)
}
