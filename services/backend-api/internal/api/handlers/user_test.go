package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irfandi/celebrum-ai-go/internal/database"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/irfandi/celebrum-ai-go/internal/testutil"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// MockTokenGenerator for testing
type MockTokenGenerator struct {
	Token string
	Error error
}

func (m *MockTokenGenerator) GenerateToken(userID, email string, duration time.Duration) (string, error) {
	if m.Error != nil {
		return "", m.Error
	}
	if m.Token != "" {
		return m.Token, nil
	}
	return "mock_token_" + userID, nil
}

// setupTestRedis creates a Redis client for testing
func setupTestRedis(t *testing.T) *redis.Client {
	// Use Redis database 15 for testing to avoid conflicts
	options := testutil.GetTestRedisOptions()
	options.DB = 15 // Override to use test database
	client := redis.NewClient(options)

	// Test the connection
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis not available for testing: %v", err)
	}

	// Clear the test database
	client.FlushDB(ctx)

	return client
}

func TestNewUserHandler(t *testing.T) {
	mockDB := &database.PostgresDB{}
	tokenGen := &MockTokenGenerator{}
	handler := NewUserHandler(mockDB, nil, tokenGen)

	assert.NotNil(t, handler)
	assert.Equal(t, mockDB, handler.db)
	assert.Equal(t, tokenGen, handler.tokenGen)
}

