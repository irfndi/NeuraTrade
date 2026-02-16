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

// Wallet represents a wallet in the system
type Wallet struct {
	ID        int       `json:"id"`
	Chain     string    `json:"chain"`
	Address   string    `json:"address"`
	Type      string    `json:"type"`
	Label     string    `json:"label"`
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

	// Query wallets
	rows, err := h.db.DB.Query(
		"SELECT id, chain, address, wallet_type, label, created_at FROM wallets WHERE user_id = ?",
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
		err := rows.Scan(&w.ID, &w.Chain, &w.Address, &w.Type, &w.Label, &w.CreatedAt)
		if err != nil {
			continue
		}
		wallets = append(wallets, w)
	}

	c.JSON(http.StatusOK, gin.H{"wallets": wallets})
}

// AddWalletRequest represents a request to add a wallet
type AddWalletRequest struct {
	Chain   string `json:"chain" binding:"required"`
	Address string `json:"address" binding:"required"`
	Type    string `json:"type"`
	Label   string `json:"label"`
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
			chatID, time.Now(),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create user"})
			return
		}
		userID64, _ := result.LastInsertId()
		userID = int(userID64)
	}

	// Insert wallet
	_, err = h.db.DB.Exec(
		"INSERT INTO wallets (user_id, chain, address, wallet_type, label, created_at) VALUES (?, ?, ?, ?, ?, ?)",
		userID, req.Chain, req.Address, req.Type, req.Label, time.Now(),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to add wallet"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "Wallet added successfully",
		"wallet": gin.H{
			"chain":   req.Chain,
			"address": req.Address,
			"type":    req.Type,
			"label":   req.Label,
		},
	})
}

// RemoveWallet removes a wallet
type RemoveWalletRequest struct {
	Address string `json:"address" binding:"required"`
}

func (h *WalletHandler) RemoveWallet(c *gin.Context) {
	var req RemoveWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	_, err := h.db.DB.Exec("DELETE FROM wallets WHERE address = ?", req.Address)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to remove wallet"})
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

	// Store API key (in production, this should be encrypted)
	_, err = h.db.DB.Exec(
		`INSERT INTO api_keys (user_id, provider, provider_type, encrypted_key, encrypted_secret, created_at) 
		 VALUES (?, ?, 'exchange', ?, ?, ?)`,
		userID, req.Exchange, req.APIKey, req.APISecret, time.Now(),
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
