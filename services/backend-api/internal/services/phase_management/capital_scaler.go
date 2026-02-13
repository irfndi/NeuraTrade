package phase_management

import (
	"fmt"

	zaplogrus "github.com/irfndi/neuratrade/internal/logging/zaplogrus"
	"github.com/shopspring/decimal"
)

type CapitalScaler struct {
	adapter *StrategyAdapter
	logger  *zaplogrus.Logger
}

func NewCapitalScaler(adapter *StrategyAdapter, logger *zaplogrus.Logger) *CapitalScaler {
	if logger == nil {
		logger = zaplogrus.New()
	}
	return &CapitalScaler{
		adapter: adapter,
		logger:  logger,
	}
}

func (cs *CapitalScaler) CalculatePositionSize(phase Phase, baseSize decimal.Decimal, confidence float64) decimal.Decimal {
	rules := cs.adapter.GetPositionSizingRules(phase)

	if confidence < 0 {
		confidence = 0
	}
	if confidence > 1 {
		confidence = 1
	}

	confidenceWeight := rules.ConfidenceWeight
	baseWeight := decimal.NewFromInt(1).Sub(confidenceWeight)

	confidenceMultiplier := decimal.NewFromFloat(confidence).Mul(confidenceWeight)
	baseMultiplier := baseWeight

	weightedMultiplier := confidenceMultiplier.Add(baseMultiplier)
	size := baseSize.Mul(rules.BaseSizeMultiplier).Mul(weightedMultiplier)

	if size.GreaterThan(rules.MaxPositionUSD) {
		cs.logger.WithField("phase", phase.String()).
			WithField("calculated_size", size.String()).
			WithField("max_size", rules.MaxPositionUSD.String()).
			Debug("Position size capped at maximum")
		return rules.MaxPositionUSD
	}

	if size.LessThan(rules.MinPositionUSD) {
		cs.logger.WithField("phase", phase.String()).
			WithField("calculated_size", size.String()).
			WithField("min_size", rules.MinPositionUSD.String()).
			Debug("Position size raised to minimum")
		return rules.MinPositionUSD
	}

	return size
}

func (cs *CapitalScaler) GetCapitalAllocation(phase Phase, totalCapital decimal.Decimal) AllocationConfig {
	alloc := cs.adapter.GetAllocationConfig(phase)

	primaryAllocation := totalCapital.Mul(alloc.PrimaryStrategyPercent.Div(decimal.NewFromInt(100)))
	secondaryAllocation := totalCapital.Mul(alloc.SecondaryStrategyPercent.Div(decimal.NewFromInt(100)))
	reserve := totalCapital.Mul(alloc.ReservePercent.Div(decimal.NewFromInt(100)))

	cs.logger.WithField("phase", phase.String()).
		WithField("total_capital", totalCapital.String()).
		WithField("primary_allocation", primaryAllocation.String()).
		WithField("secondary_allocation", secondaryAllocation.String()).
		WithField("reserve", reserve.String()).
		Debug("Capital allocation calculated")

	return AllocationConfig{
		PrimaryStrategyPercent:   alloc.PrimaryStrategyPercent,
		SecondaryStrategyPercent: alloc.SecondaryStrategyPercent,
		ReservePercent:           alloc.ReservePercent,
		MaxConcurrentPositions:   alloc.MaxConcurrentPositions,
	}
}

func (cs *CapitalScaler) CalculateScaledPositionSize(phase Phase, totalCapital decimal.Decimal, confidence float64, volatility float64) decimal.Decimal {
	alloc := cs.adapter.GetAllocationConfig(phase)
	rules := cs.adapter.GetPositionSizingRules(phase)

	availableCapital := totalCapital.Mul(alloc.PrimaryStrategyPercent.Div(decimal.NewFromInt(100)))

	maxSinglePosition := availableCapital.Div(decimal.NewFromInt(int64(alloc.MaxConcurrentPositions)))

	confidenceFactor := decimal.NewFromFloat(confidence)
	size := maxSinglePosition.Mul(confidenceFactor).Mul(rules.BaseSizeMultiplier)

	if rules.VolatilityAdjustment && volatility > 0 {
		volatilityFactor := decimal.NewFromFloat(1.0 / (1.0 + volatility))
		size = size.Mul(volatilityFactor)
	}

	return cs.enforcePositionLimits(phase, size, rules)
}

func (cs *CapitalScaler) enforcePositionLimits(phase Phase, size decimal.Decimal, rules PositionSizingRules) decimal.Decimal {
	if size.GreaterThan(rules.MaxPositionUSD) {
		return rules.MaxPositionUSD
	}
	if size.LessThan(rules.MinPositionUSD) {
		return rules.MinPositionUSD
	}
	return size
}

func (cs *CapitalScaler) GetMaxPositionSizeForPhase(phase Phase) decimal.Decimal {
	rules := cs.adapter.GetPositionSizingRules(phase)
	return rules.MaxPositionUSD
}

func (cs *CapitalScaler) GetMinPositionSizeForPhase(phase Phase) decimal.Decimal {
	rules := cs.adapter.GetPositionSizingRules(phase)
	return rules.MinPositionUSD
}

func (cs *CapitalScaler) ValidatePositionSize(phase Phase, size decimal.Decimal) error {
	rules := cs.adapter.GetPositionSizingRules(phase)

	if size.LessThan(rules.MinPositionUSD) {
		return fmt.Errorf("position size %s below minimum %s for phase %s", size.String(), rules.MinPositionUSD.String(), phase.String())
	}
	if size.GreaterThan(rules.MaxPositionUSD) {
		return fmt.Errorf("position size %s above maximum %s for phase %s", size.String(), rules.MaxPositionUSD.String(), phase.String())
	}

	return nil
}
