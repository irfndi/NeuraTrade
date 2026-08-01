# Futures Scalping: Effect v4 Migration + Bitget Readiness — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `services/neuratrade-cli-ts/` a fully Effect v4 codebase whose futures scalping passes measurable readiness gates on Bitget, validated by backtest + demo paper trading.

**Architecture:** Bun + `effect@4` (pinned beta). Hand-rolled CLI kit replaces `@effect/cli` (no v4 release exists). Strategy/backtest core converted from plain TS to Effect services. Bitget raw-REST client gains futures funding-rate fetch. Readiness = explicit gates checked by a new `scalp readiness` command.

**Tech Stack:** Bun 1.4, TypeScript 6, `effect@4.0.0-beta.102`, `@effect/platform-bun@4.0.0-beta.102`, `bun:sqlite`, `decimal.js`, `fast-check`, bun test.

**Spec:** `docs/superpowers/specs/2026-07-17-futures-scalping-ts-effect-v4-design.md`

## Global Constraints

- `effect` pinned EXACTLY to `4.0.0-beta.102` (no `^`). Same for `@effect/platform-bun`.
- No v3-line deps remain: `@effect/cli`, `@effect/platform`, `@effect/printer*` must all be removed from `package.json`.
- Money math uses `decimal.js` (`src/utils/money.ts`) — never raw float for PnL/fee math.
- All IO wrapped in `Effect.try` / `Effect.tryPromise` / services; no bare `new Database`, `fs.*`, `process.env`, `console.*` inside Effect code.
- Tests: `bun test` (co-located `*.test.ts`); property tests with `fast-check` where numeric invariants exist.
- Gates before "ready": design doc §3 G1–G5 (frequency ≥20 trades/mo/symbol on 5m, expectancy > 0 net of fees, winrate ≥ 50%, PF ≥ 1.3, avg hold ≤ 4h, OOS return ≥ 0, OOS maxDD ≤ 15%, MC p95 DD ≤ 20%, MC ruin ≤ 5%).
- Conventional Commits; bd for tracking. Do NOT commit without explicit user approval (session rule).

## Verified v4 API mappings (spike-tested 2026-07-17, `/tmp/effect-v4-spike`)

| v3 (current code) | v4 (target) |
|---|---|
| `class X extends Context.Tag("X")<X, Shape>() {}` | `class X extends Context.Service<X, Shape>()("X") {}` |
| `import { Either } from "effect"`, `Either.right/left` | `import { Result } from "effect"`, `Result.succeed/fail` |
| `import { FileSystem } from "@effect/platform"` | `import { FileSystem } from "effect"` |
| `import { Path } from "@effect/platform"` | `import { Path } from "effect"` |
| `BunContext.layer` | `BunServices.layer` (from `@effect/platform-bun`) |
| `BunFileSystem.layer` | `BunFileSystem.layer` (unchanged, still `@effect/platform-bun`) |
| `TestClock, TestContext` from `"effect"` | `effect/testing/TestClock` subpath |
| `Effect.gen`, `Layer.succeed/effect/scoped`, `Schema.*`, `Data.TaggedError`, `Config.*`, `Schedule`, `Stream`, `Console`, `Logger`, `Cause`, `Clock`, `Ref`, `Fiber`, `Option`, `Match`, `pipe`, `Effect.try/tryPromise`, `Effect.provide`, `Effect.runPromise` | unchanged |

Full effect-import inventory of the repo (files to touch): `index.ts`; `src/cli/{index,health,gateway,exchange,bitget-futures,bitget,market,doctor,status,scalp}.ts` (+ tests); `src/services/{rate-limiter,path,logger,bitget-client,gateway-state,sqlite,api-client,config,health-check,gateway-orchestrator,pid,process-manager}.ts`; `src/market-data/{gateway,gateway-repository,repository,collector,types}.ts` + `gateways/{index,binance,bitget}.ts`; `src/exchange/{adapter,futures-adapter}.ts` + `adapters/{binance-live,bitget-futures,simulated,simulated-futures}.ts`; `src/paper-trading/{engine,futures-engine,repository}.ts`; `src/risk/{guards,kill-switch,circuit-breaker}.ts`; `src/scalping/{strategy-config,soak,strategy-profile,grid-universe}.ts`; `src/utils/{env,signal}.ts`; `src/schemas/*`; tests throughout. Plain-TS core needing Effect conversion: `src/scalping/{composer,backtest,portfolio-backtest,grid,exit-engine,indicators,market-filter,strategy-library,presets,walk-forward,performance-metrics,symbol-stats,config-runner,types}.ts`, `src/utils/money.ts`.

