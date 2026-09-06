#!/usr/bin/env bun
/**
 * Single-trial evaluate of current knobs.ts
 */
import { knobs } from "./knobs.ts";
import { evaluateKnobs } from "./prepare.ts";

function arg(name: string, fallback: string): string {
  const hit = process.argv.find((a) => a.startsWith(`--${name}=`));
  return hit?.split("=")[1] ?? fallback;
}

const result = evaluateKnobs(knobs, {
  symbols: Number(arg("symbols", "8")),
  maxSteps: Number(arg("steps", "40")),
  budgetSec: Number(arg("budget-sec", "180")),
});

console.log(JSON.stringify({ knobs, result }, null, 2));
process.exit(result.guardsOk && result.score > 0 ? 0 : 1);
