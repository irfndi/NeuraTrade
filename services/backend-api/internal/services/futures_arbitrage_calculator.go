package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"github.com/irfandi/celebrum-ai-go/internal/models"
	"github.com/irfandi/celebrum-ai-go/internal/observability"
	"github.com/shopspring/decimal"
)

// FuturesArbitrageCalculator handles all calculations for futures arbitrage opportunities.
type FuturesArbitrageCalculator struct {
	// Configuration
	DefaultFundingInterval  int             // Hours between funding payments (usually 8)
	RiskFreeRate            decimal.Decimal // Annual risk-free rate for calculations
	DefaultVolatilityWindow int             // Days for volatility calculation
	feeProvider             FeeProvider
	defaultTakerFee         decimal.Decimal
}

// NewFuturesArbitrageCalculator creates a new calculator instance.
//
// Returns:
//
//	*FuturesArbitrageCalculator: Initialized calculator.
func NewFuturesArbitrageCalculator() *FuturesArbitrageCalculator {
	return &FuturesArbitrageCalculator{
		DefaultFundingInterval:  8,
		RiskFreeRate:            decimal.NewFromFloat(0.05), // 5% annual risk-free rate
		DefaultVolatilityWindow: 30,
		defaultTakerFee:         decimal.NewFromFloat(0.001),
	}
}

// WithFeeProvider attaches a fee provider to the calculator.
func (calc *FuturesArbitrageCalculator) WithFeeProvider(provider FeeProvider, defaultTakerFee decimal.Decimal) *FuturesArbitrageCalculator {
	calc.feeProvider = provider
	if !defaultTakerFee.IsZero() {
		calc.defaultTakerFee = defaultTakerFee
	}
	return calc
}

// CalculateFuturesArbitrage calculates a complete futures arbitrage opportunity.
//
// Parameters:
//
//	input: Calculation inputs.
//
// Returns:
//
//	*models.FuturesArbitrageOpportunity: Calculated opportunity.
//	error: Error if calculation fails.
func (calc *FuturesArbitrageCalculator) CalculateFuturesArbitrage(
	input models.FuturesArbitrageCalculationInput,
) (*models.FuturesArbitrageOpportunity, error) {

	// Calculate net funding rate (the key profit driver)
	netFundingRate := input.ShortFundingRate.Sub(input.LongFundingRate)

	// Calculate price difference
	priceDiff := input.ShortMarkPrice.Sub(input.LongMarkPrice)
	priceDiffPercentage := decimal.Zero
	if !input.LongMarkPrice.IsZero() {
		priceDiffPercentage = priceDiff.Div(input.LongMarkPrice).Mul(decimal.NewFromInt(100))
	}

	// Calculate time-based profit rates
	hourlyRate := calc.calculateHourlyRate(netFundingRate, input.FundingInterval)
	dailyRate := hourlyRate.Mul(decimal.NewFromInt(24))
	apy := calc.CalculateAPY(hourlyRate)

	// Calculate estimated profits for different time periods
	profit8h := calc.calculatePeriodProfit(input.BaseAmount, netFundingRate, 1) // 1 funding period
	profitDaily := calc.calculatePeriodProfit(input.BaseAmount, hourlyRate, 24)
	profitWeekly := calc.calculatePeriodProfit(input.BaseAmount, hourlyRate, 24*7)
	profitMonthly := calc.calculatePeriodProfit(input.BaseAmount, hourlyRate, 24*30)

	// Subtract estimated fees (entry + exit on both exchanges)
	estimatedFees := calc.calculateFees(input.BaseAmount, input.LongExchange, input.ShortExchange, input.Symbol)
	profit8h = profit8h.Sub(estimatedFees)
	profitDaily = profitDaily.Sub(estimatedFees)
	profitWeekly = profitWeekly.Sub(estimatedFees)
	profitMonthly = profitMonthly.Sub(estimatedFees)

	// Calculate risk metrics
	riskScore := calc.CalculateRiskScore(input)
	volatilityScore := calc.calculateVolatilityScore(input)
	liquidityScore := calc.calculateLiquidityScore(input)

	// Calculate position sizing recommendations
	positionSizing := calc.CalculatePositionSizing(input, riskScore)

	// Determine next funding time (simplified - would need real exchange data)
	nextFunding := calc.calculateNextFundingTime(input.FundingInterval)
	timeToFunding := int(time.Until(nextFunding).Minutes())

	opportunity := &models.FuturesArbitrageOpportunity{
		Symbol:                    input.Symbol,
		LongExchange:              input.LongExchange,
		ShortExchange:             input.ShortExchange,
		LongFundingRate:           input.LongFundingRate,
		ShortFundingRate:          input.ShortFundingRate,
		NetFundingRate:            netFundingRate,
		FundingInterval:           input.FundingInterval,
		LongMarkPrice:             input.LongMarkPrice,
		ShortMarkPrice:            input.ShortMarkPrice,
		PriceDifference:           priceDiff,
		PriceDifferencePercentage: priceDiffPercentage,
		HourlyRate:                hourlyRate,
		DailyRate:                 dailyRate,
		APY:                       apy,
		EstimatedProfit8h:         profit8h,
		EstimatedProfitDaily:      profitDaily,
		EstimatedProfitWeekly:     profitWeekly,
		EstimatedProfitMonthly:    profitMonthly,
		RiskScore:                 riskScore,
		VolatilityScore:           volatilityScore,
		LiquidityScore:            liquidityScore,
		RecommendedPositionSize:   positionSizing.KellyPositionSize,
		MaxLeverage:               positionSizing.MaxSafeLeverage,
		RecommendedLeverage:       positionSizing.OptimalLeverage,
		StopLossPercentage:        positionSizing.MaxLossPercentage,
		MinPositionSize:           positionSizing.ConservativeSize,
		MaxPositionSize:           positionSizing.AggressiveSize,
		OptimalPositionSize:       positionSizing.ModerateSize,
		DetectedAt:                time.Now(),
		ExpiresAt:                 time.Now().Add(time.Hour * time.Duration(input.FundingInterval)),
		NextFundingTime:           nextFunding,
		TimeToNextFunding:         timeToFunding,
		IsActive:                  calc.isOpportunityActive(netFundingRate, riskScore),
	}

	return opportunity, nil
}

