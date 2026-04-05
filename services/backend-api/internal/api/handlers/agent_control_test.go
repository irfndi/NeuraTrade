package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	handler := NewAgentControlHandler(AgentControlDeps{Autonomy: services.NewScalpingAutonomyCoordinator(store, services.AIScalpingConfig{})})

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

	handler := NewAgentControlHandler(AgentControlDeps{Autonomy: services.NewScalpingAutonomyCoordinator(store, services.AIScalpingConfig{})})

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

type mockCollectorController struct {
	paused map[string]bool
	err    error
}

func (m *mockCollectorController) PauseExchange(exchangeID string) error {
	if m.err != nil {
		return m.err
	}
	if m.paused == nil {
		m.paused = make(map[string]bool)
	}
	m.paused[exchangeID] = true
	return nil
}

func (m *mockCollectorController) ResumeExchange(exchangeID string) error {
	if m.err != nil {
		return m.err
	}
	if m.paused == nil {
		m.paused = make(map[string]bool)
	}
	m.paused[exchangeID] = false
	return nil
}

type mockRiskController struct {
	safeModeEnabled   bool
	killSwitchEngaged bool
	err               error
}

func (m *mockRiskController) EnableSafeMode(_ context.Context, _ string) error {
	if m.err != nil {
		return m.err
	}
	m.safeModeEnabled = true
	return nil
}

func (m *mockRiskController) DisableSafeMode(_ context.Context) error {
	if m.err != nil {
		return m.err
	}
	m.safeModeEnabled = false
	return nil
}

func (m *mockRiskController) EngageKillSwitch(_ context.Context, _ string) error {
	if m.err != nil {
		return m.err
	}
	m.killSwitchEngaged = true
	return nil
}

func (m *mockRiskController) DisengageKillSwitch(_ context.Context) error {
	if m.err != nil {
		return m.err
	}
	m.killSwitchEngaged = false
	return nil
}

type mockOrderController struct {
	cancelled bool
	err       error
}

func (m *mockOrderController) CancelAllOrders(_ context.Context, _, _ string) error {
	if m.err != nil {
		return m.err
	}
	m.cancelled = true
	return nil
}

func makeJSONContext(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, w
}

func decodeResponseBody(t *testing.T, body []byte) map[string]interface{} {
	t.Helper()
	var response map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &response))
	return response
}

func TestAgentControlHandler_PauseExchange_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	collector := &mockCollectorController{}
	handler := NewAgentControlHandler(AgentControlDeps{Collector: collector})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/pause-exchange", `{"exchange_id":"binance"}`)

	handler.PauseExchange(c)

	require.Equal(t, http.StatusOK, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, "pause_exchange", response["action"])
	assert.Equal(t, "binance", response["exchange_id"])
	assert.Equal(t, true, collector.paused["binance"])
}

func TestAgentControlHandler_PauseExchange_MissingExchangeID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAgentControlHandler(AgentControlDeps{Collector: &mockCollectorController{}})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/pause-exchange", `{}`)

	handler.PauseExchange(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "exchange_id is required", response["error"])
}

func TestAgentControlHandler_PauseExchange_ServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAgentControlHandler(AgentControlDeps{})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/pause-exchange", `{"exchange_id":"binance"}`)

	handler.PauseExchange(c)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "collector service unavailable", response["error"])
}

func TestAgentControlHandler_PauseExchange_ServiceError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAgentControlHandler(AgentControlDeps{Collector: &mockCollectorController{err: errors.New("collector failed")}})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/pause-exchange", `{"exchange_id":"binance"}`)

	handler.PauseExchange(c)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "collector failed", response["error"])
	assert.Equal(t, "pause_exchange", response["action"])
}

func TestAgentControlHandler_ResumeExchange_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	collector := &mockCollectorController{paused: map[string]bool{"binance": true}}
	handler := NewAgentControlHandler(AgentControlDeps{Collector: collector})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/resume-exchange", `{"exchange_id":"binance"}`)

	handler.ResumeExchange(c)

	require.Equal(t, http.StatusOK, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, "resume_exchange", response["action"])
	assert.Equal(t, false, collector.paused["binance"])
}

func TestAgentControlHandler_EnableSafeMode_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	riskController := &mockRiskController{}
	handler := NewAgentControlHandler(AgentControlDeps{Risk: riskController})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/enable-safe-mode", `{}`)

	handler.EnableSafeMode(c)

	require.Equal(t, http.StatusOK, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, "enable_safe_mode", response["action"])
	assert.True(t, riskController.safeModeEnabled)
}

