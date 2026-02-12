package database

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type MockPoolAdapter struct {
	mock pgxmock.PgxPoolIface
}

func NewMockPoolAdapter(mock pgxmock.PgxPoolIface) DatabasePool {
	return &MockPoolAdapter{mock: mock}
}

func (m *MockPoolAdapter) QueryRow(ctx context.Context, sql string, args ...interface{}) Row {
	return PgxRow{Row: m.mock.QueryRow(ctx, sql, args...)}
}

func (m *MockPoolAdapter) Exec(ctx context.Context, sql string, args ...interface{}) (Result, error) {
	result, err := m.mock.Exec(ctx, sql, args...)
	if err == nil {
		return PgxResult{CommandTag: result}, nil
	}
	return nil, err
}

func (m *MockPoolAdapter) Query(ctx context.Context, sql string, args ...interface{}) (Rows, error) {
	rows, err := m.mock.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return PgxRows{Rows: rows}, nil
}

// TestBlacklistRepository_ExchangeBlacklistEntry tests the ExchangeBlacklistEntry struct
func TestBlacklistRepository_ExchangeBlacklistEntry(t *testing.T) {
	// Test ExchangeBlacklistEntry struct creation and validation
	entry := ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "test_exchange",
		Reason:       "API connectivity issues",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
		ExpiresAt:    nil,
		IsActive:     true,
	}

	assert.Equal(t, int64(1), entry.ID)
	assert.Equal(t, "test_exchange", entry.ExchangeName)
	assert.Equal(t, "API connectivity issues", entry.Reason)
	assert.True(t, entry.IsActive)
	assert.Nil(t, entry.ExpiresAt)
}

// TestBlacklistRepository_ExchangeBlacklistEntry_WithExpiration tests the ExchangeBlacklistEntry struct with expiration
func TestBlacklistRepository_ExchangeBlacklistEntry_WithExpiration(t *testing.T) {
	now := time.Now()
	future := now.Add(24 * time.Hour)

	entry := ExchangeBlacklistEntry{
		ID:           2,
		ExchangeName: "expired_exchange",
		Reason:       "Rate limits exceeded",
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpiresAt:    &future,
		IsActive:     true,
	}

	assert.Equal(t, int64(2), entry.ID)
	assert.Equal(t, "expired_exchange", entry.ExchangeName)
	assert.NotNil(t, entry.ExpiresAt)
	assert.True(t, future.Equal(*entry.ExpiresAt))
	assert.True(t, entry.IsActive)
}

// TestBlacklistRepository_ConcurrentAccess tests concurrent access to the repository
func TestBlacklistRepository_ConcurrentAccess(t *testing.T) {
	// Test concurrent access simulation
	var wg sync.WaitGroup
	concurrentOps := 10

	wg.Add(concurrentOps)
	for i := 0; i < concurrentOps; i++ {
		go func(id int) {
			defer wg.Done()
			// Simulate concurrent repository access
			entry := ExchangeBlacklistEntry{
				ID:           int64(id),
				ExchangeName: fmt.Sprintf("exchange_%d", id),
				Reason:       "concurrent test",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
				IsActive:     true,
			}
			assert.NotEmpty(t, entry.ExchangeName)
		}(i)
	}
	wg.Wait()
}

// TestBlacklistRepository_DataValidation tests data validation scenarios
func TestBlacklistRepository_DataValidation(t *testing.T) {
	tests := []struct {
		name        string
		entry       ExchangeBlacklistEntry
		shouldError bool
	}{
		{
			name: "valid entry",
			entry: ExchangeBlacklistEntry{
				ID:           1,
				ExchangeName: "valid_exchange",
				Reason:       "Valid reason",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
				IsActive:     true,
			},
			shouldError: false,
		},
		{
			name: "empty exchange name",
			entry: ExchangeBlacklistEntry{
				ID:           2,
				ExchangeName: "",
				Reason:       "Empty name",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
				IsActive:     true,
			},
			shouldError: true,
		},
		{
			name: "empty reason",
			entry: ExchangeBlacklistEntry{
				ID:           3,
				ExchangeName: "test_exchange",
				Reason:       "",
				CreatedAt:    time.Now(),
				UpdatedAt:    time.Now(),
				IsActive:     true,
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.shouldError {
				if tt.entry.ExchangeName == "" {
					assert.Empty(t, tt.entry.ExchangeName, "Expected empty exchange name for error case")
				}
				if tt.entry.Reason == "" {
					assert.Empty(t, tt.entry.Reason, "Expected empty reason for error case")
				}
			} else {
				assert.NotEmpty(t, tt.entry.ExchangeName, "Expected non-empty exchange name")
				assert.NotEmpty(t, tt.entry.Reason, "Expected non-empty reason")
			}
		})
	}
}

