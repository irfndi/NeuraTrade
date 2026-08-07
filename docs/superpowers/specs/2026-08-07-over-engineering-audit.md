# Over-Engineering Audit — 2026-08-07 (parked, not applied)

**Status: MEMO ONLY.** Findings are verified but **no deletions were made**.
Deferred by owner decision (2026-08-07): finish the readiness path first
(soak cohort → real-money gate). Revisit after the cohort completes or when
the soak no longer depends on this tree.

**Scope:** whole repo, over-engineering/complexity only (dead code, unused
flexibility, single-implementation abstractions, wrappers, hand-rolled
stdlib, unused deps). Correctness/security/performance explicitly out of
scope. Method: 3 parallel read-only audits (Bun CLI, Go backend, aux
services) with grep-verified deadness; the two largest claims re-verified.

```
net: ~-17,300 lines, -4 deps possible (ky, @effect/platform-node, @sentry/bun, effect)
```

## Go backend (services/backend-api/) — ~7,200 lines

1. `delete:` `internal/app/bootstrap/` (704) + `internal/adapters/db|redis|telegram` (1,039) + `internal/ports/state|plugin|notifier|policy|orderbook` (790) — one dead bootstrap→ports→adapters architecture, zero importers; `Application.Timeout/Retry/Supervisor/EventBus` write-only. [internal/app/bootstrap/bootstrap.go:126]
2. `delete:` `internal/api/routes_test.go.bak` — 1,119-line stale backup, not a `_test.go`. [internal/api/routes_test.go.bak:1]
3. `delete:` `internal/services/subagent_spawner.go` (806) — referenced only by `_test.go`.
4. `delete:` `internal/services/token_bucket.go` (480) — live limiter is `middleware/ratelimit.go`.
5. `delete:` `internal/tools/executor.go` (465) — no importers.
6. `delete:` `pkg/interfaces/blacklist_cache.go` (397) — duplicated by live `internal/cache/blacklist_cache.go`.
7. `delete:` `internal/services/trading_event_bus.go` (396) — own-test-only; live bus is `internal/platform/eventbus`.
8. `delete:` `pkg/indicators/goflux_adapter.go` (371) — near-verbatim TalibAdapter copy; point 2 call sites at `NewTalibAdapter`.
9. `delete:` `internal/services/workerpool` (306) — no consumers; semaphores in `collector_*.go` handle goroutine limiting.
10. `delete:` `internal/ports/state.go` (254), `internal/ports/plugin.go` (205), `internal/ports/orderbook_repository.go` (62) + `internal/database/orderbook_repository.go` (125), `internal/ports/notifier.go` (150) — consumed only by the dead bootstrap cluster.
11. `delete:` `pkg/interfaces/arbitrage.go` (153), `pkg/interfaces/market_data.go` (140) — unused.
12. `shrink:` `internal/utils/masking.go` — keep `MaskString/MaskEmail/MaskJSON`; drop the other 8 masks (~150).
13. `delete:` `internal/crypto/operator_hasher.go` (118) — test-only; bcrypt covers it.
14. `shrink:` `pkg/interfaces/data_api.go` — keep `Position/PositionStatus`; delete the Interface trio + getters (~150).
15. `yagni:` `internal/ports/policy.go` (119) — one impl each; live code uses concrete types.
16. Smaller: `internal/ccxt/service.go` stub (4); `internal/services/testing_mocks.go` testify mock in production build → `_test.go` (40); `MultiIndicatorStack.AnalyzeSequential` test-only dup of `Analyze` (~65); duplicate AES-GCM `internal/utils/encryption.go` vs `internal/crypto/encryption.go` (~168); dead `Config.GRPC` field + "mtls not yet wired" branch.

