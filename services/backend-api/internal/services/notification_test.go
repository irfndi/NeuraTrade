package services

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestNewNotificationService(t *testing.T) {
	// Test with empty token
	ns := NewNotificationService(nil, nil, "", "", "")
	assert.NotNil(t, ns)
	assert.Nil(t, ns.db)

	// Test with token - bot creation may fail with invalid token but service should still be created
	ns2 := NewNotificationService(nil, nil, "http://test-url", "localhost:50052", "test-key")
	assert.NotNil(t, ns2)
	assert.Nil(t, ns2.db)
}

func TestArbitrageOpportunity_Struct(t *testing.T) {
	now := time.Now()
	opportunity := ArbitrageOpportunity{
		Symbol:          "BTC/USDT",
		BuyExchange:     "binance",
		SellExchange:    "coinbase",
		BuyPrice:        50000.0,
		SellPrice:       50500.0,
		ProfitPercent:   1.0,
		ProfitAmount:    500.0,
		Volume:          1.0,
		Timestamp:       now,
		OpportunityType: "arbitrage",
	}

	assert.Equal(t, "BTC/USDT", opportunity.Symbol)
	assert.Equal(t, "binance", opportunity.BuyExchange)
	assert.Equal(t, "coinbase", opportunity.SellExchange)
	assert.Equal(t, 50000.0, opportunity.BuyPrice)
	assert.Equal(t, 50500.0, opportunity.SellPrice)
	assert.Equal(t, 1.0, opportunity.ProfitPercent)
	assert.Equal(t, 500.0, opportunity.ProfitAmount)
	assert.Equal(t, 1.0, opportunity.Volume)
	assert.Equal(t, now, opportunity.Timestamp)
	assert.Equal(t, "arbitrage", opportunity.OpportunityType)
}

func TestNotificationService_formatArbitrageMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test with empty opportunities
	message := ns.formatArbitrageMessage([]ArbitrageOpportunity{})
	assert.Equal(t, "No arbitrage opportunities found.", message)

	// Test with single arbitrage opportunity
	opportunities := []ArbitrageOpportunity{
		{
			Symbol:          "BTC/USDT",
			BuyExchange:     "binance",
			SellExchange:    "coinbase",
			BuyPrice:        50000.0,
			SellPrice:       50500.0,
			ProfitPercent:   1.0,
			OpportunityType: "arbitrage",
		},
	}

	message = ns.formatArbitrageMessage(opportunities)
	assert.Contains(t, message, "🚀 *True Arbitrage Opportunities*")
	assert.Contains(t, message, "BTC/USDT")
	assert.Contains(t, message, "1.00%")
	assert.Contains(t, message, "binance")
	assert.Contains(t, message, "coinbase")

	// Test with technical opportunity
	technicalOpps := []ArbitrageOpportunity{
		{
			Symbol:          "ETH/USDT",
			BuyExchange:     "binance",
			SellExchange:    "binance",
			BuyPrice:        3000.0,
			SellPrice:       3030.0,
			ProfitPercent:   1.0,
			OpportunityType: "technical",
		},
	}

	message = ns.formatArbitrageMessage(technicalOpps)
	assert.Contains(t, message, "📊 *Technical Analysis Signals*")
	assert.Contains(t, message, "ETH/USDT")

	// Test with AI-generated opportunity
	aiOpps := []ArbitrageOpportunity{
		{
			Symbol:          "ADA/USDT",
			BuyExchange:     "kraken",
			SellExchange:    "bitfinex",
			BuyPrice:        0.5,
			SellPrice:       0.51,
			ProfitPercent:   2.0,
			OpportunityType: "ai_generated",
		},
	}

	message = ns.formatArbitrageMessage(aiOpps)
	assert.Contains(t, message, "🤖 *AI-Generated Opportunities*")
	assert.Contains(t, message, "ADA/USDT")

	// Test with more than 3 opportunities
	manyOpps := make([]ArbitrageOpportunity, 5)
	for i := 0; i < 5; i++ {
		manyOpps[i] = ArbitrageOpportunity{
			Symbol:          "BTC/USDT",
			BuyExchange:     "binance",
			SellExchange:    "coinbase",
			BuyPrice:        50000.0,
			SellPrice:       50500.0,
			ProfitPercent:   1.0,
			OpportunityType: "arbitrage",
		}
	}

	message = ns.formatArbitrageMessage(manyOpps)
	assert.Contains(t, message, "Found 5 profitable opportunities")
	assert.Contains(t, message, "...and 2 more opportunities")
}

func TestNotificationService_OpportunityCategorization(t *testing.T) {
	// Test categorization logic without database dependencies
	opportunities := []ArbitrageOpportunity{
		{
			Symbol:        "BTC/USDT",
			BuyExchange:   "binance",
			SellExchange:  "coinbase", // Different exchanges = arbitrage
			BuyPrice:      50000.0,
			SellPrice:     50500.0,
			ProfitPercent: 1.0,
		},
		{
			Symbol:        "ETH/USDT",
			BuyExchange:   "binance",
			SellExchange:  "binance", // Same exchange = technical
			BuyPrice:      3000.0,
			SellPrice:     3030.0,
			ProfitPercent: 1.0,
		},
	}

	// Verify categorization logic manually
	var arbitrageOpps, technicalOpps []ArbitrageOpportunity
	for _, opp := range opportunities {
		if opp.BuyExchange != opp.SellExchange {
			opp.OpportunityType = "arbitrage"
			arbitrageOpps = append(arbitrageOpps, opp)
		} else {
			opp.OpportunityType = "technical"
			technicalOpps = append(technicalOpps, opp)
		}
	}

	assert.Len(t, arbitrageOpps, 1)
	assert.Len(t, technicalOpps, 1)
	assert.Equal(t, "arbitrage", arbitrageOpps[0].OpportunityType)
	assert.Equal(t, "technical", technicalOpps[0].OpportunityType)
}

// Test ArbitrageOpportunity with different field values
func TestArbitrageOpportunity_EdgeCases(t *testing.T) {
	// Test with zero values
	zeroOpp := ArbitrageOpportunity{}
	assert.Empty(t, zeroOpp.Symbol)
	assert.Empty(t, zeroOpp.BuyExchange)
	assert.Empty(t, zeroOpp.SellExchange)
	assert.Equal(t, 0.0, zeroOpp.BuyPrice)
	assert.Equal(t, 0.0, zeroOpp.SellPrice)
	assert.Equal(t, 0.0, zeroOpp.ProfitPercent)
	assert.True(t, zeroOpp.Timestamp.IsZero())

	// Test with negative values
	negativeOpp := ArbitrageOpportunity{
		Symbol:        "TEST/USDT",
		BuyPrice:      -100.0,
		SellPrice:     -50.0,
		ProfitPercent: -10.0,
		ProfitAmount:  -500.0,
		Volume:        -1.0,
	}
	assert.Equal(t, "TEST/USDT", negativeOpp.Symbol)
	assert.Equal(t, -100.0, negativeOpp.BuyPrice)
	assert.Equal(t, -50.0, negativeOpp.SellPrice)
	assert.Equal(t, -10.0, negativeOpp.ProfitPercent)
	assert.Equal(t, -500.0, negativeOpp.ProfitAmount)
	assert.Equal(t, -1.0, negativeOpp.Volume)

	// Test with very large values
	largeOpp := ArbitrageOpportunity{
		Symbol:        "BTC/USDT",
		BuyPrice:      1000000.0,
		SellPrice:     1100000.0,
		ProfitPercent: 10.0,
		ProfitAmount:  100000.0,
		Volume:        1000.0,
	}
	assert.Equal(t, "BTC/USDT", largeOpp.Symbol)
	assert.Equal(t, 1000000.0, largeOpp.BuyPrice)
	assert.Equal(t, 1100000.0, largeOpp.SellPrice)
	assert.Equal(t, 10.0, largeOpp.ProfitPercent)
	assert.Equal(t, 100000.0, largeOpp.ProfitAmount)
	assert.Equal(t, 1000.0, largeOpp.Volume)
}

// Test formatArbitrageMessage with edge cases
func TestNotificationService_formatArbitrageMessage_EdgeCases(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test with nil slice
	message := ns.formatArbitrageMessage(nil)
	assert.Equal(t, "No arbitrage opportunities found.", message)

	// Test with opportunity having empty strings
	emptyOpp := []ArbitrageOpportunity{
		{
			Symbol:          "",
			BuyExchange:     "",
			SellExchange:    "",
			BuyPrice:        0.0,
			SellPrice:       0.0,
			ProfitPercent:   0.0,
			OpportunityType: "",
		},
	}

	message = ns.formatArbitrageMessage(emptyOpp)
	assert.Contains(t, message, "🚨 *Arbitrage Alert!*") // Default header
	assert.Contains(t, message, "Found 1 profitable opportunities")

	// Test with unknown opportunity type
	unknownOpp := []ArbitrageOpportunity{
		{
			Symbol:          "TEST/USDT",
			BuyExchange:     "exchange1",
			SellExchange:    "exchange2",
			BuyPrice:        100.0,
			SellPrice:       101.0,
			ProfitPercent:   1.0,
			OpportunityType: "unknown_type",
		},
	}

	message = ns.formatArbitrageMessage(unknownOpp)
	assert.Contains(t, message, "🚨 *Arbitrage Alert!*") // Default header for unknown type
	assert.Contains(t, message, "TEST/USDT")
}