// calculateHourlyRate converts funding rate to hourly rate
func (calc *FuturesArbitrageCalculator) calculateHourlyRate(netFundingRate decimal.Decimal, fundingInterval int) decimal.Decimal {
	if fundingInterval <= 0 {
		fundingInterval = calc.DefaultFundingInterval
	}

	// Convert funding rate (per funding period) to hourly rate
	return netFundingRate.Div(decimal.NewFromInt(int64(fundingInterval)))
}

// CalculateAPY calculates Annual Percentage Yield from hourly rate.
//
// Parameters:
//
//	hourlyRate: Hourly profit rate.
//
// Returns:
//
//	decimal.Decimal: APY.
func (calc *FuturesArbitrageCalculator) CalculateAPY(hourlyRate decimal.Decimal) decimal.Decimal {
	// APY = (1 + hourly_rate)^(24*365) - 1
	// Using compound interest formula

	if hourlyRate.IsZero() {
		return decimal.Zero
	}

	// Cap extreme rates to prevent float64 overflow
	// 1% per hour max (which is already an extreme 8760% APY before compounding)
	maxHourlyRate := decimal.NewFromFloat(0.01)
	cappedRate := hourlyRate
	if hourlyRate.Abs().GreaterThan(maxHourlyRate) {
		if hourlyRate.IsPositive() {
			cappedRate = maxHourlyRate
		} else {
			cappedRate = maxHourlyRate.Neg()
		}
	}

	// Convert to float for power calculation
	hourlyRateFloat, _ := cappedRate.Float64()
	hoursPerYear := 24.0 * 365.0

	// Calculate compound growth
	apyFloat := math.Pow(1+hourlyRateFloat, hoursPerYear) - 1

	// Cap APY at 1000% (10.0 as decimal) to prevent unrealistic values
	// and handle potential infinity from extreme compounding
	if math.IsInf(apyFloat, 1) || apyFloat > 10.0 {
		apyFloat = 10.0
	} else if math.IsInf(apyFloat, -1) || apyFloat < -1.0 {
		apyFloat = -1.0 // Can't lose more than 100%
	} else if math.IsNaN(apyFloat) {
		apyFloat = 0.0
	}

	// Convert back to decimal and express as percentage
	return decimal.NewFromFloat(apyFloat * 100)
}

// calculatePeriodProfit calculates profit for a specific time period
func (calc *FuturesArbitrageCalculator) calculatePeriodProfit(
	baseAmount decimal.Decimal,
	hourlyRate decimal.Decimal,
	hours int,
) decimal.Decimal {
	// Simple compound interest: amount * (1 + rate)^periods - amount
	if hourlyRate.IsZero() || baseAmount.IsZero() {
		return decimal.Zero
	}

	rateFloat, _ := hourlyRate.Float64()
	amountFloat, _ := baseAmount.Float64()

	finalAmount := amountFloat * math.Pow(1+rateFloat, float64(hours))
	profit := finalAmount - amountFloat

	return decimal.NewFromFloat(profit)
}

// CalculateRiskScore calculates overall risk score (0-100).
//
// Parameters:
//
//	input: Calculation inputs.
//
// Returns:
//
//	decimal.Decimal: Risk score.
func (calc *FuturesArbitrageCalculator) CalculateRiskScore(
	input models.FuturesArbitrageCalculationInput,
) decimal.Decimal {
	// Risk factors:
	// 1. Price difference risk (higher price diff = higher risk)
	// 2. Funding rate volatility
	// 3. Exchange reliability
	// 4. Leverage risk

	var riskFactors []decimal.Decimal

	// Price difference risk (0-30 points)
	priceDiffRisk := decimal.Zero
	if !input.LongMarkPrice.IsZero() {
		priceDiffPercentage := input.ShortMarkPrice.Sub(input.LongMarkPrice).Div(input.LongMarkPrice).Abs()
		// Higher price difference = higher risk
		priceDiffRisk = priceDiffPercentage.Mul(decimal.NewFromInt(3000)) // Scale to 0-30
		if priceDiffRisk.GreaterThan(decimal.NewFromInt(30)) {
			priceDiffRisk = decimal.NewFromInt(30)
		}
	}
	riskFactors = append(riskFactors, priceDiffRisk)

	// Funding rate magnitude risk (0-25 points)
	fundingRateRisk := input.LongFundingRate.Sub(input.ShortFundingRate).Abs().Mul(decimal.NewFromInt(2500))
	if fundingRateRisk.GreaterThan(decimal.NewFromInt(25)) {
		fundingRateRisk = decimal.NewFromInt(25)
	}
	riskFactors = append(riskFactors, fundingRateRisk)

	// Leverage risk (0-25 points)
	leverageRisk := decimal.Zero
	if input.MaxLeverageAllowed.GreaterThan(decimal.NewFromInt(1)) {
		// Higher leverage = higher risk
		leverageRisk = input.MaxLeverageAllowed.Sub(decimal.NewFromInt(1)).Mul(decimal.NewFromInt(5))
		if leverageRisk.GreaterThan(decimal.NewFromInt(25)) {
			leverageRisk = decimal.NewFromInt(25)
		}
	}
	riskFactors = append(riskFactors, leverageRisk)

	// Exchange risk (0-20 points) - simplified
	exchangeRisk := decimal.NewFromInt(10) // Base exchange risk
	riskFactors = append(riskFactors, exchangeRisk)

	// Sum all risk factors
	totalRisk := decimal.Zero
	for _, risk := range riskFactors {
		totalRisk = totalRisk.Add(risk)
	}

	// Ensure risk score is between 0-100
	if totalRisk.GreaterThan(decimal.NewFromInt(100)) {
		totalRisk = decimal.NewFromInt(100)
	}

	return totalRisk
}

// calculateVolatilityScore calculates volatility-based risk score
func (calc *FuturesArbitrageCalculator) calculateVolatilityScore(
	input models.FuturesArbitrageCalculationInput,
) decimal.Decimal {
	// Simplified volatility calculation based on price difference
	// In a real implementation, this would use historical price data

	if input.LongMarkPrice.IsZero() {
		return decimal.NewFromInt(50) // Medium volatility as default
	}

	priceDiffPercentage := input.ShortMarkPrice.Sub(input.LongMarkPrice).Div(input.LongMarkPrice).Abs()
	volatilityScore := priceDiffPercentage.Mul(decimal.NewFromInt(1000)) // Scale to 0-100

	if volatilityScore.GreaterThan(decimal.NewFromInt(100)) {
		volatilityScore = decimal.NewFromInt(100)
	}

	return volatilityScore
}

