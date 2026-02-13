package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services/risk"
	"github.com/shopspring/decimal"
)

type RiskHandler struct {
	lossTracker *risk.DailyLossTracker
}

func NewRiskHandler(lossTracker *risk.DailyLossTracker) *RiskHandler {
	return &RiskHandler{
		lossTracker: lossTracker,
	}
}

type DailyLossStatusResponse struct {
	Status        string          `json:"status"`
	CurrentLoss   decimal.Decimal `json:"current_loss"`
	MaxDailyLoss  decimal.Decimal `json:"max_daily_loss"`
	RemainingLoss decimal.Decimal `json:"remaining_loss"`
	LimitExceeded bool            `json:"limit_exceeded"`
}

func (h *RiskHandler) GetDailyLossStatus(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		userID = "default"
	}

	currentLoss, err := h.lossTracker.GetCurrentLoss(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "failed to get daily loss status",
		})
		return
	}

	maxLoss := h.lossTracker.Config().MaxDailyLoss
	exceeded := currentLoss.GreaterThanOrEqual(maxLoss)
	remaining := maxLoss.Sub(currentLoss)
	if remaining.LessThan(decimal.Zero) {
		remaining = decimal.Zero
	}

	status := "ok"
	if exceeded {
		status = "exceeded"
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data": DailyLossStatusResponse{
			Status:        status,
			CurrentLoss:   currentLoss,
			MaxDailyLoss:  maxLoss,
			RemainingLoss: remaining,
			LimitExceeded: exceeded,
		},
	})
}

func (h *RiskHandler) CheckTradeAllowed(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		userID = "default"
	}

	exceeded, currentLoss, err := h.lossTracker.CheckLossLimit(c.Request.Context(), userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"status": "error",
			"error":  "failed to check loss limit",
		})
		return
	}

	if exceeded {
		c.JSON(http.StatusForbidden, gin.H{
			"status": "error",
			"error":  "daily loss limit exceeded",
			"data": gin.H{
				"current_loss":   currentLoss,
				"max_daily_loss": h.lossTracker.Config().MaxDailyLoss,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data": gin.H{
			"allowed": true,
		},
	})
}
