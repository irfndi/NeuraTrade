package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfndi/neuratrade/internal/api"
	"github.com/irfndi/neuratrade/internal/ccxt"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/irfndi/neuratrade/internal/testutil"
	"github.com/irfndi/neuratrade/test/testmocks"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestAutonomousIntegration(t *testing.T) {
	if os.Getenv("CI") == "true" {
		t.Skip("Skipping in CI environment - test requires full service mocks")
	}

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

	var redisClient *database.RedisClient
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr != "" {
		redisClient = &database.RedisClient{
			Client: redis.NewClient(&redis.Options{Addr: redisAddr}),
		}
		defer redisClient.Close()
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()

	testAdminKey := "test-admin-key-integration-12345"
	os.Setenv("ADMIN_API_KEY", testAdminKey) // ensures adminMiddleware picks it up
	defer os.Unsetenv("ADMIN_API_KEY")

	cfg := &config.TelegramConfig{
		AdminAPIKey: testAdminKey,
		ServiceURL:  "http://telegram-service:3002",
	}

	authMiddleware := middleware.NewAuthMiddleware(testutil.MustGenerateTestSecret())
	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("http://ccxt-service:3001")
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance"})

	// Mock FetchBalance for portfolio safety
	mockCCXT.On("FetchBalance", mock.Anything, "binance").Return(&ccxt.BalanceResponse{
		Exchange:  "binance",
		Timestamp: time.Now(),
		Total:     map[string]float64{"USDT": 10000.0, "BTC": 0.5},
		Free:      map[string]float64{"USDT": 5000.0, "BTC": 0.5},
		Used:      map[string]float64{"USDT": 5000.0, "BTC": 0.0},
	}, nil)

	api.SetupRoutes(router, db, redisClient, mockCCXT, nil, nil, nil, nil, nil, cfg, nil, nil, authMiddleware, nil)

	testTelegramChatID := fmt.Sprintf("tg_auto_%s", uuid.New().String())
	testEmail := fmt.Sprintf("test_auto_%s@celebrum.ai", uuid.New().String())

	userID := uuid.New().String()
	_, err = db.Pool.Exec(context.Background(), `
		INSERT INTO users (id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at)
		VALUES ($1, $2, 'hash', $3, 'free', NOW(), NOW())
	`, userID, testEmail, testTelegramChatID)
	require.NoError(t, err)

	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userID)
	})

	t.Run("GetPortfolio", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/telegram/internal/portfolio?chat_id="+testTelegramChatID, nil)
		req.Header.Set("X-API-Key", testAdminKey)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		// Verification of PortfolioResponse output
		assert.Contains(t, resp, "total_equity")
		assert.Contains(t, resp, "available_balance")
		assert.Contains(t, resp, "safety_status")

		safetyStatus, ok := resp["safety_status"].(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, true, safetyStatus["is_safe"])
		assert.Equal(t, true, safetyStatus["trading_allowed"])
	})

	t.Run("GetDoctor", func(t *testing.T) {
		req, _ := http.NewRequest("GET", "/api/v1/telegram/internal/doctor?chat_id="+testTelegramChatID, nil)
		// Internal doctor doesn't strictly need X-API-Key in all setups but we'll provide it
		req.Header.Set("X-API-Key", testAdminKey)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)

		assert.Contains(t, resp, "checks")
		assert.Contains(t, resp, "overall_status")

		checks, ok := resp["checks"].([]interface{})
		require.True(t, ok)

		foundPortfolioSafety := false
		foundPortfolioStatus := false
		for _, c := range checks {
			checkMap := c.(map[string]interface{})
			if checkMap["name"] == "portfolio_safety" {
				foundPortfolioSafety = true
				assert.Equal(t, "healthy", checkMap["status"])
			}
			if checkMap["name"] == "portfolio_status" {
				foundPortfolioStatus = true
				assert.Equal(t, "healthy", checkMap["status"])
			}
		}

		assert.True(t, foundPortfolioSafety, "Missing portfolio_safety diagnostic")
		assert.True(t, foundPortfolioStatus, "Missing portfolio_status diagnostic")
	})
}
