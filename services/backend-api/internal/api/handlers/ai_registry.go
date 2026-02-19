package handlers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/ai"
	"github.com/irfndi/neuratrade/internal/database"
)

type AIRegistryQuerier interface {
	GetRegistry(ctx context.Context) (*ai.ModelRegistry, error)
	FindModel(ctx context.Context, modelID string) (*ai.ModelInfo, error)
	GetModelsByProvider(ctx context.Context, providerID string) ([]ai.ModelInfo, error)
	GetActiveProviders(ctx context.Context) ([]ai.ProviderInfo, error)
}

type AIHandler struct {
	registry AIRegistryQuerier
	db       any
}

type dbQuerier interface {
	Query(ctx context.Context, sql string, args ...interface{}) (database.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...interface{}) database.Row
	Exec(ctx context.Context, sql string, args ...interface{}) (database.Result, error)
}

func (h *AIHandler) useSQLiteSyntax() bool {
	_, ok := h.db.(*database.SQLiteDB)
	return ok
}

func NewAIHandler(registry AIRegistryQuerier, db any) *AIHandler {
	return &AIHandler{
		registry: registry,
		db:       db,
	}
}

type AIModelInfo struct {
	ModelID        string `json:"model_id"`
	Provider       string `json:"provider"`
	DisplayName    string `json:"display_name"`
	SupportsTools  bool   `json:"supports_tools"`
	SupportsVision bool   `json:"supports_vision"`
	Cost           string `json:"cost"`
	LatencyClass   string `json:"latency_class"`
	ContextLimit   int    `json:"context_limit"`
}

func (h *AIHandler) GetModels(c *gin.Context) {
	ctx := c.Request.Context()

	registry, err := h.registry.GetRegistry(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get models"})
		return
	}

	providerFilter := c.Query("provider")

	models := []AIModelInfo{}
	for _, m := range registry.Models {
		if m.Status != "active" {
			continue
		}
		if providerFilter != "" && m.ProviderID != providerFilter {
			continue
		}

		cost := m.Cost.InputCost.Add(m.Cost.OutputCost)
		models = append(models, AIModelInfo{
			ModelID:        m.ModelID,
			Provider:       m.ProviderID,
			DisplayName:    m.DisplayName,
			SupportsTools:  m.Capabilities.SupportsTools,
			SupportsVision: m.Capabilities.SupportsVision,
			Cost:           cost.StringFixed(2),
			LatencyClass:   m.LatencyClass,
			ContextLimit:   m.Limits.ContextLimit,
		})
	}

	c.JSON(http.StatusOK, gin.H{"models": models})
}

type AIRouteRequest struct {
	LatencyPreference string `json:"latency_preference"`
	RequireTools      bool   `json:"require_tools"`
	RequireVision     bool   `json:"require_vision"`
	RequireReasoning  bool   `json:"require_reasoning"`
	MaxCost           string `json:"max_cost"`
}

type AIRouteResponse struct {
	Model        *AIModelInfo  `json:"model"`
	Score        float64       `json:"score"`
	Reason       string        `json:"reason"`
	Alternatives []AIModelInfo `json:"alternatives"`
}

func (h *AIHandler) RouteModel(c *gin.Context) {
	ctx := c.Request.Context()

	var req AIRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	models, err := h.registry.GetRegistry(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to get models"})
		return
	}

	maxCost := 0.0
	if req.MaxCost != "" {
		_, _ = fmt.Sscanf(req.MaxCost, "%f", &maxCost)
	}

	latencyPref := req.LatencyPreference
	if latencyPref == "" {
		latencyPref = "balanced"
	}

	var bestModel *ai.ModelInfo
	bestScore := -1.0

	for _, m := range models.Models {
		if m.Status != "active" {
			continue
		}

		if req.RequireTools && !m.Capabilities.SupportsTools {
			continue
		}
		if req.RequireVision && !m.Capabilities.SupportsVision {
			continue
		}
		if req.RequireReasoning && !m.Capabilities.SupportsReasoning {
			continue
		}

		score := 0.0

		if m.LatencyClass == latencyPref {
			score += 2
		} else if latencyPref == "fast" && m.LatencyClass != "fast" {
			score -= 1
		} else if latencyPref == "accurate" && m.LatencyClass != "accurate" {
			score -= 1
		}

		cost := m.Cost.InputCost.Add(m.Cost.OutputCost)
		costFloat, _ := cost.Float64()
		if maxCost > 0 && costFloat > maxCost {
			continue
		}
		if costFloat < 5 {
			score += 2
		} else if costFloat < 20 {
			score += 1
		}

		if score > bestScore {
			bestScore = score
			bestModel = &m
		}
	}

	if bestModel == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no model found matching criteria"})
		return
	}

	cost := bestModel.Cost.InputCost.Add(bestModel.Cost.OutputCost)
	resp := AIRouteResponse{
		Model: &AIModelInfo{
			ModelID:        bestModel.ModelID,
			Provider:       bestModel.ProviderID,
			DisplayName:    bestModel.DisplayName,
			SupportsTools:  bestModel.Capabilities.SupportsTools,
			SupportsVision: bestModel.Capabilities.SupportsVision,
			Cost:           cost.StringFixed(2),
			LatencyClass:   bestModel.LatencyClass,
			ContextLimit:   bestModel.Limits.ContextLimit,
		},
		Score:        bestScore,
		Reason:       "Best match based on latency and cost preferences",
		Alternatives: []AIModelInfo{},
	}

	c.JSON(http.StatusOK, resp)
}

