package series_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/decimal"
	"github.com/irfndi/goflux/pkg/series"
)

func TestCandle_AddTrade(t *testing.T) {
	now := time.Now()
	candle := series.NewCandle(series.TimePeriod{
		Start: now,
		End:   now.Add(time.Minute),
	})

	candle.AddTrade(decimal.New(1), decimal.New(2)) // Open
	candle.AddTrade(decimal.New(1), decimal.New(5)) // High
	candle.AddTrade(decimal.New(1), decimal.New(1)) // Low
	candle.AddTrade(decimal.New(1), decimal.New(3)) // No Diff
	candle.AddTrade(decimal.New(1), decimal.New(3)) // Close

	assert.EqualValues(t, 2, candle.OpenPrice.Float())
	assert.EqualValues(t, 5, candle.MaxPrice.Float())
	assert.EqualValues(t, 1, candle.MinPrice.Float())
	assert.EqualValues(t, 3, candle.ClosePrice.Float())
	assert.EqualValues(t, 5, candle.Volume.Float())
	assert.EqualValues(t, 5, candle.TradeCount)
}

func TestCandle_String(t *testing.T) {
	now := time.Now()
	candle := series.NewCandle(series.TimePeriod{
		Start: now,
		End:   now.Add(time.Minute),
	})

	candle.ClosePrice = decimal.NewFromString("1")
	candle.OpenPrice = decimal.NewFromString("2")
	candle.MaxPrice = decimal.NewFromString("3")
	candle.MinPrice = decimal.NewFromString("0")
	candle.Volume = decimal.NewFromString("10")

	expected := strings.TrimSpace(fmt.Sprintf(`
Time:	%s
Open:	2.00
Close:	1.00
High:	3.00
Low:	0.00
Volume:	10.00
`, candle.Period))

	assert.EqualValues(t, expected, candle.String())
}
