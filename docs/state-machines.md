# State Machines — NeuraTrade TS runtime

> Living document. Every state machine the CLI runtime actually enforces, its
> invariants, its transition table, and the invalid states the code can reach
> today. Each defect has a fix status and a `bd` issue. Update the
> **Fix status** column and the changelog when a machine changes — the
> regression tests named here are the executable half of this file.

Scope: `services/neuratrade-cli-ts/src`. The Go tree (`services/backend-api`,
`cmd/neuratrade-cli`, `services/agent-control`) is frozen and out of scope; see
`AGENTS.md` → MIGRATION STATUS.

Machines:

| ID | Machine | Where | Persisted in | Fix status |
| --- | --- | --- | --- | --- |
| SM-GRID | Grid paper/live position | `paper-trading/grid-engine.ts` | `grid_paper_state` | **fixed** (RISK-01, RISK-02, RISK-04) |
| SM-LADDER | Ladder paper/live rungs | `paper-trading/ladder-engine.ts` | `ladder_paper_state`, `ladder_paper_trades` | **fixed** (RISK-02, RISK-04, RISK-05) |
| SM-FLOW | Flow Ignition single position | `scalping/flow-trade.ts` | `flow_trade_state`, `flow_trades` | **fixed** (RISK-03) |
| SM-BREAK | Daily-loss circuit breaker | `risk/circuit-breaker.ts` | `risk_circuit_breaker`, `risk_circuit_breaker_resets` | sound |
| SM-KILL | Kill switch | `risk/kill-switch.ts` | `risk_kill_switch` | sound |
| SM-GATEWAY | Gateway lifecycle | `services/gateway-orchestrator.ts`, `services/gateway-state.ts`, `services/pid.ts` | `pids/gateway-state.json`, `pids/*.pid` | **fixed** (RISK-06) |
| SM-ORDER | Exchange order/fill | `exchange/adapters/bitget-futures.ts`, `bybit-futures.ts` | exchange only | sound |
| SM-RECON | Grid live reconciliation | `paper-trading/live-position-reconciliation.ts` | derived | sound |

All six confirmed defects are fixed and each has a regression test that was
verified to fail on the pre-fix code. Fixes landed in two commits: RISK-01/02
(`ab287e916`), then RISK-03/04/05/06 (this change).

---

## SM-GRID — Grid position machine

Two orthogonal axes are stored in one row: the **position** (`side`) and the
**halt latch** (`killed`, `paused`). The engine reads the row, decides, and
writes it back once per iteration.

### States

| State | Encoding | Meaning |
| --- | --- | --- |
| `FLAT` | `side === null` | no position; entries allowed |
| `LONG` / `SHORT` | `side === "long" \| "short"` | one open position, `entryPrice`/`entryFilledQty` set |
| `PAUSED_flat` | `FLAT` + `paused > 0` | post-loss cooldown, no entries |
| `LATCHED_flat` | `FLAT` + `killed === true` | drawdown or reconciliation halt, no entries |
| `LATCHED_open` | `LONG`/`SHORT` + `killed === true` | halt latched while a position is still open |

### Invariants

| # | Invariant | Enforced at |
| --- | --- | --- |
| I1 | `side === null` ⟹ `entryPrice === 0` | `gridStateAfterPositionExit` (grid-engine.ts:1743) |
| I2 | `side !== null` ⟹ `entryFilledQty > 0` and (`entryFillSource === undefined` for legacy, or one of `simulated \| live \| adopted`) | `gridOpenedState` (grid-engine.ts:1330), `adoptedGridState` (grid-engine.ts:168) |
| I3 | `side !== null` + `isLive` ⟹ `entryFillSource ∈ {live, adopted}` ∧ `strategyConfigFingerprint` set ∧ `entryOrderId` non-empty | `gridProvenanceMismatch` (grid-engine.ts:364) |
| I4 | `killed` blocks **new entries only**; an open position keeps evaluating stop/target | `guardKilledGridState` (grid-engine.ts:594), `processGridBar` (grid-engine.ts:1899) — *was violated, see RISK-01/02* |
| I5 | A live exit must be confirmed by the venue (matching size, reducing side, positive price) before the ledger goes flat | `closeGridPosition` (grid-engine.ts:1183) |
| I6 | A state write that cannot be persisted after a venue order engages the kill switch | `saveGridStateWithRetry` (grid-engine.ts:718) |
| I7 | The drawdown limit blocks an entry **before** it is placed, not after | `processGridBar` (grid-engine.ts:1953) — *added by the RISK-01 fix* |
| I8 | The exit row and the flat state commit in ONE transaction, so a crash cannot book a close without advancing the state | `recordGridExit` (repository.ts), called from `closePosition` (grid-engine.ts:1991) |
| I9 | A live exit the venue did not confirm latches `killed` (no new entries) while still leaving the position open for retry | `closePosition` `tapError` (grid-engine.ts:2003) |

