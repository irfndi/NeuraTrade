import { describe, expect, it, beforeEach, afterEach } from "bun:test";
import { Command } from "@effect/cli";
import { BunContext, BunFileSystem } from "@effect/platform-bun";
import { Effect, Layer } from "effect";
import { Buffer } from "node:buffer";
import {
  chunksForRange,
  marketCommand,
  parseDate,
  parseSymbols,
  resolveDateRange,
  timeframeToIntervalMs,
} from "./market.ts";
import { BinanceClient } from "../services/binance-client.ts";
import {
  type CandleRange,
  type CoverageGap,
  MarketRepository,
} from "../services/market-repository.ts";
import type {
  ExchangeInfoResponse,
  RawCandle,
} from "../services/binance-client.ts";

describe("market command helpers", () => {
  describe("parseDate", () => {
    it("parses ISO date strings", () => {
      const date = parseDate("2025-01-01");
      expect(date.toISOString()).toBe("2025-01-01T00:00:00.000Z");
    });

    it("parses RFC3339 strings", () => {
      const date = parseDate("2025-06-15T12:30:00Z");
      expect(date.toISOString()).toBe("2025-06-15T12:30:00.000Z");
    });

    it("throws on invalid input", () => {
      expect(() => parseDate("not-a-date")).toThrow();
    });
  });

  describe("resolveDateRange", () => {
    it("uses provided dates", () => {
      const range = resolveDateRange("2025-01-01", "2025-02-01");
      expect(range.start.toISOString()).toBe("2025-01-01T00:00:00.000Z");
      expect(range.end.toISOString()).toBe("2025-02-01T00:00:00.000Z");
    });

    it("rejects start >= end", () => {
      expect(() => resolveDateRange("2025-02-01", "2025-01-01")).toThrow(
        "before",
      );
    });
  });

  describe("parseSymbols", () => {
    it("splits and uppercases comma-separated symbols", () => {
      expect(parseSymbols("btc/usdt,eth/usdt")).toEqual([
        "BTC/USDT",
        "ETH/USDT",
      ]);
    });

    it("ignores empty entries", () => {
      expect(parseSymbols("BTC/USDT,,  ,ETH/USDT")).toEqual([
        "BTC/USDT",
        "ETH/USDT",
      ]);
    });
  });

  describe("timeframeToIntervalMs", () => {
    it("converts known timeframes", () => {
      expect(timeframeToIntervalMs("1m")).toBe(60_000);
      expect(timeframeToIntervalMs("1h")).toBe(3_600_000);
      expect(timeframeToIntervalMs("1d")).toBe(86_400_000);
    });

    it("throws on unsupported timeframes", () => {
      expect(() => timeframeToIntervalMs("xyz")).toThrow();
    });
  });

  describe("chunksForRange", () => {
    it("splits a range into 1000-candle chunks", () => {
      const start = new Date("2025-01-01T00:00:00Z");
      const end = new Date("2025-01-01T05:00:00Z");
      const chunks = chunksForRange(start, end, 3_600_000, 2);
      expect(chunks).toHaveLength(3);
      expect(chunks[0].from.toISOString()).toBe("2025-01-01T00:00:00.000Z");
      expect(chunks[0].to.toISOString()).toBe("2025-01-01T02:00:00.000Z");
      expect(chunks[2].to.toISOString()).toBe("2025-01-01T05:00:00.000Z");
    });
  });
});

interface CapturedLog {
  level: "stdout" | "stderr";
  message: string;
}

function makeMockBinanceClient(
  exchangeInfo: ExchangeInfoResponse,
  klines: ReadonlyArray<RawCandle> = [],
): Layer.Layer<BinanceClient> {
  return Layer.succeed(BinanceClient, {
    getExchangeInfo: () => Effect.succeed(exchangeInfo),
    getKlines: () => Effect.succeed(klines),
  });
}

