import { Database } from "bun:sqlite";
import { mkdir } from "node:fs/promises";
import { join } from "node:path";

type FixtureCase = "pass" | "fail" | "missing-schema";

function optionValue(argv: readonly string[], name: string): string | null {
  const index = argv.indexOf(name);
  return index >= 0 ? (argv[index + 1] ?? null) : null;
}

function schemaSql(): string {
  return `
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
  `;
}

async function main(): Promise<void> {
  const home = optionValue(process.argv.slice(2), "--home");
  const fixtureCase = optionValue(
    process.argv.slice(2),
    "--case",
  ) as FixtureCase | null;
  if (home === null || fixtureCase === null) {
    throw new Error("--home and --case are required");
  }
  if (!["pass", "fail", "missing-schema"].includes(fixtureCase)) {
    throw new Error(`unsupported fixture case: ${fixtureCase}`);
  }
  const dataDir = join(home, "data");
  await mkdir(dataDir, { recursive: true });
  const db = new Database(join(dataDir, "neuratrade.db"));
  if (fixtureCase === "missing-schema") {
    db.exec("CREATE TABLE legacy_fixture (id INTEGER PRIMARY KEY)");
  } else {
    db.exec(schemaSql());
  }
  if (process.argv.includes("--wal")) {
    db.exec("PRAGMA journal_mode = WAL");
    db.exec("PRAGMA wal_autocheckpoint = 1000000");
    db.exec("BEGIN IMMEDIATE");
    db.exec("INSERT INTO exchanges VALUES (2, 'fixture-wal')");
  }
  db.close(false);
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
