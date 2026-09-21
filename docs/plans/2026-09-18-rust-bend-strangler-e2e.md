# NeuraTrade Rust + Bend Strangler — End-to-End Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
>
> **Box checked 2026-09-18:** `217.216.35.77` (`vmi2933244`). Data persists; hunt must stay up. Not green on CPU/disk/PnL.

**Goal:** Keep the profitability / PnL / trade-throughput hunt running while strangling NeuraTrade into a maintainable **Rust runtime + Bend parallel search + Zig-shipped binaries**, with storage split so the monolith SQLite stops starving the VPS.

**Architecture:** Strangler fig. Champion paper/demo soaks and throttled autoresearch stay online. Bend owns the trial/search kernel (with `LAWS.bend` / `PROOF.bend`). Rust owns risk, execution, ledger, adapters, CLI. Zig (`cargo-zigbuild` / `zig cc`) produces reproducible native artifacts. TS remains a temporary FFI/gRPC bridge (Cloudflare / cutover only), then is deleted. Storage splits: Postgres for transactional state; Parquet (+ optional DuckDB) for research candles — not PlanetScale TIN (TIN is Postgres FTS, not a SQLite replacement).

**Tech Stack:** Rust (edition 2024+), Bend (`bend guide`, `LAWS.bend`, `PROOF.bend`, parallelize), Zig toolchain for cross-compile, optional TS FFI, Postgres (managed or self-hosted), Parquet/object storage, SQLite only as ephemeral local cache, `bd` for tracking, PM2 on `217.216.35.77` until Rust supervisor replaces it.

**Spec / decisions locked in brainstorming (2026-09-18):**
- Full-stack direction: Rust + Bend (+ Zig ship); not Bend-only or ops-only forever.
- Keep-alive: soaks + research continue — we are still finding profitability, want **high trade throughput** and **positive expectancy quickly**.
- TIN ≠ DB migration target. Prefer Postgres (ops) + Parquet (history).
- Do not preserve obsolete paths; delete Go/TS when parity is proven (aligns with AGENTS.md working principles).
- New test files are **opt-in** per AGENTS.md — prefer existing tests + runtime/soak evidence unless the user explicitly approves new suites.

**Global Constraints:**
- Never stop champion paper/demo without an explicit owner decision window.
- Never wipe `~/.neuratrade/data/neuratrade.db`, champion JSON, or soak state without a verified backup off-box.
- Money math: decimal / fixed-point only — no float for monetary values (P0).
- Risk path remains the only path to execution (Bend search must not place live orders).
- Bend workflow: `bend guide` → encode invariants in `LAWS.bend` → `bend PROOF.bend` before commit.
- RTK-prefix shell on agents (`rtk …`). Track work in `bd`, not markdown TODOs outside this plan.
- Box disk was **87%** with `disk-breached=1`; reclaim before adding more data writers.

**Observed box baseline (2026-09-18):**
| Item | Value |
| --- | --- |
| Autoresearch | 4× `loop.ts --trials=50000`, ~5d, ~67% CPU combined, ~0.9–1.0 GB RAM |
| Host | ~99% CPU, load 23–26, disk 87% |
| SQLite | `~/.neuratrade/data/neuratrade.db` ≈ 8.8 GB + bak ≈ 6.9 GB |
| Champion paper | equity ~201.81 / 200, `open=0` |
| Champion demo (live) | equity ~198.31 / 200, `open=0`, blocked: notional >100%, capital &lt; min 50 |
| Audit finding | Confirmed — autoresearch workers starve the box |

---

## Target layout (new tree grows beside old)

```
neuratrade/
├── AGENTS.md                 # keep Bend rules + existing agent rules
├── LAWS.bend                 # cross-cutting proofs (risk, money, no bypass)
├── PROOF.bend
├── crates/                   # Rust workspace
│   ├── nt-cli/               # binary entry (replaces bun index.ts gradually)
│   ├── nt-risk/
│   ├── nt-execution/
│   ├── nt-ledger/
│   ├── nt-market/
│   ├── nt-exchange-*/        # bitget/bybit/…
│   └── nt-ffi/               # optional C ABI / uniffi for TS bridge
├── bend/                     # Bend packages
│   ├── search/               # autoresearch kernel (parallel trials)
│   ├── backtest/
│   └── laws/                 # domain laws composed into root LAWS.bend
├── build/                    # Zig / cargo-zigbuild scripts
│   ├── zig-build.sh
│   └── targets.toml
├── deploy/
│   └── ecosystem.rust.cjs    # PM2 until rust supervisor ships
└── services/neuratrade-cli-ts/  # DELETE after parity (strangler source of truth until then)
```

---

## Phase map (order is binding)

```
P0 Ops unblock (hunt can trade)
 → P1 Disk + SQLite hygiene
 → P2 Throttled search + throughput knobs (still TS)
 → P3 Storage split design + dual-write
 → P4 Bend search kernel (replace autoresearch CPU path)
 → P5 Rust runtime strangler (risk → exec → ledger → CLI)
 → P6 Zig single-artifact ship
 → P7 TS FFI bridge (temporary) + Cloudflare cutover
 → P8 Delete Go / Bun trading path; Postgres cutover; bake-off
```

