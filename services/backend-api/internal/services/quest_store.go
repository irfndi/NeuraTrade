package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

type DBQuestStore struct {
	db DBPool
}

func NewDBQuestStore(db DBPool) *DBQuestStore {
	return &DBQuestStore{db: db}
}

func (s *DBQuestStore) InitSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	_, err := s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS quests (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			type TEXT NOT NULL,
			cadence TEXT NOT NULL,
			cron_expr TEXT,
			status TEXT NOT NULL,
			prompt TEXT,
			target_count INTEGER DEFAULT 0,
			current_count INTEGER DEFAULT 0,
			checkpoint TEXT,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			last_executed_at TIMESTAMP,
			completed_at TIMESTAMP,
			last_error TEXT,
			metadata TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create quests table: %w", err)
	}

	_, err = s.db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS autonomous_state (
			chat_id TEXT PRIMARY KEY,
			is_active BOOLEAN NOT NULL,
			started_at TIMESTAMP,
			paused_at TIMESTAMP,
			active_quests TEXT,
			updated_at TIMESTAMP NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create autonomous_state table: %w", err)
	}

	_, err = s.db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_quests_status ON quests(status)`)
	if err != nil {
		return fmt.Errorf("failed to create quests status index: %w", err)
	}

	_, err = s.db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_quests_type ON quests(type)`)
	if err != nil {
		return fmt.Errorf("failed to create quests type index: %w", err)
	}

	return nil
}

func (s *DBQuestStore) SaveQuest(ctx context.Context, quest *Quest) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	checkpointJSON, _ := json.Marshal(quest.Checkpoint)
	metadataJSON, _ := json.Marshal(quest.Metadata)

	query := `
		INSERT INTO quests (
			id, name, description, type, cadence, cron_expr, status, prompt,
			target_count, current_count, checkpoint, created_at, updated_at,
			last_executed_at, completed_at, last_error, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			description = EXCLUDED.description,
			status = EXCLUDED.status,
			current_count = EXCLUDED.current_count,
			checkpoint = EXCLUDED.checkpoint,
			updated_at = EXCLUDED.updated_at,
			last_executed_at = EXCLUDED.last_executed_at,
			completed_at = EXCLUDED.completed_at,
			last_error = EXCLUDED.last_error,
			metadata = EXCLUDED.metadata
	`

	_, err := s.db.Exec(ctx, query,
		quest.ID, quest.Name, quest.Description, quest.Type, quest.Cadence, quest.CronExpr,
		quest.Status, quest.Prompt, quest.TargetCount, quest.CurrentCount, checkpointJSON,
		quest.CreatedAt, quest.UpdatedAt, quest.LastExecutedAt, quest.CompletedAt,
		quest.LastError, metadataJSON,
	)

	return err
}

func (s *DBQuestStore) GetQuest(ctx context.Context, id string) (*Quest, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var quest Quest
	var checkpointJSON, metadataJSON []byte
	var cronExpr, lastError sql.NullString
	var lastExecutedAt, completedAt sql.NullTime

	err := s.db.QueryRow(ctx, `
		SELECT id, name, description, type, cadence, cron_expr, status, prompt,
			   target_count, current_count, checkpoint, created_at, updated_at,
			   last_executed_at, completed_at, last_error, metadata
		FROM quests WHERE id = $1
	`, id).Scan(
		&quest.ID, &quest.Name, &quest.Description, &quest.Type, &quest.Cadence,
		&cronExpr, &quest.Status, &quest.Prompt, &quest.TargetCount, &quest.CurrentCount,
		&checkpointJSON, &quest.CreatedAt, &quest.UpdatedAt,
		&lastExecutedAt, &completedAt, &lastError, &metadataJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("quest not found: %w", err)
	}

	if cronExpr.Valid {
		quest.CronExpr = cronExpr.String
	}
	if lastExecutedAt.Valid {
		quest.LastExecutedAt = &lastExecutedAt.Time
	}
	if completedAt.Valid {
		quest.CompletedAt = &completedAt.Time
	}
	if lastError.Valid {
		quest.LastError = lastError.String
	}

	json.Unmarshal(checkpointJSON, &quest.Checkpoint)
	json.Unmarshal(metadataJSON, &quest.Metadata)

	return &quest, nil
}

