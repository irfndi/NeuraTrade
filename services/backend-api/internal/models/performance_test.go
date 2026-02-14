package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDecisionJournal_JSONSerialization(t *testing.T) {
	journal := DecisionJournal{
		ID:               "journal-123",
		SessionID:        "session-456",
		Symbol:           "BTC/USDT",
		Exchange:         "binance",
		SkillID:          "scalping",
		DecisionType:     "entry",
		Action:           "open_long",
		Side:             "long",
		SizePercent:      10.0,
		EntryPrice:       50000.0,
		StopLoss:         49500.0,
		TakeProfit:       51000.0,
		Confidence:       0.85,
		Reasoning:        "Bullish regime detected",
		RegimeTrend:      "bullish",
		RegimeVolatility: "normal",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	data, err := json.Marshal(journal)
	assert.NoError(t, err)
	assert.Contains(t, string(data), "journal-123")
	assert.Contains(t, string(data), "scalping")
}

func TestTradeOutcome_Fields(t *testing.T) {
	outcome := TradeOutcome{
		ID:                  "outcome-123",
		Symbol:              "ETH/USDT",
		Side:                "long",
		EntryPrice:          3000.0,
		ExitPrice:           3105.0,
		Size:                1000.0,
		PnL:                 104998.0,
		PnLPercent:          3.5,
		Fees:                2.0,
		HoldDurationSeconds: 120,
		Outcome:             "win",
		ExitReason:          "take_profit",
	}

	assert.Equal(t, "win", outcome.Outcome)
	assert.Equal(t, "take_profit", outcome.ExitReason)
	assert.Equal(t, 104998.0, outcome.PnL)
	assert.Greater(t, outcome.PnLPercent, 3.0)
}

func TestFailurePattern_EnableDisable(t *testing.T) {
	pattern := FailurePattern{
		ID:              "pattern-123",
		SkillID:         "scalping",
		PatternType:     "consecutive_loss",
		Enabled:         true,
		OccurrenceCount: 5,
	}

	assert.True(t, pattern.Enabled)

	pattern.Enabled = false
	assert.False(t, pattern.Enabled)
}

func TestStrategyParameter_ActiveState(t *testing.T) {
	param := StrategyParameter{
		ID:             "param-123",
		SkillID:        "scalping",
		Symbol:         "BTC/USDT",
		ParameterName:  "stop_loss_pct",
		ParameterValue: 0.001,
		MinValue:       0.0005,
		MaxValue:       0.002,
		IsActive:       true,
		Confidence:     0.8,
		SampleSize:     50,
	}

	assert.True(t, param.IsActive)
	assert.Greater(t, param.Confidence, 0.5)
	assert.Greater(t, param.SampleSize, 10)
}
