package trading_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/testutils"
	"github.com/irfndi/goflux/pkg/trading"
)

func TestStrategyRegistry(t *testing.T) {
	ts := testutils.MockTimeSeriesFl(100, 101, 102, 103, 104, 105)
	registry := trading.NewStrategyRegistry()

	t.Run("List strategies", func(t *testing.T) {
		list := registry.List()
		assert.Contains(t, list, "sma_cross_fast")
		assert.Contains(t, list, "ema_cross_fast")
		assert.Contains(t, list, "rsi_overbought_oversold")
		assert.Contains(t, list, "macd_cross")
		assert.Contains(t, list, "bollinger_bounce")
		assert.Contains(t, list, "supertrend")
		assert.Contains(t, list, "adx_trending")
	})

	t.Run("Instantiate sma_cross_fast", func(t *testing.T) {
		params := map[string]interface{}{
			"fast_period": 2,
			"slow_period": 5,
		}
		strategy, err := registry.Instantiate("sma_cross_fast", ts, params)
		assert.NoError(t, err)
		assert.NotNil(t, strategy)
	})

	t.Run("Instantiate all default strategies", func(t *testing.T) {
		cases := []struct {
			name           string
			params         map[string]interface{}
			unstablePeriod int
		}{
			{name: "sma_cross_fast", params: nil, unstablePeriod: 20},
			{name: "ema_cross_fast", params: nil, unstablePeriod: 26},
			{name: "rsi_overbought_oversold", params: nil, unstablePeriod: 14},
			{name: "macd_cross", params: nil, unstablePeriod: 35},
			{name: "bollinger_bounce", params: nil, unstablePeriod: 20},
			{name: "supertrend", params: nil, unstablePeriod: 10},
			{name: "adx_trending", params: nil, unstablePeriod: 14},
		}

		for _, tc := range cases {
			strategy, err := registry.Instantiate(tc.name, ts, tc.params)
			assert.NoError(t, err)
			assert.NotNil(t, strategy)

			rs, ok := strategy.(trading.RuleStrategy)
			assert.True(t, ok, tc.name)
			assert.NotNil(t, rs.EntryRule, tc.name)
			assert.NotNil(t, rs.ExitRule, tc.name)
			assert.Equal(t, tc.unstablePeriod, rs.UnstablePeriod, tc.name)
		}
	})

	t.Run("Instantiate strategies with custom params", func(t *testing.T) {
		cases := []struct {
			name           string
			params         map[string]interface{}
			unstablePeriod int
		}{
			{
				name: "macd_cross",
				params: map[string]interface{}{
					"fast_period":   3,
					"slow_period":   7,
					"signal_period": 2,
				},
				unstablePeriod: 9,
			},
			{
				name: "bollinger_bounce",
				params: map[string]interface{}{
					"period": 6,
					"stddev": 2.5,
				},
				unstablePeriod: 6,
			},
			{
				name: "supertrend",
				params: map[string]interface{}{
					"period":     7,
					"multiplier": 4.0,
				},
				unstablePeriod: 7,
			},
			{
				name: "adx_trending",
				params: map[string]interface{}{
					"period":    9,
					"threshold": 30,
				},
				unstablePeriod: 9,
			},
		}

		for _, tc := range cases {
			strategy, err := registry.Instantiate(tc.name, ts, tc.params)
			assert.NoError(t, err)
			assert.NotNil(t, strategy)

			rs, ok := strategy.(trading.RuleStrategy)
			assert.True(t, ok, tc.name)
			assert.Equal(t, tc.unstablePeriod, rs.UnstablePeriod, tc.name)
		}
	})

	t.Run("Serialization", func(t *testing.T) {
		params := map[string]interface{}{
			"fast_period": 2,
			"slow_period": 5,
		}
		data, err := trading.SerializeStrategy("sma_cross_fast", params)
		assert.NoError(t, err)

		strategy, err := trading.DeserializeStrategy(data, ts)
		assert.NoError(t, err)
		assert.NotNil(t, strategy)
	})

	t.Run("Serialization for all default strategies", func(t *testing.T) {
		names := []string{
			"sma_cross_fast",
			"ema_cross_fast",
			"rsi_overbought_oversold",
			"macd_cross",
			"bollinger_bounce",
			"supertrend",
			"adx_trending",
		}

		for _, name := range names {
			data, err := trading.SerializeStrategy(name, nil)
			assert.NoError(t, err, name)

			strategy, err := trading.DeserializeStrategy(data, ts)
			assert.NoError(t, err, name)
			assert.NotNil(t, strategy, name)
		}
	})
}

func TestVoteRule(t *testing.T) {
	record := trading.NewTradingRecord()

	r1 := trading.PositionNewRule{}
	r2 := trading.PositionNewRule{}
	r3 := trading.PositionOpenRule{}

	vote := trading.Vote(2, r1, r2, r3)

	// Since r1 and r2 are satisfied (Position is New), count = 2 >= threshold 2
	assert.True(t, vote.IsSatisfied(0, record))

	vote3 := trading.Vote(3, r1, r2, r3)
	assert.False(t, vote3.IsSatisfied(0, record))
}
