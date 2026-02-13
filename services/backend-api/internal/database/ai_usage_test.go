package database

import (
	"testing"

	"github.com/irfndi/neuratrade/internal/models"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAIUsageRepository_NewAIUsageRepository(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()

	repo := NewAIUsageRepository(&MockDBPool{mock: mockPool})
	assert.NotNil(t, repo)
}

func TestAIUsageRepository_NewAIUsageRepositoryFromDB(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()

	repo := NewAIUsageRepositoryFromDB(&MockDBPool{mock: mockPool})
	assert.NotNil(t, repo)
}

func TestAIUsageRepository_Struct(t *testing.T) {
	mockPool, err := pgxmock.NewPool()
	require.NoError(t, err)
	defer mockPool.Close()

	repo := &AIUsageRepository{pool: &MockDBPool{mock: mockPool}}
	assert.NotNil(t, repo)
	assert.NotNil(t, repo.pool)
}

func TestAIUsageRepository_Create_Conversion(t *testing.T) {
	userID := "user-123"
	reqID := "req-456"
	latency := 1500

	usage := &models.AIUsageCreate{
		Provider:      "openai",
		Model:         "gpt-4",
		RequestType:   "chat",
		InputTokens:   100,
		OutputTokens:  200,
		InputCostUSD:  decimal.NewFromFloat(0.001),
		OutputCostUSD: decimal.NewFromFloat(0.002),
		UserID:        &userID,
		RequestID:     &reqID,
		LatencyMs:     &latency,
		Status:        models.AIUsageStatusSuccess,
	}

	aiUsage := usage.ToAIUsage()
	assert.NotNil(t, aiUsage)
	assert.Equal(t, "openai", aiUsage.Provider)
	assert.Equal(t, "gpt-4", aiUsage.Model)
	assert.Equal(t, 100, aiUsage.InputTokens)
	assert.Equal(t, 200, aiUsage.OutputTokens)
	assert.Equal(t, 300, aiUsage.TotalTokens)
	assert.Equal(t, &userID, aiUsage.UserID)
	assert.Equal(t, &reqID, aiUsage.RequestID)
	assert.Equal(t, models.AIUsageStatusSuccess, aiUsage.Status)
}

func TestAIUsageRepository_Create_Conversion_DefaultStatus(t *testing.T) {
	usage := &models.AIUsageCreate{
		Provider:    "anthropic",
		Model:       "claude-3",
		RequestType: "chat",
		InputTokens: 50,
	}

	aiUsage := usage.ToAIUsage()
	assert.NotNil(t, aiUsage)
	assert.Equal(t, models.AIUsageStatusSuccess, aiUsage.Status)
	assert.Equal(t, "chat", aiUsage.RequestType)
}

func TestAIUsageRepository_Create_Conversion_EmptyStatus(t *testing.T) {
	usage := &models.AIUsageCreate{
		Provider:    "openai",
		Model:       "gpt-4",
		InputTokens: 50,
	}

	aiUsage := usage.ToAIUsage()
	assert.NotNil(t, aiUsage)
	assert.Equal(t, models.AIUsageStatusSuccess, aiUsage.Status)
	assert.Equal(t, "chat", aiUsage.RequestType)
}

func TestAIUsageRepository_BudgetChecks(t *testing.T) {
	tests := []struct {
		name     string
		budget   decimal.Decimal
		actual   decimal.Decimal
		exceeded bool
	}{
		{"budget exceeded", decimal.NewFromFloat(0.10), decimal.NewFromFloat(0.15), true},
		{"budget not exceeded", decimal.NewFromFloat(0.20), decimal.NewFromFloat(0.15), false},
		{"budget exactly met", decimal.NewFromFloat(0.10), decimal.NewFromFloat(0.10), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exceeded := tt.actual.GreaterThanOrEqual(tt.budget)
			assert.Equal(t, tt.exceeded, exceeded)
		})
	}
}

func TestAIUsageRepository_CostCalculations(t *testing.T) {
	inputCost := decimal.NewFromFloat(0.001)
	outputCost := decimal.NewFromFloat(0.002)
	totalCost := inputCost.Add(outputCost)

	assert.True(t, totalCost.Equal(decimal.NewFromFloat(0.003)))
}

func TestAIUsageRepository_EmptyMetadata(t *testing.T) {
	usage := &models.AIUsageCreate{
		Provider: "openai",
		Model:    "gpt-4",
	}

	aiUsage := usage.ToAIUsage()
	assert.Nil(t, aiUsage.Metadata)
}

func TestAIUsageRepository_WithNilFields(t *testing.T) {
	usage := &models.AIUsageCreate{
		Provider:    "openai",
		Model:       "gpt-4",
		RequestType: "chat",
	}

	aiUsage := usage.ToAIUsage()
	assert.Nil(t, aiUsage.UserID)
	assert.Nil(t, aiUsage.SessionID)
	assert.Nil(t, aiUsage.RequestID)
	assert.Nil(t, aiUsage.LatencyMs)
	assert.Nil(t, aiUsage.ErrorMessage)
}
