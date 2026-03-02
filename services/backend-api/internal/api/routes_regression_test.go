package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/api/handlers/testmocks"
	"github.com/irfndi/neuratrade/internal/config"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/middleware"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetupRoutes_HealthEndpointContractRegression(t *testing.T) {
	gin.SetMode(gin.TestMode)

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
	mockRedis := &database.RedisClient{}
	mockCCXT := &testmocks.MockCCXTService{}
	mockCCXT.On("GetServiceURL").Return("native")
	mockCCXT.On("GetSupportedExchanges").Return([]string{"binance"})

	mockTelegramConfig := &config.TelegramConfig{
		BotToken: "test-token",
	}
	teardown := SetupRoutes(router, mockDB, mockRedis, mockCCXT, nil, nil, cacheAnalyticsService, nil, nil, mockTelegramConfig, nil, nil, mockAuthMiddleware, nil, nil)
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

	assert.Contains(t, servicesPayload, "database")
	assert.Contains(t, servicesPayload, "redis")
	assert.Contains(t, servicesPayload, "ccxt")

	mockCCXT.AssertExpectations(t)
}
