package ai

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRegistry(t *testing.T) (*Registry, *miniredis.Miniredis) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})

	registry := NewRegistry(WithRedis(client))

	// Pre-populate with test data
	testRegistry := &ModelRegistry{
		Models: []ModelInfo{
			{
				ProviderID:    "openai",
				ProviderLabel: "OpenAI",
				ModelID:       "gpt-4",
				DisplayName:   "GPT-4",
				Capabilities:  ModelCapability{SupportsTools: true, SupportsReasoning: true},
				Cost:          ModelCost{InputCost: decimal.NewFromFloat(30.0), OutputCost: decimal.NewFromFloat(60.0)},
				Limits:        ModelLimits{ContextLimit: 8192, OutputLimit: 4096},
				Status:        "active",
				LatencyClass:  "medium",
				RiskLevel:     "low",
			},
			{
				ProviderID:    "openai",
				ProviderLabel: "OpenAI",
				ModelID:       "gpt-3.5-turbo",
				DisplayName:   "GPT-3.5 Turbo",
				Capabilities:  ModelCapability{SupportsTools: true},
				Cost:          ModelCost{InputCost: decimal.NewFromFloat(0.5), OutputCost: decimal.NewFromFloat(1.5)},
				Limits:        ModelLimits{ContextLimit: 16384, OutputLimit: 4096},
				Status:        "active",
				LatencyClass:  "fast",
				RiskLevel:     "low",
			},
			{
				ProviderID:    "anthropic",
				ProviderLabel: "Anthropic",
				ModelID:       "claude-3-opus",
				DisplayName:   "Claude 3 Opus",
				Capabilities:  ModelCapability{SupportsTools: true, SupportsVision: true, SupportsReasoning: true},
				Cost:          ModelCost{InputCost: decimal.NewFromFloat(15.0), OutputCost: decimal.NewFromFloat(75.0)},
				Limits:        ModelLimits{ContextLimit: 200000, OutputLimit: 4096},
				Status:        "active",
				LatencyClass:  "slow",
				RiskLevel:     "medium",
			},
			{
				ProviderID:    "anthropic",
				ProviderLabel: "Anthropic",
				ModelID:       "claude-3-sonnet",
				DisplayName:   "Claude 3 Sonnet",
				Capabilities:  ModelCapability{SupportsTools: true, SupportsVision: true},
				Cost:          ModelCost{InputCost: decimal.NewFromFloat(3.0), OutputCost: decimal.NewFromFloat(15.0)},
				Limits:        ModelLimits{ContextLimit: 200000, OutputLimit: 4096},
				Status:        "active",
				LatencyClass:  "medium",
				RiskLevel:     "low",
			},
			{
				ProviderID:   "google",
				ModelID:      "gemini-pro",
				DisplayName:  "Gemini Pro",
				Capabilities: ModelCapability{SupportsTools: true},
				Cost:         ModelCost{InputCost: decimal.NewFromFloat(0.0), OutputCost: decimal.NewFromFloat(0.0)},
				Limits:       ModelLimits{ContextLimit: 1000000, OutputLimit: 2048},
				Status:       "degraded",
				LatencyClass: "fast",
				RiskLevel:    "high",
			},
		},
		Providers: []ProviderInfo{
			{ID: "openai", Name: "OpenAI"},
			{ID: "anthropic", Name: "Anthropic"},
			{ID: "google", Name: "Google"},
		},
	}

	err := registry.cacheToRedis(context.Background(), testRegistry)
	require.NoError(t, err)

	return registry, s
}

func TestNewRouter(t *testing.T) {
	registry := NewRegistry()
	router := NewRouter(registry)
	assert.NotNil(t, router)
	assert.Equal(t, registry, router.registry)
}

