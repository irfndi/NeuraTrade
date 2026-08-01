import { describe, expect, it } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

async function runPaperTrade(
  home: string,
  args: readonly string[],
): Promise<{ readonly exitCode: number; readonly output: string }> {
  const proc = Bun.spawn(
    ["bun", "run", "index.ts", "scalp", "paper-trade", ...args],
    {
      env: { ...process.env, NEURATRADE_HOME: home },
      stdout: "pipe",
      stderr: "pipe",
    },
  );
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
      const result = await runPaperTrade(home, [
        "--live",
        "--futures",
        "--strategy-type",
        "signal",
        "--iterations",
        "1",
      ]);

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
      const result = await runPaperTrade(home, [
        "--live",
        "--futures",
        "--strategy-type",
        "grid",
        "--margin-mode",
        "invalid",
        "--iterations",
        "1",
      ]);

      expect(result.exitCode).not.toBe(0);
      expect(result.output).toContain("invalid margin-mode");
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("rejects an unvalidated grid profile before contacting the exchange", async () => {
    const home = await mkdtemp(join(tmpdir(), "neuratrade-live-grid-"));
    try {
      const result = await runPaperTrade(home, [
        "--live",
        "--futures",
        "--strategy-type",
        "grid",
        "--iterations",
        "1",
      ]);

      expect(result.exitCode).not.toBe(0);
      expect(result.output).toContain(
        "live grid must use the validated BTC 15m grid candidate",
      );
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);
});
