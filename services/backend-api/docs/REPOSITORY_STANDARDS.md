# Repository Standards

## Canonical Handler Path
- Allowed: `services/backend-api/internal/api/handlers/*`
- Banned: `services/backend-api/internal/handlers/*`

## Banned Patterns
- Import path: `github.com/irfndi/neuratrade/internal/handlers`
- Path segment in source imports: `internal/handlers`
- Non-snake_case Go file names (must match `^[a-z0-9_]+\.go$`)

## CI Guardrails
- Legacy path guard: `services/backend-api/scripts/check-legacy-paths.sh`
- Naming/import guard: `services/backend-api/scripts/check-canonical-naming.sh`
- Lint import deny rule in `services/backend-api/.golangci.yml` via `depguard`

## Pre-Commit Feedback
- Repo hook script: `.githooks/pre-commit`
- Install once per clone:
  - `git config core.hooksPath .githooks`

The pre-commit hook runs canonical path and naming checks before local commits.
