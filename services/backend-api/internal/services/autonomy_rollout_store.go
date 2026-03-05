package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/autonomous"
)

const (
	autonomyRolloutStateTable  = "autonomous_rollout_states"
	autonomyRollbackEventTable = "autonomous_rollback_events"
)

// AutonomousRolloutStore persists rollout state and rollback events for autonomous strategies.
type AutonomousRolloutStore struct {
	db *sql.DB
}

var _ autonomous.StrategyRepository = (*AutonomousRolloutStore)(nil)

func NewAutonomousRolloutStore(db *sql.DB) *AutonomousRolloutStore {
	return &AutonomousRolloutStore{db: db}
}

func (s *AutonomousRolloutStore) InitSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("autonomous rollout store database is nil")
	}
	statements, err := loadAutonomousRolloutMigrationStatements()
	if err != nil {
		return err
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("init autonomous rollout schema: %w", err)
		}
	}
	return nil
}

func loadAutonomousRolloutMigrationStatements() ([]string, error) {
	migrationPath, err := autonomousRolloutMigrationPath()
	if err != nil {
		return nil, err
	}
	// #nosec G304 -- migrationPath is resolved from repository-internal paths, not user input.
	raw, err := os.ReadFile(migrationPath)
	if err != nil {
		return nil, fmt.Errorf("read autonomous rollout migration: %w", err)
	}
	parts := strings.Split(string(raw), ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		statement := strings.TrimSpace(part)
		if statement == "" {
			continue
		}
		statements = append(statements, statement)
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("autonomous rollout migration contains no statements: %s", migrationPath)
	}
	return statements, nil
}

func autonomousRolloutMigrationPath() (string, error) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("resolve autonomous rollout migration path: runtime caller unavailable")
	}
	return filepath.Clean(filepath.Join(
		filepath.Dir(currentFile),
		"..",
		"..",
		"database",
		"migrations",
		"073_create_autonomous_rollout_tables.sql",
	)), nil
}

func (s *AutonomousRolloutStore) SaveRolloutState(ctx context.Context, state *autonomous.RolloutState) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("autonomous rollout store database is nil")
	}
	if state == nil {
		return fmt.Errorf("rollout state is nil")
	}
	if strings.TrimSpace(state.StrategyID) == "" {
		return fmt.Errorf("strategy_id is required")
	}

	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal rollout state: %w", err)
	}
	chatID := strategyChatID(state.StrategyID)
	now := time.Now().UTC()

	query := fmt.Sprintf(`
		INSERT INTO %s (strategy_id, chat_id, current_stage, status, entered_at, payload, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (strategy_id) DO UPDATE SET
			chat_id = EXCLUDED.chat_id,
			current_stage = EXCLUDED.current_stage,
			status = EXCLUDED.status,
			entered_at = EXCLUDED.entered_at,
			payload = EXCLUDED.payload,
			updated_at = EXCLUDED.updated_at
	`, autonomyRolloutStateTable)

	if _, err := s.db.ExecContext(
		ctx,
		query,
		state.StrategyID,
		chatID,
		string(state.CurrentStage),
		string(state.Status),
		state.EnteredAt.UTC(),
		string(payload),
		now,
	); err != nil {
		return fmt.Errorf("save rollout state: %w", err)
	}

	return nil
}

func (s *AutonomousRolloutStore) GetRolloutState(ctx context.Context, strategyID string) (*autonomous.RolloutState, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("autonomous rollout store database is nil")
	}
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil, fmt.Errorf("strategy_id is required")
	}

	var payload string
	query := fmt.Sprintf(`SELECT payload FROM %s WHERE strategy_id = $1`, autonomyRolloutStateTable)
	err := s.db.QueryRowContext(ctx, query, strategyID).Scan(&payload)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get rollout state: %w", err)
	}

	state := &autonomous.RolloutState{}
	if err := json.Unmarshal([]byte(payload), state); err != nil {
		return nil, fmt.Errorf("unmarshal rollout state: %w", err)
	}
	return state, nil
}