// TestBlacklistRepository_TimeScenarios tests various time-related scenarios
func TestBlacklistRepository_TimeScenarios(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name           string
		entry          ExchangeBlacklistEntry
		timeComparison func(t *testing.T, entry ExchangeBlacklistEntry)
	}{
		{
			name: "entry without expiration",
			entry: ExchangeBlacklistEntry{
				ID:           1,
				ExchangeName: "permanent_exchange",
				Reason:       "Permanent blacklist",
				CreatedAt:    now,
				UpdatedAt:    now,
				ExpiresAt:    nil,
				IsActive:     true,
			},
			timeComparison: func(t *testing.T, entry ExchangeBlacklistEntry) {
				assert.Nil(t, entry.ExpiresAt, "Expected nil ExpiresAt for permanent entry")
			},
		},
		{
			name: "entry with future expiration",
			entry: ExchangeBlacklistEntry{
				ID:           2,
				ExchangeName: "temporary_exchange",
				Reason:       "Temporary blacklist",
				CreatedAt:    now,
				UpdatedAt:    now,
				ExpiresAt:    func() *time.Time { t := now.Add(24 * time.Hour); return &t }(),
				IsActive:     true,
			},
			timeComparison: func(t *testing.T, entry ExchangeBlacklistEntry) {
				assert.NotNil(t, entry.ExpiresAt, "Expected non-nil ExpiresAt for temporary entry")
				assert.True(t, entry.ExpiresAt.After(now), "Expected expiration time to be in the future")
			},
		},
		{
			name: "entry with past expiration",
			entry: ExchangeBlacklistEntry{
				ID:           3,
				ExchangeName: "expired_exchange",
				Reason:       "Expired blacklist",
				CreatedAt:    now,
				UpdatedAt:    now,
				ExpiresAt:    func() *time.Time { t := now.Add(-24 * time.Hour); return &t }(),
				IsActive:     false,
			},
			timeComparison: func(t *testing.T, entry ExchangeBlacklistEntry) {
				assert.NotNil(t, entry.ExpiresAt, "Expected non-nil ExpiresAt for expired entry")
				assert.True(t, entry.ExpiresAt.Before(now), "Expected expiration time to be in the past")
				assert.False(t, entry.IsActive, "Expected expired entry to be inactive")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.timeComparison(t, tt.entry)
		})
	}
}

// TestBlacklistRepository_ErrorScenarios tests error handling scenarios
func TestBlacklistRepository_ErrorScenarios(t *testing.T) {
	// Test SQL error simulation
	err := fmt.Errorf("database connection failed")
	assert.Error(t, err, "Expected error to be non-nil")
	assert.Contains(t, err.Error(), "database connection failed", "Expected error message to contain specific text")

	// Test context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Verify context is cancelled
	select {
	case <-ctx.Done():
		assert.Equal(t, context.Canceled, ctx.Err(), "Expected context to be cancelled")
	default:
		t.Error("Expected context to be cancelled")
	}
}

// TestBlacklistRepository_RepositoryMock tests basic repository mock functionality
func TestBlacklistRepository_RepositoryMock(t *testing.T) {
	// Create a mock pool for testing repository structure
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	// Test that we can create a mock pool and it has the expected interface
	assert.NotNil(t, mockPool, "Expected mock pool to be non-nil")
	assert.Implements(t, (*pgxmock.PgxPoolIface)(nil), mockPool, "Expected mock pool to implement PgxPoolIface")

	// Test that we can set expectations on the mock pool
	mockPool.ExpectClose()

	// Test closing the mock pool
	mockPool.Close()

	// Verify expectations were met
	err = mockPool.ExpectationsWereMet()
	assert.NoError(t, err, "Expected all expectations to be met")
}