// SelectModelRequest represents a request to select an AI model
type SelectModelRequest struct {
	ModelID string `json:"model_id"`
}

// SelectModelResponse represents the response after selecting a model
type SelectModelResponse struct {
	Success bool         `json:"success"`
	Model   *AIModelInfo `json:"model,omitempty"`
	Message string       `json:"message,omitempty"`
}

// SelectModel selects an AI model for a user
func (h *AIHandler) SelectModel(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user ID is required"})
		return
	}

	var req SelectModelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.ModelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model_id is required"})
		return
	}

	// Verify the model exists in the registry
	ctx := c.Request.Context()
	_, err := h.registry.FindModel(ctx, req.ModelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "model not found: " + req.ModelID})
		return
	}

	// Update the user's selected model with database-compatible SQL syntax.
	if h.db != nil {
		querier, ok := h.db.(dbQuerier)
		if ok {
			updateSQL := "UPDATE users SET selected_ai_model = $1, updated_at = $2 WHERE telegram_id = $3"
			args := []interface{}{req.ModelID, time.Now().UTC(), userID}
			if h.useSQLiteSyntax() {
				updateSQL = "UPDATE users SET selected_ai_model = ?, updated_at = ? WHERE telegram_id = ?"
			}

			_, err = querier.Exec(ctx, updateSQL, args...)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update user model"})
				return
			}
		}
	}

	// Get the model info for response
	modelInfo, _ := h.registry.FindModel(ctx, req.ModelID)
	var modelResp *AIModelInfo
	if modelInfo != nil {
		cost := modelInfo.Cost.InputCost.Add(modelInfo.Cost.OutputCost)
		modelResp = &AIModelInfo{
			ModelID:        modelInfo.ModelID,
			Provider:       modelInfo.ProviderID,
			DisplayName:    modelInfo.DisplayName,
			SupportsTools:  modelInfo.Capabilities.SupportsTools,
			SupportsVision: modelInfo.Capabilities.SupportsVision,
			Cost:           cost.StringFixed(2),
			LatencyClass:   modelInfo.LatencyClass,
			ContextLimit:   modelInfo.Limits.ContextLimit,
		}
	}

	c.JSON(http.StatusOK, SelectModelResponse{
		Success: true,
		Model:   modelResp,
		Message: "Model selected successfully",
	})
}

// GetModelStatus returns the user's current AI model selection and usage
func (h *AIHandler) GetModelStatus(c *gin.Context) {
	userID := c.Param("userId")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user ID is required"})
		return
	}

	ctx := c.Request.Context()

	// Get user's selected model with database-compatible SQL syntax.
	var selectedModel string
	if h.db != nil {
		querier, ok := h.db.(dbQuerier)
		if ok {
			selectSQL := "SELECT selected_ai_model FROM users WHERE telegram_id = $1"
			if h.useSQLiteSyntax() {
				selectSQL = "SELECT selected_ai_model FROM users WHERE telegram_id = ?"
			}

			err := querier.QueryRow(ctx, selectSQL, userID).Scan(&selectedModel)

			if err != nil {
				// User not found or no model selected
				c.JSON(http.StatusOK, gin.H{
					"selected_model": "",
					"provider":       "",
					"daily_spend":    "0.00",
					"monthly_spend":  "0.00",
					"budget_limit":   "Unlimited",
				})
				return
			}
		}
	}

	if selectedModel == "" {
		c.JSON(http.StatusOK, gin.H{
			"selected_model":        "",
			"provider":              "",
			"daily_spend":           "0.00",
			"monthly_spend":         "0.00",
			"budget_limit":          "Unlimited",
			"daily_budget_exceeded": false,
		})
		return
	}

	// Get model info from registry
	modelInfo, err := h.registry.FindModel(ctx, selectedModel)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"selected_model":        selectedModel,
			"provider":              "unknown",
			"daily_spend":           "0.00",
			"monthly_spend":         "0.00",
			"budget_limit":          "Unlimited",
			"daily_budget_exceeded": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"selected_model":        modelInfo.ModelID,
		"provider":              modelInfo.ProviderID,
		"daily_spend":           "0.00",
		"monthly_spend":         "0.00",
		"budget_limit":          "Unlimited",
		"daily_budget_exceeded": false,
	})
}
