# TELEGRAM SERVICE KNOWLEDGE BASE

## OVERVIEW

`telegram-service` is a Bun + TypeScript bot service using grammY and Hono.
It supports polling/webhook operation, outbound admin send endpoint, and gRPC delivery hooks.

## WHERE TO LOOK

| Task                       | Location                         | Notes                                               |
| -------------------------- | -------------------------------- | --------------------------------------------------- |
| Bot commands and lifecycle | `index.ts`                       | Command handlers, polling/webhook startup, shutdown |
| gRPC delivery interface    | `grpc-server.ts`                 | SendMessage and health RPC handling                 |
| Proto bindings             | `proto/telegram_service.ts`      | Generated output; source in root `protos/`          |
| Test setup/mocking         | `test-setup.ts`, `index.test.ts` | Bun test conventions and API validation             |

## COMMANDS

```bash
bun run dev:bun
bun run build:bun
bun test
```

## CONVENTIONS

- `TELEGRAM_BOT_TOKEN` is mandatory for startup.
- Polling/webhook mode is env-driven (`TELEGRAM_USE_POLLING`, webhook URL/path/secret).
- Admin send endpoint remains protected by `ADMIN_API_KEY`.
- Backend coupling is explicit via `TELEGRAM_API_BASE_URL` and internal API calls.

## TESTING

- Use Bun tests for health/admin/webhook behavior.
- Preserve mock strategy from `test-setup.ts` when adding bot/API flows.

## ANTI-PATTERNS

- Editing generated `proto/*.ts` manually.
- Relaxing webhook secret checks in webhook mode.
- Hardcoding bot secrets or admin keys in source/tests.
