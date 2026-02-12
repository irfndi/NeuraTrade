# BACKEND API KNOWLEDGE BASE

## OVERVIEW
`services/backend-api` is the primary runtime: Go API server, domain services, DB access, telemetry, and operational scripts.
Runtime wiring is explicit in `cmd/server/main.go`.

## STRUCTURE
```text
backend-api/
├── cmd/server/           # process entrypoint + startup wiring
├── internal/api/         # routes + HTTP handlers
├── internal/services/    # trading, signal, collector, orchestration logic
├── internal/database/    # DB connection + repositories
├── internal/ccxt/        # CCXT client integration for backend
├── database/             # migrations + migration tooling
├── scripts/              # startup/health/env/webhook ops
└── pkg/                  # shared interfaces + generated protobuf code
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Startup dependency graph | `cmd/server/main.go` | Constructor injection order is source of truth |
| Route registration | `internal/api/routes.go` | Endpoint grouping and middleware layering |
| Business logic changes | `internal/services/` | Largest file concentration |
| DB repositories | `internal/database/` | Keep query logic out of handlers |
| Migration changes | `database/migrations/` | Ordered SQL files, idempotent style |
| Runtime scripts | `scripts/` | Deployment and diagnostics controls |

## COMMANDS
```bash
go test ./...
go test ./internal/services/...
go vet ./...
golangci-lint run
./database/migrate.sh status
./database/migrate.sh run
```

## CONVENTIONS
- Prefer `internal/*` for app-specific logic; keep reusable abstractions in `pkg/interfaces`.
- Keep API handlers thin: parse/validate/request context in handler, decision logic in services.
- Constructor injection is the standard; avoid hidden package-level singletons.
- For money/price math in domain logic, use decimal types (`shopspring/decimal`), not float primitives.
- Test files are co-located (`*_test.go`) with extra integration coverage under `test/`.

## ANTI-PATTERNS
- Adding new handlers under legacy `internal/handlers/` when `internal/api/handlers/` is active.
- Editing generated protobuf files under `pkg/pb/` directly.
- Refactoring across modules while fixing a narrow bug (keep bugfixes minimal).
- Coupling handler response shaping with repository SQL operations.

## BACKLOG (bd CLI)
Complete backend roadmap tracked via `bd` (~80 items):

### Ready to Work (No Blockers)
- `neura-4ms`: Extend CCXT wrapper for unified exchange access
- `neura-wiz`: Gamma API wrapper (market discovery)
- `neura-yus`: AES-256-GCM encryption for API keys
- `neura-myb`: Wallet minimum balance checks
- `neura-2iq`: Analyst agent role

### Exchange Integration
- `neura-4ms`: CCXT wrapper extension
- `neura-xxy`: WebSocket market data subscription (depends on neura-4ms)
- `neura-1b6`: Token bucket rate limit management (depends on neura-xxy)
- `neura-wiz`: Gamma API wrapper (market discovery)
- `neura-qts`: CLOB API wrapper (order execution)
- `neura-adu`: Data API wrapper (positions/balances)
- `neura-4p6`: Exchange resilience monitoring
- `neura-za8`: Rate limit monitoring

### Security & Encryption
- `neura-yus`: AES-256-GCM encryption for API keys
- `neura-c7r`: Key masking in logs and Telegram output
- `neura-px6`: Security audits (gosec, gitleaks)
- `neura-9ai`: Intrusion detection

### Trading Engine
- `neura-cd1`: Arbitrage trigger detection
- `neura-cha`: Sum-to-one arbitrage skill.md codification
- `neura-a7r`: Tight stop-loss execution
- `neura-sa4`: Order book imbalance detection
- `neura-we2`: Scalping skill.md codification
- `neura-1wi`: FOK (Fill or Kill) order execution
- `neura-l70a`: Refactor talib wrapper to goflux

### Risk Management
- `neura-myb`: Wallet minimum checks → `neura-fs8`: API key permissions
- `neura-8y8`: Emergency rollbacks
- `neura-kxq`: Kill switch monitoring
- `neura-e8u`: Daily loss cap
- `neura-bol`: Consecutive-loss pause
- `neura-q4j`: Max drawdown halt
- `neura-3ms`: Position-size throttle

### Paper Trading
- `neura-u4w`: Paper execution simulation
- `neura-8de`: Virtual account tracking
- `neura-mm5`: Paper trade recording
- `neura-32w`: Fund with minimal capital (USDC)

### Budget & Reporting
- `neura-5z3`: Daily budget enforcement
- `neura-7mj`: Monthly budget enforcement
- `neura-94c`: /status budget display
- `neura-fvk`: Fund milestone alerts

### Order Management
- `neura-l2z`: place_order tool endpoint
- `neura-wz7`: cancel_order tool endpoint
- `neura-2xe`: Position snapshot tool
- `neura-txu`: Controlled liquidation tool

### Notifications & Streaming
- `neura-nh5`: Risk event notifications
- `neura-im9`: Quest progress updates
- `neura-axx`: Action streaming format
- `neura-bri`: AI reasoning summaries

### Technical Infrastructure
- `neura-161`: Distributed locks
- `neura-byz`: Goroutine pool with concurrency limits
- `neura-zn8c`: Replace in-memory state with persistent storage
- `neura-lue`: Expose readiness endpoints
- `neura-1s5`: Expose risk primitives
- `neura-4eo`: Expose arbitrage primitives
- `neura-duw`: Expose cleanup endpoints

## SCOPED GUIDES
- `internal/api/AGENTS.md`
- `internal/services/AGENTS.md`
- `database/AGENTS.md`
- `scripts/AGENTS.md`
