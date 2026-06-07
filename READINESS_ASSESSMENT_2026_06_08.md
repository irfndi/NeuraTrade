# NeuraTrade Readiness Assessment

**Date**: 2026-06-08
**Assessment Type**: Multi-ticker 5-year backtest + paper trading analysis
**Verdict**: **NOT READY for real money**

---

## Executive Summary

This assessment evaluated NeuraTrade's scalping strategy across multiple time windows (30d, 90d, 180d, 5yr) using real Binance historical data. The strategy shows **short-term profitability** (+138 USDT in 30 days) but **long-term unprofitability** (-15.08 USDT over 5 years). Paper trading shows mixed results across 4 strategies.

**Key Finding**: The scalping strategy is NOT robust. It works in some 30-day windows but fails consistently over longer periods (90d, 180d, 5yr). This is a critical blocker for real-money deployment.

---

## Backtest Results (Real Historical Data)

### Data Source Disclosure

| Symbol | Candles | Time Range | Coverage | Status |
|--------|---------|------------|----------|--------|
| BTC/USDT | 1,054,295 | 2021-06-01 to 2026-06-07 (~5 years) | Full | ✅ Complete |
| ETH/USDT | 1,054,295 | 2021-06-01 to 2026-06-07 (~5 years) | Full | ✅ Complete |
| SOL/USDT | 309,655 | 2021-06-08 to 2026-06-07 (~5 years) | Sparse (~30% density) | ⚠️ Sparse |
| BNB/USDT | 2,900 | 2026-05-02 to 2026-06-07 (~1 month) | Minimal | ⚠️ Severely limited |

**Note**: SOL and BNB have incomplete coverage. SOL has sparse data (30% density of BTC/ETH). BNB has only ~1 month of data. Multi-ticker backtests primarily reflect BTC and ETH performance.

### Window Analysis

| Window | Signals | Trades | Win Rate | PnL (USDT) | Return | Sharpe | Max DD |
|--------|---------|--------|----------|------------|--------|--------|--------|
| 30d (Apr-May 2026) | ~50,000 | 808 | 46.29% | **+138.34** | +1.38% | Positive | Low |
| 90d (Mar-Jun 2026) | 109,610 | 8 | 0% | **-8.73** | -0.09% | -6.19 | 0.09% |
| 180d (Dec 2025-Jun 2026) | 213,290 | 9 | 33.33% | **-1.03** | -0.01% | -0.23 | 0.02% |
| 5yr (Jun 2021-Jun 2026) | 2,412,943 | 16 | 0% | **-15.08** | -0.15% | -1.64 | 0.15% |

### Key Observations

1. **Strategy is NOT robust**: Profitable in 30-day window but unprofitable in 90d, 180d, and 5yr windows
2. **Low trade frequency**: Only 8-16 trades over 90d-5yr periods (99.99%+ signals rejected by gates)
3. **Gate analysis**: Expectancy gate rejects 99.99% of signals. Confidence/imbalance/spread gates pass everything
4. **All 5-year trades are losses**: 16/16 losing trades
5. **Fee/slippage modeled**: Round-trip fee = 0.2% (0.1% entry + 0.1% exit), slippage = 0.1% per side. Verified in trade records: all backtests show ~0.5 USDT fees per trade. A code fix was applied to `normalizeScalpingBacktestConfig` (line 993: `IsNegative()` → `LessThanOrEqual(decimal.Zero)`) to ensure future robustness when fee_rate is omitted from config, but the reported backtests were already run with correct fees via explicit config.

### 5-Year Trade Details

| # | Symbol | Side | Entry | Exit | PnL | Outcome |
|---|--------|------|-------|------|-----|---------|
| 1 | BTC/USDT | sell | 37,514.34 | 37,568.65 | -0.86 | loss |
| 2 | BTC/USDT | sell | 37,493.59 | 37,510.80 | -0.61 | loss |
| 3 | BTC/USDT | sell | 37,435.86 | 37,647.62 | -1.91 | loss |
| 4 | ETH/USDT | sell | 2,714.86 | 2,727.82 | -1.69 | loss |
| 5 | ETH/USDT | sell | 2,718.97 | 2,733.39 | -1.83 | loss |
| 6 | ETH/USDT | sell | 2,708.56 | 2,706.14 | -0.28 | loss |
| 7 | BTC/USDT | sell | 37,465.16 | 37,530.23 | -0.93 | loss |
| 8 | ETH/USDT | sell | 2,664.21 | 2,660.92 | -0.19 | loss |
| 9 | ETH/USDT | buy | 2,633.64 | 2,633.81 | -0.48 | loss |
| 10 | ETH/USDT | buy | 2,694.59 | 2,694.62 | -0.50 | loss |
| 11 | ETH/USDT | buy | 2,700.02 | 2,689.58 | -1.47 | loss |
| 12 | ETH/USDT | buy | 2,701.47 | 2,701.77 | -0.47 | loss |
| 13 | ETH/USDT | buy | 2,701.71 | 2,689.28 | -1.65 | loss |
| 14 | ETH/USDT | buy | 2,678.84 | 2,677.96 | -0.58 | loss |
| 15 | BTC/USDT | buy | 37,329.32 | 37,272.98 | -0.85 | loss |
| 16 | ETH/USDT | buy | 2,683.32 | 2,680.21 | -0.77 | loss |