---

## Phase 0 — Data + baseline (before any migration)

Rationale: migration regressions are only detectable against recorded baseline numbers.

### Task 0.1: Bitget futures funding-rate fetch

**Files:**
- Modify: `services/neuratrade-cli-ts/src/market-data/gateways/bitget.ts` (add `fetchFundingRates` for `productType=USDT-FUTURES` via GET `/api/v2/mix/market/history-fund-rate?symbol=X&productType=USDT-FUTURES&pageSize=100`)
- Modify: `services/neuratrade-cli-ts/src/market-data/gateways/index.ts:81-87` (remove the funding gap: route `bitget-futures` funding fetch to the bitget gateway)
- Test: `services/neuratrade-cli-ts/src/market-data/gateways/bitget.test.ts`

**Interfaces:**
- Consumes: existing `FundingRate` type (`src/market-data/types.ts`), gateway fetch conventions in `bitget.ts`.
- Produces: `fetchFundingRates(symbol, productType): Effect<readonly FundingRate[], MarketDataError>` wired into the gateway registry so `market fetch-funding --exchange bitget-futures` works.

- [ ] **Step 1: Failing test** — in `bitget.test.ts`, add a test that the bitget gateway's funding fetch for `USDT-FUTURES` maps Bitget's `history-fund-rate` response (`{ symbol, fundingRate, fundingTime }`) into `FundingRate[]`. Mock `fetch` via `Effect.succeed` stub following the existing test pattern in that file.
- [ ] **Step 2: Run `bun test src/market-data/gateways/bitget.test.ts` — expect FAIL (method missing).**
- [ ] **Step 3: Implement** `fetchFundingRates` in `bitget.ts` (public REST, no signing; reuse the file's existing request/error helpers; paginate `pageNo` until empty page or limit).
- [ ] **Step 4: Wire routing** in `gateways/index.ts:81-87` so `bitget-futures` funding requests reach it.
- [ ] **Step 5: Test green; live smoke**: `cd services/neuratrade-cli-ts && bun run index.ts market fetch-funding --exchange bitget-futures --symbol BTC/USDT:USDT` then `sqlite3 ~/.neuratrade/data/neuratrade.db "SELECT COUNT(*) FROM funding_rates WHERE exchange='bitget-futures'"` > 0.

### Task 0.2: Fetch 6–12 months of bitget-futures candles

- [ ] **Step 1:** For BTC, ETH, SOL (add BNB, XRP if fast): run
  `bun run index.ts market fetch-candles --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 5m --months 12` (use the CLI's actual flag names — check `bun run index.ts market fetch-candles --help`; the gateway paginates with rate-limit backoff).
- [ ] **Step 2:** Repeat for `15m`. Run in background; each symbol/timeframe is ~50–100k candles.
- [ ] **Step 3: Verify** row counts and continuity:
  `sqlite3 ~/.neuratrade/data/neuratrade.db "SELECT COUNT(*), datetime(MIN(o.timestamp)), datetime(MAX(o.timestamp)) FROM ohlcv_data o JOIN exchanges e ON e.id=o.exchange_id JOIN trading_pairs tp ON tp.id=o.trading_pair_id WHERE e.name='bitget-futures' AND tp.symbol='BTC/USDT:USDT' AND o.timeframe='5m'"` — expect ≥ 50k rows, ≥ 6 months span. Gap check: max gap between consecutive candles ≤ 3× timeframe (allowing maintenance windows).

### Task 0.3: Baseline backtests (the "before" numbers)

- [ ] **Step 1:** Run current templates against the fresh data, e.g.
  `bun run index.ts scalp backtest --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 5m --strategy-type signal --template microScalp --oos-pct 0.2 --mc-iterations 200` for each of: `microScalp`, `connorsRsi2`, `meanReversion` on BTC+ETH, 5m+15m.
- [ ] **Step 2:** Record per run: trades, trades/month, win rate, profit factor, expectancy, total return, max DD, avg/max trade duration, OOS metrics, MC p95 DD / ruin.
- [ ] **Step 3:** Write results table into `docs/superpowers/specs/2026-07-17-baseline-backtest-results.md`. These are the regression reference for Phase 1 and the starting point for Phase 2 tuning.

---

## Phase 1 — Effect v4 migration

Order matters: deps first, then mechanical codemods, then CLI kit, then strategy-core conversion, then verification. After each task: `bunx tsc --noEmit` (via `bun run typecheck`) and `bun test`.

### Task 1.1: Dependency swap

- [ ] **Step 1:** In `services/neuratrade-cli-ts/package.json`: set `effect` to exactly `4.0.0-beta.102`, `@effect/platform-bun` to exactly `4.0.0-beta.102`; DELETE `@effect/cli`, `@effect/platform`, `@effect/platform-node` (if present). Run `bun install`.
- [ ] **Step 2:** `bun pm ls | grep effect` — verify only `effect@4.0.0-beta.102` and `@effect/platform-bun@4.0.0-beta.102` (+ transitive `@effect/platform-node-shared@4.0.0-beta.102`).
- [ ] Expected: typecheck massively red — that is fine until Task 1.6 completes. Keep tasks moving; do not commit mid-breakage.

### Task 1.2: Codemod — imports and `Context.Tag` → `Context.Service`

- [ ] **Step 1:** Mechanical sed across `src/`, `tests/`, `index.ts` (verify each with git diff):
  - `extends Context.Tag("X")<Self, Shape>() {}` → `extends Context.Service<Self, Shape>()("X") {}` (regex: `Context\.Tag\("([^"]+)"\)<([^,]+),\s*([^>]+)>\(\)` → `Context.Service<$2, $3>()("$1")`).
  - `from "@effect/platform"` → `from "effect"` (only `FileSystem`/`Path` are imported from there — inventory above).
  - `import { Either` → `import { Result` and `Either.` → `Result.`; then fix call sites: `Either.right(` → `Result.succeed(`, `Either.left(` → `Result.fail(`, `Either.isRight` → `Result.isSuccess` (check `effect/dist/Result.d.ts` for exact names), `Either.match` → `Result.match`.
  - `BunContext.layer` → `BunServices.layer`; fix the import to `import { BunServices } from "@effect/platform-bun"`.
  - `TestClock, TestContext` imports → `import { TestClock } from "effect/testing/TestClock"` (only `src/services/rate-limiter.test.ts`).
- [ ] **Step 2:** `bun run typecheck` — collect remaining errors into a list; they drive Task 1.4.

### Task 1.3: CLI kit (`src/cli/kit/`) — replaces `@effect/cli`

**Files:**
- Create: `services/neuratrade-cli-ts/src/cli/kit/kit.ts` (parser + dispatcher + help)
- Create: `services/neuratrade-cli-ts/src/cli/kit/kit.test.ts`
- Modify: all `src/cli/*.ts` command modules + `index.ts`

**Interfaces:**
- Produces (consumed by every command module):
  ```ts
  export interface OptionSpec<A> { readonly name: string; readonly alias?: string; readonly description: string; readonly required?: boolean; readonly default?: A; readonly parse?: (raw: string) => A }
  export interface FlagSpec { readonly name: string; readonly alias?: string; readonly description: string }
  export interface CommandSpec<R> {
    readonly name: string; readonly description: string;
    readonly options?: ReadonlyArray<OptionSpec<unknown>>; readonly flags?: ReadonlyArray<FlagSpec>;
    readonly args?: ReadonlyArray<{ readonly name: string; readonly required?: boolean }>;
    readonly subcommands?: ReadonlyArray<CommandSpec<R>>;
    readonly run?: (parsed: { options: Record<string, unknown>; flags: ReadonlySet<string>; args: ReadonlyArray<string> }) => Effect.Effect<void, unknown, R>;
  }
  export const runCli: <R>(root: CommandSpec<R>, argv: ReadonlyArray<string>) => Effect.Effect<void, unknown, R>
  ```
- [ ] **Step 1: Failing tests first** — kit.test.ts: parses `--opt value`, `--opt=value`, aliases, defaults, booleans, positionals, subcommand dispatch, `--help` text, unknown-option error. Property test (fast-check): parse(render(spec, values)) ≡ values for a generated option table.
- [ ] **Step 2:** `bun test src/cli/kit` — FAIL.
- [ ] **Step 3:** Implement `kit.ts` (~250 LOC, pure parser + Effect dispatcher; help generated from specs).
- [ ] **Step 4:** Tests green.
- [ ] **Step 5:** Port command modules one at a time, keeping handler logic byte-identical where possible: `status`, `health`, `doctor`, `gateway`, `market`, `exchange`, `bitget`, `bitget-futures`, `scalp` (largest — port its option tables mechanically; the `Effect.gen` bodies stay). Each: typecheck + that command's tests green before moving on.
- [ ] **Step 6:** `index.ts`: build root `CommandSpec`, `runCli(root, process.argv.slice(2))`, provide composed layers, run via `BunRuntime.runMain`.
- [ ] **Step 7: Smoke**: `bun run index.ts --help`, `bun run index.ts scalp --help`, `bun run index.ts scalp backtest --help` render; `tests/e2e/scalp-help` passes.

### Task 1.4: Fix residual typecheck errors

- [ ] **Step 1:** Work the error list from 1.2/1.3: v4 signature drift (check `node_modules/effect/dist/*.d.ts` — the d.ts files carry doc comments with examples), `Console` API, `Logger` API, `Schedule`/`Stream` in `collector.ts`, `Schema` optional/defaults in `src/schemas/*` and `market-data/types.ts`.
- [ ] **Step 2:** Gate: `bun run typecheck` clean, `bun test` green, `bun run lint` clean.

### Task 1.5: Convert plain-TS strategy core to Effect

**Files:** all of `src/scalping/*.ts` listed in the inventory + `src/utils/money.ts`.

- [ ] **Step 1:** Introduce services: `BacktestEngine` (wraps `runBacktest`), `SignalComposer` (wraps `composeSignal`), `MarketFilter`, `ExitEngine`, `StrategyLibrary` — each `Context.Service` + `Layer.succeed` delegating to the existing pure functions. Pure indicator math (`indicators.ts`, `money.ts`) stays pure and is unit-tested directly — no artificial Effect wrapping of pure math.
- [ ] **Step 2:** Move config into `effect/Schema`-validated `StrategyConfig` (extends existing `strategy-config.ts`); all tunables flow through it.
- [ ] **Step 3:** Remove impure leaks: `console.warn` in `backtest.ts:38` → `Effect.logWarning`; `new Database` / `fs.mkdirSync` in `src/cli/scalp.ts:1323,2152,2963` → `Sqlite` service / `FileSystem`; `process.env` in `index.ts:27-36` → `Config` module.
- [ ] **Step 4:** `bun run typecheck && bun test` green; coverage gate (`bun run test:coverage`, bunfig 80/80/80/70) green.

### Task 1.6: Regression gate vs baseline

- [ ] **Step 1:** Re-run the Task 0.3 backtest matrix on the migrated code.
- [ ] **Step 2:** Results MUST match `2026-07-17-baseline-backtest-results.md` trade-for-trade (same fills, same metrics; engine semantics unchanged). Any diff = migration bug; fix before Phase 2.
- [ ] **Step 3:** R1 check: `grep -r "@effect/cli\|@effect/platform\"\|Context.Tag\|from \"effect\".*Either" src/ index.ts` returns nothing; `bun pm ls | grep -c "3\." = 0` for effect packages.

---

## Phase 2 — Scalping readiness

### Task 2.1: Scalp profile (5m/15m futures defaults)

**Files:**
- Modify: `services/neuratrade-cli-ts/src/scalping/presets.ts` (add `scalp` preset)
- Modify: `services/neuratrade-cli-ts/src/scalping/strategy-profile.ts` (profile fields)
- Test: `services/neuratrade-cli-ts/src/scalping/strategy-profile.test.ts`

Starting envelope (tuning refines these; all already supported by the engine):
`maxBarsInTrade` 12 (5m) / 8 (15m); ATR-based SL ~1.0×ATR(14) with TP sized for net-of-fee R:R ≥ 1.2; fees 0.0006/side (Bitget USDT-M taker), slippage ≥ 0.0002/side, funding accrual on; entry gates loosened for 5m (minConfidence ≈ 0.4–0.45, `minConfluence` 1–2, ADX min relaxed, no session filter initially); leverage 1 for backtest validation.
- [ ] Steps: failing test for profile defaults → implement → green.

### Task 2.2: `scalp readiness` command

**Files:**
- Modify: `services/neuratrade-cli-ts/src/cli/scalp.ts` (new subcommand)
- Create: `services/neuratrade-cli-ts/src/scalping/readiness.ts` + `readiness.test.ts`

**Produces:** `evaluateReadiness(result: BacktestResult, oos: OosResult, mc: McResult, cfg): ReadinessReport` — per-gate PASS/FAIL for G1 (trades/month in-sample ≥ 20 on 5m / ≥ 10 on 15m; OOS trades ≥ 10), G2 (expectancy > 0, PF ≥ 1.3, winrate ≥ 50%), G3 (reuse `isLiveReady` at `src/cli/scalp.ts:4509`), G4 (avg duration ≤ configured envelope, default 4h on 5m). CLI prints a table and exits non-zero on any FAIL.
- [ ] Steps: failing tests per gate (synthetic BacktestResult fixtures, pass and fail cases) → implement → green → smoke on a baseline config.

### Task 2.3: Tuning loop (method, not a code task)

- [ ] **Step 1:** Grid-search with `scalp optimize` / `scalp walk-forward` over the scalp-profile parameter space (thresholds, ATR multiples, maxBarsInTrade, confidence/confluence) on BTC+ETH, 5m+15m.
- [ ] **Step 2:** For each candidate, `scalp readiness` must PASS all gates on BOTH 5m and 15m and on BOTH symbols (accept per-symbol configs if one global config fails).
- [ ] **Step 3:** Anti-overfit rule: candidates that pass IS but fail OOS/walk-forward are rejected; prefer walk-forward-stable params over IS-optimal.
- [ ] **Step 4:** Persist winning configs to `~/.neuratrade/watchlists/scalp-live-ready.json`; record full metrics in `docs/superpowers/specs/2026-07-17-readiness-results.md`.

### Task 2.4: Docs + hygiene

- [ ] Fix stale `services/neuratrade-cli-ts/AGENTS.md` (remove unwired `scalp select|validate|preset|run` docs; document `scalp readiness`, the v4 stack, the CLI kit).
- [ ] Remove leftover experiment files if unused: `research_signals.mjs`, `test_portfolio.mjs`, `add_components.patch` (confirm not referenced first); stale `dist/` stays gitignored-or-deleted decision with user.

---

## Phase 3 — Bitget demo paper validation

- [ ] **Step 1:** Configure demo env (`.env`: `BITGET_USE_SANDBOX=true`, demo API keys from Bitget paper trading; leverage per profile).
- [ ] **Step 2:** Run `bun run index.ts scalp paper-trade --exchange bitget-futures --config <winning config>` as a background soak (target ≥ 3–7 days, or ≥ 50 trades, whichever first).
- [ ] **Step 3:** Verify: orders open AND close (SL/TP/time-stop) within the hold-time envelope; risk guards engage (kill switch, circuit breaker, pre-trade limits); no errors in logs; realized paper PnL roughly tracks backtest expectancy (within MC confidence band — this is a sanity check, not a profit guarantee).
- [ ] **Step 4:** G5 sign-off report appended to readiness-results doc → hand to user for the real-money test account decision.

---

## Self-review notes

- Spec coverage: R1 (Tasks 1.1–1.6 + 1.6 step 3 check), R2 (Task 1.5), R3/G1/G4 (Tasks 2.1–2.3), R4/G2/G3 (Task 2.2 + 2.3), R5/G5 (Phase 3), data gap (Tasks 0.1–0.2), stale docs (2.4).
- Known limits: exact v4 drift fixes in Task 1.4 are typechecker-driven (d.ts is the source of truth); Phase 2.3 outcomes can't be pre-written — acceptance gates, not fixed numbers, define done.