// calculateLiquidityScore calculates liquidity-based score using simplified estimation.
// For more accurate liquidity scoring, use calculateLiquidityScoreWithOrderBook.
func (calc *FuturesArbitrageCalculator) calculateLiquidityScore(
	input models.FuturesArbitrageCalculationInput,
) decimal.Decimal {
	// Calculate liquidity score based on available market data
	// Higher score indicates better liquidity

	// Base score from price convergence - smaller spread indicates better liquidity
	priceSpread := input.LongMarkPrice.Sub(input.ShortMarkPrice).Abs()
	priceSpreadPercent := priceSpread.Div(input.LongMarkPrice).Mul(decimal.NewFromInt(100))

	// Score decreases as price spread increases
	spreadScore := decimal.NewFromInt(100).Sub(priceSpreadPercent.Mul(decimal.NewFromFloat(2.0)))
	if spreadScore.LessThan(decimal.NewFromInt(0)) {
		spreadScore = decimal.NewFromInt(0)
	}

	// Base amount consideration - larger amounts may have liquidity impact
	amountScore := decimal.NewFromInt(100)
	if input.BaseAmount.GreaterThan(decimal.NewFromInt(100000)) { // > $100k
		amountScore = decimal.NewFromInt(80) // Reduced score for large amounts
	} else if input.BaseAmount.GreaterThan(decimal.NewFromInt(50000)) { // > $50k
		amountScore = decimal.NewFromInt(90)
	}

	// Exchange-specific liquidity factors (simplified)
	exchangeScore := decimal.NewFromInt(100)
	if input.LongExchange != input.ShortExchange {
		// Different exchanges may have different liquidity profiles
		exchangeScore = decimal.NewFromInt(90)
	}

	// Weighted average of different factors
	liquidityScore := spreadScore.Mul(decimal.NewFromFloat(0.5)).
		Add(amountScore.Mul(decimal.NewFromFloat(0.3))).
		Add(exchangeScore.Mul(decimal.NewFromFloat(0.2)))

	// Ensure score is within 0-100 range
	if liquidityScore.GreaterThan(decimal.NewFromInt(100)) {
		liquidityScore = decimal.NewFromInt(100)
	} else if liquidityScore.LessThan(decimal.NewFromInt(0)) {
		liquidityScore = decimal.NewFromInt(0)
	}

	return liquidityScore
}

// OrderBookMetricsInput contains order book metrics for both exchanges.
type OrderBookMetricsInput struct {
	LongExchangeMetrics  *models.OrderBookMetrics
	ShortExchangeMetrics *models.OrderBookMetrics
}

// calculateLiquidityScoreWithOrderBook calculates liquidity score using real order book data.
// This provides more accurate liquidity assessment than the simplified estimation.
func (calc *FuturesArbitrageCalculator) calculateLiquidityScoreWithOrderBook(
	input models.FuturesArbitrageCalculationInput,
	orderBookMetrics *OrderBookMetricsInput,
) decimal.Decimal {
	// If no order book metrics provided, fall back to simplified calculation
	if orderBookMetrics == nil ||
		orderBookMetrics.LongExchangeMetrics == nil ||
		orderBookMetrics.ShortExchangeMetrics == nil {
		return calc.calculateLiquidityScore(input)
	}

	longMetrics := orderBookMetrics.LongExchangeMetrics
	shortMetrics := orderBookMetrics.ShortExchangeMetrics

	// 1. Bid-Ask Spread Score (40% weight)
	// Average spread across both exchanges
	avgSpread := longMetrics.BidAskSpread.Add(shortMetrics.BidAskSpread).Div(decimal.NewFromInt(2))
	// Lower spread = higher score. 0.01% = 100, 0.5% = 0
	spreadScore := decimal.NewFromInt(100).Sub(avgSpread.Mul(decimal.NewFromInt(200)))
	if spreadScore.LessThan(decimal.Zero) {
		spreadScore = decimal.Zero
	}

	// 2. Depth Score (35% weight)
	// Total depth within 1% of mid price on both exchanges
	totalDepth := longMetrics.BidDepth1Pct.Add(longMetrics.AskDepth1Pct).
		Add(shortMetrics.BidDepth1Pct).Add(shortMetrics.AskDepth1Pct)
	// $2M total depth = 100, scale proportionally
	depthScore := totalDepth.Div(decimal.NewFromInt(20000)).Mul(decimal.NewFromInt(10))
	if depthScore.GreaterThan(decimal.NewFromInt(100)) {
		depthScore = decimal.NewFromInt(100)
	}

	// 3. Slippage Score (25% weight)
	// Check if position size is fillable with acceptable slippage
	positionSizeKey := input.BaseAmount.Round(0).String()
	slippageScore := decimal.NewFromInt(100) // Default high if no estimate

	// Check long exchange slippage
	if longEstimate, exists := longMetrics.SlippageEstimates[positionSizeKey]; exists {
		if !longEstimate.IsFillable {
			slippageScore = decimal.NewFromInt(20) // Heavy penalty for unfillable
		} else {
			// Score based on slippage percentage (0% = 100, 1% = 0)
			longSlippageScore := decimal.NewFromInt(100).Sub(longEstimate.BuySlippage.Mul(decimal.NewFromInt(100)))
			if longSlippageScore.LessThan(slippageScore) {
				slippageScore = longSlippageScore
			}
		}
	}

	// Check short exchange slippage
	if shortEstimate, exists := shortMetrics.SlippageEstimates[positionSizeKey]; exists {
		if !shortEstimate.IsFillable {
			slippageScore = decimal.NewFromInt(20)
		} else {
			shortSlippageScore := decimal.NewFromInt(100).Sub(shortEstimate.SellSlippage.Mul(decimal.NewFromInt(100)))
			if shortSlippageScore.LessThan(slippageScore) {
				slippageScore = shortSlippageScore
			}
		}
	}

	if slippageScore.LessThan(decimal.Zero) {
		slippageScore = decimal.Zero
	}

	// Calculate weighted average
	liquidityScore := spreadScore.Mul(decimal.NewFromFloat(0.40)).
		Add(depthScore.Mul(decimal.NewFromFloat(0.35))).
		Add(slippageScore.Mul(decimal.NewFromFloat(0.25)))

	// Ensure score is within 0-100 range
	if liquidityScore.GreaterThan(decimal.NewFromInt(100)) {
		liquidityScore = decimal.NewFromInt(100)
	} else if liquidityScore.LessThan(decimal.Zero) {
		liquidityScore = decimal.Zero
	}

	return liquidityScore
}

