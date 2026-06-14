import { describe, expect, it } from "bun:test";
import { Effect, Layer } from "effect";
import { BunFileSystem } from "@effect/platform-bun";
import { mkdtempSync, rmSync } from "fs";
import { join } from "path";
import { PaperRepository, PaperRepositoryLive } from "./paper-repository.ts";
import { SqliteClientLive } from "./sqlite.ts";
import { PathLive } from "./path.ts";
import { RuntimeConfig } from "./config.ts";

function makeTestLayer(home: string) {
  return Layer.provide(PaperRepositoryLive, SqliteClientLive).pipe(
    Layer.provide(BunFileSystem.layer),
    Layer.provide(
      Layer.succeed(RuntimeConfig, {
        server: { host: "0.0.0.0", port: 8080 },
        database: {
          driver: "sqlite",
          sqlite_path: join(home, "test.db"),
        },
        redis: { host: "127.0.0.1", port: 6379 },
        ccxt: {
          service_url: "http://localhost:3001",
          grpc_address: "127.0.0.1:50051",
        },
        telegram: {
          service_url: "http://localhost:3002",
          grpc_address: "127.0.0.1:50052",
          use_polling: true,
          api_base_url: "http://localhost:8080",
        },
        ai: {
          provider: "openai",
          model: "gpt-4o-mini",
          base_url: undefined,
          temperature: 0.7,
          max_tokens: 4096,
          min_confidence: 0.7,
          daily_budget: "10",
          routing_mode: "primary",
        },
        features: {
          enable_ai: true,
          enable_ai_scalping: true,
          enable_ai_signals: false,
          enable_ai_arbitrage: false,
          paper_trading: true,
          real_trading: false,
        },
        gateway: {
          bind_host: "127.0.0.1",
          ccxt_port: 3001,
          telegram_port: 3002,
          telegram_grpc_port: 50052,
          supervised: false,
          health_timeout_seconds: 150,
          signal_timeout_seconds: 5,
          graceful_timeout_seconds: 10,
          skip_telegram: false,
        },
      }),
    ),
    Layer.provide(PathLive(home)),
  );
}

function runWithRepo<A>(
  program: Effect.Effect<A, unknown, PaperRepository>,
): Promise<A> {
  const home = mkdtempSync(join("/tmp", "paper-repo-test-"));
  const layer = makeTestLayer(home);
  return Effect.runPromise(
    program.pipe(
      Effect.provide(layer),
      Effect.tap(() =>
        Effect.sync(() => {
          try {
            rmSync(home, { recursive: true, force: true });
          } catch {
            // ignore cleanup errors
          }
        }),
      ),
    ),
  );
}

describe("PaperRepository", () => {
  it("opens and closes a paper trade", async () => {
    const result = await runWithRepo(
      Effect.gen(function* () {
        const repo = yield* PaperRepository;
        const id = yield* repo.openTrade({
          symbol: "BTC/USDT",
          exchange: "binance",
          side: "buy",
          size: "0.001",
          notional: "65",
          entry_price: "65000",
          entry_at: "2026-06-13T00:00:00Z",
          signal_id: "sig-1",
          mode: "deterministic",
        });
        const openBefore = yield* repo.getOpenTrade("BTC/USDT", "binance");
        yield* repo.closeTrade({
          id,
          exit_price: "66000",
          exit_at: "2026-06-13T01:00:00Z",
          pnl: "0.65",
          pnl_pct: "1",
          fees: "0.13",
          exit_reason: "signal_reverse",
        });
        const openAfter = yield* repo.getOpenTrade("BTC/USDT", "binance");
        const closed = yield* repo.listClosedTrades(10);
        const stats = yield* repo.getStats();
        return { id, openBefore, openAfter, closed, stats };
      }),
    );

    expect(result.openBefore).not.toBeNull();
    expect(result.openBefore?.status).toBe("open");
    expect(result.openAfter).toBeNull();
    expect(result.closed).toHaveLength(1);
    expect(result.closed[0].status).toBe("closed");
    expect(result.closed[0].pnl).toBe("0.65");
    expect(result.stats.closed_count).toBe(1);
    expect(result.stats.win_count).toBe(1);
  });

  it("lists only open trades", async () => {
    const result = await runWithRepo(
      Effect.gen(function* () {
        const repo = yield* PaperRepository;
        yield* repo.openTrade({
          symbol: "ETH/USDT",
          exchange: "binance",
          side: "buy",
          size: "0.01",
          notional: "25",
          entry_price: "2500",
          entry_at: "2026-06-13T00:00:00Z",
        });
        return yield* repo.listOpenTrades();
      }),
    );
    expect(result).toHaveLength(1);
    expect(result[0].symbol).toBe("ETH/USDT");
    expect(result[0].status).toBe("open");
  });
});
