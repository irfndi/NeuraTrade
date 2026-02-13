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

## BACKLOG (bd CLI)

**Stats:** 179 total | 98 open | 58 blocked | 81 closed | 40 ready

### Ready to Work (No Blockers)

- `neura-5of`: Bind local operator profile to Telegram chat

### Recently Completed (✓)

- ✓ `neura-ydp`: Operator identity encryption (Argon2)
- ✓ `neura-2iq`: Analyst agent role Telegram integration
- ✓ `neura-6tk`: Event-driven quest trigger notifications
- ✓ `neura-3b9`: Priority levels (CRITICAL > HIGH > NORMAL > LOW)
- ✓ `neura-hgk`: /begin and /pause autonomous mode handlers
- ✓ `neura-ik7`: /summary and /performance report handlers
- ✓ `neura-ilw`: /liquidate and /liquidate_all handlers
- ✓ `neura-09y`: Wallet management commands (/wallet, /balance)
- ✓ `neura-4gk`: Quest and monitoring commands (/quest, /monitor)
- ✓ `neura-1p0`: /doctor diagnostic handler
- ✓ `neura-nh5`: Risk event notification delivery
- ✓ `neura-axx`: Action streaming format messages

### Notifications & Alerts (Blocked)

- `neura-im9`: Quest progress update notifications
- `neura-fvk`: Fund milestone alert delivery
- `neura-bri`: AI reasoning summary messages

### Security & Privacy (Blocked)

- `neura-c7r`: API key masking in Telegram output

## ANTI-PATTERNS

- Editing generated `proto/*.ts` manually.
- Relaxing webhook secret checks in webhook mode.
- Hardcoding bot secrets or admin keys in source/tests.
