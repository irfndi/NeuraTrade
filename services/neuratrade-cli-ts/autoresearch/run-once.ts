#!/usr/bin/env bun
/**
 * Single-trial evaluate of current knobs.ts (confirm phase, cached panel).
 */
import { knobs } from "./knobs.ts";
import { loadAlignedPanel, evaluateKnobsOnPanel } from "./prepare.ts";

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}

const symbols = Number(arg("symbols", "8"));
const panel = loadAlignedPanel({ symbols });
const result = evaluateKnobsOnPanel(knobs, panel, {
  phase: "confirm",
  maxSteps: Number(arg("steps", "40")),
  budgetSec: Number(arg("budget-sec", "180")),
});

console.log(JSON.stringify({ knobs, panelSymbols: panel.symbols.length, result }, null, 2));
process.exit(result.guardsOk && result.score > 0 ? 0 : 1);
