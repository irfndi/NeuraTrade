// pm2 process definition for the Bitget PAPTRADING demo soak.
// Run: pm2 start ecosystem.demo-soak.config.cjs
// The soak runs the grid universe survivors (grid-whitelist.json) against the
// Bitget demo matching engine (PAPTRADING=1) at a 15-minute cadence, forever,
// persisting fills to ~/.neuratrade/data/neuratrade.db via NEURATRADE_HOME.
// The whitelist is produced by `scalp grid-universe-scan --output grid-whitelist.json`.
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
    let key = trimmed.slice(0, eq).trim();
    let value = trimmed.slice(eq + 1).trim();
    if (value.startsWith('"') && value.endsWith('"')) value = value.slice(1, -1);
    env[key] = value;
  }
  return env;
}

const rootEnv = loadDotEnv(path.join(__dirname, "..", "..", ".env"));
const cliTsDir = __dirname;
const neuratradeHome =
  (rootEnv.NEURATRADE_HOME || `${process.env.HOME}/.neuratrade`).replace(
    "${HOME}",
    process.env.HOME,
  );

module.exports = {
  apps: [
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
