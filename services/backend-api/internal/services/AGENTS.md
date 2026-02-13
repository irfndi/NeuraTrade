# SERVICES LAYER KNOWLEDGE BASE

## OVERVIEW
`internal/services` is the backend domain core: collector, arbitrage engines, signal pipeline, technical analysis, notifications, cleanup, and resilience components.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Market ingestion workers | `collector.go` | Highest complexity area |
| Spot arbitrage logic | `arbitrage_service.go` | Opportunity discovery + lifecycle |
| Futures/funding arbitrage | `futures_arbitrage_service.go`, `futures_arbitrage_calculator.go` | Calculation and execution planning |
| Signal orchestration | `signal_processor.go` | Pipeline coordination and quality gating |
| Signal aggregation/scoring | `signal_aggregator.go`, `signal_quality_scorer.go` | Merge and rank outputs |
| Notification dispatch | `notification.go` | Telegram integration and delivery policies |
| Resilience primitives | `circuit_breaker.go`, `error_recovery.go`, `timeout_manager.go` | Fault handling conventions |

## CONVENTIONS
- Constructors follow explicit dependency injection (`NewXService(...)`) with no DI container.
- Interfaces are used at key boundaries (`interfaces.go`) but many flows still operate on concrete services.
- Service methods should accept context where external I/O occurs.
- Keep financial arithmetic decimal-safe; avoid float-based money logic.
- Bugfixes should be minimal and local; avoid cross-service refactors in fixes.

## TESTING
```bash
go test ./internal/services/...
go test ./internal/services/... -run TestArbitrage
```
- Tests are large and co-located; prefer adding focused test cases to existing suites unless splitting is needed.
- Common patterns: table-driven tests, mock dependencies, setup helpers.

## BACKLOG (bd CLI)

**Stats:** 179 total | 98 open | 58 blocked | 81 closed | 40 ready

### Ready to Work (No Blockers)
- `neura-7et`: Build prompt builder from skill.md + context
- `neura-1wi`: FOK order execution

### Recently Completed (✓)
- ✓ `neura-06k`: Health checks for Redis, SQL storage, exchange bridges
- ✓ `neura-ydp`: Operator identity encryption (Argon2)
- ✓ `neura-l1l`: Balance/funding validation
- ✓ `neura-yzv`: CLI bootstrap command
- ✓ `neura-5of`: Bind local operator profile to Telegram chat
- ✓ `neura-aav`: Connectivity checks for all configured providers
- ✓ `neura-kpu`: Risk Manager agent role
- ✓ `neura-bol`: Consecutive-loss pause
- ✓ `neura-we2`: Scalping skill.md
- ✓ `neura-myb`: Wallet minimum checks
- ✓ `neura-3b9`: Priority levels (CRITICAL > HIGH > NORMAL > LOW)
- ✓ `neura-6tk`: Event-driven quest triggers
- ✓ `neura-2iq`: Analyst agent role
- ✓ `neura-fs8`: API key validation (permissions)
- ✓ `neura-2n4`: Quest state persistence
- ✓ `neura-e8u`: Daily loss cap
- ✓ `neura-cha`: Sum-to-one arbitrage skill.md
- ✓ `neura-2xe`: Position snapshot tool
- ✓ `neura-lue`: Readiness endpoints
- ✓ `neura-4eo`: Arbitrage primitives
- ✓ `neura-1nz`: Cron-based quest scheduling
- ✓ `neura-l70a`: Refactor talib wrapper to goflux
- ✓ `neura-161`: Distributed locks (Redis)
- ✓ `neura-byz`: Goroutine pool with concurrency limits
- ✓ `neura-axx`: Action streaming format
- ✓ `neura-nh5`: Risk event notifications
- ✓ `neura-5z3`: Daily budget enforcement
- ✓ `neura-7mj`: Monthly budget enforcement
- ✓ `neura-94c`: /status budget display
- ✓ `neura-l2z`: place_order tool endpoint
- ✓ `neura-wz7`: cancel_order tool endpoint

### Quest & Agent System (Blocked)
- `neura-im9`: Quest progress update notifications

### Signal & Analysis (Blocked)
- `neura-cd1`: Arbitrage trigger detection engine
- `neura-sa4`: Order book imbalance detection
- `neura-bri`: AI reasoning summaries for trades

### Risk & Monitoring (Blocked)
- `neura-8y8`: Emergency rollback system
- `neura-kxq`: Kill switch monitoring
- `neura-fvk`: Fund milestone alerts
- `neura-3ms`: Position-size throttle
- `neura-9ai`: Intrusion detection

### Execution & Orders (Blocked)
- `neura-a7r`: Tight stop-loss execution
- `neura-1wi`: FOK (Fill or Kill) order execution
- `neura-txu`: Controlled liquidation tool

### Paper Trading (Blocked)
- `neura-u4w`: Paper execution simulation
- `neura-8de`: Virtual account tracking
- `neura-mm5`: Paper trade recording
- `neura-32w`: Fund with minimal capital (USDC)

### Infrastructure (Blocked)
- `neura-zn8c`: Replace in-memory state with persistent storage

## ANTI-PATTERNS
- Expanding already-large orchestrator files with unrelated concerns.
- Introducing new direct DB queries in services that should use existing repository abstractions.
- Silent retries without metrics/logging context.
- Adding concurrency without clear cancellation/timeout behavior.
