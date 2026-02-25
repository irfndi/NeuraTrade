package services

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/irfndi/neuratrade/internal/database"
	"github.com/shopspring/decimal"
)

// ArbitrageOpportunityFinder represents a detected arbitrage opportunity for the finder service
type ArbitrageOpportunityFinder struct {
	Symbol          string    `json:"symbol"`
	BuyExchange     string    `json:"buy_exchange"`
	SellExchange    string    `json:"sell_exchange"`
	BuyPrice        float64   `json:"buy_price"`
	SellPrice       float64   `json:"sell_price"`
	ProfitPercent   float64   `json:"profit_percent"`
	ProfitAmount    float64   `json:"profit_amount"`
	Volume          float64   `json:"volume"`
	Timestamp       time.Time `json:"timestamp"`
	OpportunityType string    `json:"opportunity_type"`
}

// ArbitrageFinderService provides arbitrage detection and analysis functionality
type ArbitrageFinderService struct {
	repo database.ArbitrageRepository
}

// NewArbitrageFinderService creates a new arbitrage finder service
func NewArbitrageFinderService(repo database.ArbitrageRepository) *ArbitrageFinderService {
	return &ArbitrageFinderService{repo: repo}
}

// FindAllOpportunities finds arbitrage opportunities using multiple strategies
func (s *ArbitrageFinderService) FindAllOpportunities(ctx context.Context, minProfit float64, limit int, symbolFilter string) ([]ArbitrageOpportunityFinder, error) {
	var opportunities []ArbitrageOpportunityFinder

	// Strategy 1: Cross-exchange price differences
	crossExchangeOpps, err := s.FindCrossExchangeOpportunities(ctx, minProfit, symbolFilter)
	if err != nil {
		return nil, err
	}
	opportunities = append(opportunities, crossExchangeOpps...)

	// Strategy 2: Technical analysis based opportunities
	technicalOpps, err := s.FindTechnicalAnalysisOpportunities(ctx, minProfit, symbolFilter)
	if err != nil {
		return nil, err
	}
	opportunities = append(opportunities, technicalOpps...)

	// Strategy 3: Volatility and momentum based opportunities
	volatilityOpps, err := s.FindVolatilityOpportunities(ctx, minProfit, symbolFilter)
	if err != nil {
		return nil, err
	}
	opportunities = append(opportunities, volatilityOpps...)

	// Strategy 4: Bid-Ask spread analysis
	spreadOpps, err := s.FindSpreadOpportunities(ctx, minProfit, symbolFilter)
	if err != nil {
		return nil, err
	}
	opportunities = append(opportunities, spreadOpps...)

	// Sort by profit percentage (descending)
	sort.Slice(opportunities, func(i, j int) bool {
		return opportunities[i].ProfitPercent > opportunities[j].ProfitPercent
	})

	// Limit results
	if len(opportunities) > limit {
		opportunities = opportunities[:limit]
	}

	return opportunities, nil
}

// FindCrossExchangeOpportunities finds arbitrage opportunities across different exchanges
func (s *ArbitrageFinderService) FindCrossExchangeOpportunities(ctx context.Context, minProfit float64, symbolFilter string) ([]ArbitrageOpportunityFinder, error) {
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	data, err := s.repo.GetRecentMarketData(ctx, fiveMinutesAgo, symbolFilter)
	if err != nil {
		return nil, err
	}

	// Group data by symbol and exchange, keeping only most recent per exchange
	marketData := make(map[string]map[string]database.MarketDataPoint)
	for _, dp := range data {
		if marketData[dp.Symbol] == nil {
			marketData[dp.Symbol] = make(map[string]database.MarketDataPoint)
		}
		if existing, exists := marketData[dp.Symbol][dp.Exchange]; !exists || dp.Timestamp.After(existing.Timestamp) {
			marketData[dp.Symbol][dp.Exchange] = dp
		}
	}

	var opportunities []ArbitrageOpportunityFinder

	// Find cross-exchange arbitrage for each symbol
	for symbol, exchanges := range marketData {
		if len(exchanges) < 2 {
			continue
		}

		opps := s.findCrossExchangeArbitrageForSymbol(symbol, exchanges, minProfit)
		opportunities = append(opportunities, opps...)
	}

	return opportunities, nil
}

