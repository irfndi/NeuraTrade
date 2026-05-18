# NeuraTrade Operations Scripts

Native operations scripts for starting, testing, monitoring, and webhook control.

## Core Scripts

- `startup-orchestrator.sh`
  - Native gateway lifecycle wrapper
  - Usage: `./startup-orchestrator.sh [start|stop|restart|status|logs|help]`

- `health-monitor-enhanced.sh`
  - Runtime health checks for gateway/backend processes + health endpoint
  - Usage: `./health-monitor-enhanced.sh [monitor|check|restart|status|report|verify|help]`

- `test.sh`
  - Unified native test runner for backend/frontend suites
  - Usage: `./test.sh [test|backend|frontend|health|start|status|cleanup|help]`

- `scalping-soak.sh`
  - No-order public-data scalping paper soak with baseline acceptance gates
  - Usage: `SOAK_OUTPUT_FILE="$HOME/.neuratrade/data/scalping-soak-latest.json" ./scalping-soak.sh run`
  - Verify artifact: `./verify-scalping-soak-artifact.sh "$HOME/.neuratrade/data/scalping-soak-latest.json"`
  - Runbook: `../docs/SCALPING_SOAK_ACCEPTANCE.md`

- `webhook-control.sh`
  - Telegram webhook registration + external connection flags
  - Usage: `./webhook-control.sh [enable|disable|status|webhook-register|webhook-unregister|help]`

## Quick Usage

```bash
# Start services
./scripts/startup-orchestrator.sh start

# Verify health
./scripts/health-monitor-enhanced.sh check

# Run tests
./scripts/test.sh test

# Inspect webhook status
./scripts/webhook-control.sh status
```