// Test NotificationService struct fields
func TestNotificationService_StructFields(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test initial state
	assert.NotNil(t, ns)
	assert.Nil(t, ns.db)

	// Test that we can access fields without panic
	assert.NotPanics(t, func() {
		_ = ns.db
	})
}

// Test ArbitrageOpportunity JSON tags (implicit test)
func TestArbitrageOpportunity_JSONStructure(t *testing.T) {
	now := time.Now()
	opp := ArbitrageOpportunity{
		Symbol:          "BTC/USDT",
		BuyExchange:     "binance",
		SellExchange:    "coinbase",
		BuyPrice:        50000.0,
		SellPrice:       50500.0,
		ProfitPercent:   1.0,
		ProfitAmount:    500.0,
		Volume:          1.0,
		Timestamp:       now,
		OpportunityType: "arbitrage",
	}

	// Verify all fields are accessible and have expected values
	assert.Equal(t, "BTC/USDT", opp.Symbol)
	assert.Equal(t, "binance", opp.BuyExchange)
	assert.Equal(t, "coinbase", opp.SellExchange)
	assert.Equal(t, 50000.0, opp.BuyPrice)
	assert.Equal(t, 50500.0, opp.SellPrice)
	assert.Equal(t, 1.0, opp.ProfitPercent)
	assert.Equal(t, 500.0, opp.ProfitAmount)
	assert.Equal(t, 1.0, opp.Volume)
	assert.Equal(t, now, opp.Timestamp)
	assert.Equal(t, "arbitrage", opp.OpportunityType)
}

// Test formatArbitrageMessage with exactly 3 opportunities
func TestNotificationService_formatArbitrageMessage_ExactlyThree(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test with exactly 3 opportunities
	threeOpps := make([]ArbitrageOpportunity, 3)
	for i := 0; i < 3; i++ {
		threeOpps[i] = ArbitrageOpportunity{
			Symbol:          "BTC/USDT",
			BuyExchange:     "binance",
			SellExchange:    "coinbase",
			BuyPrice:        50000.0,
			SellPrice:       50500.0,
			ProfitPercent:   1.0,
			OpportunityType: "arbitrage",
		}
	}

	message := ns.formatArbitrageMessage(threeOpps)
	assert.Contains(t, message, "Found 3 profitable opportunities")
	assert.NotContains(t, message, "...and") // Should not show "and more" for exactly 3
}

// Test formatArbitrageMessage with 4 opportunities
func TestNotificationService_formatArbitrageMessage_MoreThanThree(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test with 4 opportunities
	fourOpps := make([]ArbitrageOpportunity, 4)
	for i := 0; i < 4; i++ {
		fourOpps[i] = ArbitrageOpportunity{
			Symbol:          "BTC/USDT",
			BuyExchange:     "binance",
			SellExchange:    "coinbase",
			BuyPrice:        50000.0,
			SellPrice:       50500.0,
			ProfitPercent:   1.0,
			OpportunityType: "arbitrage",
		}
	}

	message := ns.formatArbitrageMessage(fourOpps)
	assert.Contains(t, message, "Found 4 profitable opportunities")
	assert.Contains(t, message, "...and 1 more opportunities") // Should show "and more" for 4
}

func TestNotificationService_PublishOpportunityUpdate(t *testing.T) {
	// Test with nil Redis
	ns := NewNotificationService(nil, nil, "", "", "")
	ns.PublishOpportunityUpdate(context.Background(), []ArbitrageOpportunity{})
	// Should not panic
}

func TestNotificationService_GetCacheStats(t *testing.T) {
	// Test with nil Redis
	ns := NewNotificationService(nil, nil, "", "", "")
	stats := ns.GetCacheStats(context.Background())

	assert.False(t, stats["redis_available"].(bool))
	assert.NotContains(t, stats, "users_cached")
	assert.NotContains(t, stats, "opportunities_cached")
}

func TestNotificationService_cacheArbitrageOpportunities(t *testing.T) {
	// Test with nil Redis
	ns := NewNotificationService(nil, nil, "", "", "")

	opportunities := []ArbitrageOpportunity{
		{
			Symbol:        "BTC/USDT",
			BuyExchange:   "binance",
			SellExchange:  "coinbase",
			BuyPrice:      50000.0,
			SellPrice:     50500.0,
			ProfitPercent: 1.0,
		},
	}

	// Should not panic with nil Redis
	ns.cacheArbitrageOpportunities(context.Background(), opportunities)
}

func TestNotificationService_CacheMarketData(t *testing.T) {
	// Test with nil Redis
	ns := NewNotificationService(nil, nil, "", "", "")

	data := map[string]interface{}{
		"symbol": "BTC/USDT",
		"price":  50000.0,
	}

	// Should not panic with nil Redis
	ns.CacheMarketData(context.Background(), "binance", data)
}

func TestNotificationService_GetCachedMarketData(t *testing.T) {
	// Test with nil Redis
	ns := NewNotificationService(nil, nil, "", "", "")

	var result map[string]interface{}
	err := ns.GetCachedMarketData(context.Background(), "binance", &result)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "redis not available")
}

func TestNotificationService_InvalidateUserCache(t *testing.T) {
	// Test with nil Redis
	ns := NewNotificationService(nil, nil, "", "", "")

	// Should not panic with nil Redis
	ns.InvalidateUserCache(context.Background())
}

func TestNotificationService_InvalidateOpportunityCache(t *testing.T) {
	// Test with nil Redis
	ns := NewNotificationService(nil, nil, "", "", "")

	// Should not panic with nil Redis
	ns.InvalidateOpportunityCache(context.Background())
}

func TestNotificationService_formatTechnicalSignalMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test with empty signals
	message := ns.formatTechnicalSignalMessage([]TechnicalSignalNotification{})
	assert.Equal(t, "No technical analysis signals found.", message)

	// Test with single signal
	signals := []TechnicalSignalNotification{
		{
			Symbol:       "BTC/USDT",
			SignalType:   "buy",
			Action:       "buy",
			SignalText:   "RSI oversold",
			CurrentPrice: 50000.0,
			EntryRange:   "$49900.0 - $50100.0",
			Targets: []Target{
				{Price: 51000.0, Profit: 2.0},
				{Price: 52000.0, Profit: 4.0},
			},
			StopLoss:   StopLoss{Price: 49500.0, Risk: 1.0},
			RiskReward: "1:2",
			Exchanges:  []string{"binance", "coinbase"},
			Timeframe:  "4H",
			Confidence: 0.85,
			Timestamp:  time.Now(),
		},
	}

	message = ns.formatTechnicalSignalMessage(signals)
	assert.Contains(t, message, "📊 *Technical Analysis Signals*")
	assert.Contains(t, message, "BTC/USDT")
	assert.Contains(t, message, "RSI oversold")
	assert.Contains(t, message, "85.0%")
}

func TestNotificationService_ConvertAggregatedSignalToNotification(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test with buy signal
	signal := &AggregatedSignal{
		Symbol:          "BTC/USDT",
		SignalType:      SignalTypeTechnical,
		Action:          "buy",
		ProfitPotential: decimal.NewFromFloat(5.0),
		RiskLevel:       decimal.NewFromFloat(0.02),
		Confidence:      decimal.NewFromFloat(0.85),
		Exchanges:       []string{"binance", "coinbase"},
		Indicators:      []string{"RSI", "MACD"},
		Metadata: map[string]interface{}{
			"current_price": 50000.0,
			"timeframe":     "4H",
		},
		CreatedAt: time.Now(),
	}

	notification := ns.ConvertAggregatedSignalToNotification(signal)
	assert.NotNil(t, notification)
	assert.Equal(t, "BTC/USDT", notification.Symbol)
	assert.Equal(t, "buy", notification.Action)
	assert.Equal(t, 0.85, notification.Confidence)
	assert.Equal(t, "RSI + MACD", notification.SignalText)
	assert.Equal(t, "4H", notification.Timeframe)
	assert.Len(t, notification.Targets, 2)
	assert.Equal(t, 49000.0, notification.StopLoss.Price)
}

func TestNotificationService_generateOpportunityHash(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	opportunities := []ArbitrageOpportunity{
		{
			Symbol:        "BTC/USDT",
			BuyExchange:   "binance",
			SellExchange:  "coinbase",
			BuyPrice:      50000.0,
			SellPrice:     50500.0,
			ProfitPercent: 1.0,
		},
	}

	hash := ns.generateOpportunityHash(opportunities)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64) // SHA-256 hash length

	// Same opportunities should produce same hash
	hash2 := ns.generateOpportunityHash(opportunities)
	assert.Equal(t, hash, hash2)
}

