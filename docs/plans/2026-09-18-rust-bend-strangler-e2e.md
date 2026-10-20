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
  change; `closed-24h-*` never regresses to a sizing/min-capital block;
  fill-count deltas explained per cycle (clever-cabin-85m). The criterion is
  `closed-24h-demo` **strictly increasing** over the window, read together
  with the per-close `exit_reason` mix (`target` / `stop` / `max_hold`) from
  `ladder_paper_trades` — so a timeout is never read as an edge.
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
> the fix is valid. **Current state (2026-09-21, superseded):**
> `closed-24h-demo=1` — the round-trip clause now has one observation, a SOL
> short closed `max_hold` at 02:30Z. The `opened-count-*` and
> `closed-total-*` files this criterion once named no longer exist (the four
> state files were deleted from the box); see the Gate 3 entry below for the
> evidence and its limits. An intermediate version counted `open=N` log LINES,
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
reads `open=1` or a market reason, (6) watch `closed-24h-demo` — strictly
increasing, and check each close's `exit_reason` so a `max_hold` timeout is
not counted as a target or stop edge.

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
3. **The exit was a `max_hold` timeout at a loss** (111.50 vs 109.63 on a
   short). This is WEAK evidence and must not be read as the round-trip clause
   being satisfied: neither the target nor the stop was reached in 9.75h, so
   the position closed by timing out rather than by taking profit. It proves
   the exit path fires and that `max_hold` is per-rung; it says nothing about
   expectancy. A strategy that only ever closes by timing out is not taking
   profits, and one `max_hold` close is not a sustained count. Record
   `exit_reason` per close — a healthy ladder should show `target` closes
   outnumbering `max_hold` and `stop` ones.

**Gate 3 status:** fills clause proven earlier (SOL rung 1 at 16:46:52Z booked
and surviving an interval boundary, rung 2 added by design); round-trip clause
now has its first data point. Sustained > 0 still needs accumulation across
cycles — one close is the criterion becoming measurable, not the criterion met.

## Progress log (2026-09-22 — ladder slice 4 complete: parity + risk exits)

> No box writes. All work in-repo on `feat/rust-bend-strangler-p0-p5`.

