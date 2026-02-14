package services

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewDebateCoordinator(t *testing.T) {
	config := DefaultRoundRobinDebateConfig()
	coordinator := NewDebateCoordinator(config)

	assert.NotNil(t, coordinator)
	assert.NotNil(t, coordinator.debateLoop)
	assert.NotNil(t, coordinator.activeDebates)
	assert.Equal(t, config.MaxRounds, coordinator.config.MaxRounds)
	assert.Equal(t, config.ConsensusThreshold, coordinator.config.ConsensusThreshold)
}

func TestDebateCoordinator_RunRoundRobinDebate(t *testing.T) {
	config := RoundRobinDebateConfig{
		MaxRounds:          3,
		ConsensusThreshold: 0.7,
		RoundTimeout:       10 * time.Second,
		DebateTimeout:      30 * time.Second,
		RequireUnanimity:   false,
		AnalystConfig:      DefaultAnalystAgentConfig(),
		TraderConfig:       DefaultTraderAgentConfig(),
	}

	coordinator := NewDebateCoordinator(config)

	market := MarketContext{
		Symbol:       "BTC/USDT",
		CurrentPrice: 50000,
		Volume24h:    1000000,
		Liquidity:    0.8,
		Volatility:   0.3,
	}

	portfolio := PortfolioState{
		TotalValue:    10000,
		AvailableCash: 10000,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	result, err := coordinator.RunRoundRobinDebate(ctx, market, portfolio)

	// Should error due to cancelled context
	assert.Error(t, err)
	assert.NotNil(t, result)
}

func TestDebateCoordinator_calculateRoundConfidence(t *testing.T) {
	coordinator := NewDebateCoordinator(DefaultRoundRobinDebateConfig())

	tests := []struct {
		name     string
		round    *RoundRobinDebateRound
		expected float64
	}{
		{
			name:     "empty round",
			round:    &RoundRobinDebateRound{},
			expected: 0,
		},
		{
			name: "single agent",
			round: &RoundRobinDebateRound{
				AnalystTurn: &DebateTurnResult{Confidence: 0.8},
			},
			expected: 0.8,
		},
		{
			name: "two agents",
			round: &RoundRobinDebateRound{
				AnalystTurn: &DebateTurnResult{Confidence: 0.8},
				RiskTurn:    &DebateTurnResult{Confidence: 0.6},
			},
			expected: 0.7,
		},
		{
			name: "three agents",
			round: &RoundRobinDebateRound{
				AnalystTurn: &DebateTurnResult{Confidence: 0.9},
				RiskTurn:    &DebateTurnResult{Confidence: 0.7},
				TraderTurn:  &DebateTurnResult{Confidence: 0.8},
			},
			expected: 0.8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := coordinator.calculateRoundConfidence(tt.round)
			assert.InDelta(t, tt.expected, result, 0.01)
		})
	}
}

