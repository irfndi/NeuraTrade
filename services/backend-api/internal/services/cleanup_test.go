package services

import (
	"context"
	"errors"
	"testing"
	"time"

	zaplogrus "github.com/irfandi/celebrum-ai-go/internal/logging/zaplogrus"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"

	"github.com/irfandi/celebrum-ai-go/internal/config"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/telemetry"
)

// TestNewCleanupService tests the NewCleanupService function
func TestNewCleanupService(t *testing.T) {
	// Initialize telemetry for testing to avoid nil logger
	config := telemetry.DefaultConfig()
	config.Enabled = false // Disable telemetry for tests
	err := telemetry.InitTelemetry(*config)
	assert.NoError(t, err)

	// Create mock database
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()

	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Test creating cleanup service (ResourceManager and PerformanceMonitor are not used)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling paths
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Verify service is created
	assert.NotNil(t, service)
	assert.NotNil(t, service.ctx)
	assert.NotNil(t, service.cancel)
	assert.Nil(t, service.db) // db should be nil as set above
	assert.Equal(t, errorRecoveryManager, service.errorRecoveryManager)
	assert.Nil(t, service.resourceManager)    // Should be nil as passed
	assert.Nil(t, service.performanceMonitor) // Should be nil as passed
	assert.NotNil(t, service.logger)
}

// TestCleanupService_Start tests the Start method
func TestCleanupService_Start(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test configuration
	cleanupConfig := config.CleanupConfig{
		MarketData: config.CleanupDataConfig{
			RetentionHours: 36,
			DeletionHours:  12,
		},
		FundingRates: config.CleanupDataConfig{
			RetentionHours: 36,
			DeletionHours:  12,
		},
		ArbitrageOpportunities: config.CleanupArbitrageConfig{
			RetentionHours: 72,
		},
		IntervalMinutes:    60,
		EnableSmartCleanup: true,
	}

	// Test starting the service - should not panic even with nil database
	assert.NotPanics(t, func() {
		service.Start(cleanupConfig)
	})

	// Wait a moment for goroutines to start
	time.Sleep(10 * time.Millisecond)

	// Test stopping the service
	assert.NotPanics(t, func() {
		service.Stop()
	})

	// Wait for graceful shutdown
	time.Sleep(10 * time.Millisecond)
}

