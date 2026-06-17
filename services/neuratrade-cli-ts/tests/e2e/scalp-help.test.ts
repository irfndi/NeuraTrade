import { describe, expect, it } from "bun:test";

async function runCli(
  args: ReadonlyArray<string>,
): Promise<{ exitCode: number | null; stdout: string; stderr: string }> {
  const proc = Bun.spawn(["bun", "run", "index.ts", ...args], {
    cwd: import.meta.dir + "/../..",
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

const EXIT_ENGINE_FLAGS = [
  "--atr-risk-reward",
  "--scale-out-at-r",
  "--scale-out-pct",
  "--volatility-lookback",
  "--volatility-low-pct",
  "--volatility-high-pct",
  "--volatility-low-factor",
  "--volatility-high-factor",
] as const;

const PROFILE_FLAG = "--profile";

describe("scalp command help", () => {
  it.each(["backtest", "optimize", "scan", "paper-trade", "soak"])(
    "%s --help lists exit-engine options",
    async (subcommand) => {
      const { stdout, stderr } = await runCli(["scalp", subcommand, "--help"]);
      const output = stdout + stderr;
      for (const flag of EXIT_ENGINE_FLAGS) {
        expect(output).toContain(flag);
      }
    },
    15000,
  );

  it.each(["backtest", "optimize", "scan", "paper-trade", "soak"])(
    "%s --help lists --profile option",
    async (subcommand) => {
      const { stdout, stderr } = await runCli(["scalp", subcommand, "--help"]);
      const output = stdout + stderr;
      expect(output).toContain(PROFILE_FLAG);
    },
    15000,
  );
});
