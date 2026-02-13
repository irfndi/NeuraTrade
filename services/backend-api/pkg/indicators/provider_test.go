package indicators

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockProvider struct {
	sma  []decimal.Decimal
	ema  []decimal.Decimal
	rsi  []decimal.Decimal
	macd []decimal.Decimal
	atr  []decimal.Decimal
	obv  []decimal.Decimal
	vwap []decimal.Decimal
}

func (m *mockProvider) SMA(prices []decimal.Decimal, period int) []decimal.Decimal {
	return m.sma
}

func (m *mockProvider) EMA(prices []decimal.Decimal, period int) []decimal.Decimal {
	return m.ema
}

func (m *mockProvider) RSI(prices []decimal.Decimal, period int) []decimal.Decimal {
	return m.rsi
}

func (m *mockProvider) Stochastic(high, low, close []decimal.Decimal, kPeriod, dPeriod int) (k, d []decimal.Decimal) {
	return []decimal.Decimal{decimal.NewFromFloat(50)}, []decimal.Decimal{decimal.NewFromFloat(50)}
}

func (m *mockProvider) MACD(prices []decimal.Decimal, fastPeriod, slowPeriod, signalPeriod int) (macd, signal, histogram []decimal.Decimal) {
	return m.macd, m.macd, m.macd
}

func (m *mockProvider) BollingerBands(prices []decimal.Decimal, period int, stdDev float64) (upper, middle, lower []decimal.Decimal) {
	return m.sma, m.sma, m.sma
}

func (m *mockProvider) ATR(high, low, close []decimal.Decimal, period int) []decimal.Decimal {
	return m.atr
}

func (m *mockProvider) OBV(prices, volumes []decimal.Decimal) []decimal.Decimal {
	return m.obv
}

func (m *mockProvider) VWAP(high, low, close, volume []decimal.Decimal) []decimal.Decimal {
	return m.vwap
}

func (m *mockProvider) Name() string    { return "mock" }
func (m *mockProvider) Version() string { return "1.0.0" }

func TestOHLCVData_Validate(t *testing.T) {
	t.Run("valid data", func(t *testing.T) {
		data := &OHLCVData{
			Symbol:   "BTC/USDT",
			Exchange: "binance",
			Close:    make([]decimal.Decimal, 100),
			Open:     make([]decimal.Decimal, 100),
			High:     make([]decimal.Decimal, 100),
			Low:      make([]decimal.Decimal, 100),
			Volume:   make([]decimal.Decimal, 100),
		}
		assert.NoError(t, data.Validate())
	})

	t.Run("empty data", func(t *testing.T) {
		data := &OHLCVData{}
		assert.Equal(t, ErrEmptyData, data.Validate())
	})

	t.Run("inconsistent lengths", func(t *testing.T) {
		data := &OHLCVData{
			Close:  make([]decimal.Decimal, 100),
			Open:   make([]decimal.Decimal, 50),
			High:   make([]decimal.Decimal, 100),
			Low:    make([]decimal.Decimal, 100),
			Volume: make([]decimal.Decimal, 100),
		}
		assert.Equal(t, ErrInconsistentData, data.Validate())
	})
}

func TestOHLCVData_Length(t *testing.T) {
	data := &OHLCVData{
		Close: make([]decimal.Decimal, 100),
	}
	assert.Equal(t, 100, data.Length())
}

func TestOHLCVData_Last(t *testing.T) {
	t.Run("with data", func(t *testing.T) {
		data := &OHLCVData{
			Open:   []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2)},
			High:   []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(3)},
			Low:    []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(1)},
			Close:  []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2)},
			Volume: []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(100)},
		}
		o, h, l, c, v, ok := data.Last()
		assert.True(t, ok)
		assert.Equal(t, decimal.NewFromInt(2), o)
		assert.Equal(t, decimal.NewFromInt(3), h)
		assert.Equal(t, decimal.NewFromInt(1), l)
		assert.Equal(t, decimal.NewFromInt(2), c)
		assert.Equal(t, decimal.NewFromInt(100), v)
	})

	t.Run("empty data", func(t *testing.T) {
		data := &OHLCVData{}
		_, _, _, _, _, ok := data.Last()
		assert.False(t, ok)
	})
}

