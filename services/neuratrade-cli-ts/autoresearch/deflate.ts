#!/usr/bin/env bun
/**
 * Score the autoresearch ledger for selection bias.
 *
 * Reads results/ledger.jsonl and reports, per candidate champion, the Deflated
 * Sharpe Ratio computed from that trial's own Sharpe plus the spread of every
 * completed trial's Sharpe. Answers one question: given how many configurations
 * were tried, does the kept one survive multiple-testing deflation?
 *
 *   bun run autoresearch/deflate.ts
 *   bun run autoresearch/deflate.ts --phase=confirm --min-windows=100
 *
 * Why this exists: `shouldKeep` compares raw scores. With ~1e5 trials the
 * expected best-of-N score under the null is positive even when nothing has an
 * edge, so a KEEP is not by itself evidence. DSR corrects for exactly that.
 *
 * Uses only the fields already persisted (score, sharpe, skewness, kurtosis,
 * windows); historical entries predating `sharpe` are reported as unusable
 * rather than silently coerced from `score`, which is a location statistic and
 * not a Sharpe ratio.
 */
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import * as S from "effect/Schema";
import {
  classifyDeflatedSharpe,
  deflatedSharpe,
  type SharpVerdict,
} from "../src/scalping/deflated-sharpe.ts";

/**
 * The ledger contract, decoded at the I/O boundary. `sharpe`/`windows` are
 * optional because entries written before the DSR fields existed do not have
 * them; those are counted and excluded rather than coerced from `score`.
 */
const LedgerEntry = S.Struct({
  decision: S.optional(S.String),
  phase: S.optional(S.String),
  result: S.optional(
    S.Struct({
      sharpe: S.optional(S.Number),
      windows: S.optional(S.Number),
    }),
  ),
});
type LedgerEntry = typeof LedgerEntry.Type;

const here = dirname(fileURLToPath(import.meta.url));

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}

const ledgerPath = arg("ledger", join(here, "results", "ledger.jsonl"));
const minWindows = Number(arg("min-windows", "60"));
const phase = arg("phase", "all");

interface Scored {
  sharpe: number;
  windows: number;
  decision: string;
}

const trialSharpes: number[] = [];
const keeps: Scored[] = [];
let total = 0;
let skippedNoSharpe = 0;
let skippedWindows = 0;

for (const line of readFileSync(ledgerPath, "utf8").split("\n")) {
  if (line.length === 0) continue;
  let parsed: unknown;
  try {
    parsed = JSON.parse(line);
  } catch {
    continue; // truncated final line after a hard kill
  }
  const decoded = S.decodeUnknownOption(LedgerEntry)(parsed);
  if (decoded._tag === "None") continue;
  const entry = decoded.value;
  total += 1;
  const result = entry.result;
  if (result === undefined) continue;
  if (phase !== "all" && entry.phase !== phase) continue;
  const sharpe = result.sharpe;
  const windows = result.windows;
  if (sharpe === undefined || !Number.isFinite(sharpe)) {
    skippedNoSharpe += 1;
    continue;
  }
  if (windows === undefined || windows < minWindows) {
    skippedWindows += 1;
    continue;
  }
  trialSharpes.push(sharpe);
  if (entry.decision === "KEEP") {
    keeps.push({
      sharpe,
      windows,
      decision: entry.decision,
    });
  }
}

if (trialSharpes.length === 0) {
  console.log(
    `No usable trials in ${ledgerPath}.\n` +
      `  entries scanned:      ${total}\n` +
      `  missing \`sharpe\`:      ${skippedNoSharpe}  (predate the DSR fields)\n` +
      `  below ${minWindows} windows:  ${skippedWindows}\n\n` +
      "Historical ledger entries only carry `score` (median log return), which is\n" +
      "a location statistic, not a Sharpe ratio — feeding it to DSR would be a\n" +
      "units error. Re-run the search (or `run-once.ts`) to emit `sharpe`.",
  );
  process.exit(3);
}

const best = keeps.reduce<Scored | undefined>(
  (acc, k) => (acc === undefined || k.sharpe > acc.sharpe ? k : acc),
  undefined,
);

console.log(`ledger: ${ledgerPath}`);
console.log(`entries scanned:   ${total}`);
console.log(`usable trials:     ${trialSharpes.length}`);
console.log(`KEEPs with Sharpe: ${keeps.length}`);
if (skippedNoSharpe > 0) {
  console.log(`  (${skippedNoSharpe} predate the sharpe field — not scored)`);
}

const verdicts: Record<SharpVerdict, number> = {
  robust: 0,
  marginal: 0,
  "likely-false-positive": 0,
};
for (const keep of keeps) {
  const dsr = deflatedSharpe({
    observedSharpe: keep.sharpe,
    trialSharpes,
    nObs: keep.windows,
  });
  verdicts[classifyDeflatedSharpe(dsr)] += 1;
}

console.log("\nEvery KEEP re-scored against the full trial population:");
console.log(`  robust                ${verdicts.robust}`);
console.log(`  marginal              ${verdicts.marginal}`);
console.log(`  likely-false-positive ${verdicts["likely-false-positive"]}`);

if (best !== undefined) {
  const dsr = deflatedSharpe({
    observedSharpe: best.sharpe,
    trialSharpes,
    nObs: best.windows,
  });
  console.log(
    `\nBest KEEP: sharpe=${best.sharpe.toFixed(4)} over ${best.windows} windows`,
  );
  console.log(`  DSR = ${dsr.toFixed(4)}  => ${classifyDeflatedSharpe(dsr)}`);
  if (classifyDeflatedSharpe(dsr) !== "robust") {
    console.log(
      "  Interpretation: the best kept config does not survive correction for\n" +
        "  the number of trials that produced it. Treat it as unvalidated.",
    );
  }
  process.exit(classifyDeflatedSharpe(dsr) === "robust" ? 0 : 1);
}
process.exit(0);
