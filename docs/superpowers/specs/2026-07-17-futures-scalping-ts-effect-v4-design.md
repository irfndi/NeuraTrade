# Futures Scalping TS Port + Effect v4 + Scalping Readiness — Design

Date: 2026-07-17
Branch: `feature/ts-port-readiness-paper-backtest-bitget`
Status: design (auto-approved in goal mode; awaiting implementation plan)

## 1. Problem statement

The user is stuck on this branch. Three requirements:

1. **Port futures scalping to TypeScript** — a port of the Go scalping engine exists in
   `services/neuratrade-cli-ts/` but is not finished/validated.
2. **All TypeScript must be EffectTS v4** (`effect` v4, https://github.com/effect-ts/effect).
   Current state: `effect@3.21.3` + `@effect/cli@0.75.2` + `@effect/platform*@0.9x` (all v3-line).
3. **Make it actually ready for futures scalping.** Current symptoms:
   - Cannot open/close positions in short times; holds too long (not "scalping enough").
   - Too few trades in 1-month / 1-year backtests.
   - Win rate is bad. Profitability is bad.
   - Evidence: `~/.neuratrade/watchlists/grid-eth-bitget.json` is empty (no passing grid config);
     existing walk-forward artefacts are slow daily strategies (~4.5 trades/window).

First target exchange: **Bitget (USDT-M futures)**. User will provide a real-money testing
account only when the readiness gates below are green.

## 2. Current state (verified 2026-07-17)

### TS port (`services/neuratrade-cli-ts/`)

- Bun runtime. `@effect/cli` root command `neuratrade` with subcommands
  `gateway|status|health|doctor|market|scalp|exchange|bitget`
  (`index.ts`, `src/cli/index.ts`).
- `scalp` subcommands wired: backtest, optimize, scan, paper-trade, soak, profile, library,
  walk-forward (`src/cli/scalp.ts:4833`).
- Effect-styled: `src/cli/`, `src/services/`, `src/schemas/`, `src/market-data/`,
  `src/exchange/`, `src/paper-trading/`, `src/risk/`.
- **Plain TS (zero effect imports)**: the whole strategy/backtest core — `src/scalping/*`
  (composer, backtest, grid, exit-engine, indicators, market-filter, strategy-library,
  presets, walk-forward, performance-metrics, symbol-stats, config-runner), `src/utils/money.ts`.
- Impure leaks inside Effect code: direct `new Database`, `fs.mkdirSync`, `process.env`,
  `console.warn` in `src/cli/scalp.ts` / `index.ts`.
- Bitget: raw REST HMAC client (`src/services/bitget-client.ts`, 1,166 LOC) with
  USDT-FUTURES support, demo/paper via `PAPTRADING=1` header (`BITGET_USE_SANDBOX`),
  live-order safety gate requiring `--force`. Market-data gateway routes `bitget` /
  `bitget-futures`. **Gap: funding-rate fetch for bitget-futures not implemented**
  (`src/market-data/gateways/index.ts:81-87`).
- Backtest engine (`src/scalping/backtest.ts`): fees, slippage, leverage + liquidation,
  funding accrual (8h), trailing/breakeven, time-stop `maxBarsInTrade`, scale-out, RSI exit.
  Metrics: win rate, return, maxDD, Sharpe, profit factor, expectancy, Sortino, Calmar,
  OOS split, Monte-Carlo (p95/p99 DD, ruin), robustness score.
- Live-ready gates already coded (`src/cli/scalp.ts:4509` `isLiveReady`): OOS return ≥ 0,
  OOS maxDD ≤ 15%, MC p95 DD ≤ 20%, MC ruin ≤ 5%, IS and OOS trades ≥ 10
  (OOS = last 20%, 200 MC iterations).
- AGENTS.md in the service documents commands that are not wired (`scalp select|validate|
  preset|run`) — stale doc, fix during migration.

### Go reference (`services/backend-api/`)

- Backtest engine `internal/services/scalping_backtest.go` (2,438 LOC), entry
  `cmd/5yr-backtest/main.go`. Deterministic entry families (BB %b, reversal/sell-window/
  blowoff), expectancy gate, asymmetric exits (SL 0.5% / TP 1.5% / BE / trailing 0.4%),
  max-loss 1.5%, time-stop 200 candles. Leverage default 5 live / 1 in backtest.
- The Go logic is the *behavioral reference* for entry/exit semantics; the TS port already
  re-implements most of it (plus more: templates, grid, MC/OOS).

### Data (local SQLite `~/.neuratrade/data/neuratrade.db`, shared relational schema)

- Binance: rich — 5m for 1yr (many alts) and 5yr (BTC/ETH/BNB), 1h/4h/1d 5yr majors.
- Bitget: thin — **spot only** ("bitget"), ~1,000 1h candles for BTC/ETH/SOL/XRP/DOGE/WLD
  and ~1 month ETH 15m. **No bitget-futures candles yet** — must fetch before any
  meaningful Bitget backtest.

### Effect v4 feasibility (npm, 2026-07-17)

- `effect@4.0.0-beta.98` (`beta` dist-tag). v4 is beta-only; pin exact version.
- `@effect/platform-bun@4.0.0-beta.98`, `@effect/platform-node-shared@4.0.0-beta.98` exist.
- **`@effect/cli` has no v4 release** — v4 monorepo `packages/` contains: ai, atom, effect,
  opentelemetry, platform-browser, platform-bun, platform-node-shared, platform-node, sql,
  tools, vitest. The CLI layer must be re-implemented on v4 primitives.
- `@effect/vitest` exists for tests (v4 line); current tests use `bun test` — keep `bun test`.

## 3. Requirements and readiness definition

Functional requirements:

- R1: Every TS file in `services/neuratrade-cli-ts/` compiles against `effect@4` beta; no
  v3-line `@effect/*` deps remain (no `@effect/cli`, no v3 `@effect/platform*`).
- R2: Strategy/backtest core converted to Effect idiom: services as `Context.Tag` + `Layer`,
  errors as `Data.TaggedError`, config via `effect/Schema`, IO wrapped in `Effect.try*`.
  Pure math (indicators) may stay pure functions but lives in Effect-structured modules.
- R3: Scalping behavior — the system opens and closes positions on short horizons
  (minutes-to-hours, not days) with materially higher trade frequency than today.
- R4: Win rate and profitability gates (below) pass on Bitget futures backtests.
- R5: Bitget futures paper trading (demo, `PAPTRADING=1`) runs end-to-end with risk guards.

Readiness gates (measurable "ready" definition). On ≥ 6 months of bitget-futures 5m and 15m
data, per symbol (BTC/USDT:USDT, ETH/USDT:USDT at minimum):

- G1 Frequency: ≥ 20 trades/month/symbol on 5m (or ≥ 10 on 15m) in-sample and OOS ≥ 10
  (OOS gate already in code).
- G2 Economics: expectancy > 0 after taker fees (0.06%/side), slippage (≥ 2 bps/side) and
  funding accrual; profit factor ≥ 1.3; win rate ≥ 50% (with R:R ≥ 1) — win rate and R:R
  are jointly acceptable if expectancy > 0, but target both.
- G3 Robustness (existing `isLiveReady`): OOS return ≥ 0, OOS maxDD ≤ 15%, MC p95 DD ≤ 20%,
  MC ruin ≤ 5%.
- G4 Hold time: avg trade duration ≤ 4h on 5m configs (scalping, not swing); p90 duration
  reported.
- G5 Paper: futures paper engine on Bitget demo opens/closes real demo positions within
  configured hold-time envelope for a soak run without guard violations.

## 4. Design decisions

### D1. Adopt `effect@4.0.0-beta` pinned exactly

User requirement is explicit ("all TS must effectTS v4"). v4 is beta; mitigate by pinning
the exact beta (`4.0.0-beta.98` at time of writing, or newer at implementation time) with
no version ranges, and by isolating v4 API surface behind our own small wrapper modules
where APIs are churn-prone. Bun is unaffected (runtime).

### D2. Replace `@effect/cli` with a minimal hand-rolled CLI layer

No v4-compatible `@effect/cli` exists and the v4 monorepo has no cli package. Rather than
wait or keep a v3 island (violates R1), implement `src/cli/kit/` — a tiny typed argv
parser/dispatcher (~200–300 LOC) supporting exactly the used surface: named options with
defaults/required/alias, boolean flags, positional args, subcommand trees, `--help`
generation. Command handlers (the business logic in `src/cli/*.ts`) keep their signatures;
only parsing/dispatch changes. This removes `@effect/cli`, `@effect/printer*` and the v3
`@effect/platform` dependency chain in one move.

### D3. Convert the plain-TS strategy core to Effect — structurally, not cosmetically

- Keep pure functions pure: `indicators.ts`, `money.ts` stay deterministic calculations but
  are exercised through Effect services where IO/config is involved.
- Introduce services: `CandleRepository` (existing, keep), `FundingRepository`,
  `StrategyConfig` (Schema-validated), `BacktestEngine` (runs `runBacktest` as an Effect
  with config injected, no `console.warn`), `Clock`/`Random` for determinism in MC.
- Wrap all sqlite/fs/env access in `Effect.try*` / Config services (fixes the existing
  impure leaks in `src/cli/scalp.ts`, `index.ts`).
- v4 API differences to handle during migration (verify against bundled d.ts at
  implementation time): module re-exports, `Schema` API changes, `Effect.gen` retention,
  `Layer`/`Context` ergonomics. Full typecheck (`tsc --noEmit`) is the migration gate.

### D4. Scalping readiness work (strategy + engine + data)

Data first:

- Implement bitget-futures funding-rate fetch (`src/market-data/gateways/bitget.ts` +
  routing in `gateways/index.ts:81-87`) — needed for realistic futures economics.
- Fetch ≥ 6 months (target 12) of bitget-futures 5m + 15m candles for BTC, ETH (+ SOL,
  BNB, XRP if API limits allow) via existing `market fetch-candles --exchange bitget-futures`.

Engine/strategy:

- Add a first-class **scalp profile** (not just template): 5m/15m defaults —
  `maxBarsInTrade` 6–24 (30min–2h on 5m), tight fee-aware TP/SL (net-of-fee R:R ≥ 1.2),
  entry-frequency levers (loosened confluence/minConfidence for 5m noise, relaxed ADX/session
  gates), microScalp/connorsRsi2 tuned for futures fees.
- Fee realism: default futures taker fee 0.06%/side (Bitget), slippage ≥ 2 bps, funding
  accrual on — these are already engine capabilities; make the scalp profile use them by
  default.
- Add `scalp readiness` command: runs backtest + OOS + MC for a config and prints a
  per-gate PASS/FAIL report against G1–G4 (reuses `isLiveReady` + frequency/duration
  gates). This makes "ready" a command, not a judgement call.
- Tuning loop: `scalp optimize` / walk-forward over the scalp profile space to find configs
  passing all gates; persist winners as watchlist entries.

Validation: `bun test`, `tsc --noEmit`, oxlint, coverage gate (bunfig 80/80/80/70) stay green
throughout.

### D5. Paper-trade validation on Bitget demo (pre-real-money)

- Run `scalp paper-trade` (futures engine) against Bitget demo (`BITGET_USE_SANDBOX=true`,
  `PAPTRADING=1`) with the passing config; verify open/close latency, hold-time envelope,
  guards (pre-trade limits, kill switch, circuit breaker), and that no real-money endpoint
  is touched. User supplies the real-money test account only after G1–G5 pass.

## 5. Phasing

- **Phase 0 — Baseline + data.** Implement bitget-futures funding fetch; fetch candle
  history; run baseline backtests with current templates; record "before" numbers
  (trades/month, win rate, PF, expectancy, durations) in this doc's follow-up notes.
- **Phase 1 — Effect v4 migration.** Pin v4 deps, build `src/cli/kit`, remove @effect/cli,
  convert strategy core + fix impure leaks, green typecheck/tests.
- **Phase 2 — Scalping readiness.** Scalp profile, fee/funding realism defaults,
  `scalp readiness` command, optimize/walk-forward tuning until G1–G4 pass.
- **Phase 3 — Paper validation.** Bitget demo soak with guards; report; hand off for
  real-money test account.

Phase order note: Phase 0 before Phase 1 so migration regressions are detectable against
recorded baseline numbers (same engine outputs pre/post v4).

## 6. Risks / open questions

- **v4 beta churn**: pin exact versions; isolate churn-prone APIs; expect one mechanical
  migration pass over imports/types.
- **CLI rewrite scope**: `src/cli/scalp.ts` is 4.8K LOC of @effect/cli definitions; the kit
  must cover the used option/flag surface — bounded by what typecheck flags.
- **Bitget futures candle history depth**: Bitget public API caps kline lookback
  (~1000 candles/request, pagination needed); 6–12 months of 5m = 52k–105k candles/symbol —
  fetching loop with rate-limit backoff already exists in the gateway; verify pagination
  covers deep history.
- **Win-rate vs frequency tension**: loosening gates raises frequency but can sink win rate;
  the acceptance gate is expectancy/PF (G2) with win-rate target, tuned via optimize, not
  by hand-waving.
- **Data snooping**: tuning on the same 6–12 months used for gates → mitigate with the
  existing OOS split + walk-forward; treat any config that only passes IS as not ready.

## 7. Out of scope (YAGNI)

- Porting the Go backend's LLM/deepseek live decision path — futures scalping here is the
  deterministic/engine path; LLM remains in Go.
- ccxt adoption (Bitget client is raw REST and works).
- Multi-exchange generalization beyond keeping the existing binance gateway working.
- Docker/deployment changes.
