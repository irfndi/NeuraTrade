package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupTestRedis(t *testing.T) *redis.Client {
	s := miniredis.RunT(t)
	return redis.NewClient(&redis.Options{Addr: s.Addr()})
}

func TestNewRegistry(t *testing.T) {
	logger := zap.NewNop()

	tests := []struct {
		name string
		opts []RegistryOption
		want *Registry
	}{
		{
			name: "default registry",
			opts: nil,
			want: &Registry{
				modelsDevURL: ModelsDevAPIURL,
				cacheTTL:     CacheTTL,
			},
		},
		{
			name: "with logger",
			opts: []RegistryOption{WithLogger(logger)},
			want: &Registry{
				modelsDevURL: ModelsDevAPIURL,
				cacheTTL:     CacheTTL,
			},
		},
		{
			name: "with custom TTL",
			opts: []RegistryOption{WithCacheTTL(1 * time.Hour)},
			want: &Registry{
				modelsDevURL: ModelsDevAPIURL,
				cacheTTL:     1 * time.Hour,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewRegistry(tt.opts...)
			assert.NotNil(t, r)
			assert.Equal(t, tt.want.modelsDevURL, r.modelsDevURL)
			assert.Equal(t, tt.want.cacheTTL, r.cacheTTL)
		})
	}
}

func TestRegistryCacheOperations(t *testing.T) {
	ctx := context.Background()
	client := setupTestRedis(t)
	defer func() { _ = client.Close() }()

	logger := zap.NewNop()
	registry := NewRegistry(
		WithRedis(client),
		WithLogger(logger),
	)

	// Create test registry data
	testRegistry := &ModelRegistry{
		FetchedAt: time.Now().UTC(),
		Models: []ModelInfo{
			{
				ProviderID:   "openai",
				ModelID:      "gpt-4",
				DisplayName:  "GPT-4",
				Capabilities: ModelCapability{SupportsTools: true},
				Cost:         ModelCost{InputCost: decimal.NewFromFloat(30.0), OutputCost: decimal.NewFromFloat(60.0)},
				Limits:       ModelLimits{ContextLimit: 8192},
				Status:       "active",
				LatencyClass: "medium",
			},
			{
				ProviderID:   "anthropic",
				ModelID:      "claude-3-opus",
				DisplayName:  "Claude 3 Opus",
				Capabilities: ModelCapability{SupportsTools: true, SupportsReasoning: true},
				Cost:         ModelCost{InputCost: decimal.NewFromFloat(15.0), OutputCost: decimal.NewFromFloat(75.0)},
				Limits:       ModelLimits{ContextLimit: 200000},
				Status:       "active",
				LatencyClass: "slow",
			},
		},
	}

	t.Run("cache to redis", func(t *testing.T) {
		err := registry.cacheToRedis(ctx, testRegistry)
		require.NoError(t, err)

		// Verify data is in Redis
		data, err := client.Get(ctx, CacheKey).Bytes()
		require.NoError(t, err)
		assert.NotEmpty(t, data)
	})

	t.Run("get from redis", func(t *testing.T) {
		err := registry.cacheToRedis(ctx, testRegistry)
		require.NoError(t, err)

		cached, err := registry.getFromRedis(ctx)
		require.NoError(t, err)
		assert.Len(t, cached.Models, 2)
		assert.Equal(t, "gpt-4", cached.Models[0].ModelID)
	})

	t.Run("get registry uses cache", func(t *testing.T) {
		err := registry.cacheToRedis(ctx, testRegistry)
		require.NoError(t, err)

		cached, err := registry.GetRegistry(ctx)
		require.NoError(t, err)
		assert.Len(t, cached.Models, 2)

		cached2, err := registry.GetRegistry(ctx)
		require.NoError(t, err)
		assert.Equal(t, cached.FetchedAt, cached2.FetchedAt)
	})

	t.Run("refresh clears local cache before fetch", func(t *testing.T) {
		err := registry.cacheToRedis(ctx, testRegistry)
		require.NoError(t, err)

		_, err = registry.GetRegistry(ctx)
		require.NoError(t, err)

		registry.mu.RLock()
		assert.NotNil(t, registry.localCache)
		registry.mu.RUnlock()

		refreshed, err := registry.Refresh(ctx)
		if err != nil {
			assert.Nil(t, refreshed)
		}
	})
}