Each phase must leave **paper + demo soaks runnable** and produce **bd-closeable evidence** (logs, equity, fill counts).

---

### Task 0: Track the epic in beads

**Files:**
- Create: `bd` epic + child issues (no markdown TODO lists)

- [ ] **Step 1:** `bd create "Epic: Rust+Bend strangler (keep profitability hunt alive)" -t epic -p 1`
- [ ] **Step 2:** Create children for P0–P8 with `--deps` / blocking links from this plan’s phase IDs
- [ ] **Step 3:** Attach this file path in epic description: `docs/plans/2026-09-18-rust-bend-strangler-e2e.md`

---

### Task 1 (P0): Unblock live/demo trade throughput — keep hunt alive

**Why:** Demo cannot open size (notional ~150% of max; ETH/SOL capital &lt; min 50). Zero opens ⇒ no PnL learning.

**Files:**
- Modify (TS strangler, temporary): `services/neuratrade-cli-ts/ecosystem.champion-soak.config.cjs`
- Modify: champion whitelist / capital allocation under `services/neuratrade-cli-ts/autoresearch/results/champion-whitelist.json` (or generate script — do not hand-edit secrets)
- Modify as needed: `services/neuratrade-cli-ts/src/cli/scalp.ts` risk flag defaults **only if** a bug forces notional &gt; 100% with `positionFraction=1` and weight 0.25
- Reference: box logs under `/root/.neuratrade-champion-demo/logs/champion-demo.out.log`

**Interfaces:**
- Consumes: frozen `champion-soak.json` knobs; PM2 champion-demo/paper process args
- Produces: soaks that can open positions without violating max-position / min-capital; rising `opened-count-*` monitor files

- [ ] **Step 1:** On box, capture 20 recent `pre-trade risk check failed` lines from champion-demo (baseline evidence)
- [ ] **Step 2:** Decide allocation fix (prefer config): either raise per-symbol capital above min 50, lower `--min-capital`, or reduce grid notional so size ≤ `--max-position-size-pct 100`
- [ ] **Step 3:** Apply **one** change set; restart only champion-demo/paper (not full PM2 wipe)
- [ ] **Step 4:** Verify ≥1 open attempt succeeds or fails for a *market* reason (not sizing/min-capital) within 2 intervals
- [ ] **Step 5:** Record equity + open/closed counts in bd evidence; do not claim profitability yet

**Exit criteria:** Demo/paper no longer stuck solely on sizing/min-capital; hunt can generate trades.

---

### Task 2 (P0/P1): Throttle autoresearch so soaks get CPU

**Why:** Audit finding confirmed; 4×50k trials ≈ box meltdown.

**Files:**
- Modify: PM2 ecosystem for `neuratrade-autoresearch-w0..w3` (on box and/or repo ecosystem under `services/neuratrade-cli-ts/`)
- Modify: `services/neuratrade-cli-ts/autoresearch/loop.ts` CLI defaults only if defaults are unsafe for shared hosts

**Interfaces:**
- Consumes: `--worker`, `--workers`, `--trials`, screen/confirm budgets
- Produces: ≤2 workers OR night window OR CPU quota; champion JSON still updates

- [ ] **Step 1:** Snapshot `pm2 list` + `uptime`/`loadavg` (before)
- [ ] **Step 2:** Stop `neuratrade-autoresearch-w2` and `w3` (or set trials much lower); keep 1–2 workers
- [ ] **Step 3:** Optional: `nice` / cgroup / PM2 `max_memory_restart` already 2GB — add CPU scheduling notes in deploy docs
- [ ] **Step 4:** Confirm champion paper/demo interval latency improves; loadavg drops materially
- [ ] **Step 5:** Document the new “shared-host search budget” in this plan’s appendix when values settle

**Exit criteria:** Autoresearch no longer holds host at ~99% CPU continuously; soaks remain online.

---

### Task 3 (P1): Disk reclaim without losing persistence

**Why:** `disk-breached=1`, 87% disk; 6.9 GB bak duplicates live DB.

**Files / paths on box:**
- `/root/.neuratrade/data/neuratrade.db.bak-20260905`
- `/tmp/ledger-*.jsonl`, rotated syslog if safe
- Optional: off-box archive to R2 (existing `scripts/archive-ledger-r2.sh` pattern)

- [ ] **Step 1:** Confirm live DB integrity path: prefer `PRAGMA quick_check` when load is low (or copy off-box first)
- [ ] **Step 2:** Copy bak + critical champion JSON off-box (R2/S3/local) before delete
- [ ] **Step 3:** Remove confirmed-redundant bak / tmp ledgers; re-check `df -h` and clear `disk-breached` monitor when policy allows
- [ ] **Step 4:** Enable logrotate discipline (pm2-logrotate already present); cap journal if needed
- [ ] **Step 5:** Add runbook note: never keep multi-GB same-host full-file bak beside live DB

**Exit criteria:** Disk &lt; ~75% sustained; persistence intact; breach flag cleared or explained.

---

### Task 4 (P2): Throughput-oriented hunt knobs (still TS)

**Why:** Goal is “lots of trades, profitable in no time” — need measurable fill clock + expectancy gates, not flat HOLDs.

