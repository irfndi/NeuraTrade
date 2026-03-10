# APP LAYER KNOWLEDGE BASE

## OVERVIEW
`internal/app/` contains actors and use-cases: the trading pipeline from market data collection through signal generation, risk gating, execution, and portfolio tracking.

## STRUCTURE
```text
app/
├── bootstrap/     # Dependency graph construction, readiness
├── marketdata/    # CollectorActor - exchange data ingestion
├── strategy/      # StrategyActor - signal generation from market events
├── risk/          # RiskActor + PolicyEngine - safety gates
├── execution/     # ExecutionActor - order placement via adapters
├── portfolio/     # PortfolioActor - position tracking, PnL
├── plugin/        # PluginActor - strategy/indicator plugin management
└── autonomy/      # Autonomous scalping, recovery, boundaries
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Start/stop exchanges | `marketdata/collector_actor.go` | StartExchange, PauseExchange |
| Generate signals | `strategy/strategy_actor.go` | Subscribe to MarketTick, emit SignalProposed |
| Approve/reject orders | `risk/risk_actor.go`, `risk/policy_engine.go` | Policy checks, kill switch |
| Place orders | `execution/actor.go` | TradingGateway adapter calls |
| Track positions | `portfolio/actor.go` | PositionUpdated, ExposureUpdated |
| Manage plugins | `plugin/plugin_actor.go` | Enable/disable strategies |

## EVENT FLOW
```
MarketTick → StrategyActor → SignalProposed → RiskActor → OrderIntentApproved → ExecutionActor → OrderPlaced → PortfolioActor
```

## CONVENTIONS
- Actors implement `Receive(ctx, Envelope) error`
- State is actor-owned; no shared mutable state
- Commands are messages; events are published to eventbus
- RiskActor is the final gate before any order

## CODE MAP
| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `CollectorActor` | struct | `marketdata/collector_actor.go` | Market data ingestion |
| `StrategyActor` | struct | `strategy/strategy_actor.go` | Signal generation |
| `RiskActor` | struct | `risk/risk_actor.go` | Policy gate |
| `PolicyEngine` | struct | `risk/policy_engine.go` | Hard/soft policy checks |
| `ExecutionActor` | struct | `execution/actor.go` | Order placement |
| `PortfolioActor` | struct | `portfolio/actor.go` | Position tracking |
| `PluginActor` | struct | `plugin/plugin_actor.go` | Plugin management |

## BACKLOG (bd CLI)

**Stats:** 312 total | 64 open | 1 in progress | 14 blocked | 247 closed | 50 ready

## ANTI-PATTERNS
- Bypassing RiskActor with direct adapter calls.
- Shared state between actors (use message passing).
- Blocking Receive handlers that stall the actor mailbox.
- Placing orders without policy approval.
