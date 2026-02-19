package indicators

import (
	"fmt"
)

// IndicatorMetadata describes an indicator's properties
type IndicatorMetadata struct {
	Name        string
	Category    string
	Description string
	Inputs      []string
	Lookback    int
}

var metadataRegistry = make(map[string]IndicatorMetadata)

// RegisterMetadata registers metadata for an indicator name
func RegisterMetadata(name string, meta IndicatorMetadata) {
	metadataRegistry[name] = meta
}

// GetMetadata returns metadata for an indicator name
func GetMetadata(name string) (IndicatorMetadata, error) {
	meta, ok := metadataRegistry[name]
	if !ok {
		return IndicatorMetadata{}, fmt.Errorf("metadata for %s not found", name)
	}
	return meta, nil
}

func init() {
	RegisterMetadata("sma", IndicatorMetadata{
		Name:     "Simple Moving Average",
		Category: "Overlap Studies",
		Lookback: 0, // Varies by period
	})
	RegisterMetadata("ema", IndicatorMetadata{
		Name:     "Exponential Moving Average",
		Category: "Overlap Studies",
	})
	RegisterMetadata("rsi", IndicatorMetadata{
		Name:     "Relative Strength Index",
		Category: "Momentum Indicators",
	})
}
