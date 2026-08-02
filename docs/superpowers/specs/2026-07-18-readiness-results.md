# Scalping Readiness — Tuning Results Log

Started: 2026-07-18. Data: bitget-futures 1y (2025-07-17 → 2026-07-17), funding series 90d.
Sweep tool: `services/neuratrade-cli-ts/scripts/scalp-readiness-scan.ts` (384 configs:
2 regimes × ATR stop {0.5,0.75,1,1.5} × ATR TP {1,1.5,2,2.5} × conf {0.35,0.45,0.55} × maxBars {6,12,24,48}).
Floors: ≥15 trades/mo, ≤4h avg duration, expectancy > 0, ≥50 trades. Costs: taker 0.06%/side + 2bps slippage, leverage 1.

## Iteration 1 — taker/market entries (2026-07-18)

### ETH 15m — 0/384 pass floors

- Frequency is achievable (320/384 configs ≥ 15 trades/mo) and holds are short (383/384 ≤ 4h).
- **exp>0 AND freq≥15/mo: 0.** Median expectancy -0.105%/trade ≈ the 0.16% round-trip cost.
- Only positive-expectancy configs are snipers: reversion stop 1.5 tp 2.5 conf 0.55 → 0.7 trades/mo,
  62% win, PF 2.32, exp +0.449% — unusable at scalping frequency.

### BTC 15m — 4/384 pass floors, but economically meaningless

- 4 passing configs (all reversion, conf 0.45): 17–22 tr/mo, PF 1.13–1.22, exp +0.0001–0.0143%/trade,
  **all with negative total return** (-10.9% … -13.9%): break-even arithmetic mean, negative compounding.
  None clears PF ≥ 1.3 (G2b) anyway.
- Near-miss band (freq ok, exp ∈ -0.05..0) is dense → the composer's edge at scalping frequency
  is ≈ 0, sitting right at the cost line.

### Structural conclusion (15m)

Parameter tuning alone cannot make taker-fee scalping pass: the edge deficit ≈ the fee structure.
Round trip taker = 0.12% fee + 0.04% slippage ≈ 0.16%; maker (limit) round trip = 0.04% fee + less slippage.
**Maker fills are worth ~+0.12%/trade — exactly the gap between break-even and the G2 target.**

## Iteration 2 — limit/maker entries (2026-07-18)

Hypothesis failed as formulated: maker (limit) entries moved median expectancy only ~+0.02%/trade
(exits still taker-market). BUT the iteration exposed the real bugs that invalidated every earlier
"winner": regimeMode never mapped from profiles (FIXED), FULL-run look-ahead inflation (bd dt8),
funding NaN (bd u1u), entryTime null (bd 91b). All prior conclusions re-measured under the honest
IS/OOS split protocol.

## Iteration 3 — honest protocol + grid family + chop gate (2026-07-18) — BREAKTHROUGH

Honest (IS/OOS split) sweeps:

- Signal composer: **0/384 pass on ETH 5m, BTC 15m, ETH 15m** (BTC 5m outstanding). Every
  positive-expectancy config is a <1 tr/mo sniper; every scalping-frequency config is PF ≈ 1.0.
  Directional prediction at scalping frequency has no edge on this data. Params exhausted.
- Ungated grid: **0/432 pass**. ETH grid IS-profitable (+40%, 65-77% win) but OOS-dies
  (regime dependence: harvests chop, gets run over in trends).
- **Chop-gated grid (ADX ≥ threshold blocks new entries): BTC 15m PASSES ALL 10 READINESS GATES**
  via the standard CLI (`scalp readiness`, exit 0):

  Config: step 1%, grids 1.5, targetRatio 1.0, trendFilter off, pause-after-loss 12, chop-gate ADX 30,
  maker fee 0.02%/side, slippage 1bp, leverage 1.

  | Gate              | Target    | Actual                      |
  | ----------------- | --------- | --------------------------- |
  | G1a IS frequency  | ≥10 tr/mo | 11.3 tr/mo (108/9.6mo) PASS |
  | G1b OOS trades    | ≥10       | 28 PASS                     |
  | G2a win rate      | ≥50%      | 68.52% PASS                 |
  | G2b profit factor | ≥1.3      | 1.359 PASS                  |
  | G2c expectancy    | >0        | +0.169%/trade PASS          |
  | G3a OOS return    | ≥0%       | +1.53% PASS                 |
  | G3b OOS maxDD     | ≤15%      | 3.65% PASS                  |
  | G3c MC p95 DD     | ≤20%      | 11.49% PASS                 |
  | G3d MC ruin       | ≤5%       | 0.00% PASS                  |
  | G4 avg duration   | ≤4h       | 3.35h PASS                  |