function makeMockMarketRepository(
  options: {
    exchangeId?: number;
    pairId?: number;
    range?: CandleRange;
    gaps?: ReadonlyArray<CoverageGap>;
    insertedCount?: number;
  } = {},
): Layer.Layer<MarketRepository> {
  const exchangeId = options.exchangeId ?? 1;
  const pairId = options.pairId ?? 1;
  const range: CandleRange = options.range ?? {
    earliest: null,
    latest: null,
    count: 0,
  };
  const gaps: ReadonlyArray<CoverageGap> = options.gaps ?? [];
  const insertedCount = options.insertedCount ?? 0;
  return Layer.succeed(MarketRepository, {
    ensureExchange: () => Effect.succeed(exchangeId),
    ensureTradingPair: () => Effect.succeed(pairId),
    getCandleRange: () => Effect.succeed(range),
    findCoverageGaps: () => Effect.succeed(gaps),
    getCandles: () => Effect.succeed([]),
    insertCandles: () => Effect.succeed(insertedCount),
    listKnownSymbols: () => Effect.succeed([]),
  });
}

function makeExchangeInfo(
  symbols: ReadonlyArray<{ base: string; quote: string; status?: string }>,
): ExchangeInfoResponse {
  return {
    symbols: symbols.map((s) => ({
      symbol: `${s.base}${s.quote}`,
      status: s.status ?? "TRADING",
      baseAsset: s.base,
      quoteAsset: s.quote,
    })),
  };
}

async function captureConsole<T>(
  fn: () => Promise<T>,
): Promise<{ result: T; logs: ReadonlyArray<CapturedLog> }> {
  const logs: Array<CapturedLog> = [];
  const originalLog = console.log;
  const originalWrite = process.stdout.write.bind(process.stdout);
  const originalErrWrite = process.stderr.write.bind(process.stderr);
  console.log = (...args: ReadonlyArray<unknown>) => {
    logs.push({ level: "stdout", message: args.map(String).join(" ") });
  };
  process.stdout.write = ((chunk: string | Uint8Array) => {
    const text =
      typeof chunk === "string" ? chunk : Buffer.from(chunk).toString();
    if (text.length > 0) {
      logs.push({ level: "stdout", message: text });
    }
    return true;
  }) as typeof process.stdout.write;
  process.stderr.write = ((chunk: string | Uint8Array) => {
    const text =
      typeof chunk === "string" ? chunk : Buffer.from(chunk).toString();
    if (text.length > 0) {
      logs.push({ level: "stderr", message: text });
    }
    return true;
  }) as typeof process.stderr.write;
  try {
    const result = await fn();
    return { result, logs };
  } finally {
    console.log = originalLog;
    process.stdout.write = originalWrite;
    process.stderr.write = originalErrWrite;
  }
}

function makeTestLayer(
  binance: Layer.Layer<BinanceClient>,
  repo: Layer.Layer<MarketRepository>,
) {
  return Layer.mergeAll(BunContext.layer, BunFileSystem.layer, binance, repo);
}

function runMarket(
  args: ReadonlyArray<string>,
  binance: Layer.Layer<BinanceClient>,
  repo: Layer.Layer<MarketRepository>,
) {
  const cli = Command.run(marketCommand, {
    name: "test",
    version: "v1",
  });
  return cli(["node", "test", ...args]).pipe(
    Effect.provide(makeTestLayer(binance, repo)),
    Effect.scoped,
  );
}

