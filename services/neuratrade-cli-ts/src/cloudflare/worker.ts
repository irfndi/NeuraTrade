import { Effect, Layer } from "effect";

import { MarketDataGateway } from "../market-data/gateway.js";
import { MarketDataGatewayLive } from "../market-data/gateways/index.js";
import {
  runGridUniverseScan,
  DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  type GridUniverseOptions,
} from "../scalping/grid-universe.js";
import type { UniverseWatchEnv } from "../../alchemy.run.ts";
import { CloudflareMarketDataRepositoryLive } from "./market-data-repository.ts";

const EXCHANGE = "bitget-futures";
const TIMEFRAME = "15m";
const SEED_KEY = "seed-symbols";
const WATCHLIST_KEY = "grid-whitelist";

const DEFAULT_SEED_SYMBOLS = [
  "BTC/USDT",
  "ETH/USDT",
  "SOL/USDT",
  "ADA/USDT",
  "XRP/USDT",
  "HYPE/USDT",
  "HOME/USDT",
  "DOGE/USDT",
  "BNB/USDT",
  "LINK/USDT",
] as const;

function scanOptions(): GridUniverseOptions {
  return {
    exchange: EXCHANGE,
    timeframe: TIMEFRAME,
    initialCapital: 10000,
    // Live-fetch horizon: Bitget futures /history-candles caps at 200 per
    // request, so 500 is unreachable on the edge (SQLite local scans differ).
    minCandles: 200,
    trainWindow: 120,
    testWindow: 40,
    minProfitableWindowsPct: 60,
    minAggregateReturnPct: 0,
    feePct: 0.06,
    slippageBps: 2,
    trendFilterPeriod: 0,
    searchSpace: DEFAULT_GRID_UNIVERSE_SEARCH_SPACE,
  };
}

function readSeed(raw: string | null): string[] {
  if (raw === null) return [...DEFAULT_SEED_SYMBOLS];
  try {
    const parsed = JSON.parse(raw) as unknown;
    return Array.isArray(parsed)
      ? (parsed as string[])
      : [...DEFAULT_SEED_SYMBOLS];
  } catch {
    return [...DEFAULT_SEED_SYMBOLS];
  }
}

function runScan(kv: {
  get: (k: string) => Promise<string | null>;
  put: (k: string, v: string) => Promise<void>;
}) {
  return Effect.gen(function* () {
    const seed = yield* Effect.promise(() => kv.get(SEED_KEY)).pipe(
      Effect.map(readSeed),
      Effect.timeout("10 seconds"),
    );

    const gateway = yield* MarketDataGateway;

    const result = yield* runGridUniverseScan(scanOptions()).pipe(
      Effect.provide(
        Layer.provide(
          CloudflareMarketDataRepositoryLive(seed),
          Layer.succeed(MarketDataGateway, gateway),
        ),
      ),
    );

    // Same futures-symbol guard as the local scan: drop survivors whose
    // symbol has no Bitget USDT-M futures contract (public API, no creds).
    const futuresSymbols = yield* gateway
      .fetchSymbols(EXCHANGE)
      .pipe(Effect.catch(() => Effect.succeed([] as readonly string[])));
    const futuresSet = new Set(futuresSymbols);
    const canonical = (symbol: string) =>
      symbol.includes(":")
        ? symbol.slice(0, symbol.lastIndexOf(":"))
        : symbol;
    const survivors =
      futuresSet.size > 0
        ? result.survivors.filter(
            (e) => futuresSet.has(e.symbol) || futuresSet.has(canonical(e.symbol)),
          )
        : result.survivors;

    const whitelist = survivors.map((e) => ({
      symbol: e.symbol,
      exchange: EXCHANGE,
      returnPct: e.walkForward.aggregateReturnPct,
      gridParams: {
        gridStepPct: e.bestParams.gridStepPct,
        gridMaxGrids: e.bestParams.gridMaxGrids,
        gridPauseAfterLossBars: e.bestParams.gridPauseAfterLossBars,
      },
    }));

    if (whitelist.length > 0) {
      yield* Effect.promise(() =>
        kv.put(WATCHLIST_KEY, JSON.stringify(whitelist)),
      ).pipe(Effect.timeout("10 seconds"));
    }
    yield* Effect.log(
      `grid-universe scan: ${result.entries.length} symbols, ${result.survivors.length} survivors`,
    );
    return whitelist;
  }).pipe(Effect.provide(MarketDataGatewayLive));
}

export default {
  async scheduled(
    _controller: { scheduledTime: number },
    env: UniverseWatchEnv,
  ): Promise<void> {
    try {
      await Effect.runPromise(runScan(env.watchlist));
    } catch (err) {
      console.error(`cron scan failed: ${String(err)}`);
    }
  },

  async fetch(request: Request, env: UniverseWatchEnv): Promise<Response> {
    const url = new URL(request.url);

    if (request.method === "GET" && url.pathname.endsWith("/health")) {
      return Response.json({ status: "healthy", service: "universe-watch" });
    }

    if (request.method === "GET" && url.pathname.endsWith("/watchlist")) {
      const raw = await env.watchlist.get(WATCHLIST_KEY);
      if (raw === null) {
        return Response.json({ survivors: [], note: "no scan run yet" });
      }
      return new Response(raw, {
        headers: { "content-type": "application/json" },
      });
    }

    const authorized =
      env.adminKey.length > 0 &&
      request.headers.get("x-api-key") === env.adminKey;
    if (!authorized) {
      return Response.json({ error: "Unauthorized" }, { status: 401 });
    }

    if (request.method === "POST" && url.pathname.endsWith("/scan")) {
      try {
        const survivors = await Effect.runPromise(runScan(env.watchlist));
        return Response.json({ scanned: true, survivors });
      } catch (err) {
        return Response.json(
          { scanned: false, error: String(err) },
          { status: 500 },
        );
      }
    }

    if (request.method === "PUT" && url.pathname.endsWith("/seed")) {
      let symbols: unknown;
      try {
        symbols = await request.json();
      } catch {
        return Response.json({ error: "Invalid JSON body" }, { status: 400 });
      }
      if (
        !Array.isArray(symbols) ||
        symbols.some((s) => typeof s !== "string" || s.trim().length === 0)
      ) {
        return Response.json(
          { error: "seed must be a JSON array of symbol strings" },
          { status: 400 },
        );
      }
      await env.watchlist.put(SEED_KEY, JSON.stringify(symbols));
      return Response.json({ seed: symbols.length });
    }

    return new Response("Not Found", { status: 404 });
  },
};
