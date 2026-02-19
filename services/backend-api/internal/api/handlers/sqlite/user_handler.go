// Package sqlite provides SQLite-specific API handlers for NeuraTrade.
package sqlite

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
	"golang.org/x/crypto/bcrypt"
)

// UserHandler handles user management for SQLite mode.
// Manages user registration, authentication, and profile operations.
type UserHandler struct {
	db *database.SQLiteDB
}

// NewUserHandler creates a new SQLite user handler.
//
// Parameters:
//   - db: SQLite database connection.
//
// Returns:
//   - *UserHandler: Initialized handler instance.
func NewUserHandler(db *database.SQLiteDB) *UserHandler {
	return &UserHandler{db: db}
}

// RegisterRequest represents a user registration request.
// Contains user credentials and optional Telegram linkage.
type RegisterRequest struct {
	Email          string `json:"email" binding:"required,email"`
	Password       string `json:"password" binding:"required,min=8"`
	TelegramChatID string `json:"telegram_chat_id"`
}

// RegisterUser handles user registration.
// Creates a new user account with email/password authentication.
//
// Parameters:
//   - c: Gin context with JSON request body.
//
// Request Body:
//   - email: User email address (must be valid format).
//   - password: User password (minimum 8 characters, will be hashed with bcrypt).
//   - telegram_chat_id: Optional Telegram chat ID for notifications.
//
// Response:
//   - 201: User created successfully.
//   - 400: Invalid request body.
//   - 409: User already exists.
//   - 500: Database error.
func (h *UserHandler) RegisterUser(c *gin.Context) {
	var req RegisterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Check if user already exists (by telegram_id or email)
	var existingID int
	err := h.db.DB.QueryRow(
		"SELECT id FROM users WHERE telegram_id = ? OR email = ?",
		req.TelegramChatID, req.Email,
	).Scan(&existingID)

	if err == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "User already exists"})
		return
	}

	// Hash password with bcrypt
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to hash password"})
		return
	}

	// Generate a random username if not provided
	username := req.TelegramChatID
	if username == "" {
		username = generateRandomUsername()
	}

	// Insert new user with hashed password
	result, err := h.db.DB.Exec(
		`INSERT INTO users (telegram_id, email, username, password_hash, risk_level, created_at)
		 VALUES (?, ?, ?, ?, 'medium', ?)`,
		req.TelegramChatID,
		req.Email,
		username,
		string(hashedPassword),
		time.Now().Format("2006-01-02 15:04:05"),
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
		return
	}

	userID, _ := result.LastInsertId()

	c.JSON(http.StatusCreated, gin.H{
		"id":          userID,
		"telegram_id": req.TelegramChatID,
		"email":       req.Email,
		"risk_level":  "medium",
		"created_at":  time.Now(),
	})
}

// generateRandomUsername generates a random username for users without Telegram
func generateRandomUsername() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		return "user"
	}
	return "user_" + hex.EncodeToString(bytes)[:8]
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

// LoginRequest represents a user login request
type LoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse represents a user login response
type LoginResponse struct {
	Success bool   `json:"success"`
	Token   string `json:"token,omitempty"`
	UserID  int    `json:"user_id,omitempty"`
	Message string `json:"message,omitempty"`
}

// LoginUser handles user login with email/password authentication.
// Verifies password hash and returns authentication token.
//
// Parameters:
//   - c: Gin context with JSON request body.
//
// Request Body:
//   - email: User email address.
//   - password: User password to verify.
//
// Response:
//   - 200: Login successful with token.
//   - 400: Invalid request body.
//   - 401: Invalid credentials.
//   - 404: User not found.
//   - 500: Database error.
func (h *UserHandler) LoginUser(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Find user by email
	var userID int
	var passwordHash string
	err := h.db.DB.QueryRow(
		"SELECT id, password_hash FROM users WHERE email = ?",
		req.Email,
	).Scan(&userID, &passwordHash)

	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}

	// Verify password
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid credentials"})
		return
	}

	// Generate a simple token (in production, use JWT)
	token := generateAuthToken(userID)

	c.JSON(http.StatusOK, LoginResponse{
		Success: true,
		Token:   token,
		UserID:  userID,
		Message: "Login successful",
	})
}

// generateAuthToken generates a simple auth token for the user
func generateAuthToken(userID int) string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "mock-token"
	}
	return hex.EncodeToString(bytes)
}
