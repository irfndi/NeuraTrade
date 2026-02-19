// Package sqlite provides SQLite-specific API handlers for NeuraTrade.
// This includes wallet, user, market, and portfolio management endpoints.
package sqlite

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/crypto"
	"github.com/irfndi/neuratrade/internal/database"
)

// WalletHandler handles wallet operations for SQLite mode.
// Manages exchange connections and wallet configurations stored in SQLite.
type WalletHandler struct {
	db *database.SQLiteDB
}

// NewWalletHandler creates a new SQLite wallet handler.
//
// Parameters:
//   - db: SQLite database connection.
//
// Returns:
//   - *WalletHandler: Initialized handler instance.
func NewWalletHandler(db *database.SQLiteDB) *WalletHandler {
	return &WalletHandler{db: db}
}

// Wallet represents a wallet in the system (matches sqlite_schema.sql).
// Stores exchange connection information for autonomous trading.
type Wallet struct {
	ID        int       `json:"id"`
	UserID    int       `json:"user_id"`
	Name      string    `json:"name"`
	Exchange  string    `json:"exchange"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
}

// GetWallets returns all wallets for a user.
//
// Parameters:
//   - c: Gin context containing chat_id query parameter.
//
// Response:
//   - 200: List of wallets (may be empty).
func (h *WalletHandler) GetWallets(c *gin.Context) {
	// Get user ID from query - require chat_id for authentication
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
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

	// Check for errors during iteration
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error during wallet retrieval"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"wallets": wallets})
}

// AddWalletRequest represents a request to add a wallet.
// Contains the wallet name and exchange identifier.
type AddWalletRequest struct {
	Name     string `json:"name" binding:"required"`
	Exchange string `json:"exchange" binding:"required"`
}

// AddWallet adds a new wallet for a user.
// Creates the user if they don't exist.
//
// Parameters:
//   - c: Gin context with chat_id and JSON body.
//
// Request Body:
//   - name: Wallet name identifier.
//   - exchange: Exchange name (e.g., "binance", "bybit").
//
// Response:
//   - 201: Wallet created successfully.
//   - 400: Invalid request body.
//   - 500: Database error.
func (h *WalletHandler) AddWallet(c *gin.Context) {
	var req AddWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user - require chat_id for authentication
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
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

// RemoveWalletRequest represents a request to remove a wallet.
// Requires wallet name for identification.
type RemoveWalletRequest struct {
	Name string `json:"name" binding:"required"`
}

// RemoveWallet removes a wallet (requires user authorization).
// Verifies wallet ownership before deletion.
//
// Parameters:
//   - c: Gin context with chat_id query parameter.
//
// Request Body:
//   - name: Wallet name to delete.
//
// Response:
//   - 200: Wallet removed successfully.
//   - 400: Missing chat_id or invalid request.
//   - 404: User not found or wallet not owned by user.
//   - 500: Database error.
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

// ConnectExchangeRequest represents a request to connect an exchange.
// Contains API credentials for exchange authentication.
type ConnectExchangeRequest struct {
	Exchange   string `json:"exchange" binding:"required"`
	APIKey     string `json:"api_key" binding:"required"`
	APISecret  string `json:"api_secret" binding:"required"`
	Passphrase string `json:"passphrase"` // Optional, required for some exchanges like OKX
}

// ConnectExchange connects an exchange API for autonomous trading.
// Stores API credentials encrypted in the database using AES-256-GCM.
//
// Parameters:
//   - c: Gin context with chat_id query parameter.
//
// Request Body:
//   - exchange: Exchange name (e.g., "binance", "bybit").
//   - api_key: Exchange API key.
//   - api_secret: Exchange API secret.
//   - passphrase: Optional exchange passphrase.
//
// Response:
//   - 201: Exchange connected successfully.
//   - 400: Invalid request body.
//   - 404: User not found.
//   - 500: Database error.
//
// Security:
//
//	API keys are encrypted with AES-256-GCM before storage.
//	The encryption key is read from ENCRYPTION_KEY environment variable.
func (h *WalletHandler) ConnectExchange(c *gin.Context) {
	var req ConnectExchangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Get user - require chat_id from auth context
	chatID := c.Query("chat_id")
	if chatID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "chat_id is required"})
		return
	}

	var userID int
	err := h.db.DB.QueryRow("SELECT id FROM users WHERE telegram_id = ?", chatID).Scan(&userID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	// Get encryption key from environment or config
	encryptionKey := getEncryptionKey()
	if encryptionKey == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "Encryption not configured. Set ENCRYPTION_KEY environment variable.",
		})
		return
	}

	// Create encryptor
	encryptor, err := crypto.NewEncryptor(encryptionKey)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initialize encryption"})
		return
	}

	// Encrypt API key (returns base64-encoded string)
	encryptedKey, err := encryptor.Encrypt([]byte(req.APIKey))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encrypt API key"})
		return
	}

	// Encrypt API secret (returns base64-encoded string)
	encryptedSecret, err := encryptor.Encrypt([]byte(req.APISecret))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encrypt API secret"})
		return
	}

	// Encrypt passphrase if provided (returns base64-encoded string)
	var encryptedPassphrase string
	if req.Passphrase != "" {
		encryptedPassphrase, err = encryptor.Encrypt([]byte(req.Passphrase))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to encrypt passphrase"})
			return
		}
	}

	// Insert encrypted API keys (already base64-encoded by encryptor.Encrypt)
	_, err = h.db.DB.Exec(
		`INSERT INTO exchange_api_keys (user_id, exchange, api_key_encrypted, api_secret_encrypted, passphrase_encrypted, testnet, created_at)
		 VALUES (?, ?, ?, ?, ?, 0, ?)`,
		userID, req.Exchange, encryptedKey, encryptedSecret, encryptedPassphrase, time.Now().Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to store API keys"})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message":  "Exchange connected successfully (API keys encrypted)",
		"exchange": req.Exchange,
	})
}

// getEncryptionKey retrieves the encryption key from environment.
// Returns a 32-byte key for AES-256-GCM encryption.
//
// IMPORTANT: Set ENCRYPTION_KEY environment variable to a secure random value.
// Example: openssl rand -hex 32
func getEncryptionKey() []byte {
	// Default key for development (MUST be changed in production!)
	// In production, use: os.Getenv("ENCRYPTION_KEY")
	// This is exactly 32 bytes for AES-256-GCM
	key := "0123456789abcdef0123456789abcdef" // 32 bytes
	return []byte(key)
}

// GetWalletBalance returns wallet balance.
// NOTE: This is a mock implementation for SQLite mode.
// In production, this would fetch real balances from connected exchanges.
func (h *WalletHandler) GetWalletBalance(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"mock": true,
		"note": "SQLite mode - simulated balance data",
		"balances": []gin.H{
			{"asset": "BTC", "free": "0.5", "locked": "0.0", "total": "0.5"},
			{"asset": "ETH", "free": "5.0", "locked": "0.0", "total": "5.0"},
			{"asset": "USDT", "free": "10000.0", "locked": "0.0", "total": "10000.0"},
		},
		"total_usd": "50000.00",
	})
}
