package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	liveReadinessManifestTable         = "live_readiness_manifests"
	liveReadinessManifestStrategyTable = "live_readiness_manifest_strategies"
	liveReadinessManifestAcceptanceIdx = "idx_live_readiness_manifests_acceptance_ready"
	liveReadinessManifestCreatedIdx    = "idx_live_readiness_manifests_created_at"
	liveReadinessManifestStrategyIdx   = "idx_live_readiness_manifest_strategies_strategy"
)

// LiveReadinessManifestStore persists paper-trading readiness manifests and
// per-strategy evidence to the database so that containerised deployments can
// query readiness without relying on a shared filesystem.
type LiveReadinessManifestStore struct {
	db DBPool
}

// NewLiveReadinessManifestStore creates a store backed by the given DBPool.
func NewLiveReadinessManifestStore(db DBPool) *LiveReadinessManifestStore {
	return &LiveReadinessManifestStore{db: db}
}

// InitSchema creates the live_readiness_manifests tables and indexes if they do
// not already exist.  Call this once at startup when you are not using the
// numbered migration runner (e.g. in tests).
func (s *LiveReadinessManifestStore) InitSchema(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("database connection is nil")
	}

	_, err := s.db.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+liveReadinessManifestTable+` (
		id TEXT PRIMARY KEY,
		manifest_json TEXT NOT NULL,
		acceptance_ready BOOLEAN NOT NULL DEFAULT FALSE,
		acceptance_failures TEXT,
		created_at TIMESTAMP NOT NULL,
		updated_at TIMESTAMP NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("failed to create %s table: %w", liveReadinessManifestTable, err)
	}

	_, err = s.db.Exec(ctx, "CREATE INDEX IF NOT EXISTS "+liveReadinessManifestAcceptanceIdx+
		" ON "+liveReadinessManifestTable+"(acceptance_ready, created_at DESC)")
	if err != nil {
		return fmt.Errorf("failed to create %s index: %w", liveReadinessManifestAcceptanceIdx, err)
	}

	_, err = s.db.Exec(ctx, "CREATE INDEX IF NOT EXISTS "+liveReadinessManifestCreatedIdx+
		" ON "+liveReadinessManifestTable+"(created_at DESC)")
	if err != nil {
		return fmt.Errorf("failed to create %s index: %w", liveReadinessManifestCreatedIdx, err)
	}

	_, err = s.db.Exec(ctx, "CREATE TABLE IF NOT EXISTS "+liveReadinessManifestStrategyTable+` (
		manifest_id TEXT NOT NULL,
		strategy TEXT NOT NULL,
		strategy_json TEXT NOT NULL,
		created_at TIMESTAMP NOT NULL,
		PRIMARY KEY (manifest_id, strategy),
		FOREIGN KEY (manifest_id) REFERENCES `+liveReadinessManifestTable+`(id) ON DELETE CASCADE
	)`)
	if err != nil {
		return fmt.Errorf("failed to create %s table: %w", liveReadinessManifestStrategyTable, err)
	}

	_, err = s.db.Exec(ctx, "CREATE INDEX IF NOT EXISTS "+liveReadinessManifestStrategyIdx+
		" ON "+liveReadinessManifestStrategyTable+"(strategy, created_at DESC)")
	if err != nil {
		return fmt.Errorf("failed to create %s index: %w", liveReadinessManifestStrategyIdx, err)
	}

	return nil
}

// SaveManifest persists a PaperTradingReadinessManifest and its per-strategy
// evidence to the database.  It returns the generated manifest ID.
func (s *LiveReadinessManifestStore) SaveManifest(ctx context.Context, manifest *PaperTradingReadinessManifest) (string, error) {
	if s.db == nil {
		return "", fmt.Errorf("database connection is nil")
	}
	if manifest == nil {
		return "", fmt.Errorf("manifest is nil")
	}

	id := uuid.New().String()
	now := time.Now().UTC()

	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return "", fmt.Errorf("marshal manifest: %w", err)
	}

	failuresJSON, err := json.Marshal(manifest.Acceptance.Failures)
	if err != nil {
		return "", fmt.Errorf("marshal acceptance failures: %w", err)
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = tx.Exec(ctx,
		"INSERT INTO "+liveReadinessManifestTable+" (id, manifest_json, acceptance_ready, acceptance_failures, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6)",
		id, string(manifestJSON), manifest.Acceptance.Ready, string(failuresJSON), now, now,
	)
	if err != nil {
		return "", fmt.Errorf("insert manifest: %w", err)
	}

	for _, strat := range manifest.Strategies {
		strategyJSON, err := json.Marshal(strat)
		if err != nil {
			return "", fmt.Errorf("marshal strategy %s: %w", strat.Strategy, err)
		}
		_, err = tx.Exec(ctx,
			"INSERT INTO "+liveReadinessManifestStrategyTable+" (manifest_id, strategy, strategy_json, created_at) VALUES ($1, $2, $3, $4)",
			id, strat.Strategy, string(strategyJSON), now,
		)
		if err != nil {
			return "", fmt.Errorf("insert strategy %s: %w", strat.Strategy, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("commit transaction: %w", err)
	}

	return id, nil
}

// GetLatestManifest returns the most recently created manifest from the database.
func (s *LiveReadinessManifestStore) GetLatestManifest(ctx context.Context) (*PaperTradingReadinessManifest, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var raw string
	err := s.db.QueryRow(ctx,
		"SELECT manifest_json FROM "+liveReadinessManifestTable+" ORDER BY created_at DESC LIMIT 1",
	).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query latest manifest: %w", err)
	}

	var manifest PaperTradingReadinessManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}
	return &manifest, nil
}

// GetLatestReadyManifest returns the most recent manifest where acceptance_ready
// is true.  This is what the live-mode guard should consult.
func (s *LiveReadinessManifestStore) GetLatestReadyManifest(ctx context.Context) (*PaperTradingReadinessManifest, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}

	var raw string
	err := s.db.QueryRow(ctx,
		"SELECT manifest_json FROM "+liveReadinessManifestTable+" WHERE acceptance_ready = TRUE ORDER BY created_at DESC LIMIT 1",
	).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query latest ready manifest: %w", err)
	}

	var manifest PaperTradingReadinessManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}
	return &manifest, nil
}

// GetManifestByID fetches a single manifest by its UUID.
func (s *LiveReadinessManifestStore) GetManifestByID(ctx context.Context, id string) (*PaperTradingReadinessManifest, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("id is required")
	}

	var raw string
	err := s.db.QueryRow(ctx,
		"SELECT manifest_json FROM "+liveReadinessManifestTable+" WHERE id = $1",
		id,
	).Scan(&raw)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("query manifest by id: %w", err)
	}

	var manifest PaperTradingReadinessManifest
	if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}
	return &manifest, nil
}

// ListManifests returns up to limit manifests ordered by created_at DESC.
func (s *LiveReadinessManifestStore) ListManifests(ctx context.Context, limit int) ([]PaperTradingReadinessManifest, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database connection is nil")
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 500 {
		limit = 500
	}

	rows, err := s.db.Query(ctx,
		"SELECT manifest_json FROM "+liveReadinessManifestTable+" ORDER BY created_at DESC LIMIT $1", limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list manifests: %w", err)
	}
	defer rows.Close()

	manifests := make([]PaperTradingReadinessManifest, 0, limit)
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, fmt.Errorf("scan manifest: %w", err)
		}
		var manifest PaperTradingReadinessManifest
		if err := json.Unmarshal([]byte(raw), &manifest); err != nil {
			return nil, fmt.Errorf("unmarshal manifest: %w", err)
		}
		manifests = append(manifests, manifest)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate manifests: %w", err)
	}
	return manifests, nil
}

// CountManifests returns the total number of persisted manifests.
func (s *LiveReadinessManifestStore) CountManifests(ctx context.Context) (int, error) {
	if s.db == nil {
		return 0, fmt.Errorf("database connection is nil")
	}

	var count int
	err := s.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM "+liveReadinessManifestTable,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count manifests: %w", err)
	}
	return count, nil
}

// HasReadyManifest returns true if at least one manifest with acceptance_ready
// exists in the database.
func (s *LiveReadinessManifestStore) HasReadyManifest(ctx context.Context) (bool, error) {
	if s.db == nil {
		return false, fmt.Errorf("database connection is nil")
	}

	var count int
	err := s.db.QueryRow(ctx,
		"SELECT COUNT(*) FROM "+liveReadinessManifestTable+" WHERE acceptance_ready = TRUE",
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check ready manifest: %w", err)
	}
	return count > 0, nil
}
