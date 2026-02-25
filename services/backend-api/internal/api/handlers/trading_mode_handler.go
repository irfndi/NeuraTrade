package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services"
)

// OperationalModeHandler handles trading mode HTTP requests
type OperationalModeHandler struct {
	opModeService *services.OperationalModeService
}

// NewOperationalModeHandler creates a new trading mode handler
func NewOperationalModeHandler(opModeService *services.OperationalModeService) *OperationalModeHandler {
	return &OperationalModeHandler{
		opModeService: opModeService,
	}
}

// GetTradingMode returns the current trading mode for a chat
func (h *OperationalModeHandler) GetTradingMode(c *gin.Context) {
	chatID := c.Param("chatId")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	state := h.opModeService.GetState(chatID)

	c.JSON(http.StatusOK, gin.H{
		"mode":                   string(state.Mode),
		"confirmations":          state.Confirmations,
		"required_confirmations": 2, // Default requirement
		"changed_at":             state.ChangedAt,
		"changed_by":             state.ChangedBy,
	})
}

// SetTradingMode sets the trading mode for a chat
func (h *OperationalModeHandler) SetTradingMode(c *gin.Context) {
	chatID := c.Param("chatId")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	var req struct {
		Mode      string `json:"mode" binding:"required"`
		ChangedBy string `json:"changed_by"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: " + err.Error()})
		return
	}

	// Validate mode
	if req.Mode != "dry" && req.Mode != "live" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "mode must be 'dry' or 'live'"})
		return
	}

	if req.ChangedBy == "" {
		req.ChangedBy = "telegram"
	}

	err := h.opModeService.SetMode(c.Request.Context(), chatID, services.OperationalMode(req.Mode), req.ChangedBy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"mode":    req.Mode,
	})
}

// AddTradingModeConfirmation adds a confirmation for live mode
func (h *OperationalModeHandler) AddTradingModeConfirmation(c *gin.Context) {
	chatID := c.Param("chatId")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	var req struct {
		ConfirmedBy string `json:"confirmed_by"`
	}

	// Bind JSON if present, but don't require it
	_ = c.ShouldBindJSON(&req)

	if req.ConfirmedBy == "" {
		req.ConfirmedBy = "telegram"
	}

	confirmations, err := h.opModeService.AddConfirmation(c.Request.Context(), chatID, req.ConfirmedBy)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"confirmations": 0,
			"required":      2,
			"error":         err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"confirmations": confirmations,
		"required":      2,
	})
}

// ResetTradingModeConfirmations resets confirmations for a chat
func (h *OperationalModeHandler) ResetTradingModeConfirmations(c *gin.Context) {
	chatID := c.Param("chatId")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	err := h.opModeService.ResetConfirmations(c.Request.Context(), chatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "confirmations reset",
	})
}

// GetTradingModeInfo returns human-readable mode info
func (h *OperationalModeHandler) GetTradingModeInfo(c *gin.Context) {
	chatID := c.Param("chatId")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	info := h.opModeService.GetModeInfo(chatID)

	c.JSON(http.StatusOK, gin.H{
		"info": info,
	})
}
