package indicators

import (
	"fmt"
	"time"

	"github.com/irfndi/neuratrade/internal/talib"
	"github.com/shopspring/decimal"
)

// GoFluxAdapter implements IndicatorProvider using the GoFlux library.
type GoFluxAdapter struct {
	name    string
	version string
}

// NewGoFluxAdapter creates a new GoFluxAdapter.
func NewGoFluxAdapter() *GoFluxAdapter {
	return &GoFluxAdapter{
		name:    "goflux",
		version: "0.0.4",
	}
}

func (a *GoFluxAdapter) Name() string {
	return a.name
}

func (a *GoFluxAdapter) Version() string {
	return a.version
}

func (a *GoFluxAdapter) SMA(prices []decimal.Decimal, period int) []decimal.Decimal {
	floatPrices := decimalsToFloats(prices)
	result := talib.Sma(floatPrices, period)
	return floatsToDecimals(result)
}

func (a *GoFluxAdapter) EMA(prices []decimal.Decimal, period int) []decimal.Decimal {
	floatPrices := decimalsToFloats(prices)
	result := talib.Ema(floatPrices, period)
	return floatsToDecimals(result)
}

func (a *GoFluxAdapter) RSI(prices []decimal.Decimal, period int) []decimal.Decimal {
	floatPrices := decimalsToFloats(prices)
	result := talib.Rsi(floatPrices, period)
	return floatsToDecimals(result)
}

func (a *GoFluxAdapter) Stochastic(high, low, close []decimal.Decimal, kPeriod, dPeriod int) (k, d []decimal.Decimal) {
	floatHigh := decimalsToFloats(high)
	floatLow := decimalsToFloats(low)
	floatClose := decimalsToFloats(close)

	kResult, dResult := talib.StochF(floatHigh, floatLow, floatClose, kPeriod, dPeriod, 0)
	return floatsToDecimals(kResult), floatsToDecimals(dResult)
}

func (a *GoFluxAdapter) MACD(prices []decimal.Decimal, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram []decimal.Decimal) {
	floatPrices := decimalsToFloats(prices)
	macdResult, signalResult, histResult := talib.Macd(floatPrices, fastPeriod, slowPeriod, signalPeriod)
	return floatsToDecimals(macdResult), floatsToDecimals(signalResult), floatsToDecimals(histResult)
}

func (a *GoFluxAdapter) BollingerBands(prices []decimal.Decimal, period int, stdDev float64) (upper, middle, lower []decimal.Decimal) {
	floatPrices := decimalsToFloats(prices)
	upperResult, middleResult, lowerResult := talib.BBands(floatPrices, period, stdDev, stdDev, 0)
	return floatsToDecimals(upperResult), floatsToDecimals(middleResult), floatsToDecimals(lowerResult)
}

func (a *GoFluxAdapter) ATR(high, low, close []decimal.Decimal, period int) []decimal.Decimal {
	floatHigh := decimalsToFloats(high)
	floatLow := decimalsToFloats(low)
	floatClose := decimalsToFloats(close)
	result := talib.Atr(floatHigh, floatLow, floatClose, period)
	return floatsToDecimals(result)
}

func (a *GoFluxAdapter) OBV(prices, volumes []decimal.Decimal) []decimal.Decimal {
	floatPrices := decimalsToFloats(prices)
	floatVolumes := decimalsToFloats(volumes)
	result := talib.Obv(floatPrices, floatVolumes)
	return floatsToDecimals(result)
}

func (a *GoFluxAdapter) VWAP(high, low, close, volume []decimal.Decimal) []decimal.Decimal {
	n := len(close)
	if n == 0 || len(high) != n || len(low) != n || len(volume) != n {
		return nil
	}

	result := make([]decimal.Decimal, n)
	cumulativeTPV := decimal.Zero
	cumulativeVolume := decimal.Zero

	for i := 0; i < n; i++ {
		tp := high[i].Add(low[i]).Add(close[i]).Div(decimal.NewFromInt(3))
		tpv := tp.Mul(volume[i])
		cumulativeTPV = cumulativeTPV.Add(tpv)
		cumulativeVolume = cumulativeVolume.Add(volume[i])

		if !cumulativeVolume.IsZero() {
			result[i] = cumulativeTPV.Div(cumulativeVolume)
		}
	}

	return result
}

