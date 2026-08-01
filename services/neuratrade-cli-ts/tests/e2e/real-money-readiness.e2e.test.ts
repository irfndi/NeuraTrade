import { describe, expect, it } from "bun:test";
import { Database } from "bun:sqlite";
import { mkdir, mkdtemp, readdir, rm, stat } from "node:fs/promises";
import { join } from "node:path";

async function runCli(
  args: readonly string[],
  home: string,
  extraEnv: Record<string, string> = {},
): Promise<{
  readonly exitCode: number | null;
  readonly stdout: string;
  readonly stderr: string;
}> {
  const child = Bun.spawn(["bun", "run", "index.ts", ...args], {
    cwd: import.meta.dir + "/../..",
    env: { ...process.env, NEURATRADE_HOME: home, ...extraEnv },
    stdout: "pipe",
    stderr: "pipe",
  });
  const [stdout, stderr] = await Promise.all([
    new Response(child.stdout).text(),
    new Response(child.stderr).text(),
  ]);
  await child.exited;
  return { exitCode: child.exitCode, stdout, stderr };
}

async function snapshot(root: string): Promise<readonly string[]> {
  const entries: string[] = [];
  async function walk(path: string): Promise<void> {
    const children = await readdir(path, { withFileTypes: true });
    for (const child of children) {
      const childPath = join(path, child.name);
      entries.push(
        `${childPath}:${child.isDirectory() ? "directory" : (await stat(childPath)).size}`,
      );
      if (child.isDirectory()) await walk(childPath);
    }
  }
  await walk(root);
  return entries.sort();
}

async function makeSchemaHome(): Promise<string> {
  const home = await mkdtemp(join("/tmp", "neuratrade-readiness-e2e-"));
  const data = join(home, "data");
  await mkdir(data, { recursive: true });
  const db = new Database(join(data, "neuratrade.db"));
  db.exec(`
    CREATE TABLE exchanges (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
    CREATE TABLE trading_pairs (id INTEGER PRIMARY KEY, symbol TEXT NOT NULL);
    CREATE TABLE ohlcv_data (
      exchange_id INTEGER NOT NULL, trading_pair_id INTEGER NOT NULL,
      timeframe TEXT NOT NULL, open_price REAL NOT NULL, high_price REAL NOT NULL,
      low_price REAL NOT NULL, close_price REAL NOT NULL, volume REAL NOT NULL,
      timestamp DATETIME NOT NULL
    );
    CREATE TABLE grid_paper_trades (
      id TEXT PRIMARY KEY, exchange TEXT NOT NULL, symbol TEXT NOT NULL,
      timeframe TEXT NOT NULL, fill_source TEXT, entry_order_id TEXT,
      exit_order_id TEXT, entry_filled_qty_decimal TEXT, exit_filled_qty_decimal TEXT,
      entry_fee_decimal TEXT, exit_fee_decimal TEXT, realized_pnl_pct_decimal TEXT,
      opened_at DATETIME NOT NULL, closed_at DATETIME NOT NULL,
      strategy_config_fingerprint TEXT, cohort_id TEXT, candidate_lock_at DATETIME,
      dataset_cutoff_at DATETIME, entry_opened_at DATETIME, execution_environment TEXT
    );
    INSERT INTO exchanges VALUES (1, 'bitget-futures');
    INSERT INTO trading_pairs VALUES (1, 'BTC/USDT:USDT');
  `);
  db.close();
  return home;
}

describe("real-money-readiness CLI", () => {
  it("prints plain help and version without opening a database", async () => {
    const home = await mkdtemp(join("/tmp", "neuratrade-readiness-help-"));
    try {
      const before = await snapshot(home);
      const help = await runCli(
        ["scalp", "real-money-readiness", "--help"],
        home,
      );
      const version = await runCli(
        ["scalp", "real-money-readiness", "--version"],
        home,
      );

      expect(help.exitCode).toBe(0);
      expect(help.stdout).toContain("Read-only");
      expect(help.stderr).toBe("");
      expect(version.exitCode).toBe(0);
      expect(version.stdout).toContain("real-money-readiness/v1");
      expect(version.stderr).toBe("");
      expect(await snapshot(home)).toEqual(before);
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("distinguishes missing infrastructure ERROR/2 from evidence FAIL/1", async () => {
    const missingHome = await mkdtemp(
      join("/tmp", "neuratrade-readiness-missing-"),
    );
    const schemaHome = await makeSchemaHome();
    try {
      const missing = await runCli(
        ["scalp", "real-money-readiness"],
        missingHome,
      );
      const failed = await runCli(
        ["scalp", "real-money-readiness"],
        schemaHome,
      );
      const missingReport = JSON.parse(missing.stdout) as {
        readonly status: string;
        readonly exitCode: number;
      };
      const failedReport = JSON.parse(failed.stdout) as {
        readonly status: string;
        readonly exitCode: number;
      };

      expect(missing.exitCode).toBe(2);
      expect(missingReport.status).toBe("ERROR");
      expect(missingReport.exitCode).toBe(2);
      expect(failed.exitCode).toBe(1);
      expect(failedReport.status).toBe("FAIL");
      expect(failedReport.exitCode).toBe(1);
    } finally {
      await rm(missingHome, { recursive: true, force: true });
      await rm(schemaHome, { recursive: true, force: true });
    }
  }, 15_000);

  it("rejects the test-only parity fixture at the production boundary", async () => {
    const home = await mkdtemp(
      join("/tmp", "neuratrade-readiness-production-"),
    );
    try {
      const result = await runCli(
        ["scalp", "real-money-readiness", "--parity-fixture", "golden"],
        home,
        { NEURATRADE_READINESS_PARITY_FIXTURE: "golden" },
      );
      const report = JSON.parse(result.stdout) as {
        readonly status: string;
        readonly exitCode: number;
      };
      expect(result.exitCode).toBe(2);
      expect(report.status).toBe("ERROR");
      expect(report.exitCode).toBe(2);
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);
});