### Transition table

| From | Trigger (guard) | To | Code |
| --- | --- | --- | --- |
| `FLAT` | candle low ≤ mid−step ∧ trend allows long | `LONG` | `resolveGridEntry` (grid-engine.ts:1167) |
| `FLAT` | candle high ≥ mid+step ∧ trend allows short | `SHORT` | `resolveGridEntry` (grid-engine.ts:1174) |
| `FLAT` | drawdown ≥ `maxDrawdownPct` | `LATCHED_flat` | grid-engine.ts:1953 |
| `FLAT` | `paused > 0` | `PAUSED_flat` (decrement) | grid-engine.ts:1934 |
| `FLAT` | circuit breaker open | `FLAT` (hold) | `reconcileGridLiveState` (grid-engine.ts:864) |
| `LONG`/`SHORT` | high ≥ target (long) / low ≤ target (short) | `FLAT` | `resolveGridPositionExit` (grid-engine.ts:1660) |
| `LONG`/`SHORT` | low ≤ stop (long) / high ≥ stop (short) | `FLAT` + `paused = gridPauseAfterLossBars` | grid-engine.ts:1725 |
| `LONG`/`SHORT` | liquidation price crossed | `FLAT` + `killed` | grid-engine.ts:1689 |
| `LONG`/`SHORT` | live position missing/mismatched on the venue | `LATCHED_open` + kill switch engaged | `failGridLiveReconciliation` (grid-engine.ts:877) |
| `FLAT` | venue holds an untracked adoptable position | `LONG`/`SHORT` (`entryFillSource: adopted`) | `reconcileLivePosition` → `adoptedGridState` |
| `FLAT` | venue holds an untracked *unadoptable* position | `FLAT` after a venue close | grid-engine.ts:814 |
| `LATCHED_open` | stop/target/liquidation | `FLAT` (latch retained) | grid-engine.ts:1765 |
| `LATCHED_flat` | kill switch disengaged ∧ drawdown recovered | `FLAT` | `guardKilledGridState` (grid-engine.ts:622) |

### Reachable invalid states

**RISK-01 — the drawdown latch was armed and cleared on alternating bars (FIXED).**
`finalizeGridBar` set `killed = true` when the account was past the limit
(grid-engine.ts:1806) and `guardKilledGridState` cleared it on the next
iteration (old grid-engine.ts:587). Nothing engaged the kill switch, so the
machine flipped `LATCHED → FLAT → LATCHED` forever; and because the latch was
only armed *after* an entry, the breaching bar opened the position the limit
was meant to refuse.

- Evidence (probe, pre-fix): state `capital 19, peak 20, maxDrawdownPct 5` →
  `iter0 action=opened killed=true note="…kill switch triggered"`, `iter1..5
  action=hold killed=true note="kill switch active"`, `killSwitch.isEngaged()
  === false`.
- Fix: `gridDrawdownBreached` (grid-engine.ts:577) blocks the entry in
  `processGridBar` (grid-engine.ts:1953) and is re-checked in
  `guardKilledGridState` (grid-engine.ts:622), so `LATCHED_flat` is a real
  halt: it clears only when the drawdown itself recovers.
- Regression: `grid paper engine > keeps a drawdown-breached flat account
  halted until the peak re-anchors` and `… keeps evaluating exits on an open
  position latched by the drawdown limit`.
- Issue: `clever-cabin-…` (drawdown latch).

**RISK-02 — an open breaker skipped every exit rule (FIXED).**
`CircuitBreaker.isOpen()` gated `reconcileGridLiveState` *before* the position
branch (pre-fix grid-engine.ts:828), so an open breaker returned a HOLD and
`processGridPosition` never ran: stop, target and liquidation were all skipped
for an open live position while the account was already past its daily loss
limit.

- Evidence (probe, pre-fix): breaker open at −5% (limit 2%), live long with
  `stop` breached by the candle low → `action=hold
  note="CIRCUIT BREAKER OPEN: …" closesCalled=0 side=long`.
- Fix: the breaker gate now requires `state.side === null`
  (grid-engine.ts:864); `processGridPosition` always runs for an open
  position.
- Regression: `grid paper engine > keeps evaluating exits on an open live
  position while the circuit breaker is open`.
- Same fix in SM-LADDER: `guardLadderIteration` holds only when
  `openRungCount(w) === 0` (ladder-engine.ts:1979), regression
  `runLadderPaperTradingIteration (persistence + resume) > holds new rungs but
  keeps managing rungs while the circuit breaker is open`.
