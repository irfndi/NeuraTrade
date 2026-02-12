package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
)

// TestTelegramInternalHandler_GetNotificationPreferences_Success tests success case
func TestTelegramInternalHandler_GetNotificationPreferences_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/internal/telegram/users/user-123/preferences", nil)
	c.Params = gin.Params{{Key: "userId", Value: "user-123"}}

	// 1. Check disabled count
	mockDB.ExpectQuery("SELECT COUNT").
		WithArgs("user-123").
		WillReturnRows(pgxmock.NewRows([]string{"count"}).AddRow(0))

	// 2. Fetch active alert
	mockDB.ExpectQuery("SELECT conditions").
		WithArgs("user-123").
		WillReturnRows(pgxmock.NewRows([]string{"conditions"}).AddRow([]byte(`{"profit_threshold": 0.5}`)))

	handler.GetNotificationPreferences(c)

	assert.Equal(t, http.StatusOK, w.Code)
	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, true, response["enabled"])
	assert.Equal(t, 0.5, response["profit_threshold"])
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestTelegramInternalHandler_SetNotificationPreferences_Success_Enable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	req := map[string]bool{"enabled": true}
	jsonBytes, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/internal/telegram/users/user-123/preferences", bytes.NewBuffer(jsonBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "userId", Value: "user-123"}}

	mockDB.ExpectBegin()
	mockDB.ExpectExec("DELETE FROM user_alerts").
		WithArgs("user-123").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mockDB.ExpectCommit()

	handler.SetNotificationPreferences(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestTelegramInternalHandler_SetNotificationPreferences_Success_Disable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	req := map[string]bool{"enabled": false}
	jsonBytes, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/internal/telegram/users/user-123/preferences", bytes.NewBuffer(jsonBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "userId", Value: "user-123"}}

	mockDB.ExpectBegin()
	mockDB.ExpectExec("DELETE FROM user_alerts").
		WithArgs("user-123").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))
	mockDB.ExpectExec("INSERT INTO user_alerts").
		WithArgs(pgxmock.AnyArg(), "user-123", pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	mockDB.ExpectCommit()

	handler.SetNotificationPreferences(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// Keep existing basic tests
func TestNewTelegramInternalHandler(t *testing.T) {
	mockDB, _ := pgxmock.NewPool()
	defer mockDB.Close()
	userHandler := &UserHandler{}
	handler := NewTelegramInternalHandler(mockDB, userHandler)

	assert.NotNil(t, handler)
	assert.Equal(t, mockDB, handler.db)
}

func TestTelegramInternalHandler_PathParameters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		paramKey   string
		paramValue string
		expected   string
	}{
		{
			name:       "chat id parameter",
			paramKey:   "id",
			paramValue: "123456789",
			expected:   "123456789",
		},
		// ... existing tests ...
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{{Key: tt.paramKey, Value: tt.paramValue}}
			assert.Equal(t, tt.expected, c.Param(tt.paramKey))
		})
	}
}

