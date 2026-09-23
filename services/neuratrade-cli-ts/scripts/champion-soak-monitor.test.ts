import { describe, expect, it } from "bun:test";
import { execFileSync } from "node:child_process";
import {
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const SCRIPT = new URL("./champion-soak-monitor.sh", import.meta.url).pathname;

const LADDER_DDL = `CREATE TABLE ladder_paper_trades (
  id TEXT PRIMARY KEY, exchange TEXT NOT NULL, symbol TEXT NOT NULL,
  timeframe TEXT NOT NULL, side TEXT NOT NULL, rung_index INTEGER NOT NULL,
  entry_price REAL NOT NULL, exit_price REAL NOT NULL,
  capital_before REAL NOT NULL, capital_after REAL NOT NULL,
  pnl REAL NOT NULL, pnl_pct REAL NOT NULL,
  entry_price_decimal TEXT NOT NULL, exit_price_decimal TEXT NOT NULL,
  capital_before_decimal TEXT NOT NULL, capital_after_decimal TEXT NOT NULL,
  pnl_decimal TEXT NOT NULL, pnl_pct_decimal TEXT NOT NULL,
  exit_reason TEXT NOT NULL,
  opened_at DATETIME NOT NULL, closed_at DATETIME NOT NULL
);`;

function sqlite(db: string, sql: string): void {
  execFileSync("sqlite3", [db, sql]);
}

function setupHome(root: string, name: string, openedLines: number): string {
  const home = join(root, name);
  mkdirSync(join(home, "data"), { recursive: true });
  mkdirSync(join(home, "logs"), { recursive: true });
  const db = join(home, "data", "neuratrade.db");
  sqlite(db, LADDER_DDL);
  sqlite(
    db,
    `INSERT INTO ladder_paper_trades (id, exchange, symbol, timeframe, side, rung_index,
      entry_price, exit_price, capital_before, capital_after, pnl, pnl_pct,
      entry_price_decimal, exit_price_decimal, capital_before_decimal, capital_after_decimal,
      pnl_decimal, pnl_pct_decimal, exit_reason, opened_at, closed_at)
     VALUES ('t1', 'bybit-futures', 'ETH/USDT:USDT', '15m', 'long', 0,
      100, 101, 200, 202, 2, 1, '100', '101', '200', '202', '2', '1',
      'take-profit', datetime('now','-2 hours'), datetime('now','-1 hour'));`,
  );
  const lines = Array.from(
    { length: openedLines },
    (_, i) =>
      `[2026-09-07T00:4${i}:30.000Z] bybit-futures:ETH/USDT:USDT OPENED | capital=200.00 | open=1 | closed=0`,
  );
  writeFileSync(join(home, "logs", logName(name)), `${lines.join("\n")}\n`);
  return home;
}

function logName(home: string): string {
  return home.includes("paper")
    ? "champion-paper.out.log"
    : "champion-demo.out.log";
}

interface MonitorRun {
  readonly code: number;
}

function run(env: Record<string, string>): MonitorRun {
  try {
    execFileSync("sh", [SCRIPT], { env: { ...process.env, ...env } });
    const ok: MonitorRun = { code: 0 };
    return ok;
  } catch (err) {
    const e = err as { status?: number };
    const failed: MonitorRun = { code: e.status ?? 99 };
    return failed;
  }
}

describe("champion-soak-monitor.sh (clever-cabin-8rs)", () => {
  it("baselines silently, then alerts statefully on new CLAIM/fills/closes", () => {
    const root = mkdtempSync(join(tmpdir(), "champion-mon-"));
    const paperHome = setupHome(root, ".neuratrade-champion-paper", 1);
    const demoHome = setupHome(root, ".neuratrade-champion-demo", 1);
    const stateDir = join(root, "state");
    const logDir = join(root, "logs");
    mkdirSync(stateDir, { recursive: true });
    mkdirSync(logDir, { recursive: true });
    // No claimed.json yet on the first run.
    const baseEnv = {
      PAPER_HOME: paperHome,
      DEMO_HOME: demoHome,
      CLAIMED_FILE: join(root, "claimed.json"),
      STATE_DIR: stateDir,
      LOG_DIR: logDir,
      SKIP_PM2: "1",
      SKIP_DISK: "1",
    };

    // Run 1: baselines fills + 24h closes, no claim yet -> exit 0.
    expect(run(baseEnv).code).toBe(0);
    expect(existsSync(join(stateDir, "ladder-total-paper.txt"))).toBe(true);
    expect(existsSync(join(stateDir, "closed-24h-demo.txt"))).toBe(true);

    // Run 2: a fresh CLAIM appears -> exit 1, logged once.
    writeFileSync(
      join(root, "claimed.json"),
      JSON.stringify({ status: "CLAIMED", at: "2026-09-07T00:00:00Z" }),
    );
    expect(run(baseEnv).code).toBe(1);
    const logAfterClaim = readFileSync(
      join(logDir, "champion-soak-monitor.log"),
      "utf8",
    );
    expect(logAfterClaim).toContain("GOALS CLAIMED");

    // Run 3: unchanged -> exit 0 (stateful, no repeat alert).
    expect(run(baseEnv).code).toBe(0);

    // Run 4: new ladder fill (2nd DB row) in paper home -> exit 1.
    // The row also lands in the 24h close window, so both the fills
    // and the closes counters advance (the 09-21 metric: closed
    // round-trips from ladder_paper_trades, not log greps).
    const paperDb = join(paperHome, "data", "neuratrade.db");
    sqlite(
      paperDb,
      `INSERT INTO ladder_paper_trades (id, exchange, symbol, timeframe, side, rung_index,
        entry_price, exit_price, capital_before, capital_after, pnl, pnl_pct,
        entry_price_decimal, exit_price_decimal, capital_before_decimal, capital_after_decimal,
        pnl_decimal, pnl_pct_decimal, exit_reason, opened_at, closed_at)
       VALUES ('t2', 'bybit-futures', 'ETH/USDT:USDT', '15m', 'long', 1,
        100, 102, 202, 204, 2, 1, '100', '102', '202', '204', '2', '1',
        'take-profit', datetime('now','-30 minutes'), datetime('now','-10 minutes'));`,
    );
    expect(run(baseEnv).code).toBe(1);
    const logAfterFill = readFileSync(
      join(logDir, "champion-soak-monitor.log"),
      "utf8",
    );
    expect(logAfterFill).toContain("NEW LADDER FILLS");
    expect(logAfterFill).toContain("NEW LADDER CLOSES");

    // Run 5: unchanged again -> exit 0.
    expect(run(baseEnv).code).toBe(0);
  });

  it("warns (not fails) when isolated homes are absent", () => {
    const root = mkdtempSync(join(tmpdir(), "champion-mon-empty-"));
    const stateDir = join(root, "state");
    const logDir = join(root, "logs");
    mkdirSync(stateDir, { recursive: true });
    mkdirSync(logDir, { recursive: true });
    const { code } = run({
      PAPER_HOME: join(root, "no-paper"),
      DEMO_HOME: join(root, "no-demo"),
      CLAIMED_FILE: join(root, "claimed.json"),
      STATE_DIR: stateDir,
      LOG_DIR: logDir,
      SKIP_PM2: "1",
      SKIP_DISK: "1",
    });
    expect(code).toBe(0);
    expect(
      readFileSync(join(logDir, "champion-soak-monitor.log"), "utf8"),
    ).toContain("DB missing");
  });
});
