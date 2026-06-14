import { describe, expect, it, beforeAll, afterAll } from "bun:test";
import * as fs from "fs";
import * as os from "os";
import * as nodePath from "path";

function tmpDir(): string {
  return fs.mkdtempSync(nodePath.join(os.tmpdir(), "cli-e2e-"));
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

describe("CLI e2e", () => {
  let home: string;

  beforeAll(() => {
    home = tmpDir();
  });

  afterAll(() => {
    rmDir(home);
  });

  it("--help shows command tree", async () => {
    const { exitCode, stdout } = await runCli(["--help"]);
    expect(exitCode).toBe(0);
    expect(stdout).toContain("NeuraTrade CLI");
    expect(stdout).toContain("gateway");
    expect(stdout).toContain("status");
    expect(stdout).toContain("health");
    expect(stdout).toContain("doctor");
  });

  it("doctor checks runtime paths", async () => {
    const { exitCode, stdout } = await runCli(["doctor"], {
      NEURATRADE_HOME: home,
    });
    expect(exitCode).toBe(0);
    expect(stdout).toContain("NeuraTrade Doctor");
    expect(stdout).toContain(home);
    expect(stdout).toContain("Config file");
    expect(stdout).toContain("Runtime file");
  });

  it("status reports unreachable backend when not running", async () => {
    const { exitCode, stdout } = await runCli(["status"], {
      NEURATRADE_HOME: home,
      SERVER_PORT: "19999",
    });
    expect(exitCode).toBe(0);
    expect(stdout).toContain("NeuraTrade System Status");
    expect(stdout).toContain("unknown");
  });

  it("health reports failure when backend is not running", async () => {
    const { exitCode, stdout, stderr } = await runCli(["health"], {
      NEURATRADE_HOME: home,
      SERVER_PORT: "19998",
    });
    expect(exitCode).toBe(1);
    expect(stdout + stderr).toContain("Could not reach API");
  });

  it("gateway status reports down when no state exists", async () => {
    const { exitCode, stdout } = await runCli(["gateway", "status"], {
      NEURATRADE_HOME: home,
    });
    expect(exitCode).toBe(0);
    expect(stdout).toContain("NeuraTrade Service Status");
  });

  it("gateway stop reports no running services", async () => {
    const { exitCode, stdout } = await runCli(["gateway", "stop"], {
      NEURATRADE_HOME: home,
    });
    expect(exitCode).toBe(0);
    expect(stdout).toContain("No running services found");
  });

  it("bitget --help lists spot and futures commands", async () => {
    const { exitCode, stdout } = await runCli(["bitget", "--help"], {
      NEURATRADE_HOME: home,
    });
    expect(exitCode).toBe(0);
    expect(stdout).toContain("verify");
    expect(stdout).toContain("futures");
    expect(stdout).toContain("order place");
  });

  it("bitget futures order place --dry-run fails gracefully without credentials", async () => {
    const { exitCode, stdout, stderr } = await runCli(
      [
        "bitget",
        "futures",
        "order",
        "place",
        "--symbol",
        "BTC/USDT:USDT",
        "--side",
        "buy",
        "--size",
        "0.001",
        "--dry-run",
      ],
      { NEURATRADE_HOME: home },
    );
    expect(exitCode).toBe(1);
    expect(stdout + stderr).toContain("Bitget credentials missing");
  });
});