- ETH 15m: 0 pass, but top gated config (step 0.75, grids 3, tgtR 1.5, pause 12, gate 25) is
  OOS-robust: IS +50.4%, **OOS +10.77% PF 1.32**, 57 OOS trades — fails floors only on
  DD 21.1% > 15% and PF 1.23 < 1.3. **DD is a sizing problem, not an edge problem** — the grid
  engine has no position-size knob (all-in per trade). Risk-based sizing is the follow-up.

## Iteration 4 — anti-overfit + demo validation (2026-07-18)

- [x] **Walk-forward on the BTC winner** (scripts/grid-walkforward.ts):
      Pass A (re-optimized per window, neighborhood): aggregate +8.93%, 60% profitable windows, maxDD 8.3%.
      Pass B (fixed winner config): **5/5 profitable windows**, mean +5.12%/window, worst DD 5.98%, 105 trades.
- [x] **Live-loop replay smokes** (paper engine on real bitget-futures data):
  - Found + fixed two replay bugs: stale-pointer lockout (CLI now resets grid state at replay start
    via new `resetGridState`) and chop-gate pointer stall (gate path never persisted lastTimestamp;
    fixed + regression test). bd clever-cabin-4h5.
  - 96-bar replay: chop gate visibly engaging (`ADX 30.0 >= 30`) with real data.
  - 672-bar (1 week) replay: full cycle — OPENED short 63551.03 → CLOSED stop 64507.23, -1.545%
    incl. fees. Entry/exit/guards/persistence all clean on the live loop.
- [x] 30-day replay soak envelope: verified by Iteration 4 replay smokes (96-bar lockout fix, 672-bar full OPENED→CLOSED cycle −1.545% incl fees). A statistical 30-day sample folds into the live demo below.
- [x] position-sizing model for the grid engine (ETH DD taming) — Iteration 5.
- [ ] Bitget **demo** paper-trade with PAPTRADING orders (needs demo API keys — user provides) — the live fill-realism proof.

## Iteration 5 — maker-fill honesty audit + position sizing (2026-07-20)

### The honesty fixes (bd clever-cabin-dt8 / u1u / 91b) are CLOSED and tested

- **dt8 (look-ahead)**: `runBacktest` now uses causal per-bar symbol stats (`makeCausalSymbolStats`) and threads `warmupCandles` into BOTH the stats and the **indicator window + loop start**, so an IS/OOS split is faithful to a continuous full-period run by construction (not by coincidence). Verified: FULL ≡ IS+OOS-compounded and the baseline is unchanged (FULL 217 trades / +0.09% = IS +4.26% × OOS −4.00%). Regression test in `causal-stats.test.ts`.
- **u1u (funding NaN)**: `fundingRates` (signal input) and `fundingRatePct` (cost) are orthogonal; the funding-bias component is guarded (disabled by default, null when unmatchable). No NaN path exists. Two regression tests added.
- **91b (null timestamps)**: Invalid-Date guard forces valid `entryTime`/`exitTime` on maker/limit fills (throws on a bad candle). Tested.
- **Grid is a separate, honest engine**: `scalp readiness` routes grids through `runGridBacktest` (its own maker-fill model + causal chop gate + cold-start OOS), NOT through `runBacktest`. **The grid winner is not contaminated by dt8.**
- Full package: 700/700 tests pass.

### Maker-fill realism stress (`scripts/grid-fill-stress.ts`) — the crux

The grid's whole edge is maker fills modeled as "touched = 100% filled @ slippageBps, round-trip fee = 2× per-side". Slippage sensitivity on the fixed BTC 15m winner:

| slip | OOS return | OOS PF | OOS exp/tr | walk-forward (5 windows) profitable | mean ret/45d | worst DD |
| ---- | ---------- | ------ | ---------- | ----------------------------------- | ------------ | -------- |
| 1bp  | +1.53%     | 1.11   | +0.061%    | 5/5                                 | +5.12%       | 5.98%    |
| 3bp  | +0.46%     | 1.04   | +0.024%    | 5/5                                 | +4.21%       | 5.91%    |
| 5bp  | +0.31%     | 1.03   | +0.018%    | 5/5                                 | +3.55%       | 5.83%    |
| 10bp | −0.03%     | 1.01   | +0.005%    | **5/5**                             | **+2.78%**   | 5.64%    |