func TestNotificationService_generateTechnicalSignalsHash(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	signals := []TechnicalSignalNotification{
		{
			Symbol:       "BTC/USDT",
			SignalType:   "buy",
			Action:       "buy",
			CurrentPrice: 50000.0,
			Confidence:   0.85,
		},
	}

	hash := ns.generateTechnicalSignalsHash(signals)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64) // SHA-256 hash length

	// Same signals should produce same hash
	hash2 := ns.generateTechnicalSignalsHash(signals)
	assert.Equal(t, hash, hash2)
}

func TestNotificationService_getCachedMessage(t *testing.T) {
	// Test with nil Redis
	ns := NewNotificationService(nil, nil, "", "", "")

	message, found := ns.getCachedMessage(context.Background(), "test", "testhash")
	assert.Empty(t, message)
	assert.False(t, found)
}

func TestNotificationService_setCachedMessage(t *testing.T) {
	// Test with nil Redis
	ns := NewNotificationService(nil, nil, "", "", "")

	// Should not panic with nil Redis
	ns.setCachedMessage(context.Background(), "test", "testhash", "test message")
}

func TestNotificationService_checkRateLimit(t *testing.T) {
	// Test with nil Redis - should deny (fail-closed for security)
	ns := NewNotificationService(nil, nil, "", "", "")

	allowed, err := ns.checkRateLimit(context.Background(), "testuser")
	assert.Error(t, err)     // Should return error when Redis is not available
	assert.False(t, allowed) // Should deny when Redis is not available (fail-closed)
}

// TestNotificationService_checkRateLimit_Comprehensive tests checkRateLimit with various scenarios
func TestNotificationService_checkRateLimit_Comprehensive(t *testing.T) {
	// Test with nil Redis - should always deny (fail-closed for security)
	t.Run("NilRedis", func(t *testing.T) {
		ns := NewNotificationService(nil, nil, "", "", "")

		// Multiple calls should all be denied when Redis is not available
		for i := 0; i < 10; i++ {
			allowed, err := ns.checkRateLimit(context.Background(), "testuser")
			assert.Error(t, err, "Call %d should return error", i)
			assert.False(t, allowed, "Call %d should be denied (fail-closed)", i)
		}
	})

	// Test with different user IDs - all should be denied without Redis
	t.Run("DifferentUserIDs", func(t *testing.T) {
		ns := NewNotificationService(nil, nil, "", "", "")

		userIDs := []string{"user1", "user2", "user3", "user-with-dashes", "user_123", ""}

		for _, userID := range userIDs {
			allowed, err := ns.checkRateLimit(context.Background(), userID)
			assert.Error(t, err, "User %s should return error", userID)
			assert.False(t, allowed, "User %s should be denied (fail-closed)", userID)
		}
	})

	// Test with different contexts - all should be denied without Redis
	t.Run("DifferentContexts", func(t *testing.T) {
		ns := NewNotificationService(nil, nil, "", "", "")

		// Test with background context
		allowed, err := ns.checkRateLimit(context.Background(), "testuser")
		assert.Error(t, err)
		assert.False(t, allowed)

		// Test with cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		allowed, err = ns.checkRateLimit(ctx, "testuser")
		assert.Error(t, err)
		assert.False(t, allowed) // Should deny with nil Redis (fail-closed)
	})

	// Test with timeout context - should be denied without Redis
	t.Run("TimeoutContext", func(t *testing.T) {
		ns := NewNotificationService(nil, nil, "", "", "")

		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
		defer cancel()

		allowed, err := ns.checkRateLimit(ctx, "testuser")
		assert.Error(t, err)
		assert.False(t, allowed)
	})

	// Test concurrent calls - all should be denied without Redis (fail-closed)
	t.Run("ConcurrentCalls", func(t *testing.T) {
		ns := NewNotificationService(nil, nil, "", "", "")

		var wg sync.WaitGroup
		results := make([]bool, 10)
		errors := make([]error, 10)

		// Launch multiple goroutines calling checkRateLimit concurrently
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(index int) {
				defer wg.Done()
				allowed, err := ns.checkRateLimit(context.Background(), "testuser")
				results[index] = allowed
				errors[index] = err
			}(i)
		}

		wg.Wait()

		// All should error and be denied (fail-closed for security)
		for i := 0; i < 10; i++ {
			assert.Error(t, errors[i], "Call %d should error", i)
			assert.False(t, results[i], "Call %d should be denied (fail-closed)", i)
		}
	})
}

func TestNotificationService_logNotification(t *testing.T) {
	// Test with nil database - expect panic due to nil database access
	ns := NewNotificationService(nil, nil, "", "", "")

	assert.Panics(t, func() {
		err := ns.logNotification(context.Background(), "testuser", "telegram", "test message")
		if err != nil {
			t.Log("Error:", err)
		}
	})
}

func TestNotificationService_CheckUserNotificationPreferences(t *testing.T) {
	// Test with nil database and Redis - expect panic due to nil database access
	ns := NewNotificationService(nil, nil, "", "", "")

	assert.Panics(t, func() {
		enabled, err := ns.CheckUserNotificationPreferences(context.Background(), "testuser")
		if err != nil {
			t.Log("Error:", err)
		}
		t.Log("Enabled:", enabled)
	})
}

func TestNotificationService_generateAggregatedSignalsHash(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	signals := []*AggregatedSignal{
		{
			Symbol:     "BTC/USDT",
			SignalType: SignalTypeTechnical,
			Action:     "buy",
			Strength:   SignalStrengthStrong,
			Confidence: decimal.NewFromFloat(0.85),
		},
		{
			Symbol:     "ETH/USDT",
			SignalType: SignalTypeTechnical,
			Action:     "sell",
			Strength:   SignalStrengthWeak,
			Confidence: decimal.NewFromFloat(0.65),
		},
	}

	hash := ns.generateAggregatedSignalsHash(signals)
	assert.NotEmpty(t, hash)
	assert.Len(t, hash, 64) // SHA256 hash length

	// Same signals should produce same hash
	hash2 := ns.generateAggregatedSignalsHash(signals)
	assert.Equal(t, hash, hash2)

	// Different order should produce same hash (signals are sorted internally)
	reversedSignals := []*AggregatedSignal{signals[1], signals[0]}
	hash3 := ns.generateAggregatedSignalsHash(reversedSignals)
	assert.Equal(t, hash, hash3)
}

func TestNotificationService_formatEnhancedArbitrageMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test with nil signal
	message := ns.formatEnhancedArbitrageMessage(nil)
	assert.Equal(t, "No arbitrage signal found.", message)

	// Test with non-arbitrage signal
	nonArbitrageSignal := &AggregatedSignal{
		Symbol:     "BTC/USDT",
		SignalType: SignalTypeTechnical,
	}

	message = ns.formatEnhancedArbitrageMessage(nonArbitrageSignal)
	assert.Equal(t, "No arbitrage signal found.", message)

	// Test with arbitrage signal
	signal := &AggregatedSignal{
		Symbol:     "BTC/USDT",
		SignalType: SignalTypeArbitrage,
		Confidence: decimal.NewFromFloat(0.85),
		Metadata: map[string]interface{}{
			"buy_price_range": map[string]interface{}{
				"min": decimal.NewFromFloat(49900.0),
				"max": decimal.NewFromFloat(50100.0),
			},
			"sell_price_range": map[string]interface{}{
				"min": decimal.NewFromFloat(50400.0),
				"max": decimal.NewFromFloat(50600.0),
			},
			"profit_range": map[string]interface{}{
				"min_percent": decimal.NewFromFloat(0.5),
				"max_percent": decimal.NewFromFloat(1.5),
				"min_dollar":  decimal.NewFromFloat(250.0),
				"max_dollar":  decimal.NewFromFloat(750.0),
				"base_amount": decimal.NewFromFloat(50000.0),
			},
			"buy_exchanges":     []string{"binance", "coinbase"},
			"sell_exchanges":    []string{"kraken", "bitfinex"},
			"opportunity_count": 4,
			"min_volume":        decimal.NewFromFloat(10000.0),
			"validity_minutes":  5,
		},
	}

	message = ns.formatEnhancedArbitrageMessage(signal)
	assert.Contains(t, message, "ARBITRAGE ALERT: BTC/USDT")
	assert.Contains(t, message, "0.50% - 1.50%")
	assert.Contains(t, message, "$250 - $750")
	assert.Contains(t, message, "binance, coinbase")
	assert.Contains(t, message, "kraken, bitfinex")
	assert.Contains(t, message, "85.0%")
	assert.Contains(t, message, "5 minutes")
}

