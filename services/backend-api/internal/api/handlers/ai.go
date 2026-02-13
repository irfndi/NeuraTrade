package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/irfndi/neuratrade/internal/ai"
)

type AIRegistryQuerier interface {
	GetRegistry(ctx context.Context) (*ai.ModelRegistry, error)
	FindModel(ctx context.Context, modelID string) (*ai.ModelInfo, error)
	GetModelsByProvider(ctx context.Context, providerID string) ([]ai.ModelInfo, error)
	GetActiveProviders(ctx context.Context) ([]ai.ProviderInfo, error)
}

type AIHandler struct {
	registry AIRegistryQuerier
}

func NewAIHandler(registry AIRegistryQuerier) *AIHandler {
	return &AIHandler{
		registry: registry,
	}
}

type AIModelsResponse struct {
	Models []AIModelInfo `json:"models"`
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
		switch m.LatencyClass {
		case "fast":
			score += 2
		case "balanced":
			score += 1
		}

		cost := m.Cost.InputCost.Add(m.Cost.OutputCost)
		costFloat, _ := cost.Float64()
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
