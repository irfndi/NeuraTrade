# PLATFORM LAYER KNOWLEDGE BASE

## OVERVIEW
`internal/platform/` provides concurrency primitives with NO business logic: supervisor, actor runtime, eventbus, retry, and timeout utilities.

## STRUCTURE
```text
platform/
├── supervisor/    # Errgroup lifecycle, graceful shutdown
├── actor/         # Bounded mailbox, Ref, System, dead-letter queue
├── eventbus/      # In-process pub/sub (Redis/NATS upgrade path)
├── retry/         # Exponential backoff, circuit breaker helpers
└── timeout/       # Context deadline wrappers
```

## WHERE TO LOOK
| Task | Location | Notes |
|------|----------|-------|
| Supervise goroutines | `supervisor/supervisor.go` | Group, Runnable, Shutdown |
| Actor message passing | `actor/actor.go` | Mailbox, Ref, Send, Ask, Run |
| Event pub/sub | `eventbus/eventbus.go` | Publish, Subscribe, wildcard topics |
| Retry with backoff | `retry/retry.go` | Exponential, jitter |
| Timeout wrappers | `timeout/timeout.go` | IO deadline helpers |

## CONVENTIONS
- All long-running loops go through `supervisor.Group`
- Actors own their state; no shared mutable state across actors
- Every IO has a timeout
- EventBus is in-process; upgrade to Redis/NATS for distributed

## CODE MAP
| Symbol | Type | Location | Role |
|--------|------|----------|------|
| `Supervisor` | struct | `supervisor/supervisor.go` | Lifecycle manager |
| `Group` | struct | `supervisor/supervisor.go` | Errgroup wrapper |
| `Mailbox` | struct | `actor/actor.go` | Bounded message queue |
| `Ref` | struct | `actor/actor.go` | Actor reference |
| `Bus` | struct | `eventbus/eventbus.go` | Pub/sub bus |

## BACKLOG (bd CLI)

**Stats:** 312 total | 64 open | 1 in progress | 14 blocked | 247 closed | 50 ready

## ANTI-PATTERNS
- Naked goroutines without supervisor.
- Holding locks across IO in actor Receive.
- Unbounded channels that cause memory growth.
- EventBus for critical trading signals (use actor messages instead).
