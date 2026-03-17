package services

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

// TestEnhancedArbitrageSignalFormatting demonstrates the enhanced arbitrage signal formatting
// with realistic data showing price ranges, profit ranges, and multiple exchanges
func TestEnhancedArbitrageSignalFormatting(t *testing.T) {
	// Create a realistic enhanced arbitrage signal with price ranges
	signal := &AggregatedSignal{
		ID:         "test-signal-123",
		Symbol:     "BTC/USDT",
		SignalType: SignalTypeArbitrage,
		Action:     "buy",
		Strength:   SignalStrengthStrong,
		Confidence: decimal.NewFromFloat(0.85),
		CreatedAt:  time.Now(),
		Metadata: map[string]interface{}{
			"buy_price_range": map[string]interface{}{
				"min": decimal.NewFromFloat(41750.50),
				"max": decimal.NewFromFloat(41850.75),
				"avg": decimal.NewFromFloat(41800.25),
			},
			"sell_price_range": map[string]interface{}{
				"min": decimal.NewFromFloat(42250.80),
				"max": decimal.NewFromFloat(42350.90),
				"avg": decimal.NewFromFloat(42300.85),
			},
			"profit_range": map[string]interface{}{
				"min_percent": decimal.NewFromFloat(0.8),
				"max_percent": decimal.NewFromFloat(1.4),
				"avg_percent": decimal.NewFromFloat(1.2),
				"min_dollar":  decimal.NewFromFloat(160.00),
				"max_dollar":  decimal.NewFromFloat(280.00),
				"avg_dollar":  decimal.NewFromFloat(240.00),
				"base_amount": decimal.NewFromFloat(20000.00),
			},
			"buy_exchanges":     []string{"Binance", "Kraken", "OKX", "Bybit", "KuCoin"},
			"sell_exchanges":    []string{"Coinbase", "Gate.io", "MEXC"},
			"opportunity_count": 8,
			"min_volume":        decimal.NewFromFloat(10000.00),
			"validity_minutes":  5,
		},
	}

	// Create notification service
	notificationService := &NotificationService{}

	// Format the enhanced arbitrage message
	message := notificationService.formatEnhancedArbitrageMessage(signal)

	// Verify the message contains expected elements
	assert.Contains(t, message, "🔄 ARBITRAGE ALERT: BTC/USDT")
	assert.Contains(t, message, "💰 Profit: 0.80% - 1.40% ($160 - $280 on $20000)")
	assert.Contains(t, message, "📈 BUY: $41750.5000 - $41850.7500 (Binance, Kraken, OKX, Bybit, KuCoin)")
	assert.Contains(t, message, "📉 SELL: $42250.8000 - $42350.9000 (Coinbase, Gate.io, MEXC)")
	assert.Contains(t, message, "⏰ Valid for: 5 minutes")
	assert.Contains(t, message, "🎯 Min Volume: $10000")
	assert.Contains(t, message, "🎯 Confidence: 85.0%")

	// Print the formatted message for visual verification
	fmt.Println("\n=== Enhanced Arbitrage Signal Format ===")
	fmt.Println(message)
	fmt.Println("==========================================")

	// Verify JSON structure
	metadataJSON, err := json.MarshalIndent(signal.Metadata, "", "  ")
	assert.NoError(t, err)
	fmt.Println("=== Signal Metadata JSON ===")
	fmt.Println(string(metadataJSON))
	fmt.Println("============================")
}

// TestArbitragePriceRangeCalculation demonstrates price range calculation logic
func TestArbitragePriceRangeCalculation(t *testing.T) {
	// Simulate multiple exchange prices for BTC/USDT
	buyPrices := []decimal.Decimal{
		decimal.NewFromFloat(41750.50), // Binance
		decimal.NewFromFloat(41780.25), // Kraken
		decimal.NewFromFloat(41820.75), // OKX
		decimal.NewFromFloat(41850.75), // Bybit
		decimal.NewFromFloat(41795.30), // KuCoin
	}

	sellPrices := []decimal.Decimal{
		decimal.NewFromFloat(42350.90), // Coinbase
		decimal.NewFromFloat(42280.45), // Gate.io
		decimal.NewFromFloat(42250.80), // MEXC
	}

	// Calculate buy price range
	buyMin := buyPrices[0]
	buyMax := buyPrices[0]
	buySum := decimal.Zero

	for _, price := range buyPrices {
		if price.LessThan(buyMin) {
			buyMin = price
		}
		if price.GreaterThan(buyMax) {
			buyMax = price
		}
		buySum = buySum.Add(price)
	}
	buyAvg := buySum.Div(decimal.NewFromInt(int64(len(buyPrices))))

	// Calculate sell price range
	sellMin := sellPrices[0]
	sellMax := sellPrices[0]
	sellSum := decimal.Zero

	for _, price := range sellPrices {
		if price.LessThan(sellMin) {
			sellMin = price
		}
		if price.GreaterThan(sellMax) {
			sellMax = price
		}
		sellSum = sellSum.Add(price)
	}
	sellAvg := sellSum.Div(decimal.NewFromInt(int64(len(sellPrices))))

	// Calculate profit ranges
	baseAmount := decimal.NewFromFloat(20000.0)

	// Best case: buy at lowest, sell at highest
	maxProfitPercent := sellMax.Sub(buyMin).Div(buyMin).Mul(decimal.NewFromFloat(100))
	maxProfitDollar := maxProfitPercent.Div(decimal.NewFromFloat(100)).Mul(baseAmount)

	// Worst case: buy at highest, sell at lowest
	minProfitPercent := sellMin.Sub(buyMax).Div(buyMax).Mul(decimal.NewFromFloat(100))
	minProfitDollar := minProfitPercent.Div(decimal.NewFromFloat(100)).Mul(baseAmount)

	// Average case
	avgProfitPercent := sellAvg.Sub(buyAvg).Div(buyAvg).Mul(decimal.NewFromFloat(100))
	avgProfitDollar := avgProfitPercent.Div(decimal.NewFromFloat(100)).Mul(baseAmount)

	// Verify calculations
	assert.Equal(t, "41750.5", buyMin.String())
	assert.Equal(t, "41850.75", buyMax.String())
	assert.Equal(t, "42250.8", sellMin.String())
	assert.Equal(t, "42350.9", sellMax.String())

	// Print calculation results
	fmt.Println("\n=== Price Range Calculations ===")
	fmt.Printf("Buy Range: $%s - $%s (avg: $%s)\n", buyMin.String(), buyMax.String(), buyAvg.StringFixed(2))
	fmt.Printf("Sell Range: $%s - $%s (avg: $%s)\n", sellMin.String(), sellMax.String(), sellAvg.StringFixed(2))
	fmt.Printf("Profit Range: %s%% - %s%% (avg: %s%%)\n",
		minProfitPercent.StringFixed(1),
		maxProfitPercent.StringFixed(1),
		avgProfitPercent.StringFixed(1))
	fmt.Printf("Dollar Range: $%s - $%s (avg: $%s on $%s)\n",
		minProfitDollar.StringFixed(0),
		maxProfitDollar.StringFixed(0),
		avgProfitDollar.StringFixed(0),
		baseAmount.String())
	fmt.Println("=================================")
}
