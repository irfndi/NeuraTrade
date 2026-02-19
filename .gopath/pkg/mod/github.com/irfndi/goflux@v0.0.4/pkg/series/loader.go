package series

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/irfndi/goflux/pkg/decimal"
)

// CSVConfig describes how to parse a CSV file into a TimeSeries
type CSVConfig struct {
	TimeFormat  string
	TimeIndex   int
	OpenIndex   int
	HighIndex   int
	LowIndex    int
	CloseIndex  int
	VolumeIndex int
	HasHeader   bool
}

// NewCSVConfig returns a default CSVConfig with standard indices
func NewCSVConfig() CSVConfig {
	return CSVConfig{
		TimeFormat:  time.RFC3339,
		TimeIndex:   0,
		OpenIndex:   1,
		HighIndex:   2,
		LowIndex:    3,
		CloseIndex:  4,
		VolumeIndex: 5,
		HasHeader:   true,
	}
}

// LoadCSV parses CSV data from an io.Reader and returns a TimeSeries
func LoadCSV(reader io.Reader, config CSVConfig) (*TimeSeries, error) {
	csvReader := csv.NewReader(reader)
	if config.HasHeader {
		_, err := csvReader.Read() // Skip header
		if err != nil {
			return nil, fmt.Errorf("error reading CSV header: %v", err)
		}
	}

	ts := NewTimeSeries()
	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading CSV record: %v", err)
		}

		t, err := time.Parse(config.TimeFormat, record[config.TimeIndex])
		if err != nil {
			return nil, fmt.Errorf("error parsing time: %v", err)
		}

		// Calculate duration if possible, otherwise assume 1 min or similar?
		// Better to let user specify or infer from first two candles.
		// For now, we'll use a fixed duration of 0 and let users use resample or fix it later.
		// Actually, NewTimePeriod needs a duration.

		candle := NewCandle(NewTimePeriod(t, 0)) // Initial duration 0
		candle.OpenPrice = decimal.NewFromString(record[config.OpenIndex])
		candle.MaxPrice = decimal.NewFromString(record[config.HighIndex])
		candle.MinPrice = decimal.NewFromString(record[config.LowIndex])
		candle.ClosePrice = decimal.NewFromString(record[config.CloseIndex])
		if config.VolumeIndex < len(record) {
			candle.Volume = decimal.NewFromString(record[config.VolumeIndex])
		}

		ts.AddCandle(candle)
	}

	// Post-process to fix durations if we have at least 2 candles
	if ts.Length() >= 2 {
		d := ts.Candles[1].Period.Start.Sub(ts.Candles[0].Period.Start)
		for i := 0; i < ts.Length(); i++ {
			ts.Candles[i].Period.End = ts.Candles[i].Period.Start.Add(d)
		}
	} else if ts.Length() == 1 {
		// Default to 1 minute if only one candle?
		ts.Candles[0].Period.End = ts.Candles[0].Period.Start.Add(time.Minute)
	}

	return ts, nil
}

// JSONCandle represents a single candle in JSON format
type JSONCandle struct {
	Time   string  `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// LoadJSON parses JSON data from an io.Reader and returns a TimeSeries
func LoadJSON(reader io.Reader, timeFormat string) (*TimeSeries, error) {
	var jsonCandles []JSONCandle
	decoder := json.NewDecoder(reader)
	if err := decoder.Decode(&jsonCandles); err != nil {
		return nil, fmt.Errorf("error decoding JSON: %v", err)
	}

	ts := NewTimeSeries()
	for _, jc := range jsonCandles {
		t, err := time.Parse(timeFormat, jc.Time)
		if err != nil {
			return nil, fmt.Errorf("error parsing time %s: %v", jc.Time, err)
		}

		candle := NewCandle(NewTimePeriod(t, 0))
		candle.OpenPrice = decimal.New(jc.Open)
		candle.MaxPrice = decimal.New(jc.High)
		candle.MinPrice = decimal.New(jc.Low)
		candle.ClosePrice = decimal.New(jc.Close)
		candle.Volume = decimal.New(jc.Volume)

		ts.AddCandle(candle)
	}

	// Post-process to fix durations
	if ts.Length() >= 2 {
		d := ts.Candles[1].Period.Start.Sub(ts.Candles[0].Period.Start)
		for i := 0; i < ts.Length(); i++ {
			ts.Candles[i].Period.End = ts.Candles[i].Period.Start.Add(d)
		}
	} else if ts.Length() == 1 {
		ts.Candles[0].Period.End = ts.Candles[0].Period.Start.Add(time.Minute)
	}

	return ts, nil
}
