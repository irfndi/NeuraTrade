package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test ArbitrageOpportunity struct
func TestArbitrageOpportunity_Struct(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Hour)
	buyPrice := decimal.NewFromFloat(100.50)
	sellPrice := decimal.NewFromFloat(102.00)
	profitPercentage := decimal.NewFromFloat(1.49)

	arb := ArbitrageOpportunity{
		ID:               "arb-123",
		TradingPairID:    1,
		BuyExchangeID:    1,
		SellExchangeID:   2,
		BuyPrice:         buyPrice,
		SellPrice:        sellPrice,
		ProfitPercentage: profitPercentage,
		DetectedAt:       now,
		ExpiresAt:        expiry,
	}

	assert.Equal(t, "arb-123", arb.ID)
	assert.Equal(t, 1, arb.TradingPairID)
	assert.Equal(t, 1, arb.BuyExchangeID)
	assert.Equal(t, 2, arb.SellExchangeID)
	assert.True(t, buyPrice.Equal(arb.BuyPrice))
	assert.True(t, sellPrice.Equal(arb.SellPrice))
	assert.True(t, profitPercentage.Equal(arb.ProfitPercentage))
	assert.Equal(t, now, arb.DetectedAt)
	assert.Equal(t, expiry, arb.ExpiresAt)
}

// Test ArbitrageOpportunityRequest struct
func TestArbitrageOpportunityRequest_Struct(t *testing.T) {
	minProfit := decimal.NewFromFloat(1.5)
	req := ArbitrageOpportunityRequest{
		MinProfit: minProfit,
		Symbol:    "BTC/USDT",
		Limit:     10,
	}

	assert.True(t, minProfit.Equal(req.MinProfit))
	assert.Equal(t, "BTC/USDT", req.Symbol)
	assert.Equal(t, 10, req.Limit)
}

// Test ArbitrageOpportunityResponse struct
func TestArbitrageOpportunityResponse_Struct(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Hour)
	buyPrice := decimal.NewFromFloat(100.50)
	sellPrice := decimal.NewFromFloat(102.00)
	profitPercentage := decimal.NewFromFloat(1.49)

	resp := ArbitrageOpportunityResponse{
		ID:               "arb-123",
		Symbol:           "BTC/USDT",
		BuyExchange:      "binance",
		SellExchange:     "coinbase",
		BuyPrice:         buyPrice,
		SellPrice:        sellPrice,
		ProfitPercentage: profitPercentage,
		DetectedAt:       now,
		ExpiresAt:        expiry,
	}

	assert.Equal(t, "arb-123", resp.ID)
	assert.Equal(t, "BTC/USDT", resp.Symbol)
	assert.Equal(t, "binance", resp.BuyExchange)
	assert.Equal(t, "coinbase", resp.SellExchange)
	assert.True(t, buyPrice.Equal(resp.BuyPrice))
	assert.True(t, sellPrice.Equal(resp.SellPrice))
	assert.True(t, profitPercentage.Equal(resp.ProfitPercentage))
	assert.Equal(t, now, resp.DetectedAt)
	assert.Equal(t, expiry, resp.ExpiresAt)
}

// Test ArbitrageOpportunitiesResponse struct
func TestArbitrageOpportunitiesResponse_Struct(t *testing.T) {
	now := time.Now()
	opportunities := []ArbitrageOpportunityResponse{
		{ID: "arb-1", Symbol: "BTC/USDT"},
		{ID: "arb-2", Symbol: "ETH/USDT"},
	}

	resp := ArbitrageOpportunitiesResponse{
		Opportunities: opportunities,
		Count:         2,
		Timestamp:     now,
	}

	assert.Equal(t, 2, len(resp.Opportunities))
	assert.Equal(t, "arb-1", resp.Opportunities[0].ID)
	assert.Equal(t, "arb-2", resp.Opportunities[1].ID)
	assert.Equal(t, 2, resp.Count)
	assert.Equal(t, now, resp.Timestamp)
}

// Test Exchange struct
func TestExchange_Struct(t *testing.T) {
	now := time.Now()
	exchange := Exchange{
		ID:        1,
		Name:      "Binance",
		APIURL:    "https://api.binance.com",
		IsActive:  true,
		LastPing:  &now,
		CreatedAt: now,
	}

	assert.Equal(t, 1, exchange.ID)
	assert.Equal(t, "Binance", exchange.Name)
	assert.Equal(t, "https://api.binance.com", exchange.APIURL)
	assert.True(t, exchange.IsActive)
	assert.Equal(t, &now, exchange.LastPing)
	assert.Equal(t, now, exchange.CreatedAt)
}

// Test CCXTExchange struct
func TestCCXTExchange_Struct(t *testing.T) {
	now := time.Now()
	ccxtExchange := CCXTExchange{
		ID:               1,
		ExchangeID:       1,
		CCXTID:           "binance",
		IsTestnet:        false,
		APIKeyRequired:   true,
		RateLimit:        1200,
		HasFutures:       true,
		WebsocketEnabled: true,
		LastHealthCheck:  &now,
		Status:           "active",
	}

	assert.Equal(t, 1, ccxtExchange.ID)
	assert.Equal(t, 1, ccxtExchange.ExchangeID)
	assert.Equal(t, "binance", ccxtExchange.CCXTID)
	assert.False(t, ccxtExchange.IsTestnet)
	assert.True(t, ccxtExchange.APIKeyRequired)
	assert.Equal(t, 1200, ccxtExchange.RateLimit)
	assert.True(t, ccxtExchange.HasFutures)
	assert.True(t, ccxtExchange.WebsocketEnabled)
	assert.Equal(t, &now, ccxtExchange.LastHealthCheck)
	assert.Equal(t, "active", ccxtExchange.Status)
}

// Test ExchangeInfo struct
func TestExchangeInfo_Struct(t *testing.T) {
	exchangeInfo := ExchangeInfo{
		ID:               1,
		Name:             "Binance",
		CCXTID:           "binance",
		IsActive:         true,
		HasFutures:       true,
		WebsocketEnabled: true,
		Status:           "active",
	}

	assert.Equal(t, 1, exchangeInfo.ID)
	assert.Equal(t, "Binance", exchangeInfo.Name)
	assert.Equal(t, "binance", exchangeInfo.CCXTID)
	assert.True(t, exchangeInfo.IsActive)
	assert.True(t, exchangeInfo.HasFutures)
	assert.True(t, exchangeInfo.WebsocketEnabled)
	assert.Equal(t, "active", exchangeInfo.Status)
}

