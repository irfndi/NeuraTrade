package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ExchangeBlacklistEntry represents a blacklisted exchange in the database.
type ExchangeBlacklistEntry struct {
	// ID is the unique identifier.
	ID int64 `json:"id" db:"id"`
	// ExchangeName is the name of the exchange.
	ExchangeName string `json:"exchange_name" db:"exchange_name"`
	// Reason describes why the exchange was blacklisted.
	Reason string `json:"reason" db:"reason"`
	// CreatedAt is when the entry was created.
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	// UpdatedAt is when the entry was last updated.
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
	// ExpiresAt is when the blacklist expires (nil for never).
	ExpiresAt *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	// IsActive indicates if the blacklist entry is currently active.
	IsActive bool `json:"is_active" db:"is_active"`
}

type DatabasePool interface {
	QueryRow(ctx context.Context, sql string, args ...interface{}) Row
	Exec(ctx context.Context, sql string, args ...interface{}) (Result, error)
	Query(ctx context.Context, sql string, args ...interface{}) (Rows, error)
}

// BlacklistRepository handles database operations for exchange blacklist.
type BlacklistRepository struct {
	pool DatabasePool
}

// NewBlacklistRepository creates a new blacklist repository.
//
// Parameters:
//
//	pool: The database connection pool.
//
// Returns:
//
//	*BlacklistRepository: The initialized repository.
func NewBlacklistRepository(pool DatabasePool) *BlacklistRepository {
	return &BlacklistRepository{
		pool: pool,
	}
}

// AddExchange adds an exchange to the blacklist.
//
// Parameters:
//
//	ctx: Context.
//	exchangeName: Exchange name.
//	reason: Reason for blacklisting.
//	expiresAt: Expiration time.
//
// Returns:
//
//	*ExchangeBlacklistEntry: The created entry.
//	error: Error if operation fails.
func (r *BlacklistRepository) AddExchange(ctx context.Context, exchangeName, reason string, expiresAt *time.Time) (*ExchangeBlacklistEntry, error) {
	query := `
		INSERT INTO exchange_blacklist (exchange_name, reason, expires_at, is_active)
		VALUES ($1, $2, $3, true)
		ON CONFLICT (exchange_name) WHERE is_active = true
		DO UPDATE SET 
			reason = EXCLUDED.reason,
			expires_at = EXCLUDED.expires_at,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, exchange_name, reason, created_at, updated_at, expires_at, is_active
	`

	var entry ExchangeBlacklistEntry
	err := r.pool.QueryRow(ctx, query, exchangeName, reason, expiresAt).Scan(
		&entry.ID,
		&entry.ExchangeName,
		&entry.Reason,
		&entry.CreatedAt,
		&entry.UpdatedAt,
		&entry.ExpiresAt,
		&entry.IsActive,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to add exchange to blacklist: %w", err)
	}

	return &entry, nil
}

// RemoveExchange removes an exchange from the blacklist.
//
// Parameters:
//
//	ctx: Context.
//	exchangeName: Exchange name.
//
// Returns:
//
//	error: Error if operation fails.
func (r *BlacklistRepository) RemoveExchange(ctx context.Context, exchangeName string) error {
	query := `
		UPDATE exchange_blacklist 
		SET is_active = false, updated_at = CURRENT_TIMESTAMP
		WHERE exchange_name = $1 AND is_active = true
	`

	result, err := r.pool.Exec(ctx, query, exchangeName)
	if err != nil {
		return fmt.Errorf("failed to remove exchange from blacklist: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return fmt.Errorf("exchange %s not found in blacklist or already inactive", exchangeName)
	}

	return nil
}

// IsBlacklisted checks if an exchange is currently blacklisted.
//
// Parameters:
//
//	ctx: Context.
//	exchangeName: Exchange name.
//
// Returns:
//
//	bool: True if blacklisted.
//	string: Reason for blacklisting.
//	error: Error if check fails.
func (r *BlacklistRepository) IsBlacklisted(ctx context.Context, exchangeName string) (bool, string, error) {
	query := `
		SELECT reason, expires_at
		FROM exchange_blacklist 
		WHERE exchange_name = $1 AND is_active = true
		AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
	`

	var reason string
	var expiresAt *time.Time
	err := r.pool.QueryRow(ctx, query, exchangeName).Scan(&reason, &expiresAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, "", nil
		}
		return false, "", fmt.Errorf("failed to check blacklist status: %w", err)
	}

	return true, reason, nil
}

