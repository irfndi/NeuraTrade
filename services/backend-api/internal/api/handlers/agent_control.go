// Package handlers provides HTTP handlers for agent control API.
package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/services"
)

type CollectorController interface {
	PauseExchange(exchangeID string) error
	ResumeExchange(exchangeID string) error
}

type RiskController interface {
	EnableSafeMode(ctx context.Context, reason string) error
	DisableSafeMode(ctx context.Context) error
	EngageKillSwitch(ctx context.Context, reason string) error
	DisengageKillSwitch(ctx context.Context) error
}

type OrderController interface {
	CancelAllOrders(ctx context.Context, exchange, symbol string) error
}

type AgentControlDeps struct {
	Autonomy  *services.ScalpingAutonomyCoordinator
	Collector CollectorController
	Risk      RiskController
	Orders    OrderController
}

// AgentControlHandler handles agent control API requests.
type AgentControlHandler struct {
	mu          sync.RWMutex
	subscribers map[chan AgentEvent]struct{}
	autonomy    *services.ScalpingAutonomyCoordinator
	collector   CollectorController
	risk        RiskController
	orders      OrderController
}

// NewAgentControlHandler creates a new agent control handler.
func NewAgentControlHandler(deps AgentControlDeps) *AgentControlHandler {
	return &AgentControlHandler{
		subscribers: make(map[chan AgentEvent]struct{}),
		autonomy:    deps.Autonomy,
		collector:   deps.Collector,
		risk:        deps.Risk,
		orders:      deps.Orders,
	}
}

// AgentCommandRequest represents an agent command request.
type AgentCommandRequest struct {
	ExchangeID string `json:"exchange_id,omitempty"`
	Symbol     string `json:"symbol,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Engage     *bool  `json:"engage,omitempty"`
	ConfirmAll bool   `json:"confirm_all,omitempty"`
}

type StrategyModeRequest struct {
	StrategyID string `json:"strategy_id,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	Mode       string `json:"mode" binding:"required"`
}

