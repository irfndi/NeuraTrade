package trading

import (
	"encoding/json"
	"fmt"

	"github.com/irfndi/goflux/pkg/indicators"
	"github.com/irfndi/goflux/pkg/series"
)

type StrategyFactory func(ts *series.TimeSeries, params map[string]interface{}) Strategy

type NamedStrategy struct {
	Name    string
	Factory StrategyFactory
}

type StrategyRegistry struct {
	strategies map[string]NamedStrategy
}

func NewStrategyRegistry() *StrategyRegistry {
	registry := &StrategyRegistry{
		strategies: make(map[string]NamedStrategy),
	}
	registry.registerDefaults()
	return registry
}

func (sr *StrategyRegistry) Register(name string, factory StrategyFactory) {
	sr.strategies[name] = NamedStrategy{
		Name:    name,
		Factory: factory,
	}
}

func (sr *StrategyRegistry) Lookup(name string) (*NamedStrategy, bool) {
	strategy, ok := sr.strategies[name]
	return &strategy, ok
}

func (sr *StrategyRegistry) Instantiate(name string, ts *series.TimeSeries, params map[string]interface{}) (Strategy, error) {
	strategy, ok := sr.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("strategy %s not found", name)
	}
	return strategy.Factory(ts, params), nil
}

func (sr *StrategyRegistry) List() []string {
	names := make([]string, 0, len(sr.strategies))
	for name := range sr.strategies {
		names = append(names, name)
	}
	return names
}

func (sr *StrategyRegistry) registerDefaults() {
	sr.Register("sma_cross_fast", createSMACrossFastStrategy)
	sr.Register("ema_cross_fast", createEMACrossFastStrategy)
	sr.Register("rsi_overbought_oversold", createRSIStrategy)
	sr.Register("macd_cross", createMACDStrategy)
	sr.Register("bollinger_bounce", createBollingerStrategy)
	sr.Register("supertrend", createSuperTrendStrategy)
	sr.Register("adx_trending", createADXStrategy)
}

func createSMACrossFastStrategy(ts *series.TimeSeries, params map[string]interface{}) Strategy {
	fastPeriod := int(getParam(params, "fast_period", 5))
	slowPeriod := int(getParam(params, "slow_period", 20))
	overbought := getParam(params, "overbought", 70)
	oversold := getParam(params, "oversold", 30)

	closeInd := indicators.NewClosePriceIndicator(ts)
	fastMA := indicators.NewSimpleMovingAverage(closeInd, fastPeriod)
	slowMA := indicators.NewSimpleMovingAverage(closeInd, slowPeriod)
	rsi := indicators.NewRelativeStrengthIndexIndicator(closeInd, 14)

	entryRule := And(
		NewCrossUpIndicatorRule(fastMA, slowMA),
		NewUnderIndicatorRule(rsi, indicators.NewConstantIndicator(oversold)),
	)
	exitRule := Or(
		NewCrossDownIndicatorRule(fastMA, slowMA),
		NewOverIndicatorRule(rsi, indicators.NewConstantIndicator(overbought)),
	)

	return RuleStrategy{
		EntryRule:      entryRule,
		ExitRule:       exitRule,
		UnstablePeriod: slowPeriod,
	}
}

func createEMACrossFastStrategy(ts *series.TimeSeries, params map[string]interface{}) Strategy {
	fastPeriod := int(getParam(params, "fast_period", 12))
	slowPeriod := int(getParam(params, "slow_period", 26))

	closeInd := indicators.NewClosePriceIndicator(ts)
	fastEMA := indicators.NewEMAIndicator(closeInd, fastPeriod)
	slowEMA := indicators.NewEMAIndicator(closeInd, slowPeriod)

	entryRule := NewCrossUpIndicatorRule(fastEMA, slowEMA)
	exitRule := NewCrossDownIndicatorRule(fastEMA, slowEMA)

	return RuleStrategy{
		EntryRule:      entryRule,
		ExitRule:       exitRule,
		UnstablePeriod: slowPeriod,
	}
}