// EstimateExecutionSlippage estimates total slippage for executing the arbitrage strategy.
// Returns the combined slippage percentage for both legs of the trade.
func (calc *FuturesArbitrageCalculator) EstimateExecutionSlippage(
	positionSize decimal.Decimal,
	orderBookMetrics *OrderBookMetricsInput,
) decimal.Decimal {
	if orderBookMetrics == nil ||
		orderBookMetrics.LongExchangeMetrics == nil ||
		orderBookMetrics.ShortExchangeMetrics == nil {
		// Return default slippage estimate when no order book data available
		if positionSize.GreaterThan(decimal.NewFromInt(100000)) {
			return decimal.NewFromFloat(0.2) // 0.2% for large positions
		} else if positionSize.GreaterThan(decimal.NewFromInt(50000)) {
			return decimal.NewFromFloat(0.1) // 0.1% for medium positions
		}
		return decimal.NewFromFloat(0.05) // 0.05% for small positions
	}

	positionKey := positionSize.Round(0).String()
	totalSlippage := decimal.Zero

	// Add long position slippage (buying on long exchange)
	if estimate, exists := orderBookMetrics.LongExchangeMetrics.SlippageEstimates[positionKey]; exists {
		totalSlippage = totalSlippage.Add(estimate.BuySlippage)
	}

	// Add short position slippage (selling on short exchange)
	if estimate, exists := orderBookMetrics.ShortExchangeMetrics.SlippageEstimates[positionKey]; exists {
		totalSlippage = totalSlippage.Add(estimate.SellSlippage)
	}

	return totalSlippage
}

// CalculateFuturesArbitrageWithOrderBook calculates arbitrage opportunity with real order book data.
// This provides more accurate profit estimates by accounting for actual slippage.
func (calc *FuturesArbitrageCalculator) CalculateFuturesArbitrageWithOrderBook(
	input models.FuturesArbitrageCalculationInput,
	orderBookMetrics *OrderBookMetricsInput,
) (*models.FuturesArbitrageOpportunity, error) {
	// First calculate the base opportunity
	opportunity, err := calc.CalculateFuturesArbitrage(input)
	if err != nil {
		return nil, err
	}

	// If order book metrics are available, enhance the calculation
	if orderBookMetrics != nil &&
		orderBookMetrics.LongExchangeMetrics != nil &&
		orderBookMetrics.ShortExchangeMetrics != nil {

		// Recalculate liquidity score with real data
		opportunity.LiquidityScore = calc.calculateLiquidityScoreWithOrderBook(input, orderBookMetrics)

		// Calculate execution slippage
		slippage := calc.EstimateExecutionSlippage(input.BaseAmount, orderBookMetrics)

		// Adjust profit estimates by slippage
		slippageCost := input.BaseAmount.Mul(slippage).Div(decimal.NewFromInt(100))
		opportunity.EstimatedProfit8h = opportunity.EstimatedProfit8h.Sub(slippageCost)
		opportunity.EstimatedProfitDaily = opportunity.EstimatedProfitDaily.Sub(slippageCost)
		opportunity.EstimatedProfitWeekly = opportunity.EstimatedProfitWeekly.Sub(slippageCost)
		opportunity.EstimatedProfitMonthly = opportunity.EstimatedProfitMonthly.Sub(slippageCost)

		// Mark opportunity as inactive if slippage makes it unprofitable
		if opportunity.EstimatedProfitDaily.LessThanOrEqual(decimal.Zero) {
			opportunity.IsActive = false
		}
	}

	return opportunity, nil
}

// CalculatePositionSizing calculates recommended position sizes and leverage.
//
// Parameters:
//
//	input: Calculation inputs.
//	riskScore: Calculated risk score.
//
// Returns:
//
//	models.FuturesPositionSizing: Position sizing details.
func (calc *FuturesArbitrageCalculator) CalculatePositionSizing(
	input models.FuturesArbitrageCalculationInput,
	riskScore decimal.Decimal,
) models.FuturesPositionSizing {

	// Kelly Criterion calculation (simplified)
	// Kelly % = (bp - q) / b
	// where b = odds, p = probability of win, q = probability of loss

	// Estimate win probability based on funding rate differential
	netFundingRate := input.ShortFundingRate.Sub(input.LongFundingRate)
	winProbability := decimal.NewFromFloat(0.6) // Base 60% win rate
	if netFundingRate.GreaterThan(decimal.NewFromFloat(0.001)) {
		winProbability = decimal.NewFromFloat(0.75) // Higher win rate for strong signals
	}

	// Calculate Kelly percentage (conservative approach)
	kellyPercentage := winProbability.Sub(decimal.NewFromFloat(0.5)).Mul(decimal.NewFromInt(2))
	if kellyPercentage.LessThan(decimal.Zero) {
		kellyPercentage = decimal.Zero
	}
	if kellyPercentage.GreaterThan(decimal.NewFromFloat(0.25)) {
		kellyPercentage = decimal.NewFromFloat(0.25) // Cap at 25%
	}

	// Calculate position sizes based on available capital
	kellyPositionSize := input.AvailableCapital.Mul(kellyPercentage)

	// Risk-adjusted position sizes
	riskAdjustment := decimal.NewFromInt(100).Sub(riskScore).Div(decimal.NewFromInt(100))

	conservativeSize := input.AvailableCapital.Mul(decimal.NewFromFloat(0.05)).Mul(riskAdjustment)
	moderateSize := input.AvailableCapital.Mul(decimal.NewFromFloat(0.15)).Mul(riskAdjustment)
	aggressiveSize := input.AvailableCapital.Mul(decimal.NewFromFloat(0.30)).Mul(riskAdjustment)

	// Leverage calculations
	maxSafeLeverage := calc.calculateMaxSafeLeverage(riskScore)
	optimalLeverage := maxSafeLeverage.Mul(decimal.NewFromFloat(0.7)) // 70% of max safe
	minLeverage := decimal.NewFromInt(1)

	// Risk management
	maxLossPercentage := calc.calculateMaxLossPercentage(input.UserRiskTolerance)
	stopLossPrice := input.LongMarkPrice.Mul(decimal.NewFromInt(1).Sub(maxLossPercentage.Div(decimal.NewFromInt(100))))
	takeProfitPrice := input.LongMarkPrice.Mul(decimal.NewFromInt(1).Add(netFundingRate.Mul(decimal.NewFromInt(3))))

	return models.FuturesPositionSizing{
		KellyPercentage:   kellyPercentage.Mul(decimal.NewFromInt(100)),
		KellyPositionSize: kellyPositionSize,
		ConservativeSize:  conservativeSize,
		ModerateSize:      moderateSize,
		AggressiveSize:    aggressiveSize,
		MinLeverage:       minLeverage,
		OptimalLeverage:   optimalLeverage,
		MaxSafeLeverage:   maxSafeLeverage,
		StopLossPrice:     stopLossPrice,
		TakeProfitPrice:   takeProfitPrice,
		MaxLossPercentage: maxLossPercentage,
	}
}

