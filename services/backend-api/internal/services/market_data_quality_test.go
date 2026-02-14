package services

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestPriceOutlierFilter(t *testing.T) {
	config := DefaultMarketDataQualityConfig()
	config.PriceChangeThresholdPercent = 50.0
	config.PriceChangeWindow = 1 * time.Minute

	// Use nil logger (service handles nil gracefully)
	service := NewMarketDataQualityService(config, nil)

	symbol := "BTC/USDT"
	exchange := "binance"
	basePrice := decimal.NewFromFloat(50000)

	// First call - should pass (no history)
	result := service.Process(exchange, symbol, basePrice, decimal.NewFromFloat(1000000), time.Now())
	if !result.Passed {
		t.Errorf("Expected first call to pass, got %v", result.Passed)
	}

	// Second call with normal price change (< 50%)
	normalPrice := decimal.NewFromFloat(52000) // 4% increase
	result = service.Process(exchange, symbol, normalPrice, decimal.NewFromFloat(1000000), time.Now().Add(30*time.Second))
	if !result.Passed {
		t.Errorf("Expected normal price change to pass, got %v", result.Passed)
	}

	// Third call with outlier price change (> 50%)
	outlierPrice := decimal.NewFromFloat(80000) // 60% increase from base
	result = service.Process(exchange, symbol, outlierPrice, decimal.NewFromFloat(1000000), time.Now().Add(45*time.Second))
	if result.Passed {
		t.Errorf("Expected outlier price change to fail, got %v", result.Passed)
	}
	if !result.ShouldReject {
		t.Errorf("Expected outlier to be blocking, got %v", result.ShouldReject)
	}
	if len(result.Events) == 0 || result.Events[0].Type != QualityEventPriceOutlier {
		t.Errorf("Expected price outlier event")
	}
}

func TestVolumeAnomalyDetector(t *testing.T) {
	config := DefaultMarketDataQualityConfig()
	config.VolumeSpikeThresholdPercent = 500.0 // 500% spike
	config.VolumeDropThresholdPercent = 90.0   // 90% drop

	service := NewMarketDataQualityService(config, nil)

	symbol := "BTC/USDT"
	exchange := "binance"
	baseVolume := decimal.NewFromFloat(1000)

	// First call - should pass (no history)
	result := service.Process(exchange, symbol, decimal.NewFromFloat(50000), baseVolume, time.Now())
	if !result.Passed {
		t.Errorf("Expected first call to pass, got %v", result.Passed)
	}

	// Volume spike (> 500%)
	spikeVolume := decimal.NewFromFloat(6000) // 500% increase
	result = service.Process(exchange, symbol, decimal.NewFromFloat(50000), spikeVolume, time.Now().Add(30*time.Second))
	if len(result.Events) == 0 {
		t.Errorf("Expected volume spike event")
	}
	// Volume anomalies are warnings, not blocking
	if result.ShouldReject {
		t.Errorf("Expected volume spike to not be blocking")
	}

	// Volume drop (> 90%)
	dropVolume := decimal.NewFromFloat(50) // 95% decrease
	result = service.Process(exchange, symbol, decimal.NewFromFloat(50000), dropVolume, time.Now().Add(30*time.Second))
	if len(result.Events) == 0 {
		t.Errorf("Expected volume drop event")
	}
}

func TestStaleDataDetector(t *testing.T) {
	config := DefaultMarketDataQualityConfig()
	config.StaleDataThreshold = 60 * time.Second

	service := NewMarketDataQualityService(config, nil)

	symbol := "BTC/USDT"
	exchange := "binance"

	// First call - should pass
	result := service.Process(exchange, symbol, decimal.NewFromFloat(50000), decimal.NewFromFloat(1000), time.Now())
	if !result.Passed {
		t.Errorf("Expected first call to pass, got %v", result.Passed)
	}

	// Wait and check for stale data
	time.Sleep(100 * time.Millisecond)
	result = service.Process(exchange, symbol, decimal.NewFromFloat(50000), decimal.NewFromFloat(1000), time.Now().Add(-120*time.Second))
	if result.ShouldReject {
		t.Errorf("Expected recent data to not be rejected")
	}
}