func TestRouterRoute(t *testing.T) {
	registry, _ := setupTestRegistry(t)
	router := NewRouter(registry)
	ctx := context.Background()

	t.Run("route with tools capability", func(t *testing.T) {
		result, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps: ModelCapability{SupportsTools: true},
		})
		require.NoError(t, err)
		assert.NotEmpty(t, result.Model.ModelID)
		assert.True(t, result.Score > 0)
	})

	t.Run("route with latency preference fast", func(t *testing.T) {
		result, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps:      ModelCapability{SupportsTools: true},
			LatencyPreference: "fast",
		})
		require.NoError(t, err)
		assert.NotEmpty(t, result.Model.ModelID)
		assert.True(t, result.Score > 0)
	})

	t.Run("route with latency preference accurate", func(t *testing.T) {
		result, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps:      ModelCapability{SupportsTools: true},
			LatencyPreference: "accurate",
		})
		require.NoError(t, err)
		// Claude 3 Opus is slow but accurate
		assert.Equal(t, "claude-3-opus", result.Model.ModelID)
	})

	t.Run("route with budget constraints", func(t *testing.T) {
		result, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps:  ModelCapability{SupportsTools: true},
			MaxInputCost:  decimal.NewFromFloat(1.0),
			MaxOutputCost: decimal.NewFromFloat(2.0),
		})
		require.NoError(t, err)
		// Only GPT-3.5 fits budget
		assert.Equal(t, "gpt-3.5-turbo", result.Model.ModelID)
	})

	t.Run("route with provider whitelist", func(t *testing.T) {
		result, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps:     ModelCapability{SupportsTools: true},
			AllowedProviders: []string{"anthropic"},
		})
		require.NoError(t, err)
		assert.Equal(t, "anthropic", result.Model.ProviderID)
	})

	t.Run("route with provider blacklist", func(t *testing.T) {
		result, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps:     ModelCapability{SupportsTools: true},
			BlockedProviders: []string{"openai"},
		})
		require.NoError(t, err)
		assert.Equal(t, "anthropic", result.Model.ProviderID)
	})

	t.Run("route with context token requirements", func(t *testing.T) {
		result, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps:     ModelCapability{SupportsTools: true},
			MinContextTokens: 100000,
		})
		require.NoError(t, err)
		// Claude models have large context
		assert.Equal(t, "anthropic", result.Model.ProviderID)
	})

	t.Run("route with risk level constraint", func(t *testing.T) {
		result, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps: ModelCapability{SupportsTools: true},
			RiskLevel:    "low",
		})
		require.NoError(t, err)
		assert.Equal(t, "low", result.Model.RiskLevel)
	})

	t.Run("find model with all capabilities", func(t *testing.T) {
		result, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps: ModelCapability{SupportsReasoning: true, SupportsVision: true, SupportsTools: true},
		})
		require.NoError(t, err)
		assert.Equal(t, "claude-3-opus", result.Model.ModelID)
	})

	t.Run("exceeds budget constraints", func(t *testing.T) {
		_, err := router.Route(ctx, RoutingConstraints{
			RequiredCaps:  ModelCapability{SupportsTools: true},
			MaxInputCost:  decimal.NewFromFloat(0.1),
			MaxOutputCost: decimal.NewFromFloat(0.1),
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no models match")
	})
}

func TestMatchesConstraints(t *testing.T) {
	router := NewRouter(NewRegistry())

	model := ModelInfo{
		ProviderID: "openai",
		ModelID:    "gpt-4",
		Status:     "active",
		Cost:       ModelCost{InputCost: decimal.NewFromFloat(30.0), OutputCost: decimal.NewFromFloat(60.0)},
		Limits:     ModelLimits{ContextLimit: 8192},
		RiskLevel:  "low",
	}

	tests := []struct {
		name        string
		constraints RoutingConstraints
		want        bool
	}{
		{
			name:        "no constraints matches",
			constraints: RoutingConstraints{},
			want:        true,
		},
		{
			name: "active status matches",
			constraints: RoutingConstraints{
				AllowedProviders: []string{"openai"},
			},
			want: true,
		},
		{
			name: "blocked provider excluded",
			constraints: RoutingConstraints{
				BlockedProviders: []string{"openai"},
			},
			want: false,
		},
		{
			name: "cost within budget",
			constraints: RoutingConstraints{
				MaxInputCost:  decimal.NewFromFloat(50.0),
				MaxOutputCost: decimal.NewFromFloat(100.0),
			},
			want: true,
		},
		{
			name: "cost exceeds budget",
			constraints: RoutingConstraints{
				MaxInputCost:  decimal.NewFromFloat(10.0),
				MaxOutputCost: decimal.NewFromFloat(100.0),
			},
			want: false,
		},
		{
			name: "context meets requirements",
			constraints: RoutingConstraints{
				MinContextTokens: 4096,
			},
			want: true,
		},
		{
			name: "context below requirements",
			constraints: RoutingConstraints{
				MinContextTokens: 10000,
			},
			want: false,
		},
		{
			name: "risk level within limit",
			constraints: RoutingConstraints{
				RiskLevel: "medium",
			},
			want: true,
		},
		{
			name: "risk level exceeded",
			constraints: RoutingConstraints{
				RiskLevel: "low",
			},
			want: true, // low is within low
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := router.matchesConstraints(model, tt.constraints)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestScoreLatency(t *testing.T) {
	router := NewRouter(NewRegistry())

	tests := []struct {
		model      ModelInfo
		preference string
		minScore   float64
	}{
		{ModelInfo{LatencyClass: "fast"}, "fast", 90.0},
		{ModelInfo{LatencyClass: "medium"}, "balanced", 90.0},
		{ModelInfo{LatencyClass: "slow"}, "accurate", 90.0},
		{ModelInfo{LatencyClass: "fast"}, "balanced", 70.0},
		{ModelInfo{LatencyClass: "slow"}, "fast", 30.0},
	}

	for _, tt := range tests {
		score := router.scoreLatency(tt.model, tt.preference)
		assert.GreaterOrEqual(t, score, tt.minScore,
			"latency class %s with preference %s should score at least %.0f",
			tt.model.LatencyClass, tt.preference, tt.minScore)
	}
}

func TestScoreCapabilities(t *testing.T) {
	router := NewRouter(NewRegistry())

	tests := []struct {
		caps ModelCapability
		want float64
	}{
		{ModelCapability{}, 0.0},
		{ModelCapability{SupportsTools: true}, 25.0},
		{ModelCapability{SupportsTools: true, SupportsVision: true}, 50.0},
		{ModelCapability{SupportsTools: true, SupportsVision: true, SupportsReasoning: true}, 75.0},
	}

	for _, tt := range tests {
		model := ModelInfo{Capabilities: tt.caps, StructuredOutput: false}
		got := router.scoreCapabilities(model)
		assert.Equal(t, tt.want, got)
	}

	// Test with structured output
	model := ModelInfo{
		Capabilities:     ModelCapability{SupportsTools: true},
		StructuredOutput: true,
	}
	assert.Equal(t, 50.0, router.scoreCapabilities(model))
}

func TestCalculateCost(t *testing.T) {
	model := ModelInfo{
		Cost: ModelCost{
			InputCost:  decimal.NewFromFloat(10.0),
			OutputCost: decimal.NewFromFloat(30.0),
		},
	}

	tests := []struct {
		inputTokens  int
		outputTokens int
		want         decimal.Decimal
	}{
		{1000000, 0, decimal.NewFromFloat(10.0)},
		{0, 1000000, decimal.NewFromFloat(30.0)},
		{1000000, 1000000, decimal.NewFromFloat(40.0)},
		{500000, 500000, decimal.NewFromFloat(20.0)},
	}

	for _, tt := range tests {
		got := CalculateCost(model, tt.inputTokens, tt.outputTokens)
		assert.True(t, tt.want.Equal(got), "expected %s, got %s", tt.want.String(), got.String())
	}
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		text string
		min  int
	}{
		{"", 0},
		{"test", 1},
		{"This is a test sentence.", 6},
		{"a", 1},
		{"abcd", 1},
	}

	for _, tt := range tests {
		got := EstimateTokens(tt.text)
		assert.GreaterOrEqual(t, got, tt.min)
	}
}

func TestJoinReasons(t *testing.T) {
	tests := []struct {
		reasons []string
		want    string
	}{
		{[]string{}, ""},
		{[]string{"reason1"}, "reason1"},
		{[]string{"reason1", "reason2"}, "reason1, reason2"},
		{[]string{"reason1", "reason2", "reason3"}, "reason1, reason2, reason3"},
	}

	for _, tt := range tests {
		got := joinReasons(tt.reasons)
		assert.Equal(t, tt.want, got)
	}
}

func TestRiskScore(t *testing.T) {
	tests := []struct {
		level string
		want  int
	}{
		{"low", 1},
		{"medium", 2},
		{"high", 3},
		{"", 2},
		{"unknown", 2},
	}

	for _, tt := range tests {
		got := riskScore(tt.level)
		assert.Equal(t, tt.want, got)
	}
}
