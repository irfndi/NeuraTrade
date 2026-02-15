package services

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestDecisionRecord_ToDecisionJournal(t *testing.T) {
	decision := &DecisionRecord{
		SessionID:        "session-123",
		Symbol:           "BTC/USDT",
		Exchange:         "binance",
		SkillID:          "scalping",
		DecisionType:     "entry",
		Action:           "open_long",
		Side:             SideLong,
		SizePercent:      decimal.NewFromFloat(10.0),
		EntryPrice:       decimal.NewFromFloat(50000.0),
		StopLoss:         decimal.NewFromFloat(49500.0),
		TakeProfit:       decimal.NewFromFloat(51000.0),
		Confidence:       decimal.NewFromFloat(0.85),
		Reasoning:        "Bullish trend detected",
		RegimeTrend:      "bullish",
		RegimeVolatility: "normal",
		MarketConditions: map[string]interface{}{
			"volume": 1000000.0,
		},
		SignalsUsed: []interface{}{
			map[string]interface{}{"name": "RSI", "value": 35.0},
		},
	}

	assert.Equal(t, "session-123", decision.SessionID)
	assert.Equal(t, "BTC/USDT", decision.Symbol)
	assert.Equal(t, "scalping", decision.SkillID)
	assert.Equal(t, SideLong, decision.Side)
	assert.Equal(t, decimal.NewFromFloat(0.85), decision.Confidence)
}

func TestOutcomeRecord_ToTradeOutcome(t *testing.T) {
	outcome := &OutcomeRecord{
		DecisionJournalID:   "decision-123",
		Symbol:              "ETH/USDT",
		Exchange:            "binance",
		SkillID:             "scalping",
		Side:                "long",
		EntryPrice:          decimal.NewFromFloat(3000.0),
		ExitPrice:           decimal.NewFromFloat(3015.0),
		Size:                decimal.NewFromFloat(1000.0),
		PnL:                 decimal.NewFromFloat(15.0),
		PnLPercent:          decimal.NewFromFloat(0.5),
		Fees:                decimal.NewFromFloat(1.5),
		HoldDurationSeconds: 120,
		Outcome:             "win",
		ExitReason:          "take_profit",
		RegimeAtEntry:       "bullish",
		RegimeAtExit:        "bullish",
		VolatilityAtEntry:   decimal.NewFromFloat(0.3),
		VolatilityAtExit:    decimal.NewFromFloat(0.35),
	}

	assert.Equal(t, "decision-123", outcome.DecisionJournalID)
	assert.Equal(t, "win", outcome.Outcome)
	assert.Equal(t, "take_profit", outcome.ExitReason)
	assert.Equal(t, decimal.NewFromFloat(15.0), outcome.PnL)
}

func TestGenerateID(t *testing.T) {
	id1, err1 := generateID()
	assert.NoError(t, err1)
	id2, err2 := generateID()
	assert.NoError(t, err2)

	assert.NotEqual(t, id1, id2)
	assert.Len(t, id1, 16)
}

func TestPerformanceFeedbackConfig_Defaults(t *testing.T) {
	config := PerformanceFeedbackConfig{
		EnableDecisionJournal:  true,
		EnableOutcomeTracking:  true,
		EnablePatternDetection: true,
		EnableParameterTuning:  true,
		MinSampleSizeForTuning: 10,
	}

	assert.True(t, config.EnableDecisionJournal)
	assert.True(t, config.EnableOutcomeTracking)
	assert.True(t, config.EnablePatternDetection)
	assert.True(t, config.EnableParameterTuning)
	assert.Equal(t, 10, config.MinSampleSizeForTuning)
}
