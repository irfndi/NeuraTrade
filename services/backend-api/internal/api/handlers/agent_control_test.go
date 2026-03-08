package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/irfndi/neuratrade/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentControlHandler_SetStrategyMode_ByChatID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "agent-control-autonomy.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := services.NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	handler := NewAgentControlHandler(services.NewScalpingAutonomyCoordinator(store, services.AIScalpingConfig{}))

	body := bytes.NewBufferString(`{"chat_id":"777","mode":"live"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/strategy-mode", body)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SetStrategyMode(c)

	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, services.ScalpingStrategyID("777"), response["strategy_id"])
	assert.Equal(t, "live", response["mode"])
	assert.Equal(t, "live", response["current_stage"])
	assert.Equal(t, "active", response["current_status"])
	assert.NotEmpty(t, response["entered_at"])
}

func TestAgentControlHandler_SetStrategyMode_RejectsInvalidMode(t *testing.T) {
	gin.SetMode(gin.TestMode)

	sqliteDB, err := database.NewSQLiteConnection(filepath.Join(t.TempDir(), "agent-control-invalid-mode.db"))
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = sqliteDB.Close()
	})

	store := services.NewAutonomousRolloutStore(sqliteDB.DB)
	require.NoError(t, store.InitSchema(context.Background()))

	handler := NewAgentControlHandler(services.NewScalpingAutonomyCoordinator(store, services.AIScalpingConfig{}))

	body := bytes.NewBufferString(`{"strategy_id":"strategy-1","mode":"turbo"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/agent/strategy-mode", body)
	c.Request.Header.Set("Content-Type", "application/json")

	handler.SetStrategyMode(c)

	require.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	assert.Contains(t, response["error"], "allowed modes are: shadow, paper, live")
	assert.Equal(t, "turbo", response["requested_mode"])
	assert.ElementsMatch(t, []interface{}{"shadow", "paper", "live"}, response["allowed_modes"])
}
