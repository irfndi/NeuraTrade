#!/usr/bin/env bun
/**
 * Overnight mutate → screen → (confirm on KEEP) → keep/discard.
 * Panel loaded once. Parallel-safe champion updates via lockfile.
 * Never touches live kill-switch or position rows.
 */
import { mkdirSync, appendFileSync, writeFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { knobs as seedKnobs, type AutoresearchKnobs } from "./knobs.ts";
import {
  loadAlignedPanel,
  evaluateKnobsOnPanel,
  type EvaluateResult,
} from "./prepare.ts";
import { mutateKnobs, shouldKeep, renderKnobsModule } from "./mutate.ts";
import { withFileLock, readJsonFile, writeJsonFile } from "./lock.ts";

const here = dirname(fileURLToPath(import.meta.url));
const resultsDir = join(here, "results");
const knobsPath = join(here, "knobs.ts");
const championPath = join(resultsDir, "champion.json");
const championLock = join(resultsDir, "champion.lock");
const ledgerPath = join(resultsDir, "ledger.jsonl");
const goalsPath = join(resultsDir, "goals.md");

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}

const trials = Number(arg("trials", "500"));
const worker = Number(arg("worker", "0"));
const workers = Number(arg("workers", "1"));
const panelSymbols = Number(arg("symbols", "8"));
const screenSteps = Number(arg("screen-steps", "12"));
const screenBudget = Number(arg("screen-budget-sec", "45"));
const confirmSteps = Number(arg("confirm-steps", "40"));
const confirmBudget = Number(arg("confirm-budget-sec", "180"));

/** Deterministic RNG per worker for diverse mutations. */
function mulberry32(seed: number): () => number {
  let t = seed >>> 0;
  return () => {
    t += 0x6d2b79f5;
    let r = Math.imul(t ^ (t >>> 15), 1 | t);
    r ^= r + Math.imul(r ^ (r >>> 7), 61 | r);
    return ((r ^ (r >>> 14)) >>> 0) / 4294967296;
  };
}

const rng = mulberry32(0x9e3779b9 ^ (worker + 1) * 0x85ebca6b);

mkdirSync(resultsDir, { recursive: true });

interface ChampionState {
  knobs: AutoresearchKnobs;
  score: number;
  guardsOk: boolean;
}

function loadChampionUnlocked(): ChampionState {
  const raw = readJsonFile<ChampionState>(championPath);
  if (raw?.knobs) {
    return {
      knobs: raw.knobs,
      score: Number.isFinite(raw.score) ? raw.score : Number.NEGATIVE_INFINITY,
      guardsOk: Boolean(raw.guardsOk),
    };
  }
  return {
    knobs: { ...seedKnobs },
    score: Number.NEGATIVE_INFINITY,
    guardsOk: false,
  };
}

function persistChampionUnlocked(state: ChampionState): void {
  writeJsonFile(championPath, state);
  // Only worker 0 writes knobs.ts to avoid thrash; others still update champion.json.
  if (worker === 0) {
    writeFileSync(knobsPath, renderKnobsModule(state.knobs));
  }
}

function appendLedger(row: unknown): void {
  appendFileSync(ledgerPath, `${JSON.stringify(row)}\n`);
}

function goalsClaimed(r: EvaluateResult): boolean {
  return (
    r.phase === "confirm" &&
    r.guardsOk &&
    r.medianLogReturn > 0 &&
    r.winRatePct >= 48 &&
    r.tradesPerSymMonth >= 4 &&
    r.medianDrawdownPct <= 15 &&
    r.expectancyPct > 0
  );
}

function writeGoals(status: string, r: EvaluateResult | null): void {
  const body = `# Autoresearch goals

Status: **${status}**
Updated: ${new Date().toISOString()}
Worker: ${worker}/${workers}

| Goal | Target | Current (confirm) |
| --- | --- | --- |
| Profitability (med log-ret) | > 0 | ${r ? r.medianLogReturn.toFixed(4) : "n/a"} |
| Win rate | ≥ 48% | ${r ? r.winRatePct.toFixed(1) : "n/a"} |
| Throughput (trades/sym-mo) | ≥ 4 | ${r ? r.tradesPerSymMonth.toFixed(1) : "n/a"} |
| Drawdown (med) | ≤ 15% | ${r ? r.medianDrawdownPct.toFixed(1) : "n/a"} |
| Expectancy | > 0 | ${r ? r.expectancyPct.toFixed(3) : "n/a"} |

Live trading remains frozen until credential rotation + position reconciliation.
`;
  writeFileSync(goalsPath, body);
}

console.log(
  `autoresearch w${worker}/${workers}: trials=${trials} panelSymbols=${panelSymbols} screen=${screenSteps}@${screenBudget}s confirm=${confirmSteps}@${confirmBudget}s`,
);

