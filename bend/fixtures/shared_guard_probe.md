# True-parity probe spec: SAME GuardInput → TS `checkKeepGuards` + Bend `check_keep_guards` (NOT provenance-only)

Fixture: `bend/fixtures/shared_guard_panel.md` (panelHash `295c6675`,
`refLen` 6500, 3 symbols — meets EVERY `PHASE_GEOM` minimum, so TS
reaches the backtest + `checkGuards` instead of a geometry `emptyResult`).
Sources of truth (read-only, never edited by this probe):
`services/neuratrade-cli-ts/autoresearch/prepare.ts`
(`PHASE_GEOM`, `HOLDOUT_BARS`, `computePanelHash`, `checkGuards`),
`services/neuratrade-cli-ts/autoresearch/goals.ts`
(`KEEP_GUARDS` v2, `checkKeepGuards`),
`services/neuratrade-cli-ts/autoresearch/mutate.test.ts`
("autoresearch guards" ground-truth vectors), `bend/guards.bend`
(`check_keep_guards`, 5-bit mask).

Supersedes the follow-up note in `bend/fixtures/parity_probe.md` §2
(provenance-only scope): that probe compared NO shared GuardInput; THIS
probe feeds the SAME three vectors to both sides and requires bit-for-bit
agreement.

## 1. TS invocation (`checkKeepGuards`, goals.ts:47-69)

Run from `services/neuratrade-cli-ts` (bun). Panel from the fixture file
first (geometry passage), then the shared vectors (guard parity):

```ts
import { evaluateKnobsOnPanel, computePanelHash, checkGuards } from "./autoresearch/prepare.ts";
import { checkKeepGuards } from "./autoresearch/goals.ts";
import { knobs } from "./autoresearch/knobs.ts";
// panel: AlignedPanel literal from bend/fixtures/shared_guard_panel.md §5

const hash = computePanelHash({
  exchange: panel.exchange, timeframe: panel.timeframe,
  panelTimeframe: panel.panelTimeframe, symbols: [...panel.symbols],
  refLen: panel.refLen, t0Ms: 1767225600000, t1Ms: 1773074700000,
});
// REQUIRE hash === "295c6675" && hash === panel.panelHash

for (const phase of ["screen", "confirm", "holdout"] as const) {
  const r = evaluateKnobsOnPanel(knobs, panel as any, { phase });
  // REQUIRE r.reason !== "insufficient_symbols"
  //   && r.reason !== "insufficient_bars"
  //   && r.reason !== "insufficient_windows"
  //   && r.reason !== "insufficient_holdout_windows"
  console.log(JSON.stringify({ phase, reason: r.reason, symbols: r.symbols }));
}

const VECTORS = [
  { id: "V1", medianLogReturn: -0.01, winRatePct: 55, medianDrawdownPct: 8, tradesPerSymMonth: 40, expectancyPct: -0.1 },
  { id: "V2", medianLogReturn: 0.01, winRatePct: 52, medianDrawdownPct: 10, tradesPerSymMonth: 5, expectancyPct: 0.2 },
  { id: "V3", medianLogReturn: 0.001, winRatePct: 52, medianDrawdownPct: 12, tradesPerSymMonth: 4, expectancyPct: 0.001 },
];
for (const v of VECTORS) {
  const { id: _, ...input } = v;
  const g = checkKeepGuards(input);       // goals.ts ground truth
  const c = checkGuards(input);           // prepare.ts alias — REQUIRE deep-equal to g
  console.log(JSON.stringify({ id: v.id, ok: g.ok, reason: g.reason }));
}
```

Expected TS output (guard lines):

```json
{"id":"V1","ok":false,"reason":"log_return_nonpositive,expectancy_nonpositive"}
{"id":"V2","ok":true,"reason":"ok"}
{"id":"V3","ok":true,"reason":"ok"}
```

(`checkGuards` is a pure alias of `checkKeepGuards`, prepare.ts:847-850 —
any divergence between the two lines is an import/wiring bug, not a
threshold debate.)

TS reason → mask decode table (bit order = `checkKeepGuards` push order,
goals.ts:50-64 = guards.bend:24-28):

| reason token | bit |
|---|---|
| `log_return_nonpositive` | 0 (value 1) |
| `winrate_below_52` | 1 (value 2) |
| `drawdown_above_12` | 2 (value 4) |
| `throughput_below_4` | 3 (value 8) |
| `expectancy_nonpositive` | 4 (value 16) |

Expected decoded masks: V1 = 17, V2 = 0, V3 = 0.

## 2. Bend invocations (COMPILED only — F32 never reduces interpreted)

`bend/guards.bend` header gotcha: F32 arithmetic/comparisons print
unevaluated under `bend file.bend`; they DO work compiled. NEVER verify
via the interpreter.

