package risk

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/jackc/pgx/v5"
)

type PersistedKillSwitchState struct {
	Engaged      bool
	EngagedAt    time.Time
	EngagedBy    string
	Reason       string
	CancelOrders bool
	UpdatedAt    time.Time
}

type KillSwitchStore interface {
	Load(ctx context.Context) (PersistedKillSwitchState, bool, error)
	Save(ctx context.Context, s PersistedKillSwitchState) error
}

type MemoryKillSwitchStore struct {
	mu    sync.Mutex
	state *PersistedKillSwitchState
}

func NewMemoryKillSwitchStore() *MemoryKillSwitchStore {
	return &MemoryKillSwitchStore{}
}

func (m *MemoryKillSwitchStore) Load(_ context.Context) (PersistedKillSwitchState, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return PersistedKillSwitchState{}, false, nil
	}
	return *m.state, true, nil
}

func (m *MemoryKillSwitchStore) Save(_ context.Context, s PersistedKillSwitchState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.state = &s
	return nil
}

var ErrKillSwitchSchemaMissing = errors.New("risk_kill_switch_state table not present (run migrations)")

type DBExecutor interface {
	QueryRow(ctx context.Context, query string, args ...any) database.Row
	Exec(ctx context.Context, query string, args ...any) (database.Result, error)
}

type SQLKillSwitchStore struct {
	db DBExecutor
}

func NewSQLKillSwitchStore(db DBExecutor) *SQLKillSwitchStore {
	return &SQLKillSwitchStore{db: db}
}

func (s *SQLKillSwitchStore) Load(ctx context.Context) (PersistedKillSwitchState, bool, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	row := s.db.QueryRow(queryCtx, `
		SELECT engaged, engaged_at, COALESCE(engaged_by, ''), COALESCE(reason, ''), cancel_orders, last_updated_at
		FROM risk_kill_switch_state
		WHERE singleton
	`)

	var (
		engaged      int
		engagedAt    *int64
		engagedBy    string
		reason       string
		cancelOrders int
		updatedAt    int64
	)
	if err := row.Scan(&engaged, &engagedAt, &engagedBy, &reason, &cancelOrders, &updatedAt); err != nil {
		if isNoRowsErr(err) {
			return PersistedKillSwitchState{}, false, nil
		}
		if isTableMissingErr(err) {
			return PersistedKillSwitchState{}, false, ErrKillSwitchSchemaMissing
		}
		return PersistedKillSwitchState{}, false, fmt.Errorf("load kill switch state: %w", err)
	}

	state := PersistedKillSwitchState{
		Engaged:      engaged != 0,
		EngagedBy:    engagedBy,
		Reason:       reason,
		CancelOrders: cancelOrders != 0,
		UpdatedAt:    time.Unix(updatedAt, 0),
	}
	if engagedAt != nil {
		state.EngagedAt = time.Unix(*engagedAt, 0)
	}
	return state, true, nil
}

func (s *SQLKillSwitchStore) Save(ctx context.Context, st PersistedKillSwitchState) error {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var engagedAt any
	if !st.EngagedAt.IsZero() {
		engagedAt = st.EngagedAt.Unix()
	} else {
		engagedAt = nil
	}

	cancelInt := 0
	if st.CancelOrders {
		cancelInt = 1
	}
	engagedInt := 0
	if st.Engaged {
		engagedInt = 1
	}

	_, err := s.db.Exec(queryCtx, `
		INSERT INTO risk_kill_switch_state
			(singleton, engaged, engaged_at, engaged_by, reason, cancel_orders, last_updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(singleton) DO UPDATE SET
			engaged = excluded.engaged,
			engaged_at = excluded.engaged_at,
			engaged_by = excluded.engaged_by,
			reason = excluded.reason,
			cancel_orders = excluded.cancel_orders,
			last_updated_at = excluded.last_updated_at
	`, engagedInt, engagedAt, st.EngagedBy, st.Reason, cancelInt, st.UpdatedAt.Unix())

	if err != nil {
		if isTableMissingErr(err) {
			return ErrKillSwitchSchemaMissing
		}
		return fmt.Errorf("save kill switch state: %w", err)
	}
	return nil
}

func isNoRowsErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, sql.ErrNoRows) {
		return true
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return true
	}
	msg := err.Error()
	return msg == "sql: no rows in result set" || msg == "no rows in result set"
}

func isTableMissingErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "no such table") ||
		(strings.Contains(msg, "relation") && strings.Contains(msg, "does not exist")) ||
		strings.Contains(msg, "undefined table")
}
