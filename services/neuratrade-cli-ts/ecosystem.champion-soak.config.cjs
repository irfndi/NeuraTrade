/**
 * Champion paper-soak (simulated fills) + Bybit testnet demo soak.
 *
 * SINGLE FROZEN CONFIG PATH: grid geometry + overlays come from
 * autoresearch/results/champion-soak.json (loaded below into --grid-* /
 * --target-ratio / --stop-ratio / --max-hold-bars CLI knobs). The checked-in
 * champion-whitelist.json may still carry older gate-scored values; with
 * --config-mismatch-action force-reseed the frozen CLI knobs WIN per row
 * (see resolveLadderGridSettings), so the soak always validates the
 * advertised baseline. With the default `hold` action a diverged row
 * hard-fails instead of trading stale config.
 *
 * WHAT DEMO PROVES: --signal-feed testnet keeps signals AND fills on the
 * Bybit testnet feed, so a forward paper-vs-demo comparison runs on the SAME
 * feed. The demo proves testnet EXECUTION (order routing, fills, risk
 * guards) — it does NOT prove a mainnet edge (testnet/mainnet prices can
 * diverge). Signal feed (--signal-feed / BYBIT_SIGNAL_FEED) is independent
 * of the execution venue (--live + BYBIT_USE_TESTNET); both are logged at
 * startup as signalFeed=... + executionEnv=....
 *
 * Paper uses a SEPARATE NEURATRADE_HOME so the live kill-switch / ghost
 * positions cannot block or contaminate it.
 *
 * Demo uses --live + BYBIT_USE_TESTNET (fake money on Bybit testnet).
 * Live mainnet is intentionally NOT in this file.
 *
 *   pm2 start ecosystem.champion-soak.config.cjs
 *   pm2 start ecosystem.champion-soak.config.cjs --only neuratrade-champion-paper
 *   pm2 start ecosystem.champion-soak.config.cjs --only neuratrade-champion-demo
 */
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
    if (
      (value.startsWith('"') && value.endsWith('"')) ||
      (value.startsWith("'") && value.endsWith("'"))
    ) {
      value = value.slice(1, -1);
    }
    env[key] = value;
  }
  return env;
}

function loadChampionKnobs() {
  // Freeze the soak baseline so overnight autoresearch can climb champion.json
  // without changing what paper/demo are validating.
  const soak = path.join(
    __dirname,
    "autoresearch",
    "results",
    "champion-soak.json",
  );
  const fallback = path.join(
    __dirname,
    "autoresearch",
    "results",
    "champion.json",
  );
  const p = fs.existsSync(soak) ? soak : fallback;
  const raw = JSON.parse(fs.readFileSync(p, "utf8"));
  return raw.knobs;
}

const rootEnv = loadDotEnv(path.join(__dirname, "..", "..", ".env"));
const knobs = loadChampionKnobs();
const cliTsDir = __dirname;
const whitelist = path.join(
  __dirname,
  "autoresearch",
  "results",
  "champion-whitelist.json",
);

const paperHome = path.join(
  process.env.HOME || "/root",
  ".neuratrade-champion-paper",
);
const demoHome = path.join(
  process.env.HOME || "/root",
  ".neuratrade-champion-demo",
);

function championArgs(extra) {
  return [
    "run",
    "index.ts",
    "scalp",
    "paper-trade",
    "--exchange",
    "bybit-futures",
    "--timeframe",
    "15m",
    "--futures",
    "--strategy-type",
    "grid",
    "--watchlist",
    whitelist,
    "--trend-filter-period",
    String(knobs.trendFilterPeriod ?? 0),
    "--fee",
    "0.02",
    "--slippage-bps",
    "2",
    // Market-type demo entries (testnet-only opt-in): thin books leave even
    // crossed limits unfilled (clever-cabin-85m; 00:07-01:09Z qty=0 rollbacks),
    // so demo places market with venue price as reference (Bitget 40020;
    // Bybit ignores it). Shared placement is safe: live path runs only when
    // isLive === true, so paper never reaches useMarket and stays simulated.
    "--live-entry-cross-bps",
    "15",
    "--demo-live-market-entries",
    "--leverage",
    "1",
    "--capital",
    "200",
    // 30, not 50: per-symbol capital is 200/4=50, so a min-capital equal to
    // par is a one-loss ratchet deadlock (clever-cabin-qgc) — the floor
    // needs headroom below par to survive normal drawdown.
    "--min-capital",
    "30",
    "--max-position-size-pct",
    "50",
    "--max-drawdown-pct",
    "15",
    "--max-daily-loss-pct",
    "5",
    "--grid-step-pct",
    String(knobs.gridStepPct),
    "--grid-max-grids",
    String(knobs.gridMaxGrids),
    "--grid-pause-after-loss-bars",
    String(knobs.gridPauseAfterLossBars),
    "--target-ratio",
    String(knobs.targetRatio),
    "--stop-ratio",
    String(knobs.stopRatio),
    "--max-hold-bars",
    String(knobs.maxHoldBars),
    "--chop-gate-adx",
    String(knobs.chopGateAdxThreshold ?? 0),
    "--config-mismatch-action",
    "force-reseed",
    "--signal-feed",
    "testnet",
    "--iterations",
    "0",
    "--interval",
    "900",
    ...extra,
  ];
}

module.exports = {
  apps: [
    {
      name: "neuratrade-champion-paper",
      script: "bun",
      args: championArgs([]), // no --live => simulated paper
      cwd: cliTsDir,
      env: {
        ...rootEnv,
        NEURATRADE_HOME: paperHome,
        NODE_ENV: "production",
        // Ensure we do not accidentally inherit live mainnet trading flags.
        BYBIT_USE_TESTNET: "false",
        BITGET_USE_SANDBOX: "false",
      },
      autorestart: true,
      max_restarts: 20,
      restart_delay: 15_000,
      out_file: path.join(paperHome, "logs", "champion-paper.out.log"),
      error_file: path.join(paperHome, "logs", "champion-paper.err.log"),
      max_size: "50M",
      retain: 5,
      merge_logs: true,
      time: true,
    },
    {
      name: "neuratrade-champion-demo",
      script: "bun",
      args: championArgs(["--live"]), // Bybit testnet matching engine
      cwd: cliTsDir,
      env: {
        ...rootEnv,
        NEURATRADE_HOME: demoHome,
        NODE_ENV: "production",
        BYBIT_USE_TESTNET: "true",
      },
      autorestart: true,
      max_restarts: 20,
      restart_delay: 30_000,
      out_file: path.join(demoHome, "logs", "champion-demo.out.log"),
      error_file: path.join(demoHome, "logs", "champion-demo.err.log"),
      max_size: "50M",
      retain: 5,
      merge_logs: true,
      time: true,
    },
    {
      // Keep soak candles fresh. Without this, ladder holds forever on
      // "no new candle" once the one-shot seed ages past the tip.
      name: "neuratrade-champion-candle-sync",
      script: "bash",
      args: ["scripts/sync-champion-soak-candles.sh"],
      cwd: cliTsDir,
      cron_restart: "*/15 * * * *",
      autorestart: false,
      out_file: path.join(paperHome, "logs", "champion-candle-sync.out.log"),
      error_file: path.join(paperHome, "logs", "champion-candle-sync.err.log"),
      merge_logs: true,
      time: true,
    },
  ],
};
