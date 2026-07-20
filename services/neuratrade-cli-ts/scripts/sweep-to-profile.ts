#!/usr/bin/env bun
/**
 * Convert a scalp-readiness-scan sweep JSON into strategy profiles.
 *
 * Usage:
 *   bun run scripts/sweep-to-profile.ts --sweep /tmp/sweep-btc-5m.json \
 *     [--rank 0] [--name scalp-sweep-btc-5m]
 *
 * Writes ~/.neuratrade/profiles/<name>.json ready for
 *   bun run index.ts scalp readiness --profile <name> --exchange bitget-futures --symbol <symbol> --timeframe <tf>
 */
import { join } from "node:path";

function arg(name: string, dflt: string): string {
  const i = process.argv.indexOf(name);
  return i >= 0 && i + 1 < process.argv.length ? process.argv[i + 1] : dflt;
}

const sweepPath = arg("--sweep", "");
if (!sweepPath) {
  console.error("--sweep is required");
  process.exit(1);
}
const rank = Number(arg("--rank", "0"));

interface SweepRow {
  regime: string;
  stopMult: number;
  tpMult: number;
  conf: number;
  maxBars: number;
}
interface SweepFile {
  meta: {
    exchange: string;
    symbol: string;
    timeframe: string;
    fee: number;
    makerFee: number | null;
    entryOrderType?: "market" | "limit";
    limitOffsetBps?: number;
    slippageBps: number;
    leverage: number;
  };
  top: SweepRow[];
}

const sweep = (await Bun.file(sweepPath).json()) as SweepFile;
const pick = sweep.top[rank];
if (!pick) {
  console.error(`No row at rank ${rank} (top has ${sweep.top.length} entries)`);
  process.exit(1);
}

const { exchange, symbol, timeframe, fee, leverage } = sweep.meta;
const name = arg(
  "--name",
  `scalp-sweep-${symbol.split("/")[0].toLowerCase()}-${timeframe}`,
);

const profile = {
  name,
  defaults: {
    exchange,
    defaultSymbol: symbol,
    timeframe,
    feePct: fee,
    makerFeePct: sweep.meta.makerFee ?? 0,
    entryOrderType: sweep.meta.entryOrderType ?? "market",
    entryLimitOffsetBps: sweep.meta.limitOffsetBps ?? 0,
    leverage,
    positionSizePct: 100,
    riskPerTradePct: 0,
    maxPositionSizePct: 100,
    useAtrStops: true,
    atrRiskReward: 0,
    holdUntilStop: false,
  },
  symbols: {
    [symbol]: {
      regimeMode: pick.regime,
      atrStopMultiplier: pick.stopMult,
      atrTakeProfitMultiplier: pick.tpMult,
      minConfidence: pick.conf,
      maxBarsInTrade: pick.maxBars,
      stopLossPct: 0,
      takeProfitPct: 0,
    },
  },
};

const home =
  process.env.NEURATRADE_HOME ?? join(process.env.HOME!, ".neuratrade");
const out = join(home, "profiles", `${name}.json`);
await Bun.write(out, JSON.stringify(profile, null, 2));
console.log(`Wrote ${out}`);
console.log(
  `Validate: bun run index.ts scalp readiness --exchange ${exchange} --symbol '${symbol}' --timeframe ${timeframe} --futures --fee ${fee} --slippage-bps ${sweep.meta.slippageBps} --leverage ${leverage} --profile ${name}`,
);
