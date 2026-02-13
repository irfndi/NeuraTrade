package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

type mockAIUsageRepo struct {
	dailyCost     decimal.Decimal
	monthlyCost   decimal.Decimal
	dailyErr      error
	monthlyErr    error
	dailyExceed   bool
	monthlyExceed bool
}

func (m *mockAIUsageRepo) GetDailyCost(ctx context.Context, date time.Time, userID *string) (decimal.Decimal, error) {
	return m.dailyCost, m.dailyErr
}

func (m *mockAIUsageRepo) GetMonthlyCost(ctx context.Context, date time.Time, userID *string) (decimal.Decimal, error) {
	return m.monthlyCost, m.monthlyErr
}

func (m *mockAIUsageRepo) IsDailyBudgetExceeded(ctx context.Context, budget decimal.Decimal, userID *string) (bool, error) {
	return m.dailyExceed, m.dailyErr
}

func (m *mockAIUsageRepo) IsMonthlyBudgetExceeded(ctx context.Context, budget decimal.Decimal, userID *string) (bool, error) {
	return m.monthlyExceed, m.monthlyErr
}

func TestBudgetHandler_GetBudgetStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("returns budget status successfully", func(t *testing.T) {
		repo := &mockAIUsageRepo{
			dailyCost:   decimal.NewFromFloat(5.00),
			monthlyCost: decimal.NewFromFloat(50.00),
		}
		handler := NewBudgetHandler(repo, decimal.NewFromFloat(10.00), decimal.NewFromFloat(200.00))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/budget/status", nil)

		handler.GetBudgetStatus(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "5.00")
		assert.Contains(t, w.Body.String(), "50.00")
		assert.Contains(t, w.Body.String(), "10.00")
		assert.Contains(t, w.Body.String(), "200.00")
	})

	t.Run("handles database error gracefully", func(t *testing.T) {
		repo := &mockAIUsageRepo{
			dailyErr: assert.AnError,
		}
		handler := NewBudgetHandler(repo, decimal.NewFromFloat(10.00), decimal.NewFromFloat(200.00))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/budget/status", nil)

		handler.GetBudgetStatus(c)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestBudgetHandler_CheckBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("allows request when budget not exceeded", func(t *testing.T) {
		repo := &mockAIUsageRepo{
			dailyExceed:   false,
			monthlyExceed: false,
		}
		handler := NewBudgetHandler(repo, decimal.NewFromFloat(10.00), decimal.NewFromFloat(200.00))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/budget/check", nil)

		handler.CheckBudget(c)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Contains(t, w.Body.String(), "allowed")
		assert.Contains(t, w.Body.String(), "true")
	})

	t.Run("rejects when daily budget exceeded", func(t *testing.T) {
		repo := &mockAIUsageRepo{
			dailyExceed:   true,
			monthlyExceed: false,
		}
		handler := NewBudgetHandler(repo, decimal.NewFromFloat(10.00), decimal.NewFromFloat(200.00))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/budget/check", nil)

		handler.CheckBudget(c)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Contains(t, w.Body.String(), "daily budget exceeded")
	})

	t.Run("rejects when monthly budget exceeded", func(t *testing.T) {
		repo := &mockAIUsageRepo{
			dailyExceed:   false,
			monthlyExceed: true,
		}
		handler := NewBudgetHandler(repo, decimal.NewFromFloat(10.00), decimal.NewFromFloat(200.00))

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("GET", "/budget/check", nil)

		handler.CheckBudget(c)

		assert.Equal(t, http.StatusTooManyRequests, w.Code)
		assert.Contains(t, w.Body.String(), "monthly budget exceeded")
	})
}
