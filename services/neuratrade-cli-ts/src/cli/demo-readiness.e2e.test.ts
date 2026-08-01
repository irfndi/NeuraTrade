import { describe, expect, it } from "bun:test";
import { Database } from "bun:sqlite";
import { Effect } from "effect";
import { mkdir, mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { money } from "../utils/money.js";
import { PaperTradingRepositorySQLite } from "../paper-trading/repository.js";
import type { GridPaperTrade } from "../paper-trading/types.js";

async function runCli(args: readonly string[], home?: string) {
  const proc = Bun.spawn(["bun", "run", "index.ts", ...args], {
    env:
      home === undefined
        ? process.env
        : { ...process.env, NEURATRADE_HOME: home },
    stdout: "pipe",
    stderr: "pipe",
  });
  const stdout = await new Response(proc.stdout).text();
  const stderr = await new Response(proc.stderr).text();
  const exitCode = await proc.exited;
  return { exitCode, stdout, stderr };
}

async function seedPassingTrades(home: string): Promise<void> {
  const dataDir = join(home, "data");
  await mkdir(dataDir, { recursive: true });
  const db = new Database(join(dataDir, "neuratrade.db"));
  const repository = new PaperTradingRepositorySQLite(db);
  await Effect.runPromise(repository.ensureTables());
  for (let index = 0; index < 50; index++) {
    const openedAt = new Date(Date.UTC(2026, 0, 1 + index / 4));
    const trade: GridPaperTrade = {
      id: `e2e-${index}`,
      exchange: "bitget-futures",
      symbol: "BTC/USDT:USDT",
      timeframe: "15m",
      side: "long",
      entryPrice: money("70000"),
      exitPrice: money("70010"),
      capitalBefore: money("1000"),
      capitalAfter: money("1002"),
      pnlPct: money("0.2"),
      exitReason: "target",
      openedAt,
      closedAt: new Date(openedAt.getTime() + 60 * 60 * 1000),
      fillSource: "live",
      entryOrderId: `entry-${index}`,
      exitOrderId: `exit-${index}`,
      entryFilledQty: money("0.01"),
      exitFilledQty: money("0.01"),
      entryFee: money("0.01"),
      exitFee: money("0.01"),
      realizedPnlPct: money("0.2"),
    };
    await Effect.runPromise(repository.recordGridTrade(trade));
  }
  db.close();
}

describe("demo-readiness CLI", () => {
  it("exposes threshold controls through the real command surface", async () => {
    const result = await runCli(["scalp", "demo-readiness", "--help"]);

    expect(result.exitCode).toBe(0);
    expect(result.stdout).toContain("--min-trades");
    expect(result.stdout).toContain("--max-drawdown-pct");
  }, 15_000);

  it("fails closed when the real database has no completed live fills", async () => {
    const home = await mkdtemp(join(tmpdir(), "neuratrade-demo-gate-"));
    try {
      const result = await runCli(["scalp", "demo-readiness"], home);

      expect(result.exitCode).not.toBe(0);
      expect(result.stdout).toContain('"status":"FAIL"');
      expect(result.stdout).toContain("trade count is below the minimum");
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("passes through the real database when the completed live-fill fixture qualifies", async () => {
    const home = await mkdtemp(join(tmpdir(), "neuratrade-demo-pass-"));
    try {
      await seedPassingTrades(home);
      const result = await runCli(["scalp", "demo-readiness"], home);

      expect(result.exitCode).toBe(0);
      expect(result.stdout).toContain('"status":"PASS"');
      expect(result.stdout).toContain('"expectancyPct":"0.2"');
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);
});