// calculateMaxSafeLeverage determines maximum safe leverage based on risk
func (calc *FuturesArbitrageCalculator) calculateMaxSafeLeverage(riskScore decimal.Decimal) decimal.Decimal {
	// Lower risk = higher safe leverage
	// Risk score 0-20: up to 10x leverage
	// Risk score 20-40: up to 5x leverage
	// Risk score 40-60: up to 3x leverage
	// Risk score 60-80: up to 2x leverage
	// Risk score 80+: 1x leverage only

	if riskScore.LessThan(decimal.NewFromInt(20)) {
		return decimal.NewFromInt(10)
	} else if riskScore.LessThan(decimal.NewFromInt(40)) {
		return decimal.NewFromInt(5)
	} else if riskScore.LessThan(decimal.NewFromInt(60)) {
		return decimal.NewFromInt(3)
	} else if riskScore.LessThan(decimal.NewFromInt(80)) {
		return decimal.NewFromInt(2)
	}

	return decimal.NewFromInt(1)
}

// calculateMaxLossPercentage determines maximum acceptable loss based on risk tolerance
func (calc *FuturesArbitrageCalculator) calculateMaxLossPercentage(riskTolerance string) decimal.Decimal {
	switch riskTolerance {
	case "low":
		return decimal.NewFromFloat(2.0) // 2% max loss
	case "medium":
		return decimal.NewFromFloat(5.0) // 5% max loss
	case "high":
		return decimal.NewFromFloat(10.0) // 10% max loss
	default:
		return decimal.NewFromFloat(3.0) // 3% default
	}
}

// calculateNextFundingTime estimates next funding time
func (calc *FuturesArbitrageCalculator) calculateNextFundingTime(fundingInterval int) time.Time {
	// Calculate next funding time based on interval
	now := time.Now().UTC()

	// Common funding intervals: 8 hours (00:00, 08:00, 16:00) or 4 hours
	var fundingHours []int
	if fundingInterval == 4 {
		fundingHours = []int{0, 4, 8, 12, 16, 20}
	} else {
		// Default to 8-hour intervals
		fundingHours = []int{0, 8, 16}
	}

	currentHour := now.Hour()
	currentMinute := now.Minute()
	nextFundingHour := -1

	for _, hour := range fundingHours {
		// If we're past the current hour, or at the current hour but past minute 0,
		// the next funding is at the next funding hour
		if hour > currentHour || (hour == currentHour && currentMinute == 0) {
			nextFundingHour = hour
			break
		}
	}

	// If no funding hour found today (we're past the last funding hour),
	// use first hour of next day
	if nextFundingHour == -1 {
		nextFundingHour = fundingHours[0]
		return time.Date(now.Year(), now.Month(), now.Day()+1, nextFundingHour, 0, 0, 0, time.UTC)
	}

	// If the next funding time is exactly now (currentMinute == 0 and hour matches),
	// that means funding just happened, so return current time
	if nextFundingHour == currentHour && currentMinute == 0 {
		return time.Date(now.Year(), now.Month(), now.Day(), nextFundingHour, 0, 0, 0, time.UTC)
	}

	return time.Date(now.Year(), now.Month(), now.Day(), nextFundingHour, 0, 0, 0, time.UTC)
}

// isOpportunityActive determines if an opportunity is worth pursuing
func (calc *FuturesArbitrageCalculator) isOpportunityActive(
	netFundingRate decimal.Decimal,
	riskScore decimal.Decimal,
) bool {
	// Minimum thresholds for active opportunities
	minFundingRate := decimal.NewFromFloat(0.00001) // 0.001% minimum (realistic for crypto funding rates)
	maxRiskScore := decimal.NewFromInt(80)          // Maximum 80% risk

	return netFundingRate.GreaterThan(minFundingRate) && riskScore.LessThan(maxRiskScore)
}

// CalculateRiskMetrics calculates comprehensive risk assessment.
//
// Parameters:
//
//	input: Calculation inputs.
//	historicalData: Historical funding rates.
//
// Returns:
//
//	models.FuturesArbitrageRiskMetrics: Risk metrics.
func (calc *FuturesArbitrageCalculator) CalculateRiskMetrics(
	input models.FuturesArbitrageCalculationInput,
	historicalData []models.FundingRateHistoryPoint,
) models.FuturesArbitrageRiskMetrics {

	// Calculate various risk metrics
	priceCorrelation := calc.calculatePriceCorrelation(historicalData)
	priceVolatility := calc.calculatePriceVolatility(historicalData)
	fundingRateVolatility := calc.calculateFundingRateVolatility(historicalData)
	fundingRateStability := calc.calculateFundingRateStability(historicalData)

	// Market risk metrics (simplified)
	bidAskSpread := decimal.NewFromFloat(0.001) // 0.1% default spread
	marketDepth := decimal.NewFromInt(75)       // 75% depth score
	slippageRisk := decimal.NewFromInt(25)      // 25% slippage risk

	// Exchange risk (would be enhanced with real data)
	exchangeReliability := decimal.NewFromInt(85) // 85% reliability
	counterpartyRisk := decimal.NewFromInt(15)    // 15% counterparty risk

	// Calculate overall risk score
	overallRisk := calc.calculateOverallRiskScore(
		priceVolatility, fundingRateVolatility, slippageRisk, counterpartyRisk,
	)

	// Determine risk category and recommendation
	riskCategory, recommendation := calc.categorizeRisk(overallRisk)

	return models.FuturesArbitrageRiskMetrics{
		PriceCorrelation:      priceCorrelation,
		PriceVolatility:       priceVolatility,
		FundingRateVolatility: fundingRateVolatility,
		FundingRateStability:  fundingRateStability,
		BidAskSpread:          bidAskSpread,
		MarketDepth:           marketDepth,
		SlippageRisk:          slippageRisk,
		ExchangeReliability:   exchangeReliability,
		CounterpartyRisk:      counterpartyRisk,
		OverallRiskScore:      overallRisk,
		RiskCategory:          riskCategory,
		Recommendation:        recommendation,
	}
}

