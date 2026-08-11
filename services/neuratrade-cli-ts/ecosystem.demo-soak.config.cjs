// pm2 process definition for the Bitget PAPTRADING demo soak.
// Run: pm2 start ecosystem.demo-soak.config.cjs
// - neuratrade-universe-watch: continuously re-scans the universe and upserts
//   survivors into the DB watchlist (self-maintaining symbol selection).
// - neuratrade-demo-soak: runs the grid universe survivors (DB-backed watchlist
//   / grid-whitelist.json) against the Bitget demo matching engine (PAPTRADING=1)
//   at a 15-minute cadence, forever, persisting fills to ~/.neuratrade/data/neuratrade.db.
//   The whitelist is produced by `scalp grid-universe-scan --output grid-whitelist.json`.
// Log rotation: every app uses pm2's built-in rotation (max_size 50M, retain 5) on
// its out_file/error_file — prevents unbounded log growth (a crash era wrote ~400KB/hr).
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

// Promoted readiness candidate per docs/superpowers/specs/2026-08-06-readiness-gate-status.md
// and the readiness fingerprint (src/scalping/grid-candidate.ts VALIDATED_BTC_GRID_CANDIDATE):
// step 1%, grids 1.5, target ratio 3, pause 24, chop-gate ADX 28, trend filter 0,
// fee 0.02% maker, slippage 1bp, leverage 1, position fraction 0.5 (--max-position-size-pct 50).
// The args below MUST match the manifest exactly — the provenance gate rejects
// fills whose fingerprint differs.
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
        "--target-ratio",
        "3",
        "--chop-gate-adx",
        "28",
        "--no-watchlist",
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
      max_size: "50M",
      retain: 5,
      merge_logs: true,
      time: true,
    },
    // STOPPED BY DEFAULT — start deliberately (pm2 start ecosystem.demo-soak.config.cjs
    // --only neuratrade-btc-candidate-5m). The 5m grid configs are exploratory, not
    // gate-validated: same manifest as neuratrade-btc-candidate but at 5m cadence
    // (--interval 300, --timeframe 5m). Logs to btc-candidate-5m.* to avoid clobbering
    // the 15m candidate logs.
    {
      // $10 challenge starter (STOPPED BY DEFAULT): the user's $10 -> $1M goal.
      // Orderability: with contract specs wired from Bitget (live path), the
      // effective floor is max(minTradeUSDT 5, minQty x price). BTC min qty
      // 0.0001 at ~$64,795 = ~$6.48 notional, which exceeds 50% of $10 at 1x
      // (64.8% > 50% cap -> RISK BLOCKED). At 2x leverage the required margin
      // is $3.24 = 32% of $10, within the 50% cap, so --leverage 2 makes the
      // BTC min orderable position fit the account.
      name: "neuratrade-challenge-10",
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
        "2",
        "--capital",
        "10",
        "--min-capital",
        "10",
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
        "--target-ratio",
        "3",
        "--chop-gate-adx",
        "28",
        "--no-watchlist",
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
      out_file: path.join(neuratradeHome, "logs", "challenge-10.out.log"),
      error_file: path.join(neuratradeHome, "logs", "challenge-10.err.log"),
      max_size: "50M",
      retain: 5,
      merge_logs: true,
      time: true,
    },
    {
      name: "neuratrade-sol-candidate",
      script: "bun",
      args: [
        "run",
        "index.ts",
        "scalp",
        "paper-trade",
        "--exchange",
        "bitget-futures",
        "--symbol",
        "SOL/USDT:USDT",
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
        "1.25",
        "--grid-max-grids",
        "2",
        "--grid-pause-after-loss-bars",
        "36",
        "--target-ratio",
        "4",
        "--chop-gate-adx",
        "26",
        "--no-watchlist",
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
      out_file: path.join(neuratradeHome, "logs", "sol-candidate.out.log"),
      error_file: path.join(neuratradeHome, "logs", "sol-candidate.err.log"),
      max_size: "50M",
      retain: 5,
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
        // Bybit mainnet 5m candles (fetch-flow-mainnet backfill, ~12 months
        // for ~40 symbols) resampled to the scan timeframe — the ONLY
        // universe with gate-eligible grid configs. Testnet wicks are ~3.3x
        // wider than mainnet and contaminated every downstream metric
        // (verified 2026-08-11: ETH +17.85% testnet walk-forward edge but
        // -10.3% over 12 mainnet months), so the watch evaluates on
        // db-mainnet data with conservative (modeled) fills instead.
        "--exchange",
        "bybit-futures",
        "--timeframe",
        "15m",
        "--data-source",
        "db-mainnet",
        "--min-candles",
        "500",
        "--min-fill-frequency-pct",
        "10",
        // The universe soak trades a $50 demo account: scale the selection
        // target to the real capital (target = clamp(5, 50*50/1000, 50) =
        // 5 fills/day) instead of the $1000 default's 50/day ceiling.
        "--account-capital",
        "50",
        "--market",
        "--tier",
        "fast",
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
      max_size: "50M",
      retain: 5,
      merge_logs: true,
      time: true,
    },
    {
      name: "neuratrade-bybit-soak",
      script: "bun",
      args: [
        "run",
        "index.ts",
        "scalp",
        "paper-trade",
        // Bybit testnet soak: the funnel's selected pool (ETH/USDT:USDT,
        // ~13 fills/day projected) trades here against the $50 testnet AI
        // sub-account. First-fill monitor is armed and fires on the first
        // grid_paper_trades row.
        "--exchange",
        "bybit-futures",
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
      out_file: path.join(neuratradeHome, "logs", "bybit-soak.out.log"),
      error_file: path.join(neuratradeHome, "logs", "bybit-soak.err.log"),
      max_size: "50M",
      retain: 5,
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
      max_size: "50M",
      retain: 5,
      merge_logs: true,
      time: true,
    },
    {
      // Flow Ignition live recorder: streams Bybit mainnet public trades
      // (publicTrade.*) into 1-minute flow_ofi_1m buckets and liquidation
      // prints into flow_liquidations. Interval-less (runs until the WS
      // fails after reconnect retries; pm2 autorestart brings it back).
      name: "neuratrade-flow-recorder",
      script: "bun",
      args: ["run", "index.ts", "scalp", "flow-record"],
      cwd: cliTsDir,
      env: {
        ...rootEnv,
        NEURATRADE_HOME: neuratradeHome,
        NODE_ENV: "production",
      },
      autorestart: true,
      max_restarts: 10,
      restart_delay: 30_000,
      out_file: path.join(neuratradeHome, "logs", "flow-recorder.out.log"),
      error_file: path.join(neuratradeHome, "logs", "flow-recorder.err.log"),
      max_size: "50M",
      retain: 5,
      merge_logs: true,
      time: true,
    },
  ],
};
