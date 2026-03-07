package services

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/irfndi/neuratrade/internal/autonomous"
)

const (
	autonomyRolloutStateTable  = "autonomous_rollout_states"
	autonomyRollbackEventTable = "autonomous_rollback_events"

	saveRolloutStateQuery = `
		INSERT INTO autonomous_rollout_states (strategy_id, chat_id, current_stage, status, entered_at, payload, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (strategy_id) DO UPDATE SET
			chat_id = EXCLUDED.chat_id,
			current_stage = EXCLUDED.current_stage,
			status = EXCLUDED.status,
			entered_at = EXCLUDED.entered_at,
			payload = EXCLUDED.payload,
			updated_at = EXCLUDED.updated_at
	`

	getRolloutStateQuery = `SELECT payload FROM autonomous_rollout_states WHERE strategy_id = $1`

	saveRollbackEventQuery = `
		INSERT INTO autonomous_rollback_events (
			id, strategy_id, chat_id, trigger, from_stage, to_stage, reason, payload, occurred_at, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	getRollbackHistoryQuery = `
		SELECT payload
		FROM autonomous_rollback_events
		WHERE strategy_id = $1
		ORDER BY occurred_at DESC
		LIMIT $2
	`

	getChatRolloutStateQuery = `
		SELECT payload
		FROM autonomous_rollout_states
		WHERE chat_id = $1
		ORDER BY updated_at DESC
		LIMIT 1
	`
)

// AutonomousRolloutStore persists rollout state and rollback events for autonomous strategies.
type AutonomousRolloutStore struct {
	db *sql.DB
}

var _ autonomous.StrategyRepository = (*AutonomousRolloutStore)(nil)

//go:embed autonomy_rollout_migration.sql
var autonomyRolloutMigrationSQL string

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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin autonomous rollout schema transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	for _, statement := range statements {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("init autonomous rollout schema: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit autonomous rollout schema transaction: %w", err)
	}
	return nil
}

func loadAutonomousRolloutMigrationStatements() ([]string, error) {
	parts := strings.Split(autonomyRolloutMigrationSQL, ";")
	statements := make([]string, 0, len(parts))
	for _, part := range parts {
		statement := strings.TrimSpace(part)
		if statement == "" {
			continue
		}
		statements = append(statements, statement)
	}
	if len(statements) == 0 {
		return nil, fmt.Errorf("autonomous rollout migration contains no statements")
	}
	return statements, nil
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

	if _, err := s.db.ExecContext(
		ctx,
		saveRolloutStateQuery,
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
	err := s.db.QueryRowContext(ctx, getRolloutStateQuery, strategyID).Scan(&payload)
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
	if strings.TrimSpace(string(event.Trigger)) == "" {
		return fmt.Errorf("rollback event trigger is required")
	}

	occurredAt := event.Timestamp.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	event.Timestamp = occurredAt

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal rollback event: %w", err)
	}
	chatID := strategyChatID(event.StrategyID)

	if _, err := s.db.ExecContext(
		ctx,
		saveRollbackEventQuery,
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

	rows, err := s.db.QueryContext(ctx, getRollbackHistoryQuery, strategyID, limit)
	if err != nil {
		return nil, fmt.Errorf("get rollback history: %w", err)
	}
	redactedID := redactStrategyID(strategyID)
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			log.Printf("autonomous rollout store: failed to close rollback rows for strategy %s: %v", redactedID, closeErr)
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
	err := s.db.QueryRowContext(ctx, getChatRolloutStateQuery, chatID).Scan(&payload)
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

func redactStrategyID(strategyID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(strategyID)))
	return hex.EncodeToString(sum[:4])
}
