// pm2 process definition for the Bitget PAPTRADING demo soak.
// Run: pm2 start ecosystem.demo-soak.config.cjs
// - neuratrade-universe-watch: continuously re-scans the universe and upserts
//   survivors into the DB watchlist (self-maintaining symbol selection).
// - neuratrade-demo-soak: runs the grid universe survivors (DB-backed watchlist
//   / grid-whitelist.json) against the Bitget demo matching engine (PAPTRADING=1)
//   at a 15-minute cadence, forever, persisting fills to ~/.neuratrade/data/neuratrade.db.
//   The whitelist is produced by `scalp grid-universe-scan --output grid-whitelist.json`.
const fs = require("node:fs");
const path = require("node:path");

function loadDotEnv(file) {
  const env = {};
  if (!fs.existsSync(file)) return env;
  for (const line of fs.readFileSync(file, "utf8").split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;
    const eq = trimmed.indexOf("=");
    if (eq <= 0) continue;
    const key = trimmed.slice(0, eq).trim();
    let value = trimmed.slice(eq + 1).trim();
    if (value.startsWith('"') && value.endsWith('"'))
      value = value.slice(1, -1);
    env[key] = value;
  }
  return env;
}

const rootEnv = loadDotEnv(path.join(__dirname, "..", "..", ".env"));

// Promoted real-money candidate per docs/superpowers/specs/2026-08-03-gate-scored-grid-search.md:
// step 1%, grids 1.5, pause 24, chop-gate ADX 24, trend filter 0, maker fee 0.02%/side,
// slippage 1bp, leverage 1, position fraction 0.5 (--max-position-size-pct 50).
// STOPPED BY DEFAULT (pm2 start <id> only): the demo account has ~$50 USDT and is used
// by neuratrade-demo-soak; the readiness cohort needs a DEDICATED funded demo account
// (bd: BTC candidate soak issue). Start with:
//   pm2 start ecosystem.demo-soak.config.cjs --only neuratrade-btc-candidate
// and raise --capital after funding (defaults here match the ~$50 demo balance).
const cliTsDir = __dirname;
const neuratradeHome = (
  rootEnv.NEURATRADE_HOME || `${process.env.HOME}/.neuratrade`
).replace("${HOME}", process.env.HOME);

module.exports = {
  apps: [
    {
      name: "neuratrade-btc-candidate",
      script: "bun",
      args: [
        "run",
        "index.ts",
        "scalp",
        "paper-trade",
        "--exchange",
        "bitget-futures",
        "--symbol",
        "BTC/USDT:USDT",
        "--timeframe",
        "15m",
        "--futures",
        "--live",
        "--strategy-type",
        "grid",
        "--trend-filter-period",
        "0",
        "--fee",
        "0.02",
        "--slippage-bps",
        "1",
        "--leverage",
        "1",
        "--capital",
        "50",
        "--min-capital",
        "50",
        "--max-position-size-pct",
        "50",
        "--max-drawdown-pct",
        "5",
        "--max-daily-loss-pct",
        "2",
        "--grid-step-pct",
        "1",
        "--grid-max-grids",
        "1.5",
        "--grid-pause-after-loss-bars",
        "24",
        "--chop-gate-adx",
        "24",
        "--iterations",
        "0",
        "--interval",
        "900",
      ],
      cwd: cliTsDir,
      env: {
        ...rootEnv,
        NEURATRADE_HOME: neuratradeHome,
        NODE_ENV: "production",
      },
      autorestart: true,
      max_restarts: 10,
      restart_delay: 30_000,
      out_file: path.join(neuratradeHome, "logs", "btc-candidate.out.log"),
      error_file: path.join(neuratradeHome, "logs", "btc-candidate.err.log"),
      merge_logs: true,
      time: true,
    },
    {
      name: "neuratrade-universe-watch",
      script: "bun",
      args: [
        "run",
        "index.ts",
        "scalp",
        "grid-universe-scan",
        "--exchange",
        "bitget-futures",
        "--timeframe",
        "15m",
        "--min-candles",
        "500",
        "--min-fill-frequency-pct",
        "10",
        "--watch",
        "--interval",
        "21600",
      ],
      cwd: cliTsDir,
      env: {
        ...rootEnv,
        NEURATRADE_HOME: neuratradeHome,
        NODE_ENV: "production",
      },
      autorestart: true,
      max_restarts: 10,
      restart_delay: 30_000,
      out_file: path.join(neuratradeHome, "logs", "universe-watch.out.log"),
      error_file: path.join(neuratradeHome, "logs", "universe-watch.err.log"),
      merge_logs: true,
      time: true,
    },
    {
      name: "neuratrade-demo-soak",
      script: "bun",
      args: [
        "run",
        "index.ts",
        "scalp",
        "paper-trade",
        "--exchange",
        "bitget-futures",
        "--timeframe",
        "15m",
        "--futures",
        "--live",
        "--strategy-type",
        "grid",
        "--trend-filter-period",
        "0",
        "--fee",
        "0.06",
        "--slippage-bps",
        "2",
        "--leverage",
        "1",
        "--capital",
        "50",
        "--min-capital",
        "50",
        "--max-position-size-pct",
        "50",
        "--max-drawdown-pct",
        "5",
        "--max-daily-loss-pct",
        "2",
        "--watchlist",
        "grid-whitelist.json",
        "--iterations",
        "0",
        "--interval",
        "900",
      ],
      cwd: cliTsDir,
      env: {
        ...rootEnv,
        NEURATRADE_HOME: neuratradeHome,
        NODE_ENV: "production",
      },
      autorestart: true,
      max_restarts: 10,
      restart_delay: 30_000,
      out_file: path.join(neuratradeHome, "logs", "demo-soak.out.log"),
      error_file: path.join(neuratradeHome, "logs", "demo-soak.err.log"),
      merge_logs: true,
      time: true,
    },
  ],
};
