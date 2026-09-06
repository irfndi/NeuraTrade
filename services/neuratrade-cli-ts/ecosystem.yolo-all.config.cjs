// ALL BEST FASTER — 6 symbols parallel, 1m, 0.3%/2/0, 10x, 100%, 60s
// Each symbol has own 50 capital (isolated). ~120-180 fills/day total.
// BYBIT_USE_TESTNET=true — demo only. ponytail: no shared risk, 6x notional.
const path = require("node:path"),
  fs = require("node:fs");
function loadEnv(f) {
  const e = {};
  if (!fs.existsSync(f)) return e;
  for (const l of fs.readFileSync(f, "utf8").split("\n")) {
    const t = l.trim();
    if (!t || t.startsWith("#")) continue;
    const i = t.indexOf("=");
    if (i <= 0) continue;
    let k = t.slice(0, i).trim(),
      v = t.slice(i + 1).trim();
    if (v.startsWith('"') && v.endsWith('"')) v = v.slice(1, -1);
    e[k] = v;
  }
  return e;
}
const rootEnv = loadEnv(path.join(__dirname, "../..", ".env"));
const home = (
  rootEnv.NEURATRADE_HOME || `${process.env.HOME}/.neuratrade`
).replace("${HOME}", process.env.HOME);
const cli = __dirname;
function argsFor(symbol) {
  return [
    "run",
    "index.ts",
    "scalp",
    "paper-trade",
    "--exchange",
    "bybit-futures",
    "--symbol",
    symbol,
    "--timeframe",
    "1m",
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
    "10",
    "--capital",
    "50",
    "--min-capital",
    "50",
    "--max-position-size-pct",
    "100",
    "--max-drawdown-pct",
    "20",
    "--max-daily-loss-pct",
    "10",
    "--grid-step-pct",
    "0.3",
    "--grid-max-grids",
    "2",
    "--grid-pause-after-loss-bars",
    "0",
    "--target-ratio",
    "1",
    "--chop-gate-adx",
    "0",
    "--no-watchlist",
    "--iterations",
    "0",
    "--interval",
    "60",
  ];
}
const symbols = [
  "BTC/USDT:USDT",
  "ETH/USDT:USDT",
  "SOL/USDT:USDT",
  "LINK/USDT:USDT",
  "NEAR/USDT:USDT",
  "APT/USDT:USDT",
];
module.exports = {
  apps: symbols.map((s) => ({
    name: `yolo-${s.split("/")[0].toLowerCase()}`,
    script: "bun",
    args: argsFor(s),
    cwd: cli,
    env: {
      ...rootEnv,
      NEURATRADE_HOME: home,
      NODE_ENV: "production",
      BYBIT_USE_TESTNET: "true",
    },
    autorestart: true,
    max_restarts: 10,
    restart_delay: 5_000,
    out_file: path.join(
      home,
      "logs",
      `yolo-${s.split("/")[0].toLowerCase()}.out.log`,
    ),
    error_file: path.join(
      home,
      "logs",
      `yolo-${s.split("/")[0].toLowerCase()}.err.log`,
    ),
    max_size: "50M",
    retain: 5,
    merge_logs: true,
    time: true,
  })),
};