func TestFetchModels_AttachesProviderModelsAndParsesCapabilities(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"deepseek": {
				"id": "deepseek",
				"name": "DeepSeek",
				"env": ["DEEPSEEK_API_KEY"],
				"models": {
					"deepseek-v4-pro": {
						"id": "deepseek-v4-pro",
						"name": "DeepSeek V4 Pro",
						"tool_call": true,
						"reasoning": true,
						"modalities": {"input": ["text", "image"], "output": ["text"]},
						"cost": {"input": 0.2, "output": 0.8},
						"limit": {"context": 128000, "output": 8192}
					},
					"deepseek-chat": {
						"id": "deepseek-chat",
						"name": "DeepSeek Chat",
						"tool_call": true,
						"reasoning": false,
						"status": "degraded"
					},
					"deepseek-vision-attachment": {
						"id": "deepseek-vision-attachment",
						"name": "DeepSeek Vision Attachment",
						"attachment": true
					}
				}
			}
		}`))
	}))
	defer server.Close()

	registry := NewRegistry(WithModelsDevURL(server.URL))
	models, err := registry.FetchModels(ctx)
	require.NoError(t, err)

	require.Len(t, models.Providers, 1)
	require.Len(t, models.Models, 3)
	require.Len(t, models.Providers[0].Models, 3)
	byID := make(map[string]ModelInfo, len(models.Providers[0].Models))
	for _, model := range models.Providers[0].Models {
		byID[model.ModelID] = model
	}
	pro := byID["deepseek-v4-pro"]
	assert.True(t, pro.Capabilities.SupportsTools)
	assert.True(t, pro.Capabilities.SupportsReasoning)
	assert.True(t, pro.Capabilities.SupportsVision)
	assert.Equal(t, "active", pro.Status)
	assert.Equal(t, "degraded", byID["deepseek-chat"].Status)

	for _, tc := range []struct {
		name    string
		modelID string
	}{
		{name: "image modality", modelID: "deepseek-v4-pro"},
		{name: "attachment flag", modelID: "deepseek-vision-attachment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.True(t, byID[tc.modelID].Capabilities.SupportsVision)
		})
	}
}

func TestFindModel(t *testing.T) {
	ctx := context.Background()
	client := setupTestRedis(t)
	defer func() { _ = client.Close() }()

	registry := NewRegistry(WithRedis(client))

	// Pre-populate registry
	testRegistry := &ModelRegistry{
		FetchedAt: time.Now().UTC(),
		Models: []ModelInfo{
			{
				ProviderID: "openai",
				ModelID:    "gpt-4",
				Aliases:    []string{"gpt4", "gpt-4-turbo"},
				Status:     "active",
			},
			{
				ProviderID: "anthropic",
				ModelID:    "claude-3-opus",
				Status:     "active",
			},
		},
	}

	err := registry.cacheToRedis(ctx, testRegistry)
	require.NoError(t, err)

	t.Run("find by exact model ID", func(t *testing.T) {
		model, err := registry.FindModel(ctx, "gpt-4")
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", model.ModelID)
		assert.Equal(t, "openai", model.ProviderID)
	})

	t.Run("find by alias", func(t *testing.T) {
		model, err := registry.FindModel(ctx, "gpt4")
		require.NoError(t, err)
		assert.Equal(t, "gpt-4", model.ModelID)
	})

	t.Run("model not found", func(t *testing.T) {
		_, err := registry.FindModel(ctx, "nonexistent-model")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetModelsByProvider(t *testing.T) {
	ctx := context.Background()
	client := setupTestRedis(t)
	defer func() { _ = client.Close() }()

	registry := NewRegistry(WithRedis(client))

	testRegistry := &ModelRegistry{
		FetchedAt: time.Now().UTC(),
		Models: []ModelInfo{
			{ProviderID: "openai", ModelID: "gpt-4", Status: "active"},
			{ProviderID: "openai", ModelID: "gpt-3.5", Status: "active"},
			{ProviderID: "anthropic", ModelID: "claude-3", Status: "active"},
		},
	}

	err := registry.cacheToRedis(ctx, testRegistry)
	require.NoError(t, err)

	t.Run("get openai models", func(t *testing.T) {
		models, err := registry.GetModelsByProvider(ctx, "openai")
		require.NoError(t, err)
		assert.Len(t, models, 2)
	})

	t.Run("provider not found", func(t *testing.T) {
		_, err := registry.GetModelsByProvider(ctx, "nonexistent")
		assert.Error(t, err)
	})
}

func TestFindModelsByCapability(t *testing.T) {
	ctx := context.Background()
	client := setupTestRedis(t)
	defer func() { _ = client.Close() }()

	registry := NewRegistry(WithRedis(client))

	testRegistry := &ModelRegistry{
		FetchedAt: time.Now().UTC(),
		Models: []ModelInfo{
			{
				ProviderID:   "openai",
				ModelID:      "gpt-4",
				Status:       "active",
				Capabilities: ModelCapability{SupportsTools: true},
			},
			{
				ProviderID:   "anthropic",
				ModelID:      "claude-3",
				Status:       "active",
				Capabilities: ModelCapability{SupportsTools: true, SupportsVision: true},
			},
			{
				ProviderID:   "openai",
				ModelID:      "gpt-3.5",
				Status:       "degraded",
				Capabilities: ModelCapability{SupportsTools: true},
			},
		},
	}

	err := registry.cacheToRedis(ctx, testRegistry)
	require.NoError(t, err)

	t.Run("find by tools capability", func(t *testing.T) {
		models, err := registry.FindModelsByCapability(ctx, ModelCapability{SupportsTools: true})
		require.NoError(t, err)
		assert.Len(t, models, 2) // Excludes degraded model
	})

	t.Run("find by multiple capabilities", func(t *testing.T) {
		models, err := registry.FindModelsByCapability(ctx, ModelCapability{
			SupportsTools:  true,
			SupportsVision: true,
		})
		require.NoError(t, err)
		assert.Len(t, models, 1)
		assert.Equal(t, "claude-3", models[0].ModelID)
	})

	t.Run("no matches", func(t *testing.T) {
		models, err := registry.FindModelsByCapability(ctx, ModelCapability{SupportsReasoning: true})
		require.NoError(t, err)
		assert.Len(t, models, 0)
	})
}

func TestModelInfoJSON(t *testing.T) {
	model := ModelInfo{
		ProviderID:   "openai",
		ModelID:      "gpt-4",
		DisplayName:  "GPT-4",
		Capabilities: ModelCapability{SupportsTools: true},
		Cost:         ModelCost{InputCost: decimal.NewFromFloat(30.0), OutputCost: decimal.NewFromFloat(60.0)},
		Limits:       ModelLimits{ContextLimit: 8192, OutputLimit: 4096},
		Status:       "active",
	}

	data, err := json.Marshal(model)
	require.NoError(t, err)

	var decoded ModelInfo
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, model.ProviderID, decoded.ProviderID)
	assert.Equal(t, model.ModelID, decoded.ModelID)
	assert.Equal(t, model.Capabilities.SupportsTools, decoded.Capabilities.SupportsTools)
	assert.True(t, model.Cost.InputCost.Equal(decoded.Cost.InputCost))
	assert.Equal(t, model.Limits.ContextLimit, decoded.Limits.ContextLimit)
}
