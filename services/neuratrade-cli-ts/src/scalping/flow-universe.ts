/**
 * Flow Ignition universe selector.
 *
 * Pure function: rank the liquid USDT-perp set by 24h quote turnover, then
 * keep the contracts whose top-of-book spread is tight enough and whose
 * listing is old enough to have backtest history. A small set of majors is
 * always carried, even when they fall below the turnover cutoff.
 */

/**
 * Instrument metadata used to compute spread and contract age. Structurally
 * compatible with `InstrumentInfo` from the Bybit gateway.
 */
export interface FlowInstrument {
  /** Bybit wire symbol, e.g. "BTCUSDT" (matches the `volumes` key). */
  readonly symbol: string;
  /** "Trading" when the contract is online. */
  readonly status?: string;
  /** Listing time in ms epoch (instruments-info `listedTime`). */
  readonly listedTime?: number;
  /** Current best bid (undefined when the ticker carries no quote). */
  readonly bid1Price?: number;
  /** Current best ask (undefined when the ticker carries no quote). */
  readonly ask1Price?: number;
}

export interface FlowUniverseOptions {
  /** Max top-of-book spread in bps (default 6). */
  readonly maxSpreadBps?: number;
  /** Min contract age in days (default 30). */
  readonly minAgeDays?: number;
  /** Max universe size (default 100). */
  readonly topN?: number;
  /**
   * Base coins always carried even below the cutoff (default
   * BTC/ETH/SOL/XRP/DOGE). Matched as `<BASE>USDT` against wire symbols.
   */
  readonly alwaysInclude?: readonly string[];
  /**
   * Spread bps assumed when a symbol has no bid/ask quote. Defaults to
   * `maxSpreadBps`, i.e. an unmeasurable spread passes the filter.
   */
  readonly defaultSpreadBps?: number;
}

export interface FlowUniverseEntry {
  readonly symbol: string;
  readonly turnover24h: number;
  readonly spreadBps: number;
  readonly ageDays: number;
  readonly rank: number;
  /** Last funding rate as a decimal (e.g. 0.0001), when `funding` was given. */
  readonly fundingRate?: number;
}

const DEFAULT_MAX_SPREAD_BPS = 6;
const DEFAULT_MIN_AGE_DAYS = 30;
export const DEFAULT_FLOW_UNIVERSE_TOP_N = 100;
const DEFAULT_ALWAYS_INCLUDE = ["BTC", "ETH", "SOL", "XRP", "DOGE"] as const;
const MS_PER_DAY = 86_400_000;

export function selectFlowUniverse(
  volumes: Readonly<Record<string, number>>,
  symbols: readonly FlowInstrument[],
  funding?: Readonly<Record<string, number>>,
  opts: FlowUniverseOptions = {},
): readonly FlowUniverseEntry[] {
  const maxSpreadBps = opts.maxSpreadBps ?? DEFAULT_MAX_SPREAD_BPS;
  const minAgeDays = opts.minAgeDays ?? DEFAULT_MIN_AGE_DAYS;
  const topN = opts.topN ?? DEFAULT_FLOW_UNIVERSE_TOP_N;
  const alwaysInclude = opts.alwaysInclude ?? DEFAULT_ALWAYS_INCLUDE;
  const defaultSpreadBps = opts.defaultSpreadBps ?? maxSpreadBps;
  const now = Date.now();

  const bySymbol = new Map<string, FlowInstrument>();
  for (const instrument of symbols) bySymbol.set(instrument.symbol, instrument);

  const entries: FlowUniverseEntry[] = [];
  for (const [symbol, turnover24h] of Object.entries(volumes)) {
    if (!symbol.toUpperCase().endsWith("USDT")) continue;
    const instrument = bySymbol.get(symbol);
    if (!instrument?.listedTime) continue; // age is unknowable without a listing time
    if (instrument.status !== undefined && instrument.status !== "Trading") {
      continue;
    }
    const ageDays = (now - instrument.listedTime) / MS_PER_DAY;
    if (ageDays < minAgeDays) continue;
    const spreadBps = spreadInBps(instrument, defaultSpreadBps);
    if (spreadBps > maxSpreadBps) continue;
    entries.push({
      symbol,
      turnover24h,
      spreadBps,
      ageDays,
      fundingRate: funding?.[symbol],
      rank: 0,
    });
  }

  // Rank by turnover desc, then trim to topN.
  entries.sort((a, b) => b.turnover24h - a.turnover24h);
  const ranked = entries.slice(0, topN);

  // Majors are always carried, even when below the cutoff or filtered out —
  // but only when they are actually live tickers (present in `volumes`).
  for (const base of alwaysInclude) {
    const symbol = `${base.toUpperCase()}USDT`;
    if (ranked.some((e) => e.symbol === symbol)) continue;
    const turnover24h = volumes[symbol];
    if (turnover24h === undefined) continue;
    const instrument = bySymbol.get(symbol);
    if (!instrument?.listedTime) continue;
    if (instrument.status !== undefined && instrument.status !== "Trading") {
      continue;
    }
    const ageDays = (now - instrument.listedTime) / MS_PER_DAY;
    ranked.push({
      symbol,
      turnover24h,
      spreadBps: spreadInBps(instrument, defaultSpreadBps),
      ageDays,
      fundingRate: funding?.[symbol],
      rank: 0,
    });
  }

  ranked.sort((a, b) => b.turnover24h - a.turnover24h);
  return ranked.map((entry, index) => ({ ...entry, rank: index + 1 }));
}

function spreadInBps(
  instrument: FlowInstrument | undefined,
  defaultSpreadBps: number,
): number {
  const bid = instrument?.bid1Price;
  const ask = instrument?.ask1Price;
  if (bid !== undefined && ask !== undefined && bid > 0) {
    return ((ask - bid) / bid) * 10_000;
  }
  return defaultSpreadBps;
}
