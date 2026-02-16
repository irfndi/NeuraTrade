package sqlite

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
)

// UserHandler handles user management for SQLite
type UserHandler struct {
	db *database.SQLiteDB
}

// NewUserHandler creates a new SQLite user handler
func NewUserHandler(db *database.SQLiteDB) *UserHandler {
	return &UserHandler{db: db}
}

// RegisterRequest represents a user registration request
type RegisterRequest struct {
	Email          string `json:"email" binding:"required,email"`
	Password       string `json:"password" binding:"required,min=8"`
	TelegramChatID string `json:"telegram_chat_id"`
}

// RegisterUser handles user registration
func (h *UserHandler) RegisterUser(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user already exists
	var existingID int
	err := h.db.DB.QueryRow(
		"SELECT id FROM users WHERE telegram_id = ?",
		req.TelegramChatID,
	).Scan(&existingID)

	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	// Insert new user
	result, err := h.db.DB.Exec(
		`INSERT INTO users (telegram_id, risk_level, created_at) 
		 VALUES (?, 'medium', ?)`,
		req.TelegramChatID,
		time.Now(),
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	userID, _ := result.LastInsertId()

	c.JSON(http.StatusCreated, gin.H{
		"id":          userID,
		"telegram_id": req.TelegramChatID,
		"risk_level":  "medium",
		"created_at":  time.Now(),
	})
}

// GetUserByChatID gets a user by Telegram chat ID
func (h *UserHandler) GetUserByChatID(c *gin.Context) {
	chatID := c.Param("id")

	var user struct {
		ID         int       `json:"id"`
		TelegramID string    `json:"telegram_id"`
		RiskLevel  string    `json:"risk_level"`
		CreatedAt  time.Time `json:"created_at"`
	}

	err := h.db.DB.QueryRow(
		"SELECT id, telegram_id, risk_level, created_at FROM users WHERE telegram_id = ?",
		chatID,
	).Scan(&user.ID, &user.TelegramID, &user.RiskLevel, &user.CreatedAt)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	c.JSON(http.StatusOK, user)
}

// GetUserProfile gets the current user's profile
func (h *UserHandler) GetUserProfile(c *gin.Context) {
	// For SQLite mode, return a mock profile
	c.JSON(http.StatusOK, gin.H{
		"id":          1,
		"telegram_id": "1082762347",
		"risk_level":  "medium",
		"created_at":  time.Now(),
		"mode":        "sqlite",
	})
}

// LoginUser handles user login (mock for SQLite)
func (h *UserHandler) LoginUser(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"token":   "sqlite-mock-token",
		"message": "SQLite mode - authentication bypassed",
	})
}