// Helper methods for risk calculations
func (calc *FuturesArbitrageCalculator) calculatePriceCorrelation(data []models.FundingRateHistoryPoint) decimal.Decimal {
	if len(data) < 2 {
		return decimal.NewFromFloat(0.95) // Default high correlation
	}

	// Extract mark prices for correlation calculation
	prices := make([]float64, len(data))
	for i, point := range data {
		prices[i] = point.MarkPrice.InexactFloat64()
	}

	// Calculate Pearson correlation coefficient
	return calc.calculatePearsonCorrelation(prices)
}

// calculatePearsonCorrelation calculates Pearson correlation coefficient
func (calc *FuturesArbitrageCalculator) calculatePearsonCorrelation(prices []float64) decimal.Decimal {
	n := float64(len(prices))
	if n < 2 {
		return decimal.NewFromFloat(0.0)
	}

	// Calculate mean
	sum := 0.0
	for _, price := range prices {
		sum += price
	}
	mean := sum / n

	// Calculate covariance and standard deviations
	sumCovariance := 0.0
	sumVarianceX := 0.0
	sumVarianceY := 0.0

	// Use price series with lag 1 for correlation calculation
	for i := 0; i < len(prices)-1; i++ {
		x := prices[i] - mean
		y := prices[i+1] - mean

		sumCovariance += x * y
		sumVarianceX += x * x
		sumVarianceY += y * y
	}

	// Calculate correlation coefficient
	if sumVarianceX == 0 || sumVarianceY == 0 {
		return decimal.NewFromFloat(0.0)
	}

	correlation := sumCovariance / (math.Sqrt(sumVarianceX) * math.Sqrt(sumVarianceY))

	// Clamp correlation between -1 and 1
	if correlation > 1.0 {
		correlation = 1.0
	} else if correlation < -1.0 {
		correlation = -1.0
	}

	return decimal.NewFromFloat(correlation)
}

func (calc *FuturesArbitrageCalculator) calculatePriceVolatility(data []models.FundingRateHistoryPoint) decimal.Decimal {
	if len(data) < 2 {
		return decimal.NewFromInt(50) // Default volatility
	}

	// Calculate price volatility from historical data
	var prices []float64
	for _, point := range data {
		price, _ := point.MarkPrice.Float64()
		prices = append(prices, price)
	}

	// Calculate standard deviation
	mean := calc.calculateMean(prices)
	variance := calc.calculateVariance(prices, mean)
	stdDev := math.Sqrt(variance)

	// Convert to percentage and scale to 0-100
	volatility := (stdDev / mean) * 100
	if volatility > 100 {
		volatility = 100
	}

	return decimal.NewFromFloat(volatility)
}

func (calc *FuturesArbitrageCalculator) calculateFundingRateVolatility(data []models.FundingRateHistoryPoint) decimal.Decimal {
	if len(data) < 2 {
		return decimal.NewFromInt(30) // Default funding rate volatility
	}

	var rates []float64
	for _, point := range data {
		rate, _ := point.FundingRate.Float64()
		rates = append(rates, rate)
	}

	mean := calc.calculateMean(rates)
	variance := calc.calculateVariance(rates, mean)
	stdDev := math.Sqrt(variance)

	// Scale to 0-100
	volatility := stdDev * 10000 // Scale up for funding rates
	if volatility > 100 {
		volatility = 100
	}

	return decimal.NewFromFloat(volatility)
}

func (calc *FuturesArbitrageCalculator) calculateFundingRateStability(data []models.FundingRateHistoryPoint) decimal.Decimal {
	// Stability is inverse of volatility
	volatility := calc.calculateFundingRateVolatility(data)
	return decimal.NewFromInt(100).Sub(volatility)
}

func (calc *FuturesArbitrageCalculator) calculateOverallRiskScore(
	priceVol, fundingVol, slippage, counterparty decimal.Decimal,
) decimal.Decimal {
	// Weighted average of risk factors
	weights := map[string]decimal.Decimal{
		"price":        decimal.NewFromFloat(0.3),
		"funding":      decimal.NewFromFloat(0.25),
		"slippage":     decimal.NewFromFloat(0.25),
		"counterparty": decimal.NewFromFloat(0.2),
	}

	overallRisk := priceVol.Mul(weights["price"]).
		Add(fundingVol.Mul(weights["funding"])).
		Add(slippage.Mul(weights["slippage"])).
		Add(counterparty.Mul(weights["counterparty"]))

	return overallRisk
}

func (calc *FuturesArbitrageCalculator) categorizeRisk(riskScore decimal.Decimal) (string, string) {
	if riskScore.LessThan(decimal.NewFromInt(25)) {
		return "low", "Excellent opportunity with minimal risk. Recommended for all risk profiles."
	} else if riskScore.LessThan(decimal.NewFromInt(50)) {
		return "medium", "Good opportunity with moderate risk. Suitable for balanced portfolios."
	} else if riskScore.LessThan(decimal.NewFromInt(75)) {
		return "high", "Higher risk opportunity. Only suitable for aggressive risk tolerance."
	}
	return "extreme", "Very high risk. Not recommended for most traders."
}

// Mathematical helper functions
func (calc *FuturesArbitrageCalculator) calculateMean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

