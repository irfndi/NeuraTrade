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

## SCOPED GUIDES
- `internal/api/AGENTS.md`
- `internal/services/AGENTS.md`
- `database/AGENTS.md`
- `scripts/AGENTS.md`
