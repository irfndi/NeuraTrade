# PROJECT KNOWLEDGE BASE

**Generated:** 2026-02-14 13:51 Asia/Jakarta
**Commit:** c3dc227
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

**Stats:** 193 total | 20 open | 0 in progress | 6 blocked | 173 closed | 14 ready

### Recently Completed (✓)
- ✓ `neura-s9zx`: Stop-Loss Auto-Execution Loop
- ✓ `neura-ve52`: Position Tracker (Real-Time Sync)
- ✓ `neura-1wi`: FOK order execution
- ✓ `neura-7et`: Build prompt builder from skill.md + context
- ✓ `neura-8s4`: Execute Go functions for tool calls
- ✓ `neura-qts`: CLOB API wrapper (order execution)
- ✓ `neura-ur6`: Parse tool_calls from LLM responses
- ✓ `neura-2lv0`: LLM Inference Client - OpenAI/Anthropic/MLX API calls with structured output
- ✓ `neura-acq`: Expose provider/model registry in CLI and Telegram
- ✓ `neura-5nt`: Provider/model catalog source sync and cache
- ✓ `neura-otc`: Codify sentiment momentum skill.md
- ✓ `neura-lnm`: Redis job queue
- ✓ `neura-1p0`: /doctor diagnostic handler
- ✓ `neura-4gk`: Quest and monitoring commands
- ✓ `neura-09y`: Wallet management commands
- ✓ `neura-ilw`: /liquidate and /liquidate_all handlers
- ✓ `neura-ik7`: /summary and /performance handlers
- ✓ `neura-hgk`: /begin and /pause handlers
- ✓ `neura-tm8`: Store skill hashes for version tracking
- ✓ `neura-20g`: Implement skill.md file loader
- ✓ `neura-6md`: Cost tracking in ai_usage table
- ✓ `neura-32w`: Fund with minimal capital (USDC)
- ✓ `neura-9ai`: Intrusion detection
- ✓ `neura-nqxe`: Fix undefined talib and snapshots in technical_analysis.go
- ✓ `neura-hrs3`: Fix undefined CCXTClient in services package
- ✓ `neura-q82t`: Fix CI/CD failures on PR#102
- ✓ `neura-aunx`: Add RiskHandler test coverage
- ✓ `neura-thu1`: Add AutonomousHandler test coverage
- ✓ `neura-0ty7`: Add DailyLossTracker test coverage
- ✓ `neura-m1gv`: Add OTPService test coverage
- ✓ `neura-nq9l`: Add APIKeyService test coverage
- ✓ `neura-l70a`: Refactor talib wrapper to goflux
- ✓ `neura-zn8c`: Replace in-memory state with persistent storage
- ✓ `neura-8y8`: Emergency rollbacks
- ✓ `neura-za8`: Rate limit monitoring
- ✓ `neura-px6`: Security audits (gosec, gitleaks)
- ✓ `neura-4p6`: Exchange resilience monitoring
- ✓ `neura-kxq`: Kill switch monitoring
- ✓ `neura-q6o`: Containerize agent and infra services
- ✓ `neura-8de`: Virtual account tracking
- ✓ `neura-94c`: /status budget display
- ✓ `neura-7mj`: Monthly budget enforcement
- ✓ `neura-5z3`: Daily budget enforcement
- ✓ `neura-txu`: Controlled liquidation tool
- ✓ `neura-2xe`: Position snapshot tool
- ✓ `neura-wz7`: cancel_order tool endpoint
- ✓ `neura-l2z`: place_order tool endpoint
- ✓ `neura-axx`: Action streaming format
- ✓ `neura-cha`: Sum-to-one arbitrage skill.md
- ✓ `neura-we2`: Scalping skill.md codification
- ✓ `neura-duw`: Expose cleanup endpoints
- ✓ `neura-1s5`: Risk primitives
- ✓ `neura-lue`: Readiness endpoints
- ✓ `neura-3ms`: Position-size throttle
- ✓ `neura-bol`: Consecutive-loss pause
- ✓ `neura-q4j`: Max drawdown halt
- ✓ `neura-e8u`: Daily loss cap
- ✓ `neura-161`: Distributed locks (Redis)
- ✓ `neura-byz`: Goroutine pool with concurrency limits
- ✓ `neura-3b9`: Priority levels (CRITICAL > HIGH > NORMAL > LOW)
- ✓ `neura-2n4`: Quest state persistence
- ✓ `neura-6tk`: Event-driven quest triggers
- ✓ `neura-1nz`: Cron-based quest scheduling
- ✓ `neura-kpu`: Risk Manager agent role
- ✓ `neura-9m3`: Trader agent role
- ✓ `neura-2iq`: Analyst agent role
- ✓ `neura-r1d`: Progressive disclosure system
- ✓ `neura-47g`: AI provider registry with models.dev
- ✓ `neura-c7r`: Key masking in logs/Telegram
- ✓ `neura-yus`: AES-256-GCM encryption for API keys
- ✓ `neura-wiz`: Gamma API wrapper (Polymarket)
- ✓ `neura-xxy`: WebSocket market data subscription
- ✓ `neura-4ms`: CCXT wrapper extension
- ✓ `neura-06k`: Health checks for Redis, SQL storage, exchange bridges
- ✓ `neura-l1l`: Balance/funding validation
- ✓ `neura-yzv`: CLI bootstrap command
- ✓ `neura-5of`: Bind local operator profile to Telegram chat
- ✓ `neura-aav`: Connectivity checks for all configured providers
- ✓ `neura-fs8`: API key validation (permissions)
- ✓ `neura-myb`: Wallet minimum checks
- ✓ `neura-bri`: Implement AI reasoning summaries
- ✓ `neura-mm5`: Implement paper trade recording
- ✓ `neura-u4w`: Implement paper execution simulation
- ✓ `neura-bxg`: One-time auth code generation
- ✓ `neura-cd1`: Arbitrage trigger detection
- ✓ `neura-lk1`: Create install.sh shell script
- ✓ `neura-uxq`: Add .env template generation
- ✓ `neura-wiz`: Implement Gamma API wrapper (Polymarket)
- ✓ `neura-ydp`: Add operator identity encryption (Argon2)
- ✓ `neura-yns`: Create SQLite schema with all core tables

## NOTES
- LSP symbol tooling may be unavailable locally (`gopls` missing), so rely on grep/glob/read patterns for discovery.
- Session completion policy in this repository requires push verification (`git status` up-to-date with origin).
