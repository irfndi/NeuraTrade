package services

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestDetectWashTrading_Detected(t *testing.T) {
	config := &AntiManipulationFilterConfig{
		WashTradeThreshold: decimal.NewFromFloat(5.0),
	}

	filter := &AntiManipulationFilter{config: config}

	currentVolume := decimal.NewFromFloat(60000)
	avgVolume := decimal.NewFromFloat(10000)

	result := filter.detectWashTrading(currentVolume, avgVolume)

	if !result.IsDetected {
		t.Error("Expected wash trade to be detected for 6x volume spike")
	}

	expectedScore := decimal.NewFromFloat(1.0)
	if !result.Score.Equal(expectedScore) {
		t.Errorf("Expected score %s, got %s", expectedScore, result.Score)
	}
}

func TestDetectWashTrading_NotDetected(t *testing.T) {
	config := &AntiManipulationFilterConfig{
		WashTradeThreshold: decimal.NewFromFloat(5.0),
	}

	filter := &AntiManipulationFilter{config: config}

	currentVolume := decimal.NewFromFloat(15000)
	avgVolume := decimal.NewFromFloat(10000)

	result := filter.detectWashTrading(currentVolume, avgVolume)

	if result.IsDetected {
		t.Error("Expected wash trade NOT to be detected for 1.5x volume")
	}
}

func TestDetectWashTrading_ZeroAvgVolume(t *testing.T) {
	config := &AntiManipulationFilterConfig{
		WashTradeThreshold: decimal.NewFromFloat(5.0),
	}

	filter := &AntiManipulationFilter{config: config}

	currentVolume := decimal.NewFromFloat(60000)
	avgVolume := decimal.Zero

	result := filter.detectWashTrading(currentVolume, avgVolume)

	if result.IsDetected {
		t.Error("Expected wash trade NOT to be detected with zero average volume")
	}
}

func TestDetectSpoofing_Detected(t *testing.T) {
	config := &AntiManipulationFilterConfig{
		SpoofingOrderSize: decimal.NewFromFloat(10000),
		LayeringMinLevels: 3,
	}

	filter := &AntiManipulationFilter{config: config}

	orderBook := map[string]interface{}{
		"bids": []interface{}{
			map[string]interface{}{"price": 50000.0, "size": 15000.0},
			map[string]interface{}{"price": 50001.0, "size": 12000.0},
			map[string]interface{}{"price": 50002.0, "size": 11000.0},
			map[string]interface{}{"price": 50003.0, "size": 10000.0},
		},
		"asks": []interface{}{},
	}

	result := filter.detectSpoofing(orderBook)

	if !result.IsDetected {
		t.Error("Expected spoofing to be detected with 4 large orders")
	}
}

func TestDetectSpoofing_NotDetected(t *testing.T) {
	config := &AntiManipulationFilterConfig{
		SpoofingOrderSize: decimal.NewFromFloat(10000),
		LayeringMinLevels: 3,
	}

	filter := &AntiManipulationFilter{config: config}

	orderBook := map[string]interface{}{
		"bids": []interface{}{
			map[string]interface{}{"price": 50000.0, "size": 500.0},
			map[string]interface{}{"price": 50001.0, "size": 600.0},
		},
		"asks": []interface{}{},
	}

	result := filter.detectSpoofing(orderBook)

	if result.IsDetected {
		t.Error("Expected spoofing NOT to be detected with small orders")
	}
}

func TestDetectLayering_Detected(t *testing.T) {
	config := &AntiManipulationFilterConfig{
		LayeringMinLevels: 2,
	}

	filter := &AntiManipulationFilter{config: config}

	orderBook := map[string]interface{}{
		"bids": []interface{}{
			map[string]interface{}{"price": 50000.0, "size": 1000.0},
			map[string]interface{}{"price": 50000.0, "size": 1500.0},
			map[string]interface{}{"price": 50000.0, "size": 2000.0},
			map[string]interface{}{"price": 50100.0, "size": 1200.0},
			map[string]interface{}{"price": 50100.0, "size": 1800.0},
		},
		"asks": []interface{}{},
	}

	result := filter.detectLayering(orderBook)

	if !result.IsDetected {
		t.Error("Expected layering to be detected with multiple orders at price levels")
	}
}

func TestDetectLayering_NotDetected(t *testing.T) {
	config := &AntiManipulationFilterConfig{
		LayeringMinLevels: 3,
	}

	filter := &AntiManipulationFilter{config: config}

	orderBook := map[string]interface{}{
		"bids": []interface{}{
			map[string]interface{}{"price": 50000.0, "size": 1000.0},
			map[string]interface{}{"price": 50100.0, "size": 1200.0},
		},
		"asks": []interface{}{},
	}

	result := filter.detectLayering(orderBook)

	if result.IsDetected {
		t.Error("Expected layering NOT to be detected with few orders")
	}
}

func TestAnalyzePriceLevels(t *testing.T) {
	filter := &AntiManipulationFilter{}

	orders := []interface{}{
		map[string]interface{}{"price": 50000.0, "size": 1000.0},
		map[string]interface{}{"price": 50000.0, "size": 1500.0},
		map[string]interface{}{"price": 50000.0, "size": 2000.0},
		map[string]interface{}{"price": 50100.0, "size": 1200.0},
		map[string]interface{}{"price": 50100.0, "size": 1800.0},
	}

	levels := filter.analyzePriceLevels(orders)

	if levels[500] != 3 {
		t.Errorf("Expected 3 orders at level 500, got %d", levels[500])
	}
	if levels[501] != 2 {
		t.Errorf("Expected 2 orders at level 501, got %d", levels[501])
	}
}
