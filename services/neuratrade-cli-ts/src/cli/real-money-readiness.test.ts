import { describe, expect, it, afterEach } from "bun:test";
import { Database } from "bun:sqlite";
import * as fs from "fs";
import * as os from "os";
import * as nodePath from "path";
import {
  parseRealMoneyReadinessArgs,
  runRealMoneyReadiness,
} from "./real-money-readiness.js";
import { fingerprintStrategyManifest } from "../scalping/real-money-readiness.js";
import { DEFAULT_STRATEGY_MANIFEST } from "../scalping/real-money-readiness.js";

function tmpDir(): string {
  return fs.mkdtempSync(nodePath.join(os.tmpdir(), "readiness-cli-unit-"));
}

function rmDir(dir: string): void {
  try {
    fs.rmSync(dir, { recursive: true, force: true });
  } catch {
    // ignore
  }
}

/** Build a full readiness schema with the given number of valid demo trades. */
function makeSeededHome(tradeCount: number, tamper = false): string {
  const home = tmpDir();
  const data = nodePath.join(home, "data");
  fs.mkdirSync(data, { recursive: true });
  const db = new Database(nodePath.join(data, "neuratrade.db"));
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
  const expectedFingerprint = fingerprintStrategyManifest(
    DEFAULT_STRATEGY_MANIFEST,
  );
  const fingerprint = tamper ? "b".repeat(64) : expectedFingerprint;
  const insert = db.query(
    `INSERT INTO grid_paper_trades
     (id, exchange, symbol, timeframe, fill_source, entry_order_id, exit_order_id,
      entry_filled_qty_decimal, exit_filled_qty_decimal, entry_fee_decimal,
      exit_fee_decimal, realized_pnl_pct_decimal, opened_at, closed_at,
      strategy_config_fingerprint, cohort_id, candidate_lock_at, dataset_cutoff_at,
      entry_opened_at, execution_environment)
     VALUES (?, 'bitget-futures', 'BTC/USDT:USDT', '15m', 'live', ?, ?,
             '0.01', '0.01', '0.1', '0.1', '0.1', ?, ?, ?, 'cohort-unit', ?, ?, ?, 'bitget-demo')`,
  );
  for (let index = 0; index < tradeCount; index += 1) {
    insert.run(
      `trade-${index}`,
      `entry-${index}`,
      `exit-${index}`,
      "2026-08-01T00:00:00.000Z",
      `2026-08-0${(index % 9) + 1}T01:00:00.000Z`,
      fingerprint,
      "2026-07-01T00:00:00.000Z",
      "2026-07-31T23:45:00.000Z",
      "2026-08-01T00:00:00.000Z",
    );
  }
  db.close();
  return home;
}

