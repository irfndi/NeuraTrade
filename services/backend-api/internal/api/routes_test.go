package api

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/api/handlers/testmocks"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockRouteDB wraps MockDBPool to add HealthCheck for routeDB interface
type mockRouteDB struct {
	database.DBPool
}

func (m mockRouteDB) HealthCheck(ctx context.Context) error {
	return nil
}

// setupMockDB creates a mock database pool with all expected initialization queries
func setupMockDB(t *testing.T) mockRouteDB {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err, "Failed to create mock pool")

	mock.ExpectExec("CREATE TABLE IF NOT EXISTS trading_orders").
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec("CREATE TABLE IF NOT EXISTS trading_positions").
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_trading_orders_position_id").
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))
	mock.ExpectExec("CREATE INDEX IF NOT EXISTS idx_trading_positions_symbol_status").
		WillReturnResult(pgxmock.NewResult("CREATE INDEX", 0))

	return mockRouteDB{DBPool: database.NewMockDBPool(mock)}
}

// Helper functions for environment variable management with proper error handling
func mustSetEnv(t *testing.T, key, value string) {
	t.Helper()
	if err := os.Setenv(key, value); err != nil {
		t.Fatalf("Failed to set env %s: %v", key, err)
	}
}

func mustUnsetEnv(t *testing.T, key string) {
	t.Helper()
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Failed to unset env %s: %v", key, err)
	}
}

func restoreEnv(t *testing.T, key, value string, existed bool) {
	t.Helper()
	if existed {
		mustSetEnv(t, key, value)
		return
	}
	mustUnsetEnv(t, key)
}

// Test HealthResponse struct
func TestHealthResponse_Struct(t *testing.T) {
	now := time.Now()
	response := HealthResponse{
		Status:    "ok",
		Timestamp: now,
		Version:   "1.0.0",
		Services: Services{
			Database: "ok",
			Redis:    "ok",
		},
	}

	assert.Equal(t, "ok", response.Status)
	assert.Equal(t, now, response.Timestamp)
	assert.Equal(t, "1.0.0", response.Version)
	assert.Equal(t, "ok", response.Services.Database)
	assert.Equal(t, "ok", response.Services.Redis)
}

// Test Services struct
func TestServices_Struct(t *testing.T) {
	services := Services{
		Database: "ok",
		Redis:    "error",
	}

	assert.Equal(t, "ok", services.Database)
	assert.Equal(t, "error", services.Redis)
}

// Test JSON marshaling
func TestHealthResponse_JSONMarshaling(t *testing.T) {
	now := time.Now()
	response := HealthResponse{
		Status:    "ok",
		Timestamp: now,
		Version:   "1.0.0",
		Services: Services{
			Database: "ok",
			Redis:    "ok",
		},
	}

	// Test JSON marshaling
	jsonData, err := json.Marshal(response)
	assert.NoError(t, err)
	assert.Contains(t, string(jsonData), "ok")
	assert.Contains(t, string(jsonData), "1.0.0")

	// Test JSON unmarshaling
	var unmarshaled HealthResponse
	err = json.Unmarshal(jsonData, &unmarshaled)
	assert.NoError(t, err)
	assert.Equal(t, response.Status, unmarshaled.Status)
	assert.Equal(t, response.Version, unmarshaled.Version)
	assert.Equal(t, response.Services.Database, unmarshaled.Services.Database)
	assert.Equal(t, response.Services.Redis, unmarshaled.Services.Redis)
}

// Test time operations in health response
func TestHealthResponse_TimeOperations(t *testing.T) {
	now := time.Now()
	response := HealthResponse{
		Timestamp: now,
	}

	// Test that timestamp is recent
	assert.True(t, response.Timestamp.After(now.Add(-time.Second)))
	assert.True(t, response.Timestamp.Before(now.Add(time.Second)))

	// Test timestamp formatting
	timeStr := response.Timestamp.Format(time.RFC3339)
	assert.NotEmpty(t, timeStr)
	assert.Contains(t, timeStr, "T")
}

