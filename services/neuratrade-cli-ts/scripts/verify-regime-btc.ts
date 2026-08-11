// Verification for bd NeuraTrade-9m8p: regime-filtered scaling quality vs
// baseline on real BTC/USDT 1h data. Fetches live Binance klines (needs network).
import {
  composerSweepCandidate,
  runBacktest,
  sweepComposerConfigs,
} from "../src/scalping/backtest.js";
import { defaultComposerConfig } from "../src/scalping/composer.js";
import type { CandleLike } from "../src/scalping/types.js";

async function fetchKlines(
  symbol: string,
  interval: string,
  limit: number,
): Promise<CandleLike[]> {
  const url = `https://api.binance.com/api/v3/klines?symbol=${symbol}&interval=${interval}&limit=${limit}`;
  const res = await fetch(url);
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  const rows = (await res.json()) as number[][];
  return rows.map((r) => ({
    open: parseFloat(String(r[1])),
    high: parseFloat(String(r[2])),
    low: parseFloat(String(r[3])),
    close: parseFloat(String(r[4])),
    volume: parseFloat(String(r[5])),
    timestamp: new Date(r[0]),
  }));
}

const candles = await fetchKlines("BTCUSDT", "1h", 1000);
console.log(
  `Fetched ${candles.length} 1h candles, ${candles[0].timestamp.toISOString()} .. ${candles[candles.length - 1].timestamp.toISOString()}`,
);

// Baseline: default composer config.
function evaluate(candlesSrc: readonly CandleLike[], label: string): void {
  const base = {
    symbol: "BTC/USDT",
    exchange: "binance",
    timeframe: "1h",
    candles: candlesSrc,
    initialCapital: 10_000,
    positionSizePct: 100,
    stopLossPct: 1,
    takeProfitPct: 2,
    feePct: 0.06,
    minConfidence: 0.35,
    slippageBps: 2,
    leverage: 1,
    isFutures: true,
    maxBarsInTrade: 12,
    htfCandles: [],
    recordEquityCurve: false,
  } as const;

  const baseline = runBacktest({ ...base, composerConfig: defaultComposerConfig });
  const line = `[${label}] baseline return=${baseline.totalReturnPct.toFixed(2)}% sharpe=${baseline.sharpeRatio.toFixed(2)} dd=${baseline.maxDrawdownPct.toFixed(2)}% trades=${baseline.totalTrades} win=${(baseline.winRate * 100).toFixed(1)}% bench=${baseline.benchmarkReturnPct.toFixed(2)}%`;
  console.log(line);

  // Config with the regime component disabled entirely (the "no regime" baseline).
  const noRegimeConfig = {
    ...defaultComposerConfig,
    weights: {
      ...defaultComposerConfig.weights,
      regime: 0,
      spread: 0.18,
      imbalance: 0.22,
      volatility: 0.13,
      trend: 0.18,
      liquidity: 0.09,
      rsi: 0.09,
      funding: 0,
      rsiPullback: 0,
      emaPullback: 0,
      connorsRsi2: 0,
    },
    enabled: { ...defaultComposerConfig.enabled, regime: false },
  };
  const noRegimeResult = runBacktest({
    ...base,
    composerConfig: noRegimeConfig,
  });
  console.log(
    `       -> no-regime return=${noRegimeResult.totalReturnPct.toFixed(2)}% sharpe=${noRegimeResult.sharpeRatio.toFixed(2)} dd=${noRegimeResult.maxDrawdownPct.toFixed(2)}% trades=${noRegimeResult.totalTrades} win=${(noRegimeResult.winRate * 100).toFixed(1)}%`,
  );

  const candidates = [
    composerSweepCandidate("regime-tight", {
      adxWeakTrend: 25,
      bollingerEntryMinPct: 0.4,
      bollingerEntryMaxPct: 0.6,
      weights: {
        regime: 0.25,
        trend: 0.14,
        volatility: 0.1,
        rsi: 0.07,
        spread: 0.18,
        imbalance: 0.22,
        liquidity: 0.09,
      },
    }),
    composerSweepCandidate("regime-atr-cap", {
      adxWeakTrend: 25,
      atrMaxPctOfPrice: 0.03,
      bollingerEntryMinPct: 0.35,
      bollingerEntryMaxPct: 0.65,
      weights: {
        regime: 0.3,
        trend: 0.14,
        volatility: 0.1,
        rsi: 0.07,
        spread: 0.15,
        imbalance: 0.15,
        liquidity: 0.09,
      },
    }),
  ];

  const ranked = sweepComposerConfigs(base, candidates, defaultComposerConfig);
  for (const r of ranked) {
    console.log(
      `       -> ${r.name} return=${r.totalReturnPct.toFixed(2)}% sharpe=${r.sharpeRatio.toFixed(2)} dd=${r.maxDrawdownPct.toFixed(2)}% trades=${r.totalTrades} win=${(r.winRate * 100).toFixed(1)}%`,
    );
  }
}

// Evaluate three disjoint 500h windows across the fetched history.
for (const idx of [0, 1, 2]) {
  const start = idx * 500;
  const window = candles.slice(start, start + 500);
  if (window.length < 500) break;
  console.log(`\nWindow ${idx}: ${window[0].timestamp.toISOString()} .. ${window[window.length - 1].timestamp.toISOString()}`);
  evaluate(window, `w${idx}`);
}
console.log("");
