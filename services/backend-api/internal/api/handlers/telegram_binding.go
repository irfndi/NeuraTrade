package handlers

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/irfndi/neuratrade/internal/services"
)

// TelegramBindingHandler manages Telegram profile binding operations.
type TelegramBindingHandler struct {
	userHandler *UserHandler
	otpService  *services.OTPService
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
func NewTelegramBindingHandler(userHandler *UserHandler, otpService *services.OTPService) *TelegramBindingHandler {
	return &TelegramBindingHandler{
		userHandler: userHandler,
		otpService:  otpService,
	}
}

// InitiateBinding initiates the Telegram profile binding process.
// It generates a one-time code for the user to verify their identity.
func (h *TelegramBindingHandler) InitiateBinding(c *gin.Context) {
	var req struct {
		UserID string `json:"user_id" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	// Generate OTP code for telegram binding
	otp, err := h.otpService.GenerateCode(c.Request.Context(), req.UserID, models.OTPPurposeTelegramBinding)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to generate binding code: %v", err)})
		return
	}

	c.JSON(http.StatusOK, BindingResponse{
		Success:   true,
		Message:   "Binding code generated. Please verify with the code sent to your Telegram.",
		UserID:    req.UserID,
		ExpiresAt: otp.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
	})
}

// CompleteBinding completes the Telegram profile binding process.
// It verifies the OTP code and binds the Telegram chat ID to the user profile.
func (h *TelegramBindingHandler) CompleteBinding(c *gin.Context) {
	var req BindingRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: user_id, code (6 digits), and chat_id are required"})
		return
	}

	// Verify the OTP code
	if _, err := h.otpService.VerifyCode(c.Request.Context(), req.UserID, req.Code, models.OTPPurposeTelegramBinding); err != nil {
		switch err {
		case services.ErrOTPNotFound:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid or expired code"})
		case services.ErrOTPExpired:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Code has expired"})
		case services.ErrOTPAlreadyUsed:
			c.JSON(http.StatusBadRequest, gin.H{"error": "Code has already been used"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("verification failed: %v", err)})
		}
		return
	}

	// Get user by ID
	user, err := h.userHandler.getUserByID(c.Request.Context(), req.UserID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Update user's Telegram chat ID
	chatID := req.ChatID
	user.TelegramChatID = &chatID

	// Update user profile
	query := `
		UPDATE users
		SET telegram_chat_id = $2, updated_at = $3
		WHERE id = $1
	`
	_, err = c.Request.Context().Value("db").(interface {
		Exec(ctx context.Context, sql string, args ...interface{}) (interface{}, error)
	}).Exec(c.Request.Context(), query, req.UserID, chatID)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update Telegram binding"})
		return
	}

	// Invalidate cache
	h.userHandler.invalidateUserCache(c.Request.Context(), req.UserID, user, &chatID)

	c.JSON(http.StatusOK, BindingResponse{
		Success: true,
		Message: fmt.Sprintf("Telegram profile bound successfully to user %s", user.Email),
		UserID:  req.UserID,
	})
}

// GenerateBindingCode generates a one-time code for Telegram binding.
// This is an alias for InitiateBinding to match API naming conventions.
func (h *TelegramBindingHandler) GenerateBindingCode(c *gin.Context) {
	h.InitiateBinding(c)
}

// VerifyBindingCode verifies the one-time code and binds Telegram chat ID.
// This is an alias for CompleteBinding to match API naming conventions.
func (h *TelegramBindingHandler) VerifyBindingCode(c *gin.Context) {
	h.CompleteBinding(c)
}
