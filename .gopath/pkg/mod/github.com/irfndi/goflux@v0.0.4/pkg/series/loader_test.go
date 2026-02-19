package series_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/irfndi/goflux/pkg/series"
)

func TestLoadCSV(t *testing.T) {
	csvData := `time,open,high,low,close,volume
2023-01-01T00:00:00Z,100,105,95,102,1000
2023-01-01T00:01:00Z,102,107,101,105,1100
2023-01-01T00:02:00Z,105,110,104,108,1200`

	reader := strings.NewReader(csvData)
	config := series.NewCSVConfig()
	config.TimeFormat = time.RFC3339

	ts, err := series.LoadCSV(reader, config)
	assert.NoError(t, err)
	assert.Equal(t, 3, ts.Length())

	assert.Equal(t, "100.00", ts.Candles[0].OpenPrice.FormattedString(2))
	assert.Equal(t, "105.00", ts.Candles[0].MaxPrice.FormattedString(2))
	assert.Equal(t, "95.00", ts.Candles[0].MinPrice.FormattedString(2))
	assert.Equal(t, "102.00", ts.Candles[0].ClosePrice.FormattedString(2))
	assert.Equal(t, "1000.00", ts.Candles[0].Volume.FormattedString(2))

	// Check duration fix
	assert.Equal(t, time.Minute, ts.Candles[0].Period.Length())
}

func TestLoadJSON(t *testing.T) {
	jsonData := `[
		{"time": "2023-01-01T00:00:00Z", "open": 100, "high": 105, "low": 95, "close": 102, "volume": 1000},
		{"time": "2023-01-01T00:01:00Z", "open": 102, "high": 107, "low": 101, "close": 105, "volume": 1100}
	]`

	reader := strings.NewReader(jsonData)
	ts, err := series.LoadJSON(reader, time.RFC3339)
	assert.NoError(t, err)
	assert.Equal(t, 2, ts.Length())

	assert.Equal(t, "100.00", ts.Candles[0].OpenPrice.FormattedString(2))
	assert.Equal(t, "105.00", ts.Candles[0].MaxPrice.FormattedString(2))
	assert.Equal(t, time.Minute, ts.Candles[0].Period.Length())
}
