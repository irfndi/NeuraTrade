# Parity probe spec: PROVENANCE-ONLY TS eval + shared-guard-input Bend checks (NOT end-to-end parity)

Fixture: `bend/fixtures/pinned_6bar_panel.md` (panelHash `d3023228`).
Sources of truth (read-only, never edited by this probe):
`services/neuratrade-cli-ts/autoresearch/prepare.ts`
(`evaluateKnobsOnPanel`, `computePanelHash`, `PHASE_GEOM`, `HOLDOUT_BARS`),
`services/neuratrade-cli-ts/autoresearch/goals.ts`
(`KEEP_GUARDS` v2, `checkKeepGuards`), `bend/guards.bend`, `bend/search.bend`.

## 1. TS eval invocation

Run from `services/neuratrade-cli-ts` (bun), panel built per the fixture file:

```ts
import { evaluateKnobsOnPanel, computePanelHash } from "./autoresearch/prepare.ts";
import { knobs } from "./autoresearch/knobs.ts";
// panel: AlignedPanel literal from bend/fixtures/pinned_6bar_panel.md

const hash = computePanelHash({
  exchange: panel.exchange,
  timeframe: panel.timeframe,
  panelTimeframe: panel.panelTimeframe,
  symbols: [...panel.symbols],
  refLen: panel.refLen,
  t0Ms: 1767225600000,
  t1Ms: 1767230100000,
});
// REQUIRE hash === "d3023228" && hash === panel.panelHash

for (const phase of ["screen", "confirm", "holdout"] as const) {
  const r = evaluateKnobsOnPanel(knobs, panel as any, { phase });
  console.log(JSON.stringify({
    phase, score: r.score, guardsOk: r.guardsOk,
    reason: r.reason, windows: r.windows, symbols: r.symbols,
  }));
}
```

Expected output (all three phases — knob-independent):

```json
{"phase":"screen","score":null,"guardsOk":false,"reason":"insufficient_symbols","windows":0,"symbols":1}
{"phase":"confirm","score":null,"guardsOk":false,"reason":"insufficient_symbols","windows":0,"symbols":1}
{"phase":"holdout","score":null,"guardsOk":false,"reason":"insufficient_symbols","windows":0,"symbols":1}
```

(`score: null` is `JSON.stringify(-Infinity)`; assert
`r.score === Number.NEGATIVE_INFINITY` in code, not via the JSON text.
`elapsedMs` is asserted on nothing — wall-clock, excluded.)

## 2. Bend gate invocations (COMPILED only — F32 never reduces interpreted)

```sh
bend bend/guards.bend -o /tmp/p4_guards && /tmp/p4_guards  # expect 2065
bend bend/search.bend -o /tmp/p4_search && /tmp/p4_search  # expect 11
```

- `2065` = guard masks 17/0/2 packed (`m1 + m2*32 + m3*1024`), the exact
  `mutate.test.ts` "autoresearch guards" fixtures → proves Bend
  `check_keep_guards` matches TS `checkKeepGuards` bit-for-bit on
  `KEEP_GUARDS` v2 (`mlr>0`, `wr>=52`, `dd<=12`, `tpm>=4`, `ep>0`).
- `11` = `SCREEN_SLACK` 0.0025 survival bitmask (`b0+b1+b3`, exact-floor
  `0.0475` dies) → proves Bend `survives` matches `loop.ts`.
- Provenance-only scope: the 1-symbol panel forces TS `emptyResult`
  (`insufficient_symbols`, `prepare.ts:691-692`) BEFORE the backtest, so the
  TS side never produces a GuardInput — while Bend `2065`/`11` check
  `check_keep_guards`/`survives` on the pre-existing `mutate.test.ts`
  fixtures. No shared GuardInput is compared, so this probe pins
  provenance + plumbing only. Follow-up: real-size pinned panel (≥3
  symbols, ≥minCandles) feeding the SAME GuardInput to TS
  `checkKeepGuards` + Bend `check_keep_guards` for true parity.

## 3. Agreement tolerance

| check | tolerance | rationale |
|---|---|---|
| `panelHash` | exact string equality `"d3023228"` | FNV-1a over fixed preimage is deterministic; any mismatch = provenance drift, scores incomparable |
| `score` (fixture) | `r.score === Number.NEGATIVE_INFINITY` on all 3 phases, reruns identical | degenerate panel: no windows exist, so determinism is exact, not statistical |
| `score` (real-size panels, forward path) | `\|s1 - s2\| <= 1e-12` across reruns, same panel+knobs | guards float noise only; `elapsedMs`/`windows` timing excluded |
| `guardsOk` | exact boolean match TS `guardsOk` == Bend `ok` bit on the same `GuardInput` | KEEP_GUARDS are threshold comparisons; no epsilon — a flipped bit is a port bug, proven today by ground truths `2065` / `11` |
| `reason` (fixture) | exact `"insufficient_symbols"` all phases | 1 symbol < `minSymbols` 3 on every `PHASE_GEOM` |

## 4. Local verify (no box, no soak, no champion writes)

```sh
git -C . status --porcelain -- bend/   # only `?? bend/fixtures/` entries; no `M bend/*.bend`
bend bend/guards.bend -o /tmp/p4_guards && /tmp/p4_guards  # 2065
bend bend/search.bend -o /tmp/p4_search && /tmp/p4_search  # 11
```

TS side runs wherever `bun` is available (dev box or CI); this probe never
touches the champion JSON, the soak loop, PM2/ssh, or engine code.
Failure taxonomy: hash mismatch → fixture/provenance drift (fix inputs, not
thresholds); `guardsOk` flip → `goals.ts`↔`guards.bend` port bug (fix Bend
to match TS, TS is ground truth); `reason` change → `PHASE_GEOM` minimums
moved (re-pin fixture expectations explicitly).
