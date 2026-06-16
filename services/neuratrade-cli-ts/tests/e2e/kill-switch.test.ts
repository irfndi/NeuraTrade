import { describe, expect, it, beforeAll, afterAll } from "bun:test";
import { Database } from "bun:sqlite";
import { Effect } from "effect";
import * as fs from "fs";
import * as os from "os";
import * as nodePath from "path";
import {
  MarketDataRepository,
  MarketDataRepositorySQLiteLive,
} from "../../src/market-data/repository.js";
import type { Candle } from "../../src/market-data/types.js";

function tmpDir(): string {
  return fs.mkdtempSync(nodePath.join(os.tmpdir(), "kill-switch-e2e-"));
}

function rmDir(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
    // ignore
  }
}

function makeCandles(symbol: string, count: number): Candle[] {
  const candles: Candle[] = [];
  let close = 30_000;
  for (let i = 0; i < count; i++) {
    const open = close;
    close *= 1.001;
    const high = Math.max(open, close) * 1.002;
    const low = Math.min(open, close) * 0.998;
    candles.push({
      exchange: "binance",
      symbol,
      timeframe: "1h",
      open,
      high,
      low,
      close,
      volume: 1,
      timestamp: new Date(Date.now() - (count - i) * 3_600_000),
    });
  }
  return candles;
}

async function seedCandles(home: string, symbol: string): Promise<void> {
  const dbPath = nodePath.join(home, "data", "neuratrade.db");
  const db = new Database(dbPath);
  db.exec("PRAGMA foreign_keys = ON;");
  const repoLayer = MarketDataRepositorySQLiteLive(db);
  await Effect.runPromise(
    Effect.gen(function* () {
      const repo = yield* MarketDataRepository;
      yield* repo.ensureTables();
      yield* repo.saveCandles(makeCandles(symbol, 100));
    }).pipe(Effect.provide(repoLayer)),
  );
  db.close();
}

async function runCli(
  args: ReadonlyArray<string>,
  env?: Record<string, string>,
): Promise<{ exitCode: number | null; stdout: string; stderr: string }> {
  const proc = Bun.spawn(["bun", "run", "index.ts", ...args], {
    cwd: import.meta.dir + "/../..",
    env: { ...process.env, ...env },
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
  ]);
  await proc.exited;
  return { exitCode: proc.exitCode, stdout, stderr };
}

const BASE_ARGS = [
  "scalp",
  "paper-trade",
  "--symbol",
  "BTC/USDT",
  "--timeframe",
  "1h",
  "--iterations",
  "1",
] as const;

describe("kill switch e2e", () => {
  let home: string;

  beforeAll(async () => {
    home = tmpDir();
    fs.mkdirSync(nodePath.join(home, "data"), { recursive: true });
    await seedCandles(home, "BTC/USDT");
  });

  afterAll(() => {
    rmDir(home);
  });

  it(
    "kill switch blocks paper-trade",
    async () => {
      const engage = await runCli([...BASE_ARGS, "--kill-switch"], {
        NEURATRADE_HOME: home,
      });
      expect(engage.stdout).toContain("KILL SWITCH ENGAGED");

      const blocked = await runCli([...BASE_ARGS], { NEURATRADE_HOME: home });
      expect(blocked.stdout).toContain("KILL SWITCH ENGAGED");
    },
    { timeout: 15000 },
  );

  it("disengage allows paper-trade", async () => {
    const engaged = await runCli([...BASE_ARGS, "--kill-switch"], {
      NEURATRADE_HOME: home,
    });
    expect(engaged.stdout).toContain("KILL SWITCH ENGAGED");

    const disengaged = await runCli([...BASE_ARGS, "--disengage"], {
      NEURATRADE_HOME: home,
    });
    expect(disengaged.stdout).not.toContain("KILL SWITCH ENGAGED");
  });
});