func TestCrossExchangeValidation(t *testing.T) {
	config := DefaultMarketDataQualityConfig()
	config.PrimaryExchange = "binance"
	config.SecondaryExchanges = []string{"bybit"}
	config.CrossExchangeMaxSpreadPercent = 5.0

	service := NewMarketDataQualityService(config, nil)

	symbol := "BTC/USDT"

	// Add price from primary exchange
	service.updateCrossExchangePrice("binance", symbol, decimal.NewFromFloat(50000))

	// Add price from secondary exchange with normal spread (< 5%)
	service.updateCrossExchangePrice("bybit", symbol, decimal.NewFromFloat(50200)) // 0.4% spread
	result := service.checkCrossExchangeValidation(symbol)
	if len(result) > 0 {
		t.Errorf("Expected normal spread to pass, got %v events", len(result))
	}

	// Add price from secondary exchange with large spread (> 5%)
	service.updateCrossExchangePrice("bybit", symbol, decimal.NewFromFloat(53000)) // 6% spread
	result = service.checkCrossExchangeValidation(symbol)
	if len(result) == 0 {
		t.Errorf("Expected cross-exchange mismatch event")
	}
	// Check if any event is blocking - cross-exchange should be warning, not blocking
	isBlocking := false
	for _, e := range result {
		if e.IsBlocking {
			isBlocking = true
			break
		}
	}
	if isBlocking {
		t.Errorf("Expected cross-exchange warning to not be blocking")
	}
}

func TestFilterResultStruct(t *testing.T) {
	result := &FilterResult{
		Passed:       true,
		Events:       make([]QualityEvent, 0),
		ShouldReject: false,
	}

	if result.Passed != true {
		t.Errorf("Expected Passed to be true")
	}
	if result.ShouldReject != false {
		t.Errorf("Expected ShouldReject to be false")
	}
}

func TestMarketDataQualityServiceReset(t *testing.T) {
	config := DefaultMarketDataQualityConfig()
	service := NewMarketDataQualityService(config, nil)

	// Add some data
	service.Process("binance", "BTC/USDT", decimal.NewFromFloat(50000), decimal.NewFromFloat(1000), time.Now())

	// Get stats before reset
	stats := service.GetStats()
	priceHistoryCount := stats["price_history_count"].(int)
	if priceHistoryCount == 0 {
		t.Errorf("Expected non-zero price history count before reset")
	}

	// Reset
	service.Reset()

	// Get stats after reset
	stats = service.GetStats()
	priceHistoryCount = stats["price_history_count"].(int)
	if priceHistoryCount != 0 {
		t.Errorf("Expected zero price history count after reset, got %d", priceHistoryCount)
	}
}

func TestMarketDataQualityServiceGetConfig(t *testing.T) {
	config := DefaultMarketDataQualityConfig()
	service := NewMarketDataQualityService(config, nil)

	returnedConfig := service.GetConfig()
	if returnedConfig.PriceChangeThresholdPercent != 50.0 {
		t.Errorf("Expected 50.0, got %f", returnedConfig.PriceChangeThresholdPercent)
	}
	if returnedConfig.StaleDataThreshold != 60*time.Second {
		t.Errorf("Expected 60s, got %v", returnedConfig.StaleDataThreshold)
	}
}

func TestMarketDataQualityServiceUpdateConfig(t *testing.T) {
	config := DefaultMarketDataQualityConfig()
	service := NewMarketDataQualityService(config, nil)

	newConfig := &MarketDataQualityConfig{
		PriceChangeThresholdPercent: 25.0,
		PriceChangeWindow:           30 * time.Second,
	}
	service.UpdateConfig(newConfig)

	returnedConfig := service.GetConfig()
	if returnedConfig.PriceChangeThresholdPercent != 25.0 {
		t.Errorf("Expected 25.0, got %f", returnedConfig.PriceChangeThresholdPercent)
	}
}
