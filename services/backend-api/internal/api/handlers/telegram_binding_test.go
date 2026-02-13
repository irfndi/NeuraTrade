package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestNewTelegramBindingHandler_Nil(t *testing.T) {
	handler := NewTelegramBindingHandler(nil, nil, nil)

	assert.NotNil(t, handler)
	assert.Nil(t, handler.db)
	assert.Nil(t, handler.userHandler)
	assert.Nil(t, handler.otpService)
}

func TestTelegramBindingHandler_InitiateBinding(t *testing.T) {
	t.Run("missing user_id", func(t *testing.T) {
		handler := NewTelegramBindingHandler(nil, nil, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/telegram/binding/initiate", bytes.NewBuffer([]byte("{}")))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.InitiateBinding(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		assert.NoError(t, err)
		assert.Contains(t, response, "error")
	})

	t.Run("invalid JSON", func(t *testing.T) {
		handler := NewTelegramBindingHandler(nil, nil, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/telegram/binding/initiate", bytes.NewBuffer([]byte("invalid json")))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.InitiateBinding(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestTelegramBindingHandler_CompleteBinding(t *testing.T) {
	t.Run("missing required fields", func(t *testing.T) {
		handler := NewTelegramBindingHandler(nil, nil, nil)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/telegram/binding/complete", bytes.NewBuffer([]byte("{}")))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.CompleteBinding(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid code length", func(t *testing.T) {
		handler := NewTelegramBindingHandler(nil, nil, nil)

		body := map[string]interface{}{
			"user_id": "test-user-id",
			"code":    "12345",
			"chat_id": "123456789",
		}
		jsonData, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest("POST", "/telegram/binding/complete", bytes.NewBuffer(jsonData))
		c.Request.Header.Set("Content-Type", "application/json")

		handler.CompleteBinding(c)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestBindingRequest_Struct(t *testing.T) {
	t.Run("valid binding request", func(t *testing.T) {
		req := BindingRequest{
			UserID: "user-123",
			Code:   "123456",
			ChatID: "987654321",
		}
		assert.Equal(t, "user-123", req.UserID)
		assert.Equal(t, "123456", req.Code)
		assert.Equal(t, "987654321", req.ChatID)
	})
}

func TestBindingResponse_Struct(t *testing.T) {
	t.Run("success response", func(t *testing.T) {
		resp := BindingResponse{
			Success:   true,
			Message:   "Telegram profile bound successfully",
			UserID:    "user-123",
			ExpiresAt: "2024-01-01T00:00:00Z",
		}
		assert.True(t, resp.Success)
		assert.NotEmpty(t, resp.Message)
		assert.Equal(t, "user-123", resp.UserID)
	})
}

func TestTelegramBindingHandlerAliases(t *testing.T) {
	handler := NewTelegramBindingHandler(nil, nil, nil)

	assert.NotNil(t, handler.GenerateBindingCode)
	assert.NotNil(t, handler.VerifyBindingCode)
}

func TestOTPServicePurpose(t *testing.T) {
	assert.Equal(t, "telegram_binding", models.OTPPurposeTelegramBinding)
	assert.Equal(t, "password_reset", models.OTPPurposePasswordReset)
	assert.Equal(t, "email_verify", models.OTPPurposeEmailVerify)
}
