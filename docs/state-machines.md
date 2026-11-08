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
| SM-GRID | Grid paper/live position | `paper-trading/grid-engine.ts` | `grid_paper_state` | **fixed** (RISK-01, RISK-02) |
| SM-LADDER | Ladder paper/live rungs | `paper-trading/ladder-engine.ts` | `ladder_paper_state`, `ladder_paper_trades` | **fixed** (RISK-02); RISK-04/05 open |
| SM-FLOW | Flow Ignition single position | `scalping/flow-trade.ts` | `flow_trade_state` | open (RISK-03) |
| SM-BREAK | Daily-loss circuit breaker | `risk/circuit-breaker.ts` | `risk_circuit_breaker`, `risk_circuit_breaker_resets` | sound |
| SM-KILL | Kill switch | `risk/kill-switch.ts` | `risk_kill_switch` | sound |
| SM-GATEWAY | Gateway lifecycle | `services/gateway-orchestrator.ts`, `services/gateway-state.ts`, `services/pid.ts` | `pids/gateway-state.json`, `pids/*.pid` | open (RISK-06) |
| SM-ORDER | Exchange order/fill | `exchange/adapters/bitget-futures.ts`, `bybit-futures.ts` | exchange only | sound |
| SM-RECON | Grid live reconciliation | `paper-trading/live-position-reconciliation.ts` | derived | sound |

Priority order of confirmed defects: **RISK-02** (money, unbounded) →
**RISK-01** (money, bounded) → **RISK-03** → **RISK-04** → **RISK-06** →
**RISK-05**.

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

**RISK-04a — crash between the trade row and the state row double-books an exit (OPEN, P0).**
`closeGridPosition` writes `grid_paper_trades` (grid-engine.ts:1293) *and*
`circuitBreaker.recordTradeResult` (grid-engine.ts:1294) **before** the caller
persists the flat state in `finalizeGridBar` (grid-engine.ts:1824). A crash in
that window leaves the ledger holding the position while the trade row and the
day's loss already exist; the next iteration re-closes.

- Evidence (probe): iteration 1 opens, iteration 2 closes (`trades=1`); then
  restoring the pre-close state (the crash window) and iterating again produces
  a second exit row (`exit rows = ["target:1000.495", "target:1010"]`,
  `trades=2`).
- Issue: `clever-cabin-kacz`.

**RISK-04b — a failed live close is retried on the next bar (OPEN, by design, undocumented).**
`processGridPosition` returns the failure unchanged; nothing latches and
nothing engages the kill switch, so the same reduce-only close is re-sent next
bar. Idempotent on a venue that reports "nothing closable", but it hides a
sustained close outage (probe: three consecutive `action=THREW` iterations,
`side=long` and `killed=false` throughout).
- Issue: `clever-cabin-kacz`.

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

**RISK-05 — same-candle entry+exit is correct only because of loop order (OPEN, P1).**
`executeLadderBarLive` executes **all** entries, then **all** closes
(ladder-engine.ts:1663–1693) and never checks that a close's matching entry
succeeded. Today this is safe only because a failed entry aborts the whole bar
before the close loop runs. Any reordering, parallelism, or partial fill turns
a same-candle exit into a reduce-only order with no position behind it
(venue `40020`), which is the failure the comment at ladder-engine.ts:1659
says was fixed. The dependency is load-bearing and unasserted.
- Issue: `clever-cabin-80tn`.

**L8 — a ledger-write failure after a confirmed fill can leave an untracked live position (OPEN, P0).**
`executeLadderFillLive` returns an exchange-confirmed fill
(ladder-engine.ts:1576) and the state is not persisted until
ladder-engine.ts:2534. If `saveLadderState` fails there, no kill switch is
engaged (the grid path has `saveGridStateWithRetry`, ladder does not) and
nothing records that a fill landed. `guardOrphanPosition` recovers only the
`openRungCount(w) === 0` case.
- Issue: `clever-cabin-kacz`.

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
| F5 | The risk guard must see **current** capital, peak, start-of-day capital and today's trades | `openFlowTrade` (flow-trade.ts:360) — *violated, see RISK-03* |

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