func (s *DBQuestStore) ListQuests(ctx context.Context, chatID string, status QuestStatus) ([]*Quest, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	query := `SELECT id, name, description, type, cadence, cron_expr, status, prompt,
			  target_count, current_count, checkpoint, created_at, updated_at,
			  last_executed_at, completed_at, last_error, metadata
			  FROM quests WHERE 1=1`
	args := make([]interface{}, 0, 2)
	argIndex := 1

	if chatID != "" {
		query += fmt.Sprintf(" AND json_extract(metadata, '$.chat_id') = $%d", argIndex)
		args = append(args, chatID)
		argIndex++
	}

	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, string(status))
	}

	query += " ORDER BY created_at DESC"

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list quests: %w", err)
	}
	defer rows.Close()

	quests := make([]*Quest, 0)
	for rows.Next() {
		var quest Quest
		var checkpointJSON, metadataJSON []byte
		var cronExpr, lastError sql.NullString
		var lastExecutedAt, completedAt sql.NullTime

		err := rows.Scan(
			&quest.ID, &quest.Name, &quest.Description, &quest.Type, &quest.Cadence,
			&cronExpr, &quest.Status, &quest.Prompt, &quest.TargetCount, &quest.CurrentCount,
			&checkpointJSON, &quest.CreatedAt, &quest.UpdatedAt,
			&lastExecutedAt, &completedAt, &lastError, &metadataJSON,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan quest: %w", err)
		}

		if cronExpr.Valid {
			quest.CronExpr = cronExpr.String
		}
		if lastExecutedAt.Valid {
			quest.LastExecutedAt = &lastExecutedAt.Time
		}
		if completedAt.Valid {
			quest.CompletedAt = &completedAt.Time
		}
		if lastError.Valid {
			quest.LastError = lastError.String
		}

		json.Unmarshal(checkpointJSON, &quest.Checkpoint)
		json.Unmarshal(metadataJSON, &quest.Metadata)

		quests = append(quests, &quest)
	}

	return quests, nil
}

func (s *DBQuestStore) UpdateQuestProgress(ctx context.Context, id string, current int, checkpoint map[string]interface{}) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	checkpointJSON, _ := json.Marshal(checkpoint)

	_, err := s.db.Exec(ctx, `
		UPDATE quests SET current_count = $2, checkpoint = $3, updated_at = $4
		WHERE id = $1
	`, id, current, checkpointJSON, time.Now().UTC())

	return err
}

func (s *DBQuestStore) UpdateLastExecuted(ctx context.Context, id string, executedAt time.Time) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	_, err := s.db.Exec(ctx, `
		UPDATE quests SET last_executed_at = $2, updated_at = $3
		WHERE id = $1
	`, id, executedAt, time.Now().UTC())

	return err
}

func (s *DBQuestStore) SaveAutonomousState(ctx context.Context, state *AutonomousState) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	activeQuestsJSON, _ := json.Marshal(state.ActiveQuests)

	query := `
		INSERT INTO autonomous_state (chat_id, is_active, started_at, paused_at, active_quests, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chat_id) DO UPDATE SET
			is_active = EXCLUDED.is_active,
			started_at = EXCLUDED.started_at,
			paused_at = EXCLUDED.paused_at,
			active_quests = EXCLUDED.active_quests,
			updated_at = EXCLUDED.updated_at
	`

	_, err := s.db.Exec(ctx, query,
		state.ChatID, state.IsActive, state.StartedAt, state.PausedAt,
		activeQuestsJSON, time.Now().UTC(),
	)

	return err
}

func (s *DBQuestStore) GetAutonomousState(ctx context.Context, chatID string) (*AutonomousState, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var state AutonomousState
	var activeQuestsJSON []byte
	var startedAt, pausedAt sql.NullTime

	err := s.db.QueryRow(ctx, `
		SELECT chat_id, is_active, started_at, paused_at, active_quests
		FROM autonomous_state WHERE chat_id = $1
	`, chatID).Scan(
		&state.ChatID, &state.IsActive, &startedAt, &pausedAt, &activeQuestsJSON,
	)
	if err != nil {
		return &AutonomousState{ChatID: chatID, IsActive: false}, nil
	}

	if startedAt.Valid {
		state.StartedAt = startedAt.Time
	}
	if pausedAt.Valid {
		state.PausedAt = pausedAt.Time
	}

	json.Unmarshal(activeQuestsJSON, &state.ActiveQuests)

	return &state, nil
}

func (s *DBQuestStore) DeleteQuest(ctx context.Context, id string) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	_, err := s.db.Exec(ctx, `DELETE FROM quests WHERE id = $1`, id)
	return err
}

func (s *DBQuestStore) CountQuests(ctx context.Context, status QuestStatus) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("database connection is nil")
	}

	var count int
	var err error
	if status == "" {
		err = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM quests`).Scan(&count)
	} else {
		err = s.db.QueryRow(ctx, `SELECT COUNT(*) FROM quests WHERE status = $1`, string(status)).Scan(&count)
	}
	return count, err
}