func TestOHLCVData_Slice(t *testing.T) {
	data := &OHLCVData{
		Symbol:   "BTC/USDT",
		Exchange: "binance",
		Close:    []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3), decimal.NewFromInt(4)},
		Open:     []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3), decimal.NewFromInt(4)},
		High:     []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3), decimal.NewFromInt(4)},
		Low:      []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3), decimal.NewFromInt(4)},
		Volume:   []decimal.Decimal{decimal.NewFromInt(1), decimal.NewFromInt(2), decimal.NewFromInt(3), decimal.NewFromInt(4)},
	}

	sliced := data.Slice(1, 3)
	assert.Equal(t, 2, sliced.Length())
	assert.Equal(t, "BTC/USDT", sliced.Symbol)
	assert.Equal(t, "binance", sliced.Exchange)
}

func TestIndicatorConfig_Default(t *testing.T) {
	config := DefaultIndicatorConfig()
	assert.NotNil(t, config)
	assert.Equal(t, []int{10, 20, 50}, config.SMAPeriods)
	assert.Equal(t, []int{12, 26}, config.EMAPeriods)
	assert.Equal(t, 14, config.RSIPeriod)
	assert.Equal(t, 12, config.MACDFast)
	assert.Equal(t, 26, config.MACDSlow)
	assert.Equal(t, 9, config.MACDSignal)
	assert.Equal(t, 20, config.BBPeriod)
	assert.Equal(t, 2.0, config.BBStdDev)
	assert.True(t, config.OBVEnabled)
	assert.True(t, config.VWAPEnabled)
}

func TestIndicatorError(t *testing.T) {
	t.Run("error with cause", func(t *testing.T) {
		err := NewIndicatorError("RSI", "calculation failed", assert.AnError)
		assert.Contains(t, err.Error(), "RSI")
		assert.Contains(t, err.Error(), "calculation failed")
		assert.ErrorIs(t, err, assert.AnError)
	})

	t.Run("error without cause", func(t *testing.T) {
		err := NewIndicatorError("SMA", "invalid period", nil)
		assert.Contains(t, err.Error(), "SMA")
		assert.Contains(t, err.Error(), "invalid period")
	})
}

func TestMultiIndicatorStack_New(t *testing.T) {
	provider := &mockProvider{}
	stack := NewMultiIndicatorStack(provider, nil, nil)
	assert.NotNil(t, stack)
	assert.NotNil(t, stack.config)
	assert.Equal(t, provider, stack.GetProvider())
}

func TestMultiIndicatorStack_Analyze_InsufficientData(t *testing.T) {
	provider := &mockProvider{}
	stack := NewMultiIndicatorStack(provider, nil, nil)

	data := &OHLCVData{
		Symbol:   "BTC/USDT",
		Exchange: "binance",
		Close:    make([]decimal.Decimal, 10),
		Open:     make([]decimal.Decimal, 10),
		High:     make([]decimal.Decimal, 10),
		Low:      make([]decimal.Decimal, 10),
		Volume:   make([]decimal.Decimal, 10),
	}

	_, err := stack.Analyze(context.Background(), data)
	assert.Equal(t, ErrInsufficientData, err)
}

func TestMultiIndicatorStack_Analyze(t *testing.T) {
	n := 100
	provider := &mockProvider{
		sma:  makeSMAValues(n, 100),
		ema:  makeSMAValues(n, 100),
		rsi:  []decimal.Decimal{decimal.NewFromFloat(50)},
		macd: []decimal.Decimal{decimal.Zero},
		atr:  makeSMAValues(n, 1),
		obv:  makeSMAValues(n, 1000),
		vwap: makeSMAValues(n, 100),
	}

	stack := NewMultiIndicatorStack(provider, DefaultIndicatorConfig(), nil)

	data := &OHLCVData{
		Symbol:    "BTC/USDT",
		Exchange:  "binance",
		Timeframe: "1h",
		Close:     makeSMAValues(n, 100),
		Open:      makeSMAValues(n, 100),
		High:      makeSMAValues(n, 101),
		Low:       makeSMAValues(n, 99),
		Volume:    makeSMAValues(n, 1000),
	}

	result, err := stack.Analyze(context.Background(), data)
	require.NoError(t, err)

	assert.Equal(t, "BTC/USDT", result.Symbol)
	assert.Equal(t, "binance", result.Exchange)
	assert.Equal(t, "1h", result.Timeframe)
	assert.NotEmpty(t, result.Indicators)
	assert.NotEmpty(t, result.OverallSignal)
	assert.False(t, result.Confidence.IsZero())
	assert.NotZero(t, result.CalculatedAt)
}

