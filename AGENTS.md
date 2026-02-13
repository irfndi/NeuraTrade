# PROJECT KNOWLEDGE BASE

**Generated:** 2026-02-13 13:41 Asia/Jakarta
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

**Stats:** 184 total | 9 open | 5 in progress | 0 blocked | 107 closed | 72 ready

### In Progress (5)
- `neura-1wi`: FOK order execution
- `neura-7et`: Build prompt builder from skill.md + context
- `neura-8s4`: Execute Go functions for tool calls
- `neura-qts`: CLOB API wrapper (order execution)
- `neura-ur6`: Parse tool_calls from LLM responses

### Ready to Work (72)
- `neura-1b6`: Rate limit management with token bucket
- `neura-354`: Set up CI/CD pipeline
- `neura-4eo`: Expose arbitrage primitives
- `neura-a7r`: Implement tight stop-loss execution
- `neura-adu`: Implement Data API wrapper (positions/balances)
- `neura-fvk`: Implement fund milestone alerts
- `neura-im9`: Implement quest progress updates
- `neura-nh5`: Implement risk event notifications
- `neura-ntk`: Implement round-robin debate loop
- `neura-qfp`: Configure production Docker Compose
- `neura-sa4`: Implement order book imbalance detection
- `neura-wqa`: Set up QuantVPS deployment
- `neura-117`: Optimize database query performance
- `neura-1cf`: Optimize Redis caching strategy
- `neura-1ol`: Implement historical OHLCV replay
- `neura-2lv0`: Implement LLM Inference Client - OpenAI/Anthropic/MLX API calls with structured output
- `neura-3y5`: Implement slow query logging
- `neura-4g6`: Implement log aggregation
- `neura-53k`: Implement signal explanation generation
- `neura-5jj`: Implement automatic capital scaling
- `neura-5ll`: Implement phase-specific strategy adaptation
- `neura-5nt`: Implement provider/model catalog source sync and cache
- `neura-6ws`: Simulate AI decision loop
- `neura-8cv`: Implement watchdog filtering
- `neura-8on`: Implement Redis message subscription
- `neura-8sk`: Add provider selection config
- `neura-8x7`: Implement Go ↔ mlx_lm.server HTTP communication
- `neura-8xg`: Implement session lifecycle management
- `neura-9i4`: Implement session resumption
- `neura-acq`: Expose provider/model registry in CLI and Telegram
- `neura-aod`: Set up alerts
- `neura-bnv`: Implement bootstrap → growth → scale → mature transitions
- `neura-d3r`: Add goflux as dependency
- `neura-dem`: Create get_portfolio skill.md
- `neura-do1`: Define IndicatorProvider interface
- `neura-dtj`: Implement existing provider adapter
- `neura-dvl`: Implement GoFlux adapter
- `neura-dzq`: Output performance metrics
- `neura-eb8`: Serialize AI session state
- `neura-emr`: Implement multi-indicator stack
- `neura-gbd`: Create place_order skill.md
- `neura-h2p`: Store backtest results
- `neura-i3d`: Implement quantized model loading
- `neura-ir1`: Implement dashboards
- `neura-jqj`: Implement cohort-level flow analysis
- `neura-kq5`: Add parity tests between providers
- `neura-mjy`: Create operator guide
- `neura-n7l`: Define market data channels
- `neura-ov0`: Create security documentation
- `neura-sf3z`: Implement Agent Execution Loop - Wire AnalystAgent → LLM → Tool Calls → RiskManagerAgent → Execution
- `neura-t7o`: Implement AI model router policy engine from registry
- `neura-x02`: Create troubleshooting guide
- `neura-xtc`: Implement anti-manipulation filters
- `neura-xy2`: Implement shadow mode execution
- `neura-yk2`: Create get_price skill.md
- `neura-z5l`: Implement Redis message publishing
- `neura-zov`: Implement Twitter sentiment analysis
- `neura-0lu`: Progress Tracking
- `neura-1t2`: EPIC 4: PRODUCTION (Week 7+)
- `neura-6vm`: EPIC 2: AGENTIC CORE (Weeks 3-4)
- `neura-87l`: Prototype optional QMD retrieval for /doctor runbooks
- `neura-b52`: COMMAND-READY TASK CREATION
- `neura-bdb`: Next Steps
- `neura-cfo`: Architecture Constraints (LOCKED)
- `neura-kxi`: EPIC 1: FOUNDATION (Weeks 1-2)
- `neura-l0s`: TECHNICAL DEPENDENCY MATRIX
- `neura-sbh`: EPIC 3: STRATEGY & UI (Weeks 5-6)
- `neura-srd`: Add feature flag + fallback path for QMD integration
- `neura-znk`: Increase test coverage to 80%
- `neura-9zc2`: Implement News Sentiment Feed - Integrate CryptoPanic/news aggregator for sentiment scoring
- `neura-wjr8`: Implement Reddit Sentiment Integration - Fetch r/CryptoCurrency, r/Bitcoin sentiment via Reddit API
- `neura-y146`: Implement Performance Feedback Pipeline - Trade outcomes → metrics → strategy parameter adjustment

### Recently Completed (✓)
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
