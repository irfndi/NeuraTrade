package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/irfandi/celebrum-ai-go/internal/services"
)

// TelegramInternalHandler handles internal API requests from the Telegram service.
type TelegramInternalHandler struct {
	db          services.DBPool
	userHandler *UserHandler
}

// NewTelegramInternalHandler creates a new instance of TelegramInternalHandler.
func NewTelegramInternalHandler(db any, userHandler *UserHandler) *TelegramInternalHandler {
	return &TelegramInternalHandler{
		db:          normalizeDBPool(db),
		userHandler: userHandler,
	}
}

// GetUserByChatID retrieves a user by their Telegram chat ID.
func (h *TelegramInternalHandler) GetUserByChatID(c *gin.Context) {
	chatID := c.Param("id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Chat ID required"})
		return
	}

	user, err := h.userHandler.GetUserByTelegramChatID(c.Request.Context(), chatID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"user": gin.H{
			"id":                user.ID,
			"subscription_tier": user.SubscriptionTier,
			"created_at":        user.CreatedAt,
		},
	})
}

// GetNotificationPreferences retrieves notification settings for a user.
func (h *TelegramInternalHandler) GetNotificationPreferences(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID required"})
		return
	}

	// 1. Check if explicitly disabled
	queryDisabled := `
		SELECT COUNT(*) 
		FROM user_alerts 
		WHERE user_id = $1 
		  AND alert_type = 'arbitrage' 
		  AND is_active = false
	`
	var countDisabled int
	err := h.db.QueryRow(c.Request.Context(), queryDisabled, userID).Scan(&countDisabled)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch preferences"})
		return
	}
	enabled := countDisabled == 0

	// 2. Fetch active alert to get thresholds
	// If multiple, pick the most recent one
	queryActive := `
		SELECT conditions
		FROM user_alerts 
		WHERE user_id = $1 
		  AND alert_type = 'arbitrage' 
		  AND is_active = true
		ORDER BY created_at DESC
		LIMIT 1
	`
	var conditionsJSON []byte
	profitThreshold := 0.5 // Default

	// We ignore sql.ErrNoRows here, as we fall back to defaults
	row := h.db.QueryRow(c.Request.Context(), queryActive, userID)
	if err := row.Scan(&conditionsJSON); err == nil {
		var conditions models.AlertConditions
		if err := json.Unmarshal(conditionsJSON, &conditions); err == nil && conditions.ProfitThreshold != nil {
			profitThreshold, _ = conditions.ProfitThreshold.Float64()
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"enabled":          enabled,
		"profit_threshold": profitThreshold,
		"alert_frequency":  "Immediate (Periodic Scan 5m)", // Static for now as it's system config
	})
}

// SetNotificationPreferences updates notification settings for a user.
func (h *TelegramInternalHandler) SetNotificationPreferences(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "User ID required"})
		return
	}

	var req struct {
		Enabled bool `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}

	tx, err := h.db.Begin(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to begin transaction"})
		return
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	if req.Enabled {
		// To enable, we remove any disabling records for 'arbitrage'
		query := `
			DELETE FROM user_alerts 
			WHERE user_id = $1 
			  AND alert_type = 'arbitrage' 
			  AND is_active = false
		`
		_, err := tx.Exec(c.Request.Context(), query, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update preferences"})
			return
		}
	} else {
		// To disable, ensure a disabling record exists
		deleteQuery := `
			DELETE FROM user_alerts 
			WHERE user_id = $1 
			  AND alert_type = 'arbitrage'
		`
		_, err := tx.Exec(c.Request.Context(), deleteQuery, userID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to clear old preferences"})
			return
		}

		insertQuery := `
			INSERT INTO user_alerts (id, user_id, alert_type, conditions, is_active, created_at)
			VALUES ($1, $2, 'arbitrage', $3, false, $4)
		`
		conditions := map[string]interface{}{
			"notifications_enabled": false,
		}
		conditionsJSON, _ := json.Marshal(conditions)

		newID := uuid.New().String()
		_, err = tx.Exec(c.Request.Context(), insertQuery, newID, userID, conditionsJSON, time.Now())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to insert preference"})
			return
		}
	}

	if err := tx.Commit(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit transaction"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "success",
		"enabled": req.Enabled,
	})
}