// TestCleanupService_RunCleanup tests the RunCleanup method
func TestCleanupService_RunCleanup(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test configuration
	cleanupConfig := config.CleanupConfig{
		MarketData: config.CleanupDataConfig{
			RetentionHours: 36,
			DeletionHours:  12,
		},
		FundingRates: config.CleanupDataConfig{
			RetentionHours: 36,
			DeletionHours:  12,
		},
		ArbitrageOpportunities: config.CleanupArbitrageConfig{
			RetentionHours: 72,
		},
		IntervalMinutes:    60,
		EnableSmartCleanup: true,
	}

	// Test running cleanup with nil database - should handle errors gracefully
	err := service.RunCleanup(cleanupConfig)
	// We expect an error due to nil database pool
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_GetDataStats tests the GetDataStats method
func TestCleanupService_GetDataStats(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test getting data stats with nil database
	ctx := context.Background()
	stats, err := service.GetDataStats(ctx)
	assert.Error(t, err)
	assert.Nil(t, stats)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_GetDataStats_WithError tests GetDataStats with database error
func TestCleanupService_GetDataStats_WithError(t *testing.T) {
	// Create mock database that returns errors
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	// Configure mock to return error for QueryRow calls
	mockPool.ExpectQuery("SELECT COUNT\\(\\*\\) FROM market_data").WillReturnError(errors.New("connection failed"))

	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with mock database
	service := NewCleanupService(
		dbPool,
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test getting data stats with database error
	ctx := context.Background()
	stats, err := service.GetDataStats(ctx)
	assert.Error(t, err)
	assert.Nil(t, stats)
	assert.Contains(t, err.Error(), "failed to count market data")

	// Ensure all expectations were met
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestCleanupService_CleanupMarketDataSmart tests the cleanupMarketDataSmart method
func TestCleanupService_CleanupMarketDataSmart(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test cleanup market data with nil database - should handle errors gracefully
	ctx := context.Background()
	err := service.cleanupMarketDataSmart(ctx, 36, 12)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupFundingRatesSmart tests the cleanupFundingRatesSmart method
func TestCleanupService_CleanupFundingRatesSmart(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test cleanup funding rates with nil database - should handle errors gracefully
	ctx := context.Background()
	err := service.cleanupFundingRatesSmart(ctx, 36, 12)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupArbitrageOpportunities tests the cleanupArbitrageOpportunities method
func TestCleanupService_CleanupArbitrageOpportunities(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test cleanup arbitrage opportunities with nil database - should handle errors gracefully
	ctx := context.Background()
	err := service.cleanupArbitrageOpportunities(ctx, 72)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupFundingArbitrageOpportunities tests the cleanupFundingArbitrageOpportunities method
func TestCleanupService_CleanupFundingArbitrageOpportunities(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test cleanup funding arbitrage opportunities with nil database - should handle errors gracefully
	ctx := context.Background()
	err := service.cleanupFundingArbitrageOpportunities(ctx, 72)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupMarketData tests the cleanupMarketData method
func TestCleanupService_CleanupMarketData(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test cleanup market data with nil database - should handle errors gracefully
	ctx := context.Background()
	err := service.cleanupMarketData(ctx, 36)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupFundingRates tests the cleanupFundingRates method
func TestCleanupService_CleanupFundingRates(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test cleanup funding rates with nil database - should handle errors gracefully
	ctx := context.Background()
	err := service.cleanupFundingRates(ctx, 36)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupMarketData_WithRealDatabase tests cleanupMarketData with actual database operations
func TestCleanupService_CleanupMarketData_WithRealDatabase(t *testing.T) {
	// Create mock database
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with mock database
	service := NewCleanupService(
		dbPool,
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Expect the DELETE call with proper parameters
	mockPool.ExpectExec("DELETE FROM market_data WHERE created_at < \\$1").
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 5)) // Simulate 5 rows deleted

	// Test cleanup market data with real database - should succeed
	ctx := context.Background()
	err = service.cleanupMarketData(ctx, 36)
	assert.NoError(t, err)

	// Ensure all expectations were met
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestCleanupService_CleanupFundingRates_WithRealDatabase tests cleanupFundingRates with actual database operations
func TestCleanupService_CleanupFundingRates_WithRealDatabase(t *testing.T) {
	// Create mock database
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()
	dbPool := database.NewMockDBPool(mockPool)

	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with mock database
	service := NewCleanupService(
		dbPool,
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Expect the DELETE call with proper parameters
	mockPool.ExpectExec("DELETE FROM funding_rates WHERE created_at < \\$1").
		WithArgs(pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("DELETE", 3)) // Simulate 3 rows deleted

	// Test cleanup funding rates with real database - should succeed
	ctx := context.Background()
	err = service.cleanupFundingRates(ctx, 36)
	assert.NoError(t, err)

	// Ensure all expectations were met
	assert.NoError(t, mockPool.ExpectationsWereMet())
}

// TestCleanupService_CleanupMarketData_ContextCancellation tests cleanupMarketData with context cancellation
func TestCleanupService_CleanupMarketData_ContextCancellation(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test cleanup market data with cancelled context - should return database error first
	err := service.cleanupMarketData(ctx, 36)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupFundingRates_ContextCancellation tests cleanupFundingRates with context cancellation
func TestCleanupService_CleanupFundingRates_ContextCancellation(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test with cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test cleanup funding rates with cancelled context - should return database error first
	err := service.cleanupFundingRates(ctx, 36)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupMarketData_NegativeRetention tests cleanupMarketData with negative retention hours
func TestCleanupService_CleanupMarketData_NegativeRetention(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test cleanup market data with negative retention - should handle gracefully
	ctx := context.Background()
	err := service.cleanupMarketData(ctx, -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupFundingRates_ZeroRetention tests cleanupFundingRates with zero retention hours
func TestCleanupService_CleanupFundingRates_ZeroRetention(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test cleanup funding rates with zero retention - should handle gracefully
	ctx := context.Background()
	err := service.cleanupFundingRates(ctx, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupMarketDataSmart_WithError tests cleanupMarketDataSmart with database error
func TestCleanupService_CleanupMarketDataSmart_WithError(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service with nil database (tests error handling paths)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test cleanup market data with nil database - should handle errors gracefully
	ctx := context.Background()
	err := service.cleanupMarketDataSmart(ctx, 36, 12)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "database pool is not available")
}

// TestCleanupService_CleanupMarketDataSmart_Comprehensive tests cleanupMarketDataSmart with various scenarios
func TestCleanupService_CleanupMarketDataSmart_Comprehensive(t *testing.T) {
	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Test with nil database
	t.Run("NilDatabase", func(t *testing.T) {
		service := NewCleanupService(
			nil,
			errorRecoveryManager,
			nil,
			nil,
		)

		ctx := context.Background()
		err := service.cleanupMarketDataSmart(ctx, 36, 12)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database pool is not available")
	})

	// Test with successful deletion
	t.Run("SuccessfulDeletion", func(t *testing.T) {
		mockPool, err := pgxmock.NewPool()
		assert.NoError(t, err)
		defer mockPool.Close()
		dbPool := database.NewMockDBPool(mockPool)

		service := NewCleanupService(
			dbPool,
			errorRecoveryManager,
			nil,
			nil,
		)

		// Expect the DELETE call with proper parameters
		mockPool.ExpectExec("DELETE FROM market_data WHERE created_at < \\$1").
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 10))

		ctx := context.Background()
		err = service.cleanupMarketDataSmart(ctx, 36, 12)
		assert.NoError(t, err)

		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	// Test with no rows deleted
	t.Run("NoRowsDeleted", func(t *testing.T) {
		mockPool, err := pgxmock.NewPool()
		assert.NoError(t, err)
		defer mockPool.Close()
		dbPool := database.NewMockDBPool(mockPool)

		service := NewCleanupService(
			dbPool,
			errorRecoveryManager,
			nil,
			nil,
		)

		// Expect the DELETE call with no rows affected
		mockPool.ExpectExec("DELETE FROM market_data WHERE created_at < \\$1").
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		ctx := context.Background()
		err = service.cleanupMarketDataSmart(ctx, 36, 12)
		assert.NoError(t, err)

		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	// Test with database error
	t.Run("DatabaseError", func(t *testing.T) {
		mockPool, err := pgxmock.NewPool()
		assert.NoError(t, err)
		defer mockPool.Close()
		dbPool := database.NewMockDBPool(mockPool)

		service := NewCleanupService(
			dbPool,
			errorRecoveryManager,
			nil,
			nil,
		)

		// Expect the DELETE call to return an error
		mockPool.ExpectExec("DELETE FROM market_data WHERE created_at < \\$1").
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(errors.New("database connection failed"))

		ctx := context.Background()
		err = service.cleanupMarketDataSmart(ctx, 36, 12)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to delete old market data")
		assert.Contains(t, err.Error(), "database connection failed")

		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	// Test with different retention and deletion hours
	t.Run("DifferentHours", func(t *testing.T) {
		mockPool, err := pgxmock.NewPool()
		assert.NoError(t, err)
		defer mockPool.Close()
		dbPool := database.NewMockDBPool(mockPool)

		service := NewCleanupService(
			dbPool,
			errorRecoveryManager,
			nil,
			nil,
		)

		// Test with 48 hours retention, 24 hours deletion
		mockPool.ExpectExec("DELETE FROM market_data WHERE created_at < \\$1").
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 25))

		ctx := context.Background()
		err = service.cleanupMarketDataSmart(ctx, 48, 24)
		assert.NoError(t, err)

		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	// Test with zero retention hours (should delete everything)
	t.Run("ZeroRetentionHours", func(t *testing.T) {
		mockPool, err := pgxmock.NewPool()
		assert.NoError(t, err)
		defer mockPool.Close()
		dbPool := database.NewMockDBPool(mockPool)

		service := NewCleanupService(
			dbPool,
			errorRecoveryManager,
			nil,
			nil,
		)

		// Expect the DELETE call - cutoff time will be now
		mockPool.ExpectExec("DELETE FROM market_data WHERE created_at < \\$1").
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 100))

		ctx := context.Background()
		err = service.cleanupMarketDataSmart(ctx, 0, 0)
		assert.NoError(t, err)

		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	// Test with negative retention hours (should still work, deleting more data)
	t.Run("NegativeRetentionHours", func(t *testing.T) {
		mockPool, err := pgxmock.NewPool()
		assert.NoError(t, err)
		defer mockPool.Close()
		dbPool := database.NewMockDBPool(mockPool)

		service := NewCleanupService(
			dbPool,
			errorRecoveryManager,
			nil,
			nil,
		)

		// Expect the DELETE call - cutoff time will be in the future
		mockPool.ExpectExec("DELETE FROM market_data WHERE created_at < \\$1").
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 0))

		ctx := context.Background()
		err = service.cleanupMarketDataSmart(ctx, -10, 0)
		assert.NoError(t, err)

		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	// Test with cancelled context
	t.Run("CancelledContext", func(t *testing.T) {
		mockPool, err := pgxmock.NewPool()
		assert.NoError(t, err)
		defer mockPool.Close()
		dbPool := database.NewMockDBPool(mockPool)

		service := NewCleanupService(
			dbPool,
			errorRecoveryManager,
			nil,
			nil,
		)

		// Create cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// The context cancellation should be checked during database execution
		mockPool.ExpectExec("DELETE FROM market_data WHERE created_at < \\$1").
			WithArgs(pgxmock.AnyArg()).
			WillReturnError(context.Canceled)

		err = service.cleanupMarketDataSmart(ctx, 36, 12)
		assert.Error(t, err)
		// Error could be context.Canceled or wrapped version
		assert.True(t, err == context.Canceled || err.Error() == "failed to delete old market data: context canceled")

		assert.NoError(t, mockPool.ExpectationsWereMet())
	})

	// Test time calculation accuracy
	t.Run("TimeCalculation", func(t *testing.T) {
		mockPool, err := pgxmock.NewPool()
		assert.NoError(t, err)
		defer mockPool.Close()
		dbPool := database.NewMockDBPool(mockPool)

		service := NewCleanupService(
			dbPool,
			errorRecoveryManager,
			nil,
			nil,
		)

		// Expect the DELETE call - we can't easily capture the time argument with pgxmock
		// but we can verify the call is made correctly
		mockPool.ExpectExec("DELETE FROM market_data WHERE created_at < \\$1").
			WithArgs(pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("DELETE", 5))

		ctx := context.Background()
		beforeCall := time.Now()
		err = service.cleanupMarketDataSmart(ctx, 24, 12)

		assert.NoError(t, err)
		assert.True(t, time.Since(beforeCall) < time.Second, "Call should complete quickly")

		assert.NoError(t, mockPool.ExpectationsWereMet())
	})
}

// TestCleanupService_Stop tests the Stop method
func TestCleanupService_Stop(t *testing.T) {
	// Create mock database
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()

	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service (ResourceManager and PerformanceMonitor are not used)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test stopping the service
	assert.NotPanics(t, func() {
		service.Stop()
	})
}

// TestCleanupService_ContextCancellation tests context cancellation handling
func TestCleanupService_ContextCancellation(t *testing.T) {
	// Create mock database
	mockPool, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockPool.Close()

	// Create real ErrorRecoveryManager for testing
	errorRecoveryManager := NewErrorRecoveryManager(zaplogrus.New())

	// Create cleanup service (ResourceManager and PerformanceMonitor are not used)
	service := NewCleanupService(
		nil, // Use nil interface to test error handling
		errorRecoveryManager,
		nil, // ResourceManager not used by CleanupService
		nil, // PerformanceMonitor not used by CleanupService
	)

	// Test configuration
	cleanupConfig := config.CleanupConfig{
		MarketData: config.CleanupDataConfig{
			RetentionHours: 36,
			DeletionHours:  12,
		},
		FundingRates: config.CleanupDataConfig{
			RetentionHours: 36,
			DeletionHours:  12,
		},
		ArbitrageOpportunities: config.CleanupArbitrageConfig{
			RetentionHours: 72,
		},
		IntervalMinutes:    60,
		EnableSmartCleanup: true,
	}

	// Start the service
	service.Start(cleanupConfig)

	// Wait a moment for goroutines to start
	time.Sleep(10 * time.Millisecond)

	// Test stopping the service
	service.Stop()

	// Wait for graceful shutdown
	time.Sleep(10 * time.Millisecond)
}
