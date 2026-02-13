package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/services"
)

type WalletHandler struct {
	validator *services.WalletValidator
}

func NewWalletHandler(validator *services.WalletValidator) *WalletHandler {
	return &WalletHandler{validator: validator}
}

type ValidateWalletRequest struct {
	ChatID string `json:"chat_id" binding:"required"`
}

type WalletValidationResponse struct {
	Valid          bool     `json:"valid"`
	ChatID         string   `json:"chat_id"`
	USDCBalance    string   `json:"usdc_balance"`
	PortfolioValue string   `json:"portfolio_value"`
	ExchangeCount  int      `json:"exchange_count"`
	FailedChecks   []string `json:"failed_checks,omitempty"`
}

func (h *WalletHandler) ValidateWallet(c *gin.Context) {
	var req ValidateWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	status, err := h.validator.CheckWalletMinimums(c.Request.Context(), req.ChatID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := WalletValidationResponse{
		Valid:          status.IsValid,
		ChatID:         status.ChatID,
		USDCBalance:    status.USDCBalance.String(),
		PortfolioValue: status.PortfolioValue.String(),
		ExchangeCount:  status.ExchangeCount,
		FailedChecks:   status.FailedChecks,
	}

	c.JSON(http.StatusOK, response)
}
