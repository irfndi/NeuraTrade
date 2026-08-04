import { describe, expect, it } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

async function runCli(
  home: string,
  args: readonly string[],
  environment: Record<string, string | undefined> = {},
): Promise<{ readonly exitCode: number; readonly output: string }> {
  const proc = Bun.spawn(["bun", "run", "index.ts", ...args], {
    env: { ...process.env, NEURATRADE_HOME: home, ...environment },
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr, exitCode] = await Promise.all([
    new Response(proc.stdout).text(),
    new Response(proc.stderr).text(),
    proc.exited,
  ]);
  return { exitCode, output: `${stdout}\n${stderr}` };
}

describe("live execution safety", () => {
  it("fails before any market request for the disabled signal strategy", async () => {
    const home = await mkdtemp(join(tmpdir(), "neuratrade-live-signal-"));
    try {
      const result = await runCli(
        home,
        [
          "scalp",
          "paper-trade",
          "--live",
          "--futures",
          "--strategy-type",
          "signal",
          "--iterations",
          "1",
        ],
        { BITGET_USE_SANDBOX: "true" },
      );

      expect(result.exitCode).not.toBe(0);
      expect(result.output).toContain(
        "live directional signal execution is disabled",
      );
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("returns a non-zero exit when the grid live configuration is invalid", async () => {
    const home = await mkdtemp(join(tmpdir(), "neuratrade-live-config-"));
    try {
      const result = await runCli(
        home,
        [
          "scalp",
          "paper-trade",
          "--live",
          "--futures",
          "--strategy-type",
          "grid",
          "--margin-mode",
          "invalid",
          "--iterations",
          "1",
        ],
        { BITGET_USE_SANDBOX: "true" },
      );

      expect(result.exitCode).not.toBe(0);
      expect(result.output).toContain("invalid margin-mode");
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("rejects an unvalidated grid profile before contacting the exchange", async () => {
    const home = await mkdtemp(join(tmpdir(), "neuratrade-live-grid-"));
    try {
      const result = await runCli(
        home,
        [
          "scalp",
          "paper-trade",
          "--live",
          "--futures",
          "--strategy-type",
          "grid",
          "--iterations",
          "1",
        ],
        { BITGET_USE_SANDBOX: "true" },
      );

      expect(result.exitCode).not.toBe(0);
      expect(result.output).toContain(
        "live grid must use the validated BTC 15m grid candidate",
      );
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("rejects the validated live candidate when sandbox mode is disabled", async () => {
    const home = await mkdtemp(join(tmpdir(), "neuratrade-live-sandbox-"));
    try {
      const result = await runCli(
        home,
        [
          "scalp",
          "paper-trade",
          "--live",
          "--futures",
          "--strategy-type",
          "grid",
          "--exchange",
          "bitget-futures",
          "--symbol",
          "BTC/USDT:USDT",
          "--timeframe",
          "15m",
          "--product-type",
          "USDT-FUTURES",
          "--grid-step-pct",
          "1",
          "--grid-max-grids",
          "1.5",
          "--grid-pause-after-loss-bars",
          "12",
          "--fee",
          "0.02",
          "--slippage-bps",
          "1",
          "--target-ratio",
          "1",
          "--chop-gate-adx",
          "30",
          "--leverage",
          "1",
          "--max-position-size-pct",
          "50",
          "--max-drawdown-pct",
          "5",
          "--max-daily-loss-pct",
          "2",
          "--iterations",
          "1",
        ],
        { BITGET_USE_SANDBOX: "false" },
      );

      expect(result.exitCode).not.toBe(0);
      expect(result.output).toContain(
        "live execution is disabled until BITGET_USE_SANDBOX=true is configured",
      );
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("fails before loading a watchlist for the disabled live soak surface", async () => {
    const home = await mkdtemp(join(tmpdir(), "neuratrade-live-soak-"));
    try {
      const result = await runCli(home, [
        "scalp",
        "soak",
        "--live",
        "--futures",
        "--watchlist",
        "missing-watchlist.json",
      ]);

      expect(result.exitCode).not.toBe(0);
      expect(result.output).toContain("live soak is disabled");
      expect(result.output).not.toContain("Failed to load soak watchlist");
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);
});