func createRSIStrategy(ts *series.TimeSeries, params map[string]interface{}) Strategy {
	period := int(getParam(params, "period", 14))
	overbought := getParam(params, "overbought", 70)
	oversold := getParam(params, "oversold", 30)

	closeInd := indicators.NewClosePriceIndicator(ts)
	rsi := indicators.NewRelativeStrengthIndexIndicator(closeInd, period)

	entryRule := NewUnderIndicatorRule(rsi, indicators.NewConstantIndicator(oversold))
	exitRule := NewOverIndicatorRule(rsi, indicators.NewConstantIndicator(overbought))

	return RuleStrategy{
		EntryRule:      entryRule,
		ExitRule:       exitRule,
		UnstablePeriod: period,
	}
}

func createMACDStrategy(ts *series.TimeSeries, params map[string]interface{}) Strategy {
	fastPeriod := int(getParam(params, "fast_period", 12))
	slowPeriod := int(getParam(params, "slow_period", 26))
	signalPeriod := int(getParam(params, "signal_period", 9))

	closeInd := indicators.NewClosePriceIndicator(ts)
	macd := indicators.NewMACDIndicator(closeInd, fastPeriod, slowPeriod)
	signal := indicators.NewEMAIndicator(macd, signalPeriod)

	entryRule := NewCrossUpIndicatorRule(macd, signal)
	exitRule := NewCrossDownIndicatorRule(macd, signal)

	return RuleStrategy{
		EntryRule:      entryRule,
		ExitRule:       exitRule,
		UnstablePeriod: slowPeriod + signalPeriod,
	}
}

func createBollingerStrategy(ts *series.TimeSeries, params map[string]interface{}) Strategy {
	period := int(getParam(params, "period", 20))
	stdDev := getParam(params, "stddev", 2.0)

	closeInd := indicators.NewClosePriceIndicator(ts)
	bbUpper := indicators.NewBollingerUpperBandIndicator(closeInd, period, stdDev)
	bbLower := indicators.NewBollingerLowerBandIndicator(closeInd, period, stdDev)

	entryRule := NewCrossUpIndicatorRule(closeInd, bbLower)
	exitRule := NewCrossDownIndicatorRule(closeInd, bbUpper)

	return RuleStrategy{
		EntryRule:      entryRule,
		ExitRule:       exitRule,
		UnstablePeriod: period,
	}
}

func createSuperTrendStrategy(ts *series.TimeSeries, params map[string]interface{}) Strategy {
	period := int(getParam(params, "period", 10))
	multiplier := getParam(params, "multiplier", 3.0)

	st := indicators.NewSuperTrendIndicator(ts, period, multiplier)
	stSignal := indicators.NewSupertrendSignal(st)
	stRule := NewSignalRule(stSignal)

	return RuleStrategy{
		EntryRule:      stRule,
		ExitRule:       Not(stRule),
		UnstablePeriod: period,
	}
}

func createADXStrategy(ts *series.TimeSeries, params map[string]interface{}) Strategy {
	period := int(getParam(params, "period", 14))
	threshold := getParam(params, "threshold", 25)

	adx := indicators.NewADXIndicator(ts, period)
	st := indicators.NewSuperTrendIndicator(ts, period, 3.0)
	stRule := NewSignalRule(indicators.NewSupertrendSignal(st))

	entryRule := And(
		NewOverIndicatorRule(adx, indicators.NewConstantIndicator(threshold)),
		stRule,
	)
	exitRule := Not(stRule)

	return RuleStrategy{
		EntryRule:      entryRule,
		ExitRule:       exitRule,
		UnstablePeriod: period,
	}
}

func getParam(params map[string]interface{}, key string, defaultVal float64) float64 {
	if params == nil {
		return defaultVal
	}
	if val, ok := params[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case int:
			return float64(v)
		}
	}
	return defaultVal
}

type StrategyConfig struct {
	Name   string                 `json:"name"`
	Params map[string]interface{} `json:"params"`
}

func (sc *StrategyConfig) ToStrategy(ts *series.TimeSeries) (Strategy, error) {
	registry := NewStrategyRegistry()
	return registry.Instantiate(sc.Name, ts, sc.Params)
}

func SerializeStrategy(name string, params map[string]interface{}) ([]byte, error) {
	config := StrategyConfig{
		Name:   name,
		Params: params,
	}
	return json.Marshal(config)
}

func DeserializeStrategy(data []byte, ts *series.TimeSeries) (Strategy, error) {
	var config StrategyConfig
	err := json.Unmarshal(data, &config)
	if err != nil {
		return nil, err
	}

	return config.ToStrategy(ts)
}