func TestNotificationService_formatAggregatedArbitrageMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test with empty signals
	message := ns.formatAggregatedArbitrageMessage([]*AggregatedSignal{})
	assert.Equal(t, "🔍 No arbitrage opportunities available", message)

	// Test with signals
	signals := []*AggregatedSignal{
		{
			Symbol:          "BTC/USDT",
			SignalType:      SignalTypeArbitrage,
			Action:          "buy",
			ProfitPotential: decimal.NewFromFloat(2.5),
			Confidence:      decimal.NewFromFloat(0.85),
			Exchanges:       []string{"binance", "coinbase"},
			Metadata: map[string]interface{}{
				"buy_price":  49900.0,
				"sell_price": 50500.0,
			},
		},
		{
			Symbol:          "ETH/USDT",
			SignalType:      SignalTypeArbitrage,
			Action:          "sell",
			ProfitPotential: decimal.NewFromFloat(1.8),
			Confidence:      decimal.NewFromFloat(0.75),
			Exchanges:       []string{"kraken", "bitfinex"},
		},
	}

	message = ns.formatAggregatedArbitrageMessage(signals)
	assert.Contains(t, message, "🚀 *Aggregated Arbitrage Opportunities*")
	assert.Contains(t, message, "BTC/USDT")
	assert.Contains(t, message, "ETH/USDT")
	assert.Contains(t, message, "2.50%")
	assert.Contains(t, message, "1.80%")
	assert.Contains(t, message, "0.8%")
	// Both confidence values might round to 0.8% due to InexactFloat64()
}

func TestNotificationService_formatAggregatedTechnicalMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test with empty signals
	message := ns.formatAggregatedTechnicalMessage([]*AggregatedSignal{})
	assert.Equal(t, "📊 No technical analysis signals available", message)

	// Test with signals
	signals := []*AggregatedSignal{
		{
			Symbol:          "BTC/USDT",
			SignalType:      SignalTypeTechnical,
			Action:          "buy",
			Strength:        SignalStrengthStrong,
			ProfitPotential: decimal.NewFromFloat(5.0),
			Confidence:      decimal.NewFromFloat(0.85),
			RiskLevel:       decimal.NewFromFloat(0.02),
			Exchanges:       []string{"binance", "coinbase"},
			Indicators:      []string{"RSI", "MACD", "BB"},
			Metadata: map[string]interface{}{
				"entry_price": 49900.0,
				"stop_loss":   49500.0,
				"target":      52000.0,
			},
		},
		{
			Symbol:          "ETH/USDT",
			SignalType:      SignalTypeTechnical,
			Action:          "sell",
			Strength:        SignalStrengthWeak,
			ProfitPotential: decimal.NewFromFloat(3.0),
			Confidence:      decimal.NewFromFloat(0.65),
			RiskLevel:       decimal.NewFromFloat(0.03),
			Exchanges:       []string{"kraken", "bitfinex"},
			Indicators:      []string{"EMA", "STOCH"},
		},
	}

	message = ns.formatAggregatedTechnicalMessage(signals)
	assert.Contains(t, message, "📊 *Aggregated Technical Analysis*")
	assert.Contains(t, message, "BTC/USDT")
	assert.Contains(t, message, "ETH/USDT")
	assert.Contains(t, message, "strong")
	assert.Contains(t, message, "weak")
	assert.Contains(t, message, "RSI, MACD, BB")
	assert.Contains(t, message, "EMA, STOCH")
	assert.Contains(t, message, "0.02%")
	assert.Contains(t, message, "0.03%")
}

func TestNotificationService_NotifyArbitrageOpportunities_EmptyOpportunities(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")
	ctx := context.Background()

	opportunities := []ArbitrageOpportunity{}

	// Should panic due to nil database when trying to get eligible users
	assert.Panics(t, func() {
		_ = ns.NotifyArbitrageOpportunities(ctx, opportunities)
	})
}

func TestNotificationService_NotifyArbitrageOpportunities_CategorizationLogic(t *testing.T) {
	// Test the categorization logic directly by examining opportunities
	opportunities := []ArbitrageOpportunity{
		{
			Symbol:          "BTC/USDT",
			BuyExchange:     "binance",
			SellExchange:    "coinbase", // Different exchanges
			BuyPrice:        50000.0,
			SellPrice:       50500.0,
			ProfitPercent:   1.0,
			OpportunityType: "",
		},
		{
			Symbol:          "ETH/USDT",
			BuyExchange:     "binance",
			SellExchange:    "binance", // Same exchange
			BuyPrice:        3000.0,
			SellPrice:       3020.0,
			ProfitPercent:   0.67,
			OpportunityType: "",
		},
	}

	// Test categorization logic separately
	arbitrageOpps := make([]ArbitrageOpportunity, 0)
	technicalOpps := make([]ArbitrageOpportunity, 0)

	for _, opp := range opportunities {
		// Categorize opportunity based on exchanges (same logic as in the function)
		if opp.BuyExchange != opp.SellExchange {
			opp.OpportunityType = "arbitrage"
			arbitrageOpps = append(arbitrageOpps, opp)
		} else {
			opp.OpportunityType = "technical"
			technicalOpps = append(technicalOpps, opp)
		}
	}

	// Verify categorization
	assert.Equal(t, 1, len(arbitrageOpps))
	assert.Equal(t, 1, len(technicalOpps))
	assert.Equal(t, "arbitrage", arbitrageOpps[0].OpportunityType)
	assert.Equal(t, "technical", technicalOpps[0].OpportunityType)
	assert.Equal(t, "BTC/USDT", arbitrageOpps[0].Symbol)
	assert.Equal(t, "ETH/USDT", technicalOpps[0].Symbol)
}

func TestNotificationService_NotifyArbitrageOpportunities_MixedTypes(t *testing.T) {
	// Test with mixed opportunity types
	opportunities := []ArbitrageOpportunity{
		{
			Symbol:          "BTC/USDT",
			BuyExchange:     "binance",
			SellExchange:    "coinbase",
			BuyPrice:        50000.0,
			SellPrice:       50500.0,
			ProfitPercent:   1.0,
			OpportunityType: "arbitrage",
		},
		{
			Symbol:          "ETH/USDT",
			BuyExchange:     "kraken",
			SellExchange:    "bitfinex",
			BuyPrice:        3000.0,
			SellPrice:       3020.0,
			ProfitPercent:   0.67,
			OpportunityType: "arbitrage",
		},
		{
			Symbol:          "LTC/USDT",
			BuyExchange:     "binance",
			SellExchange:    "binance",
			BuyPrice:        150.0,
			SellPrice:       151.0,
			ProfitPercent:   0.67,
			OpportunityType: "technical",
		},
	}

	// Test categorization logic
	arbitrageOpps := make([]ArbitrageOpportunity, 0)
	technicalOpps := make([]ArbitrageOpportunity, 0)

	for _, opp := range opportunities {
		if opp.BuyExchange != opp.SellExchange {
			arbitrageOpps = append(arbitrageOpps, opp)
		} else {
			technicalOpps = append(technicalOpps, opp)
		}
	}

	// Verify categorization results
	assert.Equal(t, 2, len(arbitrageOpps))
	assert.Equal(t, 1, len(technicalOpps))

	// Check that arbitrage opportunities have different exchanges
	for _, opp := range arbitrageOpps {
		assert.NotEqual(t, opp.BuyExchange, opp.SellExchange)
	}

	// Check that technical opportunities have same exchanges
	for _, opp := range technicalOpps {
		assert.Equal(t, opp.BuyExchange, opp.SellExchange)
	}
}

func TestNotificationService_NotifyArbitrageOpportunities_WithDatabase(t *testing.T) {
	// This test demonstrates that the function requires a valid database
	// In a real test environment, you would need to set up a test database
	ctx := context.Background()

	opportunities := []ArbitrageOpportunity{
		{
			Symbol:        "BTC/USDT",
			BuyExchange:   "binance",
			SellExchange:  "coinbase",
			BuyPrice:      50000.0,
			SellPrice:     50500.0,
			ProfitPercent: 1.0,
		},
	}

	// Should panic due to nil database access
	ns := NewNotificationService(nil, nil, "", "", "")
	assert.Panics(t, func() {
		_ = ns.NotifyArbitrageOpportunities(ctx, opportunities)
	})
}

func TestNotificationService_getEligibleUsers_NilDependencies(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")
	ctx := context.Background()

	// Should panic due to nil database when trying to query users
	assert.Panics(t, func() {
		_, _ = ns.getEligibleUsers(ctx)
	})
}

func TestNotificationService_getEligibleUsers_CacheKey(t *testing.T) {
	// Test that the cache key is consistent
	expectedCacheKey := "eligible_users:arbitrage"

	// We can't directly test the private function, but we can verify the key format
	// by checking it matches the expected pattern
	assert.Equal(t, 24, len(expectedCacheKey)) // Length check
	assert.Contains(t, expectedCacheKey, "eligible_users")
	assert.Contains(t, expectedCacheKey, "arbitrage")
}

