# NeuraTrade — Agent Knowledge Base

> Read this first. It is intentionally short. It assumes you know Go, the `make` command line, and the `bd` issue tracker. Anything you cannot verify here, grep/Read in the source of truth.

> **Shell commands**: this project uses [RTK](RTK.md) (Rust Token Killer). When executing shell commands, prefix them with `rtk` (e.g., `rtk git status`, `rtk go test ./...`) so output is token-efficient. If a command is not recognized by RTK it passes through unchanged.

## OVERVIEW

NeuraTrade is a multi-service crypto trading platform. The runtime is native (no Docker required). SQLite is the default database; Redis is optional and non-fatal if absent.

- `services/backend-api/` — Go. Main HTTP server, domain logic, DB, telemetry, ops scripts.
- `services/agent-control/` — Go. Standalone agent control plane (playbooks, policy, audit).
- `services/telegram-service/` — Bun + grammY + Hono. Telegram bot + gRPC delivery.
- `services/ccxt/` — Bun. **Stub by default.** The real CCXT integration is embedded in `services/backend-api/internal/ccxt/`. CCXT runs as a separate process only when `CCXT_SERVICE_URL` or `CCXT_GRPC_ADDRESS` is set.
- `cmd/neuratrade-cli/` — Go. The `neuratrade` gateway CLI. **It is a process manager, not the backend.** It spawns `neuratrade-server` + (optionally) `telegram-service` as child processes and manages PIDs under `NEURATRADE_HOME`.
- `protos/` — Shared protobuf contracts (`ccxt_service.proto`, `telegram_service.proto`). Generated artifacts live in `services/backend-api/pkg/pb/` and `services/*/proto/`.

## ARCHITECTURE — THE TRADING PIPELINE

This is the single most important flow. Internalize it.

```
MarketTick
   → CollectorActor (marketdata/)
   → StrategyActor  (strategy/)            emits SignalProposed
   → RiskActor      (risk/)                final safety gate — PolicyEngine + kill switch
   → ExecutionActor (execution/)           places order via TradingGateway adapter
   → PortfolioActor (portfolio/)           emits PositionUpdated
```

**Hard invariants:**

- `RiskActor` is the only path to `ExecutionActor`. Bypassing it is a P0 safety bug.
- `supervisor.Group` (`internal/platform/supervisor/`) wraps every long-running loop. Naked goroutines are forbidden.
- Every IO has a context timeout. Silent retries are forbidden — every retry must emit a metric/log.
- The In-process `EventBus` (`internal/platform/eventbus/`) is for control-plane fanout. Do **not** use it for trading-critical signals — use actor messages.
- Money math uses `shopspring/decimal`. `float64` for monetary values is a P0 bug.
- Migrations are additive and forward-only. Never rewrite historical migrations; add a new one.

See `services/backend-api/internal/app/AGENTS.md` and `services/backend-api/internal/platform/AGENTS.md` for the full rule set.

## WHERE TO LOOK

| Task | Location | Notes |
|------|----------|-------|
| Process entrypoint, startup order, shutdown | `services/backend-api/cmd/server/main.go` | Explicit constructor injection. ~750 LOC. Read this first when debugging boot. |
| Route registration + middleware | `services/backend-api/internal/api/routes.go` | Health at root; API at `/api/v1`; auth/admin middleware boundaries. |
| Domain logic | `services/backend-api/internal/services/` | Largest code area. ~200+ files including 180K-line `ai_scalping.go`. |
| Actor pipeline (collector→strategy→risk→execution→portfolio) | `services/backend-api/internal/app/` | One package per actor. |
| Concurrency primitives | `services/backend-api/internal/platform/` | supervisor, actor, eventbus, retry, timeout. |
| DB connection + migrations | `services/backend-api/internal/database/` + `services/backend-api/database/` | Two migrate scripts: `migrate.sh` (Postgres/psql) and `sqlite-migrate.sh` (SQLite CLI). |
| Gateway CLI commands | `cmd/neuratrade-cli/main.go` + `gateway.go` | `gateway start|stop|status` lives in `gateway.go`. |
| Telegram bot | `services/telegram-service/index.ts` | grammY polling/webhook + Hono HTTP + gRPC. |
| Agent control plane | `services/agent-control/cmd/agent/main.go` | Standalone; not launched by gateway CLI. |
| Native ops scripts | `services/backend-api/scripts/` | `startup-orchestrator.sh`, `health-monitor-enhanced.sh`, `coverage-check.sh`, `bd-close-with-qa.sh`, `validate-env.sh`. |
| Recovery / scalping tunables | README "Recovery Tuning" section | `NEURATRADE_RECOVERY_*`, `NEURATRADE_SCALPING_*` env vars. |
| Proto sources | `protos/*.proto` | Regenerate via `make proto-gen` (manual, requires `protoc` + plugins). |

