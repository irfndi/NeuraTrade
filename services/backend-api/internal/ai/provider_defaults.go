package ai

import (
	"os"
	"sort"
	"strings"
)

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
		DefaultModel:    "deepseek-chat",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"google": {
		BaseURL:         "https://generativelanguage.googleapis.com/v1beta/openai",
		DefaultModel:    "gemini-2.5-flash",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"kimi": {
		BaseURL:         "https://api.kimi.com/coding/v1",
		DefaultModel:    "k2p6",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"kimi-for-coding": {
		BaseURL:         "https://api.kimi.com/coding/v1",
		DefaultModel:    "k2p6",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"moonshotai": {
		BaseURL:         "https://api.moonshot.ai/v1",
		DefaultModel:    "kimi-k2.6",
		TransportFormat: ProviderTransportOpenAI,
		RequiresAPIKey:  true,
	},
	"moonshotai-cn": {
		BaseURL:         "https://api.moonshot.cn/v1",
		DefaultModel:    "kimi-k2.6",
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

func ProviderTransportFormatEnvVars(providerID string) []string {
	prefix := ProviderEnvPrefix(providerID)
	return []string{
		"NEURATRADE_AI_PROVIDER_" + prefix + "_TRANSPORT_FORMAT",
		prefix + "_TRANSPORT_FORMAT",
	}
}

func ProviderDefaults(providerID string) (ProviderTransportDefaults, bool) {
	defaults, ok := providerTransportDefaults[NormalizeProviderID(providerID)]
	return defaults, ok
}

func ProviderIDs() []string {
	providers := make([]string, 0, len(providerTransportDefaults))
	for providerID := range providerTransportDefaults {
		providers = append(providers, providerID)
	}
	sort.Strings(providers)
	return providers
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
	if format, ok := providerTransportFormatOverride(providerID); ok {
		return format == ProviderTransportAnthropic
	}

	baseURLNorm := strings.ToLower(strings.TrimSpace(baseURL))
	if strings.Contains(baseURLNorm, "anthropic") || strings.Contains(baseURLNorm, "minimax") {
		return true
	}
	if strings.Contains(baseURLNorm, "openai") {
		return false
	}

	if defaults, ok := ProviderDefaults(providerID); ok {
		return defaults.TransportFormat == ProviderTransportAnthropic
	}
	return false
}

func providerTransportFormatOverride(providerID string) (ProviderTransportFormat, bool) {
	for _, envKey := range ProviderTransportFormatEnvVars(providerID) {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(envKey))) {
		case string(ProviderTransportOpenAI):
			return ProviderTransportOpenAI, true
		case string(ProviderTransportAnthropic):
			return ProviderTransportAnthropic, true
		}
	}
	return "", false
}

func ProviderEndpointURL(baseURL string, path string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	path = "/" + strings.TrimLeft(strings.TrimSpace(path), "/")
	if baseURL == "" {
		return ""
	}
	if strings.HasSuffix(baseURL, path) {
		return baseURL
	}
	return baseURL + path
}