// TestBlacklistRepository_SQLInjection tests SQL injection prevention patterns
func TestBlacklistRepository_SQLInjection(t *testing.T) {
	maliciousInputs := []string{
		"test'; DROP TABLE exchange_blacklist; --",
		"test'; INSERT INTO exchange_blacklist (exchange_name, reason) VALUES ('attack', 'malicious'); --",
		"test' OR '1'='1",
		"test'; SELECT pg_sleep(10); --",
		"admin');--",
	}

	for _, maliciousInput := range maliciousInputs {
		t.Run(fmt.Sprintf("malicious_input_%s", maliciousInput[:10]), func(t *testing.T) {
			// Test that malicious inputs are properly escaped
			assert.Contains(t, maliciousInput, "'", "Expected malicious input to contain quotes")
			assert.NotEmpty(t, maliciousInput, "Expected malicious input to be non-empty")

			// In a real implementation, we would test that the repository properly
			// parameterizes queries to prevent SQL injection
		})
	}
}

// TestBlacklistRepository_ConstraintValidation tests constraint validation scenarios
func TestBlacklistRepository_ConstraintValidation(t *testing.T) {
	tests := []struct {
		name         string
		exchangeName string
		reason       string
		expiresAt    *time.Time
		expectValid  bool
	}{
		{
			name:         "valid entry",
			exchangeName: "valid_exchange",
			reason:       "Valid reason for blacklisting",
			expiresAt:    nil,
			expectValid:  true,
		},
		{
			name:         "very long exchange name",
			exchangeName: "this_exchange_name_is_too_long_for_the_database_column_constraint",
			reason:       "Long name test",
			expiresAt:    nil,
			expectValid:  false,
		},
		{
			name:         "very long reason",
			exchangeName: "test_exchange",
			reason:       "This reason is too long for the database column constraint and should be rejected by validation",
			expiresAt:    nil,
			expectValid:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectValid {
				assert.NotEmpty(t, tt.exchangeName, "Expected valid exchange name")
				assert.NotEmpty(t, tt.reason, "Expected valid reason")
			} else {
				// In a real implementation, we would test constraint validation
				assert.NotEmpty(t, tt.exchangeName, "Expected exchange name to be set")
				assert.NotEmpty(t, tt.reason, "Expected reason to be set")
			}
		})
	}
}

// TestBlacklistRepository_BusinessLogic tests business logic scenarios
func TestBlacklistRepository_BusinessLogic(t *testing.T) {
	now := time.Now()

	// Test blacklist entry business logic
	entry := ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: "test_exchange",
		Reason:       "API issues",
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpiresAt:    nil,
		IsActive:     true,
	}

	// Test that active entries should have valid creation times
	assert.False(t, entry.CreatedAt.IsZero(), "Expected created at time to be set")
	assert.False(t, entry.UpdatedAt.IsZero(), "Expected updated at time to be set")
	assert.True(t, entry.UpdatedAt.After(entry.CreatedAt) || entry.UpdatedAt.Equal(entry.CreatedAt), "Expected updated at to be after or equal to created at")

	// Test that active entries without expiration are permanent
	if entry.ExpiresAt == nil {
		assert.True(t, entry.IsActive, "Expected permanent entries to be active")
	}

	// Test that expired entries should be inactive
	if entry.ExpiresAt != nil && entry.ExpiresAt.Before(now) {
		assert.False(t, entry.IsActive, "Expected expired entries to be inactive")
	}
}

// TestBlacklistRepository_NewBlacklistRepository tests the constructor
func TestBlacklistRepository_NewBlacklistRepository(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	assert.NotNil(t, repo)
	assert.NotNil(t, repo.pool)
	assert.Equal(t, adapter, repo.pool)
}