```sh
bend bend/guards.bend -o /tmp/p4_guards && /tmp/p4_guards  # expect 2065 (pre-existing ground truth, unchanged)
```

`2065` = masks 17/0/2 packed (`m1 + m2*32 + m3*1024`) on the exact
`mutate.test.ts` fixtures — proves the committed `main()` still matches
TS. This probe ADDS the shared-vector check below; it does NOT edit
`bend/*.bend` (contract: new files only).
Shared-vector Bend check — Bend imports only `Base` (no cross-file
imports), so the probe copies `bend/guards.bend` to /tmp and swaps its
`main()` for one vector literal. NEVER edit the committed file. Example
for V3 (borderline); repeat for V1/V2 with their literals:

```sh
cp bend/guards.bend /tmp/gprobe_v3.bend
# replace def main() -> U32: <through the packed U32.add line> with:
```

```bend
def main() -> U32:
  v = {GuardInput{{0.001 : F32}, {52.0 : F32}, {12.0 : F32}, {4.0 : F32}, {0.001 : F32}} : GuardInput}
  guard_mask(check_keep_guards(v))
```

```sh
bend /tmp/gprobe_v3.bend -o /tmp/gprobe_v3 && /tmp/gprobe_v3  # expect 0
```

Expected compiled outputs: V1 → `17`, V2 → `0`, V3 → `0`.
(`ok` = `mask == 0`; read `ok` via `GuardResult.ok` or derive it —
either way it MUST equal the TS `ok` bool.)

V1/V2 Bend literals (exact F32 of the TS vectors):

```text
V1: mlr F32.neg({0.01 : F32}), wr {55.0 : F32}, dd {8.0 : F32}, tpm {40.0 : F32}, ep F32.neg({0.1 : F32})
V2: mlr {0.01 : F32}, wr {52.0 : F32}, dd {10.0 : F32}, tpm {5.0 : F32}, ep {0.2 : F32}
```

(V1/V2 already exist verbatim as `case1`/`case2` in `guards.bend`
`main()`; V3 is new-but-probe-local — it lives in the /tmp probe
invocation, never in a committed `.bend` file.)

## 3. Agreement rule (bit-for-bit, TS is ground truth)

PASS requires ALL of the following on the SAME three inputs (V1, V2, V3):

1. `ok` bool equality: TS `checkKeepGuards(v).ok === Bend check_keep_guards(v).ok` on every vector.
2. 5-bit mask equality: TS-decoded mask (§1 table) `===` Bend
   `guard_mask` (U32 0..31) on every vector: `17, 0, 0`.
3. Geometry passage: real-size panel evals on screen/confirm/holdout
   return NO `insufficient_*` reason (§1 REQUIREs) — proves the panel
   meets `PHASE_GEOM` minimums (`minSymbols` 3 all phases, `minCandles`
   2500/6000/6000) and the guard comparison ran on a live path.
4. Provenance pin: `computePanelHash(...) === "295c6675"`.

Any single mismatch = FAIL. Failure taxonomy:

- `ok`/mask flip on V1/V2 → `goals.ts`↔`guards.bend` port bug (fix Bend
  to match TS; TS is ground truth; NEVER retune thresholds to pass).
- flip on V3 only → F32-vs-f64 edge rounding on a strict-`>` comparison
  (`mlr > 0`, `ep > 0`); fix the Bend literal/port, never widen the TS
  guard.
- `checkGuards` vs `checkKeepGuards` divergence → wiring bug
  (prepare.ts:847-850 is a pure alias).
- `insufficient_*` reason → fixture drift (`PHASE_GEOM` minimums moved
  or panel shrunk); re-pin explicitly, do NOT reinterpret as a guard
  verdict.
- hash mismatch → provenance drift (fix inputs, not thresholds).

Tolerances: NONE on guards — threshold comparisons are exact booleans,
no epsilon. `elapsedMs`/`score`/`windows` timing asserted on nothing
(wall-clock / backtest noise excluded); only `reason`-prefix (§3.3),
`panelHash`, `ok`, and mask are compared.

## 4. Local verify (no box, no soak, no champion writes)

```sh
ls -la bend/fixtures/shared_guard_*  # exactly shared_guard_panel.md + shared_guard_probe.md
git -C . status --porcelain -- bend/  # only `?? bend/fixtures/shared_guard_*.md`; no `M bend/*.bend`
git -C . diff --stat -- bend/         # empty (no tracked-file edits)
bend bend/guards.bend -o /tmp/p4_guards && /tmp/p4_guards  # 2065 (regression pin, unchanged)
```

TS side runs wherever `bun` is available (dev box or CI); this probe
never touches the champion JSONs, the soak loop, PM2/ssh, engine state
machines, or `knobs.ts`.
