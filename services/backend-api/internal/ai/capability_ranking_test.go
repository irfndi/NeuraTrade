package ai

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedCapabilityRanking(t *testing.T) {
	ranking := SeedCapabilityRanking()

	assert.Equal(t, "vending-bench-2-seed", ranking.BenchVersion)
	assert.Equal(t, VendingBench2URL, ranking.SourceURL)
	assert.Greater(t, len(ranking.Rankings), 0)

	assert.Equal(t, 1, ranking.Rankings[0].Rank)
	assert.Equal(t, "claude-sonnet-4-20250514", ranking.Rankings[0].ModelID)
	assert.Equal(t, "anthropic", ranking.Rankings[0].ProviderID)
	assert.Greater(t, ranking.Rankings[0].Score, 0.0)

	for i := 1; i < len(ranking.Rankings); i++ {
		assert.GreaterOrEqual(t, ranking.Rankings[i-1].Score, ranking.Rankings[i].Score,
			"rankings should be in descending order of score")
	}
}

func TestDefaultRankingScore(t *testing.T) {
	tests := []struct {
		modelID string
		wantMin float64
	}{
		{"claude-sonnet-4-20250514", 90.0},
		{"deepseek-v4-pro", 90.0},
		{"gpt-4o", 80.0},
		{"gpt-4o-mini", 70.0},
		{"unknown-model", 40.0},
		{"", 40.0},
	}

	for _, tt := range tests {
		score := DefaultRankingScore(tt.modelID)
		assert.GreaterOrEqual(t, score, tt.wantMin, "model %s", tt.modelID)
	}
}

func TestRankingServiceGetRankings(t *testing.T) {
	rs := NewRankingService()

	ranking, err := rs.GetRankings(context.Background())
	require.NoError(t, err)
	require.NotNil(t, ranking)
	assert.Greater(t, len(ranking.Rankings), 0)
}

func TestRankingServiceGetRankingForModel(t *testing.T) {
	rs := NewRankingService()

	tests := []struct {
		modelID      string
		wantErr      bool
		wantMinScore float64
	}{
		{"claude-sonnet-4-20250514", false, 90.0},
		{"gpt-4o-mini", false, 70.0},
		{"nonexistent-model", false, 40.0},
	}

	for _, tt := range tests {
		score, err := rs.GetRankingForModel(context.Background(), tt.modelID)
		if tt.wantErr {
			assert.Error(t, err)
		} else {
			require.NoError(t, err)
			assert.GreaterOrEqual(t, score, tt.wantMinScore, "model %s", tt.modelID)
		}
	}
}

func TestRankingServiceRankModels(t *testing.T) {
	rs := NewRankingService()

	models := []ConfiguredModel{
		{ProviderID: "openai", ModelID: "gpt-4o-mini"},
		{ProviderID: "anthropic", ModelID: "claude-sonnet-4-20250514"},
		{ProviderID: "openai", ModelID: "gpt-4o"},
	}

	ranked, err := rs.RankModels(context.Background(), models)
	require.NoError(t, err)
	require.Len(t, ranked, 3)

	assert.Equal(t, "claude-sonnet-4-20250514", ranked[0].ModelID,
		"highest-scored model should be first")
	assert.True(t, ranked[0].CapabilityScore >= ranked[1].CapabilityScore,
		"models should be sorted by capability score descending")
}

func TestSortByCapability(t *testing.T) {
	models := []ConfiguredModel{
		{ModelID: "low", CapabilityScore: 50.0},
		{ModelID: "high", CapabilityScore: 100.0},
		{ModelID: "mid", CapabilityScore: 75.0},
	}

	sortByCapability(models)

	assert.Equal(t, "high", models[0].ModelID)
	assert.Equal(t, "mid", models[1].ModelID)
	assert.Equal(t, "low", models[2].ModelID)
}

func TestResolveModelAliases(t *testing.T) {
	tests := []struct {
		modelID string
		wantLen int
	}{
		{"gpt-4o", 2},
		{"claude-sonnet-4", 1},
		{"unknown", 0},
	}

	for _, tt := range tests {
		aliases := resolveModelAliases(tt.modelID)
		assert.Len(t, aliases, tt.wantLen)
	}
}

func TestRankingServiceWithCustomURL(t *testing.T) {
	rs := NewRankingService(WithRankingSourceURL("https://invalid.example.com/nonexistent"))

	ranking, err := rs.GetRankings(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, ranking)
	assert.Equal(t, "vending-bench-2-seed", ranking.BenchVersion,
		"should fall back to seed data when fetch fails")
}