- Issue: `clever-cabin-…` (drawdown latch + breaker).

**RISK-04a — crash between the trade row and the state row double-booked an exit (FIXED).**
`closeGridPosition` used to write `grid_paper_trades` *and*
`circuitBreaker.recordTradeResult` **before** the caller persisted the flat
state in `finalizeGridBar`. A crash in that window left the ledger holding the
position while the trade row already existed, so the next iteration re-closed
the same position.

- Evidence (probe, pre-fix): iteration 1 opens, iteration 2 closes
  (`trades=1`); restoring the pre-close state (the crash window) and iterating
  again produced a second exit row
  (`exit rows = ["target:1000.495", "target:1010"]`, `trades=2`).
- Fix: `recordGridExit(trade, state)` writes the trade row, the state row and
  the day's opening-capital anchor in one SQLite transaction
  (`repository.ts`); `closeGridPosition` no longer records anything, and the
  breaker is fed after the durable write. A replay now fails on the duplicate
  primary key and leaves the state untouched.
- Regression: `PaperTradingRepositorySQLite > records the grid exit and its
  state in one transaction`, `grid paper engine > does not book a grid exit
  whose state write failed`.
- Issue: `clever-cabin-kacz` (closed).

**RISK-04b — a failed live close was retried with no latch (FIXED).**
Nothing latched and nothing engaged the kill switch, so the same reduce-only
close was re-sent next bar and new entries stayed possible: one stuck close
could become two open positions (probe, pre-fix: three consecutive
`action=THREW` iterations with `side=long`, `killed=false`).

- Fix: `closePosition` uses `Effect.tapError` to persist `killed: true` through
  `saveGridStateWithRetry` before rethrowing (grid-engine.ts:2003). The error
  still surfaces, the position stays open for the next bar to retry, and I4
  guarantees the latch cannot freeze the exit.
- Regression: `grid paper engine > latches the account when a live close is not
  confirmed`.
- Issue: `clever-cabin-kacz` (closed).

**Not a defect:** a live position closed on the venue but still open locally
(`reconcileLivePosition` → `mismatch "local state exists without exchange
position"`) engages the kill switch and requires an operator. Correct
fail-closed behaviour; recorded here so nobody "fixes" it into an auto-adopt.

---

## SM-LADDER — Ladder rung machine

`advanceLadderBar` mutates in-memory `WorkingState` bar by bar; the live
executor then replays the same events against the venue, and the state is
persisted once at the end (or rolled back in full on any venue failure).

### States

| State | Encoding |
| --- | --- |
| `FLAT` | no rung with `filled === true` on either side |
| `ARMED` | rungs seeded, none filled (`openRungCount(w) === 0`) |
| `FILLED` | one or more rungs with `filled === true` |
| `PAUSED` | `w.paused > 0`, decremented per bar |
| `DD_BLOCKED` | flat, drawdown ≥ `maxDrawdownPct`, peak re-anchored to capital |

### Invariants

| # | Invariant | Enforced at |
| --- | --- | --- |
| L1 | A filled rung carries the **entry** quantity (`filledQty`) and the close sends that exact quantity | `executeLadderFillLive` (ladder-engine.ts:1445), `executeLadderCloseLive` (ladder-engine.ts:1589) |
| L2 | A venue close that is null or partial fails the bar and rolls the ledger back | ladder-engine.ts:1589–1620 |
| L3 | The whole bar is atomic: any venue failure restores `WorkingState` and does **not** record the bar's trades | `executeLadderLiveEvents` (ladder-engine.ts:2219), `rollbackWorkingState` (ladder-engine.ts:2001) |
| L4 | An orphan venue position is closed only when the ledger knows of no rungs | `guardOrphanPosition` (ladder-engine.ts:1906) |
| L5 | `PAUSED` expires: the per-bar decrement always runs | `advanceLadderBar` (ladder-engine.ts:1031) |
| L6 | A drawdown block is a seed gate, not a freeze — open rungs keep their exits | `seedLadderSide` (ladder-engine.ts:654), `guardLadderIteration` (ladder-engine.ts:1979) |
| L7 | A config mismatch on an open ladder force-closes at the current close and re-seeds next bar | `forceReseedLadderState` (ladder-engine.ts:1742) |
| L8 | A state write that cannot be persisted after a venue interaction retries, then engages the kill switch | `saveLadderStateWithRetry` (ladder-engine.ts:2019) |
| L9 | The advanced state is persisted BEFORE the trade rows, so a failed history write cannot cause a bar replay | `runLadderPaperTradingIteration` (ladder-engine.ts:2531) |
| L10 | A close order is only sent for a rung whose entry this bar confirmed (or that was already filled before it) | `executeLadderBarLive` (ladder-engine.ts:1689) |

