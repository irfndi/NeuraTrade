package handlers

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/services"
)

// TelegramBindingHandler manages Telegram profile binding operations.
type TelegramBindingHandler struct {
	userHandler *UserHandler
	otpService  *services.OTPService
	db          DBQuerier
}

// BindingRequest represents the Telegram binding request.
type BindingRequest struct {
	UserID string `json:"user_id" binding:"required"`
	Code   string `json:"code" binding:"required,len=6"`
	ChatID string `json:"chat_id" binding:"required"`
}

// BindingResponse represents the binding response.
type BindingResponse struct {
	Success   bool   `json:"success"`
	Message   string `json:"message"`
	UserID    string `json:"user_id,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// NewTelegramBindingHandler creates a new TelegramBindingHandler.
func NewTelegramBindingHandler(db DBQuerier, userHandler *UserHandler, otpService *services.OTPService) *TelegramBindingHandler {
	return &TelegramBindingHandler{
		userHandler: userHandler,
		otpService:  otpService,
		db:          db,
	}
}

// InitiateBinding initiates the Telegram profile binding process.
//
// Fix for NeuraTrade-2puz: the operator identity in the request body is
// overridden by the authenticated user from the JWT context. A caller may
// only initiate binding for their own user_id — they cannot trigger a binding
// code for someone else's profile (the original implementation accepted
// any user_id in the body, allowing "anyone to rebind operator identity").
func (h *TelegramBindingHandler) InitiateBinding(c *gin.Context) {
	authedUserID := strings.TrimSpace(c.GetString("user_id"))
	if authedUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	otp, err := h.otpService.GenerateCode(c.Request.Context(), authedUserID, models.OTPPurposeTelegramBinding)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to generate binding code: %v", err)})
		return
	}

	c.JSON(http.StatusOK, BindingResponse{
		Success:   true,
		Message:   "Binding code generated. Please verify with the code sent to your Telegram.",
		UserID:    authedUserID,
		ExpiresAt: otp.ExpiresAt.Format(time.RFC3339),
	})
}

// CompleteBinding completes the Telegram profile binding process.
//
// Fix for NeuraTrade-2puz: the operator identity is taken from the JWT
// context, not the request body. A caller may only bind their own profile;
// binding another user's chat_id is rejected with 403 Forbidden.
func (h *TelegramBindingHandler) CompleteBinding(c *gin.Context) {
	authedUserID := strings.TrimSpace(c.GetString("user_id"))
	if authedUserID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "authentication required"})
		return
	}

	var req struct {
		Code   string `json:"code" binding:"required,len=6"`
		ChatID string `json:"chat_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: code (6 digits) and chat_id are required"})
		return
	}

	if _, err := h.otpService.VerifyCode(c.Request.Context(), authedUserID, req.Code, models.OTPPurposeTelegramBinding); err != nil {
		switch err {
		case services.ErrOTPNotFound:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired code"})
		case services.ErrOTPExpired:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Code has expired"})
		case services.ErrOTPAlreadyUsed:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Code has already been used"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "verification failed"})
		}
		return
	}

	user, err := h.userHandler.getUserByID(c.Request.Context(), authedUserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	query := `
		UPDATE users
		SET telegram_chat_id = $2, updated_at = $3
		WHERE id = $1
	`
	_, err = h.db.Exec(c.Request.Context(), query, authedUserID, req.ChatID, time.Now())

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Telegram binding"})
		return
	}

	h.userHandler.invalidateUserCache(c.Request.Context(), authedUserID, user, &req.ChatID)

	c.JSON(http.StatusOK, BindingResponse{
		Success: true,
		Message: fmt.Sprintf("Telegram profile bound successfully to user %s", user.Email),
		UserID:  authedUserID,
	})
}

// GenerateBindingCode generates a one-time code for Telegram binding.
func (h *TelegramBindingHandler) GenerateBindingCode(c *gin.Context) {
	h.InitiateBinding(c)
}

// VerifyBindingCode verifies the one-time code and binds Telegram chat ID.
func (h *TelegramBindingHandler) VerifyBindingCode(c *gin.Context) {
	h.CompleteBinding(c)
}