**RISK-03 — the flow engine's risk guard never sees a real account (OPEN, P0 if `--live` ever ships).**
`openFlowTrade` calls `riskGuard.check` with `capital`, `peakCapital`,
`startOfDayCapital` all hardcoded to `opts.capital`, `dailyRealizedPnl: 0`,
`tradesTodayCount: 0`, `positionValue: 0` (flow-trade.ts:360–366).
`flow_trade_state.capital` is only ever written from `opts.capital`
(flow-trade.ts:169, 434) and there is **no flow trade table**
(`repository.ts:797–816` has no `recordFlowTrade`), so realized PnL never
enters the ledger. Every check evaluates the untouched opening configuration.

- Evidence (probe): a live flow state (capital 12.5, side LONG) iterated with a
  spy `RiskGuard` → the guard is never called on the hold path, and the
  persisted capital stays `12.5`.
- Impact today: flow-trade exposes no `--live` flag, so this is a latent P0.
  It becomes live the moment the Bitget real-money adapter
  (`clever-cabin-va5`) wires it.
- Issue: `clever-cabin-20oq`.

**RISK-03b — the flow close is not atomic with its ledger (OPEN).**
`closeFlowPosition` sends the venue close and then `clearFlowTradeState`
(flow-trade.ts:597) without persisting a "closing" marker; a crash between them
re-enters `manageFlowPosition`, which reconciles against the venue and, on a
mismatch, latches the kill switch. Fail-closed and operator-recoverable —
listed for completeness, no separate issue.

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
- `flow-trade` — **never records and never reads.** See RISK-03.

**RISK-03c — the breaker's denominator is not the start-of-day capital the rest of the system uses (OPEN, P0).**
`paper_start_of_day_capital` is written only by `getStartOfDayCapital`
(`repository.ts:1631`), i.e. by whichever engine happens to run first that day;
`recordGridTrade` (`repository.ts:2266`) never writes it. `grid-engine`,
`futures-engine` and the ladder's risk guard call `getStartOfDayCapital(...)`
with capital **at first use** (ladder-engine.ts:1489), while the ladder passes
its own `startOfDayCapital` to the breaker (ladder-engine.ts:2493). An engine
that starts after a loss therefore adopts the already-lossy capital as the
day's baseline, and the two limit implementations can disagree about whether
the day is already lost.
- Issue: `clever-cabin-20oq`.

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
| `mode` | `""` (default), `starting`, `warming`, `healthy`, `down` | `degraded` — dead branch |
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

**RISK-06a — `degraded` is unreachable (OPEN, P3).** `gateway.ts:106` treats
`mode === "degraded"` as a real runtime mode and prints a warming hint for it,
but no writer ever produces it: the final mode is `warming \| healthy`
(gateway-orchestrator.ts:538). The branch can never fire, so an operator
diagnosing a partially-degraded gateway sees the "make sure the backend is
running" text instead of the warming explanation. Cosmetic, but it is the kind
of dead state that makes the other states look more expressive than they are.
- Issue: `clever-cabin-rhwr`.

**RISK-06 — non-atomic state file + lost updates + PID after spawn (OPEN, P2).**
`writeMode`/`writeServiceState` are read-modify-write on the whole JSON file
with no lock (gateway-state.ts:184, 203), `write` calls `writeFileString` on
the final path with no temp-file rename (gateway-state.ts:164) and swallows
errors (gateway-state.ts:179). A concurrent start/stop silently drops the other
writer's service entry; a torn read decodes to the **zero-value default**
(`decodeOrFallback`, gateway-state.ts:109) and is indistinguishable from "no
state". `startService` writes the PID file only **after** `Bun.spawn` returns
(process-manager.ts:307), so a crash in that window leaves a live untracked
process; a recycled PID is detected only when patterns are passed
(process-manager.ts:348).
- Issue: `clever-cabin-rhwr`.

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
