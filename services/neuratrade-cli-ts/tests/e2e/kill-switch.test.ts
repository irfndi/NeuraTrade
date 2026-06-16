import { describe, expect, it, beforeAll, afterAll } from "bun:test";
import * as fs from "fs";
import * as os from "os";
import * as nodePath from "path";

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

  beforeAll(() => {
    home = tmpDir();
    fs.mkdirSync(nodePath.join(home, "data"), { recursive: true });
  });

  afterAll(() => {
    rmDir(home);
  });

  it("kill switch blocks paper-trade", async () => {
    const engage = await runCli([...BASE_ARGS, "--kill-switch"], {
      NEURATRADE_HOME: home,
    });
    expect(engage.stdout).toContain("KILL SWITCH ENGAGED");

    const blocked = await runCli([...BASE_ARGS], { NEURATRADE_HOME: home });
    expect(blocked.stdout).toContain("KILL SWITCH ENGAGED");
  });

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