// ... include other existing tests if valuable ...
func TestTelegramInternalHandler_ErrorResponses(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tests := []struct {
		name           string
		expectedStatus int
		errorMessage   string
	}{
		{
			name:           "user not found",
			expectedStatus: http.StatusNotFound,
			errorMessage:   "User not found",
		},
		{
			name:           "bad request",
			expectedStatus: http.StatusBadRequest,
			errorMessage:   "Chat ID required",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.JSON(tt.expectedStatus, gin.H{"error": tt.errorMessage})
			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

// TestTelegramInternalHandler_GetUserByChatID_Success tests successful user retrieval
func TestTelegramInternalHandler_GetUserByChatID_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	// Create UserHandler with querier for testing
	userHandler := NewUserHandlerWithQuerier(mockDB, nil, nil)
	handler := NewTelegramInternalHandler(mockDB, userHandler)

	chatID := "123456789"
	now := time.Now()
	userID := "user-uuid-123"

	// Mock the database query for GetUserByTelegramChatID
	mockDB.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE telegram_chat_id`).
		WithArgs(chatID).
		WillReturnRows(pgxmock.NewRows([]string{"id", "email", "password_hash", "telegram_chat_id", "subscription_tier", "created_at", "updated_at"}).
			AddRow(userID, "test@example.com", "hashed_pass", &chatID, "premium", now, now))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/telegram/internal/users/"+chatID, nil)
	c.Params = gin.Params{{Key: "id", Value: chatID}}

	handler.GetUserByChatID(c)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)

	userData := response["user"].(map[string]interface{})
	assert.Equal(t, userID, userData["id"])
	assert.Equal(t, "premium", userData["subscription_tier"])
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// TestTelegramInternalHandler_GetUserByChatID_NotFound tests user not found scenario
func TestTelegramInternalHandler_GetUserByChatID_NotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	userHandler := NewUserHandlerWithQuerier(mockDB, nil, nil)
	handler := NewTelegramInternalHandler(mockDB, userHandler)

	chatID := "nonexistent123"

	// Mock the database query returning no rows
	mockDB.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE telegram_chat_id`).
		WithArgs(chatID).
		WillReturnError(pgx.ErrNoRows)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/telegram/internal/users/"+chatID, nil)
	c.Params = gin.Params{{Key: "id", Value: chatID}}

	handler.GetUserByChatID(c)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "User not found", response["error"])
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// TestTelegramInternalHandler_GetUserByChatID_EmptyID tests empty chat ID validation
func TestTelegramInternalHandler_GetUserByChatID_EmptyID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, _ := pgxmock.NewPool()
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/telegram/internal/users/", nil)
	c.Params = gin.Params{{Key: "id", Value: ""}}

	handler.GetUserByChatID(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Chat ID required", response["error"])
}

// TestTelegramInternalHandler_GetUserByChatID_DatabaseError tests database error handling
func TestTelegramInternalHandler_GetUserByChatID_DatabaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	userHandler := NewUserHandlerWithQuerier(mockDB, nil, nil)
	handler := NewTelegramInternalHandler(mockDB, userHandler)

	chatID := "123456789"

	// Mock the database query returning a generic error
	mockDB.ExpectQuery(`SELECT id, email, password_hash, telegram_chat_id, subscription_tier, created_at, updated_at FROM users WHERE telegram_chat_id`).
		WithArgs(chatID).
		WillReturnError(fmt.Errorf("database connection failed"))

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/telegram/internal/users/"+chatID, nil)
	c.Params = gin.Params{{Key: "id", Value: chatID}}

	handler.GetUserByChatID(c)

	// Returns 404 for any error (as per current implementation)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// TestTelegramInternalHandler_GetNotificationPreferences_DatabaseError tests database error handling
func TestTelegramInternalHandler_GetNotificationPreferences_DatabaseError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/telegram/internal/notifications/user-123", nil)
	c.Params = gin.Params{{Key: "userId", Value: "user-123"}}

	// Mock count query with error
	mockDB.ExpectQuery("SELECT COUNT").
		WithArgs("user-123").
		WillReturnError(fmt.Errorf("database error"))

	handler.GetNotificationPreferences(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Failed to fetch preferences", response["error"])
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// TestTelegramInternalHandler_GetNotificationPreferences_EmptyUserID tests empty user ID validation
func TestTelegramInternalHandler_GetNotificationPreferences_EmptyUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, _ := pgxmock.NewPool()
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/telegram/internal/notifications/", nil)
	c.Params = gin.Params{{Key: "userId", Value: ""}}

	handler.GetNotificationPreferences(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "User ID required", response["error"])
}

// TestTelegramInternalHandler_SetNotificationPreferences_InvalidJSON tests invalid JSON handling
func TestTelegramInternalHandler_SetNotificationPreferences_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, _ := pgxmock.NewPool()
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/telegram/internal/notifications/user-123", bytes.NewBufferString("invalid json"))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "userId", Value: "user-123"}}

	handler.SetNotificationPreferences(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Invalid request body", response["error"])
}

// TestTelegramInternalHandler_SetNotificationPreferences_EmptyUserID tests empty user ID validation
func TestTelegramInternalHandler_SetNotificationPreferences_EmptyUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, _ := pgxmock.NewPool()
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	req := map[string]bool{"enabled": true}
	jsonBytes, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/telegram/internal/notifications/", bytes.NewBuffer(jsonBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "userId", Value: ""}}

	handler.SetNotificationPreferences(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "User ID required", response["error"])
}

// TestTelegramInternalHandler_SetNotificationPreferences_TransactionBeginError tests transaction begin error
func TestTelegramInternalHandler_SetNotificationPreferences_TransactionBeginError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	req := map[string]bool{"enabled": true}
	jsonBytes, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/telegram/internal/notifications/user-123", bytes.NewBuffer(jsonBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "userId", Value: "user-123"}}

	// Mock begin transaction failure
	mockDB.ExpectBegin().WillReturnError(fmt.Errorf("failed to begin transaction"))

	handler.SetNotificationPreferences(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Failed to begin transaction", response["error"])
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

// TestTelegramInternalHandler_SetNotificationPreferences_DeleteError tests delete operation error
func TestTelegramInternalHandler_SetNotificationPreferences_DeleteError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewTelegramInternalHandler(mockDB, nil)

	req := map[string]bool{"enabled": true}
	jsonBytes, _ := json.Marshal(req)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/telegram/internal/notifications/user-123", bytes.NewBuffer(jsonBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "userId", Value: "user-123"}}

	// Mock begin transaction success
	mockDB.ExpectBegin()
	// Mock delete operation failure
	mockDB.ExpectExec("DELETE FROM user_alerts").
		WithArgs("user-123").
		WillReturnError(fmt.Errorf("delete failed"))
	mockDB.ExpectRollback()

	handler.SetNotificationPreferences(c)

	assert.Equal(t, http.StatusInternalServerError, w.Code)

	var response map[string]interface{}
	err = json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "Failed to update preferences", response["error"])
	assert.NoError(t, mockDB.ExpectationsWereMet())
}