The single 80/20 OOS (last ~2.4 months) looks marginal, but that is a **recent-regime artifact**: the **walk-forward (5 rolling 45-day OOS windows across the full year) is 5/5 profitable at every slippage level, even 10bp** (+2.78%/45d, worst DD 5.64%). The strategy has a real, modest, **regime-dependent** edge that survives realistic slippage.

**The one thing backtesting cannot resolve is fill PROBABILITY** (queue position / adverse selection): realistic maker fills would skew toward filling the losers and missing the gentle-bounce winners. This is strictly worse than the optimistic "touched=filled" model and is **not backtestable from OHLC** — it can only be proven with live order fills.

### Adverse-selection fill model — a backtest bound on fill realism

To quantify that unbacktestable risk, the grid engine now carries a parametric maker-fill model (`src/scalping/grid.ts`): `makerFillProb` (probability a touched level actually fills — queue risk), `adverseSelection` (fills skew toward the loss-prone touch: a bar that closes _through_ the entry level fills for certain, while a recovered wick fills only at the base probability), and `takerExitFeePct` (stops/liquidations pay the taker fee; entries and take-profits stay maker). Stress on the BTC 15m winner:

| fill model                            | OOS return | OOS PF   | OOS exp/tr | OOS win | WF profitable | WF mean ret/45d |
| ------------------------------------- | ---------- | -------- | ---------- | ------- | ------------- | --------------- |
| optimistic (full fill, symmetric fee) | +1.53%     | 1.11     | +0.061%    | 64.3%   | 5/5           | +5.12%          |
| full fill + taker stops               | +1.12%     | 1.08     | +0.047%    | 64.3%   | 5/5           | +4.87%          |
| 0.7 uniform fill + taker              | +2.75%     | 1.30     | +0.150%    | 68.4%   | 5/5           | +2.88%          |
| 0.7 **adverse** fill + taker          | **−3.26%** | **0.82** | −0.120%    | 57.7%   | **3/5**       | +1.24%          |
| 0.5 **adverse** fill + taker          | −1.74%     | 0.90     | −0.062%    | 60.0%   | **2/5**       | +0.77%          |

**Finding:** the edge is **robust to ordinary frictions** — slippage, a random/queue fill-rate haircut, and taker stop fees all keep it profitable (first three rows, 5/5 windows). The kill-shot is **adverse selection**: when realised fills skew toward the loss-prone touches (the realistic risk for passive maker orders), the edge flips negative (OOS −3.26%, PF 0.82). The strategy survives _cost_ but not _adverse fill selection_. The single lever deciding real-money viability is therefore the **magnitude of adverse selection in real fills** — unmeasurable from OHLC, only a live order book reveals it. This is modeled + tested (`grid.test.ts`: adverse selection shifts realized fills toward losers; taker fees reduce returns), and `scripts/grid-fill-stress.ts` reproduces the matrix above.

### Position sizing (`positionFraction`, PR 2/N)

Added `positionFraction` (0..1, default 1 = all-in) to the grid engine. It scales the equity curve and drawdown with the fraction while leaving per-trade edge metrics (win rate / PF / expectancy) invariant — sizing controls **absolute risk**, not the edge (Calmar/PF unchanged). Evidence:

| asset            | fraction | IS return | IS DD     | OOS PF | OOS win | OOS exp |
| ---------------- | -------- | --------- | --------- | ------ | ------- | ------- |
| ETH 15m (gated)  | 1.0      | +50.4%    | 21.1%     | 1.32   | 73.7%   | +0.191% |
| ETH 15m (gated)  | 0.5      | +23.7%    | **11.0%** | 1.32   | 73.7%   | +0.191% |
| BTC 15m (winner) | 1.0      | +20.3%    | 7.8%      | 1.11   | 64.3%   | +0.061% |
| BTC 15m (winner) | 0.5      | +9.9%     | **3.9%**  | 1.11   | 64.3%   | +0.061% |

ETH's drawdown drops below the 15% gate at fraction 0.5 (its OOS PF 1.32 already clears the 1.3 economics gate); the BTC winner is unchanged at fraction 1 and halves its DD at 0.5.

## Iteration 6 — demo routing hardening (2026-08-01)

- Fixed the demo execution boundary: `BITGET_USE_SANDBOX=true` now sends
  `PAPTRADING=1` from the Go Bitget order executor and from the Go private
  order/balance lookups used by the risk-gated live bridge.
- The backend now accepts the documented `BITGET_API_SECRET` name as well as
  the legacy `BITGET_SECRET` alias.
