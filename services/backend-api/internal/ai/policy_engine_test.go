package ai

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestPolicyEngine(t *testing.T) (*PolicyEngine, *Registry, *miniredis.Miniredis) {
	registry, s := setupTestRegistry(t)
	router := NewRouter(registry)
	engine := NewPolicyEngine(router)

	return engine, registry, s
}

func TestNewPolicyEngine(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	engine := NewPolicyEngine(router)

	assert.NotNil(t, engine)
	assert.NotNil(t, engine.router)
	assert.NotNil(t, engine.policies)
	assert.NotNil(t, engine.metrics)
}

func TestPolicyEngine_RegisterPolicy(t *testing.T) {
	engine, _, _ := setupTestPolicyEngine(t)

	t.Run("register valid policy", func(t *testing.T) {
		policy := &RoutingPolicy{
			ID:   "test-policy-1",
			Name: "Test Policy",
			Type: PolicyTypeBalanced,
			Constraints: RoutingConstraints{
				RequiredCaps: ModelCapability{SupportsTools: true},
			},
			IsActive: true,
		}

		err := engine.RegisterPolicy(policy)
		require.NoError(t, err)

		retrieved, err := engine.GetPolicy("test-policy-1")
		require.NoError(t, err)
		assert.Equal(t, "Test Policy", retrieved.Name)
		assert.Equal(t, PolicyTypeBalanced, retrieved.Type)
	})

	t.Run("register policy without ID", func(t *testing.T) {
		policy := &RoutingPolicy{
			Name: "No ID Policy",
			Type: PolicyTypeBalanced,
		}

		err := engine.RegisterPolicy(policy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "policy ID is required")
	})

	t.Run("register policy without type", func(t *testing.T) {
		policy := &RoutingPolicy{
			ID:   "no-type-policy",
			Name: "No Type Policy",
		}

		err := engine.RegisterPolicy(policy)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "policy type is required")
	})

	t.Run("register duplicate policy updates", func(t *testing.T) {
		policy := &RoutingPolicy{
			ID:   "duplicate-policy",
			Name: "Original",
			Type: PolicyTypeBalanced,
		}

		err := engine.RegisterPolicy(policy)
		require.NoError(t, err)

		policy.Name = "Updated"
		err = engine.RegisterPolicy(policy)
		require.NoError(t, err)

		retrieved, err := engine.GetPolicy("duplicate-policy")
		require.NoError(t, err)
		assert.Equal(t, "Updated", retrieved.Name)
	})
}

