package ai

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/shopspring/decimal"
)

// Router provides intelligent model selection based on requirements and constraints
type Router struct {
	registry *Registry
}

// RouterOption configures the Router
type RouterOption func(*Router)

// NewRouter creates a new AI model router
func NewRouter(registry *Registry, opts ...RouterOption) *Router {
	r := &Router{
		registry: registry,
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// RoutingConstraints defines constraints for model selection
type RoutingConstraints struct {
	// Capability requirements
	RequiredCaps ModelCapability

	// Budget constraints (in USD per 1M tokens)
	MaxInputCost  decimal.Decimal
	MaxOutputCost decimal.Decimal

	// Latency preference: "fast", "balanced", "accurate"
	LatencyPreference string

	// Provider whitelist (empty = all providers)
	AllowedProviders []string

	// Provider blacklist
	BlockedProviders []string

	// Risk level preference: "low", "medium", "high"
	RiskLevel string

	// Context token requirements
	MinContextTokens int
}

// RoutingResult represents a routing decision
type RoutingResult struct {
	Model        ModelInfo
	Provider     ProviderInfo
	Score        float64
	Reason       string
	Alternatives []ModelInfo
}

// Route selects the best model based on constraints
func (r *Router) Route(ctx context.Context, constraints RoutingConstraints) (*RoutingResult, error) {
	// Get all active models
	models, err := r.registry.FindModelsByCapability(ctx, constraints.RequiredCaps)
	if err != nil {
		return nil, fmt.Errorf("failed to get models: %w", err)
	}

	if len(models) == 0 {
		return nil, fmt.Errorf("no models match the required capabilities")
	}

	// Filter models based on constraints
	var candidates []ModelInfo
	for _, model := range models {
		if !r.matchesConstraints(model, constraints) {
			continue
		}
		candidates = append(candidates, model)
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("no models match all constraints")
	}

	// Score and rank candidates
	scored := r.scoreModels(candidates, constraints)

	// Sort by score (descending)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	winner := scored[0]

	// Get provider info
	providers, err := r.registry.GetActiveProviders(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get providers: %w", err)
	}

	var provider ProviderInfo
	for _, p := range providers {
		if p.ID == winner.Model.ProviderID {
			provider = p
			break
		}
	}

	// Build alternatives list (exclude winner)
	var alternatives []ModelInfo
	for i, s := range scored {
		if i > 0 && i <= 3 { // Top 3 alternatives
			alternatives = append(alternatives, s.Model)
		}
	}

	return &RoutingResult{
		Model:        winner.Model,
		Provider:     provider,
		Score:        winner.Score,
		Reason:       winner.Reason,
		Alternatives: alternatives,
	}, nil
}

// matchesConstraints checks if a model meets all constraints
func (r *Router) matchesConstraints(model ModelInfo, constraints RoutingConstraints) bool {
	// Check status
	if model.Status != "active" {
		return false
	}

	// Check provider whitelist/blacklist
	if len(constraints.AllowedProviders) > 0 {
		found := false
		for _, p := range constraints.AllowedProviders {
			if p == model.ProviderID {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	for _, p := range constraints.BlockedProviders {
		if p == model.ProviderID {
			return false
		}
	}

	// Check cost constraints
	if constraints.MaxInputCost.GreaterThan(decimal.Zero) && model.Cost.InputCost.GreaterThan(constraints.MaxInputCost) {
		return false
	}
	if constraints.MaxOutputCost.GreaterThan(decimal.Zero) && model.Cost.OutputCost.GreaterThan(constraints.MaxOutputCost) {
		return false
	}

	// Check context token requirements
	if constraints.MinContextTokens > 0 && model.Limits.ContextLimit < constraints.MinContextTokens {
		return false
	}

	// Check risk level
	if constraints.RiskLevel != "" {
		modelRisk := riskScore(model.RiskLevel)
		constraintRisk := riskScore(constraints.RiskLevel)
		if modelRisk > constraintRisk {
			return false
		}
	}

	return true
}

// riskScore converts risk level to numeric score
func riskScore(level string) int {
	switch level {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	default:
		return 2
	}
}

// ScoredModel represents a model with its routing score
type ScoredModel struct {
	Model  ModelInfo
	Score  float64
	Reason string
}

// scoreModels calculates routing scores for candidates
func (r *Router) scoreModels(models []ModelInfo, constraints RoutingConstraints) []ScoredModel {
	var scored []ScoredModel

	for _, model := range models {
		score := 0.0
		reasons := []string{}

		// Cost efficiency (lower is better, inverse scoring)
		totalCost := model.Cost.InputCost.Add(model.Cost.OutputCost)
		if totalCost.GreaterThan(decimal.Zero) {
			one := decimal.NewFromFloat(1.0)
			hundred := decimal.NewFromFloat(100.0)
			decimalCostScore := hundred.Div(one.Add(totalCost))
			costScore, _ := decimalCostScore.Float64()
			score += costScore * 0.3 // 30% weight
			reasons = append(reasons, fmt.Sprintf("cost-efficient ($%s/1M)", totalCost.String()))
		}

		// Latency match
		latencyScore := r.scoreLatency(model, constraints.LatencyPreference)
		score += latencyScore * 0.25 // 25% weight
		if latencyScore > 80 {
			reasons = append(reasons, "low-latency")
		}

		// Capability richness
		capScore := r.scoreCapabilities(model)
		score += capScore * 0.2 // 20% weight
		if capScore > 50 {
			reasons = append(reasons, "rich-capabilities")
		}

		// Context size bonus
		if model.Limits.ContextLimit > 100000 {
			score += 15.0
			reasons = append(reasons, "large-context")
		}

		// Reliability bonus for established providers
		if model.ProviderID == "openai" || model.ProviderID == "anthropic" {
			score += 10.0
			reasons = append(reasons, "reliable-provider")
		}

		scored = append(scored, ScoredModel{
			Model:  model,
			Score:  score,
			Reason: joinReasons(reasons),
		})
	}

	return scored
}

// scoreLatency calculates latency score based on preference
func (r *Router) scoreLatency(model ModelInfo, preference string) float64 {
	switch model.LatencyClass {
	case "fast":
		if preference == "fast" {
			return 100.0
		}
		return 80.0
	case "medium":
		if preference == "balanced" {
			return 100.0
		}
		if preference == "fast" {
			return 60.0
		}
		return 80.0
	case "slow":
		if preference == "accurate" {
			return 100.0
		}
		return 40.0
	default:
		return 50.0
	}
}

// scoreCapabilities scores model based on capability richness
func (r *Router) scoreCapabilities(model ModelInfo) float64 {
	score := 0.0

	if model.Capabilities.SupportsTools {
		score += 25.0
	}
	if model.Capabilities.SupportsVision {
		score += 25.0
	}
	if model.Capabilities.SupportsReasoning {
		score += 25.0
	}
	if model.StructuredOutput {
		score += 25.0
	}

	return score
}

// joinReasons combines reasons into a single string
func joinReasons(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}

	result := reasons[0]
	for i := 1; i < len(reasons); i++ {
		result += ", " + reasons[i]
	}
	return result
}

// CalculateCost estimates the cost for a given model and token usage
func CalculateCost(model ModelInfo, inputTokens, outputTokens int) decimal.Decimal {
	inputTokensDecimal := decimal.NewFromInt(int64(inputTokens))
	outputTokensDecimal := decimal.NewFromInt(int64(outputTokens))
	million := decimal.NewFromInt(1000000)

	inputCost := inputTokensDecimal.Div(million).Mul(model.Cost.InputCost)
	outputCost := outputTokensDecimal.Div(million).Mul(model.Cost.OutputCost)

	return inputCost.Add(outputCost)
}

// EstimateTokens provides a rough token estimate for text
// This is a simplified estimation (roughly 4 characters per token for English)
func EstimateTokens(text string) int {
	return int(math.Ceil(float64(len(text)) / 4.0))
}
