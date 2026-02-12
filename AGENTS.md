# PROJECT KNOWLEDGE BASE

**Generated:** 2026-02-12 22:01 Asia/Jakarta
**Commit:** d2efb34
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
Complete roadmap tracked via `bd` issue tracker (~150 items, 50+ active/open):

### Ready to Work (No Blockers)
- `neura-lk1`: Create install.sh shell script
- `neura-bxg`: One-time auth code generation
- `neura-myb`: Wallet minimum checks
- `neura-4ms`: CCXT wrapper extension
- `neura-wiz`: Gamma API wrapper (market discovery)
- `neura-yus`: AES-256-GCM encryption for API keys
- `neura-47g`: AI provider registry with models.dev
- `neura-20g`: skill.md file loader
- `neura-2iq`: Analyst agent role
- `neura-6tk`: Event-driven quest triggers

### Infrastructure & DevOps
- `neura-lk1` → `neura-yzv`: CLI bootstrap command
- `neura-354`: CI/CD pipeline setup
- `neura-qfp`: Production Docker Compose
- `neura-wqa`: QuantVPS deployment
- `neura-q6o`: Containerize agent and infra services

### Exchange Integration
- `neura-4ms`: CCXT wrapper extension
- `neura-xxy`: WebSocket market data (depends on neura-4ms)
- `neura-1b6`: Rate limit management (depends on neura-xxy)
- `neura-wiz`: Gamma API wrapper (market discovery)
- `neura-qts`: CLOB API wrapper (order execution)
- `neura-adu`: Data API wrapper (positions/balances)
- `neura-4p6`: Exchange resilience monitoring
- `neura-za8`: Rate limit monitoring

### Security & Encryption
- `neura-yus`: AES-256-GCM encryption
- `neura-c7r`: Key masking in logs/Telegram
- `neura-px6`: Security audits (gosec, gitleaks)
- `neura-9ai`: Intrusion detection

### Quest & Agent System
- `neura-1nz`: ✅ Cron-based quest scheduling (COMPLETED)
- `neura-6tk`: Event-driven quest triggers
- `neura-2iq`: Analyst agent role
- `neura-9m3`: Trader agent role
- `neura-2n4`: Quest state persistence
- `neura-bxg`: One-time auth code → `neura-5of`: Telegram profile binding

### Trading Engine
- `neura-cd1`: Arbitrage trigger detection
- `neura-cha`: Sum-to-one arbitrage skill.md
- `neura-sa4`: Order book imbalance detection
- `neura-we2`: Scalping skill.md codification
- `neura-a7r`: Tight stop-loss execution
- `neura-1wi`: FOK order execution
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

### Telegram Bot Commands
- `neura-hgk`: /begin and /pause handlers
- `neura-ik7`: /summary and /performance handlers
- `neura-ilw`: /liquidate and /liquidate_all handlers
- `neura-09y`: Wallet management commands
- `neura-4gk`: Quest and monitoring commands
- `neura-1p0`: /doctor diagnostic handler

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

## NOTES
- LSP symbol tooling may be unavailable locally (`gopls` missing), so rely on grep/glob/read patterns for discovery.
- Session completion policy in this repository requires push verification (`git status` up-to-date with origin).
