package sqlite

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/database"
	"github.com/stretchr/testify/assert"
)

// setupTestDatabase creates a test SQLite database in memory
func setupTestDatabase(t *testing.T) *database.SQLiteDB {
	db, err := database.NewSQLiteConnectionWithExtension(":memory:", "")
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Create tables
	_, err = db.DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			telegram_id TEXT UNIQUE,
			email TEXT UNIQUE,
			username TEXT,
			password_hash TEXT,
			risk_level TEXT DEFAULT 'medium',
			mode TEXT DEFAULT 'live',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create users table: %v", err)
	}

	_, err = db.DB.Exec(`
		CREATE TABLE IF NOT EXISTS wallets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			name TEXT NOT NULL,
			exchange TEXT NOT NULL,
			api_key_encrypted TEXT,
			api_secret_encrypted TEXT,
			is_active INTEGER DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create wallets table: %v", err)
	}

	_, err = db.DB.Exec(`
		CREATE TABLE IF NOT EXISTS exchange_api_keys (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			exchange TEXT NOT NULL,
			api_key_encrypted TEXT,
			api_secret_encrypted TEXT,
			testnet INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			UNIQUE(user_id, exchange, testnet)
		)
	`)
	if err != nil {
		t.Fatalf("Failed to create exchange_api_keys table: %v", err)
	}

	return db
}

func TestNewWalletHandler(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewWalletHandler(db)
	assert.NotNil(t, handler)
	assert.Equal(t, db, handler.db)
}

func TestWalletHandler_GetWallets(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewWalletHandler(db)

	// Create test user
	_, err := db.DB.Exec("INSERT INTO users (telegram_id, email, username) VALUES (?, ?, ?)",
		"test_chat_123", "test@example.com", "testuser")
	assert.NoError(t, err)

	// Create test wallet
	_, err = db.DB.Exec("INSERT INTO wallets (user_id, name, exchange) VALUES (?, ?, ?)",
		1, "my-wallet", "binance")
	assert.NoError(t, err)

	// Test GetWallets
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/wallets?chat_id=test_chat_123", nil)

	handler.GetWallets(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "wallets")
}

func TestWalletHandler_GetWallets_RequiresChatID(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewWalletHandler(db)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/wallets", nil)

	handler.GetWallets(c)

	// Should return empty list, not error (has fallback)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestWalletHandler_AddWallet(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewWalletHandler(db)

	// Create test user
	_, err := db.DB.Exec("INSERT INTO users (telegram_id, email) VALUES (?, ?)",
		"test_chat_456", "test2@example.com")
	assert.NoError(t, err)

	// Test AddWallet
	reqBody := map[string]string{
		"name":     "test-wallet",
		"exchange": "bybit",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/wallets?chat_id=test_chat_456", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.AddWallet(c)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "Wallet added successfully")
}

func TestWalletHandler_RemoveWallet_WithAuthorization(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewWalletHandler(db)

	// Create test user
	_, err := db.DB.Exec("INSERT INTO users (telegram_id) VALUES (?)", "test_chat_789")
	assert.NoError(t, err)

	// Create test wallet for user 1
	_, err = db.DB.Exec("INSERT INTO wallets (user_id, name, exchange) VALUES (?, ?, ?)",
		1, "wallet-to-delete", "binance")
	assert.NoError(t, err)

	// Test RemoveWallet with correct chat_id
	reqBody := map[string]string{"name": "wallet-to-delete"}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("DELETE", "/wallets?chat_id=test_chat_789", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.RemoveWallet(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Wallet removed successfully")
}

func TestWalletHandler_RemoveWallet_RequiresChatID(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewWalletHandler(db)

	reqBody := map[string]string{"name": "some-wallet"}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("DELETE", "/wallets", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.RemoveWallet(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "chat_id is required")
}

func TestWalletHandler_ConnectExchange_EncryptsAPIKeys(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewWalletHandler(db)

	// Set encryption key for this test
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-32-bytes!!")

	// Create test user
	_, err := db.DB.Exec("INSERT INTO users (telegram_id) VALUES (?)", "test_chat_exchange")
	assert.NoError(t, err)

	// Test ConnectExchange
	reqBody := map[string]string{
		"exchange":   "binance",
		"api_key":    "test-api-key-123",
		"api_secret": "test-api-secret-456",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/wallets/connect?chat_id=test_chat_exchange", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.ConnectExchange(c)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "Exchange connected successfully")
	assert.Contains(t, w.Body.String(), "encrypted")

	// Verify keys are encrypted in database
	var encryptedKey, encryptedSecret string
	err = db.DB.QueryRow("SELECT api_key_encrypted, api_secret_encrypted FROM exchange_api_keys WHERE user_id = 1").
		Scan(&encryptedKey, &encryptedSecret)
	assert.NoError(t, err)
	assert.NotEqual(t, "test-api-key-123", encryptedKey)
	assert.NotEqual(t, "test-api-secret-456", encryptedSecret)
	// Should be base64 encoded
	assert.Greater(t, len(encryptedKey), 32) // Encrypted + encoded should be longer
}

func TestWalletHandler_ConnectExchange_RequiresChatID(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewWalletHandler(db)

	reqBody := map[string]string{
		"exchange":   "binance",
		"api_key":    "test-key",
		"api_secret": "test-secret",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/wallets/connect", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.ConnectExchange(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "chat_id is required")
}