describe("real-money-readiness CLI contract", () => {
  const tempHomes: string[] = [];

  afterEach(() => {
    for (const home of tempHomes) rmDir(home);
    tempHomes.length = 0;
  });

  it("parses the default candidate and explicit candidate overrides", () => {
    const defaults = parseRealMoneyReadinessArgs([]);
    const explicit = parseRealMoneyReadinessArgs([
      "--exchange",
      "bitget-futures",
      "--symbol",
      "BTC/USDT:USDT",
      "--timeframe",
      "15m",
    ]);

    expect(defaults).toEqual({
      kind: "ok",
      args: {
        exchange: "bitget-futures",
        symbol: "BTC/USDT:USDT",
        timeframe: "15m",
      },
    });
    expect(explicit).toEqual(defaults);
  });

  it("rejects unknown, missing, and test-only production arguments", () => {
    expect(parseRealMoneyReadinessArgs(["--unknown", "value"])).toEqual({
      kind: "error",
      message: "unknown option: --unknown",
    });
    expect(parseRealMoneyReadinessArgs(["--symbol"])).toEqual({
      kind: "error",
      message: "missing value for --symbol",
    });
    expect(parseRealMoneyReadinessArgs(["--parity-fixture", "golden"])).toEqual(
      {
        kind: "error",
        message: "--parity-fixture is test-runner-only",
      },
    );
  });

  it("returns ERROR/2 when the requested database is absent", () => {
    const result = runRealMoneyReadiness([], {
      home: "/tmp/neuratrade-readiness-unit-database-does-not-exist",
    });

    expect(result.exitCode).toBe(2);
    expect(result.report.status).toBe("ERROR");
    expect(result.report.errors).toHaveLength(1);
  });

  it("returns ERROR/2 when the schema is missing a readiness table", () => {
    const home = tmpDir();
    tempHomes.push(home);
    const data = nodePath.join(home, "data");
    fs.mkdirSync(data, { recursive: true });
    const db = new Database(nodePath.join(data, "neuratrade.db"));
    db.exec("CREATE TABLE unrelated (id INTEGER PRIMARY KEY)");
    db.close();

    const result = runRealMoneyReadiness([], { home });

    expect(result.exitCode).toBe(2);
    expect(result.report.status).toBe("ERROR");
    expect(result.report.errors[0]).toContain("missing readiness table");
  });

  it("returns ERROR/2 when the schema is missing readiness columns", () => {
    const home = tmpDir();
    tempHomes.push(home);
    const data = nodePath.join(home, "data");
    fs.mkdirSync(data, { recursive: true });
    const db = new Database(nodePath.join(data, "neuratrade.db"));
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
        timeframe TEXT NOT NULL, opened_at DATETIME NOT NULL, closed_at DATETIME NOT NULL
      );
    `);
    db.close();

    const result = runRealMoneyReadiness([], { home });

    expect(result.exitCode).toBe(2);
    expect(result.report.status).toBe("ERROR");
    expect(result.report.errors[0]).toContain("missing readiness column");
  });

  it("computes the demo cohort and provenance from a seeded database", () => {
    const home = makeSeededHome(60);
    tempHomes.push(home);

    const result = runRealMoneyReadiness([], {
      home,
      now: new Date("2026-08-02T00:00:00.000Z"),
    });

    expect(result.exitCode).toBe(1);
    expect(result.report.status).toBe("FAIL");
    expect(result.report.metrics.prospective.completeTradeCount).toBe(60);
    expect(
      result.report.metrics.prospective.allTradesHaveLiveFillEvidence,
    ).toBe(true);
    expect(result.report.metrics.provenance.valid).toBe(true);
    expect(result.report.metrics.provenance.queriedRows).toBe(60);
    expect(result.report.metrics.provenance.expectedRows).toBe(60);
    // Data-quality/robustness gates need candles; with none present the
    // cohort must fail closed on the historical and data-quality gates.
    expect(result.report.failedGateIds).toContain("data-quality");
    // No backtest evidence → the stress gate must FAIL CLOSED, not pass on
    // zero/zero defaults (regression: the missing-evidence default carried
    // the full seed set, so the gate passed spuriously).
    const stressGate = result.report.gates.find((gate) => gate.id === "stress");
    expect(stressGate?.passed).toBe(false);
    expect(stressGate?.reasons).toContain(
      "adverse stress seed set is incomplete",
    );
  });

  it("ignores untagged legacy fills outside the readiness cohort", () => {
    const home = makeSeededHome(60);
    tempHomes.push(home);
    const db = new Database(nodePath.join(home, "data", "neuratrade.db"));
    db.exec(`INSERT INTO grid_paper_trades
      (id, exchange, symbol, timeframe, fill_source, entry_order_id, exit_order_id,
       entry_filled_qty_decimal, exit_filled_qty_decimal, entry_fee_decimal,
       exit_fee_decimal, realized_pnl_pct_decimal, opened_at, closed_at,
       strategy_config_fingerprint, cohort_id, candidate_lock_at, dataset_cutoff_at,
       entry_opened_at, execution_environment)
      VALUES ('legacy-1', 'bitget-futures', 'BTC/USDT:USDT', '15m', 'live',
              'legacy-entry', 'legacy-exit', '0.01', '0.01', '0.1', '0.1', '0.1',
              '2026-07-01T00:00:00.000Z', '2026-07-02T00:00:00.000Z',
              NULL, NULL, NULL, NULL, NULL, 'bitget-demo')`);
    db.close();

    const result = runRealMoneyReadiness([], {
      home,
      now: new Date("2026-08-02T00:00:00.000Z"),
    });

    // Untagged fills are rejected (excluded from the cohort): they must not
    // count toward prospective evidence nor veto the provenance gate
    // (regression: the evidence query returned every row, so a single old
    // fill blocked the gate forever once the cohort was clean).
    expect(result.report.metrics.prospective.completeTradeCount).toBe(60);
    expect(result.report.metrics.provenance.valid).toBe(true);
    expect(result.report.metrics.provenance.queriedRows).toBe(60);
  });

  it("fails the provenance gate when a seeded trade fingerprint is tampered", () => {
    const home = makeSeededHome(60, true);
    tempHomes.push(home);

    const result = runRealMoneyReadiness([], {
      home,
      now: new Date("2026-08-02T00:00:00.000Z"),
    });

    expect(result.report.status).toBe("FAIL");
    expect(result.report.metrics.provenance.valid).toBe(false);
    expect(result.report.failedGateIds).toContain("provenance");
  });

  it("rejects the parity fixture in production but accepts it in the test factory", () => {
    const home = makeSeededHome(60);
    tempHomes.push(home);

    const prod = runRealMoneyReadiness(["--parity-fixture", "golden"], {
      home,
    });
    expect(prod.exitCode).toBe(2);
    expect(prod.report.status).toBe("ERROR");
    expect(prod.report.errors[0]).toContain(
      "--parity-fixture is test-runner-only",
    );

    const factory = runRealMoneyReadiness(["--parity-fixture", "golden"], {
      home,
      testFactory: true,
      parityFixture: "golden",
      now: new Date("2026-08-02T00:00:00.000Z"),
    });
    expect(factory.exitCode).toBe(1);
    expect(factory.report.status).toBe("FAIL");
    expect(
      factory.report.gates.find((gate) => gate.id === "execution-parity")
        ?.passed,
    ).toBe(true);
  });

  it("passes the execution-parity gate from the measured data artifact", () => {
    const home = makeSeededHome(60);
    tempHomes.push(home);
    fs.writeFileSync(
      nodePath.join(home, "data", "execution-parity.json"),
      JSON.stringify({
        protocolVersion: "execution-parity/v1",
        generatedAt: "2026-08-01T00:00:00.000Z",
        exchange: "bitget-futures",
        symbol: "BTC/USDT:USDT",
        timeframe: "15m",
        barCount: 500,
        backtestTrades: 2,
        deployedTrades: 2,
        checks: [
          {
            name: "trigger-bar",
            passed: true,
            detail: "backtest=2 deployed=2",
          },
          {
            name: "order-type",
            passed: true,
            detail: "both use limit entry at grid level",
          },
          {
            name: "fill-price",
            passed: true,
            detail: "2/2 entries within 0.5%",
          },
          {
            name: "fees",
            passed: true,
            detail: "both charge 0.12% round-trip",
          },
          {
            name: "slippage",
            passed: true,
            detail: "both apply slippageBps=2",
          },
          {
            name: "quantity",
            passed: true,
            detail: "both size at 50% of capital",
          },
          {
            name: "exit-reason",
            passed: true,
            detail: "2/2 exit reasons equal",
          },
          { name: "pnl", passed: true, detail: "2/2 within 0.5pp" },
        ],
      }),
    );

    const result = runRealMoneyReadiness([], {
      home,
      now: new Date("2026-08-02T00:00:00.000Z"),
    });

    const parityGate = result.report.gates.find(
      (gate) => gate.id === "execution-parity",
    );
    expect(parityGate?.passed).toBe(true);
    expect(parityGate?.reasons).toEqual([]);
    // The rest of the cohort still fails closed without candle evidence.
    expect(result.exitCode).toBe(1);
    expect(result.report.failedGateIds).toContain("data-quality");
  });

  it("fails closed when the measured artifact reports a failed check", () => {
    const home = makeSeededHome(60);
    tempHomes.push(home);
    const checks = [
      "trigger-bar",
      "order-type",
      "fill-price",
      "fees",
      "slippage",
      "quantity",
      "exit-reason",
      "pnl",
    ].map((name) => ({
      name,
      passed: name !== "pnl",
      detail: `${name}: ${name === "pnl" ? "0/2 within 0.5pp" : "OK"}`,
    }));
    fs.writeFileSync(
      nodePath.join(home, "data", "execution-parity.json"),
      JSON.stringify({ protocolVersion: "execution-parity/v1", checks }),
    );

    const result = runRealMoneyReadiness([], {
      home,
      now: new Date("2026-08-02T00:00:00.000Z"),
    });

    const parityGate = result.report.gates.find(
      (gate) => gate.id === "execution-parity",
    );
    expect(parityGate?.passed).toBe(false);
    expect(parityGate?.reasons).toContain("execution parity check failed: pnl");
  });
});