func makeSMAValues(n int, base float64) []decimal.Decimal {
	values := make([]decimal.Decimal, n)
	for i := range n {
		values[i] = decimal.NewFromFloat(base + float64(i)*0.1)
	}
	return values
}

func TestMultiIndicatorStack_AnalyzeSequential(t *testing.T) {
	n := 100
	provider := &mockProvider{
		sma:  make([]decimal.Decimal, n),
		ema:  make([]decimal.Decimal, n),
		rsi:  []decimal.Decimal{decimal.NewFromFloat(50)},
		macd: []decimal.Decimal{decimal.Zero},
		atr:  make([]decimal.Decimal, n),
		obv:  make([]decimal.Decimal, n),
		vwap: make([]decimal.Decimal, n),
	}

	stack := NewMultiIndicatorStack(provider, DefaultIndicatorConfig(), nil)

	data := &OHLCVData{
		Symbol:   "BTC/USDT",
		Exchange: "binance",
		Close:    make([]decimal.Decimal, n),
		Open:     make([]decimal.Decimal, n),
		High:     make([]decimal.Decimal, n),
		Low:      make([]decimal.Decimal, n),
		Volume:   make([]decimal.Decimal, n),
	}

	result, err := stack.AnalyzeSequential(data)
	require.NoError(t, err)
	assert.NotEmpty(t, result.Indicators)
}

func TestMultiIndicatorStack_SetConfig(t *testing.T) {
	provider := &mockProvider{}
	stack := NewMultiIndicatorStack(provider, nil, nil)

	newConfig := &IndicatorConfig{
		RSIPeriod:  21,
		EMAPeriods: []int{9, 21},
	}
	stack.SetConfig(newConfig)

	assert.Equal(t, newConfig, stack.GetConfig())
	assert.Equal(t, 21, stack.GetConfig().RSIPeriod)
}

func TestTalibAdapter_New(t *testing.T) {
	adapter := NewTalibAdapter()
	assert.NotNil(t, adapter)
	assert.Equal(t, "talib", adapter.Name())
	assert.Equal(t, "1.0.0", adapter.Version())
}

func TestTalibAdapter_GetIndicatorMetadata(t *testing.T) {
	adapter := NewTalibAdapter()
	metadata := adapter.GetIndicatorMetadata()

	assert.NotEmpty(t, metadata)

	var foundSMA, foundRSI, foundMACD bool
	for _, m := range metadata {
		switch m.Name {
		case "SMA":
			foundSMA = true
		case "RSI":
			foundRSI = true
		case "MACD":
			foundMACD = true
		}
	}

	assert.True(t, foundSMA)
	assert.True(t, foundRSI)
	assert.True(t, foundMACD)
}

func TestTalibAdapter_CalculateIndicator(t *testing.T) {
	adapter := NewTalibAdapter()

	n := 100
	data := &OHLCVData{
		Symbol:   "BTC/USDT",
		Exchange: "binance",
		Close:    make([]decimal.Decimal, n),
		Open:     make([]decimal.Decimal, n),
		High:     make([]decimal.Decimal, n),
		Low:      make([]decimal.Decimal, n),
		Volume:   make([]decimal.Decimal, n),
	}

	for i := range n {
		price := decimal.NewFromFloat(float64(100 + i))
		data.Close[i] = price
		data.Open[i] = price
		data.High[i] = price.Add(decimal.NewFromInt(1))
		data.Low[i] = price.Sub(decimal.NewFromInt(1))
		data.Volume[i] = decimal.NewFromInt(1000)
	}

	t.Run("SMA", func(t *testing.T) {
		result, err := adapter.CalculateIndicator("SMA", data, map[string]any{"period": 20})
		require.NoError(t, err)
		assert.Equal(t, "SMA_20", result.Name)
		assert.Equal(t, IndicatorTypeTrend, result.Type)
		assert.NotEmpty(t, result.Values)
	})

	t.Run("RSI", func(t *testing.T) {
		result, err := adapter.CalculateIndicator("RSI", data, map[string]any{"period": 14})
		require.NoError(t, err)
		assert.Equal(t, "RSI_14", result.Name)
		assert.Equal(t, IndicatorTypeMomentum, result.Type)
		assert.NotEmpty(t, result.Values)
	})

	t.Run("MACD", func(t *testing.T) {
		result, err := adapter.CalculateIndicator("MACD", data, map[string]any{
			"fast_period":   12,
			"slow_period":   26,
			"signal_period": 9,
		})
		require.NoError(t, err)
		assert.Equal(t, "MACD", result.Name)
		assert.Contains(t, result.Metadata, "signal")
		assert.Contains(t, result.Metadata, "histogram")
	})

	t.Run("unknown indicator", func(t *testing.T) {
		_, err := adapter.CalculateIndicator("UNKNOWN", data, nil)
		assert.Error(t, err)
	})

	t.Run("invalid data", func(t *testing.T) {
		invalidData := &OHLCVData{}
		_, err := adapter.CalculateIndicator("SMA", invalidData, nil)
		assert.Equal(t, ErrEmptyData, err)
	})
}