// CalculateIndicator calculates a specific indicator by name.
func (a *GoFluxAdapter) CalculateIndicator(name string, data *OHLCVData, params map[string]any) (*IndicatorResult, error) {
	if err := data.Validate(); err != nil {
		return nil, err
	}

	switch name {
	case "SMA":
		period := getIntParam(params, "period", 20)
		values := a.SMA(data.Close, period)
		return &IndicatorResult{
			Name:       fmt.Sprintf("SMA_%d", period),
			Type:       IndicatorTypeTrend,
			Values:     values,
			Signal:     SignalHold,
			Timestamp:  time.Now(),
			Parameters: map[string]any{"period": period},
		}, nil

	case "EMA":
		period := getIntParam(params, "period", 20)
		values := a.EMA(data.Close, period)
		return &IndicatorResult{
			Name:       fmt.Sprintf("EMA_%d", period),
			Type:       IndicatorTypeTrend,
			Values:     values,
			Signal:     SignalHold,
			Timestamp:  time.Now(),
			Parameters: map[string]any{"period": period},
		}, nil

	case "RSI":
		period := getIntParam(params, "period", 14)
		values := a.RSI(data.Close, period)
		signal := a.analyzeRSI(values)
		return &IndicatorResult{
			Name:       fmt.Sprintf("RSI_%d", period),
			Type:       IndicatorTypeMomentum,
			Values:     values,
			Signal:     signal,
			Timestamp:  time.Now(),
			Parameters: map[string]any{"period": period},
		}, nil

	case "MACD":
		fast := getIntParam(params, "fast_period", 12)
		slow := getIntParam(params, "slow_period", 26)
		signalPeriod := getIntParam(params, "signal_period", 9)
		macd, signal, hist := a.MACD(data.Close, fast, slow, signalPeriod)
		return &IndicatorResult{
			Name:       "MACD",
			Type:       IndicatorTypeTrend,
			Values:     macd,
			Signal:     SignalHold,
			Timestamp:  time.Now(),
			Parameters: map[string]any{"fast_period": fast, "slow_period": slow, "signal_period": signalPeriod},
			Metadata:   map[string]any{"signal": signal, "histogram": hist},
		}, nil

	case "BollingerBands", "BB":
		period := getIntParam(params, "period", 20)
		stdDev := getFloatParam(params, "std_dev", 2.0)
		upper, middle, lower := a.BollingerBands(data.Close, period, stdDev)
		return &IndicatorResult{
			Name:       "BB",
			Type:       IndicatorTypeVolatility,
			Values:     middle,
			Signal:     SignalHold,
			Timestamp:  time.Now(),
			Parameters: map[string]any{"period": period, "std_dev": stdDev},
			Metadata:   map[string]any{"upper": upper, "lower": lower},
		}, nil

	case "ATR":
		period := getIntParam(params, "period", 14)
		values := a.ATR(data.High, data.Low, data.Close, period)
		return &IndicatorResult{
			Name:       fmt.Sprintf("ATR_%d", period),
			Type:       IndicatorTypeVolatility,
			Values:     values,
			Signal:     SignalHold,
			Timestamp:  time.Now(),
			Parameters: map[string]any{"period": period},
		}, nil

	case "Stochastic", "STOCH":
		kPeriod := getIntParam(params, "k_period", 14)
		dPeriod := getIntParam(params, "d_period", 3)
		k, d := a.Stochastic(data.High, data.Low, data.Close, kPeriod, dPeriod)
		signal := a.analyzeStochastic(k)
		return &IndicatorResult{
			Name:       "STOCH",
			Type:       IndicatorTypeMomentum,
			Values:     k,
			Signal:     signal,
			Timestamp:  time.Now(),
			Parameters: map[string]any{"k_period": kPeriod, "d_period": dPeriod},
			Metadata:   map[string]any{"d": d},
		}, nil

	case "OBV":
		values := a.OBV(data.Close, data.Volume)
		return &IndicatorResult{
			Name:      "OBV",
			Type:      IndicatorTypeVolume,
			Values:    values,
			Signal:    SignalHold,
			Timestamp: time.Now(),
		}, nil

	case "VWAP":
		values := a.VWAP(data.High, data.Low, data.Close, data.Volume)
		return &IndicatorResult{
			Name:      "VWAP",
			Type:      IndicatorTypeVolume,
			Values:    values,
			Signal:    SignalHold,
			Timestamp: time.Now(),
		}, nil

	default:
		return nil, NewIndicatorError(name, "unknown indicator", nil)
	}
}