## Bun CLI (services/neuratrade-cli-ts/) — ~4,400 lines

 1. `delete:` `src/scalping/portfolio-backtest.ts` (1,429 + test) — production-dead; only importers are its test and the dead config-runner. **Re-verified 2026-08-07.**
 2. `delete:` `src/scalping/strategy-config.ts` (575 + test), `src/scalping/config-runner.ts` (198), `src/utils/env.ts` (122 + test) — mutually-referential dead cluster.
 3. `delete:` `src/exchange/adapters/backend-risk-gated-futures.ts` (367 + 2 tests) — test-only; sole `ky` importer.
 4. `delete:` 6 of 7 `ApiClient` methods (~130; only `health()` live); `SqliteClientLive` initSchema layer (~110); `src/market-data/collector.ts` (84); `assessBacktestRealism` (~66); `Command.run` (29); `cli/gateway.ts makeLayer` (~11 + unused layer imports).
 5. `yagni:` `src/scalping/services.ts` (187) — 4 one-impl Context.Services delegating to pure functions; `Logger` service (~70) — one-impl wrapper over `Effect.log`.
 6. `shrink:` 3 hand-rolled seeded PRNGs (mulberry32 backtest.ts:1844, xorshift32 grid-validation.ts:91, inline LCG expectancy-confidence.ts:55) + pointless `nextState()` alias → one shared helper (~25).
 7. `delete:` `alchemy.run.ts` BITGET_* secret bindings (~17) — worker reads only `watchlist`/`adminKey`. `moneyOrZero` (6). 3 near-identical `scripts/grid-eth-research*.ts` (155) — keep one.
 8. `delete:` deps `ky` (only importer is #19), `@effect/platform-node` (no imports).

## Aux services + scripts — ~5,700 lines

 1. `delete:` `services/telegram-service/api.ts` + `bot-handlers.ts` (+ tests, 1,393) — mutually dead pair.
 2. `delete:` `scripts/neuratrade-cli/neuratrade` (810-line bash CLI, superseded by Go CLI), `scripts/test-autonomous-trading.sh` (564, superseded by `-simple.sh`), 3 of 4 near-identical telegram manual test scripts (432), `scripts/fetch-5yr-real-data.py` (192, superseded by `cmd/fetch-real-candles`).
 3. `delete:` `services/telegram-service/retry.ts` + `telegram-errors.ts` (+ tests, 799) — self-consumed only.
 4. `delete:` `services/telegram-service/sentry.ts` + `sentry-bun.d.ts` (421) + **`@sentry/bun` dep** — zero imports.
 5. `delete:` `TelegramApi` Effect wrapper (`src/api/client.ts:554-958`, ~405) — "Future PRs can convert" never happened; BackendApiClient 36 methods all used.
 6. `delete:` 5 unused gRPC RPCs (`streamEvents`, `sendActionAlert`, `sendQuestProgress`, `sendMilestoneAlert`, `sendRiskEvent`) + `eventStreams` infra + 3 dead templates (~370) — backend calls only `SendMessage` + `HealthCheck`.
 7. `shrink:` `src/config.ts` — `TelegramConfigTag/Live` never consumed; drop double validation (~150) → also **drop `effect` dep**.
 8. `delete:` `services/ccxt/` — stale 12 MB compiled dir (no source/package.json, zero refs; only AGENTS.md tracked). `test-api.ts` (76). `src/commands/upgrade.ts` (18). `index.ts` dead `&& bot`/`if (!bot)` checks (6).
 9. `shrink:` Makefile + `install.sh` duplicate launcher-stub generation (~25); dead Makefile targets `run-ts/install-cli/update-cli`; `scripts/gen-proto.sh` branches targeting the deleted `ccxt-service/`.

## Already applied (this session)

- `worstLowerBoundPct` dead field in readiness stress evidence (grid-validation.ts + CLI fallback + stale comment) — −5 lines, committed with the readiness fixes.

## Recommended deletion order (when un-parked)

1. Go: `routes_test.go.bak` + bootstrap/ports/adapters cluster (items 1–2, 10, 15) — pure dead architecture, biggest single cut.
2. Go: services/tools/pkg leftovers (3–9, 11–14, 16).
3. CLI: dead cluster (17–18) + `ky`/`@effect/platform-node` deps.
4. Aux: telegram dead pair + scripts (25–32) + `@sentry/bun`/`effect` deps.

Each cluster: delete → `go build`/`bun run typecheck` + test suite → commit. Never touch: readiness path (grid-validation, real-money-readiness, grid-candidate, scalp.ts, soak config), live market-data/paper-trading engines, parity replay, the Cloudflare worker.