// Test MarketData struct
func TestMarketData_Struct(t *testing.T) {
	now := time.Now()
	price := decimal.NewFromFloat(50000.50)
	volume := decimal.NewFromFloat(1000.25)

	marketData := MarketData{
		ID:            "md-123",
		ExchangeID:    1,
		TradingPairID: 1,
		LastPrice:     price,
		Volume24h:     volume,
		Timestamp:     now,
		CreatedAt:     now,
	}

	assert.Equal(t, "md-123", marketData.ID)
	assert.Equal(t, 1, marketData.ExchangeID)
	assert.Equal(t, 1, marketData.TradingPairID)
	assert.True(t, price.Equal(marketData.LastPrice))
	assert.True(t, volume.Equal(marketData.Volume24h))
	assert.Equal(t, now, marketData.Timestamp)
	assert.Equal(t, now, marketData.CreatedAt)
}

// Test MarketPrice struct
func TestMarketPrice_Struct(t *testing.T) {
	now := time.Now()
	price := decimal.NewFromFloat(50000.50)
	volume := decimal.NewFromFloat(1000.25)

	marketPrice := MarketPrice{
		ExchangeID:   1,
		ExchangeName: "Binance",
		Symbol:       "BTC/USDT",
		Price:        price,
		Volume:       volume,
		Timestamp:    now,
	}

	assert.Equal(t, 1, marketPrice.ExchangeID)
	assert.Equal(t, "Binance", marketPrice.ExchangeName)
	assert.Equal(t, "BTC/USDT", marketPrice.Symbol)
	assert.True(t, price.Equal(marketPrice.Price))
	assert.True(t, volume.Equal(marketPrice.Volume))
	assert.Equal(t, now, marketPrice.Timestamp)
}

// Test TickerData struct
func TestTickerData_Struct(t *testing.T) {
	now := time.Now()
	bid := decimal.NewFromFloat(49999.99)
	ask := decimal.NewFromFloat(50000.01)
	last := decimal.NewFromFloat(50000.00)
	high := decimal.NewFromFloat(51000.00)
	low := decimal.NewFromFloat(49000.00)
	volume := decimal.NewFromFloat(1000.25)

	ticker := TickerData{
		Symbol:    "BTC/USDT",
		Bid:       bid,
		Ask:       ask,
		Last:      last,
		High:      high,
		Low:       low,
		Volume:    volume,
		Timestamp: now,
	}

	assert.Equal(t, "BTC/USDT", ticker.Symbol)
	assert.True(t, bid.Equal(ticker.Bid))
	assert.True(t, ask.Equal(ticker.Ask))
	assert.True(t, last.Equal(ticker.Last))
	assert.True(t, high.Equal(ticker.High))
	assert.True(t, low.Equal(ticker.Low))
	assert.True(t, volume.Equal(ticker.Volume))
	assert.Equal(t, now, ticker.Timestamp)
}

// Test OrderBookData struct
func TestOrderBookData_Struct(t *testing.T) {
	now := time.Now()
	bids := [][]decimal.Decimal{
		{decimal.NewFromFloat(49999.99), decimal.NewFromFloat(1.5)},
		{decimal.NewFromFloat(49999.98), decimal.NewFromFloat(2.0)},
	}
	asks := [][]decimal.Decimal{
		{decimal.NewFromFloat(50000.01), decimal.NewFromFloat(1.2)},
		{decimal.NewFromFloat(50000.02), decimal.NewFromFloat(1.8)},
	}

	orderBook := OrderBookData{
		Symbol:    "BTC/USDT",
		Bids:      bids,
		Asks:      asks,
		Timestamp: now,
	}

	assert.Equal(t, "BTC/USDT", orderBook.Symbol)
	assert.Equal(t, 2, len(orderBook.Bids))
	assert.Equal(t, 2, len(orderBook.Asks))
	assert.True(t, decimal.NewFromFloat(49999.99).Equal(orderBook.Bids[0][0]))
	assert.True(t, decimal.NewFromFloat(50000.01).Equal(orderBook.Asks[0][0]))
	assert.Equal(t, now, orderBook.Timestamp)
}

// Test MarketDataRequest struct
func TestMarketDataRequest_Struct(t *testing.T) {
	req := MarketDataRequest{
		Symbols:  []string{"BTC/USDT", "ETH/USDT"},
		Exchange: "binance",
		Limit:    100,
	}

	assert.Equal(t, []string{"BTC/USDT", "ETH/USDT"}, req.Symbols)
	assert.Equal(t, "binance", req.Exchange)
	assert.Equal(t, 100, req.Limit)
}

