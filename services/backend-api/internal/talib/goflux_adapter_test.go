package talib

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSma(t *testing.T) {
	t.Run("valid calculation", func(t *testing.T) {
		prices := []float64{10, 20, 30, 40, 50}
		result := Sma(prices, 3)

		assert.NotNil(t, result)
		assert.Len(t, result, 3)

		// SMA calculations: (10+20+30)/3=20, (20+30+40)/3=30, (30+40+50)/3=40
		assert.InDelta(t, 20.0, result[0], 0.001)
		assert.InDelta(t, 30.0, result[1], 0.001)
		assert.InDelta(t, 40.0, result[2], 0.001)
	})

	t.Run("insufficient data", func(t *testing.T) {
		prices := []float64{10, 20}
		result := Sma(prices, 3)
		assert.Nil(t, result)
	})

	t.Run("empty prices", func(t *testing.T) {
		result := Sma([]float64{}, 3)
		assert.Nil(t, result)
	})
}

func TestEma(t *testing.T) {
	t.Run("valid calculation", func(t *testing.T) {
		prices := []float64{10, 20, 30, 40, 50}
		result := Ema(prices, 3)

		assert.NotNil(t, result)
		assert.Len(t, result, 3)
	})

	t.Run("insufficient data", func(t *testing.T) {
		prices := []float64{10, 20}
		result := Ema(prices, 3)
		assert.Nil(t, result)
	})
}

func TestRsi(t *testing.T) {
	t.Run("valid calculation", func(t *testing.T) {
		prices := []float64{10, 11, 12, 11, 13, 14, 13, 15, 16, 15, 17, 18}
		result := Rsi(prices, 5)

		assert.NotNil(t, result)
		// RSI should return values between 0 and 100
		for _, val := range result {
			assert.True(t, val >= 0 && val <= 100, "RSI should be between 0 and 100")
		}
	})

	t.Run("insufficient data", func(t *testing.T) {
		prices := []float64{10, 20, 30}
		result := Rsi(prices, 5)
		assert.Nil(t, result)
	})
}

func TestMacd(t *testing.T) {
	t.Run("valid calculation", func(t *testing.T) {
		prices := []float64{10, 20, 30, 40, 50, 45, 55, 60, 50, 40, 45, 55, 65, 70, 60}
		macd, signal, histogram := Macd(prices, 3, 5, 2)

		assert.NotNil(t, macd)
		assert.NotNil(t, signal)
		assert.NotNil(t, histogram)
		assert.Equal(t, len(macd), len(signal))
		assert.Equal(t, len(macd), len(histogram))
	})

	t.Run("insufficient data", func(t *testing.T) {
		prices := []float64{10, 20, 30}
		macd, signal, histogram := Macd(prices, 3, 5, 2)
		assert.Nil(t, macd)
		assert.Nil(t, signal)
		assert.Nil(t, histogram)
	})
}

func TestBBands(t *testing.T) {
	t.Run("valid calculation", func(t *testing.T) {
		prices := []float64{10, 20, 30, 40, 50, 45, 55, 60, 50, 40}
		upper, middle, lower := BBands(prices, 5, 2.0, 2.0, 0)

		assert.NotNil(t, upper)
		assert.NotNil(t, middle)
		assert.NotNil(t, lower)
		assert.Equal(t, len(upper), len(middle))
		assert.Equal(t, len(upper), len(lower))

		// Upper band should be >= middle band >= lower band
		for i := range upper {
			assert.True(t, upper[i] >= middle[i], "Upper band should be >= middle band")
			assert.True(t, middle[i] >= lower[i], "Middle band should be >= lower band")
		}
	})

	t.Run("insufficient data", func(t *testing.T) {
		prices := []float64{10, 20}
		upper, middle, lower := BBands(prices, 5, 2.0, 2.0, 0)
		assert.Nil(t, upper)
		assert.Nil(t, middle)
		assert.Nil(t, lower)
	})
}

func TestAtr(t *testing.T) {
	t.Run("valid calculation", func(t *testing.T) {
		high := []float64{15, 25, 35, 45, 55}
		low := []float64{5, 15, 25, 35, 45}
		close := []float64{10, 20, 30, 40, 50}
		result := Atr(high, low, close, 3)

		assert.NotNil(t, result)
		assert.True(t, len(result) > 0)
	})

	t.Run("insufficient data", func(t *testing.T) {
		high := []float64{15, 25}
		low := []float64{5, 15}
		close := []float64{10, 20}
		result := Atr(high, low, close, 3)
		assert.Nil(t, result)
	})
}

func TestStochF(t *testing.T) {
	t.Run("valid calculation", func(t *testing.T) {
		high := []float64{15, 25, 35, 45, 55, 50, 60, 65, 55, 45}
		low := []float64{5, 15, 25, 35, 45, 40, 50, 55, 45, 35}
		close := []float64{10, 20, 30, 40, 50, 45, 55, 60, 50, 40}
		k, d := StochF(high, low, close, 3, 2, 0)

		assert.NotNil(t, k)
		assert.NotNil(t, d)
		assert.Equal(t, len(k), len(d))

		// Stochastic values should be between 0 and 100
		for i := range k {
			assert.True(t, k[i] >= 0 && k[i] <= 100, "Fast %K should be between 0 and 100")
			assert.True(t, d[i] >= 0 && d[i] <= 100, "Fast %D should be between 0 and 100")
		}
	})

	t.Run("insufficient data", func(t *testing.T) {
		high := []float64{15, 25}
		low := []float64{5, 15}
		close := []float64{10, 20}
		k, d := StochF(high, low, close, 3, 2, 0)
		assert.Nil(t, k)
		assert.Nil(t, d)
	})
}

func TestObv(t *testing.T) {
	t.Run("valid calculation", func(t *testing.T) {
		prices := []float64{10, 20, 15, 25, 30}
		volumes := []float64{1000, 2000, 1500, 2500, 3000}
		result := Obv(prices, volumes)

		assert.NotNil(t, result)
		assert.Len(t, result, 5)
	})

	t.Run("mismatched lengths", func(t *testing.T) {
		prices := []float64{10, 20, 30}
		volumes := []float64{1000, 2000}
		result := Obv(prices, volumes)
		assert.Nil(t, result)
	})

	t.Run("empty data", func(t *testing.T) {
		result := Obv([]float64{}, []float64{})
		assert.Nil(t, result)
	})
}

func TestConstants(t *testing.T) {
	assert.Equal(t, 0, SMA)
	assert.Equal(t, 1, EMA)
}
