package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/shopspring/decimal"
)

// MultiLegArbitrageCalculator handles triangular arbitrage detection.
type MultiLegArbitrageCalculator struct {
	feeProvider     FeeProvider
	defaultTakerFee decimal.Decimal
}

// NewMultiLegArbitrageCalculator creates a new calculator.
func NewMultiLegArbitrageCalculator(feeProvider FeeProvider, defaultTakerFee decimal.Decimal) *MultiLegArbitrageCalculator {
	if defaultTakerFee.IsZero() {
		defaultTakerFee = decimal.NewFromFloat(0.001)
	}
	return &MultiLegArbitrageCalculator{
		feeProvider:     feeProvider,
		defaultTakerFee: defaultTakerFee,
	}
}

// TickerData simplified for calculator
type TickerData struct {
	Symbol string
	Bid    decimal.Decimal
	Ask    decimal.Decimal
}

// FindTriangularOpportunities searches for triangular arbitrage across all provided tickers for a single exchange.
func (c *MultiLegArbitrageCalculator) FindTriangularOpportunities(ctx context.Context, exchangeName string, tickers []TickerData) ([]models.MultiLegOpportunity, error) {
	// Build a map for easy lookup
	tickerMap := make(map[string]TickerData)
	for _, t := range tickers {
		tickerMap[t.Symbol] = t
	}

	// Identify all currencies involved
	currencies := make(map[string]bool)
	for _, t := range tickers {
		parts := strings.Split(t.Symbol, "/")
		if len(parts) == 2 {
			currencies[parts[0]] = true
			currencies[parts[1]] = true
		}
	}

	var opportunities []models.MultiLegOpportunity

	// Basic triangular arbitrage: Starting with USDT or common quote
	startCurrencies := []string{"USDT", "USDC", "BTC", "ETH"}

	for _, start := range startCurrencies {
		if !currencies[start] {
			continue
		}

		// Find second leg currencies
		for _, mid1 := range getConnectedCurrencies(start, tickers) {
			// Find third leg currencies
			for _, mid2 := range getConnectedCurrencies(mid1, tickers) {
				if mid2 == start {
					continue // This would be a 2-leg arbitrage which is already handled
				}

				// Check if mid2 connects back to start
				if hasConnection(mid2, start, tickers) {
					opp, err := c.calculateTriangularOpp(ctx, exchangeName, start, mid1, mid2, tickerMap)
					if err == nil && opp.ProfitPercentage.GreaterThan(decimal.Zero) {
						opportunities = append(opportunities, opp)
					}
				}
			}
		}
	}

	return opportunities, nil
}

func (c *MultiLegArbitrageCalculator) calculateTriangularOpp(ctx context.Context, exchange, start, mid1, mid2 string, tickerMap map[string]TickerData) (models.MultiLegOpportunity, error) {
	// Leg 1: start -> mid1
	leg1, rate1, err := c.getLegInfo(ctx, exchange, start, mid1, tickerMap)
	if err != nil {
		return models.MultiLegOpportunity{}, err
	}

	// Leg 2: mid1 -> mid2
	leg2, rate2, err := c.getLegInfo(ctx, exchange, mid1, mid2, tickerMap)
	if err != nil {
		return models.MultiLegOpportunity{}, err
	}

	// Leg 3: mid2 -> start
	leg3, rate3, err := c.getLegInfo(ctx, exchange, mid2, start, tickerMap)
	if err != nil {
		return models.MultiLegOpportunity{}, err
	}

	// Total return = (1 * rate1 * rate2 * rate3)
	// Factors in fees at each leg
	takerFee := c.defaultTakerFee
	if c.feeProvider != nil {
		// Attempt to get specific fees (simplified for now, using default in this draft)
		f, _ := c.feeProvider.GetTakerFee(ctx, exchange, leg1.Symbol)
		if !f.IsZero() {
			takerFee = f
		}
	}

	totalReturn := decimal.NewFromInt(1).
		Mul(rate1).Mul(decimal.NewFromInt(1).Sub(takerFee)).
		Mul(rate2).Mul(decimal.NewFromInt(1).Sub(takerFee)).
		Mul(rate3).Mul(decimal.NewFromInt(1).Sub(takerFee))

	profitPercentage := totalReturn.Sub(decimal.NewFromInt(1)).Mul(decimal.NewFromInt(100))

	return models.MultiLegOpportunity{
		ExchangeName:     exchange,
		Legs:             []models.ArbitrageLeg{leg1, leg2, leg3},
		ProfitPercentage: profitPercentage,
		DetectedAt:       time.Now(),
		ExpiresAt:        time.Now().Add(1 * time.Minute),
	}, nil
}

func (c *MultiLegArbitrageCalculator) getLegInfo(ctx context.Context, exchange, from, to string, tickerMap map[string]TickerData) (models.ArbitrageLeg, decimal.Decimal, error) {
	symbol := from + "/" + to
	if t, ok := tickerMap[symbol]; ok {
		// Buying 'to' with 'from' -> using ASK price
		// Rate = 1 / ask
		if t.Ask.IsZero() {
			return models.ArbitrageLeg{}, decimal.Zero, fmt.Errorf("zero ask price")
		}
		return models.ArbitrageLeg{
			Symbol: symbol,
			Side:   "buy",
			Price:  t.Ask,
		}, decimal.NewFromInt(1).Div(t.Ask), nil
	}

	reverseSymbol := to + "/" + from
	if t, ok := tickerMap[reverseSymbol]; ok {
		// Selling 'from' for 'to' -> using BID price
		// Rate = bid
		return models.ArbitrageLeg{
			Symbol: reverseSymbol,
			Side:   "sell",
			Price:  t.Bid,
		}, t.Bid, nil
	}

	return models.ArbitrageLeg{}, decimal.Zero, fmt.Errorf("no path from %s to %s", from, to)
}

func getConnectedCurrencies(base string, tickers []TickerData) []string {
	var connected []string
	for _, t := range tickers {
		parts := strings.Split(t.Symbol, "/")
		if len(parts) != 2 {
			continue
		}
		if parts[0] == base {
			connected = append(connected, parts[1])
		} else if parts[1] == base {
			connected = append(connected, parts[0])
		}
	}
	return connected
}

func hasConnection(from, to string, tickers []TickerData) bool {
	symbol := from + "/" + to
	reverseSymbol := to + "/" + from
	for _, t := range tickers {
		if t.Symbol == symbol || t.Symbol == reverseSymbol {
			return true
		}
	}
	return false
}
