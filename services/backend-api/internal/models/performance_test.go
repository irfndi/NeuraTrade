package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
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
		SizePercent:      decimal.NewFromFloat(10.0),
		EntryPrice:       decimal.NewFromFloat(50000.0),
		StopLoss:         decimal.NewFromFloat(49500.0),
		TakeProfit:       decimal.NewFromFloat(51000.0),
		Confidence:       decimal.NewFromFloat(0.85),
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
		EntryPrice:          decimal.NewFromFloat(3000.0),
		ExitPrice:           decimal.NewFromFloat(3105.0),
		Size:                decimal.NewFromFloat(1000.0),
		PnL:                 decimal.NewFromFloat(104998.0),
		PnLPercent:          decimal.NewFromFloat(3.5),
		Fees:                decimal.NewFromFloat(2.0),
		HoldDurationSeconds: 120,
		Outcome:             "win",
		ExitReason:          "take_profit",
	}

	assert.Equal(t, "win", outcome.Outcome)
	assert.Equal(t, "take_profit", outcome.ExitReason)
	assert.Equal(t, decimal.NewFromFloat(104998.0), outcome.PnL)
	assert.True(t, outcome.PnLPercent.GreaterThan(decimal.NewFromFloat(3.0)))
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
		ParameterValue: decimal.NewFromFloat(0.001),
		MinValue:       decimal.NewFromFloat(0.0005),
		MaxValue:       decimal.NewFromFloat(0.002),
		IsActive:       true,
		Confidence:     decimal.NewFromFloat(0.8),
		SampleSize:     50,
	}

	assert.True(t, param.IsActive)
	assert.True(t, param.Confidence.GreaterThan(decimal.NewFromFloat(0.5)))
	assert.Greater(t, param.SampleSize, 10)
}
