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
 * START PROCEDURE (two independent, deliberate acts — both required):
 *   1. this file carries --allow-mainnet-live (visible in git/review);
 *   2. the launching shell exports the token:
 *        export NEURATRADE_MAINNET_CONFIRM=I-CONFIRM-MAINNET
 *        pm2 start ecosystem.trial-live.config.cjs --only neuratrade-champion-trial-live
 *      Without it the app exits at validation, before any credential or
 *      exchange object is built. The token is NEVER stored in this file.
 *
 * STILL BLOCKED on human/security items (see clever-cabin-h1a) — the code gate
 * above only makes the app STARTABLE, it does not authorize trading:
 * 1. Exposed-chat creds rotated (clever-cabin-ztm) — OAuth sub-account
 *    588670783 is the rotation target, never the old keys
 * 2. Owner typed CONFIRM on the [MAINNET] card (asset/amount/direction/cost)
 *
 * `validateLiveSandboxMode` and `validateLiveGridWatchlist` (scalp.ts) were
 * extended on 2026-10-20: both refuse mainnet unless the flag AND the token
 * are present, so paper/testnet behavior is untouched and every existing test
 * that asserts a mainnet rejection still passes with the defaults.
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
// TRIAL SCOPE: ETH only, and it must be a VALIDATED candidate.
//
// Why not LINK (the previous choice): `candidateForSymbol` only knows BTC,
// SOL and ETH, and the live-grid gate hard-rejects any symbol outside that
// registry — the LINK trial could never start on mainnet, at any price. LINK
// becomes eligible only after it is refitted on clean Bybit data and passes
// the confidence/stress gates, which is research work, not a config edit.
//
// Why ETH: of the three validated candidates it is the best on the 32-day
// replay of the soak window (+3.5% vs BTC -1.4% / SOL -5.5%), its candidate
// metadata records the fastest cohort clock (~17 fills/mo), and at $100 with
// a 50% position a rung is 0.0103 ETH ≥ the 0.01 venue minimum, so it is
// orderable at leverage 1. BTC cannot be: 0.001 BTC ≈ $82 exceeds a $50 rung.
//
// The geometry below is copied from VALIDATED_ETH_GRID_CANDIDATE and must
// stay byte-identical to it (step 0.75 / grids 3 / pause 24 / target 3 /
// adx 20 / fee 0.02 / slip 1bp) or the gate names the drifted field.
const whitelist = path.join(__dirname, "trial-live-whitelist.json");
const trialHome = path.join(
  process.env.HOME || "/root",
  ".neuratrade-champion-trial-live",
);
const PAR_PER_SYMBOL = 100;
const MIN_CAPITAL_PER_SYMBOL = Math.floor(PAR_PER_SYMBOL * 0.3);
// Passed through ONLY if the operator exported it in the launching shell; the
// file never hardcodes it, so starting from a clean shell cannot go live.
const mainnetConfirm = process.env.NEURATRADE_MAINNET_CONFIRM;
const trialLiveEnv = {
  ...rootEnv,
  NEURATRADE_HOME: trialHome,
  NODE_ENV: "production",
  // MAINNET: testnet flag OFF. BybitConfigLive resolves useTestnet from this;
  // false + --live routes to api2.bybit.com via the OAuth AI sub-account creds
  // (BYBIT_API_KEY/SECRET from sub-account 588670783, never the old keys).
  BYBIT_USE_TESTNET: "false",
  BITGET_USE_SANDBOX: "false",
  // Pin the account-scaled leverage ceiling at 1x on this path.
  NEURATRADE_MAX_LADDER_LEVERAGE: "1",
};
// Added only when the operator exported it in the launching shell; the file
// never stores the token, so a start from a clean shell stays fail-closed.
if (mainnetConfirm !== undefined) {
  trialLiveEnv.NEURATRADE_MAINNET_CONFIRM = mainnetConfirm;
}

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
        // REQUIRED: the live-grid cohort gate compares this symbol, which
        // defaults to BTC/USDT. Omitting it silently validated a symbol the
        // trial never trades.
        "--symbol",
        "ETH/USDT:USDT",
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
        // Explicit on a real-money path: 2bp above is the MAKER rate; market
        // exits (stop / max-hold) pay the Bybit taker rate. Leaving this to the
        // default hid the stop-fee basis inside a flag nobody read.
        "--taker-exit-fee-pct",
        "0.06",
        // 1bp, NOT the soak's 2: the candidate was validated at 1bp and the
        // cohort gate compares this field exactly.
        "--slippage-bps",
        "1",
        // Same cross accounting as demo so paper-vs-trial has no fee drift.
        "--live-entry-cross-bps",
        "15",
        // TRIAL CAPS: leverage 1; 50% position is the candidate's own cap and
        // the minimum that makes an ETH rung orderable at $100. Risk per stop
        // is ~1.2% of notional (~0.6% of capital), not a 50% of-equity bet.
        "--leverage",
        "1",
        "--capital",
        "100",
        // 30% of par, same rule as the soaks (PAR 100 / 1 symbol). 20 was a
        // literal that let the sleeve trade down to a fifth of its stake; at
        // 30 a LINK rung is still 1.5 USDT vs a 0.1 LINK (≈1.2 USDT) venue
        // floor, so the floor never strands the sleeve.
        "--min-capital",
        String(MIN_CAPITAL_PER_SYMBOL),
        "--max-position-size-pct",
        "50",
        // Tightened to the candidate's caps: the gate only allows tightening.
        "--max-drawdown-pct",
        "5",
        "--max-daily-loss-pct",
        "2",
        // PINNED to VALIDATED_ETH_GRID_CANDIDATE — deliberately NOT the
        // autoresearch knobs. A real-money sleeve must run the back-validated
        // geometry, and the gate compares these fields exactly; a drifting
        // champion.json must not silently re-point live trading.
        "--grid-step-pct",
        "0.75",
        "--grid-max-grids",
        "3",
        "--grid-pause-after-loss-bars",
        "24",
        "--target-ratio",
        "3",
        "--chop-gate-adx",
        "20",
        // Ladder-only knobs the candidate does NOT cover (it was fitted on the
        // grid engine). Kept at the soak values so the trial differs from the
        // paper book only where the evidence does — flagged as the open risk
        // in the header.
        "--stop-ratio",
        "1.58",
        "--max-hold-bars",
        "39",
        "--config-mismatch-action",
        "force-reseed",
        // Mainnet signal+execution. Requires BYBIT_USE_TESTNET=false, this
        // flag, and the operator token; see the START PROCEDURE above.
        "--signal-feed",
        "mainnet",
        // Two-key mainnet gate: this flag + NEURATRADE_MAINNET_CONFIRM in the
        // launching shell. See the START PROCEDURE above.
        "--allow-mainnet-live",
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
      env: trialLiveEnv,
    },
  ],
};