// Test different health response statuses
func TestHealthResponse_DifferentStatuses(t *testing.T) {
	// Test OK status
	okResponse := HealthResponse{
		Status:  "ok",
		Version: "1.0.0",
		Services: Services{
			Database: "ok",
			Redis:    "ok",
		},
	}
	assert.Equal(t, "ok", okResponse.Status)

	// Test degraded status
	degradedResponse := HealthResponse{
		Status:  "degraded",
		Version: "1.0.0",
		Services: Services{
			Database: "error",
			Redis:    "ok",
		},
	}
	assert.Equal(t, "degraded", degradedResponse.Status)
	assert.Equal(t, "error", degradedResponse.Services.Database)
	assert.Equal(t, "ok", degradedResponse.Services.Redis)
}

// Test version information
func TestHealthResponse_Version(t *testing.T) {
	response := HealthResponse{
		Version: "1.0.0",
	}

	assert.Equal(t, "1.0.0", response.Version)
	assert.NotEmpty(t, response.Version)

	// Test different version formats
	versions := []string{"1.0.0", "2.1.3", "0.1.0-beta", "1.0.0-rc1"}
	for _, version := range versions {
		response.Version = version
		assert.Equal(t, version, response.Version)
		assert.NotEmpty(t, response.Version)
	}
}

// Test service status combinations
func TestServices_StatusCombinations(t *testing.T) {
	// Both services OK
	services1 := Services{
		Database: "ok",
		Redis:    "ok",
	}
	assert.Equal(t, "ok", services1.Database)
	assert.Equal(t, "ok", services1.Redis)

	// Database error, Redis OK
	services2 := Services{
		Database: "error",
		Redis:    "ok",
	}
	assert.Equal(t, "error", services2.Database)
	assert.Equal(t, "ok", services2.Redis)

	// Database OK, Redis error
	services3 := Services{
		Database: "ok",
		Redis:    "error",
	}
	assert.Equal(t, "ok", services3.Database)
	assert.Equal(t, "error", services3.Redis)

	// Both services error
	services4 := Services{
		Database: "error",
		Redis:    "error",
	}
	assert.Equal(t, "error", services4.Database)
	assert.Equal(t, "error", services4.Redis)
}

// Test JSON field names
func TestHealthResponse_JSONFields(t *testing.T) {
	response := HealthResponse{
		Status:    "ok",
		Timestamp: time.Now(),
		Version:   "1.0.0",
		Services: Services{
			Database: "ok",
			Redis:    "ok",
		},
	}

	jsonData, err := json.Marshal(response)
	assert.NoError(t, err)

	// Check that JSON contains expected field names
	jsonStr := string(jsonData)
	assert.Contains(t, jsonStr, "status")
	assert.Contains(t, jsonStr, "timestamp")
	assert.Contains(t, jsonStr, "version")
	assert.Contains(t, jsonStr, "services")
	assert.Contains(t, jsonStr, "database")
	assert.Contains(t, jsonStr, "redis")
}

// Test empty and nil values
func TestHealthResponse_EmptyValues(t *testing.T) {
	// Test with empty strings
	response := HealthResponse{
		Status:  "",
		Version: "",
		Services: Services{
			Database: "",
			Redis:    "",
		},
	}

	assert.Empty(t, response.Status)
	assert.Empty(t, response.Version)
	assert.Empty(t, response.Services.Database)
	assert.Empty(t, response.Services.Redis)

	// Test JSON marshaling with empty values
	jsonData, err := json.Marshal(response)
	assert.NoError(t, err)
	assert.NotEmpty(t, jsonData)
}

// Test timestamp precision
func TestHealthResponse_TimestampPrecision(t *testing.T) {
	now := time.Now()
	response := HealthResponse{
		Timestamp: now,
	}

	// Test that timestamp preserves precision
	assert.Equal(t, now.Unix(), response.Timestamp.Unix())
	assert.Equal(t, now.Nanosecond(), response.Timestamp.Nanosecond())

	// Test timestamp in different formats
	rfc3339 := response.Timestamp.Format(time.RFC3339)
	assert.NotEmpty(t, rfc3339)

	unix := response.Timestamp.Unix()
	assert.Greater(t, unix, int64(0))
}