### Transition table

| From | Trigger (guard) | To | Code |
| --- | --- | --- | --- |
| `FLAT` | seed allowed (drawdown ok ∧ chop ok ∧ trend ok) | `ARMED` | ladder-engine.ts:654 |
| `ARMED` | bar touches a level | `FILLED` | `fillLadderSide` (ladder-engine.ts:676) |
| `FILLED` | bar reaches a rung target | `FILLED`/`FLAT` | `closeLadderTargets` (ladder-engine.ts:915) |
| `FILLED` | side stop boundary crossed | `FLAT` + `paused` | `resolveLadderRiskExit` (ladder-engine.ts:784) |
| `FILLED` | `maxHoldBars` exceeded | `FLAT` (`max_hold`) | ladder-engine.ts:974 |
| any | config drifted from the persisted state | force-close then `FLAT` | `forceReseedLadderState` (ladder-engine.ts:1742) |
| `FILLED` | venue fill/close unconfirmed | same state (bar rolled back) | ladder-engine.ts:2219 |
| `FILLED` | circuit breaker open | `FILLED` (exits still managed) | ladder-engine.ts:1979 |

### Reachable invalid states

**RISK-05 — a skipped entry could still have its close sent (FIXED).**
`executeLadderBarLive` runs all entries, then all closes, and never checked
that a close's matching entry succeeded. A rung the executor skips because its
size is below the venue floor (`qty === 0`) still produces a paper close event
on the same bar, and that close was sent as a reduce-only order with no
position behind it — the `40020` failure the comment at ladder-engine.ts:1659
says was fixed. The loop order was the only thing preventing it.

- Fix: `executeLadderBarLive` builds a `confirmedEntries` set from the entries
  it actually placed and a `filledThisBar` set from the bar's fill events; a
  close whose rung was filled this bar without a confirmed entry is recorded
  but never sent (ladder-engine.ts:1689). The dependency is now explicit, so
  reordering or parallelising the loops cannot reintroduce it.
- Regression: `runLadderPaperTradingIteration (persistence + resume) > does not
  send a close order for a rung whose entry was skipped`.
- Issue: `clever-cabin-80tn` (closed).

**L8 — a ledger-write failure after a confirmed fill could leave an untracked live position (FIXED).**
`executeLadderFillLive` returns an exchange-confirmed fill and the state was
persisted much later with a bare `saveLadderState`; a failure there engaged
nothing, and `guardOrphanPosition` recovers only the `openRungCount(w) === 0`
case.

- Fix: `saveLadderStateWithRetry` retries three times, then engages the kill
  switch and returns a `KILL SWITCH ENGAGED` hold (ladder-engine.ts:2019). Both
  the success path (2531) and the rolled-back path (2333) use it.
- Fix (ordering): the state is persisted before the trade rows (L9). A replay
  re-fills and re-closes the same rungs and corrupts capital, while a missing
  history row only weakens evidence — so the state wins the race.
- Regression: `runLadderPaperTradingIteration (persistence + resume) > engages
  the kill switch when the state save fails after a confirmed fill`.
- Issue: `clever-cabin-kacz` (closed).

---

## SM-FLOW — Flow Ignition position machine

One directional position per `(exchange, symbol)`; `side` is the primary axis,
`stage` the trailing-stop phase.

### States

| State | Encoding |
| --- | --- |
| `FLAT` | `side === null`, `stage === null` |
| `INITIAL` | `side ≠ null`, `stage === "initial"`, stop = entry ∓ 1.25·ATR15 |
| `BREAKEVEN` | `stage === "breakeven"`, stop = entry |
| `TRAIL` | `stage === "trail"`, stop behind `lastPeak` |
| `LATCHED` | `killed === true` (position mismatch or explicit kill) |

### Invariants

| # | Invariant | Enforced at |
| --- | --- | --- |
| F1 | `side === null` ⟹ all entry fields null | `freshFlowTradeState` (flow-trade.ts:153) |
| F2 | `side !== null` ⟹ `entryPrice`, `qty`, `stopPrice`, `exitAt`, `atr` non-null | `openFlowTrade` (flow-trade.ts:341) |
| F3 | `stage` only moves `initial → breakeven → trail` | `updateFlowTrailing` (flow-trade.ts:510) |
| F4 | The venue must hold the same side with `qty > 0`, else `LATCHED` + kill switch | `guardFlowPosition` (flow-trade.ts:472) |
| F5 | The risk guard sees the **persisted** capital, the day's start-of-day capital, today's realized PnL and today's trade count | `openFlowTrade` (flow-trade.ts:360) |
| F6 | Every close writes a `flow_trades` row and clears `flow_trade_state` in one transaction, and the realized PnL moves the persisted capital | `closeFlowPosition` (flow-trade.ts:600), `recordFlowClose` (repository.ts) |

