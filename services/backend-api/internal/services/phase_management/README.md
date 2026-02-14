# Trading Strategy Phase Management

This feature implements dynamic strategy adaptation based on portfolio growth phases, allowing trading strategies to automatically adjust as the portfolio grows.

## Overview

The Phase Management system provides four distinct growth phases, each with tailored strategies, risk parameters, and capital allocation rules:

- **Bootstrap** (<$10k): Conservative strategy with tight risk controls
- **Growth** ($10k-$50k): Moderate strategy with expanded position limits
- **Scale** ($50k-$200k): Balanced strategy with higher capital allocation
- **Mature** (>$200k): Conservative strategy with capital preservation focus

## Components

### PhaseDetector

Detects portfolio phase transitions based on value thresholds with hysteresis protection.

```go
detector := phase_management.NewPhaseDetector(config, logger)
event, transitioned := detector.AttemptTransition(portfolioValue, "reason")
```

Features:
- Configurable phase thresholds
- Hysteresis to prevent rapid oscillation
- Minimum phase duration enforcement
- Event-driven transition callbacks

### StrategyAdapter

Provides phase-specific strategy configurations.

```go
adapter := phase_management.NewStrategyAdapter(config)
strategy := adapter.SelectStrategy(phase)
riskParams := adapter.GetRiskParams(phase)
```

Features:
- Strategy configs per phase (conservative/moderate/aggressive)
- Risk parameters per phase
- Position sizing rules per phase
- Capital allocation configs

### CapitalScaler

Calculates position sizes with confidence and volatility weighting.

```go
scaler := phase_management.NewCapitalScaler(adapter, logger)
size := scaler.CalculatePositionSize(phase, baseSize, confidence)
allocation := scaler.GetCapitalAllocation(phase, totalCapital)
```

Features:
- Confidence-weighted sizing
- Volatility adjustment
- Min/max position limits per phase
- Capital allocation across strategies

### PhaseManager

Orchestrates all components with periodic monitoring.

```go
manager := phase_management.NewPhaseManager(config, logger)
manager.SetPortfolioGetter(func() (decimal.Decimal, error) {
    return getCurrentPortfolioValue()
})
manager.Start(ctx)
defer manager.Stop()
```

Features:
- Periodic phase checking via ticker
- Automatic strategy updates on phase transition
- Thread-safe operations
- Portfolio value integration

## Configuration

Default thresholds:
- Bootstrap: ≤$10,000
- Growth: $10,001 - $50,000
- Scale: $50,001 - $200,000
- Mature: >$200,000

Default hysteresis: 5% (prevents oscillation around thresholds)
Default minimum phase duration: 24 hours

## Tasks Completed

- ✅ neura-bnv: Bootstrap → Growth → Scale → Mature transitions
- ✅ neura-5ll: Phase-specific strategy adaptation
- ✅ neura-5jj: Automatic capital scaling
