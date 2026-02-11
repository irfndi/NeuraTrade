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

## ANTI-PATTERNS

- Editing generated `proto/*.ts` files directly.
- Returning inconsistent financial value representations across endpoints.
- Bypassing admin auth checks for convenience in production paths.
