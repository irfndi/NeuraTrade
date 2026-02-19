package series

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
)

func TestResample(t *testing.T) {
	s := NewTimeSeries()
	base := time.Now().Truncate(time.Hour)

	// Create 10 candles of 1 minute each
	for i := 0; i < 10; i++ {
		c := NewCandle(NewTimePeriod(base.Add(time.Duration(i)*time.Minute), time.Minute))
		c.OpenPrice = decimal.New(100)
		c.ClosePrice = decimal.New(105)
		c.MaxPrice = decimal.New(110)
		c.MinPrice = decimal.New(95)
		c.Volume = decimal.New(100)
		s.AddCandle(c)
	}

	// Resample to 5 minutes
	resampled := Resample(s, 5*time.Minute)

	assert.Equal(t, 2, resampled.Length())

	c0 := resampled.GetCandle(0)
	assert.Equal(t, base, c0.Period.Start)
	assert.Equal(t, 5*time.Minute, c0.Period.Length())
	assert.Equal(t, 500.0, c0.Volume.Float())
	assert.Equal(t, 100.0, c0.OpenPrice.Float())
	assert.Equal(t, 105.0, c0.ClosePrice.Float())
}