## CONVENTIONS

- **No DI container.** Services are wired explicitly in `cmd/server/main.go` with constructor injection.
- **Handlers are thin.** Parse, validate, call service. No DB queries in handlers — use repositories.
- **Tests are co-located** (`*_test.go`). Integration coverage in `test/integration/`, e2e in `test/e2e/`. `internal/testutil/` and `test/testmocks/` hold shared setup.
- **Backend code is the only thing that owns secrets and exchange credentials.** Telegram/CCXT services never see API keys.
- **Bun services gate admin endpoints on `ADMIN_API_KEY`.** When unset, admin endpoints are disabled (not just unauthorized).
- **Migrations** use `NNN_descriptive_name.sql` naming, are zero-padded, run in `sort -V` order, and should be idempotent (`IF NOT EXISTS`). Backups before migration: `verify-pre-migration-backup.sh`.
- **Commit style:** Conventional Commits. Reference `bd` issue IDs in bodies.
- **Beads (bd) is the only task tracker.** Do not create markdown TODO lists. Use `bd ready`, `bd show`, `bd update`, `bd close`. See `bd prime` for full workflow.

## ANTI-PATTERNS

- Editing generated proto code (`*.pb.go`, `services/*/proto/*.ts`) directly — regenerate with `make proto-gen`.
- Adding handlers under legacy `services/backend-api/internal/handlers/` — the active path is `internal/api/handlers/`.
- Using `float64` for monetary values anywhere in domain logic. Use `decimal.Decimal`.
- Coupling handler response shaping to repository SQL.
- Introducing direct DB queries in services that already have a repository abstraction.
- Bypassing `RiskActor` with direct adapter calls from non-risk actors.
- Silent retries without metrics/logging.
- Unbounded channels; holding locks across IO; naked goroutines outside `supervisor.Group`.
- Renumbering existing migrations after they have shipped.
- Committing secrets, `.env`, or `*.db` files (already in `.gitignore`, but be explicit).
- Hardcoding tunables — use `NEURATRADE_*` env vars, not literal constants in code.

## COMMANDS

The Makefile is the canonical command surface. There is **no `dev-setup` or `dev-down`** — all tests run against SQLite in a temp `NEURATRADE_HOME`.

```bash
make build              # Build all 11 binaries to bin/
make run                # Build then `./bin/neuratrade gateway start`
make dev                # Hot reload via air (requires `go install github.com/air-verse/air@latest`)

make test               # Backend Go tests (SQLite, -race, 20m) + operational shell script tests
make test-backend       # Go unit + integration + e2e
make test-frontend      # Bun tests for telegram-service
make test-scripts       # 5 shell-based script tests (no Go toolchain needed)

make fmt                # gofmt + prettier
make fmt-check          # CI-safe formatting check
make lint               # golangci-lint v2.10.1 + oxlint
make typecheck          # tsc for telegram-service
make coverage-check     # 77% threshold; STRICT=false locally, STRICT=true in CI

make proto-gen          # Regenerate Go + TS proto code (requires protoc + plugins)
make logs               # tail $NEURATRADE_HOME/logs/backend.log
make logs-all           # tail $NEURATRADE_HOME/logs/gateway.log
make scalping-soak      # No-order public-data scalping paper soak
make ai-scalping-probe  # Real-LLM no-order scalping probe with recovery gates

make bd-close-qa        # Close a bd issue with mandatory QA evidence (see below)
```

