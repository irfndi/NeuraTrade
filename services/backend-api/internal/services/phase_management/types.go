package phase_management

import (
	"fmt"
	"time"

	"github.com/shopspring/decimal"
)

type Phase int

const (
	PhaseBootstrap Phase = iota
	PhaseGrowth
	PhaseScale
	PhaseMature
)

func (p Phase) String() string {
	switch p {
	case PhaseBootstrap:
		return "bootstrap"
	case PhaseGrowth:
		return "growth"
	case PhaseScale:
		return "scale"
	case PhaseMature:
		return "mature"
	default:
		return "unknown"
	}
}

func ParsePhase(s string) (Phase, error) {
	switch s {
	case "bootstrap":
		return PhaseBootstrap, nil
	case "growth":
		return PhaseGrowth, nil
	case "scale":
		return PhaseScale, nil
	case "mature":
		return PhaseMature, nil
	default:
		return PhaseBootstrap, fmt.Errorf("unknown phase: %s", s)
	}
}

type PhaseThresholds struct {
	BootstrapMax decimal.Decimal
	GrowthMax    decimal.Decimal
	ScaleMax     decimal.Decimal
}

func DefaultPhaseThresholds() PhaseThresholds {
	return PhaseThresholds{
		BootstrapMax: decimal.NewFromFloat(10000.0),
		GrowthMax:    decimal.NewFromFloat(50000.0),
		ScaleMax:     decimal.NewFromFloat(200000.0),
	}
}

type PhaseTransitionEvent struct {
	FromPhase      Phase
	ToPhase        Phase
	PortfolioValue decimal.Decimal
	TransitionedAt time.Time
	Reason         string
}

type PhaseTransitionHandler func(event PhaseTransitionEvent)

type StrategyType int

const (
	StrategyConservative StrategyType = iota
	StrategyModerate
	StrategyAggressive
)

func (s StrategyType) String() string {
	switch s {
	case StrategyConservative:
		return "conservative"
	case StrategyModerate:
		return "moderate"
	case StrategyAggressive:
		return "aggressive"
	default:
		return "unknown"
	}
}

type StrategyConfig struct {
	Type                StrategyType
	Name                string
	MaxPositions        int
	MaxExposurePercent  decimal.Decimal
	MinSignalConfidence float64
	HoldTimeMax         time.Duration
	RebalanceInterval   time.Duration
}

type RiskParameters struct {
	MaxDailyLossPercent    decimal.Decimal
	MaxPositionLossPercent decimal.Decimal
	MaxDrawdownPercent     decimal.Decimal
	StopLossPercent        decimal.Decimal
	TakeProfitPercent      decimal.Decimal
	RiskPerTradePercent    decimal.Decimal
}

type PositionSizingRules struct {
	BaseSizeMultiplier   decimal.Decimal
	ConfidenceWeight     decimal.Decimal
	VolatilityAdjustment bool
	MaxPositionUSD       decimal.Decimal
	MinPositionUSD       decimal.Decimal
}

type AllocationConfig struct {
	PrimaryStrategyPercent   decimal.Decimal
	SecondaryStrategyPercent decimal.Decimal
	ReservePercent           decimal.Decimal
	MaxConcurrentPositions   int
}

type PhaseHistoryRecord struct {
	ID         int64
	Phase      Phase
	EnteredAt  time.Time
	ExitedAt   *time.Time
	EntryValue decimal.Decimal
	ExitValue  *decimal.Decimal
	Duration   *time.Duration
	PnL        *decimal.Decimal
	PnLPercent *decimal.Decimal
	CreatedAt  time.Time
}
