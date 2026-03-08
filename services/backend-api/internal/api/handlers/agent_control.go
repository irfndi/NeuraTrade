// Package handlers provides HTTP handlers for agent control API.
package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/autonomous"
	"github.com/irfndi/neuratrade/internal/services"
)

// AgentControlHandler handles agent control API requests.
type AgentControlHandler struct {
	mu          sync.RWMutex
	subscribers map[chan AgentEvent]struct{}
	autonomy    *services.ScalpingAutonomyCoordinator
}

// NewAgentControlHandler creates a new agent control handler.
func NewAgentControlHandler(autonomy *services.ScalpingAutonomyCoordinator) *AgentControlHandler {
	return &AgentControlHandler{
		subscribers: make(map[chan AgentEvent]struct{}),
		autonomy:    autonomy,
	}
}

// AgentCommandRequest represents an agent command request.
type AgentCommandRequest struct {
	ExchangeID string `json:"exchange_id,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Engage     *bool  `json:"engage,omitempty"`
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

	// TODO: Wire to actual collector service
	// For now, just acknowledge the command
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

	// TODO: Wire to actual collector service
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
	// TODO: Wire to actual risk service
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
	// TODO: Wire to actual risk service
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"action": "disable_safe_mode",
	})
}

// EngageKillSwitch handles requests to engage the kill switch.
// @Summary Engage kill switch
// @Description Engage kill switch (hard stop all trading)
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

	// TODO: Wire to actual risk service
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"action": "kill_switch",
		"engage": req.Engage,
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

	// TODO: Wire to actual execution service
	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"action": "cancel_all_orders",
		"scope":  req.Scope,
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
				log.Printf("agent_control: failed to marshal event for stream: %v", err)
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