// AgentEvent represents a control-plane event streamed to the agent service.
type AgentEvent struct {
	ID        string         `json:"id"`
	Topic     string         `json:"topic"`
	Type      string         `json:"type"`
	Payload   any            `json:"payload"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

// PauseExchange handles requests to pause market data collection for an exchange.
// @Summary Pause exchange
// @Description Pause market data collection for a specific exchange
// @Tags agent
// @Accept json
// @Produce json
// @Param request body AgentCommandRequest true "Exchange ID"
// @Success 200 {object} map[string]string
// @Router /api/v1/agent/pause-exchange [post]
func (h *AgentControlHandler) PauseExchange(c *gin.Context) {
	var req AgentCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.ExchangeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange_id is required"})
		return
	}
	if h.collector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "collector service unavailable"})
		return
	}
	if err := h.collector.PauseExchange(req.ExchangeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "action": "pause_exchange"})
		return
	}
	zaplogrus.Infof("agent_control: exchange %s collection paused", req.ExchangeID)
	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"action":      "pause_exchange",
		"exchange_id": req.ExchangeID,
	})
}

// ResumeExchange handles requests to resume market data collection for an exchange.
// @Summary Resume exchange
// @Description Resume market data collection for a specific exchange
// @Tags agent
// @Accept json
// @Produce json
// @Param request body AgentCommandRequest true "Exchange ID"
// @Success 200 {object} map[string]string
// @Router /api/v1/agent/resume-exchange [post]
func (h *AgentControlHandler) ResumeExchange(c *gin.Context) {
	var req AgentCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	if req.ExchangeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange_id is required"})
		return
	}
	if h.collector == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "collector service unavailable"})
		return
	}
	if err := h.collector.ResumeExchange(req.ExchangeID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "action": "resume_exchange"})
		return
	}
	zaplogrus.Infof("agent_control: exchange %s collection resumed", req.ExchangeID)
	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"action":      "resume_exchange",
		"exchange_id": req.ExchangeID,
	})
}

// EnableSafeMode handles requests to enable safe mode.
// @Summary Enable safe mode
// @Description Enable safe mode (blocks new trades)
// @Tags agent
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/agent/enable-safe-mode [post]
func (h *AgentControlHandler) EnableSafeMode(c *gin.Context) {
	if h.risk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "risk service unavailable"})
		return
	}
	if err := h.risk.EnableSafeMode(c.Request.Context(), "operator request"); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "action": "enable_safe_mode"})
		return
	}
	zaplogrus.Infof("agent_control: safe mode enabled")
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"action": "enable_safe_mode",
	})
}

// DisableSafeMode handles requests to disable safe mode.
// @Summary Disable safe mode
// @Description Disable safe mode
// @Tags agent
// @Accept json
// @Produce json
// @Success 200 {object} map[string]string
// @Router /api/v1/agent/disable-safe-mode [post]
func (h *AgentControlHandler) DisableSafeMode(c *gin.Context) {
	if h.risk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "risk service unavailable"})
		return
	}
	if err := h.risk.DisableSafeMode(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "action": "disable_safe_mode"})
		return
	}
	zaplogrus.Infof("agent_control: safe mode disabled")
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"action": "disable_safe_mode",
	})
}

// EngageKillSwitch handles requests to engage or disengage the kill switch.
// @Summary Engage kill switch
// @Description Engage kill switch (hard stop all trading). Set engage=false to disengage.
// @Tags agent
// @Accept json
// @Produce json
// @Param request body AgentCommandRequest true "Engage flag"
// @Success 200 {object} map[string]string
// @Router /api/v1/agent/kill-switch [post]
func (h *AgentControlHandler) EngageKillSwitch(c *gin.Context) {
	var req AgentCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if h.risk == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "risk service unavailable"})
		return
	}

	engageVal := true
	if req.Engage != nil {
		engageVal = *req.Engage
	}

	reason := "operator request"
	if req.Scope != "" {
		reason = req.Scope
	}

	var err error
	if engageVal {
		err = h.risk.EngageKillSwitch(c.Request.Context(), reason)
	} else {
		err = h.risk.DisengageKillSwitch(c.Request.Context())
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "action": "kill_switch"})
		return
	}

	action := "engage_kill_switch"
	if !engageVal {
		action = "disengage_kill_switch"
	}
	zaplogrus.Infof("agent_control: kill switch %s", action)
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"action": "kill_switch",
		"engage": engageVal,
	})
}

// CancelAllOrders handles requests to cancel all open orders.
// @Summary Cancel all orders
// @Description Cancel all open orders
// @Tags agent
// @Accept json
// @Produce json
// @Param request body AgentCommandRequest true "Scope"
// @Success 200 {object} map[string]string
// @Router /api/v1/agent/cancel-all-orders [post]
func (h *AgentControlHandler) CancelAllOrders(c *gin.Context) {
	var req AgentCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if req.ExchangeID == "" && !req.ConfirmAll {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "exchange_id is required for targeted cancel; set confirm_all=true to cancel across all exchanges",
		})
		return
	}

	if h.orders == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "order execution service unavailable"})
		return
	}
	if err := h.orders.CancelAllOrders(c.Request.Context(), req.ExchangeID, req.Symbol); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "action": "cancel_all_orders"})
		return
	}
	zaplogrus.Infof("agent_control: all orders cancelled (exchange=%s, symbol=%s)", req.ExchangeID, req.Symbol)
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"action": "cancel_all_orders",
		"symbol": req.Symbol,
	})
}

// SetStrategyMode updates the rollout mode for a strategy.
// @Summary Set strategy mode
// @Description Set a strategy rollout mode (shadow, paper, live)
// @Tags agent
// @Accept json
// @Produce json
// @Param request body StrategyModeRequest true "Strategy mode request"
// @Success 200 {object} map[string]any
// @Router /api/v1/agent/strategy-mode [post]
func (h *AgentControlHandler) SetStrategyMode(c *gin.Context) {
	if h.autonomy == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "autonomy coordinator unavailable"})
		return
	}

	var req StrategyModeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	strategyID := strings.TrimSpace(req.StrategyID)
	if strategyID == "" && strings.TrimSpace(req.ChatID) != "" {
		strategyID = services.ScalpingStrategyID(req.ChatID)
	}
	if strategyID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "strategy_id or chat_id is required"})
		return
	}

	mode := autonomous.StrategyMode(strings.ToLower(strings.TrimSpace(req.Mode)))
	switch mode {
	case autonomous.ModeShadow, autonomous.ModePaper, autonomous.ModeLive:
	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error":          fmt.Sprintf("invalid mode %q; allowed modes are: shadow, paper, live", mode),
			"allowed_modes":  []string{string(autonomous.ModeShadow), string(autonomous.ModePaper), string(autonomous.ModeLive)},
			"requested_mode": mode,
		})
		return
	}

	state, err := h.autonomy.SetStrategyMode(c.Request.Context(), strategyID, mode)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":         "ok",
		"strategy_id":    strategyID,
		"mode":           mode,
		"current_stage":  state.CurrentStage,
		"current_status": state.Status,
		"entered_at":     state.EnteredAt.UTC().Format(time.RFC3339),
	})
}

// StreamEvents streams backend events to agent-control via SSE.
// @Summary Stream agent events
// @Description Stream operational and risk events for agent-control consumers
// @Tags agent
// @Produce text/event-stream
// @Success 200 {string} string "SSE stream"
// @Router /api/v1/agent/events [get]
func (h *AgentControlHandler) StreamEvents(c *gin.Context) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "streaming unsupported"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	subscription := h.subscribe()
	defer h.unsubscribe(subscription)

	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-subscription:
			payload, err := json.Marshal(event)
			if err != nil {
				zaplogrus.Warnf("agent_control: failed to marshal event for stream: %v", err)
				continue
			}

			if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		case <-heartbeat.C:
			if _, err := c.Writer.Write([]byte(": keep-alive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// PublishEvent publishes an event to connected agent-control stream subscribers.
// @Summary Publish agent event
// @Description Publish operational/risk event to agent-control consumers
// @Tags agent
// @Accept json
// @Produce json
// @Param request body AgentEvent true "Event payload"
// @Success 202 {object} map[string]any
// @Router /api/v1/agent/events [post]
func (h *AgentControlHandler) PublishEvent(c *gin.Context) {
	var event AgentEvent
	if err := c.ShouldBindJSON(&event); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if event.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type is required"})
		return
	}
	if event.Topic == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "topic is required"})
		return
	}
	if event.ID == "" {
		event.ID = fmt.Sprintf("evt-%d", time.Now().UTC().UnixNano())
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.Source == "" {
		event.Source = "backend-api"
	}

	h.broadcast(event)
	c.JSON(http.StatusAccepted, gin.H{
		"status":   "accepted",
		"event_id": event.ID,
	})
}

func (h *AgentControlHandler) subscribe() chan AgentEvent {
	ch := make(chan AgentEvent, 128)

	h.mu.Lock()
	h.subscribers[ch] = struct{}{}
	h.mu.Unlock()

	return ch
}

func (h *AgentControlHandler) unsubscribe(ch chan AgentEvent) {
	h.mu.Lock()
	delete(h.subscribers, ch)
	h.mu.Unlock()
}

func (h *AgentControlHandler) broadcast(event AgentEvent) {
	h.mu.RLock()
	subscribers := make([]chan AgentEvent, 0, len(h.subscribers))
	for ch := range h.subscribers {
		subscribers = append(subscribers, ch)
	}
	h.mu.RUnlock()

	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
			// Drop when subscriber is slow to preserve bounded memory.
		}
	}
}
