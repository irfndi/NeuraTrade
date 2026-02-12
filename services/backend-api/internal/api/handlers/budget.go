package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

type BudgetQuerier interface {
	GetDailyCost(ctx context.Context, date time.Time, userID *string) (decimal.Decimal, error)
	GetMonthlyCost(ctx context.Context, date time.Time, userID *string) (decimal.Decimal, error)
	IsDailyBudgetExceeded(ctx context.Context, budget decimal.Decimal, userID *string) (bool, error)
	IsMonthlyBudgetExceeded(ctx context.Context, budget decimal.Decimal, userID *string) (bool, error)
}

// BudgetHandler handles budget-related endpoints
type BudgetHandler struct {
	aiUsageRepo   BudgetQuerier
	dailyBudget   decimal.Decimal
	monthlyBudget decimal.Decimal
}

// NewBudgetHandler creates a new budget handler
func NewBudgetHandler(aiUsageRepo BudgetQuerier, dailyBudget, monthlyBudget decimal.Decimal) *BudgetHandler {
	return &BudgetHandler{
		aiUsageRepo:   aiUsageRepo,
		dailyBudget:   dailyBudget,
		monthlyBudget: monthlyBudget,
	}
}

// BudgetStatusResponse represents the response for /budget/status
type BudgetStatusResponse struct {
	Daily   BudgetInfo   `json:"daily"`
	Monthly BudgetInfo   `json:"monthly"`
	Limit   BudgetLimits `json:"limits"`
}

// BudgetInfo represents budget usage information
type BudgetInfo struct {
	Used      string  `json:"used"`
	Remaining string  `json:"remaining"`
	Percent   float64 `json:"percent_used"`
}

// BudgetLimits represents budget limits
type BudgetLimits struct {
	Daily   string `json:"daily"`
	Monthly string `json:"monthly"`
}

// GetBudgetStatus returns the current budget status
func (h *BudgetHandler) GetBudgetStatus(c *gin.Context) {
	ctx := c.Request.Context()
	today := time.Now().Truncate(24 * time.Hour)
	currentMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())

	// Get daily usage
	dailyUsed, err := h.aiUsageRepo.GetDailyCost(ctx, today, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get daily usage"})
		return
	}

	// Get monthly usage
	monthlyUsed, err := h.aiUsageRepo.GetMonthlyCost(ctx, currentMonth, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get monthly usage"})
		return
	}

	// Calculate remaining
	dailyRemaining := h.dailyBudget.Sub(dailyUsed)
	if dailyRemaining.LessThan(decimal.Zero) {
		dailyRemaining = decimal.Zero
	}

	monthlyRemaining := h.monthlyBudget.Sub(monthlyUsed)
	if monthlyRemaining.LessThan(decimal.Zero) {
		monthlyRemaining = decimal.Zero
	}

	// Calculate percentages
	dailyPercent := 0.0
	if h.dailyBudget.GreaterThan(decimal.Zero) {
		dailyPercent, _ = dailyUsed.Div(h.dailyBudget).Mul(decimal.NewFromInt(100)).Float64()
	}

	monthlyPercent := 0.0
	if h.monthlyBudget.GreaterThan(decimal.Zero) {
		monthlyPercent, _ = monthlyUsed.Div(h.monthlyBudget).Mul(decimal.NewFromInt(100)).Float64()
	}

	response := BudgetStatusResponse{
		Daily: BudgetInfo{
			Used:      dailyUsed.StringFixed(2),
			Remaining: dailyRemaining.StringFixed(2),
			Percent:   dailyPercent,
		},
		Monthly: BudgetInfo{
			Used:      monthlyUsed.StringFixed(2),
			Remaining: monthlyRemaining.StringFixed(2),
			Percent:   monthlyPercent,
		},
		Limit: BudgetLimits{
			Daily:   h.dailyBudget.StringFixed(2),
			Monthly: h.monthlyBudget.StringFixed(2),
		},
	}

	c.JSON(http.StatusOK, response)
}

// CheckBudget checks if budget is available before making an AI call
func (h *BudgetHandler) CheckBudget(c *gin.Context) {
	ctx := c.Request.Context()
	today := time.Now().Truncate(24 * time.Hour)
	currentMonth := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, today.Location())

	// Check daily budget
	dailyExceeded, err := h.aiUsageRepo.IsDailyBudgetExceeded(ctx, h.dailyBudget, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check daily budget"})
		return
	}

	if dailyExceeded {
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":    "daily budget exceeded",
			"limit":    h.dailyBudget.StringFixed(2),
			"reset_at": today.Add(24 * time.Hour).Format(time.RFC3339),
		})
		return
	}

	// Check monthly budget
	monthlyExceeded, err := h.aiUsageRepo.IsMonthlyBudgetExceeded(ctx, h.monthlyBudget, nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to check monthly budget"})
		return
	}

	if monthlyExceeded {
		nextMonth := currentMonth.AddDate(0, 1, 0)
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error":    "monthly budget exceeded",
			"limit":    h.monthlyBudget.StringFixed(2),
			"reset_at": nextMonth.Format(time.RFC3339),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"allowed": true})
}