**Files:**
- Read: `services/neuratrade-cli-ts/autoresearch/results/champion-soak.json`
- Modify/create research scripts under `services/neuratrade-cli-ts/autoresearch/` (prefer extending existing growth/timeframe experiments)
- Monitor: `/root/.neuratrade/state/champion-soak-monitor/`

**Interfaces:**
- Consumes: frozen soak knobs + honest fees
- Produces: candidate knobs that increase expected fills/day while keeping guards (DD, PF, expectancy) green

- [ ] **Step 1:** Define hunt KPIs: fills/day, expectancy %, max DD %, time-to-first-fill, live vs paper gap
- [ ] **Step 2:** Run existing growth / timeframe experiments (do not overwrite champion files until promote gate)
- [ ] **Step 3:** Promote only via explicit champion-soak write + whitelist regen
- [ ] **Step 4:** Restart soaks with force-reseed only when mismatch policy requires it
- [ ] **Step 5:** Attach KPI snapshot to bd issue weekly until Rust/Bend cutover

**Exit criteria:** Documented KPI loop; at least one candidate improves fill rate without blowing DD guards.

---

### Task 5 (P3): Storage architecture — Postgres + Parquet (not TIN)

**Why:** 8.8 GB monolith SQLite is operationally heavy on a 72 GB VPS; concurrent research writers hurt soaks.

**Decision record:**
| Data | Store | Notes |
| --- | --- | --- |
| Orders, positions, fills, kill-switch, ledgers | **Postgres** | Managed (PlanetScale Postgres / Neon / etc.) or dedicated instance — not co-located on full root disk forever |
| OHLCV / research panels | **Parquet** (object storage or NVMe) | Query via DuckDB or Bend/Rust readers |
| Tiny local cache | SQLite optional | Ephemeral; never system of record |
| Full-text over logs/docs | PlanetScale **TIN** later | Only if FTS needed — unrelated to candle growth |

**Files:**
- Create: `docs/plans/storage-cutover.md` (or appendix in this file once schemas land)
- Create (later): `crates/nt-ledger` migrations (SQL, additive)
- Create: Parquet writer path for candle sync (replace/augment `seed-champion-soak-candles` / sync scripts)

- [ ] **Step 1:** Inventory SQLite tables by size (top offenders = migrate first to Parquet)
- [ ] **Step 2:** Draft Postgres schema for ledger/positions only (minimal)
- [ ] **Step 3:** Dual-write from TS soak → Postgres (feature-flagged); keep SQLite readable
- [ ] **Step 4:** Backfill candles to Parquet; point autoresearch readers at Parquet
- [ ] **Step 5:** Cut reads; shrink or freeze SQLite; drop bak policy

**Exit criteria:** New candle growth does not inflate the trading DB; ledger survives process restarts in Postgres.

---

### Task 6 (P4): Bend search kernel (replace Bun autoresearch hot path)

**Why:** Parallel trials are Bend’s sweet spot; proofs block silent risk regressions.

**Files:**
- Create: `LAWS.bend`, `PROOF.bend` (repo root)
- Create: `bend/search/` (trial loop, mutate, score — port of `autoresearch/loop.ts` + `mutate.ts` + `prepare.ts` semantics)
- Create: `bend/laws/` money + risk laws (no live order placement from search)
- Modify: `AGENTS.md` Bend block (already present) — keep in sync with `bend guide`

**Interfaces:**
- Consumes: Parquet panels + knob space; same champion JSON schema under `autoresearch/results/` initially
- Produces: `champion.json` / `champion-soak.json` compatible with existing soaks
- Law examples (encode precisely in Bend): search process cannot submit exchange orders; scores use fee schedule maker0.02/takerExit0.06 when `honestFees` set; DD guard failures never promote

- [ ] **Step 1:** Install Bend; run `bend guide`; spike `pow`-style parallel map over a tiny knob grid
- [ ] **Step 2:** Write `LAWS.bend` for search isolation + fee honesty + promotion guards
- [ ] **Step 3:** Port score/prepare semantics; parallelize trial evaluation
- [ ] **Step 4:** `bend PROOF.bend` green before any commit
- [ ] **Step 5:** PM2: replace `neuratrade-autoresearch-w*` with Bend binary; keep JSON drop path identical so soaks unchanged
- [ ] **Step 6:** Compare Bend vs Bun champion on same panel hash; require agreement within tolerance before deleting Bun loop

**Exit criteria:** Autoresearch CPU work runs in Bend; soaks still consume champion JSON; proofs pass on CI/agent commit gate.

---

### Task 7 (P5): Rust runtime strangler

**Order inside Rust (hard):** market → signal/grid → **risk** → execution → ledger/portfolio. Risk is the only path to execution.

**Files:**
- Create: Cargo workspace under `crates/`
- Create: `nt-risk`, `nt-execution`, `nt-ledger`, `nt-market`, `nt-cli`
- Port behavior from: `services/neuratrade-cli-ts/src/paper-trading/`, `src/scalping/`, exchange clients under `src/`
- Do **not** extend frozen Go `services/backend-api` except to unblock TS path until D1–D4 deletion criteria are met

**Interfaces:**
- Consumes: same CLI flags as `bun run index.ts scalp paper-trade …` initially (flag parity table in README)
- Produces: `nt` binary; paper state compatible or explicitly migrated once