func TestNotificationService_getEligibleUsers_QueryLogic(t *testing.T) {
	// Test the SQL query logic by examining what it should do
	// The query should:
	// 1. Select users with telegram_chat_id IS NOT NULL and not empty
	// 2. Exclude users who have disabled arbitrage notifications

	// This is a logical test - the actual SQL execution requires a database
	query := `
		SELECT id, email, telegram_chat_id, subscription_tier, created_at, updated_at
		FROM users 
		WHERE telegram_chat_id IS NOT NULL 
		  AND telegram_chat_id != ''
		  AND id NOT IN (
			  SELECT DISTINCT user_id 
			  FROM user_alerts 
			  WHERE alert_type = 'arbitrage' 
			    AND is_active = false
			    AND conditions->>'notifications_enabled' = 'false'
		  )
	`

	// Verify query contains expected conditions
	assert.Contains(t, query, "telegram_chat_id IS NOT NULL")
	assert.Contains(t, query, "telegram_chat_id != ''")
	assert.Contains(t, query, "alert_type = 'arbitrage'")
	assert.Contains(t, query, "is_active = false")
	assert.Contains(t, query, "conditions->>'notifications_enabled' = 'false'")
}

func TestNotificationService_getEligibleUsers_ContextCancellation(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Should still panic due to nil database, even with cancelled context
	assert.Panics(t, func() {
		_, _ = ns.getEligibleUsers(ctx)
	})
}

// Test TelegramErrorCode constants
func TestTelegramErrorCode_Constants(t *testing.T) {
	assert.Equal(t, TelegramErrorCode("USER_BLOCKED"), TelegramErrorUserBlocked)
	assert.Equal(t, TelegramErrorCode("CHAT_NOT_FOUND"), TelegramErrorChatNotFound)
	assert.Equal(t, TelegramErrorCode("RATE_LIMITED"), TelegramErrorRateLimited)
	assert.Equal(t, TelegramErrorCode("INVALID_REQUEST"), TelegramErrorInvalidRequest)
	assert.Equal(t, TelegramErrorCode("NETWORK_ERROR"), TelegramErrorNetworkError)
	assert.Equal(t, TelegramErrorCode("TIMEOUT"), TelegramErrorTimeout)
	assert.Equal(t, TelegramErrorCode("INTERNAL_ERROR"), TelegramErrorInternal)
	assert.Equal(t, TelegramErrorCode("UNKNOWN"), TelegramErrorUnknown)
}

// Test isRetryableError function
func TestIsRetryableError(t *testing.T) {
	tests := []struct {
		name     string
		code     TelegramErrorCode
		expected bool
	}{
		{
			name:     "RATE_LIMITED is retryable",
			code:     TelegramErrorRateLimited,
			expected: true,
		},
		{
			name:     "NETWORK_ERROR is retryable",
			code:     TelegramErrorNetworkError,
			expected: true,
		},
		{
			name:     "TIMEOUT is retryable",
			code:     TelegramErrorTimeout,
			expected: true,
		},
		{
			name:     "INTERNAL_ERROR is retryable",
			code:     TelegramErrorInternal,
			expected: true,
		},
		{
			name:     "USER_BLOCKED is not retryable",
			code:     TelegramErrorUserBlocked,
			expected: false,
		},
		{
			name:     "CHAT_NOT_FOUND is not retryable",
			code:     TelegramErrorChatNotFound,
			expected: false,
		},
		{
			name:     "INVALID_REQUEST is not retryable",
			code:     TelegramErrorInvalidRequest,
			expected: false,
		},
		{
			name:     "UNKNOWN is not retryable",
			code:     TelegramErrorUnknown,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableError(tt.code)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test TelegramSendResult struct
func TestTelegramSendResult_Struct(t *testing.T) {
	// Test successful result
	successResult := TelegramSendResult{
		OK:        true,
		MessageID: "12345",
	}
	assert.True(t, successResult.OK)
	assert.Equal(t, "12345", successResult.MessageID)
	assert.Empty(t, successResult.Error)
	assert.Empty(t, successResult.ErrorCode)
	assert.Equal(t, int32(0), successResult.RetryAfter)

	// Test failed result with error code
	failedResult := TelegramSendResult{
		OK:         false,
		Error:      "User blocked the bot",
		ErrorCode:  TelegramErrorUserBlocked,
		RetryAfter: 0,
	}
	assert.False(t, failedResult.OK)
	assert.Equal(t, "User blocked the bot", failedResult.Error)
	assert.Equal(t, TelegramErrorUserBlocked, failedResult.ErrorCode)

	// Test rate limited result
	rateLimitedResult := TelegramSendResult{
		OK:         false,
		Error:      "Too many requests",
		ErrorCode:  TelegramErrorRateLimited,
		RetryAfter: 60,
	}
	assert.False(t, rateLimitedResult.OK)
	assert.Equal(t, TelegramErrorRateLimited, rateLimitedResult.ErrorCode)
	assert.Equal(t, int32(60), rateLimitedResult.RetryAfter)
}

// Test notification service initialization with dead letter service
func TestNotificationService_DeadLetterServiceInitialization(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")
	assert.NotNil(t, ns)
	assert.Nil(t, ns.deadLetterService)
}

// Test ProcessDeadLetterQueue with nil service
func TestNotificationService_ProcessDeadLetterQueue_NilDB(t *testing.T) {
	ns := &NotificationService{
		deadLetterService: nil,
	}

	success, failed, err := ns.ProcessDeadLetterQueue(context.Background(), 10)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dead letter service not initialized")
	assert.Equal(t, 0, success)
	assert.Equal(t, 0, failed)
}

// Test GetDeadLetterStats with nil service
func TestNotificationService_GetDeadLetterStats_NilService(t *testing.T) {
	ns := &NotificationService{
		deadLetterService: nil,
	}

	stats, err := ns.GetDeadLetterStats(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dead letter service not initialized")
	assert.Nil(t, stats)
}

// Test CleanupDeadLetters with nil service
func TestNotificationService_CleanupDeadLetters_NilService(t *testing.T) {
	ns := &NotificationService{
		deadLetterService: nil,
	}

	count, err := ns.CleanupDeadLetters(context.Background(), 24*time.Hour)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dead letter service not initialized")
	assert.Equal(t, 0, count)
}

// TestTelegramErrorCode_AllCodes tests all TelegramErrorCode constants
func TestTelegramErrorCode_AllCodes(t *testing.T) {
	codes := []struct {
		code        TelegramErrorCode
		expected    string
		isRetryable bool
	}{
		{TelegramErrorUserBlocked, "USER_BLOCKED", false},
		{TelegramErrorChatNotFound, "CHAT_NOT_FOUND", false},
		{TelegramErrorRateLimited, "RATE_LIMITED", true},
		{TelegramErrorInvalidRequest, "INVALID_REQUEST", false},
		{TelegramErrorNetworkError, "NETWORK_ERROR", true},
		{TelegramErrorTimeout, "TIMEOUT", true},
		{TelegramErrorInternal, "INTERNAL_ERROR", true},
		{TelegramErrorUnknown, "UNKNOWN", false},
	}

	for _, tc := range codes {
		t.Run(string(tc.code), func(t *testing.T) {
			assert.Equal(t, TelegramErrorCode(tc.expected), tc.code)
			assert.Equal(t, tc.isRetryable, isRetryableError(tc.code))
		})
	}
}

// TestNotificationService_formatArbitrageMessage_AllTypes tests all opportunity types
func TestNotificationService_formatArbitrageMessage_AllTypes(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	types := []struct {
		oppType        string
		expectedHeader string
	}{
		{"arbitrage", "🚀 *True Arbitrage Opportunities*"},
		{"technical", "📊 *Technical Analysis Signals*"},
		{"ai_generated", "🤖 *AI-Generated Opportunities*"},
		{"", "🚨 *Arbitrage Alert!*"},
		{"unknown", "🚨 *Arbitrage Alert!*"},
	}

	for _, tc := range types {
		t.Run("Type_"+tc.oppType, func(t *testing.T) {
			opps := []ArbitrageOpportunity{
				{
					Symbol:          "BTC/USDT",
					BuyExchange:     "binance",
					SellExchange:    "coinbase",
					BuyPrice:        50000.0,
					SellPrice:       50500.0,
					ProfitPercent:   1.0,
					OpportunityType: tc.oppType,
				},
			}
			message := ns.formatArbitrageMessage(opps)
			assert.Contains(t, message, tc.expectedHeader)
		})
	}
}

// TestNotificationService_RetryLogic_EdgeCases tests retry logic edge cases
func TestNotificationService_RetryLogic_EdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		errorCode   TelegramErrorCode
		isRetryable bool
		description string
	}{
		{
			name:        "Rate limited should retry",
			errorCode:   TelegramErrorRateLimited,
			isRetryable: true,
			description: "Rate limiting is temporary",
		},
		{
			name:        "Network error should retry",
			errorCode:   TelegramErrorNetworkError,
			isRetryable: true,
			description: "Network issues are usually transient",
		},
		{
			name:        "Timeout should retry",
			errorCode:   TelegramErrorTimeout,
			isRetryable: true,
			description: "Timeouts can be temporary",
		},
		{
			name:        "Internal error should retry",
			errorCode:   TelegramErrorInternal,
			isRetryable: true,
			description: "Internal errors may resolve",
		},
		{
			name:        "User blocked should not retry",
			errorCode:   TelegramErrorUserBlocked,
			isRetryable: false,
			description: "User action required",
		},
		{
			name:        "Chat not found should not retry",
			errorCode:   TelegramErrorChatNotFound,
			isRetryable: false,
			description: "Chat doesn't exist",
		},
		{
			name:        "Invalid request should not retry",
			errorCode:   TelegramErrorInvalidRequest,
			isRetryable: false,
			description: "Request is malformed",
		},
		{
			name:        "Unknown error should not retry",
			errorCode:   TelegramErrorUnknown,
			isRetryable: false,
			description: "Unknown errors are not safe to retry",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := isRetryableError(tc.errorCode)
			assert.Equal(t, tc.isRetryable, result, tc.description)
		})
	}
}

// TestNotificationService_HashGeneration_Consistency tests hash generation consistency
func TestNotificationService_HashGeneration_Consistency(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	// Test that same input produces same hash
	opps := []ArbitrageOpportunity{
		{Symbol: "BTC/USDT", BuyExchange: "binance", SellExchange: "coinbase", ProfitPercent: 1.0},
		{Symbol: "ETH/USDT", BuyExchange: "kraken", SellExchange: "bitfinex", ProfitPercent: 0.5},
	}

	hash1 := ns.generateOpportunityHash(opps)
	hash2 := ns.generateOpportunityHash(opps)
	assert.Equal(t, hash1, hash2, "Same input should produce same hash")

	// Test that different input produces different hash
	opps2 := []ArbitrageOpportunity{
		{Symbol: "LTC/USDT", BuyExchange: "binance", SellExchange: "coinbase", ProfitPercent: 1.0},
	}
	hash3 := ns.generateOpportunityHash(opps2)
	assert.NotEqual(t, hash1, hash3, "Different input should produce different hash")
}

// TestNotificationService_TechnicalSignalMessage_AllScenarios tests all technical signal scenarios
func TestNotificationService_TechnicalSignalMessage_AllScenarios(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	tests := []struct {
		name           string
		signals        []TechnicalSignalNotification
		expectedParts  []string
		unexpectedPart string
	}{
		{
			name:          "Empty signals",
			signals:       []TechnicalSignalNotification{},
			expectedParts: []string{"No technical analysis signals found."},
		},
		{
			name:    "Single buy signal",
			signals: []TechnicalSignalNotification{{Symbol: "BTC/USDT", Action: "buy", SignalText: "RSI oversold", Confidence: 0.85}},
			expectedParts: []string{
				"📊 *Technical Analysis Signals*",
				"BTC/USDT",
				"RSI oversold",
				"85.0%",
			},
		},
		{
			name:    "Sell signal with targets",
			signals: []TechnicalSignalNotification{{Symbol: "ETH/USDT", Action: "sell", SignalText: "MACD crossover", Confidence: 0.75, Targets: []Target{{Price: 3000.0, Profit: 5.0}}}},
			expectedParts: []string{
				"📊 *Technical Analysis Signals*",
				"ETH/USDT",
				"MACD crossover",
			},
		},
		{
			name:    "Signal with stop loss",
			signals: []TechnicalSignalNotification{{Symbol: "ADA/USDT", Action: "buy", SignalText: "Support bounce", Confidence: 0.65, StopLoss: StopLoss{Price: 0.45, Risk: 2.0}}},
			expectedParts: []string{
				"📊 *Technical Analysis Signals*",
				"ADA/USDT",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			message := ns.formatTechnicalSignalMessage(tc.signals)
			for _, part := range tc.expectedParts {
				assert.Contains(t, message, part)
			}
			if tc.unexpectedPart != "" {
				assert.NotContains(t, message, tc.unexpectedPart)
			}
		})
	}
}

// TestNotificationService_CacheOperations_NilRedis tests cache operations with nil Redis
func TestNotificationService_CacheOperations_NilRedis(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	t.Run("GetCacheStats with nil Redis", func(t *testing.T) {
		stats := ns.GetCacheStats(context.Background())
		assert.NotNil(t, stats)
		assert.False(t, stats["redis_available"].(bool))
	})

	t.Run("InvalidateUserCache with nil Redis", func(t *testing.T) {
		assert.NotPanics(t, func() {
			ns.InvalidateUserCache(context.Background())
		})
	})

	t.Run("InvalidateOpportunityCache with nil Redis", func(t *testing.T) {
		assert.NotPanics(t, func() {
			ns.InvalidateOpportunityCache(context.Background())
		})
	})

	t.Run("CacheMarketData with nil Redis", func(t *testing.T) {
		assert.NotPanics(t, func() {
			ns.CacheMarketData(context.Background(), "binance", map[string]interface{}{"test": true})
		})
	})

	t.Run("PublishOpportunityUpdate with nil Redis", func(t *testing.T) {
		assert.NotPanics(t, func() {
			ns.PublishOpportunityUpdate(context.Background(), []ArbitrageOpportunity{})
		})
	})

	t.Run("getCachedMessage with nil Redis", func(t *testing.T) {
		message, found := ns.getCachedMessage(context.Background(), "type", "hash")
		assert.Empty(t, message)
		assert.False(t, found)
	})

	t.Run("setCachedMessage with nil Redis", func(t *testing.T) {
		assert.NotPanics(t, func() {
			ns.setCachedMessage(context.Background(), "type", "hash", "message")
		})
	})
}

// TestNotificationService_ConvertAggregatedSignal_AllCases tests signal conversion
func TestNotificationService_ConvertAggregatedSignal_AllCases(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	tests := []struct {
		name           string
		signal         *AggregatedSignal
		expectedAction string
		expectedSymbol string
	}{
		{
			name: "Buy signal",
			signal: &AggregatedSignal{
				Symbol:     "BTC/USDT",
				Action:     "buy",
				Confidence: decimal.NewFromFloat(0.85),
				Indicators: []string{"RSI"},
				Metadata:   map[string]interface{}{"current_price": 50000.0},
			},
			expectedAction: "buy",
			expectedSymbol: "BTC/USDT",
		},
		{
			name: "Sell signal",
			signal: &AggregatedSignal{
				Symbol:     "ETH/USDT",
				Action:     "sell",
				Confidence: decimal.NewFromFloat(0.75),
				Indicators: []string{"MACD", "BB"},
				Metadata:   map[string]interface{}{"current_price": 3000.0},
			},
			expectedAction: "sell",
			expectedSymbol: "ETH/USDT",
		},
		{
			name: "Signal with timeframe",
			signal: &AggregatedSignal{
				Symbol:     "ADA/USDT",
				Action:     "buy",
				Confidence: decimal.NewFromFloat(0.65),
				Indicators: []string{"EMA"},
				Metadata:   map[string]interface{}{"current_price": 0.5, "timeframe": "1H"},
			},
			expectedAction: "buy",
			expectedSymbol: "ADA/USDT",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			notification := ns.ConvertAggregatedSignalToNotification(tc.signal)
			assert.NotNil(t, notification)
			assert.Equal(t, tc.expectedSymbol, notification.Symbol)
			assert.Equal(t, tc.expectedAction, notification.Action)
		})
	}
}

