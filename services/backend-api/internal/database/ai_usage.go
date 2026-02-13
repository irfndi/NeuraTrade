package database

import (
	"context"
	"encoding/json"
	"time"

	"github.com/irfndi/neuratrade/internal/models"
	"github.com/shopspring/decimal"
)

type AIUsageRepository struct {
	pool DBPool
}

func NewAIUsageRepository(pool DBPool) *AIUsageRepository {
	return &AIUsageRepository{pool: pool}
}

func (r *AIUsageRepository) Create(ctx context.Context, usage *models.AIUsageCreate) (*models.AIUsage, error) {
	aiUsage := usage.ToAIUsage()

	if aiUsage.Metadata == nil {
		aiUsage.Metadata = json.RawMessage("{}")
	}

	query := `
		INSERT INTO ai_usage (
			provider, model, request_type,
			input_tokens, output_tokens,
			input_cost_usd, output_cost_usd,
			user_id, session_id, request_id,
			latency_ms, status, error_message, metadata
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING id, created_at`

	err := r.pool.QueryRow(ctx, query,
		aiUsage.Provider,
		aiUsage.Model,
		aiUsage.RequestType,
		aiUsage.InputTokens,
		aiUsage.OutputTokens,
		aiUsage.InputCostUSD,
		aiUsage.OutputCostUSD,
		aiUsage.UserID,
		aiUsage.SessionID,
		aiUsage.RequestID,
		aiUsage.LatencyMs,
		aiUsage.Status,
		aiUsage.ErrorMessage,
		aiUsage.Metadata,
	).Scan(&aiUsage.ID, &aiUsage.CreatedAt)

	if err != nil {
		return nil, err
	}

	return aiUsage, nil
}

func (r *AIUsageRepository) GetCostForPeriod(ctx context.Context, startDate, endDate time.Time, userID *string) (decimal.Decimal, error) {
	var cost decimal.Decimal

	if userID != nil {
		query := `
			SELECT COALESCE(SUM(total_cost_usd), 0)
			FROM ai_usage
			WHERE created_at >= $1 AND created_at < $2 AND user_id = $3`
		err := r.pool.QueryRow(ctx, query, startDate, endDate, userID).Scan(&cost)
		return cost, err
	}

	query := `
		SELECT COALESCE(SUM(total_cost_usd), 0)
		FROM ai_usage
		WHERE created_at >= $1 AND created_at < $2`
	err := r.pool.QueryRow(ctx, query, startDate, endDate).Scan(&cost)
	return cost, err
}

func (r *AIUsageRepository) GetDailyCost(ctx context.Context, date time.Time, userID *string) (decimal.Decimal, error) {
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endOfDay := startOfDay.Add(24 * time.Hour)
	return r.GetCostForPeriod(ctx, startOfDay, endOfDay, userID)
}

func (r *AIUsageRepository) GetMonthlyCost(ctx context.Context, date time.Time, userID *string) (decimal.Decimal, error) {
	startOfMonth := time.Date(date.Year(), date.Month(), 1, 0, 0, 0, 0, date.Location())
	endOfMonth := startOfMonth.AddDate(0, 1, 0)
	return r.GetCostForPeriod(ctx, startOfMonth, endOfMonth, userID)
}

func (r *AIUsageRepository) IsDailyBudgetExceeded(ctx context.Context, dailyBudget decimal.Decimal, userID *string) (bool, error) {
	cost, err := r.GetDailyCost(ctx, time.Now(), userID)
	if err != nil {
		return false, err
	}
	return cost.GreaterThanOrEqual(dailyBudget), nil
}

func (r *AIUsageRepository) IsMonthlyBudgetExceeded(ctx context.Context, monthlyBudget decimal.Decimal, userID *string) (bool, error) {
	cost, err := r.GetMonthlyCost(ctx, time.Now(), userID)
	if err != nil {
		return false, err
	}
	return cost.GreaterThanOrEqual(monthlyBudget), nil
}

func (r *AIUsageRepository) GetDailySummary(ctx context.Context, startDate, endDate time.Time) ([]models.AIUsageDailySummary, error) {
	query := `
		SELECT
			DATE(created_at) as usage_date,
			provider,
			model,
			COUNT(*) as total_requests,
			SUM(input_tokens) as total_input_tokens,
			SUM(output_tokens) as total_output_tokens,
			SUM(total_tokens) as grand_total_tokens,
			SUM(input_cost_usd) as total_input_cost,
			SUM(output_cost_usd) as total_output_cost,
			SUM(total_cost_usd) as grand_total_cost,
			AVG(latency_ms) as avg_latency_ms,
			COUNT(*) FILTER (WHERE status = 'error') as error_count,
			COUNT(*) FILTER (WHERE status = 'timeout') as timeout_count
		FROM ai_usage
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY DATE(created_at), provider, model
		ORDER BY usage_date DESC, grand_total_cost DESC`

	rows, err := r.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []models.AIUsageDailySummary
	for rows.Next() {
		var s models.AIUsageDailySummary
		err := rows.Scan(
			&s.UsageDate,
			&s.Provider,
			&s.Model,
			&s.TotalRequests,
			&s.TotalInputTokens,
			&s.TotalOutputTokens,
			&s.GrandTotalTokens,
			&s.TotalInputCost,
			&s.TotalOutputCost,
			&s.GrandTotalCost,
			&s.AvgLatencyMs,
			&s.ErrorCount,
			&s.TimeoutCount,
		)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}

	return summaries, rows.Err()
}

func (r *AIUsageRepository) GetMonthlySummary(ctx context.Context, startDate, endDate time.Time) ([]models.AIUsageMonthlySummary, error) {
	query := `
		SELECT
			DATE_TRUNC('month', created_at) as usage_month,
			provider,
			COUNT(*) as total_requests,
			SUM(input_tokens) as total_input_tokens,
			SUM(output_tokens) as total_output_tokens,
			SUM(total_tokens) as grand_total_tokens,
			SUM(total_cost_usd) as grand_total_cost,
			AVG(latency_ms) as avg_latency_ms
		FROM ai_usage
		WHERE created_at >= $1 AND created_at < $2
		GROUP BY DATE_TRUNC('month', created_at), provider
		ORDER BY usage_month DESC, grand_total_cost DESC`

	rows, err := r.pool.Query(ctx, query, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var summaries []models.AIUsageMonthlySummary
	for rows.Next() {
		var s models.AIUsageMonthlySummary
		err := rows.Scan(
			&s.UsageMonth,
			&s.Provider,
			&s.TotalRequests,
			&s.TotalInputTokens,
			&s.TotalOutputTokens,
			&s.GrandTotalTokens,
			&s.GrandTotalCost,
			&s.AvgLatencyMs,
		)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, s)
	}

	return summaries, rows.Err()
}

func NewAIUsageRepositoryFromDB(pool DBPool) *AIUsageRepository {
	return &AIUsageRepository{pool: pool}
}