### Transition table

| From | Trigger (guard) | To | Code |
| --- | --- | --- | --- |
| `FLAT` | signal clears threshold ∧ ATR>0 ∧ sized qty>0 ∧ risk guard passes | `INITIAL` | flow-trade.ts:341–459 |
| `INITIAL` | favourable ≥ 1R | `BREAKEVEN` | flow-trade.ts:523 |
| `BREAKEVEN` | stop pushed past entry | `TRAIL` | flow-trade.ts:526 |
| `TRAIL` | new extreme | `TRAIL` (stop ratchets) | flow-trade.ts:533 |
| any open | price crosses stop | `FLAT` (`stop`) | `resolveFlowExit` (flow-trade.ts:563) |
| any open | `\|z_dOI\| > 1.5` ∧ OFI sign flipped | `FLAT` (`emergency`) | flow-trade.ts:565 |
| any open | `now ≥ exitAt` | `FLAT` (`time`) | flow-trade.ts:573 |
| any open | venue side/qty mismatch | `LATCHED` | `guardFlowPosition` (flow-trade.ts:472) |
| `LATCHED` | kill switch disengaged ∧ flat | `FLAT` | `guardFlowState` (flow-trade.ts:230) |

### Reachable invalid states

**RISK-03 — the flow engine's risk guard never saw a real account (FIXED).**
`openFlowTrade` called `riskGuard.check` with `capital`, `peakCapital` and
`startOfDayCapital` all hardcoded to `opts.capital`, plus `dailyRealizedPnl: 0`,
`tradesTodayCount: 0`, `positionValue: 0`. `flow_trade_state.capital` was only
ever written from `opts.capital` and there was **no flow trade table**, so
realized PnL never entered the ledger: every check evaluated the untouched
opening configuration (probe, pre-fix: a live flow state with capital 12.5
iterated with a spy `RiskGuard` never reached the guard, and the persisted
capital stayed 12.5).

- Fix (account state): the guard now receives the persisted capital, the day's
  start-of-day capital, `getTodayRealizedPnl()` and `countTradesForDate()`
  (flow-trade.ts:360). Sizing uses the same capital, so the position cannot
  outgrow the account.
- Fix (ledger): `flow_trades` (repository.ts:843) records every close through
  `recordFlowClose`, which inserts the row, deletes the state row and anchors
  the day's opening capital in ONE transaction; `getTodayRealizedPnl` and
  `countTradesForDate` now include flow rows, so the daily-loss limit and the
  breaker see flow PnL. `closeFlowPosition` moves the persisted capital by the
  realized PnL.
- Regression: `PaperTradingRepositorySQLite > counts flow trades in the daily
  risk accounting`.
- Issue: `clever-cabin-20oq` (closed).

**RISK-03b — the flow close is now atomic with its ledger (FIXED by F6).**
`closeFlowPosition` used to send the venue close and then delete the state row
with no history behind it. The row and the clear now commit together; a crash
between the venue fill and that commit leaves the position open locally, which
`guardFlowPosition` reconciles and latches on — fail-closed, operator
recoverable.

---

## SM-BREAK — Daily-loss circuit breaker

### States

| State | Storage |
| --- | --- |
| `CLOSED` | `risk_circuit_breaker.open = 0`, `reason = ''` |
| `OPEN` | `open = 1`, `reason` = threshold message, `daily_pnl` preserved |
| `VANISHED` | today's row existed in this process and no longer does |
| `FRESH_DAY` | no row for the current UTC date (row was never anchored today) |

### Invariants

| # | Invariant | Enforced at |
| --- | --- | --- |
| B1 | The day boundary is UTC (`toISOString().slice(0,10)`), matching start-of-day capital | `todayKey()` (circuit-breaker.ts:66) |
| B2 | Accumulation is a single-statement UPSERT, so concurrent writers cannot lose updates | `accumulateSQL` (circuit-breaker.ts:120) |
| B3 | `VANISHED` reads fail closed (error), never `open=false` | `isOpen`/`getReason`/`currentDailyLossPct` (circuit-breaker.ts:220+) |
| B4 | A mid-day reset requires `authorized: true` and never deletes `daily_pnl` | `reset` (circuit-breaker.ts:288–303) |
| B5 | A reset never hands back a fresh loss budget inside the same UTC day | circuit-breaker.ts:319 |
| B6 | Non-positive start-of-day capital opens the breaker instead of recording 0% | `recordTradeResult` (circuit-breaker.ts:190) |
| B7 | The day's opening capital is anchored by the day's FIRST TRADE (capital before it), not by the first reader | `ensureStartOfDayCapital` (repository.ts) |
| B8 | A read with no row yet returns the capital in hand and writes nothing | `getStartOfDayCapital` (repository.ts:1700) |

