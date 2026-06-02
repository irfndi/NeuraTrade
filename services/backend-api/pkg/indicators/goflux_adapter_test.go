package indicators

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestGoFluxAdapter_MACD_ArrayAlignment(t *testing.T) {
	adapter := NewGoFluxAdapter()

	t.Run("aligned with sufficient data", func(t *testing.T) {
		prices := make([]decimal.Decimal, 100)
		for i := range prices {
			prices[i] = decimal.NewFromFloat(100 + float64(i)*0.3)
		}

		macd, signal, histogram := adapter.MACD(prices, 12, 26, 9)

		assert.NotNil(t, macd)
		assert.NotNil(t, signal)
		assert.NotNil(t, histogram)
		assert.Equal(t, len(macd), len(signal),
			"MACD and signal arrays must have equal length: macd=%d signal=%d", len(macd), len(signal))
		assert.Equal(t, len(macd), len(histogram),
			"MACD and histogram arrays must have equal length: macd=%d histogram=%d", len(macd), len(histogram))
	})

	t.Run("insufficient data returns nil", func(t *testing.T) {
		prices := make([]decimal.Decimal, 10)
		for i := range prices {
			prices[i] = decimal.NewFromFloat(100)
		}

		macd, signal, histogram := adapter.MACD(prices, 12, 26, 9)
		assert.Nil(t, macd)
		assert.Nil(t, signal)
		assert.Nil(t, histogram)
	})

	t.Run("aligned with varying price series", func(t *testing.T) {
		prices := make([]decimal.Decimal, 200)
		for i := range prices {
			prices[i] = decimal.NewFromFloat(100 + float64(i)*0.5 + float64(i%10)*2)
		}

		macd, signal, histogram := adapter.MACD(prices, 12, 26, 9)

		assert.NotNil(t, macd)
		assert.Equal(t, len(macd), len(signal))
		assert.Equal(t, len(macd), len(histogram))
		assert.True(t, len(macd) > 0)
	})

	t.Run("aligned with small periods", func(t *testing.T) {
		prices := make([]decimal.Decimal, 50)
		for i := range prices {
			prices[i] = decimal.NewFromFloat(100 + float64(i)*0.3)
		}

		macd, signal, histogram := adapter.MACD(prices, 5, 13, 4)

		assert.NotNil(t, macd)
		assert.Equal(t, len(macd), len(signal))
		assert.Equal(t, len(macd), len(histogram))
	})

	t.Run("aligned with identical prices", func(t *testing.T) {
		prices := make([]decimal.Decimal, 100)
		for i := range prices {
			prices[i] = decimal.NewFromFloat(100)
		}

		macd, signal, histogram := adapter.MACD(prices, 12, 26, 9)

		assert.NotNil(t, macd)
		assert.Equal(t, len(macd), len(signal))
		assert.Equal(t, len(macd), len(histogram))
	})
}

func TestGoFluxAdapter_Stochastic_ArrayAlignment(t *testing.T) {
	adapter := NewGoFluxAdapter()

	t.Run("aligned with sufficient data", func(t *testing.T) {
		n := 100
		high := make([]decimal.Decimal, n)
		low := make([]decimal.Decimal, n)
		close := make([]decimal.Decimal, n)

		for i := range n {
			base := decimal.NewFromFloat(100 + float64(i)*0.5)
			high[i] = base.Add(decimal.NewFromInt(2))
			low[i] = base.Sub(decimal.NewFromInt(2))
			close[i] = base
		}

		k, d := adapter.Stochastic(high, low, close, 14, 3)

		assert.NotNil(t, k)
		assert.NotNil(t, d)
		assert.Equal(t, len(k), len(d),
			"Stochastic K and D arrays must have equal length: k=%d d=%d", len(k), len(d))
		assert.True(t, len(k) > 0)
	})

	t.Run("insufficient data returns nil", func(t *testing.T) {
		high := make([]decimal.Decimal, 5)
		low := make([]decimal.Decimal, 5)
		close := make([]decimal.Decimal, 5)

		k, d := adapter.Stochastic(high, low, close, 14, 3)
		assert.Nil(t, k)
		assert.Nil(t, d)
	})

	t.Run("aligned with varying data", func(t *testing.T) {
		n := 150
		high := make([]decimal.Decimal, n)
		low := make([]decimal.Decimal, n)
		close := make([]decimal.Decimal, n)

		for i := range n {
			high[i] = decimal.NewFromFloat(110 + float64(i%5)*5)
			low[i] = decimal.NewFromFloat(90 + float64(i%5)*3)
			close[i] = decimal.NewFromFloat(100 + float64(i)*0.3)
		}

		k, d := adapter.Stochastic(high, low, close, 14, 3)

		assert.NotNil(t, k)
		assert.NotNil(t, d)
		assert.Equal(t, len(k), len(d))
	})

	t.Run("aligned with different period settings", func(t *testing.T) {
		n := 100
		high := make([]decimal.Decimal, n)
		low := make([]decimal.Decimal, n)
		close := make([]decimal.Decimal, n)

		for i := range n {
			high[i] = decimal.NewFromFloat(110 + float64(i%3)*2)
			low[i] = decimal.NewFromFloat(90 + float64(i%3))
			close[i] = decimal.NewFromFloat(100 + float64(i)*0.5)
		}

		k, d := adapter.Stochastic(high, low, close, 7, 3)

		assert.NotNil(t, k)
		assert.NotNil(t, d)
		assert.Equal(t, len(k), len(d))
	})
}

