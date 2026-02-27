# NeuraTrade Testing Strategy and QA Gates

## Objective

Ship every Master Plan task with production-grade verification and traceable evidence.

## Required Test Layers Per Task

Every task must declare and execute:

1. Unit tests
2. Integration tests
3. E2E or smoke tests

If a layer is not applicable, record explicit rationale in the task evidence.

## Coverage Policy

- Global CI coverage gate: >= 80% (strict)
- Touched package target: >= 85%
- Risk-critical paths (risk engine, execution, auth, encryption, migrations):
  - Unit + integration are mandatory
  - E2E/smoke evidence is mandatory

## CI Enforcement

- `make ci-check` now includes strict coverage gating.
- Validation workflow exports:
  - `STRICT=true`
  - `MIN_COVERAGE=80`

## Beads Closure Gate

Use the QA closure wrapper so evidence is stored before close:

```bash
ISSUE_ID=<bd-id> \
UNIT_TESTS="..." \
INTEGRATION_TESTS="..." \
E2E_TESTS="..." \
COVERAGE_RESULT="..." \
EVIDENCE="..." \
make bd-close-qa
```

This records `QA_GATE` evidence in bd notes and then closes the issue.

## Pull Request Requirements

PRs must include:

- Why and implementation summary
- Unit/integration/e2e or smoke commands run
- Coverage evidence
- Risk and rollback plan
- Linked bd issue IDs with updated acceptance + test evidence

## Recommended Command Set

```bash
make test
make coverage-check
go test -race ./services/backend-api/...
bun test --cwd services/ccxt-service
bun test --cwd services/telegram-service
```
