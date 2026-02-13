# Repository Structure Contract

## Scope
This contract defines folder/subfolder/file naming and migration rules for NeuraTrade backend work.
It implements Master Plan section `13.13 Repository Structure and Naming Refactor Governance`.

## Service Boundaries (Source of Truth)
- `services/backend-api`: Go API runtime, routes, services, DB adapters, scripts.
- `services/ccxt-service`: Bun/TypeScript exchange bridge.
- `services/telegram-service`: Bun/TypeScript Telegram bot service.

No cross-service directory reshaping is allowed in a single migration batch.

## Target Directory Structure
```text
services/
├── backend-api/
│   ├── cmd/server/
│   ├── internal/
│   │   ├── api/
│   │   │   ├── handlers/          # Canonical HTTP handlers path
│   │   │   └── routes.go
│   │   ├── services/
│   │   ├── database/
│   │   ├── middleware/
│   │   └── ...
│   ├── scripts/
│   ├── database/
│   └── docs/migration/
├── ccxt-service/
└── telegram-service/
```

## Canonical Path Rules
- Canonical handler path: `services/backend-api/internal/api/handlers/*`.
- Route registration path: `services/backend-api/internal/api/routes.go`.
- New HTTP handler files MUST be created only under `internal/api/handlers`.
- Legacy `internal/handlers/*` is migration-only and must not receive new code.

## Legacy-to-Canonical Map
| Legacy Path | Canonical Path | Migration Policy |
|---|---|---|
| `internal/handlers/*` | `internal/api/handlers/*` | Move in phased batches; keep runtime parity |
| `internal/api/routes.go` import alias `futuresHandlers ".../internal/handlers"` | import from `.../internal/api/handlers` | Remove alias after all moved handlers compile/test green |
| `scripts/coverage-check.sh` package `./internal/handlers` | `./internal/api/handlers` only | Remove legacy package from coverage package list |

## Transitional Import Pattern (Allowed Temporarily)
During migration only, use explicit temporary alias for legacy imports to keep diffs easy to detect:

```go
legacyHandlers "github.com/irfndi/neuratrade/internal/handlers"
```

Rules:
- Alias name must be `legacyHandlers` (not arbitrary aliases).
- New code cannot introduce additional legacy imports.
- Legacy import aliases must be removed by final cleanup phase.

## Naming Conventions
- Go package directories: lowercase, short, domain-specific (`api`, `services`, `middleware`).
- Go files: lowercase snake_case where multi-word (`futures_arbitrage.go`).
- Handler files should map to route/resource domain (`market.go`, `analysis.go`, `exchange.go`).
- Avoid generic buckets (`misc`, `helpers2`, `tmp_handlers`).

## Phased Migration Protocol and Rollback Checkpoints
1. Contract phase: define and approve this document.
2. Phase 1: migrate core handlers to canonical path.
3. Phase 2: migrate trading/stateful handlers.
4. Phase 3: rewire routes/middleware imports to canonical path.
5. Phase 4: remove `internal/handlers` and stale references.
6. Phase 5: full validation, coverage checks, and handoff.

Rollback checkpoints (mandatory at each phase):
- Snapshot baseline: `go test ./...` and coverage report before edits.
- If gate fails, revert phase commit only (no cross-phase rollback).
- Record failure cause + follow-up bead before retry.

## Verification Gates Per Phase
| Phase | Required Checks | Pass Criteria |
|---|---|---|
| Contract | doc review | contract approved and referenced by migration tasks |
| 1-2 | `go test ./internal/api/...` | tests green, no new legacy imports |
| 3 | `go test ./internal/api/... ./cmd/server` | routes compile and startup wiring intact |
| 4 | `go test ./...` + `go mod tidy` | no `internal/handlers` imports remain |
| 5 | `make test` + `make lint` + coverage script | no critical regressions; coverage within accepted threshold |

Coverage baseline policy:
- Regression target: less than 5% drop from pre-migration baseline.
- Track handler coverage specifically for `internal/api/handlers`.

## CI Guardrail Baseline (Banned Patterns)
Block these patterns after Phase 4 completion:
- Import path contains `"github.com/irfndi/neuratrade/internal/handlers"`.
- Coverage package list includes `./internal/handlers`.
- New files created under `services/backend-api/internal/handlers/`.

Recommended CI checks:
- Lint/custom check that fails on banned import pattern.
- Repo grep check in validation workflow for banned path usage.
- Optional pre-commit hook for quick local feedback.

## Execution Ownership
- Architecture owner approves this contract before migration starts.
- Each migration phase must reference this contract in PR/task notes.
- Any exception requires explicit note with rollback plan.
