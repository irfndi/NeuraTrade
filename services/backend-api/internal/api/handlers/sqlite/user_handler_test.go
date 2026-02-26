package sqlite

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestNewUserHandler(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)
	assert.NotNil(t, handler)
	assert.Equal(t, db, handler.db)
}

func TestUserHandler_RegisterUser_Success(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	reqBody := map[string]string{
		"email":            "newuser@example.com",
		"password":         "password123",
		"telegram_chat_id": "test_chat_new",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.RegisterUser(c)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), "id")
	assert.Contains(t, w.Body.String(), "newuser@example.com")
}

func TestUserHandler_RegisterUser_AlreadyExists(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	// Create user first
	reqBody := map[string]string{
		"email":    "existing@example.com",
		"password": "password123",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.RegisterUser(c)

	// Try to create again
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonBody))
	c2.Request.Header.Set("Content-Type", "application/json")
	handler.RegisterUser(c2)

	assert.Equal(t, http.StatusConflict, w2.Code)
	assert.Contains(t, w2.Body.String(), "already exists")
}

func TestUserHandler_RegisterUser_InvalidEmail(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	reqBody := map[string]string{
		"email":    "invalid-email",
		"password": "password123",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.RegisterUser(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUserHandler_RegisterUser_ShortPassword(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	reqBody := map[string]string{
		"email":    "test@example.com",
		"password": "short",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.RegisterUser(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUserHandler_GetUserByChatID_Found(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	// Create user
	_, err := db.DB.Exec(
		"INSERT INTO users (telegram_id, email, password_hash, risk_level) VALUES (?, ?, ?, ?)",
		"test_chat_get", "get@example.com", "hash", "medium",
	)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "test_chat_get"}}
	c.Request = httptest.NewRequest("GET", "/users/test_chat_get", nil)

	handler.GetUserByChatID(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "test_chat_get")
}

func TestUserHandler_GetUserByChatID_NotFound(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: "nonexistent"}}
	c.Request = httptest.NewRequest("GET", "/users/nonexistent", nil)

	handler.GetUserByChatID(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not found")
}

func TestUserHandler_GetUserProfile(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/users/profile", nil)

	handler.GetUserProfile(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "sqlite")
}

func TestUserHandler_LoginUser_Success(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	// Create user with known password
	password := "testpassword123"
	reqBody := map[string]string{
		"email":            "login@example.com",
		"password":         password,
		"telegram_chat_id": "login_chat",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.RegisterUser(c)

	// Now login
	loginBody := map[string]string{
		"email":    "login@example.com",
		"password": password,
	}
	loginJSON, _ := json.Marshal(loginBody)

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest("POST", "/users/login", bytes.NewBuffer(loginJSON))
	c2.Request.Header.Set("Content-Type", "application/json")

	handler.LoginUser(c2)

	assert.Equal(t, http.StatusOK, w2.Code)
	assert.Contains(t, w2.Body.String(), "token")
	assert.Contains(t, w2.Body.String(), "Login successful")
}

func TestUserHandler_LoginUser_UserNotFound(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	reqBody := map[string]string{
		"email":    "nonexistent@example.com",
		"password": "password123",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/users/login", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.LoginUser(c)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not found")
}

func TestUserHandler_LoginUser_InvalidPassword(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	// Create user
	reqBody := map[string]string{
		"email":            "wrongpass@example.com",
		"password":         "correctpassword",
		"telegram_chat_id": "wrongpass_chat",
	}
	jsonBody, _ := json.Marshal(reqBody)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/users/register", bytes.NewBuffer(jsonBody))
	c.Request.Header.Set("Content-Type", "application/json")
	handler.RegisterUser(c)

	// Login with wrong password
	loginBody := map[string]string{
		"email":    "wrongpass@example.com",
		"password": "wrongpassword",
	}
	loginJSON, _ := json.Marshal(loginBody)

	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest("POST", "/users/login", bytes.NewBuffer(loginJSON))
	c2.Request.Header.Set("Content-Type", "application/json")

	handler.LoginUser(c2)

	assert.Equal(t, http.StatusUnauthorized, w2.Code)
	assert.Contains(t, w2.Body.String(), "Invalid credentials")
}

func TestUserHandler_LoginUser_InvalidJSON(t *testing.T) {
	db := setupTestDatabase(t)
	defer db.Close()

	handler := NewUserHandler(db)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/users/login", bytes.NewBuffer([]byte("invalid json")))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.LoginUser(c)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGenerateRandomUsername(t *testing.T) {
	username1 := generateRandomUsername()
	username2 := generateRandomUsername()

	// Should have "user_" prefix
	assert.Contains(t, username1, "user_")
	assert.Contains(t, username2, "user_")

	// Should be different
	assert.NotEqual(t, username1, username2)
}
