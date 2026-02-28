// Package handlers provides HTTP handlers for agent control API.
package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// AgentControlHandler handles agent control API requests.
type AgentControlHandler struct {
	// Add dependencies as needed (event bus, policy engine, etc.)
}

// NewAgentControlHandler creates a new agent control handler.
func NewAgentControlHandler() *AgentControlHandler {
	return &AgentControlHandler{}
}

// AgentCommandRequest represents an agent command request.
type AgentCommandRequest struct {
	ExchangeID string `json:"exchange_id,omitempty"`
	Scope      string `json:"scope,omitempty"`
	Engage     *bool  `json:"engage,omitempty"`
}

// PauseExchange handles requests to pause market data collection for an exchange.
// @Summary Pause exchange
// @Description Pause market data collection for a specific exchange
// @Tags agent
// @Accept json
// @Produce json
// @Param request body AgentCommandRequest true "Exchange ID"
// @Success 200 {object} map[string]string
// @Router /api/agent/pause-exchange [post]
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
// @Router /api/agent/resume-exchange [post]
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
// @Router /api/agent/enable-safe-mode [post]
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
// @Router /api/agent/disable-safe-mode [post]
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
// @Router /api/agent/kill-switch [post]
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
// @Router /api/agent/cancel-all-orders [post]
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
