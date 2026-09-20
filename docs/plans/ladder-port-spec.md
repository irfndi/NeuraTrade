# P5 Step 3 — Multi-Rung Ladder Port Spec (slice-by-slice)

**Status:** spec only. No code changes in this document's slice. Branch
`feat/rust-bend-strangler-p0-p5`.

**Citation staleness warning:** `crates/nt-grid/src/engine.rs` is under
active concurrent edit by sibling agents (`ResumeState` / fee-split work), and
its line numbers shifted twice while this spec was being written. Every
`engine.rs:N` citation below was verified against the tree at the time of
writing; re-pin them before implementation starts. Citations against
`ladder-engine.ts` / `types.ts` are stable (TS is sunset, not being edited).

**Ground truth (TS, being sunset):** `services/neuratrade-cli-ts/src/paper-trading/ladder-engine.ts`
(2099 lines, `engineVersion ladder-engine/v2`), state shapes in
`services/neuratrade-cli-ts/src/paper-trading/types.ts`.

**Rust target:** `crates/nt-grid/src/engine.rs` (`run_paper_engine` /
`run_paper_engine_from`) is a **single-position** grid engine ported from
`src/paper-trading/grid-engine.ts` (`engine.rs:1-12`). It cannot express N
armed rungs, two independent side ladders, or per-rung take-profits. The
multi-rung ladder that the Bend-parity example uses
(`crate::GridConfig` / `crate::evaluate`) is explicitly "a different,
unrelated model (portfolio ladder vs. single position)" (`engine.rs:11-12`).
**Why the port is required:** the box soaks run the ladder engine
(`docs/plans/2026-09-18-rust-bend-strangler-e2e.md:434` — "the box soaks run
the LADDER engine (`ladder iter over 1 bars`, multi-rung). A fill/PnL diff
between them is meaningless"), so Gate 4 cannot be read until Rust can run
the same state machine.

**Precedent for narrowing scope:** `engine.rs:5-7` fixes leverage at 1 and
drops trend filter, chop gate, max-hold-bars and pause-after-loss-bars, with
the ruling documented in the module header. This spec follows the same
pattern: Slice 1 is **leverage 1 only**, and every dropped feature is named
with the reason it is deferred.
## Soak config (the port target, verbatim)

| Knob | Value | Source |
| --- | --- | --- |
| rungs | 2 | `services/neuratrade-cli-ts/autoresearch/results/champion-soak.json:3` |
| gridStepPct | 1.3 | `champion-soak.json:4` |
| gridMaxGrids | 2 | `champion-soak.json:5` |
| gridPauseAfterLossBars | 2 | `champion-soak.json:6` |
| stopRatio | 1.58 | `champion-soak.json:7` |
| targetRatio | 1.95 | `champion-soak.json:8` |
| maxHoldBars | 39 | `champion-soak.json:9` |
| trendFilterPeriod | 0 | `champion-soak.json:10` |
| chopGateAdxThreshold | 0 | `champion-soak.json:11` |
| positionFraction | 1 | `champion-soak.json:12` |
| honestFees | maker 0.02 / takerExit 0.06 | `champion-soak.json:21` |
| --leverage | **1** | `ecosystem.champion-soak.config.cjs:123-124` |
| --max-position-size-pct | 50 | `ecosystem.champion-soak.config.cjs:132-133` |
| --fee | 0.02 | `ecosystem.champion-soak.config.cjs:111-112` |
| --slippage-bps | 2 | `ecosystem.champion-soak.config.cjs:113-114` |
| --live-entry-cross-bps | 15 | `ecosystem.champion-soak.config.cjs:120-121` |
| --capital | 200 | `ecosystem.champion-soak.config.cjs:125-126` |
| --timeframe | 15m | `ecosystem.champion-soak.config.cjs:102-103` |
| --config-mismatch-action | force-reseed | `ecosystem.champion-soak.config.cjs:152-153` |

`--leverage 1` is verified in the shared `championArgs` array
(`ecosystem.champion-soak.config.cjs:95-165`), consumed by both units
`neuratrade-champion-paper` (`ecosystem.champion-soak.config.cjs:167-169`)
and `neuratrade-champion-demo` (`ecosystem.champion-soak.config.cjs:190-192`).
Neither unit passes `--fully-dynamic-leverage` or `--max-leverage`, so
`dynamicLeverage` (`ladder-engine.ts:1198`) and `accountScaledLeverageCap`
(`ladder-engine.ts:1165`) never influence the soak.

**Deferred, therefore out of Slice 1:** `dynamicLeverage` / `fullyDynamicLeverage`
(`ladder-engine.ts:1198-1236`, options at `ladder-engine.ts:120-126`),
`accountScaledLeverageCap` (`ladder-engine.ts:1165-1195`), and the leverage
branch of `ladderRungQty` (`ladder-engine.ts:1243-1333`). At leverage 1
`liquidationPrice` returns 0 (`ladder-engine.ts:476` `if (l <= 1) return 0;`),
so the liquidation exit path is dead in the soak config.

---

## 1. State model and resume shape

### TS state (source of truth)

`WorkingState` (`ladder-engine.ts:255-265`) holds, per symbol: `capital`,
`peak`, `totalWins`, `totalLosses`, `longRungs[]`, `shortRungs[]`,
`longBase`, `shortBase`, `paused`. It is a direct in-memory mirror of the
persisted `LadderPaperState` (`types.ts:275-306`) via `stateToWorking`
(`ladder-engine.ts:1039`) and `workingToState` (`ladder-engine.ts:1053`).

`LadderPaperRungState` (`types.ts:248-267`) per rung: `rungIndex`, `side`,
`level` (pre-slippage entry level), `step` (the step **in force at seed
time** — note: persisted per rung, not derived from the base), `filled`,
`entryPrice` (post-slippage), `entryBar` (window-relative), `entryTimestamp`
(absolute ms), `filledQty` (persisted at entry so the close sends the entry
size — `types.ts:261-267`).

Two independent side ladders coexist: `longBase` / `shortBase` are each
anchored to the `candle.open` of the bar that seeded that side
(`ladder-engine.ts:622`), and each side's rungs are
`base ∓ rungIndex * step` with `step = candle.open * gridStepPct/100`
(`ladder-engine.ts:591-593`, `ladder-engine.ts:927`).

**Tick model:** `runLadderPaperTradingIteration` (`ladder-engine.ts:1980`)
loads state (`ladder-engine.ts:1994-1996`), fetches candles
(`ladder-engine.ts:1997-2002`), computes the first unprocessed index from
`state.lastTimestamp` (`resolveLadderStartIndex`, `ladder-engine.ts:1714`,
applied at `ladder-engine.ts:2006`), then loops `for (let i = startIndex; i
< candles.length; i++)` calling `advanceLadderBar` once per candle
(`ladder-engine.ts:2046-2047`), and persists
(`ladder-engine.ts:2079-2080`). So the engine is a **tick engine that can
process more than one bar per invocation** when it falls behind — the
invocation boundary is "all candles newer than `lastTimestamp`", not
"exactly one bar". Any Rust tick loop must reproduce the same
`lastTimestamp` cursor semantics, not a single-bar slice.

**Rollback:** if live execution of a bar's events fails, `WorkingState` is
restored from a pre-bar snapshot and re-persisted (`ladder-engine.ts:1888-1890`),
so the persisted state is the paper ledger, and a failed live order never
advances it (`ladder-engine.ts:1861-1866`, `1887-1898`).

### Required Rust resume shape

`ResumeState` (`engine.rs:188-208`) is single-position: one
`Option<ResumePosition>` (`engine.rs:162-167`), plus `closed_realized`,
`peak`, `window_fills`, `bar_offset`, `event_count`, `cum_*`. The ladder port
needs a **different** resume payload; the port must add a parallel type
rather than overload `ResumePosition` (which encodes one side + one price +
one qty).

Required fields (1:1 with `WorkingState` + `LadderPaperState`):

| Rust field | TS source | Notes |
| --- | --- | --- |
| `capital: Money` | `state.capital` (`types.ts:281`) | micro-USDT |
| `peak: Money` | `state.peakCapital` (`types.ts:282`) | |
| `total_wins` / `total_losses: u64` | `types.ts:283-284` | needed for `accountDrawdownBreached`-adjacent reporting only |
| `long_rungs: Vec<Rung>` / `short_rungs: Vec<Rung>` | `types.ts:285-286` | each `Rung` = `rung_index`, `level: Money`, `step: Money`, `filled`, `entry_price: Money`, `entry_bar: usize`, `entry_ts_ms: i64`, `filled_qty: i64` |
| `long_base` / `short_base: Money` | `types.ts:287-288` | |
| `paused: u64` | `types.ts:289` | |
| `last_ts_ms: Option<i64>` | `types.ts:305` | the tick cursor |
| config fingerprint | `types.ts:291-303` | `gridStepPct`, `gridMaxGrids`, `gridPauseAfterLossBars`, `rungs`, `targetRatio`, `onlyWithTrend`, `chopGateAdxThreshold`, `maxHoldBars`, `stopRatio`, `conservativeIntrabar` |

`entryBar` is window-relative in TS (`types.ts:260-261` "Window-relative bar
index"), which is only meaningful inside one candle fetch. The Rust tick loop
must carry an absolute bar counter (or the existing `bar_offset` idea from
`engine.rs:195`) so `conservativeIntrabar`'s `rung.entryBar <
ctx.barIndex` comparison (`ladder-engine.ts:849-851`) stays correct across
ticks. **Inference:** TS gets away with a window-relative index because
`resolveLadderStartIndex` returns an index into the freshly fetched array;
the port must define whether `entry_bar` is stored relative or absolute —
recommend absolute (`bars_consumed`), recorded as a deviation if relative.

Equity continuity: `capital` compounds on every close
(`ladder-engine.ts:328` `w.capital = Decimal.max(0, capitalBefore.mul(1 +
equityReturn).plus(funding))`), and `peak` ratchets
(`ladder-engine.ts:329-330`). The Rust port must apply the same compounding
into the persisted capital — `engine.rs:154-156` currently holds `capital`
fixed for the whole run and documents that as a first-pass simplification,
so this is a real behavioural change the port must carry.

### Slice 1 exit evidence (state + resume)
A resume round-trip test: seed → fill rung 1 on bar k → persist → new tick
from the persisted state → rung 1 closes at target on bar k+j and capital
matches the TS fixture for the same bars; `paused`, `longBase`, `shortBase`
and `last_ts_ms` survive unchanged when nothing traded.

---

## 2. The `a07b3dd0` port rule (anti-pattern to NOT re-import)

Commit `a07b3dd0` ("fix(cli-ts): keep armed rungs across flat bars + trend
seed note (Gate3, clever-cabin-85m)", verified present and an ancestor of
HEAD) fixed re-centering on every bar's open, which "let sideways price
chase rungs forever (demo 220 seedless HOLDs). Gates now apply to the
empty-seed only; blocked bars no longer wipe armed rungs."

The rule in code, `seedLadderSide` (`ladder-engine.ts:603-628`):

```
603  function seedLadderSide(ctx, side): void {
605    const rungs = sideRungs(w, side);
606    if (rungs.some((rung) => rung.filled)) return;      // open fills: never re-seed
608    // Keep armed rungs across flat bars: gates apply to the empty-seed only —
609    // a blocked bar must NOT wipe armed rungs (clever-cabin-85m).
610    if (rungs.length > 0) return;                        // armed rungs: never wipe
615    const allowed = !accountDrawdownBreached(w, opts) && !ctx.chopGateActive && trendAllows;
617    if (!allowed) { setSideRungs(w, side, []); setSideBase(w, side, 0); return; }
622    setSideBase(w, side, candle.open);
623    setSideRungs(w, side, buildSideRungs(side, candle.open, ctx.step, ctx.rungCount));
```

Precedence is: (a) any filled rung → return, side is managed, never
re-seeded; (b) armed-but-unfilled rungs exist → return, the ladder is kept
as-is; (c) **only** when the side's rung list is empty do the gates
(`accountDrawdownBreached`, `chopGateActive`, `trendAllows`) decide between
seeding at `candle.open` and explicitly clearing.

The anti-pattern: applying the gates (or the `setSideRungs(w, side, [])`
clear) on a non-empty rung list. That is the pre-`a07b3dd0` behaviour and it
is what produced 220 seedless HOLDs.

**Why the Rust port is at risk of re-importing it:** `run_paper_engine`
re-derives levels from the current bar's open on every flat bar
(`engine.rs:313-318`: `let step = Money(scale(candle.open.0, cfg.step_bp,
10_000));` then `buy_level = candle.open - step`, `sell_level = candle.open
+ step`). A naive ladder port that keeps that flat-bar re-derivation and
adds a gate check before it will wipe armed rungs on any blocked bar —
exactly the `a07b3dd0` bug. **The port must anchor levels on the empty seed
only:** once a side's rung list is non-empty, the rungs' persisted `level`
and `step` (`types.ts:252-254`) are the only source of truth for touch and
target, and no bar's open may move them.

### Slice 2 exit evidence (seed rule / a07b3dd0 invariant)
A regression test named for the rule: with a blocked gate (drawdown kill or
trend disallow) active while rungs are armed but unfilled, assert the rung
list and base are **unchanged** after the bar, and that the next unblocked
bar fills at the originally seeded level. A second test asserts that when
the rung list is empty and the gate blocks, the list stays empty and base is
0. Both must fail if the gate is applied unconditionally.

---

## 3. Rounding policy decision

**TS:** all sizing is `decimal.js` `Decimal` (`utils/money.ts:9-15`, `Money =
Decimal`). Per-rung allocation is `capital * positionFraction / rungs`
(`ladder-engine.ts:1145-1150`), qty is `perRungAllocation / fillPrice`
(`ladder-engine.ts:1274` `marginSizedRaw`), clamped by the notional cap
(`ladder-engine.ts:1278-1285`), then contract-rounded by `orderableQty`
(`types.ts:31-50`: ceil to `qtyStep`, floor back down if the up-round breaks
the cap, then raise to `minQty`). PnL is a Decimal ratio
(`ladder-engine.ts:312-316`) and capital compounds multiplicatively
(`ladder-engine.ts:328`).

The `.times(0.9999)` factor appears exactly once in the whole file —
`ladder-engine.ts:1382`, inside the **live** risk-guard `positionValue`
check, not in the paper sizing path. It is a live-order haircut so the guard
sees a slightly smaller position than requested. It is not part of paper
fill accounting.

**Rust:** `Money(i64)` micro-USDT (`crates/nt-risk/src/lib.rs:9`), and all
ratio math is `scale(a, num, denom)` — truncating toward zero
(`engine.rs:118-120`), including slippage (`engine.rs:129-134`), sizing
(`engine.rs:335-336`) and target delta (`engine.rs:414`).

**Decision: mirror-exact is not achievable; use explicit micro-tolerance.**
Rationale:

- TS's `Decimal` division is exact at 28+ significant digits; Rust's
  `scale()` truncates to integer micros. Even a pure `a * num / den` form
  diverges by at most 1 micro per operation, but the ladder compounds: rung
  sizing → fee (`crates/nt-execution/src/lib.rs:63-66`, `fee = notional *
  fee_bp / 10000`, itself truncating) → capital
  (`capital * (1 + equityReturn)`) → next rung sizing. The divergence is
  not a single bounded 1-micro step; it is a random walk over N rungs × M
  bars.
- The frozen grid-parity fixture already accepts exactly this class of
  difference: `crates/nt-grid/examples/grid_parity.rs:21` documents "Long
  delta <= 1u; short delta <= 3000u (bound = 1.01 * the measured ~2525u)",
  and `docs/plans/2026-09-18-rust-bend-strangler-e2e.md:446` records the
  grid half closed with `long` delta 0u and `short` 2525u. **Parity in this
  project already means "bounded integer divergence", not bit-equality.**

**Policy to implement (state in the port, do not silently choose):**

1. Money is integer micros everywhere in Rust; **no float in `crates/`** —
   unchanged from `engine.rs:19`.
2. Per-operation rounding is truncating `scale()`, matching `engine.rs:118`.
3. Quantities: TS `orderableQty` ceils to `qtyStep` then may floor back
   (`types.ts:37-45`). Rust must implement the **same two-step rule**, not
   plain truncation, or a min-qty rung will differ in size, not just in
   rounding. The port either reimplements `orderableQty` in integer micros
   or carries the contract spec into `nt-grid`. **Decision:** implement the
   same ceil-then-floor-back rule; Slice 1 may omit `contractSpecs` only if
   the soak symbols are configured without specs — verify against the live
   CLI flag before dropping it.
4. Every parity assertion uses an explicit per-leg micro budget derived
   from the fixture (same shape as `grid_parity.rs:110-141`), and the
   budget is a **measured bound × 1.01**, never widened to force green
   (matches the standing constraint "Do not widen budgets to force green").
5. The `.times(0.9999)` live haircut (`ladder-engine.ts:1382`) is out of
   scope for Slice 1 (live-path only) and must be re-added when the Rust
   live path exists — record it as a named deferral, not a permanent drop.

### Slice 3 exit evidence (fill + sizing)
A ladder-parity example replaying a frozen TS ladder fixture (rungs=2,
step 1.3, target 1.95, stop 1.58, lev 1) asserting, per rung: side match,
`entry_price` within budget, exit price within budget, and total capital
delta within a stated micro budget — with the budget printed next to the
measured max delta, exactly like `grid_parity.rs`.

---

## 4. Step-anchoring divergence (decide and record)

**TS:** `step` is re-derived from the **current** bar's open every bar
(`ladder-engine.ts:927` `step: candle.open * (opts.gridStepPct / 100)`,
inside `createLadderBarContext`, called per bar at
`ladder-engine.ts:963-972`). Two consequences:

- Touch for an **armed** rung uses the current bar's `step` for nothing —
  the rung's persisted `level` is fixed at seed time (`ladder-engine.ts:591-593`).
  So `step` only matters for the **empty seed** (level construction,
  `ladder-engine.ts:623`) and for the **stop boundary** when `stopRatio > 0`
  (`ladder-engine.ts:681-682` uses `ctx.step`, the current bar's step).
- The **target** uses the rung's own persisted `step`: `rung.entryPrice ±
  rung.step * ctx.targetRatio` (`ladder-engine.ts:843-846`). So the target
  is anchored to the step in force when the rung was seeded, and the ratio
  in force on the exit bar.
- The **legacy stop boundary** uses the side base: `sideBase ± ctx.step *
  (rungCount + gridMaxGrids)` (`ladder-engine.ts:685-686`). With the soak's
  `stopRatio = 1.58 > 0`, the boundary is instead
  `min/max(entryPrice) ± ctx.step * stopRatio` (`ladder-engine.ts:679-682`)
  — i.e. **the current bar's step times the ratio**, not the entry step.

**Rust:** `step` is likewise re-derived per bar (`engine.rs:313`), and
target/stop deltas are computed from that per-bar `step` against the
position's `entry_price` (`engine.rs:414-423`).

**The divergence:** when a position spans bars and the open moves, TS uses
the **entry-time** `rung.step` for the target (`ladder-engine.ts:845`) but
the **exit-bar** `ctx.step` for the stop boundary
(`ladder-engine.ts:681`). Rust uses the **exit-bar** step for both
(`engine.rs:414-423`). So on a multi-bar hold with a drifting open, TS's
target and Rust's target can sit at different prices.

**Named deviation: `LADDER-STEP-ANCHOR`.**

**Decision: freeze at entry.** For the target, use the rung's persisted
`step` (mirror-exact with TS `ladder-engine.ts:845`). For the stop boundary
under `stopRatio > 0`, use the **entry-time** step of the rung(s) being
stopped, i.e. the same frozen step, rather than the exit bar's step.

Reason: the `a07b3dd0` rule (Section 2) already freezes the geometry at the
empty seed — a rung's `level` and `step` are immutable once armed
(`types.ts:252-254`, and `seedLadderSide` never touches a non-empty list,
`ladder-engine.ts:606-610`). Re-deriving the stop from a *later* bar's open
is the same class of bug as the re-centering that `a07b3dd0` removed: the
rung's risk level would drift with price after entry. It also makes the
deviation one-directional and explainable: the frozen-step stop is always
derivable from the entry bar alone, so a shadow replay can reproduce it from
persisted state with no dependence on which bar the exit lands on.

Consequence to state explicitly: this is a **deliberate divergence from TS**
for the `stopRatio > 0` boundary (`ladder-engine.ts:681`). It is bounded by
`|step_exit - step_entry| * stopRatio` per stopped rung, which at
gridStepPct 1.3 / stopRatio 1.58 is small relative to the entry, but it is
NOT zero — the shadow diff must carry it as a named, quantified line item,
not fold it into "unexplained".

`conservativeIntrabar` (`ladder-engine.ts:849-851`, option default true at
`ladder-engine.ts:153-156`) also belongs here: a rung filled on bar N may
not take profit on bar N. The Rust port already has the single-position
equivalent of this rule in prose (`engine.rs:140-141` "a position opened
during bar N is first eligible for an exit check on bar N+1") but must apply
it **per rung**, not per position — with N=2 rungs, rung 1 filled on bar N
and rung 2 filled on bar N+1 have different same-bar eligibility.

### Slice 4 exit evidence (exits)
A fixture case where a rung is filled on bar k and the open moves ≥ 5% by
bar k+3, asserting the Rust target equals `entry_price + rung.step *
targetRatio` computed from the **entry** bar, and that the recorded
deviation budget accounts for the frozen-step stop difference.

---

## 5. Fee model

**TS (`closeRung`, `ladder-engine.ts:285-333`):**

```
301  const makerFee = (opts.feePct ?? 0) / 100;            // 0.02 → 0.0002
302  const takerFee = (opts.takerExitFeePct ?? opts.feePct ?? 0) / 100;
307  const crossFee = Math.max(0, opts.liveEntryCrossBps ?? 0) / 10000;  // 15 → 0.0015
308  const targetFee = makerFee * 2 + crossFee;            // entry + exit, both maker
309  const stopFee   = makerFee + takerFee + crossFee;     // entry maker, exit taker
311  const fee = reason === "target" ? targetFee : stopFee;
316  const net = pricePnl - fee;
328  w.capital = Decimal.max(0, capitalBefore.mul(1 + equityReturn).plus(funding));
```

So the soak's fee is a **fraction of the position**, charged once at close
(both legs), deducted from the return, then multiplied by `sizePerRung =
positionFraction / rungs` (`ladder-engine.ts:299-300`) to get the equity
impact. Note the entry fee is **not** charged at fill time in the paper
ledger — it is folded into the close's `fee` and only then applied to
capital.

**Soak values:** `--fee 0.02` (`ecosystem.champion-soak.config.cjs:111-112`),
`--live-entry-cross-bps 15` (`ecosystem.champion-soak.config.cjs:120-121`),
and `honestFees` = "maker0.02/takerExit0.06 rescored 2026-09-07"
(`champion-soak.json:21`). The taker 0.06 comes from `takerExitFeePct`,
defaulting to `feePct` when unset (`ladder-engine.ts:302`) — the soak config
does **not** pass `--taker-exit-fee-pct`, so **the deployed soak charges 0.02
taker on stops, not 0.06**, and `honestFees` describes the *research* fee
schedule rather than the live soak's. This is a real, citable difference
between the research scoring and the box soak and must be called out in the
port's fee decision rather than silently reconciled.

**Rust (current tree — the split ALREADY EXISTS, do not re-implement):**
`HONEST_MAKER_FEE_BP = 2` and `HONEST_TAKER_EXIT_BP = 6`
(`crates/nt-execution/src/lib.rs:21`, `crates/nt-execution/src/lib.rs:31`),
charged per order as `fee = |notional| * fee_bp / 10000`
(`crates/nt-execution/src/lib.rs:62-64`). `PaperEngineConfig` carries **two**
rates — `fee_bp` (taker) at `engine.rs:72` and `maker_fee_bp` at
`engine.rs:77` — and the exit order selects between them by reason
(`engine.rs:499-503`): `FillReason::Target` pays `cfg.maker_fee_bp` (a
resting limit fill), everything else pays `cfg.fee_bp`. Entries pay taker
(`engine.rs:383`). The CLI exposes both, defaulting to the honest constants
(`crates/nt-cli/src/main.rs:53-54`, flags at `crates/nt-cli/src/main.rs:81-93`,
rationale at `crates/nt-cli/src/main.rs:486-491`). So the maker/taker split
by exit reason is **already shipped** — the port must reuse it, not rebuild
it.

### The two real gaps

**(1) Entry-cross fee — dominant, and Rust has no term for it.** TS adds
`crossFee = liveEntryCrossBps / 10000` to **both** fee lines
(`ladder-engine.ts:307-309`): `targetFee = makerFee*2 + crossFee` and
`stopFee = makerFee + takerFee + crossFee`. At the soak's
`--live-entry-cross-bps 15` (`ecosystem.champion-soak.config.cjs:120-121`)
that is **+0.0015 on every close, on both target and stop**. Rust's
`submit` computes `fee = |notional| * fee_bp / 10000` with no cross
component at all (`crates/nt-execution/src/lib.rs:62-64`), and slippage
lives only in `worse_price` on the **price** path (`engine.rs:129-134`) —
the cross is a *fee*, not a price, in TS, so it cannot be folded into
slippage without double-counting or mis-attributing. This is the largest
single fee divergence, larger than the maker/taker asymmetry it gets
confused with.

**(2) Effective rates — the deployed soak has NO maker/taker asymmetry.**
With `--fee 0.02` (`ecosystem.champion-soak.config.cjs:111-112`) and no
`--taker-exit-fee-pct` anywhere in the ecosystem config (verified: zero
grep hits for "taker" in that file), `takerFee` falls back to `feePct`
(`ladder-engine.ts:302`), so:

```
targetFee = 0.0002 * 2 + 0.0015 = 0.0019
stopFee   = 0.0002 + 0.0002 + 0.0015 = 0.0019
```

**Both are exactly 0.0019.** The box soak therefore charges the *same* rate
on target and stop, and 0.0019 — not the `honestFees` 0.0002/0.0006
research schedule (`champion-soak.json:21`), which describes the autoresearch
scoring, not the deployed PM2 run.

**The residual, per leg, at Rust's DEFAULT 6bp taker / 2bp maker.** The two
engines charge on different bases — Rust charges **per order** at fill time
(`crates/nt-execution/src/lib.rs:62-64`), TS charges **once, at close**, as
one fraction of the position covering both legs plus the cross
(`ladder-engine.ts:308-311`, `:316`). So the comparison is per-leg, and the
cross term is inside every TS number:

| Leg | Rust (default 6/2bp) | TS (soak, at close) | Ratio | Direction |
| --- | --- | --- | --- | --- |
| Entry | 0.0006 (`engine.rs:383`, `cfg.fee_bp`) | 0.0002 folded into the close (`ladder-engine.ts:308-309`) | 3.0x | Rust overcharges |
| Target exit | 0.0002 (`engine.rs:499-500`, `maker_fee_bp`) | 0.0019 (`ladder-engine.ts:308`) | 9.5x | Rust **undercharges** |
| Stop exit | 0.0006 (`engine.rs:501-502`, `cfg.fee_bp`) | 0.0019 (`ladder-engine.ts:309`) | 3.2x | Rust overcharges |

The **target leg is the dominant error and it is an undercharge** — Rust
0.0002 against TS 0.0019, because TS's `targetFee` is `makerFee * 2` (both
legs) plus the 15bp cross, while Rust's target exit is a single 2bp maker
order with no cross term. Sizing the residual as "3x on stops" understates
the target leg by roughly 7x, which is exactly the error that would make a
shadow diff look smaller than it is.

An unflagged `nt-cli shadow` therefore overcharges entries 3.0x, overcharges
stops 3.2x, and **undercharges targets 9.5x** — the target leg dominates and
it runs the opposite way from the other two, so the three do not cancel.

**Requirement for Gate 4 — pick one, record it:**

- **Option A (recommended): make the shadow comparable.** Run the shadow
  with `--fee-bp 2 --maker-fee-bp 2` so both TS lines (0.0019 each) and the
  Rust lines are on the same 2bp base, then carry the 15bp cross as the one
  named, quantified residual — because Rust has no cross term, the residual
  is `+0.0015 * notional` per close, which is deterministic and itemisable,
  not "unexplained".
- **Option B: re-baseline the soak** to `--taker-exit-fee-pct 0.06` (and
  `--fee`/`--maker-fee-bp` matching), then re-run the soak and re-attribute
  the diff against Rust's honest 6/2bp defaults.

Do not run the comparison at Rust's default 6/2bp against the soak's
effective 2/2 — that conflates a config difference with a port defect, and
the resulting diff is not interpretable.

### Slice 5 exit evidence
For each closed rung in the ladder parity example, the Rust fee line must be
within budget of TS's `closeRung` line for the same reason, **with the cross
component itemised separately** so the `liveEntryCrossBps` term is visibly
accounted for rather than absorbed into the rounding budget. Plus one
explicit assertion that the shadow was run at `--fee-bp 2 --maker-fee-bp 2`
(or the recorded Option B equivalent), so a future reader can tell which fee
basis the diff was measured on.

---

## 6. Gate dependencies

**Gate 3 — Testnet momentum** (`docs/plans/2026-09-18-rust-bend-strangler-e2e.md:386-388`):
demo orders fill on testnet; `opened-count-demo > 0` sustained; paper-vs-demo
comparison runs on the SAME feed. Current state: `opened-count-demo=0`, Gate 3
open (`docs/plans/2026-09-18-rust-bend-strangler-e2e.md:408`, `:437`).
Evidence that closes it: the monitor's `OPENED` count, which greps the demo
out-log for `OPENED` entry lines and alerts when it increases
(`services/neuratrade-cli-ts/scripts/champion-soak-monitor.sh:222-241`,
state file `opened-count-$home_name.txt`), strictly increasing over a stated
window, with no sizing/min-capital rollbacks. Gate 3 does **not** depend on
the Rust port (it is about the TS demo filling) — but it is the precondition
that makes Gate 4's comparison meaningful: a demo that never fills produces
an empty diff that proves nothing. **Ordering consequence: Gate 3 evidence
must exist before Gate 4's ladder diff is worth running.**

**Gate 4 — Parallel live-shadow** (`docs/plans/2026-09-18-rust-bend-strangler-e2e.md:389-391`):
`nt-cli` paper `--shadow` replicates the champion-paper loop with 0
unexplained PnL/fill divergence. Current state: grid half closed, ladder half
open (`docs/plans/2026-09-18-rust-bend-strangler-e2e.md:446-451`); the
`--shadow` path exists and holds resume state
(`crates/nt-cli/src/main.rs:11-14`, `:242`), and the P5 shadow sizing bug
(`--pos-pct` sizing vs hardcoded approval cap) is fixed
(`docs/plans/2026-09-18-rust-bend-strangler-e2e.md:432`).

Evidence that closes the ladder half of Gate 4:

1. A frozen TS ladder fixture (same provenance pattern as
   `services/neuratrade-cli-ts/grid_parity_fixture.ts`, which exists because
   the TS engine must execute as ground truth) generated by the TS ladder
   engine at the soak's knobs.
2. A Rust ladder-parity example replaying it with per-rung side, entry,
   exit and capital assertions inside stated micro budgets.
3. A live shadow run of `nt-cli` paper `--shadow` over the same symbols,
   with a **daily** diff against the TS champion-paper ledger where every
   line item is attributed: rounding budget (Section 3), `LADDER-STEP-ANCHOR`
   stop-boundary divergence (Section 4), fee split/timing (Section 5), and
   the taker-rate question (Section 5). Only after all four are quantified
   and their sum accounts for the observed diff can "0 unexplained" be
   claimed.

---

## Slice plan

**6 slices.** Order is dependency-driven: state and resume first (nothing
else can be tested without persistence), then the seeding rule (the
`a07b3dd0` invariant), then the fill/size path, then exits, then fees, then
the live-only extras. Each slice is independently verifiable against a
frozen TS fixture.

| # | Scope (one line) | Blocked by |
| --- | --- | --- |
| **1** | **Ladder state + resume (lev 1):** `LadderRung`/`LadderState` types, per-side rung vectors + bases, `last_ts` tick cursor, capital/peak compounding, resume round-trip. | — |
| **2** | **Seed rule (`a07b3dd0` invariant):** `seedLadderSide` ported with the three-tier precedence — gates apply only to the empty seed; armed rungs are never wiped; levels anchored at seed, never re-derived from a later bar's open. | 1 |
| **3** | **Fill + sizing (lev 1):** progressive fill (`previousFilled` gating), touch on `low`/`high`, per-rung slippage, `ladderRungQty` with the leverage branch deferred, `orderableQty` rounding, floor-unorderable HOLD. | 2 |
| **4** | **Exits:** per-rung target (`entry ± rung.step * targetRatio`), `conservativeIntrabar` per rung, `maxHoldBars`, `stopRatio` boundary frozen at entry (`LADDER-STEP-ANCHOR`), liquidation deferred (dead at lev 1), pause-after-loss, account drawdown kill + peak re-anchor. | 3 |
| **5** | **Fee basis + cross term:** reuse the already-shipped maker/taker split (`engine.rs:499-503`); add the missing entry-cross term (TS `liveEntryCrossBps/10000` on both fee lines, `ladder-engine.ts:307-309`); pin the fee basis to the soak (`--fee-bp 2 --maker-fee-bp 2` or a recorded re-baseline); resolve entry-fee timing (TS folds entry into the close, Rust charges at fill). | 4 |
| **6** | **Deferred-surface triage + live parity:** `dynamicLeverage`/`accountScaledLeverageCap`/`ladderRungQty` leverage branch, `fundingRatePct8h`, `maintenanceMarginRate`, liquidation, `maxPositionDrawdownPct`, `.times(0.9999)` live haircut, `configMismatchAction` force-reseed — each either implemented or explicitly ruled out with the soak-config reason. | 5 |

**Order rationale:** 1 is a hard prerequisite (no test can run without
persisted state); 2 must precede 3 because the fill path reads the seeded
levels and a wrong seed produces wrong fills; 3 precedes 4 because exits
reference `entryPrice`/`entryTimestamp` set at fill; 5 is last of the
paper-path slices because fee divergence only becomes visible once fills and
exits are correct; 6 is a triage slice, not a feature slice — its output is a
list of what is deliberately absent and why, which is what lets Gate 4's
"unexplained divergence" claim be bounded rather than absolute.

**Note on `runs` and equity:** `estimateUnrealizedPnl`
(`ladder-engine.ts:440-472`) is mark-to-market reporting only (it feeds
`equity`/`unrealizedPnl` in the iteration result,
`ladder-engine.ts:1949-1951`) and never touches `capital`. It is required
for the shadow output to be comparable, but it is **not** on the critical
path for fill/PnL parity — fold it into Slice 1's state work or Slice 4,
whichever lands first, and record which.

---

## Explicit inferences

Marked, not assumed:

- **Inference:** TS `entryBar` is window-relative
  (`types.ts:257-258`) and the port's absolute-vs-relative decision is mine,
  not read from TS.
- **Inference:** the deployed soak charges taker 0.02 on stops (not the
  `honestFees` 0.06) because `--taker-exit-fee-pct` is absent from
  `ecosystem.champion-soak.config.cjs` and `ladder-engine.ts:302` defaults
  `takerFee` to `feePct`. The config is read; the conclusion about effective
  deployed behaviour is an inference from those two facts.
- **Inference:** the freeze-step stop boundary (Section 4) is a deliberate
  divergence from `ladder-engine.ts:681`, chosen for the stated reasons — it
  is not a TS behaviour.
- **Inference:** `contractSpecs` may be omittable from Slice 3 depending on
  whether the soak symbols are launched with contract specs; unverified, and
  flagged as a check for the implementer rather than a decision.
- **Verified, not inferred:** `--leverage 1` for both champion units
  (`ecosystem.champion-soak.config.cjs:123-124`, `:167-169`, `:190-192`);
  `a07b3dd0` is an ancestor of HEAD; the fee lines at
  `ladder-engine.ts:301-311`; the `a07b3dd0` guard at
  `ladder-engine.ts:606-610`; `HONEST_MAKER_FEE_BP = 2` /
  `HONEST_TAKER_EXIT_BP = 6` (`crates/nt-execution/src/lib.rs:21`, `:31`);
  the per-bar step re-derivation at `engine.rs:313`.