### `make bd-close-qa` — required env vars

Closing a `bd` issue through the QA gate requires ALL of these (the script rejects `E2E_TESTS=N/A`):

```bash
ISSUE_ID=bd-123 \
UNIT_TESTS="go test ./internal/services/... pass" \
INTEGRATION_TESTS="go test ./test/integration/... pass" \
E2E_TESTS="smoke test against running gateway" \
COVERAGE_RESULT="78.2% on touched packages" \
EVIDENCE="logs: $NEURATRADE_HOME/logs/backend.log" \
make bd-close-qa
```

## TESTING REALITY

- **Database**: SQLite (`DATABASE_DRIVER=sqlite` is the default in `internal/config/config.go` setDefaults, `.env.example`, the Makefile test target, and CI). Postgres is an alternative via Docker (`docker-compose.yml`) or `DATABASE_DRIVER=postgres` — not the default.
- **Redis**: Optional. Backend falls back to in-memory cache/queue if Redis is unreachable. CI does not spin up Redis.
- **Coverage gate**: `MIN_COVERAGE=77` (`services/backend-api/scripts/coverage-check.sh`). `STRICT=false` by default locally (warning), `STRICT=true` in CI (hard fail). Max coverage delta from baseline: 5%.
- **Race detector**: Always on (`go test -race`). Use it for any new concurrent code.
- **Targeted tests**: `go test ./internal/services/...` or `go test ./internal/api/handlers/... -run TestX`.

## CI WORKFLOWS (`.github/workflows/`)

| File | Triggers | What it does |
|------|----------|--------------|
| `validation.yml` | push to main/develop/development, PRs | 6 parallel jobs: backend fmt+lint, frontend fmt+lint+build, backend tests (SQLite, -race), frontend tests, backend security (gitleaks+gosec+govulncheck+trivy), frontend security (bun audit) |
| `autofix.yml` | PR opened/sync/reopened | Auto-formats with gofmt+goimports+golangci-lint --fix+prettier+oxlint+shfmt, auto-commits via `autofix-ci[bot]` |
| `codspeed.yml` | push to main/develop/development, PRs | Benchmarks on `test/benchmark/`, `internal/crypto/`, `internal/utils/`, `internal/database/` |
| `test-ccxt-native.yml` | push | Lightweight `make test-scripts` (no Go/Bun toolchain) |
| `backend-security-deep.yml` | push to backend paths, daily 02:37 UTC, manual | Bounded gosec taint scan on decomposed `internal/services/*` subpackages |

**Required secret for CI** (Telegram tests): `ADMIN_API_KEY`, `TELEGRAM_WEBHOOK_SECRET`. If missing, CI generates ephemeral values and logs a notice.

## RUNTIME LAYOUT

`NEURATRADE_HOME` defaults to `~/.neuratrade`. Override with the env var. The gateway CLI and `startup-orchestrator.sh` create and manage this layout:

```
$NEURATRADE_HOME/
├── run/          # backend.lock (flock)
├── pids/         # backend.pid, telegram.pid, ccxt.pid, gateway.pid, gateway-state.json
├── logs/         # backend.log, telegram.log, gateway.log, health-monitor.log
└── data/         # neuratrade.db (SQLite) + data artifacts
```

Stop with `./bin/neuratrade gateway stop` (sends SIGTERM via PID files). Health probe: `curl -s http://localhost:8080/health`.

## SCOPED GUIDES

These contain directory-specific detail. Read the one nearest to your change:

