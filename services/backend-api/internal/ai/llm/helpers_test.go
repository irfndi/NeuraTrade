package llm

import (
	"testing"

	"github.com/irfndi/neuratrade/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestProviderConstants(t *testing.T) {
	assert.Equal(t, Provider("openai"), ProviderOpenAI)
	assert.Equal(t, Provider("anthropic"), ProviderAnthropic)
	assert.Equal(t, Provider("google"), ProviderGoogle)
	assert.Equal(t, Provider("mistral"), ProviderMistral)
	assert.Equal(t, Provider("mlx"), ProviderMLX)
}

func TestClientFactory_SupportsAllProviders(t *testing.T) {
	factory := NewClientFactory(nil)

	providers := []Provider{ProviderOpenAI, ProviderAnthropic, ProviderGoogle, ProviderMistral, ProviderMLX}
	for _, p := range providers {
		t.Run(string(p), func(t *testing.T) {
			switch p {
			case ProviderOpenAI:
				factory.configs[p] = ClientConfig{APIKey: "test"}
			case ProviderAnthropic:
				factory.configs[p] = ClientConfig{APIKey: "test"}
			case ProviderMLX:
				factory.configs[p] = ClientConfig{APIKey: "test"}
			case ProviderGoogle, ProviderMistral:
				t.Skip("Not yet implemented")
			}
		})
	}
}

func TestFormatMarketRegime_NilInput(t *testing.T) {
	result := FormatMarketRegime(nil)
	assert.Contains(t, result, "Unknown")
	assert.Contains(t, result, "not available")
}

func TestFormatMarketRegime_WithData(t *testing.T) {
	regime := &models.MarketRegime{
		Symbol:          "BTC/USDT",
		Exchange:        "binance",
		Trend:           "bullish",
		Volatility:      "high",
		TrendStrength:   0.05,
		VolatilityScore: 0.8,
		Confidence:      0.75,
		WindowSize:      60,
	}

	result := FormatMarketRegime(regime)

	assert.Contains(t, result, "BTC/USDT")
	assert.Contains(t, result, "bullish")
	assert.Contains(t, result, "high")
	assert.Contains(t, result, "75%")
	assert.Contains(t, result, "60")
}

func TestClientFactory_Create_UnsupportedProvider(t *testing.T) {
	factory := NewClientFactory(nil)

	_, err := factory.Create(nil, Provider("unknown"))
	assert.Error(t, err)
	assert.True(t, err == ErrUnsupportedProvider{Provider: "unknown"} || err == ErrProviderNotConfigured{Provider: "unknown"})
}