// Test TradingPair struct and methods
func TestTradingPair_Struct(t *testing.T) {
	id := 1
	now := time.Now()

	tp := TradingPair{
		ID:            id,
		Symbol:        "BTC/USDT",
		BaseCurrency:  "BTC",
		QuoteCurrency: "USDT",
		IsActive:      true,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	assert.Equal(t, id, tp.ID)
	assert.Equal(t, "BTC/USDT", tp.Symbol)
	assert.Equal(t, "BTC", tp.BaseCurrency)
	assert.Equal(t, "USDT", tp.QuoteCurrency)
	assert.True(t, tp.IsActive)
	assert.Equal(t, now, tp.CreatedAt)
	assert.Equal(t, now, tp.UpdatedAt)
}

func TestTradingPair_TableName(t *testing.T) {
	tp := TradingPair{}
	assert.Equal(t, "trading_pairs", tp.TableName())
}

func TestTradingPair_String(t *testing.T) {
	tp := TradingPair{Symbol: "BTC/USDT"}
	assert.Equal(t, "BTC/USDT", tp.String())
}

func TestTradingPair_GetFullSymbol(t *testing.T) {
	tp := TradingPair{
		BaseCurrency:  "BTC",
		QuoteCurrency: "USDT",
	}
	assert.Equal(t, "BTC/USDT", tp.GetFullSymbol())
}

// Test User struct
func TestUser_Struct(t *testing.T) {
	now := time.Now()
	chatID := "123456789"

	user := User{
		ID:               "user-123",
		Email:            "test@example.com",
		PasswordHash:     "hashed_password",
		TelegramChatID:   &chatID,
		SubscriptionTier: "premium",
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	assert.Equal(t, "user-123", user.ID)
	assert.Equal(t, "test@example.com", user.Email)
	assert.Equal(t, "hashed_password", user.PasswordHash)
	assert.Equal(t, &chatID, user.TelegramChatID)
	assert.Equal(t, "premium", user.SubscriptionTier)
	assert.Equal(t, now, user.CreatedAt)
	assert.Equal(t, now, user.UpdatedAt)
}

// Test UserAlert struct
func TestUserAlert_Struct(t *testing.T) {
	now := time.Now()
	conditions := json.RawMessage(`{"price_threshold": 50000}`)

	alert := UserAlert{
		ID:         "alert-123",
		UserID:     "user-123",
		AlertType:  "price",
		Conditions: conditions,
		IsActive:   true,
		CreatedAt:  now,
	}

	assert.Equal(t, "alert-123", alert.ID)
	assert.Equal(t, "user-123", alert.UserID)
	assert.Equal(t, "price", alert.AlertType)
	assert.Equal(t, conditions, alert.Conditions)
	assert.True(t, alert.IsActive)
	assert.Equal(t, now, alert.CreatedAt)
}

// Test Portfolio struct
func TestPortfolio_Struct(t *testing.T) {
	now := time.Now()
	quantity := decimal.NewFromFloat(1.5)
	avgPrice := decimal.NewFromFloat(50000.00)

	portfolio := Portfolio{
		ID:        "portfolio-123",
		UserID:    "user-123",
		Symbol:    "BTC",
		Quantity:  quantity,
		AvgPrice:  avgPrice,
		UpdatedAt: now,
	}

	assert.Equal(t, "portfolio-123", portfolio.ID)
	assert.Equal(t, "user-123", portfolio.UserID)
	assert.Equal(t, "BTC", portfolio.Symbol)
	assert.True(t, quantity.Equal(portfolio.Quantity))
	assert.True(t, avgPrice.Equal(portfolio.AvgPrice))
	assert.Equal(t, now, portfolio.UpdatedAt)
}

// Test UserRequest struct
func TestUserRequest_Struct(t *testing.T) {
	req := UserRequest{
		Email:            "test@example.com",
		Password:         "password123",
		TelegramChatID:   "123456789",
		SubscriptionTier: "premium",
	}

	assert.Equal(t, "test@example.com", req.Email)
	assert.Equal(t, "password123", req.Password)
	assert.Equal(t, "123456789", req.TelegramChatID)
	assert.Equal(t, "premium", req.SubscriptionTier)
}

// Test UserResponse struct
func TestUserResponse_Struct(t *testing.T) {
	now := time.Now()

	resp := UserResponse{
		ID:               "user-123",
		Email:            "test@example.com",
		TelegramChatID:   "123456789",
		SubscriptionTier: "premium",
		CreatedAt:        now,
	}

	assert.Equal(t, "user-123", resp.ID)
	assert.Equal(t, "test@example.com", resp.Email)
	assert.Equal(t, "123456789", resp.TelegramChatID)
	assert.Equal(t, "premium", resp.SubscriptionTier)
	assert.Equal(t, now, resp.CreatedAt)
}

// Test AlertConditions struct
func TestAlertConditions_Struct(t *testing.T) {
	priceThreshold := decimal.NewFromFloat(50000.00)
	profitThreshold := decimal.NewFromFloat(5.0)
	volumeThreshold := decimal.NewFromFloat(1000.0)

	conditions := AlertConditions{
		PriceThreshold:  &priceThreshold,
		ProfitThreshold: &profitThreshold,
		VolumeThreshold: &volumeThreshold,
		Symbol:          "BTC/USDT",
		Exchange:        "binance",
	}

	assert.True(t, priceThreshold.Equal(*conditions.PriceThreshold))
	assert.True(t, profitThreshold.Equal(*conditions.ProfitThreshold))
	assert.True(t, volumeThreshold.Equal(*conditions.VolumeThreshold))
	assert.Equal(t, "BTC/USDT", conditions.Symbol)
	assert.Equal(t, "binance", conditions.Exchange)
}

// Test TechnicalIndicator struct
func TestTechnicalIndicator_Struct(t *testing.T) {
	now := time.Now()
	values := json.RawMessage(`{"rsi": 65.5, "signal": "neutral"}`)

	indicator := TechnicalIndicator{
		ID:            "indicator-123",
		TradingPairID: 1,
		IndicatorType: "RSI",
		Timeframe:     "1h",
		Values:        values,
		CalculatedAt:  now,
	}

	assert.Equal(t, "indicator-123", indicator.ID)
	assert.Equal(t, 1, indicator.TradingPairID)
	assert.Equal(t, "RSI", indicator.IndicatorType)
	assert.Equal(t, "1h", indicator.Timeframe)
	assert.Equal(t, values, indicator.Values)
	assert.Equal(t, now, indicator.CalculatedAt)
}

// Test IndicatorData struct
func TestIndicatorData_Struct(t *testing.T) {
	now := time.Now()
	indicators := map[string]interface{}{
		"rsi":  65.5,
		"macd": map[string]float64{"macd": 1.2, "signal": 1.1},
	}

	data := IndicatorData{
		Symbol:     "BTC/USDT",
		Timeframe:  "1h",
		Indicators: indicators,
		Timestamp:  now,
	}

	assert.Equal(t, "BTC/USDT", data.Symbol)
	assert.Equal(t, "1h", data.Timeframe)
	assert.Equal(t, indicators, data.Indicators)
	assert.Equal(t, now, data.Timestamp)
}

// Test RSIData struct
func TestRSIData_Struct(t *testing.T) {
	now := time.Now()
	value := decimal.NewFromFloat(65.5)

	rsi := RSIData{
		Value:     value,
		Signal:    "neutral",
		Timestamp: now,
	}

	assert.True(t, value.Equal(rsi.Value))
	assert.Equal(t, "neutral", rsi.Signal)
	assert.Equal(t, now, rsi.Timestamp)
}

// Test MACDData struct
func TestMACDData_Struct(t *testing.T) {
	now := time.Now()
	macd := decimal.NewFromFloat(1.2)
	signal := decimal.NewFromFloat(1.1)
	histogram := decimal.NewFromFloat(0.1)

	macdData := MACDData{
		MACD:      macd,
		Signal:    signal,
		Histogram: histogram,
		Trend:     "bullish",
		Timestamp: now,
	}

	assert.True(t, macd.Equal(macdData.MACD))
	assert.True(t, signal.Equal(macdData.Signal))
	assert.True(t, histogram.Equal(macdData.Histogram))
	assert.Equal(t, "bullish", macdData.Trend)
	assert.Equal(t, now, macdData.Timestamp)
}

// Test SMAData struct
func TestSMAData_Struct(t *testing.T) {
	now := time.Now()
	value := decimal.NewFromFloat(50000.00)

	sma := SMAData{
		Period:    20,
		Value:     value,
		Trend:     "up",
		Timestamp: now,
	}

	assert.Equal(t, 20, sma.Period)
	assert.True(t, value.Equal(sma.Value))
	assert.Equal(t, "up", sma.Trend)
	assert.Equal(t, now, sma.Timestamp)
}

// Test Signal struct
func TestSignal_Struct(t *testing.T) {
	now := time.Now()
	price := decimal.NewFromFloat(50000.00)
	confidence := decimal.NewFromFloat(85.5)

	signal := Signal{
		Type:       "buy",
		Strength:   "strong",
		Price:      price,
		Indicator:  "RSI",
		Confidence: confidence,
		Timestamp:  now,
	}

	assert.Equal(t, "buy", signal.Type)
	assert.Equal(t, "strong", signal.Strength)
	assert.True(t, price.Equal(signal.Price))
	assert.Equal(t, "RSI", signal.Indicator)
	assert.True(t, confidence.Equal(signal.Confidence))
	assert.Equal(t, now, signal.Timestamp)
}

// Test TechnicalAnalysisRequest struct
func TestTechnicalAnalysisRequest_Struct(t *testing.T) {
	req := TechnicalAnalysisRequest{
		Symbol:     "BTC/USDT",
		Timeframe:  "1h",
		Indicators: []string{"RSI", "MACD", "SMA"},
		Period:     20,
	}

	assert.Equal(t, "BTC/USDT", req.Symbol)
	assert.Equal(t, "1h", req.Timeframe)
	assert.Equal(t, []string{"RSI", "MACD", "SMA"}, req.Indicators)
	assert.Equal(t, 20, req.Period)
}

// Test TechnicalAnalysisResponse struct
func TestTechnicalAnalysisResponse_Struct(t *testing.T) {
	now := time.Now()
	data := IndicatorData{
		Symbol:    "BTC/USDT",
		Timeframe: "1h",
	}
	signals := []Signal{
		{Type: "buy", Strength: "strong"},
		{Type: "sell", Strength: "weak"},
	}

	resp := TechnicalAnalysisResponse{
		Data:      data,
		Signals:   signals,
		Timestamp: now,
	}

	assert.Equal(t, "BTC/USDT", resp.Data.Symbol)
	assert.Equal(t, "1h", resp.Data.Timeframe)
	assert.Equal(t, 2, len(resp.Signals))
	assert.Equal(t, "buy", resp.Signals[0].Type)
	assert.Equal(t, "sell", resp.Signals[1].Type)
	assert.Equal(t, now, resp.Timestamp)
}

// Test JSON marshaling/unmarshaling for complex types
func TestAlertConditions_JSON(t *testing.T) {
	priceThreshold := decimal.NewFromFloat(50000.00)
	original := AlertConditions{
		PriceThreshold: &priceThreshold,
		Symbol:         "BTC/USDT",
		Exchange:       "binance",
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal from JSON
	var unmarshaled AlertConditions
	err = json.Unmarshal(jsonData, &unmarshaled)
	require.NoError(t, err)

	// Verify the data
	assert.True(t, priceThreshold.Equal(*unmarshaled.PriceThreshold))
	assert.Equal(t, "BTC/USDT", unmarshaled.Symbol)
	assert.Equal(t, "binance", unmarshaled.Exchange)
}

// Test EnhancedArbitrageOpportunity struct
func TestEnhancedArbitrageOpportunity_Struct(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Hour)
	buyPriceRange := PriceRange{Min: decimal.NewFromFloat(50000.00), Max: decimal.NewFromFloat(50100.00)}
	sellPriceRange := PriceRange{Min: decimal.NewFromFloat(50200.00), Max: decimal.NewFromFloat(50300.00)}
	profitRange := ProfitRange{
		MinPercentage: decimal.NewFromFloat(0.5),
		MaxPercentage: decimal.NewFromFloat(1.0),
		MinAmount:     decimal.NewFromFloat(100.0),
		MaxAmount:     decimal.NewFromFloat(200.0),
		BaseAmount:    decimal.NewFromFloat(20000.0),
	}

	buyExchanges := []ExchangePrice{
		{ExchangeID: 1, ExchangeName: "Binance", Price: decimal.NewFromFloat(50050.00), Volume: decimal.NewFromFloat(10.5)},
	}
	sellExchanges := []ExchangePrice{
		{ExchangeID: 2, ExchangeName: "Coinbase", Price: decimal.NewFromFloat(50250.00), Volume: decimal.NewFromFloat(8.2)},
	}

	volumeWeighted := VolumeWeightedPrices{
		BuyVWAP:  decimal.NewFromFloat(50050.00),
		SellVWAP: decimal.NewFromFloat(50250.00),
	}

	enhancedArb := EnhancedArbitrageOpportunity{
		ID:                  "enhanced-arb-123",
		Symbol:              "BTC/USDT",
		BuyPriceRange:       buyPriceRange,
		SellPriceRange:      sellPriceRange,
		ProfitRange:         profitRange,
		BuyExchanges:        buyExchanges,
		SellExchanges:       sellExchanges,
		MinVolume:           decimal.NewFromFloat(1.0),
		TotalVolume:         decimal.NewFromFloat(18.7),
		ValidityDuration:    time.Minute * 5,
		DetectedAt:          now,
		ExpiresAt:           expiry,
		QualityScore:        decimal.NewFromFloat(85.5),
		VolumeWeightedPrice: volumeWeighted,
	}

	assert.Equal(t, "enhanced-arb-123", enhancedArb.ID)
	assert.Equal(t, "BTC/USDT", enhancedArb.Symbol)
	assert.True(t, buyPriceRange.Min.Equal(enhancedArb.BuyPriceRange.Min))
	assert.True(t, buyPriceRange.Max.Equal(enhancedArb.BuyPriceRange.Max))
	assert.True(t, sellPriceRange.Min.Equal(enhancedArb.SellPriceRange.Min))
	assert.True(t, sellPriceRange.Max.Equal(enhancedArb.SellPriceRange.Max))
	assert.True(t, profitRange.MinPercentage.Equal(enhancedArb.ProfitRange.MinPercentage))
	assert.True(t, profitRange.MaxPercentage.Equal(enhancedArb.ProfitRange.MaxPercentage))
	assert.True(t, profitRange.MinAmount.Equal(enhancedArb.ProfitRange.MinAmount))
	assert.True(t, profitRange.MaxAmount.Equal(enhancedArb.ProfitRange.MaxAmount))
	assert.True(t, profitRange.BaseAmount.Equal(enhancedArb.ProfitRange.BaseAmount))
	assert.Equal(t, 1, len(enhancedArb.BuyExchanges))
	assert.Equal(t, 1, len(enhancedArb.SellExchanges))
	assert.True(t, decimal.NewFromFloat(1.0).Equal(enhancedArb.MinVolume))
	assert.True(t, decimal.NewFromFloat(18.7).Equal(enhancedArb.TotalVolume))
	assert.Equal(t, time.Minute*5, enhancedArb.ValidityDuration)
	assert.Equal(t, now, enhancedArb.DetectedAt)
	assert.Equal(t, expiry, enhancedArb.ExpiresAt)
	assert.True(t, decimal.NewFromFloat(85.5).Equal(enhancedArb.QualityScore))
	assert.True(t, volumeWeighted.BuyVWAP.Equal(enhancedArb.VolumeWeightedPrice.BuyVWAP))
	assert.True(t, volumeWeighted.SellVWAP.Equal(enhancedArb.VolumeWeightedPrice.SellVWAP))
}

// Test ExchangePrice struct
func TestExchangePrice_Struct(t *testing.T) {
	exchangePrice := ExchangePrice{
		ExchangeID:   1,
		ExchangeName: "Binance",
		Price:        decimal.NewFromFloat(50000.00),
		Volume:       decimal.NewFromFloat(10.5),
		Spread:       decimal.NewFromFloat(50.00),
		Reliability:  decimal.NewFromFloat(0.95),
	}

	assert.Equal(t, 1, exchangePrice.ExchangeID)
	assert.Equal(t, "Binance", exchangePrice.ExchangeName)
	assert.True(t, decimal.NewFromFloat(50000.00).Equal(exchangePrice.Price))
	assert.True(t, decimal.NewFromFloat(10.5).Equal(exchangePrice.Volume))
	assert.True(t, decimal.NewFromFloat(50.00).Equal(exchangePrice.Spread))
	assert.True(t, decimal.NewFromFloat(0.95).Equal(exchangePrice.Reliability))
}

// Test VolumeWeightedPrices struct
func TestVolumeWeightedPrices_Struct(t *testing.T) {
	vwp := VolumeWeightedPrices{
		BuyVWAP:  decimal.NewFromFloat(50000.00),
		SellVWAP: decimal.NewFromFloat(50200.00),
	}

	assert.True(t, decimal.NewFromFloat(50000.00).Equal(vwp.BuyVWAP))
	assert.True(t, decimal.NewFromFloat(50200.00).Equal(vwp.SellVWAP))
}

// Test ArbitrageAggregationInput struct
func TestArbitrageAggregationInput_Struct(t *testing.T) {
	opportunities := []ArbitrageOpportunity{
		{ID: "arb-1", TradingPairID: 1, BuyExchangeID: 1, SellExchangeID: 2},
		{ID: "arb-2", TradingPairID: 2, BuyExchangeID: 1, SellExchangeID: 2},
	}

	input := ArbitrageAggregationInput{
		Opportunities: opportunities,
		MinVolume:     decimal.NewFromFloat(1.0),
		MaxSpread:     decimal.NewFromFloat(0.1),
		BaseAmount:    decimal.NewFromFloat(1000.0),
	}

	assert.Equal(t, 2, len(input.Opportunities))
	assert.Equal(t, "arb-1", input.Opportunities[0].ID)
	assert.Equal(t, "arb-2", input.Opportunities[1].ID)
	assert.True(t, decimal.NewFromFloat(1.0).Equal(input.MinVolume))
	assert.True(t, decimal.NewFromFloat(0.1).Equal(input.MaxSpread))
	assert.True(t, decimal.NewFromFloat(1000.0).Equal(input.BaseAmount))
}

// Test ArbitrageQualityMetrics struct
func TestArbitrageQualityMetrics_Struct(t *testing.T) {
	metrics := ArbitrageQualityMetrics{
		VolumeScore:     decimal.NewFromFloat(85.0),
		SpreadScore:     decimal.NewFromFloat(90.0),
		ExchangeScore:   decimal.NewFromFloat(95.0),
		LiquidityScore:  decimal.NewFromFloat(88.0),
		OverallScore:    decimal.NewFromFloat(89.5),
		IsAcceptable:    true,
		RejectionReason: "",
	}

	assert.True(t, decimal.NewFromFloat(85.0).Equal(metrics.VolumeScore))
	assert.True(t, decimal.NewFromFloat(90.0).Equal(metrics.SpreadScore))
	assert.True(t, decimal.NewFromFloat(95.0).Equal(metrics.ExchangeScore))
	assert.True(t, decimal.NewFromFloat(88.0).Equal(metrics.LiquidityScore))
	assert.True(t, decimal.NewFromFloat(89.5).Equal(metrics.OverallScore))
	assert.True(t, metrics.IsAcceptable)
	assert.Equal(t, "", metrics.RejectionReason)
}

// Test PriceRange struct
func TestPriceRange_Struct(t *testing.T) {
	priceRange := PriceRange{
		Min: decimal.NewFromFloat(50000.00),
		Max: decimal.NewFromFloat(50100.00),
	}

	assert.True(t, decimal.NewFromFloat(50000.00).Equal(priceRange.Min))
	assert.True(t, decimal.NewFromFloat(50100.00).Equal(priceRange.Max))
}

// Test ProfitRange struct
func TestProfitRange_Struct(t *testing.T) {
	profitRange := ProfitRange{
		MinPercentage: decimal.NewFromFloat(0.5),
		MaxPercentage: decimal.NewFromFloat(1.0),
		MinAmount:     decimal.NewFromFloat(100.0),
		MaxAmount:     decimal.NewFromFloat(200.0),
		BaseAmount:    decimal.NewFromFloat(20000.0),
	}

	assert.True(t, decimal.NewFromFloat(0.5).Equal(profitRange.MinPercentage))
	assert.True(t, decimal.NewFromFloat(1.0).Equal(profitRange.MaxPercentage))
	assert.True(t, decimal.NewFromFloat(100.0).Equal(profitRange.MinAmount))
	assert.True(t, decimal.NewFromFloat(200.0).Equal(profitRange.MaxAmount))
	assert.True(t, decimal.NewFromFloat(20000.0).Equal(profitRange.BaseAmount))
}

// Test FuturesArbitrageOpportunity struct
func TestFuturesArbitrageOpportunity_Struct(t *testing.T) {
	now := time.Now()
	expiry := now.Add(8 * time.Hour)
	nextFunding := now.Add(4 * time.Hour)
	history := []FundingRateHistoryPoint{
		{Timestamp: now.Add(-8 * time.Hour), FundingRate: decimal.NewFromFloat(0.0001), MarkPrice: decimal.NewFromFloat(50000.00)},
	}

	opportunity := FuturesArbitrageOpportunity{
		ID:                        "futures-arb-123",
		Symbol:                    "BTC/USDT",
		BaseCurrency:              "BTC",
		QuoteCurrency:             "USDT",
		LongExchange:              "Binance",
		ShortExchange:             "Bybit",
		LongExchangeID:            1,
		ShortExchangeID:           2,
		LongFundingRate:           decimal.NewFromFloat(0.0001),
		ShortFundingRate:          decimal.NewFromFloat(-0.0002),
		NetFundingRate:            decimal.NewFromFloat(0.0003),
		FundingInterval:           8,
		LongMarkPrice:             decimal.NewFromFloat(50000.00),
		ShortMarkPrice:            decimal.NewFromFloat(50100.00),
		PriceDifference:           decimal.NewFromFloat(100.00),
		PriceDifferencePercentage: decimal.NewFromFloat(0.2),
		HourlyRate:                decimal.NewFromFloat(0.0000375),
		DailyRate:                 decimal.NewFromFloat(0.0009),
		APY:                       decimal.NewFromFloat(0.3285),
		EstimatedProfit8h:         decimal.NewFromFloat(0.3),
		EstimatedProfitDaily:      decimal.NewFromFloat(0.9),
		EstimatedProfitWeekly:     decimal.NewFromFloat(6.3),
		EstimatedProfitMonthly:    decimal.NewFromFloat(27.0),
		RiskScore:                 decimal.NewFromFloat(3.5),
		VolatilityScore:           decimal.NewFromFloat(2.1),
		LiquidityScore:            decimal.NewFromFloat(8.5),
		RecommendedPositionSize:   decimal.NewFromFloat(1.0),
		MaxLeverage:               decimal.NewFromFloat(10.0),
		RecommendedLeverage:       decimal.NewFromFloat(5.0),
		StopLossPercentage:        decimal.NewFromFloat(2.0),
		MinPositionSize:           decimal.NewFromFloat(0.1),
		MaxPositionSize:           decimal.NewFromFloat(10.0),
		OptimalPositionSize:       decimal.NewFromFloat(2.5),
		DetectedAt:                now,
		ExpiresAt:                 expiry,
		NextFundingTime:           nextFunding,
		TimeToNextFunding:         240,
		IsActive:                  true,
		MarketTrend:               "bullish",
		Volume24h:                 decimal.NewFromFloat(1000000.0),
		OpenInterest:              decimal.NewFromFloat(500000.0),
		FundingRateHistory:        history,
	}

	assert.Equal(t, "futures-arb-123", opportunity.ID)
	assert.Equal(t, "BTC/USDT", opportunity.Symbol)
	assert.Equal(t, "Binance", opportunity.LongExchange)
	assert.Equal(t, "Bybit", opportunity.ShortExchange)
	assert.True(t, decimal.NewFromFloat(0.0003).Equal(opportunity.NetFundingRate))
	assert.True(t, decimal.NewFromFloat(0.3285).Equal(opportunity.APY))
	assert.True(t, decimal.NewFromFloat(1.0).Equal(opportunity.RecommendedPositionSize))
	assert.Equal(t, 240, opportunity.TimeToNextFunding)
	assert.True(t, opportunity.IsActive)
	assert.Equal(t, "bullish", opportunity.MarketTrend)
	assert.Equal(t, 1, len(opportunity.FundingRateHistory))
}

// Test FundingRateHistoryPoint struct
func TestFundingRateHistoryPoint_Struct(t *testing.T) {
	now := time.Now()
	point := FundingRateHistoryPoint{
		Timestamp:   now,
		FundingRate: decimal.NewFromFloat(0.0001),
		MarkPrice:   decimal.NewFromFloat(50000.00),
	}

	assert.Equal(t, now, point.Timestamp)
	assert.True(t, decimal.NewFromFloat(0.0001).Equal(point.FundingRate))
	assert.True(t, decimal.NewFromFloat(50000.00).Equal(point.MarkPrice))
}

// Test FuturesArbitrageCalculationInput struct
func TestFuturesArbitrageCalculationInput_Struct(t *testing.T) {
	input := FuturesArbitrageCalculationInput{
		Symbol:             "BTC/USDT",
		LongExchange:       "Binance",
		ShortExchange:      "Bybit",
		LongFundingRate:    decimal.NewFromFloat(0.0001),
		ShortFundingRate:   decimal.NewFromFloat(-0.0002),
		LongMarkPrice:      decimal.NewFromFloat(50000.00),
		ShortMarkPrice:     decimal.NewFromFloat(50100.00),
		BaseAmount:         decimal.NewFromFloat(1000.0),
		UserRiskTolerance:  "medium",
		MaxLeverageAllowed: decimal.NewFromFloat(10.0),
		AvailableCapital:   decimal.NewFromFloat(10000.0),
		FundingInterval:    8,
	}

	assert.Equal(t, "BTC/USDT", input.Symbol)
	assert.Equal(t, "Binance", input.LongExchange)
	assert.Equal(t, "Bybit", input.ShortExchange)
	assert.True(t, decimal.NewFromFloat(0.0003).Equal(input.LongFundingRate.Sub(input.ShortFundingRate)))
	assert.Equal(t, "medium", input.UserRiskTolerance)
	assert.Equal(t, 8, input.FundingInterval)
}

// Test FuturesArbitrageRiskMetrics struct
func TestFuturesArbitrageRiskMetrics_Struct(t *testing.T) {
	metrics := FuturesArbitrageRiskMetrics{
		PriceCorrelation:      decimal.NewFromFloat(0.95),
		PriceVolatility:       decimal.NewFromFloat(0.15),
		MaxDrawdown:           decimal.NewFromFloat(0.08),
		FundingRateVolatility: decimal.NewFromFloat(0.05),
		FundingRateStability:  decimal.NewFromFloat(0.92),
		BidAskSpread:          decimal.NewFromFloat(0.001),
		MarketDepth:           decimal.NewFromFloat(0.95),
		SlippageRisk:          decimal.NewFromFloat(0.02),
		ExchangeReliability:   decimal.NewFromFloat(0.98),
		CounterpartyRisk:      decimal.NewFromFloat(0.01),
		OverallRiskScore:      decimal.NewFromFloat(25.5),
		RiskCategory:          "medium",
		Recommendation:        "Proceed with caution",
	}

	assert.True(t, decimal.NewFromFloat(0.95).Equal(metrics.PriceCorrelation))
	assert.True(t, decimal.NewFromFloat(0.15).Equal(metrics.PriceVolatility))
	assert.Equal(t, "medium", metrics.RiskCategory)
	assert.Equal(t, "Proceed with caution", metrics.Recommendation)
}

// Test FuturesPositionSizing struct
func TestFuturesPositionSizing_Struct(t *testing.T) {
	sizing := FuturesPositionSizing{
		KellyPercentage:   decimal.NewFromFloat(0.25),
		KellyPositionSize: decimal.NewFromFloat(2500.0),
		ConservativeSize:  decimal.NewFromFloat(1000.0),
		ModerateSize:      decimal.NewFromFloat(2000.0),
		AggressiveSize:    decimal.NewFromFloat(3000.0),
		MinLeverage:       decimal.NewFromFloat(1.0),
		OptimalLeverage:   decimal.NewFromFloat(5.0),
		MaxSafeLeverage:   decimal.NewFromFloat(8.0),
		StopLossPrice:     decimal.NewFromFloat(49000.00),
		TakeProfitPrice:   decimal.NewFromFloat(51000.00),
		MaxLossPercentage: decimal.NewFromFloat(2.0),
	}

	assert.True(t, decimal.NewFromFloat(0.25).Equal(sizing.KellyPercentage))
	assert.True(t, decimal.NewFromFloat(2500.0).Equal(sizing.KellyPositionSize))
	assert.True(t, decimal.NewFromFloat(1000.0).Equal(sizing.ConservativeSize))
	assert.True(t, decimal.NewFromFloat(5.0).Equal(sizing.OptimalLeverage))
}

// Test FuturesArbitrageStrategy struct
func TestFuturesArbitrageStrategy_Struct(t *testing.T) {
	now := time.Now()
	opportunity := FuturesArbitrageOpportunity{
		ID:     "futures-arb-123",
		Symbol: "BTC/USDT",
	}
	riskMetrics := FuturesArbitrageRiskMetrics{
		OverallRiskScore: decimal.NewFromFloat(25.5),
		RiskCategory:     "medium",
	}
	positionSizing := FuturesPositionSizing{
		KellyPositionSize: decimal.NewFromFloat(2500.0),
	}
	longPosition := PositionDetails{
		Exchange:       "Binance",
		Symbol:         "BTC/USDT",
		Side:           "long",
		Size:           decimal.NewFromFloat(1.0),
		Leverage:       decimal.NewFromFloat(5.0),
		EntryPrice:     decimal.NewFromFloat(50000.00),
		StopLoss:       decimal.NewFromFloat(49000.00),
		TakeProfit:     decimal.NewFromFloat(51000.00),
		MarginRequired: decimal.NewFromFloat(10000.00),
		EstimatedFees:  decimal.NewFromFloat(10.0),
	}
	shortPosition := PositionDetails{
		Exchange:       "Bybit",
		Symbol:         "BTC/USDT",
		Side:           "short",
		Size:           decimal.NewFromFloat(1.0),
		Leverage:       decimal.NewFromFloat(5.0),
		EntryPrice:     decimal.NewFromFloat(50100.00),
		StopLoss:       decimal.NewFromFloat(51100.00),
		TakeProfit:     decimal.NewFromFloat(49100.00),
		MarginRequired: decimal.NewFromFloat(10020.00),
		EstimatedFees:  decimal.NewFromFloat(10.0),
	}

	strategy := FuturesArbitrageStrategy{
		ID:                     "strategy-123",
		Name:                   "BTC/USDT Funding Rate Arbitrage",
		Description:            "Exploit funding rate differential between Binance and Bybit",
		Opportunity:            opportunity,
		RiskMetrics:            riskMetrics,
		PositionSizing:         positionSizing,
		LongPosition:           longPosition,
		ShortPosition:          shortPosition,
		ExecutionOrder:         []string{"Open long position", "Open short position", "Set stop losses"},
		EstimatedExecutionTime: 30,
		ExpectedReturn:         decimal.NewFromFloat(0.3),
		SharpeRatio:            decimal.NewFromFloat(2.5),
		MaxDrawdownExpected:    decimal.NewFromFloat(0.08),
		CreatedAt:              now,
		UpdatedAt:              now,
		IsActive:               true,
	}

	assert.Equal(t, "strategy-123", strategy.ID)
	assert.Equal(t, "BTC/USDT Funding Rate Arbitrage", strategy.Name)
	assert.Equal(t, 3, len(strategy.ExecutionOrder))
	assert.Equal(t, 30, strategy.EstimatedExecutionTime)
	assert.True(t, decimal.NewFromFloat(2.5).Equal(strategy.SharpeRatio))
	assert.True(t, strategy.IsActive)
}

// Test PositionDetails struct
func TestPositionDetails_Struct(t *testing.T) {
	position := PositionDetails{
		Exchange:       "Binance",
		Symbol:         "BTC/USDT",
		Side:           "long",
		Size:           decimal.NewFromFloat(1.0),
		Leverage:       decimal.NewFromFloat(5.0),
		EntryPrice:     decimal.NewFromFloat(50000.00),
		StopLoss:       decimal.NewFromFloat(49000.00),
		TakeProfit:     decimal.NewFromFloat(51000.00),
		MarginRequired: decimal.NewFromFloat(10000.00),
		EstimatedFees:  decimal.NewFromFloat(10.0),
	}

	assert.Equal(t, "Binance", position.Exchange)
	assert.Equal(t, "BTC/USDT", position.Symbol)
	assert.Equal(t, "long", position.Side)
	assert.True(t, decimal.NewFromFloat(1.0).Equal(position.Size))
	assert.True(t, decimal.NewFromFloat(5.0).Equal(position.Leverage))
}

// Test FuturesArbitrageRequest struct
func TestFuturesArbitrageRequest_Struct(t *testing.T) {
	request := FuturesArbitrageRequest{
		Symbols:               []string{"BTC/USDT", "ETH/USDT"},
		Exchanges:             []string{"Binance", "Bybit", "OKX"},
		MinAPY:                decimal.NewFromFloat(0.1),
		MaxRiskScore:          decimal.NewFromFloat(50.0),
		RiskTolerance:         "medium",
		AvailableCapital:      decimal.NewFromFloat(10000.0),
		MaxLeverage:           decimal.NewFromFloat(10.0),
		TimeHorizon:           "medium",
		IncludeRiskMetrics:    true,
		IncludePositionSizing: true,
		Limit:                 50,
		Page:                  1,
	}

	assert.Equal(t, 2, len(request.Symbols))
	assert.Equal(t, 3, len(request.Exchanges))
	assert.Contains(t, request.Symbols, "BTC/USDT")
	assert.Contains(t, request.Exchanges, "Binance")
	assert.True(t, decimal.NewFromFloat(0.1).Equal(request.MinAPY))
	assert.Equal(t, "medium", request.RiskTolerance)
	assert.True(t, request.IncludeRiskMetrics)
	assert.Equal(t, 50, request.Limit)
}

// Test FuturesArbitrageResponse struct
func TestFuturesArbitrageResponse_Struct(t *testing.T) {
	now := time.Now()
	opportunities := []FuturesArbitrageOpportunity{
		{ID: "arb-1", Symbol: "BTC/USDT"},
		{ID: "arb-2", Symbol: "ETH/USDT"},
	}
	strategies := []FuturesArbitrageStrategy{
		{ID: "strategy-1", Name: "BTC Strategy"},
		{ID: "strategy-2", Name: "ETH Strategy"},
	}
	marketSummary := FuturesMarketSummary{
		TotalOpportunities:  10,
		AverageAPY:          decimal.NewFromFloat(0.15),
		HighestAPY:          decimal.NewFromFloat(0.35),
		AverageRiskScore:    decimal.NewFromFloat(30.0),
		MarketVolatility:    decimal.NewFromFloat(0.12),
		FundingRateTrend:    "stable",
		RecommendedStrategy: "Conservative approach",
		MarketCondition:     "favorable",
	}

	response := FuturesArbitrageResponse{
		Opportunities: opportunities,
		Strategies:    strategies,
		Count:         2,
		TotalCount:    10,
		Page:          1,
		Limit:         50,
		Timestamp:     now,
		MarketSummary: marketSummary,
	}

	assert.Equal(t, 2, len(response.Opportunities))
	assert.Equal(t, 2, len(response.Strategies))
	assert.Equal(t, 2, response.Count)
	assert.Equal(t, 10, response.TotalCount)
	assert.Equal(t, 1, response.Page)
	assert.Equal(t, now, response.Timestamp)
	assert.True(t, decimal.NewFromFloat(0.15).Equal(response.MarketSummary.AverageAPY))
	assert.Equal(t, "stable", response.MarketSummary.FundingRateTrend)
}

// Test FuturesMarketSummary struct
func TestFuturesMarketSummary_Struct(t *testing.T) {
	summary := FuturesMarketSummary{
		TotalOpportunities:  15,
		AverageAPY:          decimal.NewFromFloat(0.18),
		HighestAPY:          decimal.NewFromFloat(0.42),
		AverageRiskScore:    decimal.NewFromFloat(28.5),
		MarketVolatility:    decimal.NewFromFloat(0.14),
		FundingRateTrend:    "increasing",
		RecommendedStrategy: "Aggressive funding arbitrage",
		MarketCondition:     "very favorable",
	}

	assert.Equal(t, 15, summary.TotalOpportunities)
	assert.True(t, decimal.NewFromFloat(0.18).Equal(summary.AverageAPY))
	assert.True(t, decimal.NewFromFloat(0.42).Equal(summary.HighestAPY))
	assert.Equal(t, "increasing", summary.FundingRateTrend)
	assert.Equal(t, "very favorable", summary.MarketCondition)
}

// Test ArbitrageOpportunityResponse getter methods
func TestArbitrageOpportunityResponse_Getters(t *testing.T) {
	now := time.Now()
	expiry := now.Add(time.Hour)

	response := ArbitrageOpportunityResponse{
		ID:               "arb-123",
		Symbol:           "BTC/USDT",
		BuyExchange:      "Binance",
		SellExchange:     "Coinbase",
		BuyPrice:         decimal.NewFromFloat(50000.00),
		SellPrice:        decimal.NewFromFloat(50200.00),
		ProfitPercentage: decimal.NewFromFloat(0.4),
		DetectedAt:       now,
		ExpiresAt:        expiry,
	}

	assert.Equal(t, "BTC/USDT", response.GetSymbol())
	assert.Equal(t, "Binance", response.GetBuyExchange())
	assert.Equal(t, "Coinbase", response.GetSellExchange())
	assert.True(t, decimal.NewFromFloat(50000.00).Equal(response.GetBuyPrice()))
	assert.True(t, decimal.NewFromFloat(50200.00).Equal(response.GetSellPrice()))
	assert.True(t, decimal.NewFromFloat(0.4).Equal(response.GetProfitPercentage()))
	assert.Equal(t, now, response.GetDetectedAt())
	assert.Equal(t, expiry, response.GetExpiresAt())
}

// Test MarketPrice getter methods
func TestMarketPrice_Getters(t *testing.T) {
	now := time.Now()
	marketPrice := MarketPrice{
		ExchangeID:   1,
		ExchangeName: "Binance",
		Symbol:       "BTC/USDT",
		Price:        decimal.NewFromFloat(50000.00),
		Volume:       decimal.NewFromFloat(10.5),
		Bid:          decimal.NewFromFloat(49999.50),
		Ask:          decimal.NewFromFloat(50000.50),
		Timestamp:    now,
	}

	assert.Equal(t, 50000.00, marketPrice.GetPrice())
	assert.Equal(t, 10.5, marketPrice.GetVolume())
	assert.Equal(t, now, marketPrice.GetTimestamp())
	assert.Equal(t, "Binance", marketPrice.GetExchangeName())
	assert.Equal(t, "BTC/USDT", marketPrice.GetSymbol())
	assert.Equal(t, 49999.50, marketPrice.GetBid())
	assert.Equal(t, 50000.50, marketPrice.GetAsk())
}
