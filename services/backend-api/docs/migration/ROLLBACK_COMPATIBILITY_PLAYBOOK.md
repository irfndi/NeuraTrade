# Rollback and Compatibility Playbook

## Pre-Migration Backup Verification
Use `services/backend-api/scripts/verify-pre-migration-backup.sh` before applying any phase rollback or forward migration.

Example checks:
- `bash services/backend-api/scripts/verify-pre-migration-backup.sh --tag migration-prephase1`
- `bash services/backend-api/scripts/verify-pre-migration-backup.sh --branch backup/migration-phase2`
- `bash services/backend-api/scripts/verify-pre-migration-backup.sh --patch /tmp/migration-phase3.patch`

## Rapid Git Revert Script
Use `services/backend-api/scripts/rollback-migration-phase.sh`.

Modes:
- Dry run (default): prints files and commands.
- Execute mode: apply rollback file restoration/removal.
- Commit revert mode: `--commit <sha>` to run `git revert --no-edit` quickly.

Examples:
- `bash services/backend-api/scripts/rollback-migration-phase.sh phase1`
- `bash services/backend-api/scripts/rollback-migration-phase.sh phase2 --execute`
- `bash services/backend-api/scripts/rollback-migration-phase.sh phase3 --commit <sha>`

## Phase Rollback Procedures

### Phase 1 (core handler migration)
- Scope: canonical migration for futures/core handler path and route wiring.
- Command: `bash services/backend-api/scripts/rollback-migration-phase.sh phase1 --execute`
- Compatibility checkpoints:
  - `go test ./services/backend-api/internal/api/...`
  - `bash services/backend-api/scripts/check-legacy-paths.sh`

### Phase 2 (trading handlers)
- Scope: `trading.go`, trading routes, trading tests, contract yaml.
- Command: `bash services/backend-api/scripts/rollback-migration-phase.sh phase2 --execute`
- Compatibility checkpoints:
  - `go test ./services/backend-api/internal/api/handlers/...`
  - `go test ./services/backend-api/internal/api/...`

### Phase 3 (routing and startup wiring)
- Scope: startup/routing canonical wiring and warning-only degradation.
- Command: `bash services/backend-api/scripts/rollback-migration-phase.sh phase3 --execute`
- Compatibility checkpoints:
  - `go build ./services/backend-api/cmd/server`
  - `go test ./services/backend-api/cmd/server`

### Phase 4 (final cleanup)
- Scope: final cleanup checklist and migration artifacts.
- Command: `bash services/backend-api/scripts/rollback-migration-phase.sh phase4 --execute`
- Compatibility checkpoints:
  - `go test ./services/backend-api/...`
  - `bash services/backend-api/scripts/check-legacy-paths.sh`

## Compatibility Checklist
- Canonical handler package remains `services/backend-api/internal/api/handlers`.
- Route registration remains in `services/backend-api/internal/api/routes.go`.
- No runtime import path references to legacy `internal/handlers`.
- CI/path guardrail script remains available and executable.
- Coverage and validation artifacts regenerate after rollback/forward operations.