func (a *GoFluxAdapter) analyzeRSI(values []decimal.Decimal) SignalType {
	if len(values) == 0 {
		return SignalHold
	}

	last := values[len(values)-1]
	oversold := decimal.NewFromFloat(30)
	overbought := decimal.NewFromFloat(70)

	if last.LessThan(oversold) {
		return SignalBuy
	}
	if last.GreaterThan(overbought) {
		return SignalSell
	}
	return SignalHold
}

func (a *GoFluxAdapter) analyzeStochastic(k []decimal.Decimal) SignalType {
	if len(k) == 0 {
		return SignalHold
	}

	last := k[len(k)-1]
	oversold := decimal.NewFromFloat(20)
	overbought := decimal.NewFromFloat(80)

	if last.LessThan(oversold) {
		return SignalBuy
	}
	if last.GreaterThan(overbought) {
		return SignalSell
	}
	return SignalHold
}

// GetIndicatorMetadata returns metadata for all supported indicators.
func (a *GoFluxAdapter) GetIndicatorMetadata() []IndicatorMetadata {
	return []IndicatorMetadata{
		{
			Name:        "SMA",
			Type:        IndicatorTypeTrend,
			Description: "Simple Moving Average",
			Parameters: []Parameter{
				{Name: "period", Type: "int", Default: 20, Min: 1, Description: "Number of periods"},
			},
		},
		{
			Name:        "EMA",
			Type:        IndicatorTypeTrend,
			Description: "Exponential Moving Average",
			Parameters: []Parameter{
				{Name: "period", Type: "int", Default: 20, Min: 1, Description: "Number of periods"},
			},
		},
		{
			Name:        "RSI",
			Type:        IndicatorTypeMomentum,
			Description: "Relative Strength Index",
			Parameters: []Parameter{
				{Name: "period", Type: "int", Default: 14, Min: 1, Description: "Number of periods"},
			},
		},
		{
			Name:        "MACD",
			Type:        IndicatorTypeTrend,
			Description: "Moving Average Convergence Divergence",
			Parameters: []Parameter{
				{Name: "fast_period", Type: "int", Default: 12, Min: 1},
				{Name: "slow_period", Type: "int", Default: 26, Min: 1},
				{Name: "signal_period", Type: "int", Default: 9, Min: 1},
			},
		},
		{
			Name:        "BollingerBands",
			Type:        IndicatorTypeVolatility,
			Description: "Bollinger Bands",
			Parameters: []Parameter{
				{Name: "period", Type: "int", Default: 20, Min: 1},
				{Name: "std_dev", Type: "float", Default: 2.0, Min: 0.1, Max: 5.0},
			},
		},
		{
			Name:        "ATR",
			Type:        IndicatorTypeVolatility,
			Description: "Average True Range",
			Parameters: []Parameter{
				{Name: "period", Type: "int", Default: 14, Min: 1},
			},
		},
		{
			Name:        "Stochastic",
			Type:        IndicatorTypeMomentum,
			Description: "Stochastic Oscillator",
			Parameters: []Parameter{
				{Name: "k_period", Type: "int", Default: 14, Min: 1},
				{Name: "d_period", Type: "int", Default: 3, Min: 1},
			},
		},
		{
			Name:        "OBV",
			Type:        IndicatorTypeVolume,
			Description: "On-Balance Volume",
			Parameters:  []Parameter{},
		},
		{
			Name:        "VWAP",
			Type:        IndicatorTypeVolume,
			Description: "Volume Weighted Average Price",
			Parameters:  []Parameter{},
		},
	}
}