func (s *AutonomousRolloutStore) SaveRollbackEvent(ctx context.Context, event *autonomous.RollbackEvent) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("autonomous rollout store database is nil")
	}
	if event == nil {
		return fmt.Errorf("rollback event is nil")
	}
	if strings.TrimSpace(event.ID) == "" {
		return fmt.Errorf("rollback event id is required")
	}
	if strings.TrimSpace(event.StrategyID) == "" {
		return fmt.Errorf("rollback event strategy_id is required")
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal rollback event: %w", err)
	}
	chatID := strategyChatID(event.StrategyID)
	occurredAt := event.Timestamp.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	query := fmt.Sprintf(`
		INSERT INTO %s (
			id, strategy_id, chat_id, trigger, from_stage, to_stage, reason, payload, occurred_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`, autonomyRollbackEventTable)

	if _, err := s.db.ExecContext(
		ctx,
		query,
		event.ID,
		event.StrategyID,
		chatID,
		string(event.Trigger),
		string(event.FromStage),
		string(event.ToStage),
		event.Reason,
		string(payload),
		occurredAt,
		time.Now().UTC(),
	); err != nil {
		if isUniqueConstraintErr(err) {
			return fmt.Errorf("save rollback event: duplicate rollback event id %q: %w", event.ID, err)
		}
		return fmt.Errorf("save rollback event: %w", err)
	}

	return nil
}

func (s *AutonomousRolloutStore) GetRollbackHistory(ctx context.Context, strategyID string, limit int) ([]autonomous.RollbackEvent, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("autonomous rollout store database is nil")
	}
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return nil, fmt.Errorf("strategy_id is required")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}

	query := fmt.Sprintf(`
		SELECT payload
		FROM %s
		WHERE strategy_id = $1
		ORDER BY occurred_at DESC
		LIMIT $2
	`, autonomyRollbackEventTable)
	rows, err := s.db.QueryContext(ctx, query, strategyID, limit)
	if err != nil {
		return nil, fmt.Errorf("get rollback history: %w", err)
	}
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Printf("autonomous rollout store: failed to close rollback rows for strategy %s: %v", strategyID, closeErr)
		}
	}()

	history := make([]autonomous.RollbackEvent, 0, limit)
	for rows.Next() {
		var payload string
		if scanErr := rows.Scan(&payload); scanErr != nil {
			return nil, fmt.Errorf("scan rollback history: %w", scanErr)
		}
		event := autonomous.RollbackEvent{}
		if unmarshalErr := json.Unmarshal([]byte(payload), &event); unmarshalErr != nil {
			return nil, fmt.Errorf("unmarshal rollback history: %w", unmarshalErr)
		}
		history = append(history, event)
	}
	if rowsErr := rows.Err(); rowsErr != nil {
		return nil, fmt.Errorf("iterate rollback history: %w", rowsErr)
	}

	return history, nil
}

func (s *AutonomousRolloutStore) GetChatRolloutState(ctx context.Context, chatID string) (*autonomous.RolloutState, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("autonomous rollout store database is nil")
	}
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return nil, nil
	}

	var payload string
	query := fmt.Sprintf(`
		SELECT payload
		FROM %s
		WHERE chat_id = $1
		ORDER BY updated_at DESC
		LIMIT 1
	`, autonomyRolloutStateTable)
	err := s.db.QueryRowContext(ctx, query, chatID).Scan(&payload)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get chat rollout state: %w", err)
	}

	state := &autonomous.RolloutState{}
	if err := json.Unmarshal([]byte(payload), state); err != nil {
		return nil, fmt.Errorf("unmarshal chat rollout state: %w", err)
	}
	return state, nil
}

func strategyChatID(strategyID string) string {
	strategyID = strings.TrimSpace(strategyID)
	if strategyID == "" {
		return ""
	}
	parts := strings.Split(strategyID, ":")
	if len(parts) < 3 {
		return ""
	}
	if strings.TrimSpace(parts[0]) != "scalping" {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "duplicate key value violates unique constraint") ||
		strings.Contains(lower, "unique constraint failed")
}
