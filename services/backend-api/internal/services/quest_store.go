package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	runtimeQuestTable      = "autonomous_quests"
	runtimeStateTable      = "autonomous_state_runtime"
	runtimeQuestStatusIdx  = "idx_autonomous_quests_status"
	runtimeQuestTypeIdx    = "idx_autonomous_quests_type"
	runtimeStateUpdatedIdx = "idx_autonomous_state_runtime_updated_at"
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

	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
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
	`, runtimeQuestTable))
	if err != nil {
		return fmt.Errorf("failed to create %s table: %w", runtimeQuestTable, err)
	}

	_, err = s.db.Exec(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			chat_id TEXT PRIMARY KEY,
			is_active BOOLEAN NOT NULL,
			started_at TIMESTAMP,
			paused_at TIMESTAMP,
			active_quests TEXT,
			updated_at TIMESTAMP NOT NULL
		)
	`, runtimeStateTable))
	if err != nil {
		return fmt.Errorf("failed to create %s table: %w", runtimeStateTable, err)
	}

	_, err = s.db.Exec(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(status)`, runtimeQuestStatusIdx, runtimeQuestTable))
	if err != nil {
		return fmt.Errorf("failed to create runtime quest status index: %w", err)
	}

	_, err = s.db.Exec(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(type)`, runtimeQuestTypeIdx, runtimeQuestTable))
	if err != nil {
		return fmt.Errorf("failed to create runtime quest type index: %w", err)
	}

	_, err = s.db.Exec(ctx, fmt.Sprintf(`CREATE INDEX IF NOT EXISTS %s ON %s(updated_at)`, runtimeStateUpdatedIdx, runtimeStateTable))
	if err != nil {
		return fmt.Errorf("failed to create runtime autonomous_state updated_at index: %w", err)
	}

	return nil
}

func (s *DBQuestStore) SaveQuest(ctx context.Context, quest *Quest) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	checkpointJSON, _ := json.Marshal(quest.Checkpoint)
	metadataJSON, _ := json.Marshal(quest.Metadata)

	query := fmt.Sprintf(`
		INSERT INTO %s (
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
	`, runtimeQuestTable)

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

	err := s.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT id, name, description, type, cadence, cron_expr, status, prompt,
			   target_count, current_count, checkpoint, created_at, updated_at,
			   last_executed_at, completed_at, last_error, metadata
		FROM %s WHERE id = $1
	`, runtimeQuestTable), id).Scan(
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

	if err := json.Unmarshal(checkpointJSON, &quest.Checkpoint); err != nil {
		return nil, fmt.Errorf("failed to unmarshal checkpoint: %w", err)
	}
	if err := json.Unmarshal(metadataJSON, &quest.Metadata); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &quest, nil
}

func (s *DBQuestStore) ListQuests(ctx context.Context, chatID string, status QuestStatus) ([]*Quest, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	query := fmt.Sprintf(`SELECT id, name, description, type, cadence, cron_expr, status, prompt,
			  target_count, current_count, checkpoint, created_at, updated_at,
			  last_executed_at, completed_at, last_error, metadata
			  FROM %s WHERE 1=1`, runtimeQuestTable)
	args := make([]interface{}, 0, 1)
	argIndex := 1

	if status != "" {
		query += fmt.Sprintf(" AND status = $%d", argIndex)
		args = append(args, string(status))
		argIndex++
	}

	if strings.TrimSpace(chatID) != "" {
		query += fmt.Sprintf(" AND metadata LIKE $%d ESCAPE '\\\\'", argIndex)
		args = append(args, metadataChatIDLikePattern(chatID))
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

		if err := json.Unmarshal(checkpointJSON, &quest.Checkpoint); err != nil {
			return nil, fmt.Errorf("failed to unmarshal checkpoint: %w", err)
		}
		if err := json.Unmarshal(metadataJSON, &quest.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}

		if strings.TrimSpace(chatID) != "" && strings.TrimSpace(quest.Metadata["chat_id"]) != strings.TrimSpace(chatID) {
			continue
		}
		quests = append(quests, &quest)
	}

	return quests, nil
}

func metadataChatIDLikePattern(chatID string) string {
	escaped := strings.TrimSpace(chatID)
	escaped = strings.ReplaceAll(escaped, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `%`, `\%`)
	escaped = strings.ReplaceAll(escaped, `_`, `\_`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)
	return fmt.Sprintf("%%\"chat_id\":\"%s\"%%", escaped)
}

func (s *DBQuestStore) UpdateQuestProgress(ctx context.Context, id string, current int, checkpoint map[string]interface{}) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	checkpointJSON, _ := json.Marshal(checkpoint)

	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET current_count = $2, checkpoint = $3, updated_at = $4
		WHERE id = $1
	`, runtimeQuestTable), id, current, checkpointJSON, time.Now().UTC())

	return err
}

func (s *DBQuestStore) UpdateLastExecuted(ctx context.Context, id string, executedAt time.Time) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	_, err := s.db.Exec(ctx, fmt.Sprintf(`
		UPDATE %s SET last_executed_at = $2, updated_at = $3
		WHERE id = $1
	`, runtimeQuestTable), id, executedAt, time.Now().UTC())

	return err
}

func (s *DBQuestStore) SaveAutonomousState(ctx context.Context, state *AutonomousState) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	activeQuestsJSON, _ := json.Marshal(state.ActiveQuests)

	query := fmt.Sprintf(`
		INSERT INTO %s (chat_id, is_active, started_at, paused_at, active_quests, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (chat_id) DO UPDATE SET
			is_active = EXCLUDED.is_active,
			started_at = EXCLUDED.started_at,
			paused_at = EXCLUDED.paused_at,
			active_quests = EXCLUDED.active_quests,
			updated_at = EXCLUDED.updated_at
	`, runtimeStateTable)

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

	err := s.db.QueryRow(ctx, fmt.Sprintf(`
		SELECT chat_id, is_active, started_at, paused_at, active_quests
		FROM %s WHERE chat_id = $1
	`, runtimeStateTable), chatID).Scan(
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

	if err := json.Unmarshal(activeQuestsJSON, &state.ActiveQuests); err != nil {
		return nil, fmt.Errorf("failed to unmarshal active quests: %w", err)
	}

	return &state, nil
}

func (s *DBQuestStore) DeleteQuest(ctx context.Context, id string) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	_, err := s.db.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, runtimeQuestTable), id)
	return err
}

func (s *DBQuestStore) CountQuests(ctx context.Context, status QuestStatus) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("database connection is nil")
	}

	var count int
	var err error
	if status == "" {
		err = s.db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, runtimeQuestTable)).Scan(&count)
	} else {
		err = s.db.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE status = $1`, runtimeQuestTable), string(status)).Scan(&count)
	}
	return count, err
}
