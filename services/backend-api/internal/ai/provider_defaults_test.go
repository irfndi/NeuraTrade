package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderDefaults(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		baseURL    string
		model      string
		format     ProviderTransportFormat
		apiKey     bool
		modelKnown bool
	}{
		{
			name:       "minimax anthropic transport",
			provider:   "minimax",
			baseURL:    "https://api.minimax.io/anthropic/v1",
			model:      "minimax-m2.5",
			format:     ProviderTransportAnthropic,
			apiKey:     true,
			modelKnown: true,
		},
		{
			name:       "zhipu openai compatible transport",
			provider:   "zhipu",
			baseURL:    "https://api.z.ai/api/paas/v4",
			model:      "glm-5-turbo",
			format:     ProviderTransportOpenAI,
			apiKey:     true,
			modelKnown: true,
		},
		{
			name:       "mlx local transport without api key",
			provider:   "mlx",
			baseURL:    "http://localhost:8080/v1",
			format:     ProviderTransportOpenAI,
			apiKey:     false,
			modelKnown: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defaults, ok := ProviderDefaults(tt.provider)
			require.True(t, ok)
			assert.Equal(t, tt.baseURL, defaults.BaseURL)
			assert.Equal(t, tt.model, defaults.DefaultModel)
			assert.Equal(t, tt.format, defaults.TransportFormat)
			assert.Equal(t, tt.apiKey, ProviderRequiresAPIKey(tt.provider))

			model, ok := ProviderDefaultModel(tt.provider)
			assert.Equal(t, tt.modelKnown, ok)
			assert.Equal(t, tt.model, model)
		})
	}
}

func TestProviderDefaultLookupNormalizesProviderID(t *testing.T) {
	baseURL, ok := ProviderDefaultBaseURL(" ZAI-CODING-PLAN ")
	require.True(t, ok)
	assert.Equal(t, "https://api.z.ai/api/coding/paas/v4", baseURL)
	assert.Equal(t, "ZAI_CODING_PLAN", ProviderEnvPrefix("zai-coding-plan"))
	assert.Equal(t, []string{
		"anthropic",
		"deepseek",
		"google",
		"minimax",
		"mlx",
		"openai",
		"zai",
		"zai-coding-plan",
		"zhipu",
	}, ProviderIDs())
}

func TestProviderEnvVarNames(t *testing.T) {
	assert.Equal(t, []string{
		"NEURATRADE_AI_PROVIDER_DEEPSEEK_API_KEY",
		"DEEPSEEK_API_KEY",
	}, ProviderAPIKeyEnvVars("deepseek"))

	assert.Equal(t, []string{
		"NEURATRADE_AI_PROVIDER_DEEPSEEK_MODEL",
		"DEEPSEEK_MODEL",
	}, ProviderModelEnvVars("deepseek"))

	assert.Equal(t, []string{
		"NEURATRADE_AI_PROVIDER_ZAI_CODING_PLAN_BASE_URL",
		"ZAI_CODING_PLAN_BASE_URL",
	}, ProviderBaseURLEnvVars("zai-coding-plan"))

	assert.Equal(t, []string{
		"NEURATRADE_AI_PROVIDER_ZAI_CODING_PLAN_MODEL",
		"ZAI_CODING_PLAN_MODEL",
	}, ProviderModelEnvVars("zai-coding-plan"))

	assert.Equal(t, []string{
		"NEURATRADE_AI_PROVIDER_DEEPSEEK_API_KEY",
		"DEEPSEEK_API_KEY",
	}, ProviderAPIKeyEnvVars("  DeepSeek  "))

	assert.Equal(t, []string{
		"NEURATRADE_AI_PROVIDER_DEEPSEEK_MODEL",
		"DEEPSEEK_MODEL",
	}, ProviderModelEnvVars("  DeepSeek  "))

	assert.Equal(t, []string{
		"NEURATRADE_AI_PROVIDER_ZAI_CODING_PLAN_BASE_URL",
		"ZAI_CODING_PLAN_BASE_URL",
	}, ProviderBaseURLEnvVars("  ZAI-Coding-Plan  "))

	assert.Equal(t, []string{
		"NEURATRADE_AI_PROVIDER_ZAI_CODING_PLAN_MODEL",
		"ZAI_CODING_PLAN_MODEL",
	}, ProviderModelEnvVars("  ZAI-Coding-Plan  "))

	assert.Equal(t, []string{
		"NEURATRADE_AI_PROVIDER_ZAI_CODING_PLAN_TRANSPORT_FORMAT",
		"ZAI_CODING_PLAN_TRANSPORT_FORMAT",
	}, ProviderTransportFormatEnvVars("  ZAI-Coding-Plan  "))
}

func TestProviderUsesAnthropicFormat(t *testing.T) {
	assert.True(t, ProviderUsesAnthropicFormat("minimax", ""))
	assert.True(t, ProviderUsesAnthropicFormat("unknown", "https://example.com/anthropic/v1"))
	assert.False(t, ProviderUsesAnthropicFormat("minimax", "https://openai-proxy.example/v1"))
	assert.False(t, ProviderUsesAnthropicFormat("zhipu", "https://api.z.ai/api/paas/v4"))

	t.Setenv("NEURATRADE_AI_PROVIDER_MINIMAX_TRANSPORT_FORMAT", "openai")
	assert.False(t, ProviderUsesAnthropicFormat("minimax", "https://api.minimax.io/anthropic/v1"))
}

func TestProviderEndpointURL(t *testing.T) {
	assert.Equal(t, "https://proxy.example/v1/chat/completions", ProviderEndpointURL("https://proxy.example/v1", "/chat/completions"))
	assert.Equal(t, "https://proxy.example/v1/chat/completions", ProviderEndpointURL("https://proxy.example/v1/chat/completions", "/chat/completions"))
	assert.Empty(t, ProviderEndpointURL("", "/messages"))
}

func TestClientProviderEndpointDefaults(t *testing.T) {
	client := NewClient(nil)

	assert.Equal(t, "https://api.minimax.io/anthropic/v1", client.getBaseURL("minimax"))
	assert.Equal(t, "minimax-m2.5", client.resolveModel(context.Background(), "minimax", ""))

	t.Setenv("NEURATRADE_AI_PROVIDER_MINIMAX_BASE_URL", "https://override.example/v1")
	t.Setenv("NEURATRADE_AI_PROVIDER_MINIMAX_MODEL", "minimax-override")
	assert.Equal(t, "https://override.example/v1", client.getBaseURL("minimax"))
	assert.Equal(t, "minimax-override", client.resolveModel(context.Background(), "minimax", ""))

	assert.Empty(t, client.getBaseURL("unknown-provider"))
	assert.Empty(t, client.resolveModel(context.Background(), "unknown-provider", ""))
}

func TestClientChatAllowsKeylessProviderDefaults(t *testing.T) {
	chatServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/chat/completions", r.URL.Path)
		assert.Empty(t, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"mlx-test-model","choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer chatServer.Close()

	registryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"mlx":{"name":"MLX","models":{}}}`))
	}))
	defer registryServer.Close()

	t.Setenv("NEURATRADE_AI_PROVIDER_MLX_BASE_URL", chatServer.URL)
	t.Setenv("NEURATRADE_AI_PROVIDER_MLX_MODEL", "mlx-test-model")

	client := NewClient(NewRegistry(WithModelsDevURL(registryServer.URL)))
	resp, err := client.Chat(context.Background(), "mlx", "", []Message{{Role: "user", Content: "ping"}})

	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Content)
	assert.Equal(t, "mlx-test-model", resp.Model)
}