- [ ] **Step 1:** Scaffold workspace + `nt` CLI stub that prints version / health
- [ ] **Step 2:** Port decimal money + pre-trade risk guards (`max-drawdown`, `max-daily-loss`, `max-position-size`, min capital)
- [ ] **Step 3:** Paper grid engine parity vs TS on a fixed candle fixture (runtime compare; new test files only if user approves)
- [ ] **Step 4:** Wire Bybit/Bitget public market + signed trading behind risk
- [ ] **Step 5:** Shadow mode: Rust paper beside TS paper on same whitelist; diff equity/fills daily
- [ ] **Step 6:** Cut PM2 champion-paper to Rust when shadow gap acceptable; then demo

**Exit criteria:** Rust paper soak matches TS within agreed tolerance; demo cutover planned with owner.

---

### Task 8 (P6): Zig packaging — everything ships as binaries

**Files:**
- Create: `build/zig-build.sh`, `build/targets.toml`
- Integrate: `cargo-zigbuild` for musl/glibc Linux targets used on the VPS
- Create: checksummed release tarball / single `nt` + `nt-search` (Bend) artifacts

- [ ] **Step 1:** Pin Zig version in repo (`.zig-version` or docs)
- [ ] **Step 2:** Script: build Rust crates via zig linker for `x86_64-unknown-linux-gnu` (box arch)
- [ ] **Step 3:** Script: package Bend search binary beside `nt`
- [ ] **Step 4:** Deploy script: stop one PM2 app, replace binary, start; health probe
- [ ] **Step 5:** Document one-command release: `./build/zig-build.sh release`

**Exit criteria:** Box runs versioned binaries without `bun run` for search + paper path.

---

### Task 9 (P7): Temporary TS FFI / bridge

**Why:** Cloudflare worker / alchemy path may lag native cutover.

**Files:**
- Create: `crates/nt-ffi` (C ABI or uniffi)
- Modify: `services/neuratrade-cli-ts/src/cloudflare/` to call native where possible **or** HTTP to local `nt` — prefer HTTP/gRPC over FFI if simpler
- Delete bridge when worker rewritten or retired

- [ ] **Step 1:** Choose bridge: gRPC/HTTP first; FFI only if in-process required
- [ ] **Step 2:** Expose read-only status + optional paper tick
- [ ] **Step 3:** No exchange secrets in Worker; secrets stay in native runtime env
- [ ] **Step 4:** Schedule deletion date once native covers the surface

**Exit criteria:** Worker does not own trading logic; bridge is thin and deletable.

---

### Task 10 (P8): Delete legacy; bake-off; ops freeze

**Files:**
- Delete when green: Bun autoresearch loop, TS paper engine, Go backend spawn paths per D1–D4 in AGENTS.md
- Update: Makefile / CI to Rust+Bend+Zig; neutralize Go targets
- Update: `AGENTS.md` WHERE TO LOOK table

- [ ] **Step 1:** Checklist parity: search, paper, demo, ledger restore, kill switch
- [ ] **Step 2:** Tag `archive/ts-cli-YYYY-MM-DD` before deletion
- [ ] **Step 3:** Remove PM2 bun entries; only binary apps remain
- [ ] **Step 4:** Close epic via `make bd-close-qa` with soak + search evidence (E2E ≠ N/A)

**Exit criteria:** One maintainable native stack; hunt still running; disk/CPU budgets documented.

---

## Success metrics (hunt + rewrite)

| Metric | Near-term (P0–P2) | Done (P8) |
| --- | --- | --- |
| Host CPU while soaks run | &lt; ~60% avg (search throttled) | Search on Bend with caps |
| Disk | &lt; 75%, no co-located multi-GB bak | Postgres + Parquet; thin cache only |
| Paper fills/day | Rising vs 2026-09-18 flat open=0 | KPI dashboard from ledger |
| Demo | Not blocked by min-capital/notional bugs | Rust path; risk laws proven |
| Expectancy | Positive on honest fees before promote | Same gates in Bend laws |
| Ship | bun + PM2 | Zig-built `nt` + `nt-search` binaries |

---

## Explicit non-goals

- Big-bang rewrite that pauses the profitability hunt.
- Using PlanetScale TIN as the SQLite replacement.
- New Go features (Go stays frozen until D1–D4 deletion).
- Silent champion overwrites without promote gate.
- Adding large new test trees without user opt-in (AGENTS.md).

---

## Appendix A — Box operator cheat sheet

```bash
# Status
ssh root@217.216.35.77 'pm2 list; df -h /; cat ~/.neuratrade/state/champion-soak-monitor/*'

# Champion logs
tail -f ~/.neuratrade-champion-paper/logs/champion-paper.out.log
tail -f ~/.neuratrade-champion-demo/logs/champion-demo.out.log

# Throttle search (example)
pm2 stop neuratrade-autoresearch-w2 neuratrade-autoresearch-w3
```

## Appendix B — Bend agent rules (must stay in AGENTS.md)

```
When using Bend:
- run `bend guide` to learn it
- use `LAWS.bend` to keep important rules
- run `bend PROOF.bend` before committing
- parallelize the code whenever possible
```