func TestAgentControlHandler_EnableSafeMode_ServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAgentControlHandler(AgentControlDeps{})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/enable-safe-mode", `{}`)

	handler.EnableSafeMode(c)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "risk service unavailable", response["error"])
}

func TestAgentControlHandler_DisableSafeMode_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	riskController := &mockRiskController{safeModeEnabled: true}
	handler := NewAgentControlHandler(AgentControlDeps{Risk: riskController})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/disable-safe-mode", `{}`)

	handler.DisableSafeMode(c)

	require.Equal(t, http.StatusOK, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, "disable_safe_mode", response["action"])
	assert.False(t, riskController.safeModeEnabled)
}

func TestAgentControlHandler_EngageKillSwitch_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	riskController := &mockRiskController{}
	handler := NewAgentControlHandler(AgentControlDeps{Risk: riskController})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/kill-switch", `{"scope":"emergency"}`)

	handler.EngageKillSwitch(c)

	require.Equal(t, http.StatusOK, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, "kill_switch", response["action"])
	assert.Equal(t, true, response["engage"])
	assert.True(t, riskController.killSwitchEngaged)
}

func TestAgentControlHandler_EngageKillSwitch_Disengage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	riskController := &mockRiskController{killSwitchEngaged: true}
	handler := NewAgentControlHandler(AgentControlDeps{Risk: riskController})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/kill-switch", `{"engage":false}`)

	handler.EngageKillSwitch(c)

	require.Equal(t, http.StatusOK, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, false, response["engage"])
	assert.False(t, riskController.killSwitchEngaged)
}

func TestAgentControlHandler_EngageKillSwitch_NilEngageDefaultsToTrue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	riskController := &mockRiskController{}
	handler := NewAgentControlHandler(AgentControlDeps{Risk: riskController})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/kill-switch", `{}`)

	handler.EngageKillSwitch(c)

	require.Equal(t, http.StatusOK, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, true, response["engage"])
	assert.True(t, riskController.killSwitchEngaged)
}

func TestAgentControlHandler_EngageKillSwitch_ServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAgentControlHandler(AgentControlDeps{})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/kill-switch", `{}`)

	handler.EngageKillSwitch(c)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "risk service unavailable", response["error"])
}

func TestAgentControlHandler_CancelAllOrders_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orders := &mockOrderController{}
	handler := NewAgentControlHandler(AgentControlDeps{Orders: orders})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/cancel-all-orders", `{"exchange_id":"binance","symbol":"BTC/USDT"}`)

	handler.CancelAllOrders(c)

	require.Equal(t, http.StatusOK, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "ok", response["status"])
	assert.Equal(t, "cancel_all_orders", response["action"])
	assert.Equal(t, "BTC/USDT", response["symbol"])
	assert.True(t, orders.cancelled)
}

func TestAgentControlHandler_CancelAllOrders_ServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAgentControlHandler(AgentControlDeps{})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/cancel-all-orders", `{}`)

	handler.CancelAllOrders(c)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "order execution service unavailable", response["error"])
}

func TestAgentControlHandler_ResumeExchange_MissingExchangeID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewAgentControlHandler(AgentControlDeps{Collector: &mockCollectorController{}})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/resume-exchange", `{}`)

	handler.ResumeExchange(c)

	require.Equal(t, http.StatusBadRequest, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "exchange_id is required", response["error"])
}

func TestAgentControlHandler_DisableSafeMode_Error(t *testing.T) {
	gin.SetMode(gin.TestMode)
	riskController := &mockRiskController{err: errors.New("safe mode unavailable")}
	handler := NewAgentControlHandler(AgentControlDeps{Risk: riskController})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/disable-safe-mode", `{}`)

	handler.DisableSafeMode(c)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "safe mode unavailable", response["error"])
	assert.Equal(t, "disable_safe_mode", response["action"])
}

func TestAgentControlHandler_CancelAllOrders_Error(t *testing.T) {
	gin.SetMode(gin.TestMode)
	orders := &mockOrderController{err: errors.New("exchange unreachable")}
	handler := NewAgentControlHandler(AgentControlDeps{Orders: orders})
	c, w := makeJSONContext(http.MethodPost, "/api/v1/agent/cancel-all-orders", `{"exchange_id":"binance","symbol":"BTC/USDT"}`)

	handler.CancelAllOrders(c)

	require.Equal(t, http.StatusInternalServerError, w.Code)
	response := decodeResponseBody(t, w.Body.Bytes())
	assert.Equal(t, "exchange unreachable", response["error"])
	assert.Equal(t, "cancel_all_orders", response["action"])
}