// GetAllBlacklisted returns all currently blacklisted exchanges.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	[]ExchangeBlacklistEntry: List of blacklist entries.
//	error: Error if retrieval fails.
func (r *BlacklistRepository) GetAllBlacklisted(ctx context.Context) ([]ExchangeBlacklistEntry, error) {
	query := `
		SELECT id, exchange_name, reason, created_at, updated_at, expires_at, is_active
		FROM exchange_blacklist 
		WHERE is_active = true
		AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		ORDER BY created_at DESC
	`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to get blacklisted exchanges: %w", err)
	}
	defer rows.Close()

	var entries []ExchangeBlacklistEntry
	for rows.Next() {
		var entry ExchangeBlacklistEntry
		err := rows.Scan(
			&entry.ID,
			&entry.ExchangeName,
			&entry.Reason,
			&entry.CreatedAt,
			&entry.UpdatedAt,
			&entry.ExpiresAt,
			&entry.IsActive,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan blacklist entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blacklist entries: %w", err)
	}

	return entries, nil
}

// CleanupExpired removes expired blacklist entries.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	int64: Number of entries deactivated.
//	error: Error if cleanup fails.
func (r *BlacklistRepository) CleanupExpired(ctx context.Context) (int64, error) {
	query := `
		UPDATE exchange_blacklist 
		SET is_active = false, updated_at = CURRENT_TIMESTAMP
		WHERE is_active = true 
		AND expires_at IS NOT NULL 
		AND expires_at <= CURRENT_TIMESTAMP
	`

	result, err := r.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired blacklist entries: %w", err)
	}

	return result.RowsAffected()
}

// GetBlacklistHistory returns the history of blacklist changes.
//
// Parameters:
//
//	ctx: Context.
//	limit: Maximum number of entries.
//
// Returns:
//
//	[]ExchangeBlacklistEntry: History entries.
//	error: Error if retrieval fails.
func (r *BlacklistRepository) GetBlacklistHistory(ctx context.Context, limit int) ([]ExchangeBlacklistEntry, error) {
	query := `
		SELECT id, exchange_name, reason, created_at, updated_at, expires_at, is_active
		FROM exchange_blacklist 
		ORDER BY updated_at DESC
		LIMIT $1
	`

	rows, err := r.pool.Query(ctx, query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get blacklist history: %w", err)
	}
	defer rows.Close()

	var entries []ExchangeBlacklistEntry
	for rows.Next() {
		var entry ExchangeBlacklistEntry
		err := rows.Scan(
			&entry.ID,
			&entry.ExchangeName,
			&entry.Reason,
			&entry.CreatedAt,
			&entry.UpdatedAt,
			&entry.ExpiresAt,
			&entry.IsActive,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan blacklist history entry: %w", err)
		}
		entries = append(entries, entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating blacklist history: %w", err)
	}

	return entries, nil
}

// ClearAll deactivates all blacklist entries.
//
// Parameters:
//
//	ctx: Context.
//
// Returns:
//
//	int64: Number of entries deactivated.
//	error: Error if operation fails.
func (r *BlacklistRepository) ClearAll(ctx context.Context) (int64, error) {
	query := `
		UPDATE exchange_blacklist 
		SET is_active = false, updated_at = CURRENT_TIMESTAMP
		WHERE is_active = true
	`

	result, err := r.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to clear all blacklist entries: %w", err)
	}

	return result.RowsAffected()
}