## Appendix C — Open owner decisions (resolve during P0–P3)

1. Per-symbol capital vs lower `--min-capital` vs smaller grid notional for demo.
2. Managed Postgres vendor vs self-hosted on a second volume.
3. When demo is allowed to use real funds vs paper-only until Rust shadow passes.
4. Autoresearch steady-state budget (workers × trials × schedule).

---

## Implementation note

Execute **Task 0 → Task 3** immediately on the live box (ops), then Task 4 in parallel with scaffolding Tasks 5–6 in repo. Do not start Task 10 until shadow parity and Bend proofs are green.

---

## Final gates (2026-09-18, owner-ordered — all must be green before P8)

- [ ] **Gate 1 — Zero warnings/errors:** `cargo clippy --offline --all-targets`
  clean, `bunx tsc --noEmit` clean (minus pre-existing TS2688 bun-types),
  `bun test` green on touched suites. No new warnings introduced per commit.
- [ ] **Gate 2 — No regression:** paper + demo soaks stay up through every
  change; `opened-count-*` never regresses to a sizing/min-capital block;
  fill-count deltas explained per cycle (clever-cabin-85m).
- [ ] **Gate 3 — Testnet momentum:** demo orders fill on testnet
  (`liveEntryCrossBps` cross-touch); `closed-24h-demo > 0` sustained across
  N consecutive monitor cycles (see the 2026-09-21 note for why the window,
  not the total, is the criterion);
  paper-vs-demo comparison runs on the SAME feed.

> **Gate 3 criterion correction (2026-09-21).** The original metric
> `opened-count-demo` never worked: the monitor grepped the soak logs for an
> `OPENED` token the ladder engine has never emitted (it prints
> `HOLD | ... open=N`), so the counter read 0 for the entire history of BOTH
> soaks. Every earlier "Gate 3 open, `opened-count-demo=0`" entry in this
> log measured a broken instrument, and no before/after comparison across
> the fix is valid. An intermediate version counted `open=N` log LINES,
> which over-reports by the number of intervals a position is held (one SOL
> rung across 3 intervals read as 3).
>
> The metric now counts closed round-trips from `ladder_paper_trades` — one
> row per trade, logs rotate but tables don't. **The criterion is the 24h
> WINDOW, not the lifetime total.** The demo's lifetime total is 28, all of
> it pre-fix history, so a total-based criterion is satisfied by work done
> before the poll fix shipped and proves nothing about it. The total is
> logged alongside as context; `total 28 / window 0` is the honest complete
> statement, and either number alone is misleading. Because this is a CLOSE
> count, it cannot see the event the poll fix actually fixed (a venue fill
> being booked); a rung that opens and holds produces zero closes by
> construction. Status is therefore two separate claims:
> - **Fills clause — SATISFIED.** SOL opened on the venue 2026-09-20
>   16:46:52Z, the ledger booked it, it survived an interval boundary, and
>   the ladder later added a second rung after price fell through the first.
> - **Round-trip clause — PENDING on a multi-hour clock.** `closed-24h-demo`
>   = 0 (lifetime total 28, all pre-fix). With `maxHoldBars: 39` (~9.75h) and
>   a ~2.5% target distance, a close cannot arrive before ~02:30Z at the
>   earliest, so a 0 here is neither a pass nor a failure for hours.
- [ ] **Gate 4 — Parallel live-shadow:** `nt-cli` paper `--shadow` replicates
  the champion-paper loop with 0 unexplained PnL/fill divergence; then cut
  paper → demo → bridge → TS per the sunset checklist.

---

## Progress log (2026-09-19 — regression sweep, branch `feat/rust-bend-strangler-p0-p5`)

> Local evidence only. Box state quoted read-only; no box writes. Commits to
> `origin/feat/rust-bend-strangler-p0-p5`: `dc719c72` shadow ledger,
> `5a192090` tick convergence, `7a7b18cc` market-entries flag,
> `0a8802b4` always-pass-venue-price fix (pushed).

| Slice | Result | Evidence |
| --- | --- | --- |
| Rust tests | PASS | `cargo test -q --workspace` exit 0 (nt-ladder: 5 unit tests — the seed-rule regression set; the rest carry coverage in `examples/`); `cargo clippy --offline --all-targets` no warnings |
| Rust examples | PASS (9 in CI) | `nt-grid`: check / paper_engine_check (fills=4 net=-1.74) / parity_checksum (`663097394`) / pump_catch / sleeves_check / grid_parity / rung_parity_probe / engine_vs_ts_probe / day_start_check; `nt-execution` + `nt-ledger`: check — all exit 0 |
| Bend proofs | PASS | `bend PROOF.bend` → `laws hold` exit 0; `bend/grid.bend` → `663097394` (matches Rust parity checksum); search/guards/sleeves elaborate exit 0 |
| TS Gate1 | PASS | `bunx tsc --noEmit` exit 0; `bun test src/paper-trading/` 152 pass 0 fail; tree clean in scope |
| Box soaks | HOLD (no regression, no fill) | paper + demo online (`bun run index.ts`, NOT native yet); demo `opened-count-demo=0`, 16h log zero real fills (4 limit attempts 00:07–01:09Z rolled back qty=0; no touch since); paper flat open=0; disk 73% |
| nt-cli binary | HEALTHY (shadow-only) | `/opt/neuratrade/bin/nt-cli -> nt-cli-7a7b18cc`, `health` → `status=ok runtime=rust-strangler` |