func TestDebateCoordinator_hasAchievedConsensus(t *testing.T) {
	tests := []struct {
		name             string
		requireUnanimity bool
		threshold        float64
		round            *RoundRobinDebateRound
		expected         bool
	}{
		{
			name:             "unanimity - all agree",
			requireUnanimity: true,
			threshold:        0.7,
			round: &RoundRobinDebateRound{
				AnalystTurn:    &DebateTurnResult{Decision: "buy", Confidence: 0.8},
				RiskTurn:       &DebateTurnResult{Decision: "buy", Confidence: 0.8},
				TraderTurn:     &DebateTurnResult{Decision: "buy", Confidence: 0.8},
				RoundConsensus: "approved",
				Confidence:     0.8,
			},
			expected: true,
		},
		{
			name:             "unanimity - disagree",
			requireUnanimity: true,
			threshold:        0.7,
			round: &RoundRobinDebateRound{
				AnalystTurn:    &DebateTurnResult{Decision: "buy", Confidence: 0.8},
				RiskTurn:       &DebateTurnResult{Decision: "sell", Confidence: 0.8},
				TraderTurn:     &DebateTurnResult{Decision: "buy", Confidence: 0.8},
				RoundConsensus: "hold",
				Confidence:     0.8,
			},
			expected: false,
		},
		{
			name:             "majority - above threshold",
			requireUnanimity: false,
			threshold:        0.7,
			round: &RoundRobinDebateRound{
				RoundConsensus: "approved",
				Confidence:     0.75,
			},
			expected: true,
		},
		{
			name:             "majority - below threshold",
			requireUnanimity: false,
			threshold:        0.7,
			round: &RoundRobinDebateRound{
				RoundConsensus: "approved",
				Confidence:     0.65,
			},
			expected: false,
		},
		{
			name:             "majority - not approved",
			requireUnanimity: false,
			threshold:        0.7,
			round: &RoundRobinDebateRound{
				RoundConsensus: "rejected",
				Confidence:     0.8,
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultRoundRobinDebateConfig()
			config.RequireUnanimity = tt.requireUnanimity
			config.ConsensusThreshold = tt.threshold
			coordinator := NewDebateCoordinator(config)

			result := coordinator.hasAchievedConsensus(tt.round)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDebateCoordinator_finalizeDebate(t *testing.T) {
	coordinator := NewDebateCoordinator(DefaultRoundRobinDebateConfig())

	tests := []struct {
		name              string
		result            *RoundRobinDebateResult
		expectedConsensus string
		expectedDecision  string
	}{
		{
			name:              "empty rounds",
			result:            &RoundRobinDebateResult{Rounds: []*RoundRobinDebateRound{}},
			expectedConsensus: "no_consensus",
			expectedDecision:  "hold",
		},
		{
			name: "approved with trader",
			result: &RoundRobinDebateResult{
				Rounds: []*RoundRobinDebateRound{
					{
						RoundConsensus: "approved",
						Confidence:     0.8,
						TraderTurn:     &DebateTurnResult{Decision: "buy"},
					},
				},
			},
			expectedConsensus: "approved",
			expectedDecision:  "buy",
		},
		{
			name: "risk rejected",
			result: &RoundRobinDebateResult{
				Rounds: []*RoundRobinDebateRound{
					{
						RoundConsensus: "approved",
						Confidence:     0.8,
						RiskTurn:       &DebateTurnResult{Decision: "rejected"},
						TraderTurn:     &DebateTurnResult{Decision: "buy"},
					},
				},
			},
			expectedConsensus: "rejected",
			expectedDecision:  "hold",
		},
		{
			name: "only analyst",
			result: &RoundRobinDebateResult{
				Rounds: []*RoundRobinDebateRound{
					{
						RoundConsensus: "hold",
						Confidence:     0.5,
						AnalystTurn:    &DebateTurnResult{Decision: "sell"},
					},
				},
			},
			expectedConsensus: "hold",
			expectedDecision:  "sell",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coordinator.finalizeDebate(tt.result)
			assert.Equal(t, tt.expectedConsensus, tt.result.FinalConsensus)
			assert.Equal(t, tt.expectedDecision, tt.result.FinalDecision)
			assert.False(t, tt.result.EndedAt.IsZero())
		})
	}
}

func TestDebateCoordinator_GetActiveDebates(t *testing.T) {
	coordinator := NewDebateCoordinator(DefaultRoundRobinDebateConfig())

	// Initially no active debates
	active := coordinator.GetActiveDebates()
	assert.Empty(t, active)

	// Add a mock active debate
	coordinator.activeDebates["test-1"] = &RoundRobinDebateResult{
		DebateID:  "test-1",
		StartedAt: time.Now(),
	}

	active = coordinator.GetActiveDebates()
	assert.Len(t, active, 1)
	assert.Equal(t, "test-1", active[0].DebateID)
}

func TestDebateCoordinator_GetDebateMetrics(t *testing.T) {
	config := DefaultRoundRobinDebateConfig()
	config.MaxRounds = 10
	config.ConsensusThreshold = 0.8

	coordinator := NewDebateCoordinator(config)
	coordinator.activeDebates["test-1"] = &RoundRobinDebateResult{DebateID: "test-1"}
	coordinator.activeDebates["test-2"] = &RoundRobinDebateResult{DebateID: "test-2"}

	metrics := coordinator.GetDebateMetrics()

	assert.Equal(t, 2, metrics["active_debates"])
	assert.Equal(t, 10, metrics["max_rounds"])
	assert.Equal(t, 0.8, metrics["consensus_threshold"])
}

func TestDefaultRoundRobinDebateConfig(t *testing.T) {
	config := DefaultRoundRobinDebateConfig()

	assert.Equal(t, 5, config.MaxRounds)
	assert.Equal(t, 0.7, config.ConsensusThreshold)
	assert.Equal(t, 30*time.Second, config.RoundTimeout)
	assert.Equal(t, 5*time.Minute, config.DebateTimeout)
	assert.False(t, config.RequireUnanimity)
}
