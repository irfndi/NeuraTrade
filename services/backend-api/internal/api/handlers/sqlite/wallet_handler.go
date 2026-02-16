package sqlite

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
)

// WalletHandler handles wallet operations for SQLite
type WalletHandler struct {
	db *database.SQLiteDB
}

// NewWalletHandler creates a new SQLite wallet handler
func NewWalletHandler(db *database.SQLiteDB) *WalletHandler {
	return &WalletHandler{db: db}
}

// Wallet represents a wallet in the system (matches sqlite_schema.sql)
type Wallet struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Name      string    `json:"name"`
	Exchange  string    `json:"exchange"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// GetWallets returns all wallets for a user
func (h *WalletHandler) GetWallets(c *gin.Context) {
	// Get user ID from query or use default
	chatID := c.Query("chat_id")
	if chatID == "" {
		chatID = "1082762347"
	}

	// Find user
	var userID int
	err := h.db.DB.QueryRow("SELECT id FROM users WHERE telegram_id = ?", chatID).Scan(&userID)
	if err != nil {
		// Return empty list if user not found
		c.JSON(http.StatusOK, gin.H{"wallets": []interface{}{}})
		return
	}

	// Query wallets (matches sqlite_schema.sql: id, user_id, name, exchange, is_active, created_at)
	rows, err := h.db.DB.Query(
		"SELECT id, user_id, name, exchange, is_active, created_at FROM wallets WHERE user_id = ?",
		userID,
	)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"wallets": []interface{}{}})
		return
	}
	defer func() { _ = rows.Close() }()

	var wallets []Wallet
	for rows.Next() {
		var w Wallet
		err := rows.Scan(&w.ID, &w.UserID, &w.Name, &w.Exchange, &w.IsActive, &w.CreatedAt)
		if err != nil {
			continue
		}
		wallets = append(wallets, w)
	}

	c.JSON(http.StatusOK, gin.H{"wallets": wallets})
}

// AddWalletRequest represents a request to add a wallet
type AddWalletRequest struct {
	Name     string `json:"name" binding:"required"`
	Exchange string `json:"exchange" binding:"required"`
}

// AddWallet adds a new wallet
func (h *WalletHandler) AddWallet(c *gin.Context) {
	var req AddWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user
	chatID := c.Query("chat_id")
	if chatID == "" {
		chatID = "1082762347"
	}

	var userID int
	err := h.db.DB.QueryRow("SELECT id FROM users WHERE telegram_id = ?", chatID).Scan(&userID)
	if err != nil {
		// Create user if not exists
		result, err := h.db.DB.Exec(
			"INSERT INTO users (telegram_id, risk_level, created_at) VALUES (?, 'medium', ?)",
			chatID, time.Now().Format("2006-01-02 15:04:05"),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}
		userID64, _ := result.LastInsertId()
		userID = int(userID64)
	}

	// Insert wallet (matches sqlite_schema.sql)
	_, err = h.db.DB.Exec(
		"INSERT INTO wallets (user_id, name, exchange, is_active, created_at) VALUES (?, ?, ?, 1, ?)",
		userID, req.Name, req.Exchange, time.Now().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add wallet"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Wallet added successfully",
		"wallet": gin.H{
			"name":     req.Name,
			"exchange": req.Exchange,
		},
	})
}

// RemoveWallet removes a wallet (requires user authorization)
type RemoveWalletRequest struct {
	Name string `json:"name" binding:"required"`
}

func (h *WalletHandler) RemoveWallet(c *gin.Context) {
	var req RemoveWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user ID from query for authorization
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	// Find user to verify ownership
	var userID int
	err := h.db.DB.QueryRow("SELECT id FROM users WHERE telegram_id = ?", chatID).Scan(&userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Delete wallet only if it belongs to this user
	result, err := h.db.DB.Exec("DELETE FROM wallets WHERE name = ? AND user_id = ?", req.Name, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove wallet"})
		return
	}

	// Check if any row was affected
	rowsAffected, err := result.RowsAffected()
	if err != nil || rowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Wallet not found or not owned by user"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Wallet removed successfully"})
}

// ConnectExchangeRequest represents a request to connect an exchange
type ConnectExchangeRequest struct {
	Exchange   string `json:"exchange" binding:"required"`
	APIKey     string `json:"api_key" binding:"required"`
	APISecret  string `json:"api_secret" binding:"required"`
	Passphrase string `json:"passphrase"`
}

// ConnectExchange connects an exchange API
func (h *WalletHandler) ConnectExchange(c *gin.Context) {
	var req ConnectExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user
	chatID := c.Query("chat_id")
	if chatID == "" {
		chatID = "1082762347"
	}

	var userID int
	err := h.db.DB.QueryRow("SELECT id FROM users WHERE telegram_id = ?", chatID).Scan(&userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// SECURITY NOTE: In production, API keys should be encrypted before storage
	// Use the crypto package: internal/crypto/encryption.go
	// For now, storing plaintext (not recommended for production)
	_, err = h.db.DB.Exec(
		`INSERT INTO exchange_api_keys (user_id, exchange, api_key_encrypted, api_secret_encrypted, testnet, created_at)
		 VALUES (?, ?, ?, ?, 0, ?)`,
		userID, req.Exchange, req.APIKey, req.APISecret, time.Now().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store API keys"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Exchange connected successfully",
		"exchange": req.Exchange,
	})
}

// GetWalletBalance returns wallet balance (mock for now)
func (h *WalletHandler) GetWalletBalance(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"balances": []gin.H{
			{"asset": "BTC", "free": "0.5", "locked": "0.0", "total": "0.5"},
			{"asset": "ETH", "free": "5.0", "locked": "0.0", "total": "5.0"},
			{"asset": "USDT", "free": "10000.0", "locked": "0.0", "total": "10000.0"},
		},
		"total_usd": "50000.00",
	})
}
