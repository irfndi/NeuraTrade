package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModelConfig(t *testing.T) {
	mc := NewModelConfig()
	assert.NotNil(t, mc)
	assert.Equal(t, 0, mc.Count())
}

func TestNewModelConfigFromSingle(t *testing.T) {
	mc := NewModelConfigFromSingle("openai", "gpt-4o-mini", "sk-test", "https://api.openai.com/v1")

	assert.Equal(t, 1, mc.Count())
	assert.False(t, mc.HasMultiple())

	primary, err := mc.PrimaryModel()
	require.NoError(t, err)
	assert.Equal(t, "openai", primary.ProviderID)
	assert.Equal(t, "gpt-4o-mini", primary.ModelID)
	assert.Equal(t, "sk-test", primary.APIKey)
}

func TestNewModelConfigFromList(t *testing.T) {
	models := []ConfiguredModel{
		{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"},
		{ProviderID: "openai", ModelID: "gpt-4o-mini"},
	}
	mc := NewModelConfigFromList(models)

	assert.Equal(t, 2, mc.Count())
	assert.True(t, mc.HasMultiple())
	assert.True(t, mc.RequiresFallback())
}

func TestModelConfigAddRemove(t *testing.T) {
	mc := NewModelConfig()

	mc.AddModel(ConfiguredModel{ProviderID: "openai", ModelID: "gpt-4o"})
	assert.Equal(t, 1, mc.Count())

	mc.AddModel(ConfiguredModel{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"})
	assert.Equal(t, 2, mc.Count())

	removed := mc.RemoveModel("openai", "gpt-4o")
	assert.True(t, removed)
	assert.Equal(t, 1, mc.Count())

	removed = mc.RemoveModel("openai", "gpt-4o")
	assert.False(t, removed)
	assert.Equal(t, 1, mc.Count())
}

func TestModelConfigFindModel(t *testing.T) {
	mc := NewModelConfigFromList([]ConfiguredModel{
		{ProviderID: "openai", ModelID: "gpt-4o-mini"},
		{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"},
	})

	model, err := mc.FindModel("anthropic", "claude-sonnet-4-20250514")
	require.NoError(t, err)
	assert.Equal(t, "claude-sonnet-4-20250514", model.ModelID)

	_, err = mc.FindModel("google", "gemini")
	assert.Equal(t, ErrModelNotFound, err)
}

func TestModelConfigPrimaryModel(t *testing.T) {
	mc := NewModelConfig()
	_, err := mc.PrimaryModel()
	assert.Equal(t, ErrNoModelsConfigured, err)

	mc.AddModel(ConfiguredModel{ProviderID: "openai", ModelID: "gpt-4o"})
	primary, err := mc.PrimaryModel()
	require.NoError(t, err)
	assert.Equal(t, "gpt-4o", primary.ModelID)
}

func TestModelConfigModels(t *testing.T) {
	mc := NewModelConfigFromSingle("openai", "gpt-4o-mini", "key", "")
	models := mc.Models()

	models[0].ModelID = "modified"
	original, _ := mc.PrimaryModel()
	assert.Equal(t, "gpt-4o-mini", original.ModelID, "Models() should return a copy")
}

func TestModelSelectorSingleModel(t *testing.T) {
	mc := NewModelConfigFromSingle("anthropic", "claude-sonnet-4-20250514", "key", "")
	rs := NewRankingService()
	selector := NewModelSelector(mc, rs)

	result, err := selector.Select(context.Background(), SelectionConstraints{})
	require.NoError(t, err)
	assert.Equal(t, RoutingModePrimary, result.Mode)
	assert.Equal(t, "claude-sonnet-4-20250514", result.Model.ModelID)
	assert.Equal(t, 1, result.Total)
	assert.True(t, selector.IsSingleModel())
}

func TestModelSelectorFallback(t *testing.T) {
	mc := NewModelConfigFromList([]ConfiguredModel{
		{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"},
		{ProviderID: "openai", ModelID: "gpt-4o-mini"},
	})
	rs := NewRankingService()
	selector := NewModelSelector(mc, rs)

	result, err := selector.Select(context.Background(), SelectionConstraints{})
	require.NoError(t, err)
	assert.Equal(t, RoutingModeFallback, result.Mode)
	assert.Equal(t, "claude-sonnet-4-20250514", result.Model.ModelID,
		"highest capability model should be selected first")
	assert.False(t, selector.IsSingleModel())
}

func TestModelSelectorRoundRobin(t *testing.T) {
	mc := NewModelConfigFromList([]ConfiguredModel{
		{ProviderID: "openai", ModelID: "gpt-4o-mini"},
		{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"},
	})
	rs := NewRankingService()
	selector := NewModelSelector(mc, rs)

	r1, err := selector.SelectRoundRobin(context.Background(), SelectionConstraints{})
	require.NoError(t, err)
	assert.Equal(t, RoutingModeRoundRobin, r1.Mode)

	r2, err := selector.SelectRoundRobin(context.Background(), SelectionConstraints{})
	require.NoError(t, err)
	assert.NotEqual(t, r1.Model.ModelID, r2.Model.ModelID,
		"round-robin should cycle through models")
}

func TestModelSelectorReselectNext(t *testing.T) {
	mc := NewModelConfigFromList([]ConfiguredModel{
		{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"},
		{ProviderID: "openai", ModelID: "gpt-4o-mini"},
		{ProviderID: "openai", ModelID: "gpt-4o"},
	})
	rs := NewRankingService()
	selector := NewModelSelector(mc, rs)

	next, err := selector.ReselectNext(context.Background(), "anthropic", "claude-sonnet-4-20250514", SelectionConstraints{})
	require.NoError(t, err)
	assert.NotEqual(t, "claude-sonnet-4-20250514", next.Model.ModelID)
}

func TestModelSelectorReselectExhausted(t *testing.T) {
	mc := NewModelConfigFromSingle("openai", "gpt-4o-mini", "key", "")
	rs := NewRankingService()
	selector := NewModelSelector(mc, rs)

	_, err := selector.ReselectNext(context.Background(), "openai", "gpt-4o-mini", SelectionConstraints{})
	assert.Error(t, err)
}

func TestModelSelectorEmptyConfig(t *testing.T) {
	mc := NewModelConfig()
	rs := NewRankingService()
	selector := NewModelSelector(mc, rs)

	_, err := selector.Select(context.Background(), SelectionConstraints{})
	assert.Equal(t, ErrNoModelsConfigured, err)
}

func TestModelSelectorFallbackChain(t *testing.T) {
	mc := NewModelConfigFromList([]ConfiguredModel{
		{ProviderID: "openai", ModelID: "gpt-4o-mini"},
		{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"},
		{ProviderID: "openai", ModelID: "gpt-4o"},
	})
	rs := NewRankingService()
	selector := NewModelSelector(mc, rs)

	chain, err := selector.SelectFallbackChain(context.Background(), SelectionConstraints{})
	require.NoError(t, err)
	require.Len(t, chain, 3)
	assert.Equal(t, "claude-sonnet-4-20250514", chain[0].ModelID,
		"fallback chain should be ordered by capability")
}

func TestModelSelectorActiveModels(t *testing.T) {
	mc := NewModelConfigFromList([]ConfiguredModel{
		{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"},
		{ProviderID: "openai", ModelID: "gpt-4o-mini"},
	})
	rs := NewRankingService()
	selector := NewModelSelector(mc, rs)

	active := selector.ActiveModels()
	assert.Len(t, active, 2)
}