### Transition table

| From | Trigger (guard) | To | Code |
| --- | --- | --- | --- |
| `CLOSED` | cumulative `daily_pnl ≤ −maxDailyLossPct` | `OPEN` | `accumulateSQL` (circuit-breaker.ts:120) |
| `CLOSED` | start-of-day capital ≤ 0 | `OPEN` (capital reason) | `openBreaker` (circuit-breaker.ts:149) |
| `OPEN` | `reset({authorized:true})` | `CLOSED` (PnL retained, archived) | circuit-breaker.ts:303–319 |
| `OPEN` | `reset()` without authorization | `OPEN` (fails) | circuit-breaker.ts:293 |
| `CLOSED`(`daily_pnl=0`) | `reset()` | `CLOSED` (no audit noise) | circuit-breaker.ts:299 |
| any | UTC day rolls over | `FRESH_DAY` | `todayKey()` |
| any | row deleted underneath the process | `VANISHED` (reads fail) | `rowVanished` (circuit-breaker.ts:186) |

### Consumers and the missing coupling

The breaker is **only** a new-risk gate. Consumers:

- `grid-engine` — `recordTradeResult` on every close (grid-engine.ts:1294);
  `isOpen` gates entries only (grid-engine.ts:864, after the RISK-02 fix).
- `ladder-engine` — `recordTradeResult` per iteration with closes
  (ladder-engine.ts:2527); `isOpen` holds only while flat
  (ladder-engine.ts:1979).
- `futures-engine` — `recordTradeResult` on close (futures-engine.ts:442, 528).
- `flow-trade` — records every close through `recordFlowClose` and reads
  `isOpen` via `guardFlowState` (flow-trade.ts:250). Before RISK-03 was fixed
  it did neither.

**RISK-03c — the baseline was "capital when some engine first asked" (FIXED).**
`paper_start_of_day_capital` used to be written by `getStartOfDayCapital`
itself, i.e. by whichever engine happened to ask first that day. An engine
starting after a loss adopted the already-reduced balance as the day's opening
capital, so that loss became invisible to every daily limit and the ladder's
two denominators could disagree about whether the day was already lost.

- Fix: `getStartOfDayCapital` is now read-only and returns the capital in hand
  when no row exists (correct: no trade yet today). The row is written by the
  first trade's recording path — `recordGridExit` / `recordFlowClose` /
  `recordLadderTrades` — with `capitalBefore`, via
  `INSERT ... ON CONFLICT DO NOTHING`, so the first trade of the day is
  authoritative regardless of which engine trades.
- Regression: `PaperTradingRepositorySQLite > anchors start-of-day capital at
  the first trade, not at the first read`.
- Issue: `clever-cabin-20oq` (closed).

---

## SM-KILL — Kill switch

### States

| State | Storage |
| --- | --- |
| `DISENGAGED` | `risk_kill_switch.engaged = 0` |
| `ENGAGED` | `engaged = 1`, `reason` set |
| `ENGAGED_UNPERSISTED` | in-process `persistFailed = true`; disk still says disengaged |
| `UNKNOWN` | row missing/unreadable → treated as engaged |

### Invariants

| # | Invariant | Enforced at |
| --- | --- | --- |
| K1 | Unknown/unreadable state is treated as engaged | `isEngaged` (kill-switch.ts:132) |
| K2 | `engage` sets the in-process mirror **before** the write, so a failed persist still blocks this process | kill-switch.ts:76 |
| K3 | `disengage` clears the mirror only after a confirmed write (`changes > 0`) | kill-switch.ts:112 |
| K4 | A failed `engage` write blocks until a confirmed engage or disengage | `persistFailed` (kill-switch.ts:64, 134) |
| K5 | Every `isEngaged` reads the DB, so another process's engage is visible | kill-switch.ts:142 |

### Transition table

| From | Trigger | To | Code |
| --- | --- | --- | --- |
| `DISENGAGED` | `engage(reason)` write succeeds | `ENGAGED` | kill-switch.ts:72 |
| `DISENGAGED` | `engage(reason)` write fails | `ENGAGED_UNPERSISTED` | kill-switch.ts:88 |
| `ENGAGED_UNPERSISTED` | any `isEngaged()` | fails closed | kill-switch.ts:134 |
| `ENGAGED` | `disengage()` with `changes > 0` | `DISENGAGED` | kill-switch.ts:102 |
| `ENGAGED` | `disengage()` with `changes === 0` | `ENGAGED` (fails) | kill-switch.ts:112 |
| any | row deleted | `UNKNOWN` (fails closed) | kill-switch.ts:151 |

