# Feature: AI Decision Loop + GoFlux Adapter

## neura-6ws: Simulate AI decision loop

- **Status**: in_progress
- **Description**: Implement decision simulation using historical OHLCV replay
- **Dependencies**: neura-1ol (completed in PR #145)
- **Implementation**:
  - Created `AIDecisionSimulator` in `internal/services/ai_decision_simulator.go`
  - Uses `OHLCVReplayEngine` for historical data replay
  - Integrates with `TraderAgent` for decision making on each candle
  - Records simulated decisions with confidence, reasoning, risk scores
  - Calculates P&L summary with return percentages

## neura-dvl: Implement GoFlux adapter

- **Status**: in_progress
- **Description**: Wrap goflux library for technical indicator calculations
- **Dependencies**: neura-d3r (goflux dependency added)
- **Implementation**:
  - Created `GoFluxAdapter` in `pkg/indicators/goflux_adapter.go`
  - Implements `IndicatorProvider` interface
  - Wraps existing goflux functions from `internal/talib`
  - Supports all indicators: SMA, EMA, RSI, MACD, BollingerBands, ATR, Stochastic, OBV, VWAP
  - Updated factory to return GoFlux adapter

## Test Evidence

- All indicator tests pass: `go test ./pkg/indicators/...`
- Build passes: `go build ./...`
