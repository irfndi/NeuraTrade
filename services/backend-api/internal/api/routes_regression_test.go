package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/api/handlers/testmocks"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupRoutes_HealthEndpointContractRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Cleanup(func() {
		gin.SetMode(gin.DebugMode)
	})

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

	router := gin.New()
	mockDB := setupMockDB(t)

	// Use miniredis for proper Redis mocking
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	mockRedis := &database.RedisClient{Client: rdb}
	t.Cleanup(func() {
		_ = rdb.Close()
		mr.Close()
	})

	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("native")
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance"})

	mockTelegramConfig := &config.TelegramConfig{
		BotToken: "test-token",
	}
	cacheAnalyticsService := services.NewCacheAnalyticsService(nil)
	mockAuthMiddleware := middleware.MustNewAuthMiddleware("test-secret-key-must-be-32-chars-min!")

	teardown, err := SetupRoutes(router, mockDB, mockRedis, mockCCXT, nil, nil, cacheAnalyticsService, nil, nil, mockTelegramConfig, nil, nil, mockAuthMiddleware, nil, nil, nil, nil, nil, nil, nil, nil)
	require.NoError(t, err)
	defer teardown()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &payload))

	assert.Contains(t, payload, "status")
	assert.Contains(t, payload, "timestamp")
	assert.Contains(t, payload, "services")
	assert.Contains(t, payload, "version")
	assert.Contains(t, payload, "uptime")

	servicesPayload, ok := payload["services"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, servicesPayload, "database")
	assert.Contains(t, servicesPayload, "redis")
	assert.Contains(t, servicesPayload, "ccxt")

	mockCCXT.AssertExpectations(t)
}