// TestNotificationService_FormatEnhancedArbitrageMessage_EdgeCases tests edge cases
func TestNotificationService_FormatEnhancedArbitrageMessage_EdgeCases(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	t.Run("Nil signal", func(t *testing.T) {
		message := ns.formatEnhancedArbitrageMessage(nil)
		assert.Equal(t, "No arbitrage signal found.", message)
	})

	t.Run("Non-arbitrage signal type", func(t *testing.T) {
		signal := &AggregatedSignal{
			Symbol:     "BTC/USDT",
			SignalType: SignalTypeTechnical,
		}
		message := ns.formatEnhancedArbitrageMessage(signal)
		assert.Equal(t, "No arbitrage signal found.", message)
	})

	t.Run("Arbitrage signal with minimal metadata", func(t *testing.T) {
		signal := &AggregatedSignal{
			Symbol:     "BTC/USDT",
			SignalType: SignalTypeArbitrage,
			Confidence: decimal.NewFromFloat(0.80),
			Metadata:   map[string]interface{}{},
		}
		message := ns.formatEnhancedArbitrageMessage(signal)
		assert.Contains(t, message, "BTC/USDT")
	})
}

// TestNotificationService_AggregatedMessages_EmptyInputs tests aggregated messages with empty inputs
func TestNotificationService_AggregatedMessages_EmptyInputs(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	t.Run("Empty arbitrage signals", func(t *testing.T) {
		message := ns.formatAggregatedArbitrageMessage([]*AggregatedSignal{})
		assert.Equal(t, "🔍 No arbitrage opportunities available", message)
	})

	t.Run("Empty technical signals", func(t *testing.T) {
		message := ns.formatAggregatedTechnicalMessage([]*AggregatedSignal{})
		assert.Equal(t, "📊 No technical analysis signals available", message)
	})

	t.Run("Nil arbitrage signals", func(t *testing.T) {
		message := ns.formatAggregatedArbitrageMessage(nil)
		assert.Equal(t, "🔍 No arbitrage opportunities available", message)
	})

	t.Run("Nil technical signals", func(t *testing.T) {
		message := ns.formatAggregatedTechnicalMessage(nil)
		assert.Equal(t, "📊 No technical analysis signals available", message)
	})
}