func (calc *FuturesArbitrageCalculator) calculateVariance(values []float64, mean float64) float64 {
	if len(values) <= 1 {
		return 0
	}

	sum := 0.0
	for _, v := range values {
		diff := v - mean
		sum += diff * diff
	}
	return sum / float64(len(values)-1)
}

// CalculateArbitrageOpportunities calculates regular price arbitrage opportunities across exchanges.
//
// Parameters:
//
//	ctx: Context.
//	marketData: Market data map.
//
// Returns:
//
//	[]models.ArbitrageOpportunity: List of opportunities.
//	error: Error if calculation fails.
func (calc *FuturesArbitrageCalculator) CalculateArbitrageOpportunities(
	ctx context.Context,
	marketData map[string][]models.MarketData,
) ([]models.ArbitrageOpportunity, error) {
	spanCtx, span := observability.StartSpanWithTags(ctx, observability.SpanOpArbitrage, "FuturesArbitrageCalculator.CalculateArbitrageOpportunities", map[string]string{
		"exchange_count": fmt.Sprintf("%d", len(marketData)),
	})
	defer observability.FinishSpan(span, nil)

	observability.AddBreadcrumb(spanCtx, "arbitrage_calculator", fmt.Sprintf("Calculating arbitrage opportunities across %d exchanges", len(marketData)), sentry.LevelInfo)

	var opportunities []models.ArbitrageOpportunity

	// Group market data by symbol across all exchanges
	symbolPrices := make(map[string]map[string]models.MarketData)

	for exchange, data := range marketData {
		for _, marketData := range data {
			symbol := ""
			if marketData.TradingPair != nil && marketData.TradingPair.Symbol != "" {
				symbol = marketData.TradingPair.Symbol
			}

			if symbol == "" {
				continue
			}

			if symbolPrices[symbol] == nil {
				symbolPrices[symbol] = make(map[string]models.MarketData)
			}
			symbolPrices[symbol][exchange] = marketData
		}
	}

	// Calculate arbitrage opportunities for each symbol
	for symbol, exchangeData := range symbolPrices {
		if len(exchangeData) < 2 {
			continue // Need at least 2 exchanges for arbitrage
		}

		symbolOpportunities := calc.calculateSymbolArbitrage(symbol, exchangeData)
		opportunities = append(opportunities, symbolOpportunities...)
	}

	observability.AddBreadcrumb(spanCtx, "arbitrage_calculator", fmt.Sprintf("Found %d arbitrage opportunities", len(opportunities)), sentry.LevelInfo)

	return opportunities, nil
}

// calculateSymbolArbitrage calculates arbitrage opportunities for a specific symbol
func (calc *FuturesArbitrageCalculator) calculateSymbolArbitrage(
	symbol string,
	exchangeData map[string]models.MarketData,
) []models.ArbitrageOpportunity {

	var opportunities []models.ArbitrageOpportunity

	// Find the lowest and highest prices
	var lowestPrice decimal.Decimal
	var highestPrice decimal.Decimal
	var lowestExchange string
	var highestExchange string
	var lowestData, highestData models.MarketData

	first := true
	for exchange, data := range exchangeData {
		if first {
			lowestPrice = data.LastPrice
			highestPrice = data.LastPrice
			lowestExchange = exchange
			highestExchange = exchange
			lowestData = data
			highestData = data
			first = false
			continue
		}

		if data.LastPrice.LessThan(lowestPrice) {
			lowestPrice = data.LastPrice
			lowestExchange = exchange
			lowestData = data
		}

		if data.LastPrice.GreaterThan(highestPrice) {
			highestPrice = data.LastPrice
			highestExchange = exchange
			highestData = data
		}
	}

	// Calculate profit percentage
	if lowestPrice.IsZero() {
		return opportunities
	}

	priceDifference := highestPrice.Sub(lowestPrice)
	profitPercentage := priceDifference.Div(lowestPrice).Mul(decimal.NewFromInt(100))

	// Only create opportunity if profit is meaningful (above 0.1%)
	if profitPercentage.GreaterThan(decimal.NewFromFloat(0.1)) {
		// Calculate estimated profit for a $10,000 position
		baseAmount := decimal.NewFromInt(10000)
		estimatedAmount := baseAmount.Div(lowestPrice).Mul(highestPrice)
		_ = estimatedAmount.Sub(baseAmount) // profitAmount for logging/debugging

		opportunity := models.ArbitrageOpportunity{
			ID:               uuid.New().String(),
			TradingPairID:    lowestData.TradingPairID,
			BuyExchangeID:    lowestData.ExchangeID,
			SellExchangeID:   highestData.ExchangeID,
			BuyPrice:         lowestPrice,
			SellPrice:        highestPrice,
			ProfitPercentage: profitPercentage,
			DetectedAt:       time.Now(),
			ExpiresAt:        time.Now().Add(time.Minute * 30), // Expire after 30 minutes
			BuyExchange:      &models.Exchange{ID: lowestData.ExchangeID, Name: lowestExchange},
			SellExchange:     &models.Exchange{ID: highestData.ExchangeID, Name: highestExchange},
			TradingPair:      lowestData.TradingPair,
		}

		opportunities = append(opportunities, opportunity)
	}

	return opportunities
}

