// Package autonomous implements Level-4 Agent Trading Autonomy for NeuraTrade.
//
// This package provides the core components for autonomous trading operations:
//   - StrategyProposalEngine: Generates and validates trading strategies from AI
//   - StagedRolloutManager: Manages staged deployment (shadow → paper → live)
//   - AutoRollbackEngine: Automatic rollback on performance degradation
//   - LiveTradingGate: Final authorization gate for live trading
//
// Architecture follows the actor-based platform design from docs/4-level.md PR10.
//
// # Safety Guarantees
//
// Live trading is only enabled when ALL conditions are met:
//   - Policy validation passes
//   - Safe mode is OFF
//   - Kill switch is OFF
//   - Strategy is in LIVE mode
//   - Risk budget is available
//   - Exchange connection is healthy
//
// # Rollout Stages
//
//   - Shadow: Strategy runs without placing orders, predictions compared to actual
//   - Paper: Simulated orders placed, behavior verified
//   - Live: Real orders with risk limits
//
// References:
//   - docs/Refactor.md - Full refactor plan
//   - docs/4-level.md - Level-4 autonomous trading plan
package autonomous