**Regression verdict 2026-09-19:** no regression — every local gate green,
box soaks alive with zero sizing/min-capital blocks. NOT cutover-ready:
soaks still run Bun TS (P5 Step 6 uncut), Bend search not yet in PM2
(P4 Step 5), demo never filled (Gate 3 open), shadow-vs-live daily diff
pending (Gate 4 open).

**Next toward Rust+Bend:** P4 Step 5 (Bend binary into PM2, keep JSON drop
path) → P5 Step 6 (cut champion-paper to Rust on shadow parity) → Gate 3
fill → P6 Zig ship (`build/zig-build.sh` missing; `.zig-version` pins
0.16.0) → P8 sunset.

## Progress log (2026-09-20 — parallel switch, no-cutover session)

> Box writes: versioned binaries only (`nt-cli-8440d006`, `nt-search-8440d006`);
> live symlink untouched (`nt-cli -> nt-cli-7a7b18cc`). No TS/PM2/config writes.
> TS fixes are read-only ground truth; all new fixes land in Rust+Bend (TS sunsets).

| Slice | Result | Evidence |
| --- | --- | --- |
| CI artifacts | VERIFIED | run 35502231204 green; sha256 OK both; `nt-search` → `11`, `nt-cli health` → `status=ok runtime=rust-strangler` on box |
| P4 liveness shadow | PASS | PM2 one-shot `nt-search-a07b3dd0` printed pinned `11`; process deleted after |
| P5 shadow fix (Rust+Bend) | FIXED | `nt-cli shadow` sized entries at `--pos-pct` but approved vs hardcoded `max_position_size_pct=10` → every champion entry rejected, `fills=0` on ANY panel. Now `max_position_size_pct: pos_pct`. Default runs still pin `paper_engine_check` byte-identical |
| P5 4-symbol shadow | RUN | same 39,877-bar bybit-futures 15m panels per symbol, capital 50, champion knobs step130/target195/stop2/slip2/pos50: BTC 10 fills net -3.40 / ETH 10 fills net -0.81 / SOL 10 fills net -0.78 / LINK 10 fills net -0.86 |
| Gate 4 read | N/A — engine mismatch (no comparison made) | Rust `run_paper_engine` is single-position grid (`grid-engine.ts` semantics); the box soaks run the LADDER engine ("ladder iter over 1 bars", multi-rung). A fill/PnL diff between them is meaningless. PREREQUISITE: port ladder engine to Rust (P5 Step 3 scope), OR compare Rust grid vs `grid-engine.ts` on a fixture. NOT partially satisfied — untouched. |

**Attribution note:** Rust `fills=10` per symbol = `max_trades_per_day=10` daily-count gate saturating on the full-history replay (burst 5 entries + 5 exits early, then halted) — a backfill artifact, NOT a live rate. TS tick-walk never replays this way.
**Not cutover-ready:** P5 Step 6 uncut, Bend search not in PM2 steady-state, demo `opened-count-demo=0`, Gate 3 open.

## Progress log (2026-09-20 evening — parity harness, bun out of parity CI)

> No box writes this block. All work in-repo, pushed to
> `feat/rust-bend-strangler-p0-p5`; CI `Native` green.

| Slice | Result | Evidence |
| --- | --- | --- |
| Gate 4 GRID half | CLOSED (grid only, not ladder) | `nt-grid/examples/grid_parity.rs` replays frozen TS output; `long` leg delta **0u**, `short` leg 2525u bounded by the documented asymmetric-slippage simplification (`engine.rs:124`) |
| Parity gate CI | Rust-only | TS engine's output frozen as committed CSV (deterministic, md5-stable); generator kept at `services/neuratrade-cli-ts/grid_parity_fixture.ts` as provenance. No bun in the parity step |
| Slippage debt | FOUND (bounded) | TS short leg enters/stops at `level/(1+slip)`; Rust uses `x*(10000-bps)/10000` — O(slip^2) ~2525u at 50bp. Long leg symmetric ⇒ exact. Fix is a real candidate, NOT a blocker |
| Box hygiene | DONE | live knobs snapshot at `autoresearch/results/knobs-live-20260920T1600Z.ts` (untracked, survives any restart/pull); probe PM2 entries deleted |

**Gate 4 status:** grid half green, ladder half still requires the P5 Step 3 multi-rung port (2099-line `ladder-engine.ts`). Do not read grid green as soak parity.
**Out of scope, noted:** `services/telegram-service` (grammY/Hono/gRPC) and `ccxt-service` are NOT in P8's deletion list — telegram is not a trading path; ccxt is the exchange plane Rust must absorb later (separate port, bigger than P5).

## Progress log (2026-09-20 night — debt closure, four parallel agents)

> No box writes. All work in-repo on `feat/rust-bend-strangler-p0-p5`;
> CI `Native` green at each push.

