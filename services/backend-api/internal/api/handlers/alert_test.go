package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
)

// TestNewAlertHandler tests the constructor
func TestNewAlertHandler(t *testing.T) {
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewAlertHandler(mockDB)

	assert.NotNil(t, handler)
	assert.Equal(t, mockDB, handler.db)
}

func TestAlertHandler_GetUserAlerts_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewAlertHandler(mockDB)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/alerts", nil)
	c.Set("user_id", "user-123")

	now := time.Now()
	mockDB.ExpectQuery("SELECT id, user_id, alert_type, conditions").
		WithArgs("user-123").
		WillReturnRows(pgxmock.NewRows([]string{"id", "user_id", "alert_type", "conditions", "is_active", "created_at"}).
			AddRow("alert-1", "user-123", "arbitrage", []byte("{}"), true, now))

	handler.GetUserAlerts(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestAlertHandler_CreateAlert_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewAlertHandler(mockDB)

	alert := models.UserAlert{
		AlertType:  "arbitrage",
		Conditions: json.RawMessage(`{"threshold": 0.5}`),
	}
	jsonBytes, _ := json.Marshal(alert)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/v1/alerts", bytes.NewBuffer(jsonBytes))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", "user-123")

	mockDB.ExpectExec("INSERT INTO user_alerts").
		WithArgs(pgxmock.AnyArg(), "user-123", "arbitrage", pgxmock.AnyArg(), true, pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	handler.CreateAlert(c)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestAlertHandler_UpdateAlert_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewAlertHandler(mockDB)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("PUT", "/api/v1/alerts/alert-123", bytes.NewBufferString(`{"is_active": false}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Params = gin.Params{{Key: "id", Value: "alert-123"}}
	c.Set("user_id", "user-123")

	// 1. Check ownership
	mockDB.ExpectQuery("SELECT user_id FROM user_alerts").
		WithArgs("alert-123").
		WillReturnRows(pgxmock.NewRows([]string{"user_id"}).AddRow("user-123"))

	// 2. Update
	mockDB.ExpectExec("UPDATE user_alerts").
		WithArgs("alert-123", pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	handler.UpdateAlert(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}

func TestAlertHandler_DeleteAlert_Success(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mockDB, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mockDB.Close()

	handler := NewAlertHandler(mockDB)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("DELETE", "/api/v1/alerts/alert-123", nil)
	c.Params = gin.Params{{Key: "id", Value: "alert-123"}}
	c.Set("user_id", "user-123")

	// 1. Check ownership
	mockDB.ExpectQuery("SELECT user_id FROM user_alerts").
		WithArgs("alert-123").
		WillReturnRows(pgxmock.NewRows([]string{"user_id"}).AddRow("user-123"))

	// 2. Delete
	mockDB.ExpectExec("DELETE FROM user_alerts").
		WithArgs("alert-123").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	handler.DeleteAlert(c)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.NoError(t, mockDB.ExpectationsWereMet())
}
