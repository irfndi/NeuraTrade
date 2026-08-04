# Baseline Backtest Results ("before" numbers) — 2026-07-17

Branch: `feature/ts-port-readiness-paper-backtest-bitget`
Data: bitget-futures USDT-M perpetuals, 1 year (2025-07-17 → 2026-07-17), SQLite `~/.neuratrade/data/neuratrade.db`
Economics: capital 10,000 USDT, `--futures --leverage 1 --fee 0.06/side --slippage-bps 2 --oos-pct 20 --mc-iterations 200`
Reproduce: `cd services/neuratrade-cli-ts && bash scripts/baseline-matrix.sh`

Purpose: (1) regression reference for the Effect v4 migration (results must match
trade-for-trade after migration); (2) starting point for Phase 2 readiness tuning.

## Readiness gates (from design doc §3)

| Gate | Target |
|---|---|
| G1 frequency | ≥ 20 trades/month/symbol on 5m (≥ 10 on 15m); OOS trades ≥ 10 |
| G2 economics | expectancy > 0 net of fees; profit factor ≥ 1.3; win rate ≥ 50% |
| G3 robustness | OOS return ≥ 0; OOS maxDD ≤ 15%; MC p95 DD ≤ 20%; MC ruin ≤ 5% |
| G4 hold time | avg trade duration ≤ 4h on 5m |

## Results — engine defaults

### BTC/USDT:USDT 5m (1y)

| Metric | Value | Gate | Verdict |
|---|---|---|---|
| Trades | 110 (9.2/mo) | G1 ≥ 20/mo | FAIL |
| Win rate | 40.00% | G2 ≥ 50% | FAIL |
| Profit factor | 1.201 | G2 ≥ 1.3 | FAIL |
| Expectancy | 0.096% | G2 > 0 | pass (barely) |
| Total return | 1.92% | — | — |
| Max drawdown | 14.83% | — | — |
| Avg trade duration | 9.10h | G4 ≤ 4h | FAIL |
| OOS trades / winrate / return | 17 / 23.53% / **-9.15%** | G3 | FAIL |
| MC p95 DD | 25.97% | G3 ≤ 20% | FAIL |
| MC ruin | 0.00% | G3 ≤ 5% | pass |

### ETH/USDT:USDT 5m (1y)

| Metric | Value | Gate | Verdict |
|---|---|---|---|
| Trades | 279 (23.3/mo) | G1 ≥ 20/mo | pass |
| Win rate | 35.48% | G2 ≥ 50% | FAIL |
| Profit factor | 0.897 | G2 ≥ 1.3 | FAIL |
| Expectancy | **-0.124%** | G2 > 0 | FAIL |
| Total return | **-43.40%** | — | — |
| Max drawdown | 49.21% | — | — |
| Avg trade duration | 6.28h | G4 ≤ 4h | FAIL |
| OOS trades / winrate / return | 45 / 31.11% / **-13.39%** | G3 | FAIL |
| MC p95 DD | 59.01% | G3 ≤ 20% | FAIL |
| MC ruin | 0.00% | G3 ≤ 5% | pass |

**Baseline summary so far:** BTC trades too rarely and breaks even IS (loses
OOS); ETH trades often enough but bleeds on every trade (negative expectancy).
Both hold far too long (6–9h avg). This is the quantified "not scalping
enough": frequency is config-dependent, win rate/expectancy are negative or
flat, hold times are swing-scale.

### BTC/USDT:USDT 15m (1y)

| Metric | Value | Gate | Verdict |
|---|---|---|---|
| Trades | 107 (8.9/mo) | G1 ≥ 10/mo (15m) | FAIL |
| Win rate | 35.51% | G2 ≥ 50% | FAIL |
| Profit factor | 0.949 | G2 ≥ 1.3 | FAIL |
| Expectancy | **-0.069%** | G2 > 0 | FAIL |
| Total return | **-15.37%** | — | — |
| Max drawdown | 27.41% | — | — |
| Avg trade duration | 16.12h | G4 ≤ 4h | FAIL |
| OOS trades / winrate / return | 19 / 31.58% / **-7.26%** | G3 | FAIL |
| MC p95 DD | 32.85% | G3 ≤ 20% | FAIL |
| MC ruin | 0.00% | G3 ≤ 5% | pass |

### ETH/USDT:USDT 15m (1y)

| Metric | Value | Gate | Verdict |
|---|---|---|---|
| Trades | 233 (19.4/mo) | G1 ≥ 10/mo (15m) | pass |
| Win rate | 33.48% | G2 ≥ 50% | FAIL |
| Profit factor | 0.952 | G2 ≥ 1.3 | FAIL |
| Expectancy | **-0.078%** | G2 > 0 | FAIL |
| Total return | **-31.50%** | — | — |
| Max drawdown | 37.42% | — | — |
| Avg trade duration | 9.82h | G4 ≤ 4h | FAIL |
| OOS trades / winrate / return | 47 / 34.04% / **-9.11%** | G3 | FAIL |
| MC p95 DD | 53.96% | G3 ≤ 20% | FAIL |
| MC ruin | 0.00% | G3 ≤ 5% | pass |

### scalp-optimized profile, BTC 5m — pre-fix INVALID run (for the record)

16,583 trades, 0.00% win rate, -100% return, avg duration 0.00h. Root cause:
**profile symbol-key mismatch** — the profile keys overrides by `BTC/USDT` but
futures runs use `BTC/USDT:USDT`, so per-symbol tuned params silently fell
back to defaults (bd `clever-cabin-3px`, FIXED this session: `findSymbolOverride`
now matches settle-suffix variants both ways + logs a loud warning on no match).

### scalp-optimized profile, BTC 5m — post-fix (valid, for reference)

27 trades (2.3/mo), win rate 18.52%, return -6.78%, PF 0.660, expectancy
-0.196%, avg duration 0.86h, OOS 1 trade. The incumbent tuned profile is worse
than defaults on Bitget futures — strong evidence that Phase 2 needs a proper
tuning loop on bitget-futures data, not hand-me-down Binance profiles.

## Data integrity

- BTC 5m: 105,120 candles (2025-07-17T01:30Z → 2026-07-17T01:25Z), ~105,121 expected. PASS
- ETH 5m: 105,119 candles (same span). PASS
- SOL 5m: 105,120 candles (same span). PASS
- BTC 15m: 35,039 candles (~35,041 expected). PASS
- ETH 15m: 35,039 candles. PASS
- SOL 15m: 35,039 candles. PASS
- Funding rates: BTC 270 rows (~90 days, Bitget history cap); ETH/SOL pending retry.

## Matrix complete — all four default-config cases recorded above.

## Cross-case summary (engine defaults)

| Case | Trades/mo | Winrate | Expectancy | Return | Avg hold | OOS return |
|---|---|---|---|---|---|---|
| BTC 5m | 9.2 | 40.0% | +0.096% | +1.9% | 9.1h | -9.2% |
| ETH 5m | 23.3 | 35.5% | -0.124% | -43.4% | 6.3h | -13.4% |
| BTC 15m | 8.9 | 35.5% | -0.069% | -15.4% | 16.1h | -7.3% |
| ETH 15m | 19.4 | 33.5% | -0.078% | -31.5% | 9.8h | -9.1% |

All four fail G2 (winrate/PF/expectancy), G3 (OOS + MC), and G4 (hold time).
Frequency (G1) fails on BTC, passes on ETH. This is the quantified starting
point Phase 2 must move.
