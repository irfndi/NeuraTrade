package ai

import "strings"

type ProviderTransportFormat string

const (
	ProviderTransportOpenAI    ProviderTransportFormat = "openai"
	ProviderTransportAnthropic ProviderTransportFormat = "anthropic"
)

type ProviderTransportDefaults struct {
	BaseURL         string
	DefaultModel    string
	TransportFormat ProviderTransportFormat
	RequiresAPIKey  bool
}

var providerTransportDefaults = map[string]ProviderTransportDefaults{
	"anthropic": {
		BaseURL:         "https://api.anthropic.com/v1",
		DefaultModel:    "claude-sonnet-4-20250514",
		TransportFormat: ProviderTransportAnthropic,
		RequiresAPIKey:  true,
	},
	"deepseek": {
		BaseURL:         "https://api.deepseek.com/v1",
		DefaultModel:    "deepseek-v4-pro",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"google": {
		BaseURL:         "https://generativelanguage.googleapis.com/v1beta/openai",
		DefaultModel:    "gemini-2.5-flash",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"minimax": {
		BaseURL:         "https://api.minimax.io/anthropic/v1",
		DefaultModel:    "minimax-m2.5",
		TransportFormat: ProviderTransportAnthropic,
		RequiresAPIKey:  true,
	},
	"mlx": {
		BaseURL:         "http://localhost:8080/v1",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  false,
	},
	"openai": {
		BaseURL:         "https://api.openai.com/v1",
		DefaultModel:    "gpt-4o-mini",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"zai": {
		BaseURL:         "https://api.z.ai/api/paas/v4",
		DefaultModel:    "glm-5-turbo",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"zai-coding-plan": {
		BaseURL:         "https://api.z.ai/api/coding/paas/v4",
		DefaultModel:    "glm-5-turbo",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"zhipu": {
		BaseURL:         "https://api.z.ai/api/paas/v4",
		DefaultModel:    "glm-5-turbo",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
}

func NormalizeProviderID(providerID string) string {
	return strings.ToLower(strings.TrimSpace(providerID))
}

func ProviderEnvPrefix(providerID string) string {
	return strings.ToUpper(strings.ReplaceAll(NormalizeProviderID(providerID), "-", "_"))
}

func ProviderAPIKeyEnvVars(providerID string) []string {
	prefix := ProviderEnvPrefix(providerID)
	return []string{
		"NEURATRADE_AI_PROVIDER_" + prefix + "_API_KEY",
		prefix + "_API_KEY",
	}
}

func ProviderBaseURLEnvVars(providerID string) []string {
	prefix := ProviderEnvPrefix(providerID)
	return []string{
		"NEURATRADE_AI_PROVIDER_" + prefix + "_BASE_URL",
		prefix + "_BASE_URL",
	}
}

func ProviderModelEnvVars(providerID string) []string {
	prefix := ProviderEnvPrefix(providerID)
	return []string{
		"NEURATRADE_AI_PROVIDER_" + prefix + "_MODEL",
		prefix + "_MODEL",
	}
}

func ProviderDefaults(providerID string) (ProviderTransportDefaults, bool) {
	defaults, ok := providerTransportDefaults[NormalizeProviderID(providerID)]
	return defaults, ok
}

func ProviderDefaultBaseURL(providerID string) (string, bool) {
	defaults, ok := ProviderDefaults(providerID)
	if !ok || defaults.BaseURL == "" {
		return "", false
	}
	return defaults.BaseURL, true
}

func ProviderDefaultModel(providerID string) (string, bool) {
	defaults, ok := ProviderDefaults(providerID)
	if !ok || defaults.DefaultModel == "" {
		return "", false
	}
	return defaults.DefaultModel, true
}

func ProviderRequiresAPIKey(providerID string) bool {
	defaults, ok := ProviderDefaults(providerID)
	return !ok || defaults.RequiresAPIKey
}

func ProviderUsesAnthropicFormat(providerID string, baseURL string) bool {
	if defaults, ok := ProviderDefaults(providerID); ok {
		return defaults.TransportFormat == ProviderTransportAnthropic
	}
	baseURL = strings.ToLower(strings.TrimSpace(baseURL))
	return strings.Contains(baseURL, "anthropic") || strings.Contains(baseURL, "minimax")
}