console.log("loading candle panel once...");
const panel = loadAlignedPanel({ symbols: panelSymbols });
console.log(
  `panel ready: ${panel.symbols.length} symbols, refLen=${panel.refLen}, loadedMs=${panel.loadedMs}`,
);

const shard =
  workers > 1
    ? { symbolOffset: worker, symbolStride: workers }
    : { symbolOffset: 0, symbolStride: 1 };

function evalScreen(k: AutoresearchKnobs): EvaluateResult {
  return evaluateKnobsOnPanel(k, panel, {
    phase: "screen",
    maxSteps: screenSteps,
    budgetSec: screenBudget,
    ...shard,
  });
}

function evalConfirm(k: AutoresearchKnobs): EvaluateResult {
  return evaluateKnobsOnPanel(k, panel, {
    phase: "confirm",
    maxSteps: confirmSteps,
    budgetSec: confirmBudget,
    // Confirm always uses full panel (no shard) for claim-quality scores.
    symbolOffset: 0,
    symbolStride: 1,
  });
}

// Seed champion under lock if missing / -Infinity.
withFileLock(championLock, () => {
  let champ = loadChampionUnlocked();
  if (
    !Number.isFinite(champ.score) ||
    champ.score === Number.NEGATIVE_INFINITY
  ) {
    console.log("evaluating seed champion (screen → confirm)...");
    const screen = evalScreen(champ.knobs);
    const base = evalConfirm(champ.knobs);
    champ = {
      knobs: champ.knobs,
      score: base.score,
      guardsOk: base.guardsOk,
    };
    persistChampionUnlocked(champ);
    appendLedger({
      ts: new Date().toISOString(),
      worker,
      trial: 0,
      decision: "SEED",
      axis: null,
      knobs: champ.knobs,
      screen,
      result: base,
    });
    writeGoals(goalsClaimed(base) ? "CLAIMED" : "IN_PROGRESS", base);
    console.log(
      `SEED confirm score=${base.score.toFixed(4)} guardsOk=${base.guardsOk} reason=${base.reason} elapsedMs=${base.elapsedMs}`,
    );
    if (goalsClaimed(base)) {
      console.log("GOALS CLAIMED on seed — stopping.");
      process.exit(0);
    }
  }
});

for (let i = 1; i <= trials; i++) {
  const localChamp = loadChampionUnlocked();
  const { next, axis } = mutateKnobs(localChamp.knobs, rng);
  console.log(`\n[w${worker} trial ${i}/${trials}] mutate ${axis} → screen...`);
  const screen = evalScreen(next);
  console.log(
    `  screen score=${screen.score.toFixed(4)} guardsOk=${screen.guardsOk} elapsedMs=${screen.elapsedMs}`,
  );

  // Cheap reject on screen score only (confirm is the claim gate).
  const screenPromising =
    Number.isFinite(screen.score) &&
    (localChamp.score === Number.NEGATIVE_INFINITY ||
      screen.score > localChamp.score);

  if (!screenPromising) {
    appendLedger({
      ts: new Date().toISOString(),
      worker,
      trial: i,
      decision: "DISCARD_SCREEN",
      axis,
      knobs: next,
      screen,
      championScore: localChamp.score,
    });
    console.log(
      `  DISCARD_SCREEN (screen ${screen.score.toFixed(4)} <= champ ${localChamp.score.toFixed(4)})`,
    );
    continue;
  }

  console.log("  screen promising → confirm...");
  const result = evalConfirm(next);

  const decision = withFileLock(championLock, () => {
    const champ = loadChampionUnlocked();
    const keep = shouldKeep({
      candidateScore: result.score,
      candidateGuardsOk: result.guardsOk,
      championScore: champ.score,
      championGuardsOk: champ.guardsOk,
    });
    appendLedger({
      ts: new Date().toISOString(),
      worker,
      trial: i,
      decision: keep ? "KEEP" : "DISCARD_CONFIRM",
      axis,
      knobs: next,
      screen,
      result,
      championScore: champ.score,
    });
    if (keep) {
      const nextState: ChampionState = {
        knobs: next,
        score: result.score,
        guardsOk: result.guardsOk,
      };
      persistChampionUnlocked(nextState);
      writeGoals(goalsClaimed(result) ? "CLAIMED" : "IN_PROGRESS", result);
      return "KEEP" as const;
    }
    writeGoals("IN_PROGRESS", result);
    return "DISCARD_CONFIRM" as const;
  });

  console.log(
    `  ${decision} confirm score=${result.score.toFixed(4)} guardsOk=${result.guardsOk} reason=${result.reason} elapsedMs=${result.elapsedMs}`,
  );

  if (decision === "KEEP" && goalsClaimed(result)) {
    console.log("\nGOALS CLAIMED — stopping loop.");
    process.exit(0);
  }
}

console.log("\nloop finished without claim — champion retained.");
process.exit(1);
