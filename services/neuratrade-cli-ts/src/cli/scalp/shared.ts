// Shared plumbing for the grouped scalp command modules. Lives outside
// scalp.ts so sibling modules (flow, …) can import it without a cycle.
import { Layer } from "effect";
import { BunServices } from "@effect/platform-bun";
import { PathLive } from "../../services/path.js";
import { ConfigLive } from "../../services/config.js";
import { SqliteClientLiveRaw } from "../../services/sqlite.js";
import {
  BacktestEngineLive,
  ExitEngineLive,
  SignalComposerLive,
  StrategyLibraryLive,
} from "../../scalping/services.js";
import type { FuturesMarginMode } from "../../exchange/futures-adapter.js";
import type { BitgetProductType } from "../../services/bitget-client.js";

export function makeLayer(home?: string) {
  return Layer.mergeAll(
    BunServices.layer,
    PathLive(home),
    BacktestEngineLive,
    SignalComposerLive,
    ExitEngineLive,
    StrategyLibraryLive,
  );
}

/**
 * Layer for commands that read the market-data SQLite database. On top of
 * `makeLayer` it provides the runtime config and the scoped `SqliteClient`
 * (opens the DB via Effect, closes it when the command's scope ends). Uses
 * the raw open mode — repositories own their schema via ensureTables.
 */
export function makeDbLayer(home?: string) {
  const base = makeLayer(home);
  const config = Layer.provide(ConfigLive(home), base);
  return Layer.mergeAll(
    base,
    config,
    Layer.provide(SqliteClientLiveRaw, Layer.merge(base, config)),
  );
}

export function parseMarginMode(value: string): FuturesMarginMode {
  if (value === "isolated" || value === "crossed") {
    return value;
  }
  throw new Error(
    `invalid margin-mode: ${value} (expected "crossed" or "isolated")`,
  );
}

export function parseProductType(value: string): BitgetProductType {
  if (
    value === "USDT-FUTURES" ||
    value === "COIN-FUTURES" ||
    value === "USDC-FUTURES"
  ) {
    return value;
  }
  throw new Error(
    `invalid product-type: ${value} (expected USDT-FUTURES, COIN-FUTURES, or USDC-FUTURES)`,
  );
}

export type MutablePartialRiskLimits = {
  -readonly [K in keyof import("../../risk/guards.js").RiskLimits]?: import("../../risk/guards.js").RiskLimits[K];
};