// GenerateCompleteStrategy creates a complete futures arbitrage strategy with execution details.
//
// Parameters:
//
//	opportunity: The arbitrage opportunity.
//	availableCapital: Capital to allocate.
//	riskTolerance: User risk tolerance ("low", "medium", "high").
//
// Returns:
//
//	*models.FuturesArbitrageStrategy: Generated strategy.
func (calc *FuturesArbitrageCalculator) GenerateCompleteStrategy(
	opportunity *models.FuturesArbitrageOpportunity,
	availableCapital decimal.Decimal,
	riskTolerance string,
) *models.FuturesArbitrageStrategy {

	// Use default capital if not provided
	if availableCapital.LessThanOrEqual(decimal.Zero) {
		availableCapital = decimal.NewFromInt(1000) // $1000 fallback
	}

	// Determine position size based on risk tolerance
	var chosenSize decimal.Decimal
	switch riskTolerance {
	case "low":
		chosenSize = opportunity.MinPositionSize
	case "high":
		chosenSize = opportunity.OptimalPositionSize
	default: // "medium"
		chosenSize = opportunity.OptimalPositionSize
	}

	// Ensure position size doesn't exceed available capital
	if chosenSize.GreaterThan(availableCapital) {
		chosenSize = availableCapital.Mul(decimal.NewFromFloat(0.1)) // 10% of capital
	}

	// Generate LONG position details
	longPosition := models.PositionDetails{
		Exchange:   opportunity.LongExchange,
		Symbol:     opportunity.Symbol,
		Side:       "long",
		Size:       chosenSize,
		Leverage:   opportunity.RecommendedLeverage,
		EntryPrice: opportunity.LongMarkPrice,
		StopLoss: calc.calculateStopLossPrice(
			opportunity.LongMarkPrice,
			"long",
			opportunity.StopLossPercentage,
		),
		TakeProfit: calc.calculateTakeProfitPrice(
			opportunity.LongMarkPrice,
			"long",
			opportunity.NetFundingRate,
		),
		MarginRequired: chosenSize.Div(opportunity.RecommendedLeverage),
		EstimatedFees:  calc.calculateFees(chosenSize, opportunity.LongExchange, opportunity.ShortExchange, opportunity.Symbol),
	}

	// Generate SHORT position details
	shortPosition := models.PositionDetails{
		Exchange:   opportunity.ShortExchange,
		Symbol:     opportunity.Symbol,
		Side:       "short",
		Size:       chosenSize,
		Leverage:   opportunity.RecommendedLeverage,
		EntryPrice: opportunity.ShortMarkPrice,
		StopLoss: calc.calculateStopLossPrice(
			opportunity.ShortMarkPrice,
			"short",
			opportunity.StopLossPercentage,
		),
		TakeProfit: calc.calculateTakeProfitPrice(
			opportunity.ShortMarkPrice,
			"short",
			opportunity.NetFundingRate.Neg(),
		),
		MarginRequired: chosenSize.Div(opportunity.RecommendedLeverage),
		EstimatedFees:  calc.calculateFees(chosenSize, opportunity.LongExchange, opportunity.ShortExchange, opportunity.Symbol),
	}

	// Generate execution plan
	executionOrder := calc.generateExecutionOrder(longPosition, shortPosition)

	// Calculate expected returns
	expectedReturn := opportunity.EstimatedProfitDaily.Div(chosenSize).Mul(decimal.NewFromInt(100))

	// Create strategy
	strategy := &models.FuturesArbitrageStrategy{
		ID:   fmt.Sprintf("strategy_%s_%d", opportunity.Symbol, time.Now().Unix()),
		Name: fmt.Sprintf("Funding Rate Arbitrage: %s", opportunity.Symbol),
		Description: fmt.Sprintf("Long %s on %s, Short on %s. Net funding: %.4f%%",
			opportunity.Symbol, opportunity.LongExchange, opportunity.ShortExchange,
			opportunity.NetFundingRate.InexactFloat64()*100),
		Opportunity:    *opportunity,
		LongPosition:   longPosition,
		ShortPosition:  shortPosition,
		ExecutionOrder: executionOrder,
		ExpectedReturn: expectedReturn,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
		IsActive:       opportunity.IsActive,
	}

	return strategy
}

// Helper methods for strategy generation

func (calc *FuturesArbitrageCalculator) calculateStopLossPrice(
	entryPrice decimal.Decimal,
	side string,
	stopLossPercentage decimal.Decimal,
) decimal.Decimal {
	slFactor := stopLossPercentage.Div(decimal.NewFromInt(100))

	if side == "long" {
		// For long: stop loss is below entry
		return entryPrice.Mul(decimal.NewFromInt(1).Sub(slFactor))
	}

	// For short: stop loss is above entry
	return entryPrice.Mul(decimal.NewFromInt(1).Add(slFactor))
}

func (calc *FuturesArbitrageCalculator) calculateTakeProfitPrice(
	entryPrice decimal.Decimal,
	side string,
	netFundingRate decimal.Decimal,
) decimal.Decimal {
	// Target 3x the funding rate profit
	targetProfit := netFundingRate.Abs().Mul(decimal.NewFromInt(3))

	// Minimum 1% take profit
	if targetProfit.LessThan(decimal.NewFromFloat(0.01)) {
		targetProfit = decimal.NewFromFloat(0.01)
	}

	if side == "long" {
		// For long: take profit is above entry
		return entryPrice.Mul(decimal.NewFromInt(1).Add(targetProfit))
	}

	// For short: take profit is below entry
	return entryPrice.Mul(decimal.NewFromInt(1).Sub(targetProfit))
}

func (calc *FuturesArbitrageCalculator) calculateFees(positionSize decimal.Decimal, longExchange string, shortExchange string, symbol string) decimal.Decimal {
	// Typical taker fee 0.05% * 2 (entry + exit) on both legs
	longFee := calc.defaultTakerFee
	shortFee := calc.defaultTakerFee

	if calc.feeProvider != nil {
		if fee, err := calc.feeProvider.GetTakerFee(context.Background(), longExchange, symbol); err == nil && !fee.IsZero() {
			longFee = fee
		}
		if fee, err := calc.feeProvider.GetTakerFee(context.Background(), shortExchange, symbol); err == nil && !fee.IsZero() {
			shortFee = fee
		}
	}

	totalFeeRate := longFee.Add(shortFee).Mul(decimal.NewFromInt(2))
	return positionSize.Mul(totalFeeRate)
}

func (calc *FuturesArbitrageCalculator) generateExecutionOrder(
	long models.PositionDetails,
	short models.PositionDetails,
) []string {
	return []string{
		fmt.Sprintf("1. Deposit $%s margin to %s", long.MarginRequired.StringFixed(2), long.Exchange),
		fmt.Sprintf("2. Deposit $%s margin to %s", short.MarginRequired.StringFixed(2), short.Exchange),
		fmt.Sprintf("3. Open LONG %s on %s at $%s (%.1fx leverage)",
			long.Symbol, long.Exchange, long.EntryPrice.StringFixed(2), long.Leverage.InexactFloat64()),
		fmt.Sprintf("4. Open SHORT %s on %s at $%s (%.1fx leverage)",
			short.Symbol, short.Exchange, short.EntryPrice.StringFixed(2), short.Leverage.InexactFloat64()),
		"5. Set stop loss orders on both exchanges",
		"6. Set take profit orders on both exchanges",
		"7. Monitor funding payments every 8 hours",
		"8. Close positions if funding rate flips or target profit reached",
	}
}