func TestPolicyEngine_GetPolicy(t *testing.T) {
	engine, _, _ := setupTestPolicyEngine(t)

	policy := &RoutingPolicy{
		ID:   "get-test",
		Name: "Get Test",
		Type: PolicyTypeBalanced,
	}

	err := engine.RegisterPolicy(policy)
	require.NoError(t, err)

	t.Run("get existing policy", func(t *testing.T) {
		retrieved, err := engine.GetPolicy("get-test")
		require.NoError(t, err)
		assert.Equal(t, "Get Test", retrieved.Name)
	})

	t.Run("get non-existent policy", func(t *testing.T) {
		_, err := engine.GetPolicy("does-not-exist")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestPolicyEngine_ListPolicies(t *testing.T) {
	engine, _, _ := setupTestPolicyEngine(t)

	t.Run("list empty policies", func(t *testing.T) {
		policies := engine.ListPolicies()
		assert.Empty(t, policies)
	})

	t.Run("list multiple policies", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			policy := &RoutingPolicy{
				ID:   "list-policy-" + string(rune('a'+i)),
				Name: "Policy " + string(rune('A'+i)),
				Type: PolicyTypeBalanced,
			}
			err := engine.RegisterPolicy(policy)
			require.NoError(t, err)
		}

		policies := engine.ListPolicies()
		assert.Len(t, policies, 3)
	})
}

func TestPolicyEngine_DeletePolicy(t *testing.T) {
	engine, _, _ := setupTestPolicyEngine(t)

	policy := &RoutingPolicy{
		ID:   "delete-test",
		Name: "Delete Test",
		Type: PolicyTypeBalanced,
	}

	err := engine.RegisterPolicy(policy)
	require.NoError(t, err)

	t.Run("delete existing policy", func(t *testing.T) {
		err := engine.DeletePolicy("delete-test")
		require.NoError(t, err)

		_, err = engine.GetPolicy("delete-test")
		assert.Error(t, err)
	})

	t.Run("delete non-existent policy", func(t *testing.T) {
		err := engine.DeletePolicy("does-not-exist")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestPolicyEngine_RouteWithPolicy(t *testing.T) {
	engine, _, _ := setupTestPolicyEngine(t)
	ctx := context.Background()

	t.Run("route with valid policy", func(t *testing.T) {
		policy := &RoutingPolicy{
			ID:   "route-test",
			Name: "Route Test",
			Type: PolicyTypeBalanced,
			Constraints: RoutingConstraints{
				RequiredCaps: ModelCapability{SupportsTools: true},
			},
		}

		err := engine.RegisterPolicy(policy)
		require.NoError(t, err)

		result, err := engine.RouteWithPolicy(ctx, "route-test")
		require.NoError(t, err)
		assert.NotEmpty(t, result.Model.ModelID)
		assert.True(t, result.Score > 0)
	})

	t.Run("route with non-existent policy", func(t *testing.T) {
		_, err := engine.RouteWithPolicy(ctx, "does-not-exist")
		assert.Error(t, err)
	})
}

func TestPolicyEngine_RouteWithStrategy(t *testing.T) {
	engine, _, _ := setupTestPolicyEngine(t)
	ctx := context.Background()

	tests := []struct {
		name     string
		strategy PolicyType
		wantErr  bool
	}{
		{
			name:     "cost optimized strategy",
			strategy: PolicyTypeCostOptimized,
			wantErr:  false,
		},
		{
			name:     "latency optimized strategy",
			strategy: PolicyTypeLatencyOptimized,
			wantErr:  false,
		},
		{
			name:     "quality optimized strategy",
			strategy: PolicyTypeQualityOptimized,
			wantErr:  false,
		},
		{
			name:     "balanced strategy",
			strategy: PolicyTypeBalanced,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := engine.RouteWithStrategy(ctx, tt.strategy, RoutingConstraints{
				RequiredCaps: ModelCapability{SupportsTools: true},
			})
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, result.Model.ModelID)
			}
		})
	}
}

func TestPolicyEngine_Metrics(t *testing.T) {
	engine, _, _ := setupTestPolicyEngine(t)
	ctx := context.Background()

	policy := &RoutingPolicy{
		ID:   "metrics-test",
		Name: "Metrics Test",
		Type: PolicyTypeBalanced,
		Constraints: RoutingConstraints{
			RequiredCaps: ModelCapability{SupportsTools: true},
		},
	}

	err := engine.RegisterPolicy(policy)
	require.NoError(t, err)

	t.Run("metrics are recorded", func(t *testing.T) {
		engine.metrics = &PolicyMetrics{
			PolicyUsage:      make(map[string]int64),
			LatencyHistogram: make(map[string][]time.Duration),
		}

		_, err := engine.RouteWithPolicy(ctx, "metrics-test")
		require.NoError(t, err)

		metrics := engine.GetMetrics()
		assert.Equal(t, int64(1), metrics.TotalRequests)
		assert.Equal(t, int64(1), metrics.SuccessfulRoutes)
		assert.Equal(t, int64(0), metrics.FailedRoutes)
		assert.Equal(t, int64(1), metrics.PolicyUsage["metrics-test"])
	})
}

func TestPolicyWeightsByType(t *testing.T) {
	tests := []struct {
		name         string
		policyType   PolicyType
		expectedCost float64
	}{
		{
			name:         "cost optimized weights",
			policyType:   PolicyTypeCostOptimized,
			expectedCost: 0.50,
		},
		{
			name:         "latency optimized weights",
			policyType:   PolicyTypeLatencyOptimized,
			expectedCost: 0.15,
		},
		{
			name:         "quality optimized weights",
			policyType:   PolicyTypeQualityOptimized,
			expectedCost: 0.15,
		},
		{
			name:         "balanced weights",
			policyType:   PolicyTypeBalanced,
			expectedCost: 0.25,
		},
		{
			name:         "unknown type defaults to balanced",
			policyType:   PolicyType("unknown"),
			expectedCost: 0.25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			weights := PolicyWeightsByType(tt.policyType)
			assert.InDelta(t, tt.expectedCost, weights.CostWeight, 0.01)
		})
	}
}