func TestTalibAdapter_MACD_ArrayAlignment(t *testing.T) {
	adapter := NewTalibAdapter()

	prices := make([]decimal.Decimal, 100)
	for i := range prices {
		prices[i] = decimal.NewFromFloat(100 + float64(i)*0.3)
	}

	macd, signal, histogram := adapter.MACD(prices, 12, 26, 9)

	assert.NotNil(t, macd)
	assert.NotNil(t, signal)
	assert.NotNil(t, histogram)
	assert.Equal(t, len(macd), len(signal))
	assert.Equal(t, len(macd), len(histogram))
}

func TestTalibAdapter_Stochastic_ArrayAlignment(t *testing.T) {
	adapter := NewTalibAdapter()

	n := 100
	high := make([]decimal.Decimal, n)
	low := make([]decimal.Decimal, n)
	close := make([]decimal.Decimal, n)

	for i := range n {
		base := decimal.NewFromFloat(100 + float64(i)*0.5)
		high[i] = base.Add(decimal.NewFromInt(2))
		low[i] = base.Sub(decimal.NewFromInt(2))
		close[i] = base
	}

	k, d := adapter.Stochastic(high, low, close, 14, 3)

	assert.NotNil(t, k)
	assert.NotNil(t, d)
	assert.Equal(t, len(k), len(d))
}

func TestAlignThree(t *testing.T) {
	t.Run("all same length", func(t *testing.T) {
		a := []int{1, 2, 3, 4, 5}
		b := []int{10, 20, 30, 40, 50}
		c := []int{100, 200, 300, 400, 500}

		ra, rb, rc := alignThree(a, b, c)
		assert.Equal(t, 5, len(ra))
		assert.Equal(t, 5, len(rb))
		assert.Equal(t, 5, len(rc))
	})

	t.Run("second is shortest", func(t *testing.T) {
		a := []int{1, 2, 3, 4, 5}
		b := []int{10, 20, 30}
		c := []int{100, 200, 300, 400, 500}

		ra, rb, rc := alignThree(a, b, c)
		assert.Equal(t, 3, len(ra))
		assert.Equal(t, 3, len(rb))
		assert.Equal(t, 3, len(rc))
		assert.Equal(t, 1, ra[0])
		assert.Equal(t, 3, ra[2])
	})

	t.Run("third is shortest", func(t *testing.T) {
		a := []int{1, 2, 3, 4}
		b := []int{10, 20, 30, 40}
		c := []int{100, 200}

		ra, rb, rc := alignThree(a, b, c)
		assert.Equal(t, 2, len(ra))
		assert.Equal(t, 2, len(rb))
		assert.Equal(t, 2, len(rc))
	})

	t.Run("first is shortest", func(t *testing.T) {
		a := []int{1, 2}
		b := []int{10, 20, 30, 40}
		c := []int{100, 200, 300, 400}

		ra, rb, rc := alignThree(a, b, c)
		assert.Equal(t, 2, len(ra))
		assert.Equal(t, 2, len(rb))
		assert.Equal(t, 2, len(rc))
	})

	t.Run("empty slices", func(t *testing.T) {
		a := []int{}
		b := []int{1, 2, 3}
		c := []int{1, 2, 3}

		ra, rb, rc := alignThree(a, b, c)
		assert.Equal(t, 0, len(ra))
		assert.Equal(t, 0, len(rb))
		assert.Equal(t, 0, len(rc))
	})
}

func TestAlignTwo(t *testing.T) {
	t.Run("same length", func(t *testing.T) {
		a := []string{"a", "b", "c"}
		b := []string{"x", "y", "z"}

		ra, rb := alignTwo(a, b)
		assert.Equal(t, 3, len(ra))
		assert.Equal(t, 3, len(rb))
	})

	t.Run("second is shorter", func(t *testing.T) {
		a := []string{"a", "b", "c", "d"}
		b := []string{"x", "y"}

		ra, rb := alignTwo(a, b)
		assert.Equal(t, 2, len(ra))
		assert.Equal(t, 2, len(rb))
		assert.Equal(t, "a", ra[0])
		assert.Equal(t, "b", ra[1])
	})

	t.Run("first is shorter", func(t *testing.T) {
		a := []string{"a", "b"}
		b := []string{"x", "y", "z", "w"}

		ra, rb := alignTwo(a, b)
		assert.Equal(t, 2, len(ra))
		assert.Equal(t, 2, len(rb))
	})

	t.Run("first is empty", func(t *testing.T) {
		a := []string{}
		b := []string{"x", "y", "z"}

		ra, rb := alignTwo(a, b)
		assert.Equal(t, 0, len(ra))
		assert.Equal(t, 0, len(rb))
	})

	t.Run("both empty", func(t *testing.T) {
		a := []string{}
		b := []string{}

		ra, rb := alignTwo(a, b)
		assert.Equal(t, 0, len(ra))
		assert.Equal(t, 0, len(rb))
	})
}
