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

**Stats:** 185 total | 58 open | 8 in progress | 33 blocked | 119 closed | 28 ready

### Ready to Work (No Blockers)
- `neura-06k`: Health checks for Redis, SQL storage, exchange bridges
- `neura-xxy`: WebSocket market data subscription
- `neura-qts`: CLOB API wrapper (order execution)
- `neura-7et`: Build prompt builder from skill.md + context
- `neura-r1d`: Progressive disclosure system

### Recently Completed (✓)
- ✓ `neura-ydp`: Operator identity encryption (Argon2)
- ✓ `neura-l1l`: Balance/funding validation
- ✓ `neura-yzv`: CLI bootstrap command
- ✓ `neura-5of`: Bind local operator profile to Telegram chat
- ✓ `neura-aav`: Connectivity checks for all configured providers
- ✓ `neura-kpu`: Risk Manager agent role
- ✓ `neura-bol`: Consecutive-loss pause
- ✓ `neura-we2`: Scalping skill.md
- ✓ `neura-myb`: Wallet minimum balance checks
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
- ✓ `neura-1s5`: Risk primitives
- ✓ `neura-47g`: AI provider registry with models.dev
- ✓ `neura-yus`: AES-256-GCM encryption for API keys
- ✓ `neura-za8`: Rate limit monitoring
- ✓ `neura-px6`: Security audits (gosec, gitleaks)
- ✓ `neura-1nz`: Cron-based quest scheduling
- ✓ `neura-l70a`: Refactor talib wrapper to goflux
- ✓ `neura-161`: Distributed locks (Redis)
- ✓ `neura-byz`: Goroutine pool with concurrency limits
- ✓ `neura-axx`: Action streaming format
- ✓ `neura-nh5`: Risk event notifications
- ✓ `neura-wiz`: Gamma API wrapper (Polymarket)
- ✓ `neura-5z3`: Daily budget enforcement
- ✓ `neura-7mj`: Monthly budget enforcement
- ✓ `neura-94c`: /status budget display
- ✓ `neura-l2z`: place_order tool endpoint
- ✓ `neura-wz7`: cancel_order tool endpoint

### Exchange Integration (Blocked)
- `neura-4ms`: CCXT wrapper extension
- `neura-1b6`: Rate limit management (depends on neura-xxy)
- `neura-adu`: Data API wrapper (positions/balances)
- `neura-4p6`: Exchange resilience monitoring

### Security & Encryption (Blocked)
- `neura-c7r`: Key masking in logs/Telegram

### Trading Engine (Blocked)
- `neura-cd1`: Arbitrage trigger detection
- `neura-sa4`: Order book imbalance detection
- `neura-a7r`: Tight stop-loss execution
- `neura-1wi`: FOK order execution

### Risk Management (Blocked)
- `neura-8y8`: Emergency rollbacks
- `neura-kxq`: Kill switch monitoring
- `neura-3ms`: Position-size throttle

### Paper Trading (Blocked)
- `neura-u4w`: Paper execution simulation
- `neura-8de`: Virtual account tracking
- `neura-mm5`: Paper trade recording

### Budget & Reporting (Blocked)
- `neura-fvk`: Fund milestone alerts

### Notifications & Streaming (Blocked)
- `neura-bri`: AI reasoning summaries

### Technical Infrastructure (Blocked)
- `neura-zn8c`: Replace in-memory state with persistent storage
- `neura-duw`: Expose cleanup endpoints

### Order Management (Blocked)
- `neura-txu`: Controlled liquidation tool
- `neura-duw`: Expose cleanup endpoints

### Order Management (Blocked)
- `neura-2xe`: Position snapshot tool
- `neura-txu`: Controlled liquidation tool

## SCOPED GUIDES
- `internal/api/AGENTS.md`
- `internal/services/AGENTS.md`
- `database/AGENTS.md`
- `scripts/AGENTS.md`
