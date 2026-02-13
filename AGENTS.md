# PROJECT KNOWLEDGE BASE

**Generated:** 2026-02-13 11:20 Asia/Jakarta
**Commit:** 31953a0
**Branch:** development

## OVERVIEW
NeuraTrade is a multi-service trading platform: Go backend API + Bun TypeScript sidecar services for CCXT exchange access and Telegram delivery.
Work is concentrated in `services/backend-api`; sibling Bun services are `services/ccxt-service` and `services/telegram-service`.

## STRUCTURE
```text
NeuraTrade/
├── services/
│   ├── backend-api/      # Go API, domain logic, DB migrations, ops scripts
│   ├── ccxt-service/     # Bun + CCXT HTTP/gRPC exchange bridge
│   └── telegram-service/ # Bun + grammY bot + delivery endpoints
├── protos/               # Shared protobuf definitions
├── docs/                 # Plans and legacy docs
├── Makefile              # Canonical dev/test/build entrypoint
└── docker-compose.yaml   # Local/prod orchestration
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Backend runtime wiring | `services/backend-api/cmd/server/main.go` | Service construction order and startup lifecycle |
| API routing and middleware | `services/backend-api/internal/api/routes.go` | Route groups, auth/admin/telemetry touchpoints |
| Core trading logic | `services/backend-api/internal/services/` | Largest complexity hotspot |
| DB schema and migrations | `services/backend-api/database/` | `migrate.sh` + ordered SQL migrations |
| Operations scripts | `services/backend-api/scripts/` | Health, startup, env, webhook controls |
| Exchange bridge behavior | `services/ccxt-service/index.ts` | CCXT init, admin endpoints, gRPC |
| Telegram behavior | `services/telegram-service/index.ts` | Bot commands, webhook/polling, admin send |

## CODE MAP
| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `main` | function | `services/backend-api/cmd/server/main.go` | backend process entrypoint |
| `run` | function | `services/backend-api/cmd/server/main.go` | full backend initialization flow |
| `SetupRoutes` | function | `services/backend-api/internal/api/routes.go` | route registration root |
| `CollectorService` | struct | `services/backend-api/internal/services/collector.go` | market ingestion orchestrator |
| `SignalProcessor` | struct | `services/backend-api/internal/services/signal_processor.go` | signal pipeline coordinator |
| `FuturesArbitrageService` | struct | `services/backend-api/internal/services/futures_arbitrage_service.go` | funding-arb engine |
| `QuestEngine` | struct | `services/backend-api/internal/services/quest_engine.go` | autonomous quest scheduling with Redis coordination |

## CONVENTIONS
- Repository is service-first: contributors should edit under `services/*`; root-level `internal/` and `pkg/` are not primary development targets.
- Main command surface is Makefile-driven (`make build`, `make test`, `make lint`, `make typecheck`, `make coverage-check`).
- CI validation order is formatting -> lint/test/build/security (`.github/workflows/validation.yml`).
- Go lint profile is intentionally strict but with test-heavy exclusions in `services/backend-api/.golangci.yml`.

## ANTI-PATTERNS (THIS PROJECT)
- Editing generated protobuf artifacts directly (`*.pb.go`, `services/*/proto/*.ts`) — regenerate instead.
- Adding new API handlers under legacy `services/backend-api/internal/handlers/` when `internal/api/handlers/` is the active path.
- Using float primitives for money math in backend domain logic.
- Committing secrets or env files; test credentials should be generated dynamically.

## UNIQUE STYLES
- Backend wiring is explicit constructor injection in `cmd/server/main.go` (no DI container).
- Bun services enforce production `ADMIN_API_KEY` validation and disable admin endpoints when unset.
- Coverage gate is warning mode by default; set `STRICT=true` to enforce hard fail.

## COMMANDS
```bash
make build
make run
make dev
make test
make lint
make typecheck
make coverage-check
make dev-setup
make dev-down
```

## SCOPED GUIDES
- `services/backend-api/AGENTS.md`
- `services/backend-api/internal/api/AGENTS.md`
- `services/backend-api/internal/services/AGENTS.md`
- `services/backend-api/database/AGENTS.md`
- `services/backend-api/scripts/AGENTS.md`
- `services/ccxt-service/AGENTS.md`
- `services/telegram-service/AGENTS.md`

## BACKLOG (bd CLI)

**Stats:** 179 total | 91 open | 57 blocked | 86 closed | 34 ready

### Ready to Work (No Blockers)
- `neura-06k`: Health checks for Redis, SQL storage, exchange bridges
- `neura-xxy`: WebSocket market data subscription
- `neura-qts`: CLOB API wrapper (order execution)
- `neura-7et`: Build prompt builder from skill.md + context
- `neura-r1d`: Progressive disclosure system
- `neura-kpu`: Risk Manager agent role
- `neura-bol`: Consecutive-loss pause
- `neura-lue`: Expose readiness endpoints
- `neura-sa4`: Order book imbalance detection
- `neura-1wi`: FOK order execution

### Recently Completed (✓)
- ✓ `neura-we2`: Scalping skill.md codification
- ✓ `neura-ydp`: Operator identity encryption (Argon2)
- ✓ `neura-l1l`: Balance/funding validation
- ✓ `neura-yzv`: CLI bootstrap command
- ✓ `neura-5of`: Bind local operator profile to Telegram chat
- ✓ `neura-aav`: Connectivity checks for all configured providers
- ✓ `neura-9m3`: Trader agent role
- ✓ `neura-q4j`: Max drawdown halt
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
- ✓ `neura-8y8`: Emergency rollbacks

### Infrastructure & DevOps (Blocked)
- `neura-354`: CI/CD pipeline setup
- `neura-qfp`: Production Docker Compose
- `neura-wqa`: QuantVPS deployment
- `neura-q6o`: Containerize agent and infra services

### Exchange Integration (Blocked)
- `neura-4ms`: CCXT wrapper extension
- `neura-1b6`: Rate limit management (depends on neura-xxy)
- `neura-adu`: Data API wrapper (positions/balances)
- `neura-4p6`: Exchange resilience monitoring

### Security & Encryption (Blocked)
- `neura-c7r`: Key masking in logs/Telegram

### Quest & Agent System (Blocked)
- `neura-bxg`: One-time auth code generation
- `neura-im9`: Quest progress updates

### Trading Engine (Blocked)
- `neura-cd1`: Arbitrage trigger detection
- `neura-a7r`: Tight stop-loss execution

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

## NOTES
- LSP symbol tooling may be unavailable locally (`gopls` missing), so rely on grep/glob/read patterns for discovery.
- Session completion policy in this repository requires push verification (`git status` up-to-date with origin).