func TestPresetPolicies(t *testing.T) {
	policies := PresetPolicies()

	assert.Len(t, policies, 4)

	policyMap := make(map[string]*RoutingPolicy)
	for _, p := range policies {
		policyMap[p.ID] = p
	}

	assert.Contains(t, policyMap, "cost-optimized-default")
	assert.Contains(t, policyMap, "latency-optimized-default")
	assert.Contains(t, policyMap, "quality-optimized-default")
	assert.Contains(t, policyMap, "balanced-default")

	costPolicy := policyMap["cost-optimized-default"]
	assert.Equal(t, PolicyTypeCostOptimized, costPolicy.Type)
	assert.True(t, costPolicy.IsActive)
	assert.True(t, costPolicy.Weights.CostWeight > costPolicy.Weights.LatencyWeight)
}

func TestValidatePolicy(t *testing.T) {
	tests := []struct {
		name    string
		policy  *RoutingPolicy
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid policy",
			policy: &RoutingPolicy{
				ID:   "valid",
				Name: "Valid",
				Type: PolicyTypeBalanced,
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			policy: &RoutingPolicy{
				Name: "No ID",
				Type: PolicyTypeBalanced,
			},
			wantErr: true,
			errMsg:  "policy ID is required",
		},
		{
			name: "missing name",
			policy: &RoutingPolicy{
				ID:   "no-name",
				Type: PolicyTypeBalanced,
			},
			wantErr: true,
			errMsg:  "policy name is required",
		},
		{
			name: "missing type",
			policy: &RoutingPolicy{
				ID:   "no-type",
				Name: "No Type",
			},
			wantErr: true,
			errMsg:  "policy type is required",
		},
		{
			name: "invalid weight sum",
			policy: &RoutingPolicy{
				ID:   "bad-weights",
				Name: "Bad Weights",
				Type: PolicyTypeBalanced,
				Weights: PolicyWeights{
					CostWeight:        0.8,
					LatencyWeight:     0.8,
					CapabilityWeight:  0.0,
					ReliabilityWeight: 0.0,
				},
			},
			wantErr: true,
			errMsg:  "weights should sum to 1.0",
		},
		{
			name: "invalid fallback retry delay",
			policy: &RoutingPolicy{
				ID:   "bad-fallback",
				Name: "Bad Fallback",
				Type: PolicyTypeBalanced,
				Fallback: &FallbackPolicy{
					Enabled:    true,
					RetryDelay: "invalid",
				},
			},
			wantErr: true,
			errMsg:  "invalid fallback retry_delay",
		},
		{
			name: "negative fallback retries",
			policy: &RoutingPolicy{
				ID:   "neg-retries",
				Name: "Negative Retries",
				Type: PolicyTypeBalanced,
				Fallback: &FallbackPolicy{
					Enabled:    true,
					MaxRetries: -1,
				},
			},
			wantErr: true,
			errMsg:  "max_retries must be non-negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePolicy(tt.policy)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestPolicyEngine_CostEstimate(t *testing.T) {
	engine, _, _ := setupTestPolicyEngine(t)
	ctx := context.Background()

	policy := &RoutingPolicy{
		ID:   "cost-estimate-test",
		Name: "Cost Estimate Test",
		Type: PolicyTypeBalanced,
		Constraints: RoutingConstraints{
			RequiredCaps: ModelCapability{SupportsTools: true},
		},
	}

	err := engine.RegisterPolicy(policy)
	require.NoError(t, err)

	t.Run("estimate cost for valid policy", func(t *testing.T) {
		cost, err := engine.CostEstimate(ctx, "cost-estimate-test", 1000, 500)
		require.NoError(t, err)
		assert.True(t, cost.GreaterThan(decimal.Zero))
	})

	t.Run("estimate cost for non-existent policy", func(t *testing.T) {
		_, err := engine.CostEstimate(ctx, "does-not-exist", 1000, 500)
		assert.Error(t, err)
	})
}

func TestDefaultPolicyWeights(t *testing.T) {
	weights := DefaultPolicyWeights()

	total := weights.CostWeight + weights.LatencyWeight + weights.CapabilityWeight + weights.ReliabilityWeight
	assert.InDelta(t, 1.0, total, 0.01)

	assert.Equal(t, 0.25, weights.CostWeight)
	assert.Equal(t, 0.25, weights.LatencyWeight)
	assert.Equal(t, 0.25, weights.CapabilityWeight)
	assert.Equal(t, 0.25, weights.ReliabilityWeight)
}