| Item | Status | Evidence |
| --- | --- | --- |
| Day boundary (engine.rs) | FIXED | `ResumeState`/`EndState` persist `day_index`/`day_fills`/`day_start_capital`; rollover at tick entry AND mid-tick; `trades_today` sources per-day fills (was cumulative `event_count` → permanent halt at 10 total fills). CLI proof: 3-day panel → tick1 fills=30, then days 4-5 → fills=50 with `day_index` 20456→20458 |
| Resume persistence | FIXED (was the real gap) | `store_resume`/`load_resume` write+parse the triple (v2 header; v1 files still parse — missing lines = unset). In-process proof never covered the file round trip, which is the only production path |
| Equity fallback | FIXED | v1 resume (no `day_start` line) fell back to the raw deposit → daily-loss gate read 16% vs 5% cap on capital 50M / realized -8M → permanent halt at every midnight and on first tick after deploy. Now `capital + closed_realized`; proven `day_start = 42M` |
| Fee split | SHIPPED (both engines) | `PaperEngineConfig.maker_fee_bp`; target exits pay maker 2bp, entries/stops taker 6bp (`champion-soak.json honestFees`). CLI: `--fee-bp`/`--maker-fee-bp`, `--fee-bp` alone keeps flat-taker reproducible (fees=242113). `paper_engine_check` pinned byte-identical; PUMPFUN drag pinned flat 69/69 |
| Parity examples | 9/9 GREEN (all nine in CI) | check, paper_engine_check (fees=242113 net=-1748112), grid_parity (long 0u / short 2525u), rung_parity_probe, engine_vs_ts_probe, sleeves_check, pump_catch, day_start_check, plus nt-execution check — all exit 0; fmt+clippy `-D warnings` clean |
| Ladder fixtures | EXTRACTED | `bend/fixtures/ladder-{6bar,boundary,drawdown}.md` — 4 portable vectors (oscillator, boundary stop-out, peak re-anchor/reseed, drawdown risk-exit), each with bars+knobs+pinned outcome and the a07b3dd0 port rule; `bun test src/paper-trading/ladder-engine.test.ts` 24 pass as ground truth |
| Ladder port spec | WRITTEN | `docs/plans/ladder-port-spec.md` (597 lines): 6 slices, state/resume model, rounding decision (micro-tolerance), named deviations |

**Corrections to the table above:** the `sleeves.rs` exit fee gap flagged mid-session is now closed — `sleeves.rs` routes `FillReason::Target` to maker and `Stop` to taker, mirroring `engine.rs`; `pump_catch`'s sleeves net moved 20441711 → 21370924 as a result. `day_start_check` (new) covers the loss-carrying midnight rollover the in-process proof could not, and `rung_parity_probe` / `engine_vs_ts_probe` / `day_start_check` are now in the CI loop.

**Gate 4 honest status:** grid half green in CI (step verified executing, not just green overall). Ladder half BLOCKED on the P5 Step 3 port — the spec is now the work order. Gate 3 still blocked on the ladder port (demo runs the ladder engine).
**Spec findings that matter for Gate 4:** (1) TS adds `liveEntryCrossBps` (15bp) to BOTH fee lines — Rust has no cross term. (2) With `--fee 0.02` and zero taker hits in the ecosystem config the soak has NO maker/taker asymmetry, so Gate 4 needs `--fee-bp 2 --maker-fee-bp 2` or a recorded re-baseline; three-leg delta: entry 3.0x over, target 9.5x UNDER (dominant), stop 3.2x over. (3) `RiskLimits::live().min_capital = 100` but `nt-cli` overrides it to 30, while the soak partitions 200/4 = 50 per symbol — so `RiskLimits::live()` used directly rejects EVERY entry at soak capital (bit `day_start_check` too; fixed there by lowering the floor to 30). Any Rust-vs-TS parity comparison must pin this or the diff is a config artifact, not engine divergence. (3) LADDER-STEP-ANCHOR: TS freezes stop at entry step but uses current-bar step for the stopRatio boundary — Rust uses exit-bar step for both.
**Note:** `bend/fixtures/ladder-6bar.md` pins TS-ladder-vs-`runLadderGridBacktest` agreement, which means the oscillator vector is engine-vs-engine (not a pure vector) — a Rust port must replay the bars itself, not port `ladder-grid.ts`.

## Progress log (2026-09-20 night — Gate 3 root cause)

> TS-only change (the live path is TS; Rust has no live adapter yet). Read-only
> box work; no PM2 restart, no soak writes.

**Root cause of `opened-count-demo=0` — fill confirmation, not rung seeding.**
From the box's own Bybit testnet history: Open Sell 0.2 SOL @109.55 at
19:57:42Z, then two Close Buys @108.27/108.28 (+0.25 realized). The demo log
for the same minute reads `not filled (status=, qty=0, avgPrice=0)` with
`open=0 closed=0`. The fills happen on the venue; the engine cannot prove them.

`96629fd1` gave limit entries a 10-attempt (~2.5s) window and left market
orders at ONE attempt on the assumption they fill instantly. The demo runs
`--demo-live-market-entries`, so every rung fill is MARKET — one `getOrder`
0ms after submit reads `status=""` (testnet not-yet-indexed), the one-shot
history fallback runs before the venue indexes it, and the bar rolls back a
real fill.

