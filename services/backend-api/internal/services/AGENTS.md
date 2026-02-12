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

## ANTI-PATTERNS
- Expanding already-large orchestrator files with unrelated concerns.
- Introducing new direct DB queries in services that should use existing repository abstractions.
- Silent retries without metrics/logging context.
- Adding concurrency without clear cancellation/timeout behavior.
