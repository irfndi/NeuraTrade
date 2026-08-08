# Universe Funnel Design — 2026-08-08 (research phase)

**Goal:** detect ALL market futures (~2000), filter through a cost-ordered
multi-stage funnel until eligible to trade, and manage the
profitability-vs-trade-speed tradeoff (target 5–50 fills/day, account-scaled;
$1000 reference). Research by 3 parallel agents on live code + sweep data.

## Key empirical facts

- USDT-FUTURES = **741 contracts** today; ~2000 requires adding USDC-FUTURES +
  COIN-FUTURES (the trading client `BitgetClient` already supports all three
  product types; the gateway hardcodes USDT-FUTURES).
- A single gated symbol yields ≤0.6 fills/day (BTC 16 passers: 0.218–0.252/d;
  SOL 5 passers: 0.267–0.596/d). **5–50/day requires pooling 10–100+ symbols.**
- Small-step grids fail the readiness gates on BTC (0/576 PASS — fee drag +
  adverse selection), but pass universe walk-forward on some altcoins (ADA
  step 0.5, BLESS step 0.5 +17.95%) — the universe funnel and the readiness
  board serve different purposes and must stay separate.
- Full-market backfill cost: 750×500-candle ≈ 39 min sequential; 2000 ≈ 105
  min (breaks the 50–60 min budget). Steady-state incremental = 1
  req/symbol/cycle ≈ 13 min (750) / 35 min (2000). Rate limits are survivable
  (existing 429 backoff); latency + pacing is the binding constraint.
- **Biggest structural gap:** `runMarketUniverseScan` persists NOTHING — every
  6h cycle refetches from scratch. And the watchlist persists only
  step/grids/pause — **target_ratio/chop_gate_adx are lost**, so the soak
  cannot reproduce a validated config.

## Funnel architecture (cost-ordered stages)

```
~2000 contracts (USDT+USDC+COIN futures)
  │ 0. enumerate + registry            (1 req/symbol/cycle after backfill)
  │ 1. market screens: 24h volume ≥1M, price/spec sanity
  │ 2. cheap stats: volatility, ADX regime, fill frequency
  │    (200–500 bars, no walk-forward)
  │ 3. walk-forward survival           (existing: grid-universe.ts)
  │ 4. gate-scored validation          (existing: grid-validation.ts
  │    windows>50%, comp≥0, dd≤15%, OOS≥30, confLB≥0, stressLB≥0, fresh≤48h)
  │ 5. frequency-targeted selection    (NEW: greedy top-K by edge)
  ▼
eligible set → soak with per-symbol capital allocation
```

Pass-through estimate: 2000 → 1500 (volume) → 300 (stats) → 50
(walk-forward) → 10–20 (gates) → top-N by edge to hit the fills/day target.

## Frequency + account model (ResearchFrequency)

- fills/day per candidate ≈ k × (daily range % / step %) × fill-gate factor;
  calibrated on real sweep data (BTC + SOL fit ±7%).
- Selection = greedy top-K by edge/trade (degenerate knapsack — exact, weights
  ≪ capacity), per-symbol fill cap (≤10/day) to avoid concentration.
- Account scaling: `target = clamp(5, 50 × A/1000, 50)`; per-trade risk
  `r = 2% / (N × p_loss)` derived from the 2% daily loss cap; position size =
  `risk × A / step%`.
- Watchlist schema additions: `target_ratio, chop_gate_adx, oos_trades,
  fills_per_day, edge_per_trade_pct, volatility, allocated_weight` — the soak
  needs these to reproduce validated configs and size positions.

## Enumeration pipeline (ResearchEnumeration)

- New tables: `symbol_registry (symbol, product_type, listed_at, volume_24h,
  last_scanned_at, candle_freshness, status)` + write `trading_pairs.volume_24h`
  (column exists, never written).
- Incremental candle fetch: `getCandleRange` MIN/MAX per symbol → fetch only
  newer bars (1 req/symbol/cycle steady-state); backfill in volume order,
  resume-safe.
- Gateway gaps: fetchSymbols drops specs (launchTime, symbolType) — use
  `BitgetClient.getContracts` for the registry; multi-product enumeration;
  surface rate-limit headers.

## Batch digestion (larger batches, better opportunity finding)

The funnel is batch-shaped by construction — the expensive stages never scale
with the batch:

- **Stage 0–1 (whole market, cheap):** enumerate (~2000 contracts, 1 request);
  volume screen = ONE request for the entire market (`fetch24hrVolumes`);
  spec sanity from the contracts endpoint.
- **Stage 2 (whole market, cached):** volatility/ADX/fill-freq from the candle
  cache — zero requests once the cache is warm; the incremental tail keeps it
  warm at 1 request/symbol/cycle.
- **Concurrency:** tail-fetch with 2–3 workers (per-request pacing preserved,
  ~300ms) → ~2000 symbols in ~8–12 min instead of ~35.
- **Two-phase cadence:** discovery pass (stages 1–2 over ALL symbols) daily;
  deep pass (walk-forward + gates on survivors) every 6h; new listings
  fast-tracked through the funnel via the registry's launchTime.
- **Candidate pool, not a single list:** the deep pass ranks survivors by
  (edge/trade × fills/day); the frequency-targeted selection picks the day's
  top-K portfolio from the pool. Filtering finds *eligible*; ranking finds
  *best*.

## Implementation order (scan universe first)

1. **Symbol registry + incremental candle cache** — persist what the market
   scan fetches; registry table; volume_24h writes; incremental resume.
2. **Persist full candidate params** in the watchlist (target_ratio, ADX,
   oos_trades, fills/day, edge) — fixes the reproduction gap.
3. **Funnel stages in the scan** — volume → cheap stats → walk-forward →
   gate-scored → selection; costs ordered, later stages on survivors only.
4. **Frequency-targeted selection + account scaling** — greedy top-K,
   per-symbol allocation, soak consumes allocated_weight.
5. **Multi-product enumeration** (USDC/COIN futures) for the ~2000 ceiling.

Locked-candidate readiness gates (BTC/SOL cohort) stay as-is — the funnel
feeds the exploratory soak; the readiness cohort keeps its per-locked-candidate
gate board.