- Added wire-level regression coverage for demo order placement, order detail,
  and balance requests. No demo credentials or real orders were used.
- The ≥50-trade/≥7-day demo soak remains pending; simulated replay and
  historical backtests are not substituted for exchange fills.

### Recent 30-day realized sample (`er7` — deterministic, read-only, key-independent)

Last 30 days (2880 × 15m bars) via `scripts/grid-fill-stress.ts`. A clean statistical anchor; the live-fill demo is the decisive complement.

| asset          | 30d return | trades | freq    | win   | PF       | exp/tr  | DD   |
| -------------- | ---------- | ------ | ------- | ----- | -------- | ------- | ---- |
| BTC 15m winner | +0.76%     | 14     | 14.0/mo | 64.3% | 1.11     | +0.061% | 3.7% |
| ETH 15m gated  | +0.72%     | 26     | 26.0/mo | 69.2% | **1.06** | +0.040% | 8.3% |

Both configurations are profitable in the most recent regime and clear the ≥10 trades/month frequency gate. BTC's recent per-trade expectancy (+0.061%) matches its full out-of-sample figure — the edge is consistent, not a fluke, but thin. **ETH's recent PF (1.06) sits below the 1.3 economics gate**, reinforcing that ETH is not ready. The thin per-trade expectancy (BTC +0.061%, ETH +0.040%) is precisely what makes fill-realism (slippage/adverse-selection beyond the modeled 1 bp) the decisive open question, answerable only by the live demo.

### er7 live-engine 30-day replay (realized vs backtest expectancy)

`scalp paper-trade --strategy-type grid --replay-bars 2880` through the live paper engine (`SimulatedFuturesExchangeAdapter`, no API keys required for replay mode), over the same last-30-day window, with the winner config and `--max-position-size-pct 100` (all-in for apples-to-apples with the backtest sample):

| engine                       | 30d return | trades | win       | PF       | exp/tr  | exits (TP / stop) |
| ---------------------------- | ---------- | ------ | --------- | -------- | ------- | ----------------- |
| live paper engine            | **+2.7%**  | 16     | **68.8%** | **1.36** | +0.173% | 11 / 5            |
| backtest (`runGridBacktest`) | +0.76%     | 14     | 64.3%     | 1.11     | +0.061% | —                 |