// findCrossExchangeArbitrageForSymbol finds arbitrage opportunities for a single symbol
func (s *ArbitrageFinderService) findCrossExchangeArbitrageForSymbol(symbol string, exchanges map[string]database.MarketDataPoint, minProfit float64) []ArbitrageOpportunityFinder {
	var opportunities []ArbitrageOpportunityFinder

	// Find lowest and highest prices across exchanges
	var lowestPrice, highestPrice struct {
		exchange  string
		price     float64
		volume    float64
		timestamp time.Time
	}

	for exchange, data := range exchanges {
		// Find lowest price (best buy opportunity)
		if lowestPrice.price == 0 || data.Price < lowestPrice.price {
			lowestPrice.exchange = exchange
			lowestPrice.price = data.Price
			lowestPrice.volume = data.Volume
			lowestPrice.timestamp = data.Timestamp
		}

		// Find highest price (best sell opportunity)
		if highestPrice.price == 0 || data.Price > highestPrice.price {
			highestPrice.exchange = exchange
			highestPrice.price = data.Price
			highestPrice.volume = data.Volume
			highestPrice.timestamp = data.Timestamp
		}
	}

	// Calculate profit opportunity using high-precision decimals
	if lowestPrice.price > 0 && highestPrice.price > lowestPrice.price && lowestPrice.exchange != highestPrice.exchange {
		buy := decimal.NewFromFloat(lowestPrice.price)
		sell := decimal.NewFromFloat(highestPrice.price)

		if buy.GreaterThan(decimal.Zero) && sell.GreaterThan(buy) {
			profitPercentDec := sell.Sub(buy).Div(buy).Mul(decimal.NewFromInt(100))
			minProfitDec := decimal.NewFromFloat(minProfit)

			if profitPercentDec.GreaterThanOrEqual(minProfitDec) {
				// Use minimum volume between exchanges
				minVol := math.Min(lowestPrice.volume, highestPrice.volume)
				volDec := decimal.NewFromFloat(minVol)
				profitAmountDec := sell.Sub(buy).Mul(volDec)

				buyF, _ := buy.Float64()
				sellF, _ := sell.Float64()
				profitPercentF, _ := profitPercentDec.Float64()
				profitAmountF, _ := profitAmountDec.Float64()

				opportunity := ArbitrageOpportunityFinder{
					Symbol:          symbol,
					BuyExchange:     lowestPrice.exchange,
					SellExchange:    highestPrice.exchange,
					BuyPrice:        buyF,
					SellPrice:       sellF,
					ProfitPercent:   profitPercentF,
					ProfitAmount:    profitAmountF,
					Volume:          minVol,
					Timestamp:       time.Now(),
					OpportunityType: "arbitrage",
				}

				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities
}

// FindTechnicalAnalysisOpportunities finds opportunities based on technical indicators
func (s *ArbitrageFinderService) FindTechnicalAnalysisOpportunities(ctx context.Context, minProfit float64, symbolFilter string) ([]ArbitrageOpportunityFinder, error) {
	thirtyMinutesAgo := time.Now().Add(-30 * time.Minute)
	marketData, err := s.repo.GetMarketDataForAnalysis(ctx, thirtyMinutesAgo, symbolFilter)
	if err != nil {
		return nil, err
	}

	var opportunities []ArbitrageOpportunityFinder

	// Analyze each symbol for technical opportunities
	for symbol, exchanges := range marketData {
		for exchange, priceHistory := range exchanges {
			if len(priceHistory) < 5 {
				continue // Need at least 5 data points
			}

			// Calculate moving averages and volatility
			var prices []float64
			for _, data := range priceHistory {
				prices = append(prices, data.Price)
			}

			// Simple moving average
			sma := s.calculateSMA(prices, 5)
			currentPrice := prices[0] // Most recent price

			// Check for oversold/overbought conditions
			smaDec := decimal.NewFromFloat(sma)
			curDec := decimal.NewFromFloat(currentPrice)

			if smaDec.IsZero() {
				continue
			}

			deviationPercentDec := curDec.Sub(smaDec).Div(smaDec).Mul(decimal.NewFromInt(100)).Abs()
			minProfitDec := decimal.NewFromFloat(minProfit)

			if deviationPercentDec.GreaterThanOrEqual(minProfitDec) {
				var buyPriceDec, sellPriceDec decimal.Decimal
				var opportunityType string

				if curDec.LessThan(smaDec) {
					// Oversold - buy opportunity
					buyPriceDec = curDec
					sellPriceDec = smaDec
					opportunityType = "technical_oversold"
				} else {
					// Overbought - sell opportunity
					buyPriceDec = smaDec
					sellPriceDec = curDec
					opportunityType = "technical_overbought"
				}

				profitPercentDec := sellPriceDec.Sub(buyPriceDec).Div(buyPriceDec).Mul(decimal.NewFromInt(100))

				buyF, _ := buyPriceDec.Float64()
				sellF, _ := sellPriceDec.Float64()
				profitPercentF, _ := profitPercentDec.Float64()

				vol := priceHistory[0].Volume

				opportunity := ArbitrageOpportunityFinder{
					Symbol:          symbol,
					BuyExchange:     exchange,
					SellExchange:    exchange,
					BuyPrice:        buyF,
					SellPrice:       sellF,
					ProfitPercent:   profitPercentF,
					ProfitAmount:    (sellF - buyF) * vol,
					Volume:          vol,
					Timestamp:       time.Now(),
					OpportunityType: opportunityType,
				}

				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities, nil
}

// FindVolatilityOpportunities finds opportunities based on volatility patterns
func (s *ArbitrageFinderService) FindVolatilityOpportunities(ctx context.Context, minProfit float64, symbolFilter string) ([]ArbitrageOpportunityFinder, error) {
	fifteenMinutesAgo := time.Now().Add(-15 * time.Minute)
	marketData, err := s.repo.GetMarketDataForAnalysis(ctx, fifteenMinutesAgo, symbolFilter)
	if err != nil {
		return nil, err
	}

	var opportunities []ArbitrageOpportunityFinder

	for symbol, exchanges := range marketData {
		for exchange, priceHistory := range exchanges {
			if len(priceHistory) < 3 {
				continue
			}

			// Calculate volatility (standard deviation of price changes)
			var changes []float64
			for i := 1; i < len(priceHistory); i++ {
				if priceHistory[i].Price > 0 {
					change := (priceHistory[i-1].Price - priceHistory[i].Price) / priceHistory[i].Price * 100
					changes = append(changes, change)
				}
			}

			if len(changes) < 2 {
				continue
			}

			// Calculate average volatility
			var sum float64
			for _, c := range changes {
				sum += math.Abs(c)
			}
			avgVolatility := sum / float64(len(changes))

			// If volatility is above threshold, create opportunity
			if avgVolatility >= minProfit {
				currentPrice := priceHistory[0].Price
				vol := priceHistory[0].Volume

				// Estimate profit based on volatility
				estimatedProfit := avgVolatility * 0.5 // Conservative estimate

				opportunity := ArbitrageOpportunityFinder{
					Symbol:          symbol,
					BuyExchange:     exchange,
					SellExchange:    exchange,
					BuyPrice:        currentPrice * (1 - avgVolatility/200),
					SellPrice:       currentPrice * (1 + avgVolatility/200),
					ProfitPercent:   estimatedProfit,
					ProfitAmount:    currentPrice * estimatedProfit / 100 * vol,
					Volume:          vol,
					Timestamp:       time.Now(),
					OpportunityType: "volatility",
				}

				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities, nil
}

// FindSpreadOpportunities finds opportunities based on bid-ask spread analysis
func (s *ArbitrageFinderService) FindSpreadOpportunities(ctx context.Context, minProfit float64, symbolFilter string) ([]ArbitrageOpportunityFinder, error) {
	// Spread analysis typically requires order book data
	// This is a simplified version using recent price variations as proxy
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	data, err := s.repo.GetRecentMarketData(ctx, fiveMinutesAgo, symbolFilter)
	if err != nil {
		return nil, err
	}

	var opportunities []ArbitrageOpportunityFinder

	// Group by symbol-exchange
	grouped := make(map[string][]database.MarketDataPoint)
	for _, dp := range data {
		key := dp.Symbol + ":" + dp.Exchange
		grouped[key] = append(grouped[key], dp)
	}

	for key, history := range grouped {
		if len(history) < 2 {
			continue
		}

		// Find high-low spread in recent data
		var high, low float64
		var totalVol float64

		for _, dp := range history {
			if high == 0 || dp.Price > high {
				high = dp.Price
			}
			if low == 0 || dp.Price < low {
				low = dp.Price
			}
			totalVol += dp.Volume
		}

		if low > 0 && high > low {
			spreadPercent := (high - low) / low * 100

			if spreadPercent >= minProfit {
				avgVol := totalVol / float64(len(history))

				// Parse key back to symbol and exchange
				// Simplified - in production, use proper parsing
				symbol := key
				exchange := "multiple"

				opportunity := ArbitrageOpportunityFinder{
					Symbol:          symbol,
					BuyExchange:     exchange,
					SellExchange:    exchange,
					BuyPrice:        low,
					SellPrice:       high,
					ProfitPercent:   spreadPercent,
					ProfitAmount:    (high - low) * avgVol,
					Volume:          avgVol,
					Timestamp:       time.Now(),
					OpportunityType: "spread",
				}

				opportunities = append(opportunities, opportunity)
			}
		}
	}

	return opportunities, nil
}

// GetHistory retrieves historical arbitrage opportunities
func (s *ArbitrageFinderService) GetHistory(ctx context.Context, limit, offset int, symbolFilter string) ([]ArbitrageOpportunityFinder, error) {
	records, err := s.repo.GetArbitrageHistory(ctx, limit, offset, symbolFilter)
	if err != nil {
		return nil, err
	}

	var opportunities []ArbitrageOpportunityFinder
	for _, r := range records {
		opportunities = append(opportunities, ArbitrageOpportunityFinder{
			Symbol:          r.Symbol,
			BuyExchange:     r.BuyExchange,
			SellExchange:    r.SellExchange,
			BuyPrice:        r.BuyPrice,
			SellPrice:       r.SellPrice,
			ProfitPercent:   r.ProfitPercent,
			Volume:          r.Volume,
			OpportunityType: r.OpportunityType,
			Timestamp:       r.DetectedAt,
		})
	}

	return opportunities, nil
}

// GetStats retrieves arbitrage statistics
func (s *ArbitrageFinderService) GetStats(ctx context.Context, window time.Duration) (*database.ArbitrageStatsRecord, error) {
	since := time.Now().Add(-window)
	return s.repo.GetArbitrageStats(ctx, since)
}

// calculateSMA calculates simple moving average
func (s *ArbitrageFinderService) calculateSMA(prices []float64, period int) float64 {
	if len(prices) < period {
		return 0
	}

	var sum float64
	for i := 0; i < period && i < len(prices); i++ {
		sum += prices[i]
	}

	return sum / float64(min(period, len(prices)))
}

