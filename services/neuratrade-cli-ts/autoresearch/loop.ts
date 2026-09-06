#!/usr/bin/env bun
/**
 * Overnight mutate → evaluate → keep/discard loop.
 * Never touches live kill-switch or position rows.
 */
import { mkdirSync, appendFileSync, writeFileSync, readFileSync } from "node:fs";
import { join, dirname } from "node:path";
import { fileURLToPath } from "node:url";
import { knobs as seedKnobs, type AutoresearchKnobs } from "./knobs.ts";
import { evaluateKnobs, type EvaluateResult } from "./prepare.ts";
import { mutateKnobs, shouldKeep, renderKnobsModule } from "./mutate.ts";

const here = dirname(fileURLToPath(import.meta.url));
const resultsDir = join(here, "results");
const knobsPath = join(here, "knobs.ts");
const championPath = join(resultsDir, "champion.json");
const ledgerPath = join(resultsDir, "ledger.jsonl");
const goalsPath = join(resultsDir, "goals.md");

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}

const trials = Number(arg("trials", "100"));
const budgetSec = Number(arg("budget-sec", "180"));
const symbols = Number(arg("symbols", "8"));
const steps = Number(arg("steps", "40"));

mkdirSync(resultsDir, { recursive: true });

function loadChampion(): {
  knobs: AutoresearchKnobs;
  score: number;
  guardsOk: boolean;
} {
  try {
    const raw = JSON.parse(readFileSync(championPath, "utf8")) as {
      knobs: AutoresearchKnobs;
      score: number;
      guardsOk?: boolean;
    };
    return {
      knobs: raw.knobs,
      score: raw.score,
      guardsOk: Boolean(raw.guardsOk),
    };
  } catch {
    return {
      knobs: { ...seedKnobs },
      score: Number.NEGATIVE_INFINITY,
      guardsOk: false,
    };
  }
}

function persistChampion(
  k: AutoresearchKnobs,
  score: number,
  guardsOk: boolean,
): void {
  writeFileSync(
    championPath,
    JSON.stringify({ knobs: k, score, guardsOk }, null, 2),
  );
  writeFileSync(knobsPath, renderKnobsModule(k));
}

function appendLedger(row: unknown): void {
  appendFileSync(ledgerPath, `${JSON.stringify(row)}\n`);
}

function goalsClaimed(r: EvaluateResult): boolean {
  return (
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

| Goal | Target | Current |
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

let champion = loadChampion();
console.log(
  `autoresearch loop: trials=${trials} budgetSec=${budgetSec} championScore=${champion.score}`,
);

// Baseline evaluate if champion has no score yet.
if (!Number.isFinite(champion.score) || champion.score === Number.NEGATIVE_INFINITY) {
  console.log("evaluating seed champion...");
  const base = evaluateKnobs(champion.knobs, {
    budgetSec,
    symbols,
    maxSteps: steps,
  });
  champion = {
    knobs: champion.knobs,
    score: base.score,
    guardsOk: base.guardsOk,
  };
  persistChampion(champion.knobs, champion.score, champion.guardsOk);
  appendLedger({
    ts: new Date().toISOString(),
    trial: 0,
    decision: "SEED",
    axis: null,
    knobs: champion.knobs,
    result: base,
  });
  writeGoals(goalsClaimed(base) ? "CLAIMED" : "IN_PROGRESS", base);
  console.log(
    `SEED score=${base.score.toFixed(4)} guardsOk=${base.guardsOk} reason=${base.reason}`,
  );
  if (goalsClaimed(base)) {
    console.log("GOALS CLAIMED on seed — stopping.");
    process.exit(0);
  }
}

for (let i = 1; i <= trials; i++) {
  const { next, axis } = mutateKnobs(champion.knobs);
  console.log(`\n[trial ${i}/${trials}] mutate ${axis} → evaluate...`);
  const result = evaluateKnobs(next, { budgetSec, symbols, maxSteps: steps });
  const keep = shouldKeep({
    candidateScore: result.score,
    candidateGuardsOk: result.guardsOk,
    championScore: champion.score,
    championGuardsOk: champion.guardsOk,
  });
  const decision = keep ? "KEEP" : "DISCARD";
  appendLedger({
    ts: new Date().toISOString(),
    trial: i,
    decision,
    axis,
    knobs: next,
    result,
    championScore: champion.score,
  });
  console.log(
    `${decision} score=${result.score.toFixed(4)} (champ=${champion.score.toFixed(4)}) guardsOk=${result.guardsOk} reason=${result.reason} elapsedMs=${result.elapsedMs}`,
  );
  if (keep) {
    champion = {
      knobs: next,
      score: result.score,
      guardsOk: result.guardsOk,
    };
    persistChampion(next, result.score, result.guardsOk);
    writeGoals(goalsClaimed(result) ? "CLAIMED" : "IN_PROGRESS", result);
    if (goalsClaimed(result)) {
      console.log("\nGOALS CLAIMED — stopping loop.");
      process.exit(0);
    }
  } else {
    writeGoals("IN_PROGRESS", result);
  }
}

console.log("\nloop finished without claim — champion retained.");
process.exit(1);
