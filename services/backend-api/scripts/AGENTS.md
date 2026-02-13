# SCRIPTS KNOWLEDGE BASE

## OVERVIEW
`scripts/` contains operational tooling for startup orchestration, health checks, environment validation, webhook control, migration helpers, and diagnostics.

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Sequential service startup | `startup-orchestrator.sh` | Phased startup + health gating |
| Container boot lifecycle | `entrypoint.sh` | Environment normalization + child process supervision |
| Health monitoring/recovery | `health-monitor-enhanced.sh` | Continuous checks and restart actions |
| Environment validation | `validate-env.sh`, `verify-env-sync.sh` | Required var checks and drift detection |
| Telegram diagnostics | `diagnose-telegram-bot.sh`, `set-telegram-webhook.sh`, `webhook-control.sh` | Bot/webhook triage and control |
| Test orchestration | `test.sh`, `coverage-check.sh` | Local/CI test workflows and thresholds |

## CONVENTIONS
- Most operational scripts use strict shell mode (`set -euo pipefail`) for safety.
- Scripts assume Docker runtime availability for full-stack workflows.
- Prefer explicit preflight checks before mutating state (connectivity, env var presence, health).
- Use colorized, timestamped logs to keep long runs debuggable.

## GOTCHAS
- Some scripts expect specific container naming conventions.
- `entrypoint.sh` may normalize env vars from multiple provider formats; verify resolved values when debugging.
- Port-conflict remediation may terminate existing processes; check local dev impact first.
- Migration helper scripts differ between host and container contexts.

## SAFE USAGE PATTERN
```bash
bash scripts/validate-env.sh
bash scripts/verify-env-sync.sh
bash scripts/startup-orchestrator.sh
bash scripts/health-monitor-enhanced.sh check
```

## BACKLOG (bd CLI)

**Stats:** 179 total | 98 open | 58 blocked | 81 closed | 40 ready

### Recently Completed (✓)
- ✓ `neura-yzv`: NeuraTrade CLI bootstrap command
- ✓ `neura-px6`: Security audit scripts (gosec, gitleaks integration)
- ✓ `neura-za8`: Rate limit monitoring dashboard scripts
- ✓ `neura-1p0`: /doctor diagnostic automation scripts
- ✓ `neura-9ai`: Intrusion detection monitoring scripts
- ✓ `neura-lue`: Enhanced readiness check scripts
- ✓ `neura-06k`: Health checks scripts

### Infrastructure & Deployment (Blocked)
- `neura-354`: CI/CD pipeline setup scripts
- `neura-qfp`: Production Docker Compose configuration
- `neura-wqa`: QuantVPS deployment scripts
- `neura-q6o`: Containerize agent and infrastructure services

### Security & Monitoring (Blocked)
- `neura-kxq`: Kill switch monitoring and activation scripts
- `neura-8y8`: Emergency rollback procedures

### Health & Operations (Blocked)
- `neura-4p6`: Exchange resilience monitoring scripts

## ANTI-PATTERNS
- Running operational scripts without validating env inputs.
- Hardcoding secrets in script invocations/history.
- Editing scripts to bypass health gates instead of fixing underlying service readiness.