func TestUserHandler_RegisterUser(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer([]byte("invalid json")))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("missing email", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user := map[string]interface{}{
			"password": TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("missing password", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user := map[string]interface{}{
			"email": "test@example.com",
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("password too short", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user := map[string]interface{}{
			"email":    "test@example.com",
			"password": "short",
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("invalid email format", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user := map[string]interface{}{
			"email":    "invalid-email",
			"password": TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	// Success scenario with database mocking
	t.Run("successful registration", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Mock userExists check - user doesn't exist
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("test@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

		// Mock user insertion
		mock.ExpectExec("INSERT INTO users").
			WithArgs(pgxmock.AnyArg(), "test@example.com", pgxmock.AnyArg(), (*string)(nil), "free").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := RegisterRequest{
			Email:    "test@example.com",
			Password: TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "user")

		userData := response["user"].(map[string]interface{})
		assert.Equal(t, "test@example.com", userData["email"])
		assert.Equal(t, "free", userData["subscription_tier"])
		assert.NotEmpty(t, userData["id"])

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// User already exists scenario
	t.Run("user already exists", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Mock userExists check - user exists
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("existing@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := RegisterRequest{
			Email:    "existing@example.com",
			Password: TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusConflict, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "already exists")

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Database error during userExists check
	t.Run("database error during user existence check", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Mock userExists check with error
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("test@example.com").
			WillReturnError(fmt.Errorf("database connection error"))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := RegisterRequest{
			Email:    "test@example.com",
			Password: TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "Failed to check user existence")

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Database error during user insertion
	t.Run("database error during user insertion", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Mock userExists check - user doesn't exist
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("test@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

		// Mock user insertion with error
		mock.ExpectExec("INSERT INTO users").
			WithArgs(pgxmock.AnyArg(), "test@example.com", pgxmock.AnyArg(), (*string)(nil), "free").
			WillReturnError(fmt.Errorf("insertion failed"))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := RegisterRequest{
			Email:    "test@example.com",
			Password: TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "Failed to create user")

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Registration with telegram chat ID
	t.Run("successful registration with telegram chat ID", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		telegramChatID := "123456789"

		// Mock userExists check - user doesn't exist
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("test@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

		// Mock user insertion
		mock.ExpectExec("INSERT INTO users").
			WithArgs(pgxmock.AnyArg(), "test@example.com", pgxmock.AnyArg(), &telegramChatID, "free").
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := RegisterRequest{
			Email:          "test@example.com",
			Password:       TestValidPassword,
			TelegramChatID: &telegramChatID,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "user")

		userData := response["user"].(map[string]interface{})
		assert.Equal(t, "test@example.com", userData["email"])
		assert.Equal(t, telegramChatID, userData["telegram_chat_id"])

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// UPSERT: Telegram user exists with NULL chat ID - should update
	t.Run("telegram user exists with NULL chat ID - updates chat ID", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		newChatID := "123456789"
		existingUserID := uuid.New().String()
		existingCreatedAt := time.Now().Add(-24 * time.Hour)

		// Mock userExists check - user exists
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("telegram_12345@celebrum.ai").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

		// Mock getUserByEmail - existing user with NULL telegram_chat_id
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE email = \$1`).
			WithArgs("telegram_12345@celebrum.ai").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow(existingUserID, "telegram_12345@celebrum.ai", "", nil, "free", existingCreatedAt, existingCreatedAt))

		// Mock UPDATE for telegram_chat_id
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$1, updated_at = NOW\(\) WHERE email = \$2`).
			WithArgs(&newChatID, "telegram_12345@celebrum.ai").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := RegisterRequest{
			Email:          "telegram_12345@celebrum.ai",
			Password:       TestValidPassword,
			TelegramChatID: &newChatID,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "user")
		assert.True(t, response["already_exists"].(bool))

		userData := response["user"].(map[string]interface{})
		assert.Equal(t, existingUserID, userData["id"])
		assert.Equal(t, newChatID, userData["telegram_chat_id"])

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// UPSERT: Telegram user exists with different chat ID - should update
	t.Run("telegram user exists with different chat ID - updates chat ID", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		oldChatID := "111111111"
		newChatID := "222222222"
		existingUserID := uuid.New().String()
		existingCreatedAt := time.Now().Add(-24 * time.Hour)

		// Mock userExists check - user exists
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("telegram_12345@celebrum.ai").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

		// Mock getUserByEmail - existing user with different telegram_chat_id
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE email = \$1`).
			WithArgs("telegram_12345@celebrum.ai").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow(existingUserID, "telegram_12345@celebrum.ai", "", &oldChatID, "free", existingCreatedAt, existingCreatedAt))

		// Mock UPDATE for telegram_chat_id
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$1, updated_at = NOW\(\) WHERE email = \$2`).
			WithArgs(&newChatID, "telegram_12345@celebrum.ai").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := RegisterRequest{
			Email:          "telegram_12345@celebrum.ai",
			Password:       TestValidPassword,
			TelegramChatID: &newChatID,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "user")
		assert.True(t, response["already_exists"].(bool))

		userData := response["user"].(map[string]interface{})
		assert.Equal(t, newChatID, userData["telegram_chat_id"])

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// UPSERT: Telegram user exists with same chat ID - no update needed
	t.Run("telegram user exists with same chat ID - returns existing user", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		chatID := "123456789"
		existingUserID := uuid.New().String()
		existingCreatedAt := time.Now().Add(-24 * time.Hour)

		// Mock userExists check - user exists
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("telegram_12345@celebrum.ai").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

		// Mock getUserByEmail - existing user with same telegram_chat_id
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE email = \$1`).
			WithArgs("telegram_12345@celebrum.ai").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow(existingUserID, "telegram_12345@celebrum.ai", "", &chatID, "free", existingCreatedAt, existingCreatedAt))

		// No UPDATE expected since chat ID is the same

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := RegisterRequest{
			Email:          "telegram_12345@celebrum.ai",
			Password:       TestValidPassword,
			TelegramChatID: &chatID,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "user")
		assert.True(t, response["already_exists"].(bool))

		userData := response["user"].(map[string]interface{})
		assert.Equal(t, existingUserID, userData["id"])
		assert.Equal(t, chatID, userData["telegram_chat_id"])

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUserHandler_LoginUser(t *testing.T) {
	t.Run("invalid JSON body", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer([]byte("invalid json")))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("missing email", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user := map[string]interface{}{
			"password": TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("missing password", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user := map[string]interface{}{
			"email": "test@example.com",
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	// Database error during getUserByEmail check
	t.Run("database error during user existence check", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Mock getUserByEmail with error
		mock.ExpectQuery(`
			SELECT id, email, password_hash, telegram_chat_id, 
			       subscription_tier, created_at, updated_at
			FROM users WHERE email = \$1
		`).
			WithArgs("test@example.com").
			WillReturnError(fmt.Errorf("database connection error"))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := LoginRequest{
			Email:    "test@example.com",
			Password: TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "Invalid email or password")

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty email", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user := map[string]interface{}{
			"email":    "",
			"password": TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("empty password", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user := map[string]interface{}{
			"email":    "test@example.com",
			"password": "",
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	// Successful login scenario
	t.Run("successful login", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Hash the password for comparison
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(TestValidPassword), bcrypt.DefaultCost)
		userID := uuid.New()

		// Mock getUserByEmail - user exists
		mock.ExpectQuery(`
			SELECT id, email, password_hash, telegram_chat_id, 
			       subscription_tier, created_at, updated_at
			FROM users WHERE email = \$1
		`).
			WithArgs("test@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow(userID.String(), "test@example.com", string(hashedPassword), nil, "free", time.Now(), time.Now()))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := LoginRequest{
			Email:    "test@example.com",
			Password: TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var response LoginResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Equal(t, "test@example.com", response.User.Email)
		assert.Equal(t, userID.String(), response.User.ID)
		assert.NotEmpty(t, response.Token)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// User not found scenario
	t.Run("user not found", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Mock getUserByEmail - user not found
		mock.ExpectQuery(`
			SELECT id, email, password_hash, telegram_chat_id, 
			       subscription_tier, created_at, updated_at
			FROM users WHERE email = \$1
		`).
			WithArgs("nonexistent@example.com").
			WillReturnError(pgx.ErrNoRows)

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := LoginRequest{
			Email:    "nonexistent@example.com",
			Password: TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "Invalid email or password")

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Invalid password scenario
	t.Run("invalid password", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Hash a different password
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(TestDifferentPassword), bcrypt.DefaultCost)
		userID := uuid.New()

		// Mock getUserByEmail - user exists but password doesn't match
		mock.ExpectQuery(`
			SELECT id, email, password_hash, telegram_chat_id, 
			       subscription_tier, created_at, updated_at
			FROM users WHERE email = \$1
		`).
			WithArgs("test@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow(userID.String(), "test@example.com", string(hashedPassword), nil, "free", time.Now(), time.Now()))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := LoginRequest{
			Email:    "test@example.com",
			Password: TestWrongPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "Invalid email or password")

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Database error during getUserByEmail
	t.Run("database error during user lookup", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Mock getUserByEmail - database error
		mock.ExpectQuery(`
			SELECT id, email, password_hash, telegram_chat_id, 
			       subscription_tier, created_at, updated_at
			FROM users WHERE email = \$1
		`).
			WithArgs("test@example.com").
			WillReturnError(fmt.Errorf("database connection error"))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := LoginRequest{
			Email:    "test@example.com",
			Password: TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "Invalid email or password")

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Login with telegram chat ID
	t.Run("successful login with telegram chat ID", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		// Hash the password for comparison
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(TestValidPassword), bcrypt.DefaultCost)
		userID := uuid.New()
		telegramChatID := "123456789"

		// Mock getUserByEmail - user exists with telegram chat ID
		mock.ExpectQuery(`
			SELECT id, email, password_hash, telegram_chat_id, 
			       subscription_tier, created_at, updated_at
			FROM users WHERE email = \$1
		`).
			WithArgs("test@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow(userID.String(), "test@example.com", string(hashedPassword), &telegramChatID, "premium", time.Now(), time.Now()))

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		user := LoginRequest{
			Email:    "test@example.com",
			Password: TestValidPassword,
		}

		jsonData, _ := json.Marshal(user)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusOK, w.Code)

		var response LoginResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.NotNil(t, response.User)
		assert.Equal(t, "test@example.com", response.User.Email)
		assert.NotNil(t, response.User.TelegramChatID)
		assert.Equal(t, telegramChatID, *response.User.TelegramChatID)
		assert.Equal(t, "premium", response.User.SubscriptionTier)
		assert.NotEmpty(t, response.Token)

		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUserHandler_GetUserProfile(t *testing.T) {
	t.Run("missing user ID in context", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/users/profile", nil)
		// Do not set user_id in context

		handler.GetUserProfile(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "User ID required")
	})

	t.Run("user ID in context", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/users/profile", nil)
		c.Set("user_id", "test-user-id")

		handler.GetUserProfile(c)

		// Will return 404 since we don't have a real database
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("ignores X-User-ID header", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("GET", "/users/profile", nil)
		c.Request.Header.Set("X-User-ID", "test-user-id")
		// user_id in context is missing

		handler.GetUserProfile(c)

		// Should be unauthorized because it ignores header
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestUserHandler_UpdateUserProfile(t *testing.T) {
	t.Run("missing user ID", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		update := map[string]interface{}{
			"telegram_chat_id": "123456789",
		}

		jsonData, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/users/profile", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "User ID required")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/users/profile", bytes.NewBuffer([]byte("invalid json")))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "test-user-id")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("ignores query parameter", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		update := map[string]interface{}{
			"telegram_chat_id": "123456789",
		}

		jsonData, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/users/profile?user_id=test-user-id", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Request.URL.RawQuery = "user_id=test-user-id"
		// No user_id in context

		handler.UpdateUserProfile(c)

		// Should return Unauthorized because query param is ignored
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("empty telegram chat ID", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		emptyChatID := ""
		update := UpdateProfileRequest{
			TelegramChatID: &emptyChatID,
		}

		jsonData, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("PUT", "/users/profile", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "test-user-id")

		handler.UpdateUserProfile(c)

		// Should return 500 since database is not available
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestRegisterRequest_Struct(t *testing.T) {
	chatID := "123456789"
	req := RegisterRequest{
		Email:          "test@example.com",
		Password:       TestPassword123,
		TelegramChatID: &chatID,
	}

	assert.Equal(t, "test@example.com", req.Email)
	assert.Equal(t, TestPassword123, req.Password)
	assert.NotNil(t, req.TelegramChatID)
	assert.Equal(t, "123456789", *req.TelegramChatID)
}

func TestLoginRequest_Struct(t *testing.T) {
	req := LoginRequest{
		Email:    "test@example.com",
		Password: TestPassword123,
	}

	assert.Equal(t, "test@example.com", req.Email)
	assert.Equal(t, TestPassword123, req.Password)
}

func TestUserResponse_Struct(t *testing.T) {
	chatID := "123456789"
	now := time.Now()
	resp := UserResponse{
		ID:               "user-123",
		Email:            "test@example.com",
		TelegramChatID:   &chatID,
		SubscriptionTier: "free",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	assert.Equal(t, "user-123", resp.ID)
	assert.Equal(t, "test@example.com", resp.Email)
	assert.NotNil(t, resp.TelegramChatID)
	assert.Equal(t, "123456789", *resp.TelegramChatID)
	assert.Equal(t, "free", resp.SubscriptionTier)
	assert.Equal(t, now, resp.CreatedAt)
	assert.Equal(t, now, resp.UpdatedAt)
}

func TestLoginResponse_Struct(t *testing.T) {
	chatID := "123456789"
	now := time.Now()
	userResp := UserResponse{
		ID:               "user-123",
		Email:            "test@example.com",
		TelegramChatID:   &chatID,
		SubscriptionTier: "free",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	resp := LoginResponse{
		User:  userResp,
		Token: "token_user-123_1234567890",
	}

	assert.Equal(t, userResp, resp.User)
	assert.Equal(t, "token_user-123_1234567890", resp.Token)
}

func TestUpdateProfileRequest_Struct(t *testing.T) {
	chatID := "987654321"
	req := UpdateProfileRequest{
		TelegramChatID: &chatID,
	}

	assert.NotNil(t, req.TelegramChatID)
	assert.Equal(t, "987654321", *req.TelegramChatID)
}

func TestUserHandler_RegisterUser_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB := &database.PostgresDB{}
	handler := NewUserHandler(mockDB, nil, &MockTokenGenerator{})

	// Create request with invalid JSON
	invalidJSON := `{"email": "test@example.com", "password":}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/register", bytes.NewBufferString(invalidJSON))
	c.Request.Header.Set("Content-Type", "application/json")

	// Execute
	handler.RegisterUser(c)

	// Assert
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "error")
}

func TestUserHandler_RegisterUser_MissingEmail(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB := &database.PostgresDB{}
	handler := NewUserHandler(mockDB, nil, &MockTokenGenerator{})

	// Create request without email
	reqBody := RegisterRequest{
		Password: TestPassword123,
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/register", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	// Execute
	handler.RegisterUser(c)

	// Assert
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "error")
}

func TestUserHandler_RegisterUser_WithMocks(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("successful registration", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock userExists query
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).WithArgs("test@example.com").WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

		// Mock user insertion
		mock.ExpectExec(`INSERT INTO users \(id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at\)`).WithArgs(pgxmock.AnyArg(), "test@example.com", pgxmock.AnyArg(), (*string)(nil), "free").WillReturnResult(pgxmock.NewResult("INSERT", 1))

		reqBody := RegisterRequest{
			Email:    "test@example.com",
			Password: TestPassword123,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/register", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("user already exists", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock userExists query returning true
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).WithArgs("existing@example.com").WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

		reqBody := RegisterRequest{
			Email:    "existing@example.com",
			Password: TestPassword123,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/register", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusConflict, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database error on user check", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock userExists query with error
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).WithArgs("test@example.com").WillReturnError(assert.AnError)

		reqBody := RegisterRequest{
			Email:    "test@example.com",
			Password: TestPassword123,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/register", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database error on user insertion", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock userExists query
		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).WithArgs("test@example.com").WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

		// Mock user insertion with error
		mock.ExpectExec(`INSERT INTO users \(id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at\)`).WithArgs(pgxmock.AnyArg(), "test@example.com", pgxmock.AnyArg(), (*string)(nil), "free").WillReturnError(assert.AnError)

		reqBody := RegisterRequest{
			Email:    "test@example.com",
			Password: TestPassword123,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/register", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.RegisterUser(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUserHandler_LoginUser_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB := &database.PostgresDB{}
	handler := NewUserHandler(mockDB, nil, &MockTokenGenerator{})

	// Create request with invalid JSON
	invalidJSON := `{"email": "test@example.com", "password":}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/login", bytes.NewBufferString(invalidJSON))
	c.Request.Header.Set("Content-Type", "application/json")

	// Execute
	handler.LoginUser(c)

	// Assert
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "error")
}

func TestUserHandler_LoginUser_WithMocks(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("successful login", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock getUserByEmail query
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(TestPassword123), bcrypt.DefaultCost)
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE email = \$1`).WithArgs("test@example.com").WillReturnRows(
			pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow("user-123", "test@example.com", string(hashedPassword), nil, "free", time.Now(), time.Now()))

		reqBody := LoginRequest{
			Email:    "test@example.com",
			Password: TestPassword123,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("user not found", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock getUserByEmail query with no rows
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE email = \$1`).WithArgs("nonexistent@example.com").WillReturnError(pgx.ErrNoRows)

		reqBody := LoginRequest{
			Email:    "nonexistent@example.com",
			Password: TestPassword123,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("invalid password", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock getUserByEmail query with different password
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(TestDifferentPassword), bcrypt.DefaultCost)
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE email = \$1`).WithArgs("test@example.com").WillReturnRows(
			pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow("user-123", "test@example.com", string(hashedPassword), nil, "free", time.Now(), time.Now()))

		reqBody := LoginRequest{
			Email:    "test@example.com",
			Password: TestWrongPassword,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/login", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.LoginUser(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUserHandler_GetUserProfile_MissingUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB := &database.PostgresDB{}
	handler := NewUserHandler(mockDB, nil, &MockTokenGenerator{})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/profile", nil)

	// Execute
	handler.GetUserProfile(c)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "User ID required", response["error"])
}

func TestUserHandler_UpdateUserProfile_MissingUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB := &database.PostgresDB{}
	handler := NewUserHandler(mockDB, nil, &MockTokenGenerator{})

	reqBody := UpdateProfileRequest{
		TelegramChatID: nil,
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PUT", "/profile", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	// Execute
	handler.UpdateUserProfile(c)

	// Assert
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "User ID required", response["error"])
}

func TestUserHandler_UpdateUserProfile_WithMocks(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("successful update", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock update query
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$2, updated_at = \$3 WHERE id = \$1`).WithArgs("user-123", (*string)(nil), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		// Mock getUserByID query for returning updated user
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE id = \$1`).WithArgs("user-123").WillReturnRows(
			pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow("user-123", "test@example.com", "hashedpass", nil, "free", time.Now(), time.Now()))

		reqBody := UpdateProfileRequest{
			TelegramChatID: nil,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile?user_id=user-123", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("update error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock update query with error
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$2, updated_at = \$3 WHERE id = \$1`).WithArgs("user-123", (*string)(nil), pgxmock.AnyArg()).WillReturnError(assert.AnError)

		reqBody := UpdateProfileRequest{
			TelegramChatID: nil,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile?user_id=user-123", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("fetch updated user error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock successful update
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$2, updated_at = \$3 WHERE id = \$1`).WithArgs("user-123", (*string)(nil), pgxmock.AnyArg()).WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		// Mock getUserByID query with error
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE id = \$1`).WithArgs("user-123").WillReturnError(assert.AnError)

		reqBody := UpdateProfileRequest{
			TelegramChatID: nil,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile?user_id=user-123", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestUserHandler_UpdateUserProfile_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockDB := &database.PostgresDB{}
	handler := NewUserHandler(mockDB, nil, &MockTokenGenerator{})

	// Create request with invalid JSON
	invalidJSON := `{"telegram_chat_id":}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PUT", "/profile", bytes.NewBufferString(invalidJSON))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", "user-123")

	// Execute
	handler.UpdateUserProfile(c)

	// Assert
	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response, "error")
}

func TestUserHandler_UpdateUserProfile_Comprehensive(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("successful update with telegram chat ID", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		chatID := "987654321"
		// Mock update query
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$2, updated_at = \$3 WHERE id = \$1`).
			WithArgs("user-123", &chatID, pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		// Mock getUserByID query for returning updated user
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE id = \$1`).
			WithArgs("user-123").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow("user-123", "test@example.com", "hashedpass", &chatID, "free", time.Now(), time.Now()))

		reqBody := UpdateProfileRequest{
			TelegramChatID: &chatID,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "user")
	})

	t.Run("user ID from context", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock update query
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$2, updated_at = \$3 WHERE id = \$1`).
			WithArgs("context-user-123", (*string)(nil), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		// Mock getUserByID query
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE id = \$1`).
			WithArgs("context-user-123").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow("context-user-123", "test@example.com", "hashedpass", nil, "free", time.Now(), time.Now()))

		reqBody := UpdateProfileRequest{
			TelegramChatID: nil,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "context-user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("missing user ID", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		reqBody := UpdateProfileRequest{
			TelegramChatID: nil,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusUnauthorized, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "User ID required")
	})

	t.Run("database not available", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		reqBody := UpdateProfileRequest{
			TelegramChatID: nil,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile?user_id=user-123", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "Database not available")
	})

	t.Run("empty request body", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock update query with nil telegram_chat_id
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$2, updated_at = \$3 WHERE id = \$1`).
			WithArgs("user-123", (*string)(nil), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		// Mock getUserByID query
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE id = \$1`).
			WithArgs("user-123").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow("user-123", "test@example.com", "hashedpass", nil, "free", time.Now(), time.Now()))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile?user_id=user-123", bytes.NewBuffer([]byte("{}")))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("update affects no rows", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock update query that affects 0 rows (user not found)
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$2, updated_at = \$3 WHERE id = \$1`).
			WithArgs("nonexistent-user", (*string)(nil), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		reqBody := UpdateProfileRequest{
			TelegramChatID: nil,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile?user_id=nonexistent-user", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "nonexistent-user")

		handler.UpdateUserProfile(c)

		// Should still proceed to fetch user, which will fail
		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	// Additional database error scenarios
	t.Run("database exec error during update", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock update query to return database error
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$2, updated_at = \$3 WHERE id = \$1`).
			WithArgs("user-123", (*string)(nil), pgxmock.AnyArg()).
			WillReturnError(assert.AnError)

		reqBody := UpdateProfileRequest{
			TelegramChatID: nil,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile?user_id=user-123", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "Failed to update profile")
	})

	t.Run("getUserByID error after successful update", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		chatID := "987654321"
		// Mock successful update query
		mock.ExpectExec(`UPDATE users SET telegram_chat_id = \$2, updated_at = \$3 WHERE id = \$1`).
			WithArgs("user-123", &chatID, pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		// Mock getUserByID query to return error
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE id = \$1`).
			WithArgs("user-123").
			WillReturnError(assert.AnError)

		reqBody := UpdateProfileRequest{
			TelegramChatID: &chatID,
		}
		jsonBody, _ := json.Marshal(reqBody)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile?user_id=user-123", bytes.NewBuffer(jsonBody))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.NoError(t, mock.ExpectationsWereMet())

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "Failed to fetch updated user")
	})

	t.Run("invalid JSON body", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("PUT", "/profile?user_id=user-123", bytes.NewBuffer([]byte("invalid json")))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("user_id", "user-123")

		handler.UpdateUserProfile(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
		assert.Contains(t, response["error"], "invalid character")
	})
}

// Helper function tests with pgxmock

func TestUserHandler_userExists(t *testing.T) {
	t.Run("user exists", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("test@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(1))

		exists, err := handler.userExists(context.Background(), "test@example.com")
		require.NoError(t, err)
		assert.True(t, exists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("user does not exist", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("nonexistent@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

		exists, err := handler.userExists(context.Background(), "nonexistent@example.com")
		require.NoError(t, err)
		assert.False(t, exists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		mock.ExpectQuery(`SELECT COUNT\(\*\) FROM users WHERE email = \$1`).
			WithArgs("error@example.com").
			WillReturnError(assert.AnError)

		exists, err := handler.userExists(context.Background(), "error@example.com")
		assert.Error(t, err)
		assert.False(t, exists)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database not available", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})
		exists, err := handler.userExists(context.Background(), "test@example.com")
		assert.Error(t, err)
		assert.False(t, exists)
		assert.Contains(t, err.Error(), "database not available")
	})
}

func TestUserHandler_getUserByEmail(t *testing.T) {
	t.Run("successful retrieval", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		userID := uuid.New().String()
		now := time.Now()
		chatID := "123456789"

		// Mock the query to return a user
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE email = \$1`).
			WithArgs("test@example.com").
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow(userID, "test@example.com", "hashed_password", &chatID, "free", now, now))

		user, err := handler.getUserByEmail(context.Background(), "test@example.com")
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, "test@example.com", user.Email)
		assert.Equal(t, "hashed_password", user.PasswordHash)
		assert.NotNil(t, user.TelegramChatID)
		assert.Equal(t, chatID, *user.TelegramChatID)
		assert.Equal(t, "free", user.SubscriptionTier)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("user not found", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock the query to return no rows
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE email = \$1`).
			WithArgs("nonexistent@example.com").
			WillReturnError(pgx.ErrNoRows)

		user, err := handler.getUserByEmail(context.Background(), "nonexistent@example.com")
		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Equal(t, pgx.ErrNoRows, err)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		// Mock the query to return a database error
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE email = \$1`).
			WithArgs("test@example.com").
			WillReturnError(assert.AnError)

		user, err := handler.getUserByEmail(context.Background(), "test@example.com")
		assert.Error(t, err)
		assert.Nil(t, user)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database not available", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user, err := handler.getUserByEmail(context.Background(), "test@example.com")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
		assert.Nil(t, user)
	})
}

func TestUserHandler_getUserByID(t *testing.T) {
	t.Run("successful retrieval from database with querier", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{}) // No Redis for this test

		userID := uuid.New().String()
		now := time.Now()
		chatID := "123456789"

		// Mock the query to return a user
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE id = \$1`).
			WithArgs(userID).
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow(userID, "test@example.com", "hashed_password", &chatID, "free", now, now))

		user, err := handler.getUserByID(context.Background(), userID)
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, "test@example.com", user.Email)
		assert.Equal(t, "hashed_password", user.PasswordHash)
		assert.NotNil(t, user.TelegramChatID)
		assert.Equal(t, chatID, *user.TelegramChatID)
		assert.Equal(t, "free", user.SubscriptionTier)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("successful retrieval from Redis cache", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		defer func() {
			if err := redisClient.Close(); err != nil {
				t.Logf("Failed to close Redis client: %v", err)
			}
		}()

		db := &database.PostgresDB{Pool: nil} // Mock database
		handler := &UserHandler{db: db, redis: redisClient}

		userID := uuid.New().String()
		now := time.Now()
		chatID := "123456789"
		cacheKey := fmt.Sprintf("user:id:%s", userID)

		// Create test user and cache it
		testUser := &models.User{
			ID:               userID,
			Email:            "test@example.com",
			PasswordHash:     "hashed_password",
			TelegramChatID:   &chatID,
			SubscriptionTier: "free",
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		userJSON, _ := json.Marshal(testUser)
		redisClient.Set(context.Background(), cacheKey, string(userJSON), 5*time.Minute)

		user, err := handler.getUserByID(context.Background(), userID)
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, "test@example.com", user.Email)
	})

	t.Run("user not found", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		userID := uuid.New().String()

		// Mock the query to return no rows
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE id = \$1`).
			WithArgs(userID).
			WillReturnError(pgx.ErrNoRows)

		user, err := handler.getUserByID(context.Background(), userID)
		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Equal(t, pgx.ErrNoRows, err)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		userID := uuid.New().String()

		// Mock the query to return a database error
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE id = \$1`).
			WithArgs(userID).
			WillReturnError(assert.AnError)

		user, err := handler.getUserByID(context.Background(), userID)
		assert.Error(t, err)
		assert.Nil(t, user)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database not available", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		userID := uuid.New().String()
		user, err := handler.getUserByID(context.Background(), userID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
		assert.Nil(t, user)
	})
}

func TestUserHandler_GetUserByTelegramChatID(t *testing.T) {
	t.Run("successful retrieval with querier", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		chatID := "123456789"
		userID := uuid.New().String()
		now := time.Now()

		// Mock the query to return a user
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE telegram_chat_id = \$1`).
			WithArgs(chatID).
			WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
				AddRow(userID, "test@example.com", "hashed_password", &chatID, "free", now, now))

		user, err := handler.GetUserByTelegramChatID(context.Background(), chatID)
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, "test@example.com", user.Email)
		assert.Equal(t, "hashed_password", user.PasswordHash)
		assert.NotNil(t, user.TelegramChatID)
		assert.Equal(t, chatID, *user.TelegramChatID)
		assert.Equal(t, "free", user.SubscriptionTier)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("successful retrieval from Redis cache", func(t *testing.T) {
		redisClient := setupTestRedis(t)
		defer func() {
			if err := redisClient.Close(); err != nil {
				t.Logf("Failed to close Redis client: %v", err)
			}
		}()

		db := &database.PostgresDB{Pool: nil} // Mock database
		handler := &UserHandler{db: db, redis: redisClient}

		chatID := "123456789"
		userID := uuid.New().String()
		now := time.Now()
		cacheKey := fmt.Sprintf("user:telegram:%s", chatID)

		// Create test user and cache it
		testUser := &models.User{
			ID:               userID,
			Email:            "test@example.com",
			PasswordHash:     "hashed_password",
			TelegramChatID:   &chatID,
			SubscriptionTier: "free",
			CreatedAt:        now,
			UpdatedAt:        now,
		}

		userJSON, _ := json.Marshal(testUser)
		redisClient.Set(context.Background(), cacheKey, string(userJSON), 5*time.Minute)

		user, err := handler.GetUserByTelegramChatID(context.Background(), chatID)
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.Equal(t, userID, user.ID)
		assert.Equal(t, "test@example.com", user.Email)
		assert.Equal(t, chatID, *user.TelegramChatID)
	})

	t.Run("user not found", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		chatID := "123456789"

		// Mock the query to return no rows
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE telegram_chat_id = \$1`).
			WithArgs(chatID).
			WillReturnError(pgx.ErrNoRows)

		user, err := handler.GetUserByTelegramChatID(context.Background(), chatID)
		assert.Error(t, err)
		assert.Nil(t, user)
		assert.Equal(t, pgx.ErrNoRows, err)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		chatID := "123456789"

		// Mock the query to return a database error
		mock.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE telegram_chat_id = \$1`).
			WithArgs(chatID).
			WillReturnError(assert.AnError)

		user, err := handler.GetUserByTelegramChatID(context.Background(), chatID)
		assert.Error(t, err)
		assert.Nil(t, user)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database not available", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		chatID := "123456789"
		user, err := handler.GetUserByTelegramChatID(context.Background(), chatID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
		assert.Nil(t, user)
	})
}