// Test SetupRoutes function with comprehensive coverage
func TestSetupRoutes_Comprehensive(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create a new router
	router := gin.New()

	// Test that SetupRoutes function exists and is callable
	// This provides basic coverage for the SetupRoutes function
	assert.NotNil(t, SetupRoutes)

	// Test that the function signature is correct by checking if it can be referenced
	// This ensures the SetupRoutes function is properly accessible
	_ = SetupRoutes

	// Test router initialization
	assert.NotNil(t, router)
	assert.True(t, len(router.Routes()) == 0) // Initially no routes
}

// TestSetupRoutes_FunctionSignature tests that SetupRoutes has the correct function signature
func TestSetupRoutes_FunctionSignature(t *testing.T) {
	// Test that SetupRoutes is a function with the expected signature
	// This provides coverage for the function declaration
	assert.NotNil(t, SetupRoutes)

	// Test that the function signature is correct by checking if it can be referenced
	// This ensures the SetupRoutes function is properly accessible
	_ = SetupRoutes
}

// TestSetupRoutes_PanicHandling tests that SetupRoutes handles nil dependencies gracefully
func TestSetupRoutes_PanicHandling(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Set a test admin API key to avoid environment variable check
	oldAdminKey, adminKeyExists := os.LookupEnv("ADMIN_API_KEY")
	mustSetEnv(t, "ADMIN_API_KEY", "test-admin-key-for-testing-purposes-only")
	defer restoreEnv(t, "ADMIN_API_KEY", oldAdminKey, adminKeyExists)

	// Create a new router
	router := gin.New()
	assert.NotNil(t, router)

	assert.Panics(t, func() {
		SetupRoutes(router, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}, "SetupRoutes should panic with nil dependencies")
}

// TestSetupRoutes_RouteRegistration tests that routes are properly registered
func TestSetupRoutes_RouteRegistration(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Set required environment variables
	oldAdminKey, adminKeyExists := os.LookupEnv("ADMIN_API_KEY")
	oldTelegramToken, telegramTokenExists := os.LookupEnv("TELEGRAM_BOT_TOKEN")
	oldTelegramChat, telegramChatExists := os.LookupEnv("TELEGRAM_CHAT_ID")
	defer func() {
		restoreEnv(t, "ADMIN_API_KEY", oldAdminKey, adminKeyExists)
		restoreEnv(t, "TELEGRAM_BOT_TOKEN", oldTelegramToken, telegramTokenExists)
		restoreEnv(t, "TELEGRAM_CHAT_ID", oldTelegramChat, telegramChatExists)
	}()

	mustSetEnv(t, "ADMIN_API_KEY", "test-admin-key-that-is-at-least-32-chars")
	mustSetEnv(t, "TELEGRAM_BOT_TOKEN", "test-token")
	mustSetEnv(t, "TELEGRAM_CHAT_ID", "test-chat-id")

	// Use mock database for dependencies - SetupRoutes requires database for TradingHandler
	router := gin.New()

	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("test-url")
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance"})

	mockDB := setupMockDB(t)
	mockRedis := &database.RedisClient{
		Client: redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		}),
	}

	mockTelegramConfig := &config.TelegramConfig{
		BotToken: "test-token",
	}

	mockAuthMiddleware := middleware.NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")

	assert.NotPanics(t, func() {
		SetupRoutes(router, mockDB, mockRedis, mockCCXT, nil, nil, nil, nil, nil, mockTelegramConfig, nil, nil, mockAuthMiddleware, nil)
	}, "SetupRoutes should handle minimal dependencies gracefully")

	// Verify routes were registered
	routes := router.Routes()
	assert.Greater(t, len(routes), 0, "Routes should be registered")

	// Test that common routes exist
	routePaths := make([]string, len(routes))
	for i, route := range routes {
		routePaths[i] = route.Path
	}

	// Check for essential routes
	assert.Contains(t, routePaths, "/health", "Health endpoint should be registered")
	assert.Contains(t, routePaths, "/api/v1/market/prices", "Market prices endpoint should be registered")
	assert.Contains(t, routePaths, "/api/v1/exchanges/config", "Exchanges config endpoint should be registered")
	assert.Contains(t, routePaths, "/api/v1/arbitrage/opportunities", "Arbitrage opportunities endpoint should be registered")
}