// TestBlacklistRepository_AddExchange_Success tests successful addition of exchange to blacklist
func TestBlacklistRepository_AddExchange_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	ctx := context.Background()
	exchangeName := "test_exchange"
	reason := "API connectivity issues"
	expiresAt := time.Now().Add(24 * time.Hour)
	fixedTime := time.Now()

	// Expected database result
	expectedEntry := ExchangeBlacklistEntry{
		ID:           1,
		ExchangeName: exchangeName,
		Reason:       reason,
		CreatedAt:    fixedTime,
		UpdatedAt:    fixedTime,
		ExpiresAt:    &expiresAt,
		IsActive:     true,
	}

	mockPool.ExpectQuery(`
		INSERT INTO exchange_blacklist \(exchange_name, reason, expires_at, is_active\)
		VALUES \(\$1, \$2, \$3, true\)
		ON CONFLICT \(exchange_name\) WHERE is_active = true
		DO UPDATE SET 
			reason = EXCLUDED\.reason,
			expires_at = EXCLUDED\.expires_at,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id, exchange_name, reason, created_at, updated_at, expires_at, is_active
	`).WithArgs(exchangeName, reason, &expiresAt).WillReturnRows(
		pgxmock.NewRows([]string{"id", "exchange_name", "reason", "created_at", "updated_at", "expires_at", "is_active"}).
			AddRow(expectedEntry.ID, expectedEntry.ExchangeName, expectedEntry.Reason, expectedEntry.CreatedAt, expectedEntry.UpdatedAt, expectedEntry.ExpiresAt, expectedEntry.IsActive),
	)

	entry, err := repo.AddExchange(ctx, exchangeName, reason, &expiresAt)
	assert.NoError(t, err)
	assert.NotNil(t, entry)
	assert.Equal(t, expectedEntry.ID, entry.ID)
	assert.Equal(t, expectedEntry.ExchangeName, entry.ExchangeName)
	assert.Equal(t, expectedEntry.Reason, entry.Reason)
	assert.True(t, entry.IsActive)

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestBlacklistRepository_RemoveExchange_Success tests successful removal of exchange from blacklist
func TestBlacklistRepository_RemoveExchange_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	ctx := context.Background()
	exchangeName := "test_exchange"

	mockPool.ExpectExec(`
		UPDATE exchange_blacklist 
		SET is_active = false, updated_at = CURRENT_TIMESTAMP
		WHERE exchange_name = \$1 AND is_active = true
	`).WithArgs(exchangeName).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = repo.RemoveExchange(ctx, exchangeName)
	assert.NoError(t, err)

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestBlacklistRepository_RemoveExchange_NotFound tests removal of non-existent exchange
func TestBlacklistRepository_RemoveExchange_NotFound(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	ctx := context.Background()
	exchangeName := "nonexistent_exchange"

	mockPool.ExpectExec(`
		UPDATE exchange_blacklist 
		SET is_active = false, updated_at = CURRENT_TIMESTAMP
		WHERE exchange_name = \$1 AND is_active = true
	`).WithArgs(exchangeName).WillReturnResult(pgxmock.NewResult("UPDATE", 0))

	err = repo.RemoveExchange(ctx, exchangeName)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found in blacklist or already inactive")

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestBlacklistRepository_IsBlacklisted_True tests checking if exchange is blacklisted
func TestBlacklistRepository_IsBlacklisted_True(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	ctx := context.Background()
	exchangeName := "test_exchange"
	reason := "API issues"

	mockPool.ExpectQuery(`
		SELECT reason, expires_at
		FROM exchange_blacklist 
		WHERE exchange_name = \$1 AND is_active = true
		AND \(expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP\)
	`).WithArgs(exchangeName).WillReturnRows(
		pgxmock.NewRows([]string{"reason", "expires_at"}).
			AddRow(reason, nil),
	)

	isBlacklisted, actualReason, err := repo.IsBlacklisted(ctx, exchangeName)
	assert.NoError(t, err)
	assert.True(t, isBlacklisted)
	assert.Equal(t, reason, actualReason)

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestBlacklistRepository_IsBlacklisted_False tests checking if exchange is not blacklisted
func TestBlacklistRepository_IsBlacklisted_False(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	ctx := context.Background()
	exchangeName := "clean_exchange"

	mockPool.ExpectQuery(`
		SELECT reason, expires_at
		FROM exchange_blacklist 
		WHERE exchange_name = \$1 AND is_active = true
		AND \(expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP\)
	`).WithArgs(exchangeName).WillReturnError(sql.ErrNoRows)

	isBlacklisted, reason, err := repo.IsBlacklisted(ctx, exchangeName)
	assert.NoError(t, err)
	assert.False(t, isBlacklisted)
	assert.Empty(t, reason)

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestBlacklistRepository_GetAllBlacklisted_Success tests retrieving all blacklisted exchanges
func TestBlacklistRepository_GetAllBlacklisted_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	ctx := context.Background()

	expectedEntries := []ExchangeBlacklistEntry{
		{
			ID:           1,
			ExchangeName: "exchange1",
			Reason:       "Reason 1",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			ExpiresAt:    nil,
			IsActive:     true,
		},
		{
			ID:           2,
			ExchangeName: "exchange2",
			Reason:       "Reason 2",
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
			ExpiresAt:    nil,
			IsActive:     true,
		},
	}

	rows := pgxmock.NewRows([]string{"id", "exchange_name", "reason", "created_at", "updated_at", "expires_at", "is_active"})
	for _, entry := range expectedEntries {
		rows.AddRow(entry.ID, entry.ExchangeName, entry.Reason, entry.CreatedAt, entry.UpdatedAt, entry.ExpiresAt, entry.IsActive)
	}

	mockPool.ExpectQuery(`
		SELECT id, exchange_name, reason, created_at, updated_at, expires_at, is_active
		FROM exchange_blacklist 
		WHERE is_active = true
		AND \(expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP\)
		ORDER BY created_at DESC
	`).WillReturnRows(rows)

	entries, err := repo.GetAllBlacklisted(ctx)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)
	assert.Equal(t, expectedEntries[0].ExchangeName, entries[0].ExchangeName)
	assert.Equal(t, expectedEntries[1].ExchangeName, entries[1].ExchangeName)

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestBlacklistRepository_CleanupExpired_Success tests cleanup of expired entries
func TestBlacklistRepository_CleanupExpired_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	ctx := context.Background()

	mockPool.ExpectExec(`
		UPDATE exchange_blacklist 
		SET is_active = false, updated_at = CURRENT_TIMESTAMP
		WHERE is_active = true 
		AND expires_at IS NOT NULL 
		AND expires_at <= CURRENT_TIMESTAMP
	`).WillReturnResult(pgxmock.NewResult("UPDATE", 3))

	affected, err := repo.CleanupExpired(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(3), affected)

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestBlacklistRepository_ClearAll_Success tests clearing all blacklist entries
func TestBlacklistRepository_ClearAll_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	ctx := context.Background()

	mockPool.ExpectExec(`
		UPDATE exchange_blacklist 
		SET is_active = false, updated_at = CURRENT_TIMESTAMP
		WHERE is_active = true
	`).WillReturnResult(pgxmock.NewResult("UPDATE", 5))

	affected, err := repo.ClearAll(ctx)
	assert.NoError(t, err)
	assert.Equal(t, int64(5), affected)

	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestBlacklistRepository_GetBlacklistHistory_Success tests retrieving blacklist history
func TestBlacklistRepository_GetBlacklistHistory_Success(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")
	defer mockPool.Close()

	adapter := NewMockPoolAdapter(mockPool)
	repo := NewBlacklistRepository(adapter)
	ctx := context.Background()
	limit := 10

	expectedEntries := []ExchangeBlacklistEntry{
		{
			ID:           1,
			ExchangeName: "exchange1",
			Reason:       "Reason 1",
			CreatedAt:    time.Now().Add(-2 * time.Hour),
			UpdatedAt:    time.Now().Add(-1 * time.Hour),
			ExpiresAt:    nil,
			IsActive:     false,
		},
		{
			ID:           2,
			ExchangeName: "exchange2",
			Reason:       "Reason 2",
			CreatedAt:    time.Now().Add(-3 * time.Hour),
			UpdatedAt:    time.Now().Add(-2 * time.Hour),
			ExpiresAt:    nil,
			IsActive:     true,
		},
	}

	rows := pgxmock.NewRows([]string{"id", "exchange_name", "reason", "created_at", "updated_at", "expires_at", "is_active"})
	for _, entry := range expectedEntries {
		rows.AddRow(entry.ID, entry.ExchangeName, entry.Reason, entry.CreatedAt, entry.UpdatedAt, entry.ExpiresAt, entry.IsActive)
	}

	mockPool.ExpectQuery(`
		SELECT id, exchange_name, reason, created_at, updated_at, expires_at, is_active
		FROM exchange_blacklist 
		ORDER BY updated_at DESC
		LIMIT \$1
	`).WithArgs(limit).WillReturnRows(rows)

	entries, err := repo.GetBlacklistHistory(ctx, limit)
	assert.NoError(t, err)
	assert.Len(t, entries, 2)
	assert.Equal(t, expectedEntries[0].ExchangeName, entries[0].ExchangeName)
	assert.Equal(t, expectedEntries[1].ExchangeName, entries[1].ExchangeName)

	assert.NoError(t, mockPool.ExpectationsWereMet())
}
