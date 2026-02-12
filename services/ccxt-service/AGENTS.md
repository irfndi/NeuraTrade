# CCXT SERVICE KNOWLEDGE BASE

## OVERVIEW

`ccxt-service` is a Bun + TypeScript exchange bridge exposing HTTP and gRPC interfaces over CCXT.
It centralizes exchange initialization, market data fetches, admin exchange controls, and funding-rate endpoints.

## WHERE TO LOOK

| Task                      | Location                | Notes                                       |
| ------------------------- | ----------------------- | ------------------------------------------- |
| HTTP API behavior         | `index.ts`              | Hono app routes, admin auth, runtime config |
| gRPC behavior             | `grpc-server.ts`        | Service handlers and response mapping       |
| Shared response types     | `types.ts`              | Request/response typing contracts           |
| Observability integration | `sentry.ts`             | Sentry behavior and current constraints     |
| Proto bindings            | `proto/ccxt_service.ts` | Generated file, do not edit manually        |

## COMMANDS

```bash
bun run dev:bun
bun run build:bun
bun test
```

## CONVENTIONS

- Keep named export style for `app` and avoid Bun auto-serve pitfalls from default export behavior.
- Validate and normalize runtime env vars (ports, admin key) early in startup.
- Admin endpoints must remain gated by `ADMIN_API_KEY`.
- Preserve compatibility for both HTTP and gRPC consumers when changing payload shapes.

## TESTING

- Tests use Bun test runner with module mocking via `test-setup.ts`.
- Prefer deterministic endpoint checks (health, auth boundaries, response shape).

## BACKLOG (bd CLI)

**Stats:** 173 total | 117 open | 67 blocked | 56 closed | 50 ready

### Ready to Work (No Blockers)
- `neura-xxy`: WebSocket market data subscription
- `neura-qts`: CLOB API wrapper for order execution

### Recently Completed (✓)
- ✓ `neura-wiz`: Gamma API wrapper for market discovery (Polymarket)
- ✓ `neura-za8`: Rate limit monitoring and alerting

### WebSocket & Real-time Data (Blocked)
- `neura-4ms`: Extend CCXT wrapper functionality (backend-api integration)

### Rate Limiting & Resilience (Blocked)
- `neura-1b6`: Token bucket rate limit management (depends on neura-xxy)
- `neura-4p6`: Exchange resilience monitoring

### API Wrappers (Blocked)
- `neura-adu`: Data API wrapper for positions/balances

## ANTI-PATTERNS

- Editing generated `proto/*.ts` files directly.
- Returning inconsistent financial value representations across endpoints.
- Bypassing admin auth checks for convenience in production paths.