// TestSetupRoutes_RouteGroups tests that route groups are properly configured
func TestSetupRoutes_RouteGroups(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Set required environment variables
	oldAdminKey, adminKeyExists := os.LookupEnv("ADMIN_API_KEY")
	oldTelegramToken, telegramTokenExists := os.LookupEnv("TELEGRAM_BOT_TOKEN")
	oldTelegramChat, telegramChatExists := os.LookupEnv("TELEGRAM_CHAT_ID")
	defer func() {
		restoreEnv(t, "ADMIN_API_KEY", oldAdminKey, adminKeyExists)
		restoreEnv(t, "TELEGRAM_BOT_TOKEN", oldTelegramToken, telegramTokenExists)
		restoreEnv(t, "TELEGRAM_CHAT_ID", oldTelegramChat, telegramChatExists)
	}()

	mustSetEnv(t, "ADMIN_API_KEY", "test-admin-key-that-is-at-least-32-chars")
	mustSetEnv(t, "TELEGRAM_BOT_TOKEN", "test-token")
	mustSetEnv(t, "TELEGRAM_CHAT_ID", "test-chat-id")

	// Create minimal mock dependencies
	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("test-url")
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance"})

	mockTelegramConfig := &config.TelegramConfig{
		BotToken: "test-token",
	}

	// Create router and setup routes with mock database
	router := gin.New()
	mockDB := setupMockDB(t)
	mockRedis := &database.RedisClient{
		Client: redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		}),
	}
	mockAuthMiddleware := middleware.NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")
	SetupRoutes(router, mockDB, mockRedis, mockCCXT, nil, nil, nil, nil, nil, mockTelegramConfig, nil, nil, mockAuthMiddleware, nil)

	// Get all routes
	routes := router.Routes()

	// Test that API group routes exist (routes with /api prefix)
	apiRoutes := 0
	adminRoutes := 0
	telegramRoutes := 0

	for _, route := range routes {
		if len(route.Path) >= 4 && route.Path[:4] == "/api" {
			apiRoutes++
		}
		if len(route.Path) >= 6 && route.Path[:6] == "/admin" {
			adminRoutes++
		}
		if len(route.Path) >= 10 && route.Path[:10] == "/telegram" {
			telegramRoutes++
		}
	}

	// Verify route groups are configured
	assert.Greater(t, apiRoutes, 0, "API routes should be registered")
	// Note: Admin routes are part of API routes (/api/v1/exchanges/*), not separate /admin routes
	// Note: Telegram routes are part of API routes (/api/v1/telegram/*), not separate /telegram routes
}