describe("market command handlers", () => {
  let originalConsoleLog: typeof console.log;

  beforeEach(() => {
    originalConsoleLog = console.log;
  });

  afterEach(() => {
    console.log = originalConsoleLog;
  });

  describe("fetch-universe", () => {
    it("prints top N USDT symbols in dry-run mode without persisting", async () => {
      const exchangeInfo = makeExchangeInfo([
        { base: "BTC", quote: "USDT" },
        { base: "ETH", quote: "USDT" },
        { base: "BNB", quote: "USDT" },
        { base: "BTC", quote: "BTC" },
      ]);
      const { logs } = await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(
            ["fetch-universe", "--top", "2", "--dry-run"],
            makeMockBinanceClient(exchangeInfo),
            makeMockMarketRepository(),
          ),
        );
        expect(exit._tag).toBe("Success");
      });
      const stdout = logs
        .filter((l) => l.level === "stdout")
        .map((l) => l.message)
        .join("");
      expect(stdout).toContain("Would fetch 2 symbols");
      expect(stdout).toContain("BNB/USDT");
      expect(stdout).toContain("BTC/USDT");
      expect(stdout).not.toContain("ETH/USDT");
    });

    it("persists selected symbols when not in dry-run mode", async () => {
      const exchangeInfo = makeExchangeInfo([
        { base: "BTC", quote: "USDT" },
        { base: "ETH", quote: "USDT" },
      ]);
      let ensureExchangeCalled = 0;
      let ensureTradingPairCalls: Array<string> = [];
      const binance = Layer.succeed(BinanceClient, {
        getExchangeInfo: () => Effect.succeed(exchangeInfo),
        getKlines: () => Effect.succeed([]),
      });
      const repo = Layer.succeed(MarketRepository, {
        ensureExchange: () => {
          ensureExchangeCalled++;
          return Effect.succeed(1);
        },
        ensureTradingPair: (symbol) => {
          ensureTradingPairCalls.push(symbol);
          return Effect.succeed(1);
        },
        getCandleRange: () =>
          Effect.succeed({
            earliest: null,
            latest: null,
            count: 0,
          }),
        findCoverageGaps: () => Effect.succeed([]),
        getCandles: () => Effect.succeed([]),
        insertCandles: () => Effect.succeed(0),
        listKnownSymbols: () => Effect.succeed([]),
      });
      const { logs } = await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(["fetch-universe", "--top", "2"], binance, repo),
        );
        expect(exit._tag).toBe("Success");
      });
      expect(ensureExchangeCalled).toBe(1);
      expect(ensureTradingPairCalls).toEqual(["BTC/USDT", "ETH/USDT"]);
      const stdout = logs
        .filter((l) => l.level === "stdout")
        .map((l) => l.message)
        .join("");
      expect(stdout).toContain("Persisted 2 trading pairs");
    });

    it("fails when neither --top nor --all is provided", async () => {
      const { logs } = await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(
            ["fetch-universe"],
            makeMockBinanceClient(makeExchangeInfo([])),
            makeMockMarketRepository(),
          ),
        );
        expect(exit._tag).toBe("Failure");
      });
      const allLogs = logs.map((l) => l.message).join("");
      expect(allLogs).toContain("Specify --top N or --all");
    });

    it("uses --all to fetch every USDT symbol", async () => {
      const exchangeInfo = makeExchangeInfo([
        { base: "BTC", quote: "USDT" },
        { base: "ETH", quote: "USDT" },
        { base: "SOL", quote: "USDT" },
      ]);
      let ensureTradingPairCalls: Array<string> = [];
      const binance = Layer.succeed(BinanceClient, {
        getExchangeInfo: () => Effect.succeed(exchangeInfo),
        getKlines: () => Effect.succeed([]),
      });
      const repo = Layer.succeed(MarketRepository, {
        ensureExchange: () => Effect.succeed(1),
        ensureTradingPair: (symbol) => {
          ensureTradingPairCalls.push(symbol);
          return Effect.succeed(1);
        },
        getCandleRange: () =>
          Effect.succeed({ earliest: null, latest: null, count: 0 }),
        findCoverageGaps: () => Effect.succeed([]),
        getCandles: () => Effect.succeed([]),
        insertCandles: () => Effect.succeed(0),
        listKnownSymbols: () => Effect.succeed([]),
      });
      await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(["fetch-universe", "--all"], binance, repo),
        );
        expect(exit._tag).toBe("Success");
      });
      expect(ensureTradingPairCalls).toHaveLength(3);
      expect(ensureTradingPairCalls).toContain("SOL/USDT");
    });
  });

  describe("fetch-candles", () => {
    it("inserts candles from gap-filling klines", async () => {
      const klines: ReadonlyArray<RawCandle> = [
        {
          timestamp: new Date("2025-01-01T00:00:00Z"),
          open: "100",
          high: "110",
          low: "95",
          close: "105",
          volume: "1000",
        },
        {
          timestamp: new Date("2025-01-01T01:00:00Z"),
          open: "105",
          high: "115",
          low: "100",
          close: "110",
          volume: "1200",
        },
      ];
      let insertCalls: Array<number> = [];
      const binance = Layer.succeed(BinanceClient, {
        getExchangeInfo: () => Effect.succeed(makeExchangeInfo([])),
        getKlines: () => Effect.succeed(klines),
      });
      const repo = Layer.succeed(MarketRepository, {
        ensureExchange: () => Effect.succeed(1),
        ensureTradingPair: () => Effect.succeed(1),
        getCandleRange: () =>
          Effect.succeed({ earliest: null, latest: null, count: 0 }),
        findCoverageGaps: () =>
          Effect.succeed([
            {
              from: new Date("2025-01-01T00:00:00Z"),
              to: new Date("2025-01-01T02:00:00Z"),
            },
          ]),
        getCandles: () => Effect.succeed([]),
        insertCandles: (candles) => {
          insertCalls.push(candles.length);
          return Effect.succeed(candles.length);
        },
        listKnownSymbols: () => Effect.succeed([]),
      });
      const { logs } = await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(
            [
              "fetch-candles",
              "--symbols",
              "BTC/USDT",
              "--start",
              "2025-01-01",
              "--end",
              "2025-01-02",
              "--timeframe",
              "1h",
            ],
            binance,
            repo,
          ),
        );
        expect(exit._tag).toBe("Success");
      });
      expect(insertCalls[0]).toBe(2);
      const stdout = logs
        .filter((l) => l.level === "stdout")
        .map((l) => l.message)
        .join("");
      expect(stdout).toContain("Inserted 2 candles total");
      expect(stdout).toContain("BTC/USDT");
    });

    it("skips symbols whose coverage is already complete", async () => {
      let getKlinesCalls = 0;
      const binance = Layer.succeed(BinanceClient, {
        getExchangeInfo: () => Effect.succeed(makeExchangeInfo([])),
        getKlines: () => {
          getKlinesCalls++;
          return Effect.succeed([]);
        },
      });
      const repo = Layer.succeed(MarketRepository, {
        ensureExchange: () => Effect.succeed(1),
        ensureTradingPair: () => Effect.succeed(1),
        getCandleRange: () =>
          Effect.succeed({ earliest: null, latest: null, count: 0 }),
        findCoverageGaps: () => Effect.succeed([]),
        getCandles: () => Effect.succeed([]),
        insertCandles: () => Effect.succeed(0),
        listKnownSymbols: () => Effect.succeed([]),
      });
      const { logs } = await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(
            [
              "fetch-candles",
              "--symbols",
              "BTC/USDT",
              "--start",
              "2025-01-01",
              "--end",
              "2025-01-02",
            ],
            binance,
            repo,
          ),
        );
        expect(exit._tag).toBe("Success");
      });
      expect(getKlinesCalls).toBe(0);
      const stdout = logs
        .filter((l) => l.level === "stdout")
        .map((l) => l.message)
        .join("");
      expect(stdout).toContain("already complete");
    });

    it("fails when --symbols is empty", async () => {
      const { logs } = await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(
            ["fetch-candles"],
            makeMockBinanceClient(makeExchangeInfo([])),
            makeMockMarketRepository(),
          ),
        );
        expect(exit._tag).toBe("Failure");
      });
      const allLogs = logs.map((l) => l.message).join("");
      expect(allLogs).toContain("--symbols is required");
    });

    it("uses --no-resume to refetch the full range", async () => {
      let findGapsCalls = 0;
      const klines: ReadonlyArray<RawCandle> = [
        {
          timestamp: new Date("2025-01-01T00:00:00Z"),
          open: "100",
          high: "110",
          low: "95",
          close: "105",
          volume: "1000",
        },
      ];
      const binance = Layer.succeed(BinanceClient, {
        getExchangeInfo: () => Effect.succeed(makeExchangeInfo([])),
        getKlines: () => Effect.succeed(klines),
      });
      const repo = Layer.succeed(MarketRepository, {
        ensureExchange: () => Effect.succeed(1),
        ensureTradingPair: () => Effect.succeed(1),
        getCandleRange: () =>
          Effect.succeed({ earliest: null, latest: null, count: 0 }),
        findCoverageGaps: () => {
          findGapsCalls++;
          return Effect.succeed([]);
        },
        getCandles: () => Effect.succeed([]),
        insertCandles: () => Effect.succeed(1),
        listKnownSymbols: () => Effect.succeed([]),
      });
      await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(
            [
              "fetch-candles",
              "--symbols",
              "BTC/USDT",
              "--start",
              "2025-01-01",
              "--end",
              "2025-01-02",
              "--no-resume",
            ],
            binance,
            repo,
          ),
        );
        expect(exit._tag).toBe("Success");
      });
      expect(findGapsCalls).toBe(0);
    });
  });

  describe("coverage", () => {
    it("prints a coverage report with earliest, latest, count, and gaps", async () => {
      const repo = Layer.succeed(MarketRepository, {
        ensureExchange: () => Effect.succeed(1),
        ensureTradingPair: () => Effect.succeed(1),
        getCandleRange: () =>
          Effect.succeed({
            earliest: new Date("2025-01-01T00:00:00Z"),
            latest: new Date("2025-01-02T00:00:00Z"),
            count: 24,
          }),
        findCoverageGaps: () =>
          Effect.succeed([
            {
              from: new Date("2025-01-01T12:00:00Z"),
              to: new Date("2025-01-01T15:00:00Z"),
            },
          ]),
        getCandles: () => Effect.succeed([]),
        insertCandles: () => Effect.succeed(0),
        listKnownSymbols: () => Effect.succeed([]),
      });
      const { logs } = await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(
            [
              "coverage",
              "--symbols",
              "BTC/USDT",
              "--start",
              "2025-01-01",
              "--end",
              "2025-01-02",
            ],
            makeMockBinanceClient(makeExchangeInfo([])),
            repo,
          ),
        );
        expect(exit._tag).toBe("Success");
      });
      const stdout = logs
        .filter((l) => l.level === "stdout")
        .map((l) => l.message)
        .join("");
      expect(stdout).toContain("Symbol coverage report");
      expect(stdout).toContain("BTC/USDT:");
      expect(stdout).toContain("earliest: 2025-01-01T00:00:00.000Z");
      expect(stdout).toContain("latest:   2025-01-02T00:00:00.000Z");
      expect(stdout).toContain("count:    24");
      expect(stdout).toContain("gaps:     1");
      expect(stdout).toContain("2025-01-01T12:00:00.000Z");
    });

    it("reports n/a when no candles exist", async () => {
      const { logs } = await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(
            [
              "coverage",
              "--symbols",
              "ETH/USDT",
              "--start",
              "2025-01-01",
              "--end",
              "2025-01-02",
            ],
            makeMockBinanceClient(makeExchangeInfo([])),
            makeMockMarketRepository(),
          ),
        );
        expect(exit._tag).toBe("Success");
      });
      const stdout = logs
        .filter((l) => l.level === "stdout")
        .map((l) => l.message)
        .join("");
      expect(stdout).toContain("ETH/USDT:");
      expect(stdout).toContain("earliest: n/a");
      expect(stdout).toContain("latest:   n/a");
      expect(stdout).toContain("count:    0");
    });

    it("fails when --symbols is empty", async () => {
      const { logs } = await captureConsole(async () => {
        const exit = await Effect.runPromiseExit(
          runMarket(
            ["coverage"],
            makeMockBinanceClient(makeExchangeInfo([])),
            makeMockMarketRepository(),
          ),
        );
        expect(exit._tag).toBe("Failure");
      });
      const allLogs = logs.map((l) => l.message).join("");
      expect(allLogs).toContain("--symbols is required");
    });
  });
});
