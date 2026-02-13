// Package ai provides AI provider registry and routing functionality.
// This file implements the Policy Engine layer for intelligent model selection.
package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/shopspring/decimal"
)

// PolicyType defines the type of routing policy
type PolicyType string

const (
	// PolicyTypeCostOptimized prioritizes low cost
	PolicyTypeCostOptimized PolicyType = "cost_optimized"
	// PolicyTypeLatencyOptimized prioritizes fast responses
	PolicyTypeLatencyOptimized PolicyType = "latency_optimized"
	// PolicyTypeQualityOptimized prioritizes high-quality responses
	PolicyTypeQualityOptimized PolicyType = "quality_optimized"
	// PolicyTypeBalanced balances cost, latency, and quality
	PolicyTypeBalanced PolicyType = "balanced"
	// PolicyTypeFallback provides fallback chains
	PolicyTypeFallback PolicyType = "fallback"
)

// RoutingPolicy defines a configurable policy for model selection
type RoutingPolicy struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Type        PolicyType             `json:"type"`
	Description string                 `json:"description"`
	Constraints RoutingConstraints     `json:"constraints"`
	Weights     PolicyWeights          `json:"weights"`
	Fallback    *FallbackPolicy        `json:"fallback,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
	IsActive    bool                   `json:"is_active"`
}

// PolicyWeights defines the scoring weights for different factors
type PolicyWeights struct {
	CostWeight        float64 `json:"cost_weight"`
	LatencyWeight     float64 `json:"latency_weight"`
	CapabilityWeight  float64 `json:"capability_weight"`
	ReliabilityWeight float64 `json:"reliability_weight"`
}

// DefaultPolicyWeights returns balanced weights
func DefaultPolicyWeights() PolicyWeights {
	return PolicyWeights{
		CostWeight:        0.25,
		LatencyWeight:     0.25,
		CapabilityWeight:  0.25,
		ReliabilityWeight: 0.25,
	}
}

// PolicyWeightsByType returns preset weights for common policy types
func PolicyWeightsByType(policyType PolicyType) PolicyWeights {
	switch policyType {
	case PolicyTypeCostOptimized:
		return PolicyWeights{
			CostWeight:        0.50,
			LatencyWeight:     0.15,
			CapabilityWeight:  0.20,
			ReliabilityWeight: 0.15,
		}
	case PolicyTypeLatencyOptimized:
		return PolicyWeights{
			CostWeight:        0.15,
			LatencyWeight:     0.50,
			CapabilityWeight:  0.20,
			ReliabilityWeight: 0.15,
		}
	case PolicyTypeQualityOptimized:
		return PolicyWeights{
			CostWeight:        0.15,
			LatencyWeight:     0.15,
			CapabilityWeight:  0.40,
			ReliabilityWeight: 0.30,
		}
	default:
		return DefaultPolicyWeights()
	}
}

// FallbackPolicy defines fallback behavior when primary model fails
type FallbackPolicy struct {
	Enabled           bool     `json:"enabled"`
	MaxRetries        int      `json:"max_retries"`
	RetryDelay        string   `json:"retry_delay"`
	AlternativeModels []string `json:"alternative_models"`
	ProviderFailover  bool     `json:"provider_failover"`
}

// PolicyEngine manages routing policies and executes routing decisions
type PolicyEngine struct {
	router   *Router
	policies map[string]*RoutingPolicy
	mu       sync.RWMutex
	metrics  *PolicyMetrics
}

// PolicyMetrics tracks policy execution metrics
type PolicyMetrics struct {
	TotalRequests    int64                      `json:"total_requests"`
	SuccessfulRoutes int64                      `json:"successful_routes"`
	FailedRoutes     int64                      `json:"failed_routes"`
	FallbackTriggers int64                      `json:"fallback_triggers"`
	PolicyUsage      map[string]int64           `json:"policy_usage"`
	LatencyHistogram map[string][]time.Duration `json:"latency_histogram"`
}

// NewPolicyEngine creates a new policy engine
func NewPolicyEngine(router *Router) *PolicyEngine {
	return &PolicyEngine{
		router:   router,
		policies: make(map[string]*RoutingPolicy),
		metrics: &PolicyMetrics{
			PolicyUsage:      make(map[string]int64),
			LatencyHistogram: make(map[string][]time.Duration),
		},
	}
}

// RegisterPolicy registers a new routing policy
func (pe *PolicyEngine) RegisterPolicy(policy *RoutingPolicy) error {
	if policy.ID == "" {
		return fmt.Errorf("policy ID is required")
	}
	if policy.Type == "" {
		return fmt.Errorf("policy type is required")
	}

	// Set default weights if not provided
	if policy.Weights.CostWeight == 0 && policy.Weights.LatencyWeight == 0 {
		policy.Weights = PolicyWeightsByType(policy.Type)
	}

	// Normalize weights to sum to 1.0
	pe.normalizeWeights(&policy.Weights)

	policy.UpdatedAt = time.Now().UTC()
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = policy.UpdatedAt
	}

	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.policies[policy.ID] = policy

	return nil
}

// normalizeWeights ensures weights sum to 1.0
func (pe *PolicyEngine) normalizeWeights(weights *PolicyWeights) {
	total := weights.CostWeight + weights.LatencyWeight + weights.CapabilityWeight + weights.ReliabilityWeight
	if total > 0 {
		weights.CostWeight /= total
		weights.LatencyWeight /= total
		weights.CapabilityWeight /= total
		weights.ReliabilityWeight /= total
	}
}

// GetPolicy retrieves a policy by ID
func (pe *PolicyEngine) GetPolicy(policyID string) (*RoutingPolicy, error) {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	policy, exists := pe.policies[policyID]
	if !exists {
		return nil, fmt.Errorf("policy %s not found", policyID)
	}

	return policy, nil
}

// ListPolicies returns all registered policies
func (pe *PolicyEngine) ListPolicies() []*RoutingPolicy {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	policies := make([]*RoutingPolicy, 0, len(pe.policies))
	for _, policy := range pe.policies {
		policies = append(policies, policy)
	}

	return policies
}

// DeletePolicy removes a policy
func (pe *PolicyEngine) DeletePolicy(policyID string) error {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	if _, exists := pe.policies[policyID]; !exists {
		return fmt.Errorf("policy %s not found", policyID)
	}

	delete(pe.policies, policyID)
	return nil
}

// RouteWithPolicy routes using a specific policy
func (pe *PolicyEngine) RouteWithPolicy(ctx context.Context, policyID string) (*RoutingResult, error) {
	start := time.Now()

	policy, err := pe.GetPolicy(policyID)
	if err != nil {
		pe.recordFailure()
		return nil, err
	}

	// Update constraints with policy weights
	constraints := policy.Constraints
	result, err := pe.router.Route(ctx, constraints)

	duration := time.Since(start)
	pe.recordMetrics(policyID, result, err, duration)

	if err != nil {
		// Check if fallback is enabled
		if policy.Fallback != nil && policy.Fallback.Enabled {
			return pe.executeFallback(ctx, policy, constraints)
		}
		return nil, err
	}

	return result, nil
}

// RouteWithStrategy routes using a strategy without a predefined policy
func (pe *PolicyEngine) RouteWithStrategy(
	ctx context.Context,
	strategy PolicyType,
	baseConstraints RoutingConstraints,
) (*RoutingResult, error) {
	_ = PolicyWeightsByType(strategy)
	return pe.router.Route(ctx, baseConstraints)
}

// executeFallback attempts fallback routing when primary fails
func (pe *PolicyEngine) executeFallback(
	ctx context.Context,
	policy *RoutingPolicy,
	originalConstraints RoutingConstraints,
) (*RoutingResult, error) {
	if policy.Fallback == nil || !policy.Fallback.Enabled {
		return nil, fmt.Errorf("fallback not available")
	}

	for range policy.Fallback.AlternativeModels {
		result, err := pe.router.Route(ctx, originalConstraints)
		if err == nil {
			pe.metrics.FallbackTriggers++
			return result, nil
		}
	}

	return nil, fmt.Errorf("fallback routing failed")
}

// recordMetrics updates execution metrics
func (pe *PolicyEngine) recordMetrics(policyID string, result *RoutingResult, err error, duration time.Duration) {
	pe.mu.Lock()
	defer pe.mu.Unlock()

	pe.metrics.TotalRequests++
	pe.metrics.PolicyUsage[policyID]++

	if err != nil {
		pe.metrics.FailedRoutes++
	} else {
		pe.metrics.SuccessfulRoutes++
	}

	// Record latency
	if pe.metrics.LatencyHistogram[policyID] == nil {
		pe.metrics.LatencyHistogram[policyID] = make([]time.Duration, 0)
	}
	pe.metrics.LatencyHistogram[policyID] = append(pe.metrics.LatencyHistogram[policyID], duration)
}

// recordFailure increments failure counter
func (pe *PolicyEngine) recordFailure() {
	pe.mu.Lock()
	defer pe.mu.Unlock()
	pe.metrics.FailedRoutes++
}

// GetMetrics returns current policy metrics
func (pe *PolicyEngine) GetMetrics() PolicyMetrics {
	pe.mu.RLock()
	defer pe.mu.RUnlock()

	// Return a copy
	metrics := *pe.metrics
	metrics.PolicyUsage = make(map[string]int64)
	for k, v := range pe.metrics.PolicyUsage {
		metrics.PolicyUsage[k] = v
	}

	return metrics
}

// PresetPolicies returns predefined policy templates
func PresetPolicies() []*RoutingPolicy {
	return []*RoutingPolicy{
		{
			ID:          "cost-optimized-default",
			Name:        "Cost Optimized",
			Type:        PolicyTypeCostOptimized,
			Description: "Prioritizes cost efficiency for budget-conscious workloads",
			Weights:     PolicyWeightsByType(PolicyTypeCostOptimized),
			Constraints: RoutingConstraints{
				RequiredCaps: ModelCapability{SupportsTools: true},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			IsActive:  true,
		},
		{
			ID:          "latency-optimized-default",
			Name:        "Latency Optimized",
			Type:        PolicyTypeLatencyOptimized,
			Description: "Prioritizes fast response times for real-time applications",
			Weights:     PolicyWeightsByType(PolicyTypeLatencyOptimized),
			Constraints: RoutingConstraints{
				LatencyPreference: "fast",
				RequiredCaps:      ModelCapability{SupportsTools: true},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			IsActive:  true,
		},
		{
			ID:          "quality-optimized-default",
			Name:        "Quality Optimized",
			Type:        PolicyTypeQualityOptimized,
			Description: "Prioritizes model capabilities and reasoning quality",
			Weights:     PolicyWeightsByType(PolicyTypeQualityOptimized),
			Constraints: RoutingConstraints{
				LatencyPreference: "accurate",
				RequiredCaps:      ModelCapability{SupportsTools: true, SupportsReasoning: true},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			IsActive:  true,
		},
		{
			ID:          "balanced-default",
			Name:        "Balanced",
			Type:        PolicyTypeBalanced,
			Description: "Balanced approach considering cost, latency, and quality",
			Weights:     PolicyWeightsByType(PolicyTypeBalanced),
			Constraints: RoutingConstraints{
				LatencyPreference: "balanced",
				RequiredCaps:      ModelCapability{SupportsTools: true},
			},
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
			IsActive:  true,
		},
	}
}

// ValidatePolicy validates a routing policy configuration
func ValidatePolicy(policy *RoutingPolicy) error {
	if policy.ID == "" {
		return fmt.Errorf("policy ID is required")
	}
	if policy.Name == "" {
		return fmt.Errorf("policy name is required")
	}
	if policy.Type == "" {
		return fmt.Errorf("policy type is required")
	}

	// Validate weights sum to approximately 1.0
	weights := policy.Weights
	total := weights.CostWeight + weights.LatencyWeight + weights.CapabilityWeight + weights.ReliabilityWeight
	if total > 0 && (total < 0.99 || total > 1.01) {
		return fmt.Errorf("policy weights should sum to 1.0, got %f", total)
	}

	// Validate fallback configuration
	if policy.Fallback != nil && policy.Fallback.Enabled {
		if policy.Fallback.MaxRetries < 0 {
			return fmt.Errorf("fallback max_retries must be non-negative")
		}
		if _, err := time.ParseDuration(policy.Fallback.RetryDelay); err != nil && policy.Fallback.RetryDelay != "" {
			return fmt.Errorf("invalid fallback retry_delay: %w", err)
		}
	}

	return nil
}

// MarshalJSON custom serialization for decimal.Decimal
func (pm PolicyMetrics) MarshalJSON() ([]byte, error) {
	type Alias PolicyMetrics
	return json.Marshal(&struct {
		*Alias
	}{
		Alias: (*Alias)(&pm),
	})
}

// CostEstimate estimates the cost for a policy execution
func (pe *PolicyEngine) CostEstimate(ctx context.Context, policyID string, inputTokens, outputTokens int) (decimal.Decimal, error) {
	policy, err := pe.GetPolicy(policyID)
	if err != nil {
		return decimal.Zero, err
	}

	// Get the best model for this policy
	result, err := pe.router.Route(ctx, policy.Constraints)
	if err != nil {
		return decimal.Zero, err
	}

	return CalculateCost(result.Model, inputTokens, outputTokens), nil
}