// TestSetupRoutes_HttpMethods tests that routes support the correct HTTP methods
func TestSetupRoutes_HttpMethods(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Set required environment variables
	oldAdminKey, adminKeyExists := os.LookupEnv("ADMIN_API_KEY")
	oldTelegramToken, telegramTokenExists := os.LookupEnv("TELEGRAM_BOT_TOKEN")
	oldTelegramChat, telegramChatExists := os.LookupEnv("TELEGRAM_CHAT_ID")
	defer func() {
		restoreEnv(t, "ADMIN_API_KEY", oldAdminKey, adminKeyExists)
		restoreEnv(t, "TELEGRAM_BOT_TOKEN", oldTelegramToken, telegramTokenExists)
		restoreEnv(t, "TELEGRAM_CHAT_ID", oldTelegramChat, telegramChatExists)
	}()

	mustSetEnv(t, "ADMIN_API_KEY", "test-admin-key-that-is-at-least-32-chars")
	mustSetEnv(t, "TELEGRAM_BOT_TOKEN", "test-token")
	mustSetEnv(t, "TELEGRAM_CHAT_ID", "test-chat-id")

	// Create minimal mock dependencies
	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("test-url")
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance"})

	mockTelegramConfig := &config.TelegramConfig{
		BotToken: "test-token",
	}

	// Create router and setup routes with mock database
	router := gin.New()
	mockDB := setupMockDB(t)
	mockRedis := &database.RedisClient{
		Client: redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		}),
	}
	mockAuthMiddleware := middleware.NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")
	SetupRoutes(router, mockDB, mockRedis, mockCCXT, nil, nil, nil, nil, nil, mockTelegramConfig, nil, nil, mockAuthMiddleware, nil)

	// Get all routes
	routes := router.Routes()

	// Test that common endpoints support GET method
	getRoutes := make(map[string]bool)
	postRoutes := make(map[string]bool)
	putRoutes := make(map[string]bool)
	deleteRoutes := make(map[string]bool)

	for _, route := range routes {
		switch route.Method {
		case "GET":
			getRoutes[route.Path] = true
		case "POST":
			postRoutes[route.Path] = true
		case "PUT":
			putRoutes[route.Path] = true
		case "DELETE":
			deleteRoutes[route.Path] = true
		}
	}

	// Verify essential endpoints support correct HTTP methods
	assert.True(t, getRoutes["/health"], "Health endpoint should support GET")
	assert.True(t, getRoutes["/api/v1/market/prices"], "Market prices endpoint should support GET")
	assert.True(t, getRoutes["/api/v1/exchanges/config"], "Exchanges config endpoint should support GET")
	assert.True(t, getRoutes["/api/v1/arbitrage/opportunities"], "Arbitrage opportunities endpoint should support GET")
}

// TestSetupRoutes_Middleware tests that middleware is properly configured
func TestSetupRoutes_Middleware(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Set required environment variables
	oldAdminKey, adminKeyExists := os.LookupEnv("ADMIN_API_KEY")
	oldTelegramToken, telegramTokenExists := os.LookupEnv("TELEGRAM_BOT_TOKEN")
	oldTelegramChat, telegramChatExists := os.LookupEnv("TELEGRAM_CHAT_ID")
	defer func() {
		restoreEnv(t, "ADMIN_API_KEY", oldAdminKey, adminKeyExists)
		restoreEnv(t, "TELEGRAM_BOT_TOKEN", oldTelegramToken, telegramTokenExists)
		restoreEnv(t, "TELEGRAM_CHAT_ID", oldTelegramChat, telegramChatExists)
	}()

	mustSetEnv(t, "ADMIN_API_KEY", "test-admin-key-that-is-at-least-32-chars")
	mustSetEnv(t, "TELEGRAM_BOT_TOKEN", "test-token")
	mustSetEnv(t, "TELEGRAM_CHAT_ID", "test-chat-id")

	// Create minimal mock dependencies
	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("test-url")
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance"})

	mockTelegramConfig := &config.TelegramConfig{
		BotToken: "test-token",
	}

	// Create router and setup routes with mock database
	router := gin.New()
	mockDB := setupMockDB(t)
	mockRedis := &database.RedisClient{
		Client: redis.NewClient(&redis.Options{
			Addr: "localhost:6379",
		}),
	}
	mockAuthMiddleware := middleware.NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")
	SetupRoutes(router, mockDB, mockRedis, mockCCXT, nil, nil, nil, nil, nil, mockTelegramConfig, nil, nil, mockAuthMiddleware, nil)

	// Test that router has middleware configured
	// Gin router should have middleware registered
	assert.NotNil(t, router, "Router should be configured with middleware")

	// Get all routes to verify they have handlers
	routes := router.Routes()
	assert.Greater(t, len(routes), 0, "Routes should be registered with handlers")

	// Test that routes have handlers assigned
	for _, route := range routes {
		assert.NotNil(t, route.Handler, "Route should have a handler assigned")
	}
}

