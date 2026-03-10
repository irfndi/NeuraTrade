# AGENT CONTROL PLANE KNOWLEDGE BASE

## OVERVIEW
`services/agent-control/` is the Level-4 autonomous AI trading control plane. It consumes events, runs playbooks, and issues safe commands to the execution plane.

## STRUCTURE
```text
agent-control/
├── cmd/                                # CLI entrypoint
├── agent_runtime.go                    # Core agent orchestration
├── audit.go                            # Action audit logging
├── client.go                           # Backend API client
├── ingest.go                           # Event ingestion from eventbus
├── playbooks.go                        # Self-heal and degradation playbooks
├── policy.go                           # Agent permission policies
└── *_test.go                           # Tests
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Agent orchestration | `agent_runtime.go` | Quest scheduling, state management |
| Event ingestion | `ingest.go` | Subscribe to market/risk events |
| Playbook execution | `playbooks.go` | Self-heal, pause exchange, rollback |
| Permission checks | `policy.go` | What agent can do automatically |
| Audit trail | `audit.go` | Log every agent action |

## CONVENTIONS
- Agent actions are always logged for audit
- Policy gates prevent unsafe autonomous operations
- Playbooks are deterministic and idempotent
- Backend communication via `client.go`

## BACKLOG (bd CLI)

**Stats:** 312 total | 64 open | 1 in progress | 14 blocked | 247 closed | 50 ready

## ANTI-PATTERNS
- Allowing agent to bypass risk policy.
- Autonomous actions without audit logging.
- Non-idempotent playbooks that cause side effects on retry.
