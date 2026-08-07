import { describe, expect, it } from "bun:test";
import { Database } from "bun:sqlite";
import { mkdir, mkdtemp, readdir, rm, stat } from "node:fs/promises";
import { join } from "node:path";
import { fingerprintStrategyManifest } from "../../src/scalping/real-money-readiness.js";
import { DEFAULT_STRATEGY_MANIFEST } from "../../src/scalping/real-money-readiness.js";

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
      // SQLite WAL-mode transient files (-wal/-shm) are timing artifacts of
      // the DB close/checkpoint and must not be treated as state changes.
      if (child.name.endsWith("-wal") || child.name.endsWith("-shm")) continue;
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
      expect(help.stdout.length).toBeGreaterThan(0);
      expect(help.stderr).toBe("");
      expect(version.exitCode).toBe(0);
      expect(version.stdout).toContain("real-money-readiness/v2");
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
      const schemaBefore = await snapshot(schemaHome);
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
      expect(await snapshot(schemaHome)).toEqual(schemaBefore);
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

  it("runs against the shipped legacy-schema fixture without migrating it", async () => {
    const home = await mkdtemp(join("/tmp", "neuratrade-readiness-fixture-"));
    try {
      const fixture = Bun.spawn(
        [
          "bun",
          "run",
          "tests/fixtures/seed-real-money-readiness.ts",
          "--home",
          home,
          "--case",
          "missing-schema",
        ],
        {
          cwd: import.meta.dir + "/../..",
          stdout: "pipe",
          stderr: "pipe",
        },
      );
      const fixtureStderr = await new Response(fixture.stderr).text();
      await fixture.exited;
      expect(fixture.exitCode).toBe(0);
      expect(fixtureStderr).toBe("");

      const before = await snapshot(home);
      const result = await runCli(["scalp", "real-money-readiness"], home);
      const report = JSON.parse(result.stdout) as {
        readonly status: string;
        readonly exitCode: number;
      };

      expect(result.exitCode).toBe(2);
      expect(report.status).toBe("ERROR");
      expect(report.exitCode).toBe(2);
      expect(await snapshot(home)).toEqual(before);
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("reports evidence FAIL after the writer migrates a legacy schema", async () => {
    const home = await mkdtemp(join("/tmp", "neuratrade-readiness-migrated-"));
    try {
      const fixture = Bun.spawn(
        [
          "bun",
          "run",
          "tests/fixtures/seed-real-money-readiness.ts",
          "--home",
          home,
          "--case",
          "migrated-schema",
        ],
        {
          cwd: import.meta.dir + "/../..",
          stdout: "pipe",
          stderr: "pipe",
        },
      );
      const fixtureStderr = await new Response(fixture.stderr).text();
      await fixture.exited;
      expect(fixture.exitCode).toBe(0);
      expect(fixtureStderr).toBe("");

      const before = await snapshot(home);
      const result = await runCli(["scalp", "real-money-readiness"], home);
      const report = JSON.parse(result.stdout) as {
        readonly status: string;
        readonly exitCode: number;
      };

      expect(result.exitCode).toBe(1);
      expect(report.status).toBe("FAIL");
      expect(report.exitCode).toBe(1);
      expect(await snapshot(home)).toEqual(before);
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("fails the provenance gate when a trade fingerprint is tampered", async () => {
    const home = await mkdtemp(join("/tmp", "neuratrade-readiness-tampered-"));
    try {
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
      // A trade whose fingerprint does not match the audited candidate is
      // evidence that the deployed strategy drifted from the validated one.
      // The provenance gate must fail closed on it.
      db.query(
        `INSERT INTO grid_paper_trades
         (id, exchange, symbol, timeframe, fill_source, entry_order_id, exit_order_id,
          entry_filled_qty_decimal, exit_filled_qty_decimal, entry_fee_decimal,
          exit_fee_decimal, realized_pnl_pct_decimal, opened_at, closed_at,
          strategy_config_fingerprint, cohort_id, candidate_lock_at, dataset_cutoff_at,
          entry_opened_at, execution_environment)
         VALUES (?, 'bitget-futures', 'BTC/USDT:USDT', '15m', 'live', 'entry-1', 'exit-1',
                 '0.01', '0.01', '0.1', '0.1', '0.1',
                 ?, ?, ?, 'cohort-e2e', ?, ?, ?, 'bitget-demo')`,
      ).run(
        "tampered-trade-1",
        "2026-08-01T00:00:00.000Z",
        "2026-08-01T01:00:00.000Z",
        "b".repeat(64), // not the audited fingerprint
        "2026-07-01T00:00:00.000Z",
        "2026-07-31T23:45:00.000Z",
        "2026-08-01T00:00:00.000Z",
      );
      db.close();

      const before = await snapshot(home);
      const result = await runCli(["scalp", "real-money-readiness"], home);
      const report = JSON.parse(result.stdout) as {
        readonly status: string;
        readonly exitCode: number;
        readonly gates: Array<{
          readonly id: string;
          readonly passed: boolean;
        }>;
      };

      expect(result.exitCode).toBe(1);
      expect(report.status).toBe("FAIL");
      const provenanceGate = report.gates.find(
        (gate) => gate.id === "provenance",
      );
      expect(provenanceGate?.passed).toBe(false);
      expect(await snapshot(home)).toEqual(before);
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);

  it("queries a distinct cohort when symbol/timeframe args override the defaults", async () => {
    const home = await mkdtemp(join("/tmp", "neuratrade-readiness-args-"));
    try {
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
        INSERT INTO trading_pairs VALUES (2, 'SOL/USDT:USDT');
      `);
      const expectedFingerprint = fingerprintStrategyManifest(
        DEFAULT_STRATEGY_MANIFEST,
      );
      const insert = db.query(
        `INSERT INTO grid_paper_trades
         (id, exchange, symbol, timeframe, fill_source, entry_order_id, exit_order_id,
          entry_filled_qty_decimal, exit_filled_qty_decimal, entry_fee_decimal,
          exit_fee_decimal, realized_pnl_pct_decimal, opened_at, closed_at,
          strategy_config_fingerprint, cohort_id, candidate_lock_at, dataset_cutoff_at,
          entry_opened_at, execution_environment)
         VALUES (?, 'bitget-futures', ?, '15m', 'live', 'entry-1', 'exit-1',
                 '0.01', '0.01', '0.1', '0.1', '0.1',
                 ?, ?, ?, 'cohort-e2e', ?, ?, ?, 'bitget-demo')`,
      );
      insert.run(
        "btc-trade-1",
        "BTC/USDT:USDT",
        "2026-08-01T00:00:00.000Z",
        "2026-08-01T01:00:00.000Z",
        expectedFingerprint,
        "2026-07-01T00:00:00.000Z",
        "2026-07-31T23:45:00.000Z",
        "2026-08-01T00:00:00.000Z",
      );
      insert.run(
        "sol-trade-1",
        "SOL/USDT:USDT",
        "2026-08-01T00:00:00.000Z",
        "2026-08-01T01:00:00.000Z",
        expectedFingerprint,
        "2026-07-01T00:00:00.000Z",
        "2026-07-31T23:45:00.000Z",
        "2026-08-01T00:00:00.000Z",
      );
      db.close();

      const btc = await runCli(["scalp", "real-money-readiness"], home);
      const sol = await runCli(
        [
          "scalp",
          "real-money-readiness",
          "--symbol",
          "SOL/USDT:USDT",
          "--exchange",
          "bitget-futures",
          "--timeframe",
          "15m",
        ],
        home,
      );
      const btcReport = JSON.parse(btc.stdout) as {
        readonly metrics: {
          readonly prospective: { readonly completeTradeCount: number };
        };
      };
      const solReport = JSON.parse(sol.stdout) as {
        readonly metrics: {
          readonly prospective: { readonly completeTradeCount: number };
        };
      };

      // Default = full BTC+SOL cohort: the union sees both fills.
      expect(btcReport.metrics.prospective.completeTradeCount).toBe(2);
      // Scoped run: only the SOL fill.
      expect(solReport.metrics.prospective.completeTradeCount).toBe(1);
    } finally {
      await rm(home, { recursive: true, force: true });
    }
  }, 15_000);
});