**Verification**:
```bash
sqlite3 ~/.neuratrade/data/neuratrade.db \
  "SELECT symbol, side, entry_price, exit_price, pnl, outcome \
   FROM scalping_backtest_trades \
   WHERE run_id = '5yr-backtest-1780858462' \
   ORDER BY entry_timestamp;"
```

---

## Paper Trading Results

| Strategy | Closed Trades | PnL (USDT) | Avg PnL | Status |
|----------|---------------|------------|---------|--------|
| arbitrage | 16 | +156.92 | +9.81 | ✅ Profitable |
| daily_trading | 19 | +85.86 | +4.52 | ✅ Profitable |
| swing_trading | 16 | +54.36 | +3.40 | ✅ Profitable |
| scalping | 25 | +25.98 | +1.04 | ✅ Profitable |
| baseline | 53 | 0.00 | 0.00 | ⚠️ Shadow (closed) |

**Combined**: 129 total trades (all closed), +323.12 USDT across 4 active strategies.

### ⚠️ CRITICAL LIMITATION: No Paper Trading for Backtested Symbols

**The 4 active strategies (arbitrage, daily, swing, scalping) trade ONLY BNB/USDT.** The baseline strategy trades 9 other symbols (ZEC, SKYAI, SLX, STO, NEAR, ZORA, OPN, MYX, NIGHT) but generates zero PnL.

**There is zero paper trading evidence for the symbols actually used in backtests:**
- BTC/USDT (backtested extensively but never paper-traded)
- ETH/USDT (backtested extensively but never paper-traded)
- SOL/USDT (backtested but never paper-traded)

**This means paper trading proves nothing about the symbols actually used in backtests.** The +323.12 USDT profit comes from BNB/USDT only, which had only ~1 month of historical data in the backtest. This is a material disconnect between backtest and paper trading universes.

**Verification**:
```bash
sqlite3 ~/.neuratrade/data/neuratrade.db \
  "SELECT strategy_id, COUNT(*), SUM(CAST(pnl AS REAL)) \
   FROM paper_trades WHERE status = 'closed' \
   GROUP BY strategy_id;"
```

---

## Critical Blockers

### 1. Strategy Not Robust (P0)
- 30-day window: +138 USDT ✅
- 90-day window: -8.73 USDT ❌
- 180-day window: -1.03 USDT ❌
- 5-year window: -15.08 USDT ❌

**Impact**: Strategy only works in cherry-picked short windows. Cannot be trusted with real capital.

### 2. Incomplete Data Coverage
- SOL: Sparse data (30% density)
- BNB: Only ~1 month of data
- Multi-ticker results are effectively BTC+ETH only

### 3. No Testnet Validation
- Testnet configuration does not exist
- No live-market proof for scalping strategy

### 4. Risk Management Gaps
- Daily loss cap exists but enforcement unclear
- Kill switch monitoring blocked (issue neura-kxq)
- Position-size throttle blocked (issue neura-3ms)
- float64 used for monetary values in critical code (P0 safety bug)

### 5. Paper Trading Limitations
- Only BNB/USDT for closed trades
- No multi-ticker paper trading
- Baseline strategy has 53 closed trades with $0 PnL and 1 open position

---

## Verification Commands

### Verify 5-year backtest
```bash
sqlite3 ~/.neuratrade/data/neuratrade.db \
  "SELECT COUNT(*) FROM scalping_backtest_trades \
   WHERE run_id = '5yr-backtest-1780858462';"
# Expected: 16
```

### Verify 30-day backtest
```bash
sqlite3 ~/.neuratrade/data/neuratrade.db \
  "SELECT COUNT(*), SUM(CAST(pnl AS REAL)) \
   FROM scalping_backtest_trades \
   WHERE run_id = '883a0208-f3db-4601-b5dc-f82a72a0c569';"
# Expected: 808, +138.34
```

### Verify 90-day backtest
```bash
sqlite3 ~/.neuratrade/data/neuratrade.db \
  "SELECT COUNT(*), SUM(CAST(pnl AS REAL)) \
   FROM scalping_backtest_trades \
   WHERE run_id = 'backtest-1780859403';"
# Expected: 8, -8.73
```

### Verify 180-day backtest
```bash
sqlite3 ~/.neuratrade/data/neuratrade.db \
  "SELECT COUNT(*), SUM(CAST(pnl AS REAL)) \
   FROM scalping_backtest_trades \
   WHERE run_id = 'backtest-1780859435';"
# Expected: 9, -1.03
```