**Fix (1ee5c153):** market entries share the limit-entry window; reduce-only
closes get 3 attempts. Regression test verified by temporary revert — fails on
the old code, passes on the new. 55 adapter tests pass, tsc clean, oxfmt clean.

**Deploy BLOCKED (owner decision, not a code gap):** picking this up needs a
demo restart, and the ladder engine's live-state load force-CLOSES unknown
venue positions (`ladder-engine.ts:1794-1818`, `openRungCount(w) === 0`)
instead of adopting them — the grid engine has `reconcileLivePosition`, the
ladder never calls it. Ledger is flat (0 filled rungs on all 4 symbols), but
the ledger is exactly what the rollback corrupted, so it cannot certify the
venue is flat. No read-only Bybit position CLI exists (`exchange` only exposes
`test`; `bybit-snapshot` is not registered in the subcommand list).

**Deploy checklist when approved:** (1) confirm venue SOL position flat via
Bybit UI/API, (2) preserve the dirty `autoresearch/knobs.ts` (already snapshotted
at `autoresearch/results/knobs-live-20260920T1600Z.ts`), (3) `git pull` on the
box, (4) restart ONLY `neuratrade-champion-demo`, (5) verify the first interval
reads `open=1` or a market reason, (6) watch `opened-count-demo`.

## Progress log (2026-09-21 — gate metric repaired, Bend pin, slice 1)

> Box writes: monitor script only (no PM2 restart, no soak config). Demo was
> restarted once earlier to pick up the poll fix.

**Gate metric was broken, so prior Gate-3 readings were invalid.**
`champion-soak-monitor.sh` grepped the soak logs for the token `OPENED`,
which the ladder engine has never printed — it prints `HOLD | ... open=N`.
The counter therefore read 0 permanently: paper held genuinely open
positions (2 log lines) while its counter said 0. Every earlier
"`opened-count-demo=0`, Gate 3 open" conclusion measured a broken instrument.

**Metric now counts closed round-trips** from `ladder_paper_trades` (one row
per trade, indexed on `closed_at`), 24h window. Counting `open=N` log lines
was rejected as a replacement: one position surviving N intervals would read
as N. A failed query returns instead of coercing `unknown` to 0, so a broken
DB cannot read as zero closes. Verified on the box: **demo 0, paper 3** —
matching the DB exactly.

**Gate 3 honest status:** the market-entry poll fix (`1ee5c153`, deployed) is
confirmed live — SOL opened on the venue, the ledger booked it, it survived an
interval boundary, uPnL improving (-0.1605 → -0.0224). But the demo has
**0 closes in 24h**, so the gate's success condition (sustained fills) is NOT
met. The fix works; the evidence is not yet collected.

**Bend pin:** upstream installer moved to 2.0.21 while CI pinned 2.0.20, which
failed the version assertion deterministically (~1.4s — not a network flake, so
retrying could never help). All four kernel checksums re-verified locally on
2.0.21 before bumping: grid 663097394, guards 2065, search 11, sleeves 13.
`search` matters most — its header documents F32 codegen quirks verified against
an older compiler, and it reproduces exactly. A patch that deleted the install
line from both steps was caught and reverted in the same session.

**Slice 1 landed (`nt-ladder` crate):** `Rung`/`Side`/`LadderState` 1:1 with TS
`LadderPaperState`/`LadderPaperRungState`, resume in a new `ladder v3` format so
a grid resume fails loudly rather than parsing zeros. Two parser defects shipped
in the first commit and were fixed in the next: `load` accepted a truncated file
(`ladder v3\ncapital 50000000\n`) as a valid zero-capital state, and
`BadRungLine` reported a rung count instead of a file line. A fixed-point
assertion guards the tick loop's per-interval rewrite. `entry_bar` is stored
absolute rather than window-relative (recorded deviation).

## Gate 3 — first round-trip observed (2026-09-21 02:30Z)

The demo completed its first closed round-trip under the corrected metric.

| Field | Value |
|---|---|
| Symbol / side | SOL/USDT:USDT short |
| Entry → exit | 109.62519496 → 111.502296 |
| Reason | `max_hold` |
| Closed at | 2026-09-21T02:30:00Z |
| Metric | `closed-24h-demo` 0 → **1** |

Three facts this settles, all previously speculation in this plan:

1. **The exit path fires.** The rung filled at 16:46:52Z and max-hold closed it at
   02:30Z — 9.75h = `maxHoldBars: 39` × 15m, exactly on schedule.
2. **`max_hold` is per-rung, not per-ladder.** `open` went 2 → 1 at the close:
   one rung exited, the other is still held. So `closed-24h-demo` counts rows
   in `ladder_paper_trades` and two rungs closing is 2, not 1.
3. **The exit was a loss** (111.50 vs 109.63 on a short) — expected for a
   max-hold exit, since neither target nor stop was reached in 9.75h. This is
   evidence the exit path works, NOT that the strategy is profitable.

**Gate 3 status:** fills clause proven earlier (SOL rung 1 at 16:46:52Z booked
and surviving an interval boundary, rung 2 added by design); round-trip clause
now has its first data point. Sustained > 0 still needs accumulation across
cycles — one close is the criterion becoming measurable, not the criterion met.
