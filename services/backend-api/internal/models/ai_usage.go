package models

import (
	"encoding/json"
	"time"

	"github.com/shopspring/decimal"
)

type AIUsageStatus string

const (
	AIUsageStatusSuccess   AIUsageStatus = "success"
	AIUsageStatusError     AIUsageStatus = "error"
	AIUsageStatusTimeout   AIUsageStatus = "timeout"
	AIUsageStatusCancelled AIUsageStatus = "cancelled"
)

type AIUsage struct {
	ID            string          `json:"id" db:"id"`
	Provider      string          `json:"provider" db:"provider"`
	Model         string          `json:"model" db:"model"`
	RequestType   string          `json:"request_type" db:"request_type"`
	InputTokens   int             `json:"input_tokens" db:"input_tokens"`
	OutputTokens  int             `json:"output_tokens" db:"output_tokens"`
	TotalTokens   int             `json:"total_tokens" db:"total_tokens"`
	InputCostUSD  decimal.Decimal `json:"input_cost_usd" db:"input_cost_usd"`
	OutputCostUSD decimal.Decimal `json:"output_cost_usd" db:"output_cost_usd"`
	TotalCostUSD  decimal.Decimal `json:"total_cost_usd" db:"total_cost_usd"`
	UserID        *string         `json:"user_id" db:"user_id"`
	SessionID     *string         `json:"session_id" db:"session_id"`
	RequestID     *string         `json:"request_id" db:"request_id"`
	LatencyMs     *int            `json:"latency_ms" db:"latency_ms"`
	Status        AIUsageStatus   `json:"status" db:"status"`
	ErrorMessage  *string         `json:"error_message" db:"error_message"`
	Metadata      json.RawMessage `json:"metadata" db:"metadata"`
	CreatedAt     time.Time       `json:"created_at" db:"created_at"`
}

type AIUsageCreate struct {
	Provider      string          `json:"provider" binding:"required"`
	Model         string          `json:"model" binding:"required"`
	RequestType   string          `json:"request_type"`
	InputTokens   int             `json:"input_tokens" binding:"min=0"`
	OutputTokens  int             `json:"output_tokens" binding:"min=0"`
	InputCostUSD  decimal.Decimal `json:"input_cost_usd"`
	OutputCostUSD decimal.Decimal `json:"output_cost_usd"`
	UserID        *string         `json:"user_id"`
	SessionID     *string         `json:"session_id"`
	RequestID     *string         `json:"request_id"`
	LatencyMs     *int            `json:"latency_ms"`
	Status        AIUsageStatus   `json:"status"`
	ErrorMessage  *string         `json:"error_message"`
	Metadata      json.RawMessage `json:"metadata"`
}

type AIUsageDailySummary struct {
	UsageDate         time.Time       `json:"usage_date" db:"usage_date"`
	Provider          string          `json:"provider" db:"provider"`
	Model             string          `json:"model" db:"model"`
	TotalRequests     int             `json:"total_requests" db:"total_requests"`
	TotalInputTokens  int             `json:"total_input_tokens" db:"total_input_tokens"`
	TotalOutputTokens int             `json:"total_output_tokens" db:"total_output_tokens"`
	GrandTotalTokens  int             `json:"grand_total_tokens" db:"grand_total_tokens"`
	TotalInputCost    decimal.Decimal `json:"total_input_cost" db:"total_input_cost"`
	TotalOutputCost   decimal.Decimal `json:"total_output_cost" db:"total_output_cost"`
	GrandTotalCost    decimal.Decimal `json:"grand_total_cost" db:"grand_total_cost"`
	AvgLatencyMs      *float64        `json:"avg_latency_ms" db:"avg_latency_ms"`
	ErrorCount        int             `json:"error_count" db:"error_count"`
	TimeoutCount      int             `json:"timeout_count" db:"timeout_count"`
}

type AIUsageMonthlySummary struct {
	UsageMonth        time.Time       `json:"usage_month" db:"usage_month"`
	Provider          string          `json:"provider" db:"provider"`
	TotalRequests     int             `json:"total_requests" db:"total_requests"`
	TotalInputTokens  int             `json:"total_input_tokens" db:"total_input_tokens"`
	TotalOutputTokens int             `json:"total_output_tokens" db:"total_output_tokens"`
	GrandTotalTokens  int             `json:"grand_total_tokens" db:"grand_total_tokens"`
	GrandTotalCost    decimal.Decimal `json:"grand_total_cost" db:"grand_total_cost"`
	AvgLatencyMs      *float64        `json:"avg_latency_ms" db:"avg_latency_ms"`
}

type AIUsageUserSummary struct {
	UserID           *string         `json:"user_id" db:"user_id"`
	Provider         string          `json:"provider" db:"provider"`
	TotalRequests    int             `json:"total_requests" db:"total_requests"`
	GrandTotalTokens int             `json:"grand_total_tokens" db:"grand_total_tokens"`
	GrandTotalCost   decimal.Decimal `json:"grand_total_cost" db:"grand_total_cost"`
}

func (c *AIUsageCreate) ToAIUsage() *AIUsage {
	now := time.Now()
	status := c.Status
	if status == "" {
		status = AIUsageStatusSuccess
	}
	requestType := c.RequestType
	if requestType == "" {
		requestType = "chat"
	}

	return &AIUsage{
		Provider:      c.Provider,
		Model:         c.Model,
		RequestType:   requestType,
		InputTokens:   c.InputTokens,
		OutputTokens:  c.OutputTokens,
		TotalTokens:   c.InputTokens + c.OutputTokens,
		InputCostUSD:  c.InputCostUSD,
		OutputCostUSD: c.OutputCostUSD,
		TotalCostUSD:  c.InputCostUSD.Add(c.OutputCostUSD),
		UserID:        c.UserID,
		SessionID:     c.SessionID,
		RequestID:     c.RequestID,
		LatencyMs:     c.LatencyMs,
		Status:        status,
		ErrorMessage:  c.ErrorMessage,
		Metadata:      c.Metadata,
		CreatedAt:     now,
	}
}