func TestUserHandler_CreateTelegramUser(t *testing.T) {
	t.Run("successful creation", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		chatID := "123456789"

		// Mock the INSERT query
		mock.ExpectExec(`INSERT INTO users \(id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at\) VALUES \(\$1, \$2, \$3, \$4, \$5, \$6, \$7\)`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "", chatID, "free", pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		user, err := handler.CreateTelegramUser(context.Background(), chatID, "testuser")
		assert.NoError(t, err)
		assert.NotNil(t, user)
		assert.NotEmpty(t, user.ID) // ID is generated by the method
		assert.Equal(t, "telegram_testuser@neuratrade.ai", user.Email)
		assert.Equal(t, "", user.PasswordHash)
		assert.NotNil(t, user.TelegramChatID)
		assert.Equal(t, chatID, *user.TelegramChatID)
		assert.Equal(t, "free", user.SubscriptionTier)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty chat ID", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		user, err := handler.CreateTelegramUser(context.Background(), "", "testuser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "telegram chat ID cannot be empty")
		assert.Nil(t, user)
	})

	t.Run("database error", func(t *testing.T) {
		mock, err := pgxmock.NewPool()
		require.NoError(t, err)
		defer mock.Close()

		handler := NewUserHandlerWithQuerier(mock, nil, &MockTokenGenerator{})

		chatID := "123456789"

		// Mock the INSERT query to return an error
		mock.ExpectExec(`INSERT INTO users \(id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at\) VALUES \(\$1, \$2, \$3, \$4, \$5, \$6, \$7\)`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), "", chatID, "free", pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(assert.AnError)

		user, err := handler.CreateTelegramUser(context.Background(), chatID, "testuser")
		assert.Error(t, err)
		assert.Nil(t, user)

		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("database not available", func(t *testing.T) {
		handler := NewUserHandler(nil, nil, &MockTokenGenerator{})

		chatID := "123456789"
		user, err := handler.CreateTelegramUser(context.Background(), chatID, "testuser")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "database not available")
		assert.Nil(t, user)
	})
}
