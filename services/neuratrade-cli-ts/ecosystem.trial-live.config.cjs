/**
 * Trial-live MAINNET sleeve (PM2 app, default DISABLED).
 *
 * Parallel with paper/demo, never replacing them: same champion knobs, own
 * NEURATRADE_HOME (~/.neuratrade-champion-trial-live), own PM2 app name, own
 * ledger DB. Mainnet execution ONLY when the operator starts this app
 * explicitly (`pm2 start ... --only neuratrade-champion-trial-live`).
 *
 * HARD CAPS (every flag below is wired today — verified against scalp.ts):
 * - leverage 1 + NEURATRADE_MAX_LADDER_LEVERAGE=1 (account-scaled cap
 *   cannot raise above 1x; dynamic path stays at 1x by construction)
 * - --max-position-size-pct 10 (per-symbol margin cap, wired)
 * - --capital 100 (trial stake; per-symbol partition ~25 USDT)
 * - kill-switch + circuit breaker engaged (fail-closed, same as demo)
 * NOTE: --max-notional-pct is NOT a CLI flag (scalp.ts hardcodes 100) —
 * the 10% position cap at 1x IS the notional cap (10% of capital).
 *
 * BLOCKED until ALL hold (see clever-cabin-h1a):
 * 1. risk_kill_switch disengaged (box DB reads 1|1|LIVE POSITION MISMATCH)
 * 2. Exposed-chat creds rotated (clever-cabin-ztm) — OAuth sub-account
 *    588670783 is the rotation target, never the old keys
 * 3. Owner typed CONFIRM on the [MAINNET] card (asset/amount/direction/cost)
 * 4. CODE: validateLiveSandboxMode (scalp.ts:3248) rejects --live with
 *    BYBIT_USE_TESTNET=false by design — trial-live needs an explicit
 *    mainnet allow-flag + owner-CONFIRM gate wired before this app can
 *    start. Until then operator-start fails closed on validation.
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
// TRIAL SCOPE: LINK only (lowest venue floor — smallest-amount trial).
// BTC at $100/4 = ~$25/partition can never pass the ~$115 venue floor
// (already guard-doomed at 150%+ on the $50 demo partition); ETH/SOL stay
// demo/paper-only until trial proves fills. Single-symbol watchlist keeps
// the full $100 behind one orderable rung.
const whitelist = path.join(__dirname, "trial-live-whitelist.json");
const trialHome = path.join(
  process.env.HOME || "/root",
  ".neuratrade-champion-trial-live",
);
module.exports = {
  apps: [
    {
      name: "neuratrade-champion-trial-live",
      script: "bun",
      args: [
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
        // Same cross accounting as demo so paper-vs-trial has no fee drift.
        "--live-entry-cross-bps",
        "15",
        // TRIAL CAPS: leverage 1, 10% position (= 10% notional at 1x).
        "--leverage",
        "1",
        "--capital",
        "100",
        "--min-capital",
        "20",
        "--max-position-size-pct",
        "10",
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
        // Mainnet signal+execution. Requires BYBIT_USE_TESTNET=false AND
        // explicit operator start; validateLiveSandboxMode rejects --live
        // without a non-sandbox account by design.
        "--signal-feed",
        "mainnet",
        "--iterations",
        "0",
        "--interval",
        "900",
        "--live",
      ],
      cwd: cliTsDir,
      // Disabled by default: never autostart with the soak file. Operator
      // starts explicitly after the three unblock conditions above hold.
      autorestart: false,
      max_restarts: 3,
      restart_delay: 60_000,
      out_file: path.join(trialHome, "logs", "champion-trial-live.out.log"),
      error_file: path.join(trialHome, "logs", "champion-trial-live.err.log"),
      max_size: "50M",
      retain: 5,
      merge_logs: true,
      time: true,
      env: {
        ...rootEnv,
        NEURATRADE_HOME: trialHome,
        NODE_ENV: "production",
        // MAINNET: testnet flag OFF. BybitConfigLive resolves useTestnet
        // from this; false + --live routes to api2.bybit.com via the OAuth
        // AI sub-account creds (BYBIT_API_KEY/SECRET from sub-account
        // 588670783, never the old chat-exposed keys).
        BYBIT_USE_TESTNET: "false",
        BITGET_USE_SANDBOX: "false",
        // Pin the account-scaled leverage ceiling at 1x on this path.
        NEURATRADE_MAX_LADDER_LEVERAGE: "1",
      },
    },
  ],
};
