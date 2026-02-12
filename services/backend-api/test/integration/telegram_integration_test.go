package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/api"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/irfndi/neuratrade/test/testmocks"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTelegramIntegration verifies internal Telegram endpoints used by the bot
func TestTelegramIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Setup database connection
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := database.NewPostgresConnection(&config.DatabaseConfig{
		DatabaseURL:     dbURL,
		MaxOpenConns:    5,
		MaxIdleConns:    2,
		ConnMaxLifetime: "60s",
	})
	require.NoError(t, err)
	defer db.Close()

	// Setup Redis (optional for this test, but needed by SetupRoutes)
	// We'll mock it if not available or just pass nil if handled gracefully
	// For integration, we prefer real Redis if REDIS_URL/ADDR is set
	var redisClient *database.RedisClient
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr != "" {
		redisClient = &database.RedisClient{
			Client: redis.NewClient(&redis.Options{Addr: redisAddr}),
		}
		defer redisClient.Close()
	}

	// Setup Router
	gin.SetMode(gin.TestMode)
	router := gin.New()

	testAdminKey := "test-admin-key-integration-12345"
	cfg := &config.TelegramConfig{
		AdminAPIKey: testAdminKey,
		ServiceURL:  "http://telegram-service:3002",
	}

	// Mock other dependencies
	// ... (We can reuse nil or simple mocks as SetupRoutes handles them)
	// We need actual user handler functioning, so we need DB.

	// Create required middlewares
	authMiddleware := middleware.NewAuthMiddleware("test-jwt-secret")
	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("http://ccxt-service:3001")

	// Call SetupRoutes
	// We pass nil for services not involved in this test flow
	api.SetupRoutes(router, db, redisClient, mockCCXT, nil, nil, nil, nil, nil, cfg, authMiddleware)

	// Test Data
	testTelegramChatID := fmt.Sprintf("telegram_integration_%s", uuid.New().String())
	testEmail := fmt.Sprintf("test_integration_%s@celebrum.ai", uuid.New().String())

	// 1. Create a user (simulate registration via other means or manually insert)
	// We'll manually insert to ensure clean state
	userID := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at)
		VALUES ($1, $2, 'hash', $3, 'free', NOW(), NOW())
	`, userID, testEmail, testTelegramChatID)
	require.NoError(t, err)

	// Clean up after test
	defer func() {
		_, _ = db.Pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
		_, _ = db.Pool.Exec(context.Background(), "DELETE FROM user_alerts WHERE user_id = $1", userID)
	}()

	t.Run("GetUserByChatID", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+testTelegramChatID, nil)
		req.Header.Set("X-API-Key", testAdminKey)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		userData := resp["user"].(map[string]interface{})
		assert.Equal(t, userID, userData["id"])
		assert.Equal(t, "free", userData["subscription_tier"])
	})

	t.Run("GetNotificationPreferences_Default", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+userID, nil)
		req.Header.Set("X-API-Key", testAdminKey)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		// exptected default: enabled=true, profit=0.5
		assert.Equal(t, true, resp["enabled"])
		assert.Equal(t, 0.5, resp["profit_threshold"])
	})

	t.Run("SetNotificationPreferences_Disable", func(t *testing.T) {
		body := []byte(`{"enabled": false}`)
		req, _ := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+userID, bytes.NewBuffer(body))
		req.Header.Set("X-API-Key", testAdminKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Verify it's persistent
		reqValues, _ := http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+userID, nil)
		reqValues.Header.Set("X-API-Key", testAdminKey)
		wValues := httptest.NewRecorder()
		router.ServeHTTP(wValues, reqValues)

		var resp map[string]interface{}
		_ = json.Unmarshal(wValues.Body.Bytes(), &resp)
		assert.Equal(t, false, resp["enabled"])
	})

	t.Run("SetNotificationPreferences_EnableAgain", func(t *testing.T) {
		body := []byte(`{"enabled": true}`)
		req, _ := http.NewRequest("POST", "/api/v1/telegram/internal/notifications/"+userID, bytes.NewBuffer(body))
		req.Header.Set("X-API-Key", testAdminKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Verify it's persistent
		reqValues, _ := http.NewRequest("GET", "/api/v1/telegram/internal/notifications/"+userID, nil)
		reqValues.Header.Set("X-API-Key", testAdminKey)
		wValues := httptest.NewRecorder()
		router.ServeHTTP(wValues, reqValues)

		var resp map[string]interface{}
		_ = json.Unmarshal(wValues.Body.Bytes(), &resp)
		assert.Equal(t, true, resp["enabled"])
	})

	t.Run("InternalRoute_AllowsNoKey", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/telegram/internal/users/"+testTelegramChatID, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}
