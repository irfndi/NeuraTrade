package phase_management

import (
	"time"

	"github.com/shopspring/decimal"
)

type StrategyAdapter struct {
	strategyConfigs map[Phase]StrategyConfig
	riskParams      map[Phase]RiskParameters
	sizingRules     map[Phase]PositionSizingRules
	allocations     map[Phase]AllocationConfig
}

type StrategyAdapterConfig struct {
	BootstrapStrategy StrategyConfig
	GrowthStrategy    StrategyConfig
	ScaleStrategy     StrategyConfig
	MatureStrategy    StrategyConfig
	BootstrapRisk     RiskParameters
	GrowthRisk        RiskParameters
	ScaleRisk         RiskParameters
	MatureRisk        RiskParameters
	BootstrapSizing   PositionSizingRules
	GrowthSizing      PositionSizingRules
	ScaleSizing       PositionSizingRules
	MatureSizing      PositionSizingRules
	BootstrapAlloc    AllocationConfig
	GrowthAlloc       AllocationConfig
	ScaleAlloc        AllocationConfig
	MatureAlloc       AllocationConfig
}

func DefaultStrategyAdapterConfig() StrategyAdapterConfig {
	return StrategyAdapterConfig{
		BootstrapStrategy: StrategyConfig{
			Type:                StrategyConservative,
			Name:                "conservative_bootstrap",
			MaxPositions:        3,
			MaxExposurePercent:  decimal.NewFromFloat(20.0),
			MinSignalConfidence: 0.75,
			HoldTimeMax:         4 * time.Hour,
			RebalanceInterval:   1 * time.Hour,
		},
		GrowthStrategy: StrategyConfig{
			Type:                StrategyModerate,
			Name:                "moderate_growth",
			MaxPositions:        5,
			MaxExposurePercent:  decimal.NewFromFloat(25.0),
			MinSignalConfidence: 0.65,
			HoldTimeMax:         6 * time.Hour,
			RebalanceInterval:   2 * time.Hour,
		},
		ScaleStrategy: StrategyConfig{
			Type:                StrategyModerate,
			Name:                "moderate_scale",
			MaxPositions:        8,
			MaxExposurePercent:  decimal.NewFromFloat(20.0),
			MinSignalConfidence: 0.60,
			HoldTimeMax:         8 * time.Hour,
			RebalanceInterval:   4 * time.Hour,
		},
		MatureStrategy: StrategyConfig{
			Type:                StrategyConservative,
			Name:                "conservative_mature",
			MaxPositions:        10,
			MaxExposurePercent:  decimal.NewFromFloat(15.0),
			MinSignalConfidence: 0.70,
			HoldTimeMax:         12 * time.Hour,
			RebalanceInterval:   6 * time.Hour,
		},
		BootstrapRisk: RiskParameters{
			MaxDailyLossPercent:    decimal.NewFromFloat(2.0),
			MaxPositionLossPercent: decimal.NewFromFloat(3.0),
			MaxDrawdownPercent:     decimal.NewFromFloat(5.0),
			StopLossPercent:        decimal.NewFromFloat(2.0),
			TakeProfitPercent:      decimal.NewFromFloat(4.0),
			RiskPerTradePercent:    decimal.NewFromFloat(1.0),
		},
		GrowthRisk: RiskParameters{
			MaxDailyLossPercent:    decimal.NewFromFloat(3.0),
			MaxPositionLossPercent: decimal.NewFromFloat(5.0),
			MaxDrawdownPercent:     decimal.NewFromFloat(10.0),
			StopLossPercent:        decimal.NewFromFloat(3.0),
			TakeProfitPercent:      decimal.NewFromFloat(6.0),
			RiskPerTradePercent:    decimal.NewFromFloat(1.5),
		},
		ScaleRisk: RiskParameters{
			MaxDailyLossPercent:    decimal.NewFromFloat(2.5),
			MaxPositionLossPercent: decimal.NewFromFloat(4.0),
			MaxDrawdownPercent:     decimal.NewFromFloat(8.0),
			StopLossPercent:        decimal.NewFromFloat(2.5),
			TakeProfitPercent:      decimal.NewFromFloat(5.0),
			RiskPerTradePercent:    decimal.NewFromFloat(1.0),
		},
		MatureRisk: RiskParameters{
			MaxDailyLossPercent:    decimal.NewFromFloat(1.5),
			MaxPositionLossPercent: decimal.NewFromFloat(2.5),
			MaxDrawdownPercent:     decimal.NewFromFloat(5.0),
			StopLossPercent:        decimal.NewFromFloat(1.5),
			TakeProfitPercent:      decimal.NewFromFloat(3.0),
			RiskPerTradePercent:    decimal.NewFromFloat(0.75),
		},
		BootstrapSizing: PositionSizingRules{
			BaseSizeMultiplier:   decimal.NewFromFloat(0.5),
			ConfidenceWeight:     decimal.NewFromFloat(0.7),
			VolatilityAdjustment: true,
			MaxPositionUSD:       decimal.NewFromFloat(2000.0),
			MinPositionUSD:       decimal.NewFromFloat(100.0),
		},
		GrowthSizing: PositionSizingRules{
			BaseSizeMultiplier:   decimal.NewFromFloat(0.75),
			ConfidenceWeight:     decimal.NewFromFloat(0.6),
			VolatilityAdjustment: true,
			MaxPositionUSD:       decimal.NewFromFloat(10000.0),
			MinPositionUSD:       decimal.NewFromFloat(500.0),
		},
		ScaleSizing: PositionSizingRules{
			BaseSizeMultiplier:   decimal.NewFromFloat(1.0),
			ConfidenceWeight:     decimal.NewFromFloat(0.5),
			VolatilityAdjustment: true,
			MaxPositionUSD:       decimal.NewFromFloat(50000.0),
			MinPositionUSD:       decimal.NewFromFloat(1000.0),
		},
		MatureSizing: PositionSizingRules{
			BaseSizeMultiplier:   decimal.NewFromFloat(0.8),
			ConfidenceWeight:     decimal.NewFromFloat(0.4),
			VolatilityAdjustment: true,
			MaxPositionUSD:       decimal.NewFromFloat(100000.0),
			MinPositionUSD:       decimal.NewFromFloat(2000.0),
		},
		BootstrapAlloc: AllocationConfig{
			PrimaryStrategyPercent:   decimal.NewFromFloat(80.0),
			SecondaryStrategyPercent: decimal.NewFromFloat(10.0),
			ReservePercent:           decimal.NewFromFloat(10.0),
			MaxConcurrentPositions:   3,
		},
		GrowthAlloc: AllocationConfig{
			PrimaryStrategyPercent:   decimal.NewFromFloat(70.0),
			SecondaryStrategyPercent: decimal.NewFromFloat(20.0),
			ReservePercent:           decimal.NewFromFloat(10.0),
			MaxConcurrentPositions:   5,
		},
		ScaleAlloc: AllocationConfig{
			PrimaryStrategyPercent:   decimal.NewFromFloat(60.0),
			SecondaryStrategyPercent: decimal.NewFromFloat(25.0),
			ReservePercent:           decimal.NewFromFloat(15.0),
			MaxConcurrentPositions:   8,
		},
		MatureAlloc: AllocationConfig{
			PrimaryStrategyPercent:   decimal.NewFromFloat(50.0),
			SecondaryStrategyPercent: decimal.NewFromFloat(30.0),
			ReservePercent:           decimal.NewFromFloat(20.0),
			MaxConcurrentPositions:   10,
		},
	}
}

