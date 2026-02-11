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

## ANTI-PATTERNS
- Running operational scripts without validating env inputs.
- Hardcoding secrets in script invocations/history.
- Editing scripts to bypass health gates instead of fixing underlying service readiness.