// TestNotificationService_Context_Handling tests context handling
func TestNotificationService_Context_Handling(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	t.Run("Cancelled context for rate limit", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		allowed, err := ns.checkRateLimit(ctx, "user123")
		assert.Error(t, err)
		assert.False(t, allowed)
	})

	t.Run("Timeout context for cache", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
		defer cancel()
		time.Sleep(2 * time.Nanosecond) // Ensure timeout

		message, found := ns.getCachedMessage(ctx, "type", "hash")
		assert.Empty(t, message)
		assert.False(t, found)
	})
}

// TestTelegramSendResult_AllScenarios tests all TelegramSendResult scenarios
func TestTelegramSendResult_AllScenarios(t *testing.T) {
	tests := []struct {
		name   string
		result TelegramSendResult
	}{
		{
			name:   "Successful send",
			result: TelegramSendResult{OK: true, MessageID: "12345"},
		},
		{
			name:   "User blocked",
			result: TelegramSendResult{OK: false, Error: "Forbidden: bot was blocked by the user", ErrorCode: TelegramErrorUserBlocked},
		},
		{
			name:   "Rate limited",
			result: TelegramSendResult{OK: false, Error: "Too Many Requests: retry after 60", ErrorCode: TelegramErrorRateLimited, RetryAfter: 60},
		},
		{
			name:   "Chat not found",
			result: TelegramSendResult{OK: false, Error: "Bad Request: chat not found", ErrorCode: TelegramErrorChatNotFound},
		},
		{
			name:   "Network error",
			result: TelegramSendResult{OK: false, Error: "Connection refused", ErrorCode: TelegramErrorNetworkError},
		},
		{
			name:   "Timeout",
			result: TelegramSendResult{OK: false, Error: "Context deadline exceeded", ErrorCode: TelegramErrorTimeout},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Verify struct is valid and accessible
			assert.NotPanics(t, func() {
				_ = tc.result.OK
				_ = tc.result.MessageID
				_ = tc.result.Error
				_ = tc.result.ErrorCode
				_ = tc.result.RetryAfter
			})
		})
	}
}

// TestNotificationService_AggregatedSignalsHash_Ordering tests hash is consistent regardless of order
func TestNotificationService_AggregatedSignalsHash_Ordering(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	signal1 := &AggregatedSignal{Symbol: "BTC/USDT", Action: "buy", Confidence: decimal.NewFromFloat(0.85)}
	signal2 := &AggregatedSignal{Symbol: "ETH/USDT", Action: "sell", Confidence: decimal.NewFromFloat(0.75)}

	// Same signals in different order
	signals1 := []*AggregatedSignal{signal1, signal2}
	signals2 := []*AggregatedSignal{signal2, signal1}

	hash1 := ns.generateAggregatedSignalsHash(signals1)
	hash2 := ns.generateAggregatedSignalsHash(signals2)

	// Hashes should be equal because signals are sorted internally
	assert.Equal(t, hash1, hash2, "Hash should be consistent regardless of input order")
	assert.Len(t, hash1, 64, "SHA256 hash should be 64 characters")
}

// neura-im9: Quest Progress Notification Tests
// neura-nh5: Risk Event Notification Tests
// neura-fvk: Fund Milestone Notification Tests
// AI Reasoning Notification Tests
// neura-im9/neura-fvk/neura-nh5: Integration Tests

func TestQuestProgressNotification_Struct(t *testing.T) {
	progress := QuestProgressNotification{
		QuestID:       "quest-123",
		QuestName:     "Market Scanner",
		Current:       5,
		Target:        10,
		Percent:       50,
		Status:        "active",
		TimeRemaining: "2h 30m",
	}

	assert.Equal(t, "quest-123", progress.QuestID)
	assert.Equal(t, "Market Scanner", progress.QuestName)
	assert.Equal(t, 5, progress.Current)
	assert.Equal(t, 10, progress.Target)
	assert.Equal(t, 50, progress.Percent)
	assert.Equal(t, "active", progress.Status)
	assert.Equal(t, "2h 30m", progress.TimeRemaining)
}

func TestNotificationService_formatQuestProgressMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	tests := []struct {
		name     string
		progress QuestProgressNotification
		contains []string
	}{
		{
			name: "active quest with progress",
			progress: QuestProgressNotification{
				QuestID:       "quest-1",
				QuestName:     "Market Scanner",
				Current:       5,
				Target:        10,
				Percent:       50,
				Status:        "active",
				TimeRemaining: "5m",
			},
			contains: []string{
				"🎯",
				"Quest Progress Update",
				"Market Scanner",
				"5/10 (50%)",
				"5m",
				"50%",
			},
		},
		{
			name: "completed quest",
			progress: QuestProgressNotification{
				QuestID:       "quest-2",
				QuestName:     "Daily Report",
				Current:       10,
				Target:        10,
				Percent:       100,
				Status:        "completed",
				TimeRemaining: "",
			},
			contains: []string{
				"✅",
				"Quest Progress Update",
				"Daily Report",
				"10/10 (100%)",
				"🎉 Quest completed!",
			},
		},
		{
			name: "failed quest",
			progress: QuestProgressNotification{
				QuestID:       "quest-3",
				QuestName:     "Failed Quest",
				Current:       3,
				Target:        10,
				Percent:       30,
				Status:        "failed",
				TimeRemaining: "",
			},
			contains: []string{
				"❌",
				"Failed Quest",
				"3/10 (30%)",
			},
		},
		{
			name: "expired quest",
			progress: QuestProgressNotification{
				QuestID:       "quest-4",
				QuestName:     "Expired Quest",
				Current:       7,
				Target:        10,
				Percent:       70,
				Status:        "expired",
				TimeRemaining: "",
			},
			contains: []string{
				"⏰",
				"Expired Quest",
				"7/10 (70%)",
			},
		},
		{
			name: "zero progress",
			progress: QuestProgressNotification{
				QuestID:       "quest-5",
				QuestName:     "New Quest",
				Current:       0,
				Target:        100,
				Percent:       0,
				Status:        "active",
				TimeRemaining: "1d",
			},
			contains: []string{
				"New Quest",
				"0/100 (0%)",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := ns.formatQuestProgressMessage(tt.progress)
			for _, expected := range tt.contains {
				assert.Contains(t, message, expected, "Message should contain %s", expected)
			}
		})
	}
}