No reachable invalid state found. The one caller-side gap is that the grid
drawdown latch does **not** engage this switch (RISK-01's original shape);
after the fix it is a local halt with an explicit reason, which is the
documented posture.

---

## SM-GATEWAY — Gateway lifecycle

### States

`gateway-state.json.mode` × per-service `status`:

| Field | Values **written** | Values only **read** |
| --- | --- | --- |
| `mode` | `""` (default), `starting`, `warming`, `healthy`, `down` | — (the dead `degraded` branch was removed) |
| service `status` | `starting`, `healthy`, `warming`, `down`, `disabled`, `skipped` | — |

The only `writeMode` callers are gateway-orchestrator.ts:249, 356 (both
`warming`), 538 (final `warming \| healthy`), plus `markStopped`
(gateway-state.ts:225, `down`) and the `starting` state literal at
gateway-orchestrator.ts:463.

### Transitions

| From | Trigger (guard) | To | Code |
| --- | --- | --- | --- |
| (none) | `gateway start`, TS-only runtime | backend `skipped` | gateway-orchestrator.ts:196 |
| (none) | spawn + healthy probe | backend `healthy` | gateway-orchestrator.ts:238 |
| starting | spawn ok, probe failed, `supervised` | `warming` | gateway-orchestrator.ts:245 |
| starting | spawn ok, probe failed, not supervised | `down` + stop + `markStopped` | gateway-orchestrator.ts:258 |
| (none) | telegram disabled | telegram `disabled` | gateway-orchestrator.ts:294 |
| any | services started, any still warming | `warming` | gateway-orchestrator.ts:538 |
| any | services started, all healthy | `healthy` | gateway-orchestrator.ts:538 |
| any | `gateway stop` | `down` for every known service | `markStopped` (gateway-state.ts:225) |

### Reachable invalid states

**RISK-06a — `degraded` was unreachable (FIXED).** `gateway.ts:106` treated
`mode === "degraded"` as a real runtime mode and printed a warming hint for
it, but no writer ever produced it (the final mode is `warming \| healthy`,
gateway-orchestrator.ts:538). The branch could never fire, so an operator
diagnosing a partially-degraded gateway saw the "make sure the backend is
running" text instead of the warming explanation. The dead branch is removed;
the state list now matches what is actually written.

**RISK-06b — torn reads and lost updates in the state file (FIXED).**
`write` used to call `writeFileString` on the final path, so a reader could
observe a truncated document, and a truncated document decodes to the
**zero-value default** (`decodeOrFallback`, gateway-state.ts:109) — a torn read
was indistinguishable from "no state". Fixed with write-then-`rename(2)`
(gateway-state.ts:173): a reader now sees either the old file or the new one.

Remaining, deliberately not "fixed": `writeMode`/`writeServiceState` are still
read-modify-write on the whole file, so two concurrent `gateway start`/`stop`
processes can still lose each other's service entry. Serialising them needs a
lock (or a single writer) — a lifecycle decision, not a local patch. The file
is diagnostic state; trading never reads it.

**RISK-06c — a spawned process could be left untracked (FIXED).**
`startService` writes the PID file after `Bun.spawn`, and `PidFile.write`
swallows filesystem errors, so a failed write left a live process that
`gateway stop` could not find, still holding its port. The PID is now read back
and compared; on mismatch the just-spawned process is terminated and the call
fails with `ProcessError` (process-manager.ts:247).

A recycled PID is still only detected when `patterns` are supplied
(process-manager.ts:365) — callers that pass no patterns accept any live PID in
the file. Unchanged: the check needs the binary name, which only the caller
knows.
- Issue: `clever-cabin-rhwr` (closed for 06a/06b/06c; the concurrent-writer
  note above is recorded there as accepted).

---

## SM-ORDER — Exchange order/fill (Bitget & Bybit futures)

### States

| State | Meaning |
| --- | --- |
| `SUBMITTED` | venue acknowledged, no fill data yet |
| `INDEXING` | detail endpoint reports empty status / qty 0 |
| `RESTING` | accepted, working, unfilled |
| `PARTIAL` | filled size < requested |
| `FILLED` | filled size ≥ requested |
| `CANCELLED` | venue reports a terminal unfilled state |
| `CANCEL_FAILED` | cancel rejected and re-read shows no fill |

### Transitions

| From | Trigger (guard) | To | Code |
| --- | --- | --- | --- |
| `SUBMITTED` | next poll | `INDEXING`/`RESTING`/`FILLED` | bitget-futures.ts:224 |
| `INDEXING` | `250ms` × poll budget | `RESTING`/`FILLED` | bitget-futures.ts:230 |
| `RESTING` | `cancelFuturesOrder` ok | `CANCELLED` (fail) | bitget-futures.ts:250 |
| `RESTING` | cancel rejected, re-read has a fill | `FILLED` | bitget-futures.ts:266 |
| `RESTING` | cancel rejected, re-read has no fill | `CANCEL_FAILED` (fail, "may still rest") | bitget-futures.ts:270 |
| `SUBMITTED` | terminal unfilled status | `CANCELLED` (fail) | bitget-futures.ts:283 |

### Invariants

| # | Invariant | Enforced at |
| --- | --- | --- |
| O1 | A resting order is cancelled before reporting failure, so a retry cannot duplicate it | bitget-futures.ts:249 |
| O2 | A fill's fee is stored non-negative | bitget-futures.ts:300 |
| O3 | A close resolves the reducing leg (`holdSide`) and caps at `available` | bitget-futures.ts:317 |
| O4 | A zero-available reducing leg is a failure, not a benign `null` | bitget-futures.ts:336 |
| O5 | Positions are read with `abs()` so a short never reports negative quantity | bybit-futures.ts:1640 |

No reachable invalid state found. Note the deliberate asymmetry: an
unfilled-but-cancelled order fails, which the grid/ladder bar rollback treats
as "no position" — correct, because O1 guarantees the order is gone.

---

## SM-RECON — Grid live reconciliation

### States (result kinds)

`matched`, `adopt`, `unadoptable`, `mismatch`.

Transitions are pure functions of `(local side, exchange position, expected)`:

| Local | Exchange | Result | Handling |
| --- | --- | --- | --- |
| `null` | `null` | `matched` | continue |
| `null` | adoptable | `adopt` → `LONG`/`SHORT` (adopted) | persist with retry, else kill switch |
| `null` | unadoptable | `unadoptable` → venue close, else kill switch | grid-engine.ts:820 |
| open | `null` | `mismatch` | kill switch + cancel resting orders |
| open | opposite side | `mismatch` | kill switch |
| open | product/margin/leverage/price differ | `mismatch` | kill switch |
| open | incomplete local evidence | `mismatch` | kill switch |
| open | quantity differs | `mismatch` | kill switch |
| open | `available ≠ quantity` | `mismatch` | kill switch |
| open | all equal | `matched` | continue |

Invariant: a mismatch always engages the kill switch and cancels resting orders
before returning (`failGridLiveReconciliation`, grid-engine.ts:877). No
reachable invalid state found.

---

## Changelog

- **2026-11-07** — initial document. RISK-01 (drawdown latch) and RISK-02 (open
  breaker skipping exits) fixed in `grid-engine.ts` + `ladder-engine.ts` with
  four regression tests. RISK-03, RISK-04, RISK-06 filed; RISK-05 documented
  as an unasserted dependency.
- **2026-11-08** — remaining four fixed. RISK-04a: `recordGridExit` commits the
  exit row + state + day anchor in one transaction. RISK-04b: an unconfirmed
  live close latches `killed`. L8: `saveLadderStateWithRetry` + state-before-
  trades ordering. RISK-05: closes require a confirmed entry. RISK-03: flow
  engine gets a real ledger (`flow_trades`), truthful risk inputs and an atomic
  close. RISK-03c: the day's baseline is anchored by the first trade. RISK-06:
  atomic state-file rename, PID read-back verification, dead `degraded` branch
  removed. Six regression tests, each verified to fail on the pre-fix code.

## Known gaps (accepted, with the reason)

- Concurrent `gateway start`/`stop` can lose a service entry in
  `gateway-state.json` (SM-GATEWAY). Diagnostic state only; fixing it needs a
  single writer or a lock.
- A recycled PID is trusted when a caller passes no `patterns`
  (process-manager.ts:365). The pattern needs the binary name, which only the
  caller knows.
- The grid breaker's `recordTradeResult` runs just after the atomic exit write.
  A crash in that microsecond loses one day-loss accumulation while the trade
  row survives; `getTodayRealizedPnl` still sees it, so the guard stays honest
  and only the breaker's own accumulator drifts until the next close.
- `paper_start_of_day_capital` is anchored by grid/ladder/flow trade recording.
  The spot paper engine (`paper-trading/engine.ts`) records through
  `closePosition`/`scaleOutPosition`, which do not anchor it yet; its baseline
  therefore still comes from `getStartOfDayCapital` at first use.