func NewStrategyAdapter(config StrategyAdapterConfig) *StrategyAdapter {
	return &StrategyAdapter{
		strategyConfigs: map[Phase]StrategyConfig{
			PhaseBootstrap: config.BootstrapStrategy,
			PhaseGrowth:    config.GrowthStrategy,
			PhaseScale:     config.ScaleStrategy,
			PhaseMature:    config.MatureStrategy,
		},
		riskParams: map[Phase]RiskParameters{
			PhaseBootstrap: config.BootstrapRisk,
			PhaseGrowth:    config.GrowthRisk,
			PhaseScale:     config.ScaleRisk,
			PhaseMature:    config.MatureRisk,
		},
		sizingRules: map[Phase]PositionSizingRules{
			PhaseBootstrap: config.BootstrapSizing,
			PhaseGrowth:    config.GrowthSizing,
			PhaseScale:     config.ScaleSizing,
			PhaseMature:    config.MatureSizing,
		},
		allocations: map[Phase]AllocationConfig{
			PhaseBootstrap: config.BootstrapAlloc,
			PhaseGrowth:    config.GrowthAlloc,
			PhaseScale:     config.ScaleAlloc,
			PhaseMature:    config.MatureAlloc,
		},
	}
}

func (sa *StrategyAdapter) SelectStrategy(phase Phase) StrategyConfig {
	if config, ok := sa.strategyConfigs[phase]; ok {
		return config
	}
	return sa.strategyConfigs[PhaseBootstrap]
}

func (sa *StrategyAdapter) GetRiskParams(phase Phase) RiskParameters {
	if params, ok := sa.riskParams[phase]; ok {
		return params
	}
	return sa.riskParams[PhaseBootstrap]
}

func (sa *StrategyAdapter) GetPositionSizingRules(phase Phase) PositionSizingRules {
	if rules, ok := sa.sizingRules[phase]; ok {
		return rules
	}
	return sa.sizingRules[PhaseBootstrap]
}

func (sa *StrategyAdapter) GetAllocationConfig(phase Phase) AllocationConfig {
	if alloc, ok := sa.allocations[phase]; ok {
		return alloc
	}
	return sa.allocations[PhaseBootstrap]
}

func (sa *StrategyAdapter) UpdateStrategyConfig(phase Phase, config StrategyConfig) {
	sa.strategyConfigs[phase] = config
}

func (sa *StrategyAdapter) UpdateRiskParams(phase Phase, params RiskParameters) {
	sa.riskParams[phase] = params
}

func (sa *StrategyAdapter) GetAllStrategies() map[Phase]StrategyConfig {
	result := make(map[Phase]StrategyConfig, len(sa.strategyConfigs))
	for k, v := range sa.strategyConfigs {
		result[k] = v
	}
	return result
}