### Verify paper trading
```bash
sqlite3 ~/.neuratrade/data/neuratrade.db \
  "SELECT strategy_id, COUNT(*), SUM(CAST(pnl AS REAL)) \
   FROM paper_trades WHERE status = 'closed' \
   GROUP BY strategy_id;"
```

### Verify data coverage
```bash
sqlite3 ~/.neuratrade/data/neuratrade.db \
  "SELECT tp.symbol, COUNT(o.id), MIN(o.timestamp), MAX(o.timestamp) \
   FROM ohlcv_data o \
   JOIN trading_pairs tp ON o.trading_pair_id = tp.id \
   JOIN exchanges e ON o.exchange_id = e.id \
   WHERE e.name = 'binance' \
   GROUP BY tp.symbol;"
```

---

## Recommendations

### Before Real Money Deployment

1. **Fix strategy robustness**
   - Investigate why strategy fails over 90d+ windows
   - Tune expectancy gate parameters
   - Add regime-specific parameter adaptation

2. **Complete data coverage**
   - Fill SOL data gaps
   - Extend BNB data to full 5 years
   - Verify all symbols have consistent 5m intervals

3. **Implement testnet validation**
   - Add testnet exchange configuration
   - Run 30-day testnet soak test
   - Validate paper/testnet correlation

4. **Fix risk management**
   - Replace float64 with decimal.Decimal in monetary code
   - Implement kill switch monitoring (unblock neura-kxq)
   - Implement position-size throttle (unblock neura-3ms)

5. **Expand paper trading**
   - Run multi-ticker paper trading
   - Close baseline strategy open positions
   - Collect 90+ days of paper trading data

### Minimum Viable Criteria

Before declaring "ready for real money":
- [ ] Profitable 90-day backtest
- [ ] Profitable 180-day backtest
- [ ] Full 5-year data coverage for all symbols
- [ ] 30-day testnet validation
- [ ] Risk management fully implemented
- [ ] Multi-ticker paper trading proof

---

## Appendix: Run IDs for Reference

| Test | Run ID | Date | Data Source | Notes |
|------|--------|------|-------------|-------|
| 5-year backtest | 5yr-backtest-1780858462 | 2026-06-07 | Real Binance 5m | 16 trades, 0% win, -15.08 USDT |
| 30-day backtest | 883a0208-f3db-4601-b5dc-f82a72a0c569 | 2026-06-07 | Real Binance 5m | 808 trades, 46% win, +138.34 USDT |
| 90-day backtest | backtest-1780859403 | 2026-06-08 | Real Binance 5m | 8 trades, 0% win, -8.73 USDT |
| 180-day backtest | backtest-1780859435 | 2026-06-08 | Real Binance 5m | 9 trades, 33% win, -1.03 USDT |
| ~~5-year synthetic~~ | ~~bdefd785-e8c6-48c9-84c6-63cfbdd46d40~~ | ~~2026-06-07~~ | ~~Synthetic/seeded~~ | ~~Excluded from analysis~~ |

### ⚠️ Synthetic Backtest Disclosure

The run `bdefd785` used **synthetic/seeded data**, NOT real historical data. Evidence:
- BTC prices at ~$65,600 in June 2021 (actual 2021 price: ~$30-40K)
- ETH prices at ~$3,690 in June 2021 (actual 2021 price: ~$2,000-2,500)
- SOL prices at ~$150 in June 2021 (SOL was not listed on Binance yet)

**This run is excluded from all real-data analysis.** It was likely generated by the seed/test candle script (`neuratrade-seed-test-candles`) for development purposes.

### Fee and Slippage Modeling

The backtest engine models real-world trading costs:
- **Fee rate**: 0.1% per trade (round-trip = 0.2%)
- **Slippage**: 0.1% per side (entry and exit)
- **Net PnL** = Gross PnL - (Fees + Slippage impact)

**Verified in all backtest trades**: All runs show ~0.5 USDT avg fees per trade, confirming fees are correctly applied.

**Code fix applied**: `normalizeScalpingBacktestConfig` line 993 changed from `IsNegative()` to `LessThanOrEqual(decimal.Zero)` to ensure robust defaulting when fee_rate is omitted from config. This is a defensive fix for future API-based backtests where config JSON may omit the field.

**Verification**:
```bash
sqlite3 ~/.neuratrade/data/neuratrade.db \
  "SELECT id, pnl, fees FROM scalping_backtest_trades \
   WHERE run_id = '5yr-backtest-1780858462' LIMIT 3;"
# fees column shows ~0.5 per trade — correctly applied
```

---

*Assessment generated by NeuraTrade backtest engine*
*Oracle verification: PENDING (awaiting re-verification)*