| Slice | Result | Evidence |
| --- | --- | --- |
| 6bar parity (`fdadd642`) | PASS | `nt-ladder/examples/ladder_parity.rs` replays `bend/fixtures/ladder-6bar.md`: fills/closes/reasons/win-loss exact, V1 capital delta **7.9e-7**, V1b **1.7e-7** (budget 1e-6 = `Money` micro-USDT's 6dp ceiling — TS uses arbitrary-precision Decimal, so 1e-9 would fail correct code). Fees now bp-exact (`maker*2+cross` target / `maker+taker+cross` stop), capital conversion rounds (truncating twice per round-trip compounded to 1.2e-6, past the ceiling) |
| Boundary stop-out (`1932746`) | PASS | Whole-side risk exits ported: `resolve_risk_exit` runs after fill, before targets, no same-bar gate. Legacy boundary TS-exact on the EXIT-bar step → exits **93.049 x2** pinned, capital delta **7.7e-7**, losses 2, pause 3→2 on free tick, long side cleared, short stays armed |
| `LADDER-STEP-ANCHOR` stopRatio | PROVEN DIVERGENT | stopRatio 1.58 boundary uses the rung's FROZEN entry step → **96.42**; TS exit-bar form would be 96.424774. The deviation is now executable, not just documented |
| Drawdown semantics fix | FIXED | Re-anchor + seed gate used `peak > capital` (any dip fires) instead of TS's `accountDrawdownBreached` threshold — below `maxDrawdownPct` TS keeps seeding with the old peak, never pauses. One shared helper now feeds both sites; `BarContext.max_drawdown_pct` carries the knob (TS reads opts; not in the 10-field fingerprint) |
| `side_exit_price` form fix | FIXED | Was division (`p/(1+slip)`); TS `sideExitPrice` is multiplicative (`p*(1±slip)`) — wrong for max-hold + every risk exit by O(slip^2), same class as nt-grid's recorded short-leg debt. Fixtures run at slip 0, so no pin moved |
| Drawdown fixture (`63d4c043`) | PASS | Replays `ladder-drawdown.md` (ENA-latch case): peak 91/paused 3 pinned on bar 1, armed rungs survive 3 pause ticks, bar-5 fill entryBar 5 / entryTimestamp 4500000 / qty 45.5/99, capital 91 untouched |
| `entry_bar` window-index fix | FIXED (found by the fixture's `entryBar 5` pin) | Fill bar was stored as `bars_consumed` (skips paused bars, starts 0 for a window beginning at bar 1) while the conservative gate reads `ctx.bar_index` — two coordinate systems; post-resume `entry_bar` (400+) could never be `< bar_index`, barring target exits forever. Now `ctx.bar_index` = TS `entryBar` 1:1 (`types.ts:257-258`); `bars_consumed` stays a persisted stat |
| Suite | PASS | 21 tests, clippy 0 warnings, fmt clean, all 13 CI examples exit 0 |

**Ladder port status:** slices 1-4 code-complete against THREE frozen
fixtures (6bar target, boundary stop-out, drawdown re-anchor). Remaining:
slice 5 (fee basis pin to the soak `--fee-bp 2 --maker-fee-bp 2` + shadow
assertion), slice 6 (deferred-surface triage: liquidation,
`maxPositionDrawdownPct`, funding, leverage branch), then Gate 4's ladder
half can run.

## Progress log (2026-09-22 later — V1c cross pin + nt-cli ladder-shadow)

> No box writes yet this block. All work in-repo, pushed.

| Slice | Result | Evidence |
| --- | --- | --- |
| V1c cross term (`98b187ae`) | PASS | `ladder_parity` V1c replays at the soak's `liveEntryCrossBps 15`: fee line = maker 5bp*2 + 15bp = 25bp/target close, capital 100.7515063850135 exact-rational, micros delta 6.15e-7. Cross cost of the round trip **-0.1506 USDT** — the item a Gate 4 diff must attribute to cross, not rounding. `replay()` now takes `cross_bp` |
| `nt-cli ladder-shadow` (`e0cf5687`) | PASS | Gate 4 ladder-half primitive: panel CSV → `nt-ladder`, output line itemises `target/stop/maxhold/liquidation` reasons + `capital/paused/open`; `--ledger` appends one CSV row per closed rung. Cursor lives IN the ladder v3 `--resume` (`last_ts`), so there is no second `--state` file to drift; fresh start = window index 1 (TS `resolveLadderStartIndex:1739`). Defaults = the deployed soak's exact knobs (step130/target195/stop158/pause2/grids2/hold39/fee2-2/cross15/slip2/pos50/dd15/rungs2/15m/50U), every one a flag so a diff artifact carries its basis on the command line |
| Grid-shadow non-regression | PASS | Panel load extracted to shared `load_panel()`; same CSV before/after → byte-identical `shadow bars=4 fills=3 gross=1010030 fees=1400 net=1008630 events=3` |
| CLI fixture pins | PASS | 6bar V1 through `ladder-shadow` → `capital=100902126` (=100.902126, the example's pin); boundary → `capital=94447135`, `stop=2` exits @93.049, `paused=3`; rerun with no new bars → `tick bars=0`, cumulative wins/losses/capital intact |

**Next:** CI x86_64 artifact → versioned `nt-cli` on the box (symlink
untouched, per the box-write rule) → first Gate 4 ladder diff: TS
`ladder_paper_trades` rows vs `nt-cli ladder-shadow --ledger` over the
same window at the soak knobs, with every line item attributed
(rounding 1e-6, `LADDER-STEP-ANCHOR`, fee split, cross 15bp, taker rate).
P4 Step 5 stays BLOCKED-as-written: `nt-search` is the screen-slack
kernel (prints its checksum), not a trial loop — swapping PM2's
`autoresearch-w*` for it would kill research. The swap needs the worker
to call the kernel first; never regress the running loop.

## Progress log (2026-09-23 — first Gate 4 ladder diff + TS live target-gate root cause)

> Box touched only read-only (sqlite/pm2/log reads). CI gate repair is
> in-repo. No live-path writes.

| Item | Result | Evidence |
| --- | --- | --- |
| Lightweight gate, root cause 1 | FIXED (`96f399db`) | Workflow ran `bun test` on a fresh checkout with no `bun install` → `Cannot find package 'effect'` in 8s on every push since the workflow existed. Added frozen-lockfile installs for `neuratrade-cli-ts` + `telegram-service` before `make test`. Run time 13s → 2m12s (tests actually execute) |
| Lightweight gate, root cause 2 | FIXED (`2d7e6ae`) | After install, one failure remained: `champion-soak-monitor.test.ts` pinned the pre-09-21 contract (`opened-count-*.txt`, `NEW LADDER ENTRIES`) while the monitor now counts closed round-trips from `ladder_paper_trades` (`closed-24h-*.txt`, `NEW LADDER CLOSES`). Test realigned; `bun test champion-soak-monitor` → 2 pass, typecheck clean. The 4 remaining oxlint errors (3× complexity in the sunset engine, 1× assertion in `grid_parity_fixture.ts`) predate this work — debt, not this gate |
| First Gate 4 ladder diff (TS ledger vs Rust replay, soak knobs, window `09-18T00:00`) | RAN — divergence quantified | TS closes since 09-18: **24** (BTC4/ETH6/SOL6/LINK8); Rust fresh replay: **38** (BTC8/ETH9/SOL9/LINK12). Capital: BTC/ETH/SOL within ±4.5 USDT; **LINK widest: TS 52.78 vs Rust 48.26**. Rust reasons include 5 `target` exits (ETH1/SOL1/LINK3); TS reasons: `max_hold`/`stop` only |
| Root cause of the target gap | PROVEN FROM SOURCE | TS lifetime ledger: **70 closes = 52 max_hold + 18 stop + 0 target**. With the deployed soak knobs (`trend 0`, `chop 0`, no replay), `ladderRequiredCandles` returns **2** (`ladder-engine.ts:1968-1973`): every live iteration is a 2-candle sliding window, so a fill stamps `entryBar 1` and every later iteration's current bar is also `1`. The conservative gate `entryBar < barIndex` (`types.ts` same-bar rule) is therefore false **forever** across interval boundaries — target exits are structurally unreachable in live TS; only the ungated `stop`/`max_hold` paths ever fire. Unit fixtures don't catch it because they replay one growing window |
| Diff attribution (what is NOT a Rust defect) | RECORDED | The 24-vs-38 gap and the LINK capital split decompose into: (a) window origin — TS state was seeded 09-07…09-11 and carries capital/rungs across the 09-18 cohort lock, Rust replay starts fresh; (b) target-gate starvation — Rust finding targets where TS structurally cannot is the fix inheriting with the cutover, NOT divergence to chase to parity; (c) rounding 1e-6 / `LADDER-STEP-ANCHOR` / cross-15bp, itemised earlier |

**Cutover note (P5 Step 6):** when champion-paper cuts to Rust, `target`
closes start appearing for the first time — soak dashboards and Gate 3
expectancy reading must treat that as the corrected baseline, and the
live Rust caller must keep supplying a monotonically growing window (or
re-anchor `entry_bar`) so the conservative gate keeps TS's same-bar
semantics without inheriting its starvation.

**Next:** ~~watch Lightweight at `2d7e6ae`~~ → GREEN (`d090fb3b`, first
green Lightweight on this branch: install fix + test realign both proven
in CI). Versioned binaries installed (`nt-cli-6f207dc4`,
`nt-search-6f207dc4`, sha-verified, `health`=ok, search gate `=11`,
symlink untouched — one scp-followed-symlink incident during the deploy
was fully repaired: `nt-cli-7a7b18cc` restored byte-true from the local
cross-build, stable path re-created). Forward diff driver landed as
`scripts/gate4-shadow-diff.sh` (box read-only, local `/tmp/gate4`,
idempotent): first forward run walked 3 new bars per symbol
(`tick bars=3`, no new rust closes) and printed the window diff —
TS 25 closes (0 target) vs Rust 38 (5 Target). Remaining: accumulate
Gate 3/4 evidence across cycles, then P5 Step 6 cutover prep; P4 Step 5
stays blocked-as-written (kernel must be called by the worker, never
swap PM2 under a running research loop).

## Progress log (2026-09-28 — Gate 4 cycle 1 on post-deploy basis + attribution (d))

> Box touched read-only (sqlite/pm2/log reads) except `pm2 stop
> neuratrade-autoresearch-w2 w3` (plan Task 2 Step 2 drift revert, back to 2
> workers). No live-path writes. `/tmp/gate4` reseeded from `SINCE=2026-09-18`
> (994 bars/symbol); reseed reproduces the 09-23 basis (Rust BTC 8 closes /
> 47.925355 byte-identical), so panels are comparable going forward.

| Item | Result | Evidence |
| --- | --- | --- |
| Diff attribution (d): seed-persistence deploy boundary | RECORDED | `a07b3dd0` (2026-09-20 06:52Z, clever-cabin-85m) added Tier-2 keep-armed-rungs to `seedLadderSide` (+3 lines). Before it, TS re-centered base+rungs from every flat bar's open; after it, armed rungs persist. Rust (`seed_side` tier 2, `nt-ladder`) replays post-deploy semantics from the 09-18 panel start, but live TS ran old semantics 09-18→09-20. Proof: TS LINK long fill 09-20T02:30 entry 12.10402732 = `12.261 open − 1.3% step`, `×1.0002` slippage exact to 1e-8 (fresh same-bar seed); Rust LINK pins `long_base 11388000` (bar-0 open) 10 days later, panel min-low 11.337 never touching rung 11.24 — all 21 Rust LINK closes Short vs TS 9 long/12 short |
| Ruled out for the 09-17→09-20 LINK gap | PROVEN | No unrecorded close (capital continuous 52.23→52.23; "52.57" was that row's `capital_before`), no DD block (sleeve peaks ≤4.5%: LINK 53.23/52.52, BTC 50.85/48.53 — `ladder_paper_state`), no force-reseed (would move/reset capital), 09-18 breaker trip reset 09-19 (`risk_circuit_breaker` open=0), 09-19 err log = frozen-knob warnings only (no feed hold) |
| Re-baseline decision | RECORDED | The ≥09-18 window permanently mixes seed regimes, so no clean-cycle count can include the 09-18→09-20 span. This pass = **cycle 1 on the post-deploy basis** (both sides run keep-armed rules from here). LINK side-mix difference is permanent for the window, not divergence |
| Cycle 1 diff (TS 49 closes / Rust 53) | 0 unexplained under (a)+(b)+(c)+(d) | BTC 6 (4MH+2S)/48.53 vs 8 (5MH+3S)/47.93; ETH 9/48.86 vs 10 (+1T)/49.08; SOL 13/48.85 vs 14 (+3T)/46.89; LINK 21 (14MH+7S)/52.52 vs 21 (6MH+9S+6T)/46.49. TS 0 target (2-candle starvation, unchanged); Rust targets = fix-inheriting (b). LINK cap gap = (b)+(c step-anchor: Short-only replay vs mixed live through the 09-23→27 rally)+(d) |
| Demo guard finding (Gate-3 watch) | RECORDED | Demo SOL 30.27; `risk_circuit_breaker` tripped 4 straight days 09-24→27 (daily loss 6.7/9.3/6.7/5.8% vs 5% cap), reset 09-28 UTC (row `-1.03|0`). SOD row writes ~50 (BTC/ETH/LINK sleeve) so SOL's daily bleed reads ~3× understated — per-sleeve breaker with account-keyed SOD. ~2 USDT headroom 09-28; re-trip can fire this UTC day |

**Next:** daily `./scripts/gate4-shadow-diff.sh`; one more consecutive clean pass makes two, then P5 Step 6 cutover prep. P4 Step 5 stays blocked-as-written. Instrument note: guard-hold iterations leave no per-symbol trace (HOLD without save) — log hold reason per symbol if Gate-4 ever needs seeded-not-touched vs held distinguished.

## Progress log (2026-10-20 — dependency upgrade + Gate 4 cycle 2, `stranded armed ladder`)

> Box touched read-only (sqlite/pm2/log reads) — no PM2 restart, no soak or
> config writes. All repo work on `feat/rust-bend-strangler-p0-p5`.

### Dependency upgrade (all manifests)

| Surface | Result | Evidence |
| --- | --- | --- |
| Rust | NOTHING TO UPGRADE — verified, not skipped | Every crate in `crates/` depends only on path members; `crates/Cargo.lock` lists the 7 workspace crates and no third-party package. The one-line lock diff was the missing `nt-ladder` member entry |
| cli-ts runtime | UPGRADED | `effect` 4.0.0-rc.108 → **4.0.5** (first stable v4), `@effect/platform-bun`/`-node` → 4.0.5, `alchemy` 2.0.0-beta.76 → beta.83, `zod` 4.5.4 → 4.6.7, `ky` 2.1.0 → 2.2.0 |
| cli-ts tooling | UPGRADED | `@effect/tsgo` 0.41.0 → 0.54.1, `oxlint`/`@oxlint/plugins` 1.81.0 → 1.89.0, `oxlint-tsgolint` 7.0.2001 → 7.0.2004, `@types/bun` 1.4.1 → 1.4.3, `fast-check` 4.9.0 → 4.10.2 |
| telegram-service | UPGRADED | `@sentry/bun` 10.73.0 → **11.8.0** (major), `hono` 4.13.5 → 4.13.13, `@bufbuild/protobuf` 2.14.1 → 2.16.0, `@grpc/grpc-js` 1.14.4 → 1.14.6, `effect` → 4.0.5, same tooling bumps |
| **Breaking change 1** | FIXED (22 call sites, 5 files) | effect 4.0.5 capitalises the Config constructors: `Config.string/boolean/redacted(...)` no longer exist, `Config.String/Boolean/Redacted(...)` do (`effect/dist/Config.d.ts:1004,1244,1406`). Touched `src/services/bitget-config.ts`, `src/services/bybit-config.ts`, `index.ts`, `alchemy.run.ts`, `telegram-service/src/config.ts`. The 16 tsc errors included `Layer<X, unknown, unknown>` / `Effect<..., unknown, never>` cascades — all were symptoms of the missing constructor types, not separate breakages; they vanished with the rename |
| **Breaking change 2** | VERIFIED AT RUNTIME | `@sentry/bun` 11 major. `sentry.ts` is unaffected, but `sentry-bun.d.ts` is a local `declare module` shim, so **tsc cannot see a v11 API break** — the guard is a runtime init with the real options (`integrations` as a filter function + `beforeSend`): `INIT_OK version=11.8.0`, `getClient().getIntegrationByName` present |
| Gates after upgrade | GREEN | cli-ts `tsc --noEmit` 0 errors, `bun test` **1431 pass / 0 fail**; telegram `tsc` 0, `bun test` **186 pass / 0 fail**; `cargo test --workspace` 21 tests pass, `cargo clippy --offline --all-targets -D warnings` clean; CLI boots (`--help`, `scalp --help` exit 0); both services bundle (435 / 454 modules) |

Note: a stale `node_modules` (`ENOENT … node_modules/zod`) made the first baseline read red — `bun install --frozen-lockfile` restored it, and the pre-upgrade baseline was green (1431 / 186). The upgrade was measured against that, not against the broken tree.

### Gate 4 cycle 2 (window ≥ 2026-09-18, panels reseeded 3112 bars/symbol)

| Symbol | TS (DB) closes | Rust (fresh replay) closes | TS cap | Rust cap |
| --- | --- | --- | --- | --- |
| BTC | 14 (12 max_hold, 2 stop) | 8 (5 MH, 3 stop) | 47.99 | 47.93 |
| ETH | 25 (21 MH, 4 stop) | 22 (14 MH, 1 stop, 7 target) | 48.74 | 51.77 |
| SOL | 38 (31 MH, 7 stop) | 15 (6 MH, 6 stop, 3 target) | 47.75 | 47.25 |
| LINK | 61 (34 MH, 27 stop) | 29 (6 MH, 12 stop, 11 target) | 43.17 | 47.27 |
| **Total** | **138** | **74** | | |

**New attribution (e) — `stranded armed ladder` (PROVEN FROM GEOMETRY, not inference):**
Rust goes silent mid-window on BTC/SOL/LINK (last closes 09-22T18:30Z,
10-02T14:30Z, 09-28T23:30Z) while ETH keeps trading to 10-19. Cause is
not arithmetic: after the last close both sides stay armed at levels the
market never revisits.

| Symbol | Armed long rungs vs post-close min_low | Armed short rungs vs post-close max_high | Bars silent |
| --- | --- | --- | --- |
| BTC | 75,431 / 74,438 vs **79,750** | 87,484 / 88,607 vs **87,282** | 2,653 (28d) |
| SOL | 100.12 / 98.80 vs **100.32** | 122.95 / 124.53 vs **122.28** | 1,709 (18d) |
| LINK | 11.240 / 11.092 vs **11.508** | 15.818 / 16.021 vs **15.777** | 2,057 (21d) |

The seed rule re-anchors only an EMPTY side (`seed_side` tier 2 == TS
`seedLadderSide:606-609`), so a side armed outside the later range stays
armed forever. This is **not** a Rust port defect — TS has the identical
early-return — and the same failure mode is visible in production: demo
BTC and LINK have **0 fills in six weeks**. Two of the three misses are
tiny (SOL 0.2%, LINK 0.26% past the rung), so the difference between a
silent symbol and a trading symbol is path luck, not edge.

**Gate 4 honest status:** cycle 2 is NOT a second clean pass. The 138-vs-74
gap decomposes into (a) window origin (TS carries pre-09-18 capital/rungs),
(b) target-gate starvation (TS 0 targets again; Rust's 21 targets inherit as
fixes), (c) rounding / `LADDER-STEP-ANCHOR` / cross-15bp, and (e) stranded
armed ladders above. Do not read (e) as parity-fixable: re-anchoring a
stranded side is a **strategy change** that moves the hunt's P&L, so it is an
owner decision, not a silent patch. Until it is decided, a same-feed
fill/PnL diff cannot be "0 divergence" — the compare is dominated by path
dependence.

**Box snapshot (read-only, same session):** paper equity 187.65/200, 185
lifetime closes / 6 in 24h; demo 178.85/200, 72 lifetime / **0 in 24h**
(last close 09-28T14:00Z); demo SOL permanently blocked (`capital 28.56 is
below minimum 30`); both soaks online 21h (host rebooted 10-19), disk 73%,
2 research workers at 100% CPU each, breaker clean.

**Next:** owner call on the re-anchor policy → then re-run the diff on a
shared starting state (same seed bar for both sides) so (a) stops masking
the engine comparison. Demo needs its min-capital/SOL decision before Gate 3
can move at all. P4 Step 5 stays blocked-as-written.

## Provenance check — box binaries (2026-10-20)

> Read-only: `sha256sum` on the box, `scp` of the three **versioned** files
> only (never the `nt-cli` symlink — that is what corrupted `7a7b18cc` during
> the 09-23 deploy). Local copies re-hashed and matched the box byte-for-byte.

| Artifact (filename == commit prefix) | SHA-256 | `ladder-shadow` | `--maker-fee-bp` |
| --- | --- | --- | --- |
| `nt-cli-8440d006` | `994d1007…0bcec287` | no | no |
| `nt-cli-7a7b18cc` **← live symlink** | `faccc0b0…02c5771d` | **no** | **no** |
| `nt-cli-6f207dc4` | `109b30dc…220adbf8` | **yes** | **yes** |
| `nt-search-6f207dc4` | `2ef0b124…56b7a33c` | — | — |
| `nt-cli-a07b3dd0` | `381d267f…a413d457` | — | — |

**Finding (artifact evidence, not inference):** the live `/opt/neuratrade/bin/nt-cli`
is the **pre-`ladder-shadow`** build. Its embedded usage string is the old
`nt-cli shadow --bars …` with no `--maker-fee-bp`; `6f207dc4` carries both
`ladder-shadow` and the maker-fee flag. So the plan's "symlink untouched"
note is now verified against the bytes, and anyone running `nt-cli
ladder-shadow` from the stable path on the box gets an unknown-command, not
a Gate-4 run. The Gate-4 driver was always invoked with the local release
build (`NT=./crates/target/release/nt-cli`), which is why cycle 1/2 still ran.

All three are **stripped** PIE x86-64 ELF, so symbol-level provenance is not
recoverable from the artifacts themselves. REA's `inspect-binary-layout`
provider (`pwntools-elf`) requires a **Linux x64 host** and refuses on the
local macOS/arm64 machine (`capability_unavailable` / `unsupported_provider`).
Deeper layout/symbol evidence would need REA run on the box or in an x64
container; the hash + usage-string evidence above already answers the live
question.

**Consequence for P5 Step 6 (cutover):** the cutover cannot reuse the stable
`nt-cli` path as-is — it must point at a build that actually contains the
ladder engine, and the install must copy the resolved file, never follow the
symlink.

## Live target-gate fix (2026-10-20, TS)

> No box writes. In-repo only; not deployed (a deploy restarts the soaks).

`closeLadderTargets` now measures the conservative-intrabar same-bar rule with
`entryTimestamp` (the absolute fill clock max-hold already uses) instead of the
window-relative `entryBar`:

```ts
enteredOnCurrentBar(rung, candle.timestamp.getTime(), ctx.barIndex)
  = rung.entryTimestamp > 0 ? entryTimestamp >= currentBarTs : entryBar >= barIndex
```

The index rule stays only as a fallback for states persisted before
`entryTimestamp` existed. Live fetches 2 candles, so every iteration's current
bar is index 1 and a fill stamps `entryBar 1`: `entryBar < barIndex` is false
forever and **target exits were structurally unreachable** on the live path
(70 lifetime demo closes = 52 max_hold + 18 stop + 0 target). Replay keeps the
same semantics because bars are timestamp-ordered, so `entryBar < barIndex`
and `entryTs < barTs` agree there.

**Proof the test catches the bug:** the new case
(`live 2-candle rolling window — target exits`) fails on the old gate —
`Expected: 1, Received: 0` closes — and passes on the new one. Suite 1431 →
1433, all green, tsc/oxlint/oxfmt clean.

**Not yet observable:** the soaks still run the pre-fix code until the box is
synced and the champion apps restarted, so `closed-24h-*` and the `target`
exit mix will not move until then. This is the intended baseline change —
Gate 3's expectancy read must treat the first target closes as the corrected
baseline, not as a strategy shift.

## Same fix ported to `nt-ladder` (2026-10-20, Rust)

Ported in the same session, because the gate is a **parity** rule: leaving Rust
on the index comparison would have made every future Gate-4 target diff
attributable to the fix rather than to the engines.

`close_targets` (`crates/nt-ladder/src/lib.rs`) now calls
`entered_on_current_bar(rung, ctx.candle.ts_ms, ctx.bar_index)` — the module's
own `entry_ts_ms` was already persisted in `ladder v3` and already drove
max-hold, so no state-format change was needed. The fallback to `entry_bar`
keeps resume files written before `entry_ts` readable.

**Evidence, not assertion:**

| Check | Result |
| --- | --- |
| New case `target_exit_survives_a_sliding_window_where_the_current_bar_is_always_index_1` (`tests/tick.rs`) | PASS — seeds and fills at `bar_index 1`, then target-exits on the next tick, also at `bar_index 1` (the live 2-candle shape) |
| Same case against the OLD index rule (temporarily restored, then reverted) | **FAIL** — `closes: []` where 1 expected |
| `cargo test --offline --workspace` | 8 tick tests + all workspace suites pass |
| `cargo clippy --offline --all-targets -D warnings`, `cargo fmt --check` | clean |
| `cargo run --example ladder_parity` | every pin unchanged (V1 100.902126, V1b 102.718527, V1c 100.751507, boundary 94.447135 / 93.049 / 96.42, drawdown 91) — replay results are identical, as they must be, since on a monotonic panel `entry_ts < bar_ts` and `entry_bar < bar_index` agree |

The Rust module docs previously told callers to re-anchor `entry_bar` when
sliding the window; measuring the entry in time removes that obligation, so
the note now says what it actually guarantees.

## DEPLOYED to the box (2026-10-20 15:16–15:19 UTC)

The owner approved the deploy window. Only the two champion apps were
restarted; every other pm2 app was left untouched.

| Step | Evidence |
| --- | --- |
| Box checkout `601c5e8a` → `21771e37` (fast-forward, ancestor verified locally) | `git log --oneline -1` on `/opt/neuratrade` = `21771e37` |
| Box-local edits parked, nothing discarded | `champion-soak-monitor.sh` stash hash **== HEAD** (the edit was already upstream, loss impossible); the `knobs.ts` experiment differs from HEAD and survives in `stash@{0}` + `/tmp/predeploy-20261020T151634Z/box-local.diff`. It is not in the live path — both soaks pass explicit CLI flags |
| Deps under the new runtime (`effect` 4.0.5, zod 4.6.7, ky 2.2.0, alchemy beta.83) | `bun install --frozen-lockfile` → 69 packages; `bun x tsc --noEmit` exit **0** on the box; `scalp --help` and `scalp paper-trade --help` exit **0** |
| Restart | `pm2 restart neuratrade-champion-paper neuratrade-champion-demo`; both `online`, restarts 1 |
| **The running process is the fixed code** | file mtime `15:16:40Z` < process start `15:19:10Z` (paper) / `15:19:11Z` (demo) |
| State continuity | paper `capital 187.74082479` / equity 186.50, per-symbol BTC 47.99 · ETH 48.81 · SOL 47.75 · LINK 43.19 — identical before and after; demo `178.85168868` unchanged. Open rung counts preserved (BTC 1 · ETH 1 · SOL 2 · LINK 2) |

### The defect, visible in the production state itself

Reading `ladder_paper_state` on the box: **all six open rungs carry
`entryBar = 1`** (BTC short r1, ETH short r1, LINK short r1/r2, SOL short
r1/r2). That is the root cause in production data, not an inference from a
test — with the live 2-candle window the current bar is always index 1, so
`entryBar < barIndex` was false for every rung that ever existed.

Reason mix proving the starvation: **paper 187 closes = 137 max_hold + 50 stop
+ 0 target; demo 72 = 54 stop + 18 max_hold + 0 target.**

### What must happen next, and how it will be visible

No close appeared on the 15:30Z bar (no target touched). The open shorts sit
1.0–2.4% from their targets:

| Rung | Target | Mark | Distance |
| --- | --- | --- | --- |
| LINK short r2 | 12.0263 | 12.1630 | **−1.12%** |
| SOL short r2 | 104.4765 | 105.5500 | **−1.02%** |
| BTC short r1 | 81138.65 | 82382.30 | −1.51% |
| ETH short r1 | 2385.50 | 2436.00 | −2.07% |
| SOL short r1 | 103.1191 | 105.5500 | −2.30% |
| LINK short r1 | 11.8701 | 12.1630 | −2.41% |

The first `target` row in `ladder_paper_trades` — a reason value that has
**never once appeared in 259 lifetime closes** — is now a price event, not a
code event. Gate 3's expectancy read must treat the first few target closes as
a corrected baseline, not a strategy shift.

**Unchanged by this deploy:** demo SOL is still blocked (`capital 29.36 <
--min-capital 30`), which is the separate min-capital decision, and the
stranded-rung re-anchor question is still open.

## Demo SOL unblock — decision (2026-10-20)

**Decision: lower the floor to 30% of par (`--min-capital` 30 → **15**),
expressed as a derived constant, not a new magic number.**

The ecosystem file's own comment already recorded the first occurrence of
this deadlock: `—min-capital 50` (== par) was a one-loss ratchet, so it was
lowered to 30. SOL then hit 28.54 and died at 30 — the same deadlock one
notch down. A fixed number cannot survive its own purpose here; a fraction of
par can be reasoned about.

Why this is not a risk weakening: `minCapital` was never the orderability
guard. `ladderRungQty` decides whether a rung can be placed — contract-spec
min qty, `orderableQty` floor-up, `dynamicLeverage`, account-scaled cap, and
a visible `skipReason` when it cannot. At the SOL sleeve's capital the engine
needs 1.48x for one min-size rung (0.1 SOL) and the account-scaled cap at
under 500 USDT is 10x discounted by the 50% budget → 5x, so the rung is
placeable. 15 still refuses a sleeve that has lost 70% of its stake.

Rejected alternatives: reseeding the SOL sleeve to 50 (breaks PnL continuity
— the sleeve's loss is the measurement), and leaving it blocked (the demo
exists to prove testnet routing across four symbols; a dead sleeve proves
nothing and the block is permanent by construction).

Applied to **both** champion apps, because they share one config path: paper's
smallest sleeve is 43.19 so its behaviour is unchanged today, and leaving it
on a stale 30 would desync `pm2 save` from the file.

**Acceptance check:** the SOL iteration line must stop carrying
`pre-trade risk check failed: capital ... is below minimum 30` and read as a
normal `HOLD` alongside the other three symbols.

## Stranded-rung re-anchor — MEASURED, decision: keep it OFF (2026-10-20)

Gate 4 finding (e) said a re-anchor rule must not be slipped in silently
because it moves the hunt's P&L. It has now been measured instead of argued.

**Instrument.** `nt-ladder` gained `BarContext::seed_reanchor_bps` (0 = off,
the default and the only value any live path passes): a side that is armed and
has **never filled** is cleared once the whole bar has cleared that side's
anchor by `bps`, after which the existing tier-3 seed re-anchors at the bar's
open. A side holding any filled rung is never touched. The third arm of the
test pins exactly that boundary. `nt-cli ladder-shadow --seed-reanchor-bps`
exposes it; `scripts/seed-reanchor-diff.sh` sweeps it.

**Inputs.** The same 3,112-bar bybit-futures 15m panels per symbol
(2026-09-18 → 2026-10-20), the deployed soak knobs, 50 USDT per sleeve, fresh
resume per arm so every arm starts from the same seed bar. The off arm
reproduces the documented Rust replay exactly (**74 closes, 75 fills**, BTC
47.93 / ETH 51.77 / SOL 47.25 / LINK 47.27) — that is the harness check.

**Reproduce the table below with one command** (deterministic: re-running it
reproduces all ten rows digit for digit, verified):

```bash
cargo build --offline --release -p nt-cli   # from crates/
ARMS="0 10 25 50 100 200 400 600 1000 2000" ./scripts/seed-reanchor-diff.sh
```

| `bps` | capital (4 sleeves) | Δ vs off | closes | target | stop | max_hold | fills |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **0 (shipped)** | **194.21** | — | **74** | 21 | 22 | 31 | 75 |
| 10 | 168.38 | **−25.83** | 448 | 84 | 100 | 264 | 452 |
| 25 | 167.36 | **−26.86** | 434 | 82 | 97 | 255 | 438 |
| 50 | 165.54 | **−28.67** | 406 | 72 | 94 | 240 | 411 |
| 100 | 169.91 | −24.30 | 372 | 69 | 82 | 221 | 376 |
| 200 | 180.03 | −14.18 | 317 | 63 | 60 | 194 | 319 |
| 400 | 190.29 | −3.92 | 241 | 55 | 52 | 134 | 243 |
| 600 | 189.67 | −4.54 | 204 | 50 | 47 | 107 | 206 |
| 1000 | 191.26 | −2.96 | 159 | 37 | 35 | 87 | 161 |
| 2000 | 195.77 | +1.56 | 133 | 39 | 31 | 63 | 135 |

**Reading it.** Every runway that actually un-silences a symbol destroys
value, and the tighter the trigger the worse it is: a 50bp runway turns 74
closes into 406 for **−28.67 USDT**. The mechanism is not subtle — the win
rate is essentially unchanged across the sweep (45.9% off → 45.7% at 200 →
48.9% at 400 → 50.3% at 2000) while trade count multiplies up to 6×. The rule
buys **volume at the same edge**, and at 2bp a side plus 15bp entry cross
extra volume is negative by construction.

`bps 2000` is the only arm nominally ahead (+1.56), and it is not a finding:
BTC and ETH are **byte-identical to off** (47.93 / 51.77 — the rule never
fires for them), SOL gains +2.03 and LINK loses −0.47. That is one symbol's
luck over 32 days, not an edge.

**LINK, the symbol the rule exists to rescue:**

| arm | LINK capital | LINK closes |
| --- | --- | --- |
| off | 47.27 | 29 |
| 200 | 39.03 | 128 |
| 400 | 39.43 | 108 |
| 1000 | 43.56 | 70 |
| 2000 | 46.80 | 63 |

The previously silent symbol does come back to life — at 200bp it trades 4.4×
as often and ends **8.24 USDT worse**. Un-silencing is not free.

**Decision: do not enable.** The silence that Gate 4 finding (e) flagged is
not a defect to patch; it is the cost centre of a mean-reversion ladder — a
side only earns when price returns to it — and re-centring the ladder converts
it into a trend-chaser paying the same stop distance for a worse entry. The
flag stays in the engine **off by default** purely as the instrument, so the
owner can re-run the sweep on new data without rebuilding it; no live path
passes a non-zero value, and the default arm is covered by the existing suite.

## Live path — FOUR gates, and why trial-live could never start (2026-10-20)

Asked "when we live, is it set?", I audited the trial-live sleeve instead of
trusting its header. It could not start, for four independent reasons — only
one of which the header knew about:

| Gate | Before | Now |
| --- | --- | --- |
| 1. Venue (`validateLiveSandboxMode`) | `--live` + non-sandbox ⇒ rejected, no override existed | `--allow-mainnet-live` **and** `NEURATRADE_MAINNET_CONFIRM=I-CONFIRM-MAINNET`; either missing fails closed |
| 2. Watchlist (`validateLiveGridWatchlist`) | every mainnet grid watchlist rejected | same two-key gate; without both it still refuses |
| 3. Cohort candidate (`validateLiveGridConfiguration`) | registry is BTC/SOL/ETH — **LINK is not in it**, so a LINK-only trial could never run | trial repointed to the ETH candidate; message now names the remedy (unvalidated symbol vs drifted field, with both values) |
| 4. Symbol | `--symbol` defaults to `BTC/USDT` while the registry keys on `BTC/USDT:USDT` — even BTC failed the lookup | `candidateForSymbol` matches on the base pair; trial passes `--symbol ETH/USDT:USDT` |

**Why ETH, not LINK.** LINK was chosen for its low venue floor, not its edge;
the cohort gate makes that choice impossible. Of the validated set ETH is best
on the 32-day soak window (+3.5% vs BTC −1.4% / SOL −5.5%) and is orderable at
$100 with a 50% position (0.0103 ETH ≥ the 0.01 venue minimum). BTC cannot be:
0.001 BTC ≈ $82 exceeds a $50 rung.

**Config fixes in the same pass:** `--min-capital` 20 → 30 (30% of par, the rule
the soaks now use), `--max-drawdown-pct` 15 → **5** and `--max-daily-loss-pct`
5 → **2** (the candidate's caps; the gate only allows tightening), `--fee 0.02`
plus an explicit `--taker-exit-fee-pct 0.06` so the stop-fee basis is visible
rather than hidden in a default, and the geometry pinned to the candidate
(step 0.75 / grids 3 / pause 24 / target 3 / adx 20 / slip 1bp) instead of
following `champion.json`.

**Verified end-to-end with dummy credentials:** no token → exits 1 with
`mainnet live execution requires NEURATRADE_MAINNET_CONFIRM=...`; with the
token → all four gates pass and the startup line reads
`signalFeed=bybit-mainnet (https://api.bybit.com) | executionEnv=bybit-live`.
No order can be placed with dummy keys and no process was left running (0
stray, 0 rows in the temp ledger).

**Residual risk, deliberately not papered over:** the validated candidate was
fitted on the **grid** engine while the live path executes the **ladder**, and
`stopRatio` / `maxHoldBars` are ladder-only knobs the candidate never covered.
The gate cannot see that difference. That is the main reason the first live
sleeve should be a routing test with a small stake, not a profit proof.

**What this does not measure** (stated so it is not over-read): only the
"follow the market" family. A *widen-rather-than-recentre* policy (leave the
armed rungs and add one beyond them) is a different strategy that this
instrument does not express, and would need its own sweep. The window is one
32-day sample on four symbols with no funding, and no bootstrap interval was
computed — the sign at tight runways is monotone and large, which is what the
decision turns on.