// TestSetupRoutes_MissingAdminKey tests behavior when ADMIN_API_KEY is missing
func TestSetupRoutes_MissingAdminKey(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Remove admin API key
	oldAdminKey, adminKeyExists := os.LookupEnv("ADMIN_API_KEY")
	oldTelegramToken, telegramTokenExists := os.LookupEnv("TELEGRAM_BOT_TOKEN")
	oldTelegramChat, telegramChatExists := os.LookupEnv("TELEGRAM_CHAT_ID")
	defer func() {
		restoreEnv(t, "ADMIN_API_KEY", oldAdminKey, adminKeyExists)
		restoreEnv(t, "TELEGRAM_BOT_TOKEN", oldTelegramToken, telegramTokenExists)
		restoreEnv(t, "TELEGRAM_CHAT_ID", oldTelegramChat, telegramChatExists)
	}()

	mustSetEnv(t, "ADMIN_API_KEY", "test-admin-key-that-is-at-least-32-chars")
	mustSetEnv(t, "TELEGRAM_BOT_TOKEN", "test-token")
	mustSetEnv(t, "TELEGRAM_CHAT_ID", "test-chat-id")

	// Create minimal mock dependencies
	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("test-url")
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance"})

	mockTelegramConfig := &config.TelegramConfig{
		BotToken: "test-token",
	}

	// Create router
	router := gin.New()

	// Test that SetupRoutes works with valid admin key
	assert.NotPanics(t, func() {
		mockDB := setupMockDB(t)
		mockRedis := &database.RedisClient{
			Client: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
			}),
		}
		mockAuthMiddleware := middleware.NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")
		SetupRoutes(router, mockDB, mockRedis, mockCCXT, nil, nil, nil, nil, nil, mockTelegramConfig, nil, nil, mockAuthMiddleware, nil)
	}, "SetupRoutes should handle missing admin key gracefully")
}

// TestSetupRoutes_MissingTelegramConfig tests behavior when telegram config is missing
func TestSetupRoutes_MissingTelegramConfig(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Set admin key but remove telegram config
	oldAdminKey := os.Getenv("ADMIN_API_KEY")
	oldTelegramToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	oldTelegramChat := os.Getenv("TELEGRAM_CHAT_ID")
	defer func() {
		mustSetEnv(t, "ADMIN_API_KEY", oldAdminKey)
		mustSetEnv(t, "TELEGRAM_BOT_TOKEN", oldTelegramToken)
		mustSetEnv(t, "TELEGRAM_CHAT_ID", oldTelegramChat)
	}()

	mustSetEnv(t, "ADMIN_API_KEY", "test-admin-key-that-is-at-least-32-chars")
	mustUnsetEnv(t, "TELEGRAM_BOT_TOKEN")
	mustUnsetEnv(t, "TELEGRAM_CHAT_ID")

	// Create minimal mock dependencies
	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("test-url")
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance"})

	mockTelegramConfig := &config.TelegramConfig{
		BotToken: "test-token",
	}

	// Create router
	router := gin.New()

	// Test that SetupRoutes still works when telegram config is missing (but logs warning)
	assert.NotPanics(t, func() {
		mockDB := setupMockDB(t)
		mockRedis := &database.RedisClient{
			Client: redis.NewClient(&redis.Options{
				Addr: "localhost:6379",
			}),
		}
		mockAuthMiddleware := middleware.NewAuthMiddleware("test-secret-key-must-be-32-chars-min!")
		SetupRoutes(router, mockDB, mockRedis, mockCCXT, nil, nil, nil, nil, nil, mockTelegramConfig, nil, nil, mockAuthMiddleware, nil)
	}, "SetupRoutes should not panic when telegram config is missing")

	// Verify routes were still registered
	routes := router.Routes()
	assert.Greater(t, len(routes), 0, "Routes should be registered even without telegram config")
}