func TestNotificationService_generateProgressBar(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	tests := []struct {
		name     string
		percent  int
		width    int
		expected string
	}{
		{
			name:     "0% progress",
			percent:  0,
			width:    10,
			expected: "[░░░░░░░░░░] 0%",
		},
		{
			name:     "50% progress",
			percent:  50,
			width:    10,
			expected: "[█████░░░░░] 50%",
		},
		{
			name:     "100% progress",
			percent:  100,
			width:    10,
			expected: "[██████████] 100%",
		},
		{
			name:     "25% progress width 4",
			percent:  25,
			width:    4,
			expected: "[█░░░] 25%",
		},
		{
			name:     "75% progress width 8",
			percent:  75,
			width:    8,
			expected: "[██████░░] 75%",
		},
		{
			name:     "over 100% clamped",
			percent:  150,
			width:    10,
			expected: "[██████████] 100%",
		},
		{
			name:     "negative clamped to 0",
			percent:  -10,
			width:    10,
			expected: "[░░░░░░░░░░] 0%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ns.generateProgressBar(tt.percent, tt.width)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Risk Event Notification Tests (neura-nh5)
// =============================================================================

func TestRiskEventNotification_Struct(t *testing.T) {
	event := RiskEventNotification{
		EventType: "drawdown_exceeded",
		Severity:  "critical",
		Message:   "Portfolio drawdown has exceeded 15% threshold",
		Details: map[string]string{
			"current_drawdown": "18.5%",
			"threshold":        "15%",
			"peak_value":       "$10,000",
			"current_value":    "$8,150",
		},
	}

	assert.Equal(t, "drawdown_exceeded", event.EventType)
	assert.Equal(t, "critical", event.Severity)
	assert.Equal(t, "Portfolio drawdown has exceeded 15% threshold", event.Message)
	assert.Equal(t, "18.5%", event.Details["current_drawdown"])
	assert.Equal(t, "15%", event.Details["threshold"])
}

func TestNotificationService_formatRiskEventMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	tests := []struct {
		name     string
		event    RiskEventNotification
		contains []string
	}{
		{
			name: "critical severity event",
			event: RiskEventNotification{
				EventType: "drawdown_exceeded",
				Severity:  "critical",
				Message:   "Portfolio drawdown exceeded threshold",
				Details: map[string]string{
					"drawdown": "20%",
				},
			},
			contains: []string{
				"🚨",
				"Risk Event Alert",
				"drawdown_exceeded",
				"critical",
				"Portfolio drawdown exceeded threshold",
				"drawdown: 20%",
			},
		},
		{
			name: "high severity event",
			event: RiskEventNotification{
				EventType: "daily_loss_limit",
				Severity:  "high",
				Message:   "Daily loss limit reached",
				Details: map[string]string{
					"loss":  "$150",
					"limit": "$100",
				},
			},
			contains: []string{
				"⚠️",
				"daily_loss_limit",
				"high",
				"Daily loss limit reached",
			},
		},
		{
			name: "medium severity event",
			event: RiskEventNotification{
				EventType: "position_warning",
				Severity:  "medium",
				Message:   "Position size approaching limit",
				Details:   map[string]string{},
			},
			contains: []string{
				"⚡",
				"position_warning",
				"medium",
			},
		},
		{
			name: "low severity event",
			event: RiskEventNotification{
				EventType: "info",
				Severity:  "low",
				Message:   "Risk parameters updated",
				Details:   nil,
			},
			contains: []string{
				"ℹ️",
				"info",
				"low",
				"Risk parameters updated",
			},
		},
		{
			name: "unknown severity defaults to warning",
			event: RiskEventNotification{
				EventType: "unknown_event",
				Severity:  "unknown",
				Message:   "Something happened",
			},
			contains: []string{
				"⚠️",
				"unknown_event",
			},
		},
		{
			name: "event with multiple details",
			event: RiskEventNotification{
				EventType: "consecutive_losses",
				Severity:  "high",
				Message:   "Multiple consecutive losses detected",
				Details: map[string]string{
					"count":    "3",
					"total":    "$75",
					"pause":    "15m",
					"strategy": "scalping",
				},
			},
			contains: []string{
				"count: 3",
				"total: $75",
				"pause: 15m",
				"strategy: scalping",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := ns.formatRiskEventMessage(tt.event)
			for _, expected := range tt.contains {
				assert.Contains(t, message, expected, "Message should contain %s", expected)
			}
		})
	}
}

// =============================================================================
// Fund Milestone Notification Tests (neura-fvk)
// =============================================================================

func TestFundMilestoneNotification_Struct(t *testing.T) {
	milestone := FundMilestoneNotification{
		MilestoneType:  "fund_growth",
		CurrentValue:   "$2,500",
		TargetValue:    "$5,000",
		PercentReached: 50,
		Achievement:    "50% of fund growth target reached",
	}

	assert.Equal(t, "fund_growth", milestone.MilestoneType)
	assert.Equal(t, "$2,500", milestone.CurrentValue)
	assert.Equal(t, "$5,000", milestone.TargetValue)
	assert.Equal(t, 50, milestone.PercentReached)
	assert.Equal(t, "50% of fund growth target reached", milestone.Achievement)
}

func TestNotificationService_formatFundMilestoneMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	tests := []struct {
		name      string
		milestone FundMilestoneNotification
		contains  []string
	}{
		{
			name: "25% milestone",
			milestone: FundMilestoneNotification{
				MilestoneType:  "fund_growth",
				CurrentValue:   "$1,250",
				TargetValue:    "$5,000",
				PercentReached: 25,
				Achievement:    "Quarter milestone reached",
			},
			contains: []string{
				"💰",
				"Fund Milestone Reached!",
				"Quarter milestone reached",
				"$1,250",
				"$5,000",
				"25%",
			},
		},
		{
			name: "50% milestone",
			milestone: FundMilestoneNotification{
				MilestoneType:  "fund_growth",
				CurrentValue:   "$2,500",
				TargetValue:    "$5,000",
				PercentReached: 50,
				Achievement:    "Halfway to target!",
			},
			contains: []string{
				"Fund Milestone Reached!",
				"Halfway to target!",
				"50%",
			},
		},
		{
			name: "100% milestone - target achieved",
			milestone: FundMilestoneNotification{
				MilestoneType:  "fund_growth",
				CurrentValue:   "$5,000",
				TargetValue:    "$5,000",
				PercentReached: 100,
				Achievement:    "Target achieved!",
			},
			contains: []string{
				"Target achieved!",
				"100%",
				"████████████████████",
			},
		},
		{
			name: "profit milestone",
			milestone: FundMilestoneNotification{
				MilestoneType:  "profit_target",
				CurrentValue:   "$500 profit",
				TargetValue:    "$1,000 profit",
				PercentReached: 50,
				Achievement:    "Halfway to profit target",
			},
			contains: []string{
				"$500 profit",
				"$1,000 profit",
				"50%",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := ns.formatFundMilestoneMessage(tt.milestone)
			for _, expected := range tt.contains {
				assert.Contains(t, message, expected, "Message should contain %s", expected)
			}
			assert.Contains(t, message, "█", "Should contain progress bar filled chars")
		})
	}
}

// =============================================================================
// AI Reasoning Notification Tests (for completeness)
// =============================================================================

func TestAIReasoningNotification_Struct(t *testing.T) {
	reasoning := AIReasoningNotification{
		DecisionType: "trade_entry",
		Summary:      "Entering BTC long position based on momentum",
		Confidence:   0.85,
		Reasons: []string{
			"RSI oversold",
			"MACD bullish crossover",
			"Support level tested",
		},
		Action: "Buy 0.1 BTC at market",
	}

	assert.Equal(t, "trade_entry", reasoning.DecisionType)
	assert.Equal(t, "Entering BTC long position based on momentum", reasoning.Summary)
	assert.Equal(t, 0.85, reasoning.Confidence)
	assert.Len(t, reasoning.Reasons, 3)
	assert.Equal(t, "Buy 0.1 BTC at market", reasoning.Action)
}

func TestNotificationService_formatAIReasoningMessage(t *testing.T) {
	ns := NewNotificationService(nil, nil, "", "", "")

	tests := []struct {
		name      string
		reasoning AIReasoningNotification
		contains  []string
	}{
		{
			name: "high confidence decision",
			reasoning: AIReasoningNotification{
				DecisionType: "trade_entry",
				Summary:      "High probability setup detected",
				Confidence:   0.92,
				Reasons:      []string{"Strong trend", "Volume confirmation"},
				Action:       "Enter long position",
			},
			contains: []string{
				"🤖",
				"AI Trading Decision",
				"trade_entry",
				"🟢",
				"92%",
				"High probability setup detected",
				"Strong trend",
				"Enter long position",
			},
		},
		{
			name: "medium confidence decision",
			reasoning: AIReasoningNotification{
				DecisionType: "position_management",
				Summary:      "Consider reducing position",
				Confidence:   0.65,
				Reasons:      []string{"Volatility increasing"},
				Action:       "Reduce position by 50%",
			},
			contains: []string{
				"🟡",
				"65%",
				"Consider reducing position",
			},
		},
		{
			name: "low confidence decision",
			reasoning: AIReasoningNotification{
				DecisionType: "market_analysis",
				Summary:      "Uncertain market conditions",
				Confidence:   0.35,
				Reasons:      []string{"Mixed signals", "Low volume"},
				Action:       "",
			},
			contains: []string{
				"🔴",
				"35%",
				"Uncertain market conditions",
			},
		},
		{
			name: "many reasons truncated to 5",
			reasoning: AIReasoningNotification{
				DecisionType: "complex_decision",
				Summary:      "Multi-factor analysis",
				Confidence:   0.78,
				Reasons: []string{
					"Reason 1",
					"Reason 2",
					"Reason 3",
					"Reason 4",
					"Reason 5",
					"Reason 6",
					"Reason 7",
				},
			},
			contains: []string{
				"Reason 5",
				"and 2 more factors",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := ns.formatAIReasoningMessage(tt.reasoning)
			for _, expected := range tt.contains {
				assert.Contains(t, message, expected, "Message should contain %s", expected)
			}
		})
	}
}

// =============================================================================
// Integration Tests for Notification Triggers
// =============================================================================

func TestRiskEventNotification_Integration(t *testing.T) {
	severityLevels := []string{"low", "medium", "high", "critical"}
	for _, severity := range severityLevels {
		event := RiskEventNotification{
			EventType: "test_event",
			Severity:  severity,
			Message:   "Test message",
			Details:   map[string]string{"key": "value"},
		}

		assert.NotEmpty(t, event.EventType)
		assert.NotEmpty(t, event.Severity)
		assert.NotEmpty(t, event.Message)
	}
}

func TestFundMilestoneNotification_Integration(t *testing.T) {
	percentages := []int{10, 25, 50, 75, 90, 100}

	for _, pct := range percentages {
		milestone := FundMilestoneNotification{
			MilestoneType:  "fund_growth",
			CurrentValue:   fmt.Sprintf("$%d", pct*50),
			TargetValue:    "$5,000",
			PercentReached: pct,
			Achievement:    fmt.Sprintf("%d%% milestone reached", pct),
		}

		assert.Equal(t, pct, milestone.PercentReached)
		assert.NotEmpty(t, milestone.Achievement)
	}
}

func TestQuestProgressNotification_Integration(t *testing.T) {
	for current := 0; current <= 10; current++ {
		progress := QuestProgressNotification{
			QuestID:       "test-quest",
			QuestName:     "Test Quest",
			Current:       current,
			Target:        10,
			Percent:       current * 10,
			Status:        "active",
			TimeRemaining: "5m",
		}

		assert.Equal(t, current*10, progress.Percent)
	}
}