func TestSignalTypes(t *testing.T) {
	assert.Equal(t, SignalType("buy"), SignalBuy)
	assert.Equal(t, SignalType("sell"), SignalSell)
	assert.Equal(t, SignalType("hold"), SignalHold)
}

func TestIndicatorTypes(t *testing.T) {
	assert.Equal(t, IndicatorType("trend"), IndicatorTypeTrend)
	assert.Equal(t, IndicatorType("momentum"), IndicatorTypeMomentum)
	assert.Equal(t, IndicatorType("volatility"), IndicatorTypeVolatility)
	assert.Equal(t, IndicatorType("volume"), IndicatorTypeVolume)
}

func TestDecimalsToFloats(t *testing.T) {
	decimals := []decimal.Decimal{
		decimal.NewFromFloat(1.5),
		decimal.NewFromFloat(2.5),
		decimal.NewFromFloat(3.5),
	}

	floats := decimalsToFloats(decimals)
	assert.Equal(t, []float64{1.5, 2.5, 3.5}, floats)
}

func TestFloatsToDecimals(t *testing.T) {
	floats := []float64{1.5, 2.5, 3.5}
	decimals := floatsToDecimals(floats)

	assert.Equal(t, 3, len(decimals))
	assert.True(t, decimals[0].Equal(decimal.NewFromFloat(1.5)))
	assert.True(t, decimals[1].Equal(decimal.NewFromFloat(2.5)))
	assert.True(t, decimals[2].Equal(decimal.NewFromFloat(3.5)))
}

func TestFloatsToDecimals_Nil(t *testing.T) {
	assert.Nil(t, floatsToDecimals(nil))
}

func TestGetIntParam(t *testing.T) {
	t.Run("int value", func(t *testing.T) {
		params := map[string]any{"period": 20}
		assert.Equal(t, 20, getIntParam(params, "period", 14))
	})

	t.Run("float64 value", func(t *testing.T) {
		params := map[string]any{"period": float64(20)}
		assert.Equal(t, 20, getIntParam(params, "period", 14))
	})

	t.Run("missing key", func(t *testing.T) {
		params := map[string]any{}
		assert.Equal(t, 14, getIntParam(params, "period", 14))
	})
}

func TestGetFloatParam(t *testing.T) {
	t.Run("float64 value", func(t *testing.T) {
		params := map[string]any{"std_dev": 2.5}
		assert.Equal(t, 2.5, getFloatParam(params, "std_dev", 2.0))
	})

	t.Run("int value", func(t *testing.T) {
		params := map[string]any{"std_dev": 2}
		assert.Equal(t, 2.0, getFloatParam(params, "std_dev", 2.0))
	})

	t.Run("missing key", func(t *testing.T) {
		params := map[string]any{}
		assert.Equal(t, 2.0, getFloatParam(params, "std_dev", 2.0))
	})
}

func TestOHLCVData_ToFloats(t *testing.T) {
	data := &OHLCVData{
		Open:   []decimal.Decimal{decimal.NewFromFloat(100), decimal.NewFromFloat(101)},
		High:   []decimal.Decimal{decimal.NewFromFloat(102), decimal.NewFromFloat(103)},
		Low:    []decimal.Decimal{decimal.NewFromFloat(99), decimal.NewFromFloat(100)},
		Close:  []decimal.Decimal{decimal.NewFromFloat(101), decimal.NewFromFloat(102)},
		Volume: []decimal.Decimal{decimal.NewFromInt(1000), decimal.NewFromInt(1100)},
	}

	open, high, low, close, volume := data.ToFloats()

	assert.Equal(t, []float64{100, 101}, open)
	assert.Equal(t, []float64{102, 103}, high)
	assert.Equal(t, []float64{99, 100}, low)
	assert.Equal(t, []float64{101, 102}, close)
	assert.Equal(t, []float64{1000, 1100}, volume)
}
