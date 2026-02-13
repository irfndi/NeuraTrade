package services

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestNewWalletConvictionScorer(t *testing.T) {
	config := DefaultWalletConvictionConfig()
	scorer := NewWalletConvictionScorer(nil, config)

	assert.NotNil(t, scorer)
	assert.Equal(t, config, scorer.config)
}

func TestDefaultWalletConvictionConfig(t *testing.T) {
	config := DefaultWalletConvictionConfig()

	assert.Equal(t, 10, config.MaxHistoricalScores)
	assert.Equal(t, 30, config.MinAccountAgeDays)
	assert.Equal(t, decimal.NewFromInt(1000), config.MinBalanceForHigh)
	assert.Equal(t, 3, config.DiversityBonusAssets)
	assert.Equal(t, 30, config.ActivityLookbackDays)
}

func TestDetermineLevel(t *testing.T) {
	config := DefaultWalletConvictionConfig()
	scorer := NewWalletConvictionScorer(nil, config)

	tests := []struct {
		score    float64
		expected ConvictionLevel
	}{
		{85, ConvictionLevelVeryHigh},
		{80, ConvictionLevelVeryHigh},
		{75, ConvictionLevelHigh},
		{60, ConvictionLevelHigh},
		{55, ConvictionLevelMedium},
		{40, ConvictionLevelMedium},
		{35, ConvictionLevelLow},
		{0, ConvictionLevelLow},
	}

	for _, tt := range tests {
		t.Run(string(tt.expected), func(t *testing.T) {
			level := scorer.determineLevel(decimal.NewFromFloat(tt.score))
			assert.Equal(t, tt.expected, level)
		})
	}
}

func TestCalculateTrend(t *testing.T) {
	config := DefaultWalletConvictionConfig()
	scorer := NewWalletConvictionScorer(nil, config)

	tests := []struct {
		name         string
		currentScore float64
		historical   []float64
		expected     ScoreTrend
	}{
		{
			name:         "improving trend",
			currentScore: 70,
			historical:   []float64{50, 55, 60},
			expected:     ScoreTrendImproving,
		},
		{
			name:         "declining trend",
			currentScore: 40,
			historical:   []float64{60, 55, 50},
			expected:     ScoreTrendDeclining,
		},
		{
			name:         "stable trend",
			currentScore: 55,
			historical:   []float64{52, 54, 53},
			expected:     ScoreTrendStable,
		},
		{
			name:         "insufficient history",
			currentScore: 50,
			historical:   []float64{48},
			expected:     ScoreTrendStable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := &WalletConvictionScore{
				Score:            decimal.NewFromFloat(tt.currentScore),
				HistoricalScores: make([]decimal.Decimal, len(tt.historical)),
			}
			for i, h := range tt.historical {
				score.HistoricalScores[i] = decimal.NewFromFloat(h)
			}

			trend := scorer.calculateTrend(score)
			assert.Equal(t, tt.expected, trend)
		})
	}
}

func TestUpdateHistoricalScores(t *testing.T) {
	config := DefaultWalletConvictionConfig()
	scorer := NewWalletConvictionScorer(nil, config)

	score := &WalletConvictionScore{
		Score:            decimal.NewFromInt(50),
		HistoricalScores: []decimal.Decimal{},
	}

	for i := 0; i < 15; i++ {
		score.Score = decimal.NewFromInt(int64(i * 10))
		scorer.updateHistoricalScores(score)
	}

	assert.LessOrEqual(t, len(score.HistoricalScores), config.MaxHistoricalScores)
}

func TestGetScore(t *testing.T) {
	config := DefaultWalletConvictionConfig()
	scorer := NewWalletConvictionScorer(nil, config)

	scorer.scores["test-chat"] = &WalletConvictionScore{
		ChatID: "test-chat",
		Score:  decimal.NewFromInt(75),
	}

	score := scorer.GetScore("test-chat")
	assert.NotNil(t, score)
	assert.Equal(t, "test-chat", score.ChatID)
	assert.Equal(t, decimal.NewFromInt(75), score.Score)

	missingScore := scorer.GetScore("non-existent")
	assert.Nil(t, missingScore)
}

func TestGetAllScores(t *testing.T) {
	config := DefaultWalletConvictionConfig()
	scorer := NewWalletConvictionScorer(nil, config)

	scorer.scores["chat1"] = &WalletConvictionScore{ChatID: "chat1", Score: decimal.NewFromInt(60)}
	scorer.scores["chat2"] = &WalletConvictionScore{ChatID: "chat2", Score: decimal.NewFromInt(80)}

	allScores := scorer.GetAllScores()
	assert.Len(t, allScores, 2)
	assert.Contains(t, allScores, "chat1")
	assert.Contains(t, allScores, "chat2")
}

func TestGetCohortScores(t *testing.T) {
	config := DefaultWalletConvictionConfig()
	scorer := NewWalletConvictionScorer(nil, config)

	scorer.scores["chat1"] = &WalletConvictionScore{
		ChatID: "chat1",
		Score:  decimal.NewFromInt(85),
		Level:  ConvictionLevelVeryHigh,
	}
	scorer.scores["chat2"] = &WalletConvictionScore{
		ChatID: "chat2",
		Score:  decimal.NewFromInt(65),
		Level:  ConvictionLevelHigh,
	}
	scorer.scores["chat3"] = &WalletConvictionScore{
		ChatID: "chat3",
		Score:  decimal.NewFromInt(90),
		Level:  ConvictionLevelVeryHigh,
	}

	cohorts := scorer.GetCohortScores()
	assert.Len(t, cohorts[ConvictionLevelVeryHigh], 2)
	assert.Len(t, cohorts[ConvictionLevelHigh], 1)
}

func TestCalculateScoreWithNilDB(t *testing.T) {
	config := DefaultWalletConvictionConfig()
	scorer := NewWalletConvictionScorer(nil, config)

	score, err := scorer.CalculateScore(context.Background(), "test-chat")

	assert.NoError(t, err)
	assert.NotNil(t, score)
	assert.Equal(t, "test-chat", score.ChatID)
}