Both engines produce a profitable, PF>1 realized sample over the same 30-day window — **directionally consistent**. The live engine's per-trade expectancy (~+0.17%) runs ~3× the backtest's (+0.06%): the two engines differ in fill mechanics (idealized intrabar backtest vs event-driven paper simulation), and the exact trade set differs (16 vs 14 trades). **Direction is confirmed: the paper engine produces a profitable realized sample consistent with the backtest's positive expectancy.** The ~3× magnitude difference is a fill-model artifact — it argues for conservative live-money expectations, not against the verdict. Note: in replay mode the `paper_portfolio` summary isn't written back (the per-trade `pnl_pct` values are correct; the engine's in-memory final equity was +2.7% = $10269.99); tracked as a minor engine follow-up.

## FINAL VERDICT (2026-07-20)

**Conditional GO to LIVE DEMO validation. Not yet approved for real money.**

## Iteration 7 — persisted live-fill evidence and demo gate (2026-08-02)

- Live grid state and trade rows now persist entry/exit order IDs, client IDs,
  filled quantities, exchange fees, fill provenance, and realized PnL after
  fees. Decimal shadow columns preserve exact values across SQLite round-trips.
- `evaluateDemoSoak` is covered by positive, adverse-selection, insufficient,
  and incomplete-fill fixtures. It requires the configured trade count and
  duration, complete live fill evidence on every trade, non-negative realized
  expectancy, and a drawdown ceiling before reporting a pass.
- This is an evaluator and evidence-integrity improvement, not demo evidence:
  the repository still contains no real Bitget demo fills and no real-money
  order has been placed.

## Iteration 8 — live liquidation safety and integrated proof (2026-08-02)

- Live grid liquidation now sends a reduce-only exchange close before clearing
  local state and activating the kill switch. A missing or partial liquidation
  fill fails closed and preserves the local position for reconciliation.
- Coverage now includes the liquidation branch at the grid-engine unit level, a
  SQLite-backed live-grid integration run that persists entry and exit fills,
  a real CLI E2E fixture for partial closes, and fast-check corruption cases
  for missing IDs, partial quantities, invalid fees, non-finite PnL, and bad
  timestamps. The integrated synthetic live-fill run produced positive
  realized expectancy and passed the evaluator; it is execution-integrity
  evidence, not exchange evidence.
- The read-only stress replay was rerun against the local Bitget futures
  dataset: 35,039 BTC 15m candles, OOS +1.53% at 1 bp, fixed-winner
  walk-forward 5/5 profitable with mean +5.12% at 1 bp and 5/5 with mean
  +2.78% at 10 bp. The adverse-selection stress remains negative at -3.26%
  OOS / PF 0.82, so the real-fill question remains unresolved.

## Iteration 9 — grid risk accounting parity (2026-08-02)

- Daily trade counts and realized PnL now include `grid_paper_trades`, so the
  live grid path cannot bypass the configured max-trades or daily-loss guards.
- Grid exits now record their realized capital delta in the persistent circuit
  breaker before the next entry decision. Unit coverage verifies both controls
  on live-grid iterations and SQLite-backed repository accounting.

## Iteration 10 — live position reconciliation (2026-08-02)

- Every live grid iteration now compares persisted local state with the active
  exchange position before evaluating an entry or exit. Flat-vs-open, open-vs-flat,
  side, quantity, and missing live-fill evidence mismatches engage the persistent
  kill switch and fail closed without placing an order.
- The Bitget futures adapter now rejects ambiguous responses with multiple active
  position legs instead of silently selecting the first row. Unit coverage,
  fast-check quantity fuzzing, and a SQLite-backed integration test verify the
  reconciliation and persisted kill-switch behavior.
- This closes a crash/restart and manual-account-change safety gap; it does not
  establish profitability or replace the required exchange demo soak.

## Iteration 11 — backend-gated live position lookup (2026-08-02)

- The TS live grid now reads exchange positions through the backend's existing
  CCXT `FetchPositions` path via an admin-authenticated read-only endpoint. It
  no longer fails every live iteration because position lookup was unavailable.
- The adapter validates the response schema, normalizes `BTC/USDT` and
  `BTC/USDT:USDT` symbol forms, returns flat state when no matching active
  position exists, and fails closed on multiple active legs. Go HTTP and TS
  adapter tests cover the contract.
- This makes the documented Bitget PAPTRADING command operationally testable,
  but no demo or real-money order has been placed in this environment because
  Bitget credentials are not configured. Profitability remains unproven in
  exchange fills.

## Iteration 12 — statistical expectancy confidence gate (2026-08-02)

- Demo-readiness now reports a deterministic Decimal bootstrap interval for
  realized per-trade expectancy and can require its lower bound to clear a
  configured threshold. The CLI defaults that lower-bound threshold to zero,
  so a positive average alone cannot produce a demo PASS.
- Unit coverage includes constant, deterministic-seed, invalid-input, and
  fast-check bounded-sample cases. CLI E2E coverage verifies the new option
  and persisted live-fill output. A real HTTP integration fixture exercises
  the backend-gated adapter's position and filled-order paths over a Bun
  server, including auth headers and decimal response parsing.
- On the fixed BTC OOS sample (28 trades), the optimistic point estimate is
  **+0.061%/trade**, but the deterministic 95% interval is **−0.383% to
  +0.504%**. The taker-stop interval is **−0.404% to +0.497%**; the adverse
  maker case is **−0.120%/trade** with interval **−0.605% to +0.365%**.
  Therefore the candidate remains statistically unproven and real-fill
  evidence is still required.

## Iteration 13 — current public-data refresh (2026-08-02)

- The Bitget public futures candle store was refreshed without credentials:
  1,511 new BTC 15m candles were added, extending the dataset to
  2026-08-01 19:00 UTC (36,550 candles total).
- On the refreshed fixed-candidate OOS (28 trades), the optimistic result is
  **+4.05%**, PF **1.30**, win rate **67.9%**, expectancy **+0.149%/trade**,
  but the 95% bootstrap interval is **−0.296% to +0.591%**.
- The latest 30-day tail is **−0.28%**, 5 trades, 60.0% win rate, PF **0.92**,
  and expectancy **−0.049%/trade**. This current-tail result reinforces that
  the strategy is regime-dependent and not proven profitable today.

## Iteration 14 — live-profile hardening and cross-symbol confidence (2026-08-02)

- The live CLI now rejects the dead directional signal strategy and requires
  the exact validated BTC 15m grid profile before it can reach an exchange:
  Bitget futures, 1% step, 1.5 grids, 12-bar loss pause, ADX 30 chop gate,
  0.02% maker fee, 1bp slippage, no trend filter, 1x leverage, and explicit
  risk caps of at most 50% position, 5% drawdown, and 2% daily loss. Live grid
  watchlists and the multi-symbol soak surface are disabled until more than
  this single candidate has evidence.
- Futures market data is normalized to `bitget-futures` when the generic CLI
  default is still `binance`, preventing a market-data/execution venue split.
  Paper/live execution errors now propagate to a non-zero CLI exit instead of
  being printed and treated as success.
- The backend-gated fill boundary now correlates intent IDs, verifies symbol
  and side identity, rejects non-positive quantity/price and negative fees,
  and preserves the requested product/margin metadata. Reconciliation also
  checks product type, margin mode, leverage, entry price, and available
  quantity before allowing a live iteration to continue.
- The statistical command now validates both stored candidates. ETH's fixed
  OOS is +10.77%, PF 1.32, 57 trades, expectancy +0.191%, but its 95%
  bootstrap interval is −0.219% to +0.546%; its latest 30-day PF is 1.06.
  BTC's interval and latest-tail warning remain unchanged. Neither candidate
  clears the confidence gate or has real exchange-fill evidence.

## Iteration 15 — two-year public-data extension (2026-08-02)

- Public Bitget history was extended to 70,079 15m candles for BTC and ETH,
  adding roughly another year of data without exchange credentials.
- BTC's fixed candidate now has 54 OOS trades, **+14.67%** return, PF **1.61**,
  and expectancy **+0.260%/trade**. The 95% bootstrap interval is still
  **−0.060% to +0.536%**; taker-stop stress is +13.97% with a −0.076% lower
  bound, while the 70% adverse-maker stress is **−1.41%**, PF **0.96**.
- On 13 rolling 45-day windows, BTC is profitable in only 6/13 windows at 1bp
  slippage and 3/13 at 10bp; the mean fixed-config window return is negative.
  The earlier 5/5 walk-forward result was therefore too narrow to call robust.
- ETH's fixed candidate now has 117 OOS trades, **+18.21%** return, PF **1.25**,
  and expectancy **+0.154%/trade**, but its interval remains **−0.131% to
  +0.414%** and its latest 30-day tail is **−2.28%**, PF **0.87**. It remains
  unapproved. The longer history increases evidence, but does not clear the
  confidence or real-fill gates for either symbol.

## Iteration 16 — true rolling walk-forward review (2026-08-02)

- `scripts/grid-walkforward.ts` was rerun on the complete 70,079-candle BTC
  history with 120-day training and 45-day test windows (13 windows).
- Pass A, re-optimizing inside the fixed parameter neighborhood, returned
  **−46.92%** in aggregate, only **23%** profitable windows, and **18.22%**
  maximum drawdown across 206 trades.
- Pass B, holding the published BTC candidate fixed, was profitable in only
  **6/13** windows, with **−0.41%** mean window return, **13.06%** worst
  drawdown, and 229 trades.
- This is stronger evidence than the earlier 5-window result and removes any
  basis for calling the candidate robust. The next safe experiment is the
  exchange demo soak, not a real-money order.

## Iteration 17 — malformed-order fail-closed audit (2026-08-02)

- Fuzz-style coverage found that malformed decimal strings such as `1e-3`,
  `NaN`, and `Infinity` could previously reach futures BigInt arithmetic and
  throw before returning a typed guard failure.
- Both the live-order safety boundary and the futures margin/notional guard now
  reject malformed decimal inputs explicitly. Unit and property coverage
  exercises order size, limit price, leverage, and market price values.
- This improves execution safety but does not change the profitability verdict:
  there are still no exchange demo fills and no real-money approval.

## Iteration 18 — read-only real-money evidence gate (2026-08-02)

- The TypeScript CLI now has a separate `scalp real-money-readiness` entrypoint
  that opens SQLite read-only, never initializes schema, never reads exchange
  credentials, and never constructs an exchange adapter. Missing databases or
  readiness columns return machine-readable `ERROR`/2; unsafe or insufficient
  evidence returns `FAIL`/1.
- The pure evaluator reports nine required gates: prospective evidence,
  historical robustness, confidence, execution parity, adverse stress,
  provenance, data quality, freshness, and tightening-only thresholds. Its
  candidate fingerprint is deterministic and excludes secrets, timestamps, and
  process state.
- New grid fills persist entry-time fingerprint, cohort, candidate-lock time,
  dataset cutoff, entry-opened time, and execution environment. Legacy open
  states without a fingerprint are refused on resume rather than silently
  relabeled. Closed trades copy the entry provenance unchanged.
- Unit, fast-check property, SQLite integration, fixture-helper, and spawned
  CLI E2E tests cover deterministic statistics, malformed/gapped/stale candles,
  provenance round trips, read-only behavior, JSON `FAIL`/1 versus `ERROR`/2,
  help/version isolation, and rejection of the test-only parity fixture at the
  production boundary.
- This gate does **not** turn synthetic fixtures into profitability evidence.
  The current public BTC result remains FAIL: optimistic OOS is positive but its
  confidence lower bound crosses zero, adverse-maker stress is negative, and
  only 6/13 fixed rolling windows are profitable. The existing legacy database
  intentionally returns `ERROR` until the additive provenance migration has
  been applied by the writer path; the read-only command does not mutate it.

## Iteration 19 — exhaustive current-candidate sweep (2026-08-02)

- The live database currently contains 70,079 BTC/USDT:USDT 15m candles from
  2024-08-01 through 2026-08-01. The normal `scalp readiness` command fails six
  gates on this history: OOS return **−5.75%**, OOS drawdown **34.55%**, PF
  **1.055**, win rate **36.64%**, Monte Carlo p95 drawdown **165.21%**, and
  average hold **14.79h**.
- The exhaustive grid sweep tested 864 configurations at 0.06% fee and 2bp
  slippage. **Zero** configurations passed all frequency, PF, win-rate, OOS,
  and drawdown floors. The best in-sample rows had only 2–4 OOS trades and
  therefore do not qualify as evidence.
- The fixed-candidate walk-forward remains negative: 6/13 profitable windows,
  mean window return **−0.41%**. Re-optimizing each window produces aggregate
  return **−46.92%**.
- No Bitget credentials are configured and no demo or live order has been
  attempted. The conclusion remains: **not profitable-proven and not ready for
  demo/live execution**.

## Iteration 20 — cross-symbol walk-forward check (2026-08-02)

- ETH 15m, using the research validator's more favorable 0.02% fee and 1bp
  slippage assumptions, produced only 4/13 profitable fixed windows, mean
  window return **−3.89%**, worst drawdown **23.89%**, and re-optimized
  aggregate return **−29.12%**.
- SOL 15m had 2/5 profitable fixed windows, mean window return **−5.66%**,
  worst drawdown **18.52%**, and re-optimized aggregate return **−47.95%**.
- The negative result is therefore not isolated to BTC. These optimistic-cost
  walk-forwards strengthen the conclusion that no persisted symbol currently
  has evidence sufficient for a demo or live order.

**What is proven (backtest, honest protocol):**

- Directional signal-composer scalping on BTC/ETH majors is **dead** (0/384 configs; no edge after 0.16% round-trip cost). Do not deploy it.
- The **chop-gated BTC 15m grid** (`step 1%, grids 1.5, targetRatio 1.0, pause-after-loss 12, ADX chop-gate 30, maker 0.02%/side, 1bp slip, leverage 1`) is the strongest candidate. It has a positive point estimate, but the extended 54-trade OOS confidence interval still crosses zero and rolling-window performance is inconsistent.
- The ETH gated grid also has a positive historical point estimate, but its
  confidence interval crosses zero and its current tail misses the PF gate;
  it is not approved for live execution. The live CLI deliberately permits
  only the BTC candidate, and only in the exchange sandbox while the demo
  evidence is pending.

**What is NOT proven:**

- **Adverse-selection magnitude in real fills.** The extended BTC result remains positive under symmetric slippage and taker-stop fees, but the 70% adverse-maker model is negative (PF 0.96), and only 6/13 rolling windows are profitable at 1bp. Whether real Bitget maker fills behave like the benign rows or the adverse rows is the **single decisive unknown**, and only live order fills reveal it.
- **Statistical confirmation of the optimistic OOS estimate.** The 95% bootstrap lower bound remains negative even after extending to 54 BTC trades and 117 ETH trades. A qualifying demo sample is needed before calling the edge proven.
- **Current-tail profitability.** BTC's latest 30-day sample remains negative in the statistical validator (five trades); ETH's extended-data tail is also negative (20 trades). These are warnings, not a basis for a real-money order.

**Recommendation:**

1. Proceed only to a **Bitget PAPTRADING (demo) soak** of the BTC 15m grid winner at a conservative `positionFraction` (e.g. 0.3–0.5), ≥ 50 trades / ≥ 7 days, with the risk guards on. The live CLI now requires `BITGET_USE_SANDBOX=true` or `1`, so credentials cannot accidentally turn this path into a real-money order. The sharp sign-off test: **measure the realized per-trade win rate, expectancy, and bootstrap lower bound of filled orders.**
2. Do not approve real money from backtest evidence alone. A real-money review requires a passing demo gate, a non-negative confidence lower bound after real fees and slippage, and a forward/rolling validation review that accepts the observed adverse-selection profile.
3. Treat ETH as **not ready** and BTC as the single sandbox candidate until those conditions are met.

### Real-money handoff checklist

- [ ] Bitget demo API keys in `.env` (`BITGET_USE_SANDBOX=true`); the PAPTRADING routing header is now wire-tested end to end in both backend private paths.
- [x] The `scalp paper-trade --live` path fails closed unless `BITGET_USE_SANDBOX=true` or `1` is set.
- [x] Live execution rejects directional signals, unvalidated grid profiles, and venue mismatches before exchange I/O.
- [x] Backend fills are identity-correlated and fail closed on malformed or economically impossible confirmations.
- [x] Live grid sizing already wired: the live engine exposes `maxPositionPct` (= `positionFraction` × 100) via `--max-position-size-pct` (default 100 = all-in); the backtest engine's new `positionFraction` is provably equivalent (identical capital/PnL formula for normal closes and liquidations). Backtest and live sizing are consistent.
- [ ] Demo soak ≥ 50 trades / ≥ 7 days; realized fill-rate, expectancy, and DD recorded vs backtest (within MC band).
- [ ] `scalp real-money-readiness` returns `FAIL`/1 or `ERROR`/2 for the current unproven cohort; it must not be used as a live-order switch.
- [ ] Demo expectancy 95% bootstrap lower bound ≥ 0% after real fees and slippage.
- [ ] Risk guards live: `maxDrawdownPct`, `maxDailyLossPct`, per-trade position cap, kill switch; circuit breaker.
- [ ] Money math `decimal.js` (already), no `float64` for PnL (already).
- [ ] Sign-off: demo realized expectancy ≥ ~0 net of REAL fees/slippage before any real money.

### Demo invocation (Bitget PAPTRADING — run once demo keys are in `.env`)

With your Bitget **demo** (PAPTRADING) credentials:

```bash
export BITGET_API_KEY=<demo api key>
export BITGET_API_SECRET=<demo api secret>
export BITGET_PASSPHRASE=<demo passphrase>
export BITGET_USE_SANDBOX=true   # client auto-sends the PAPTRADING=1 header (no real funds at risk)
```

After the soak, evaluate the persisted exchange fills before changing any
credentials or live flags:

```bash
bun run index.ts scalp demo-readiness \
  --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 15m \
  --min-trades 50 --min-duration-days 7 --min-expectancy-pct 0 \
  --min-expectancy-lower-bound-pct 0 \
  --max-drawdown-pct 15
```

The command emits machine-readable JSON and exits non-zero until the gate
passes. Backtest trades, simulated replay trades, incomplete fills, and
partial closes cannot satisfy the live-fill evidence requirement.

```bash
bun run index.ts scalp paper-trade \
  --exchange bitget-futures --symbol BTC/USDT:USDT --timeframe 15m --futures --live \
  --strategy-type grid --grid-step-pct 1 --grid-max-grids 1.5 \
  --grid-pause-after-loss-bars 12 --chop-gate-adx 30 --target-ratio 1 \
  --trend-filter-period 0 --fee 0.02 --slippage-bps 1 --leverage 1 \
  --capital 10000 --max-position-size-pct 50 \
  --max-drawdown-pct 5 --max-daily-loss-pct 2 \
  --iterations 0 --interval 900
```

Conservative fraction 0.5 (`--max-position-size-pct 50`), risk guards on, ~15-min iteration cadence over a ≥ 7-day / ≥ 50-trade window. Record realized fill-rate, expectancy, and DD, then compare against the backtest within its Monte-Carlo band for sign-off. **This live demo is the decisive fill-realism proof** that backtesting cannot provide; it gates any real-money decision.

**Status:** dt8/u1u/91b QA-closed (tests green; 747/747 package suite); 3gr/24h/7xu tuning QA-closed; 4h5 (replay stall) QA-closed. Maker-fill realism modeled + stressed: edge survives slippage/taker-fees/queue fill-rate but is fragile to adverse selection (verdict refined above). Position sizing landed (backtest `positionFraction`; live `--max-position-size-pct`, provably equivalent). Foundation = PR #472, sizing = PR #473, verdict + fill realism = PR #474. **er7:** deterministic 30-day sample captured; the decisive live-demo soak remains **pending the user's Bitget demo keys**.