- `services/backend-api/AGENTS.md` — backend overview + scoped index
- `services/backend-api/internal/api/AGENTS.md` — routes, handlers, middleware
- `services/backend-api/internal/services/AGENTS.md` — domain service map, testing patterns
- `services/backend-api/internal/platform/AGENTS.md` — concurrency primitives
- `services/backend-api/internal/app/AGENTS.md` — actor pipeline + event flow
- `services/backend-api/database/AGENTS.md` — migration rules
- `services/backend-api/scripts/AGENTS.md` — ops script inventory
- `services/telegram-service/AGENTS.md` — Bun bot env vars + tests
- `services/ccxt/AGENTS.md` — CCXT stub (most logic lives in `backend-api/internal/ccxt/`)
- `services/agent-control/AGENTS.md` — autonomous playbooks, policy, audit

## NOTES

- `gopls` may be missing locally. Rely on grep/glob/Read + AST-grep rather than LSP symbol queries.
- `services/backend-api/internal/services/ai_scalping.go` is 180K LOC. Touch with care; prefer adding focused test cases over rewriting.
- `services/backend-api/internal/services/ai_scalping_test.go` is 140K+ LOC — large existing suites, add cases rather than splitting.
- The CCXT service is a stub (`bin/ccxt-service` is a 5-line bash script). Real CCXT integration is in `services/backend-api/internal/ccxt/`. Don't add features to the stub.
- `internal/services` is undergoing decomposition (see `backend-security-deep.yml` header). Touching it triggers gosec taint exceptions; expect CI TODOs about `G701–G707` exclusions.

## SESSION WORKFLOW (bd + landing the plane)

This project uses **bd (beads)** for ALL issue tracking. Do not use markdown TODOs.

### During the session

```bash
bd ready                            # Show unblocked work
bd show <id>                        # Read the issue + dependencies
bd update <id> --status in_progress # Claim atomically
bd create "..." -t task -p 2        # Discover new work; link with --deps discovered-from:<id>
```

When you find a bug or follow-up while implementing, create a linked issue immediately. Do not lose it in a scratch file.

### Closing work — QA gate is mandatory

`bd close <id>` alone will fail validation if the QA evidence isn't attached. Use:

```bash
ISSUE_ID=neura-XXXX UNIT_TESTS=... INTEGRATION_TESTS=... E2E_TESTS=... COVERAGE_RESULT=... EVIDENCE=... \
  make bd-close-qa
```

All five QA fields are required. Bare `N/A` for E2E is rejected.

### Ending the session — landing the plane

Work is **not complete** until `git push` succeeds and `bd` is synced.

```bash
git status                          # Review what changed
git add <files>                     # Stage intentionally; never -A
bd sync --from-main                 # Pull beads updates from main
git commit -m "..."                 # Conventional commit
git push                            # Must succeed
git status                          # MUST show "up to date with origin"
```

The push is the contract. "Ready to push" is not done.

<!-- BEGIN BEADS INTEGRATION -->
## bd — quick reference

- `bd ready` / `bd blocked` — pick work
- `bd show <id>` — inspect + dependencies
- `bd update <id> --status in_progress` — claim
- `bd create "Title" -t bug|feature|task -p 0-4 --deps discovered-from:<id>` — link discovered work
- `bd close <id> --reason "..."` — only valid AFTER `make bd-close-qa`
- Priorities: `0` critical, `1` high, `2` medium (default), `3` low, `4` backlog
- All commands accept `--json` for programmatic use

See `README.md` and `docs/QUICKSTART.md` for full bd workflow.
<!-- END BEADS INTEGRATION -->

## WORKING PRINCIPLES

- Do not preserve backward compatibility. Remove obsolete paths instead of adding compatibility layers, fallbacks, or migrations.
- Choose the simplest implementation that fully meets the current requirements. Avoid speculative abstractions, configuration, and indirection.
- Grow the system in layers. Start from the smallest version that works end to end, and add each new capability on top of a product that already works. Never trade a working product for unfinished complexity.
- Keep components modular and concerns clearly separated.
- Prefer established, well-maintained libraries when they reduce overall complexity or improve reliability. Do not reimplement common functionality without a clear reason.
- Lean on the dependencies already in the project before writing your own implementation or adding packages. Do not assume a library lacks a capability without checking its documentation and types.
- Make architectural decisions for the long term. Do not accept a stopgap that only works for now and is meant to be replaced later.
