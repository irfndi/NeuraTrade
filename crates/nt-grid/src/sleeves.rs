//! Multi-sleeve backtester (additive): parallel filters, combined vote, dynamic leverage.
//!
//! `run_paper_engine` (engine.rs) stays frozen at single-position lev-1 parity.
//! This module reuses `approve_full` -> `submit` -> `Ledger`, so the risk
//! invariant holds: no order without a seal. Grow the backtester by adding
//! `FilterKind` variants and appending `SleeveCfg` rows — no engine edits.
//!
//! Integer-only (no f32/f64): micros, basis points, whole-percent caps.

use nt_execution::{Order, submit};
use nt_ledger::Ledger;
use nt_market::Candle;
use nt_risk::{
    EquityWindow, Money, RiskLimits, ThroughputLimits, ThroughputTracker, TradeIntent, approve_full,
};

use crate::{FillReason, PaperEngineConfig, Side};

/// One directional filter. New filters arrive as variants, not new engines.
/// Additive set (each needs a `filter_vote` arm + a sleeves_check row):
/// Grid (rung touch), Breakout (donchian), Trend (SMA side), Momentum
/// (close-vs-close-N thrust), Chop (ADX-style veto), Funding (carry veto).
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FilterKind {
    /// Grid rung touch (same geometry as `run_paper_engine` entries).
    Grid,
    /// Close breaks beyond the prior `lookback` bars' high/low.
    Breakout,
    /// Close vs its own `lookback`-bar SMA (trend-following).
    Trend,
    /// Close thrust vs the close `lookback` bars ago: long when
    /// `close[i] >= close[i-n] * (1 + step_bp/10000)`, short on the mirror.
    Momentum,
    /// Range veto: SKIP unless the `lookback`-bar range/high-low mean
    /// exceeds `step_bp` of price (dead-flat bars abstain instead of voting).
    Chop,
    /// Carry veto: SKIP when the per-bar funding drag in `step_bp`
    /// would erase the edge (else abstains like Chop).
    Funding,
}

/// One voting sleeve: a filter plus its vote weight and leverage ceiling.
#[derive(Debug, Clone, Copy)]
pub struct SleeveCfg {
    pub kind: FilterKind,
    pub weight_bp: i64,
    pub max_leverage: i64,
    pub step_bp: i64,
    pub lookback: usize,
}

/// Directional vote.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Vote {
    Long,
    Short,
    Skip,
}

/// One combined-signal fill.
#[derive(Debug, Clone, Copy)]
pub struct SleeveFillEvent {
    pub bar: usize,
    pub side: Side,
    pub reason: FillReason,
    pub price: Money,
    pub qty_base_micros: i64,
    pub fee: Money,
    pub leverage: i64,
    pub long_weight_bp: i64,
    pub short_weight_bp: i64,
}

#[derive(Debug, Clone, Copy)]
struct OpenSleeve {
    side: Side,
    entry_price: Money,
    qty_base_micros: i64,
}

fn scale(a: i64, num: i64, denom: i64) -> i64 {
    ((a as i128 * num as i128) / denom as i128) as i64
}

fn worse_price(price: Money, bps: i64, is_buy: bool) -> Money {
    if is_buy {
        Money(scale(price.0, 10_000 + bps, 10_000))
    } else {
        Money(scale(price.0, 10_000 - bps, 10_000))
    }
}

/// Highest high over the `n` bars before `i` (causal: excludes bar `i`).
fn prior_highest_high(candles: &[Candle], i: usize, n: usize) -> Option<Money> {
    if n == 0 || i < n {
        return None;
    }
    candles[i - n..i].iter().map(|c| c.high.0).max().map(Money)
}

/// Lowest low over the `n` bars before `i`.
fn prior_lowest_low(candles: &[Candle], i: usize, n: usize) -> Option<Money> {
    if n == 0 || i < n {
        return None;
    }
    candles[i - n..i].iter().map(|c| c.low.0).min().map(Money)
}

/// SMA of the `n` closes ending at bar `i` (includes the closed bar `i`).
fn sma_close(candles: &[Candle], i: usize, n: usize) -> Option<Money> {
    if n == 0 || i + 1 < n {
        return None;
    }
    let sum: i128 = candles[i + 1 - n..=i]
        .iter()
        .map(|c| c.close.0 as i128)
        .sum();
    Some(Money((sum / n as i128) as i64))
}

/// One filter's vote at bar `i`.
pub fn filter_vote(kind: FilterKind, candles: &[Candle], i: usize, cfg: &SleeveCfg) -> Vote {
    let c = &candles[i];
    match kind {
        FilterKind::Grid => {
            if cfg.step_bp <= 0 {
                return Vote::Skip;
            }
            let step = Money(scale(c.open.0, cfg.step_bp, 10_000));
            if c.low <= Money(c.open.0 - step.0) {
                Vote::Long
            } else if c.high >= Money(c.open.0 + step.0) {
                Vote::Short
            } else {
                Vote::Skip
            }
        }
        FilterKind::Breakout => {
            let (Some(hi), Some(lo)) = (
                prior_highest_high(candles, i, cfg.lookback),
                prior_lowest_low(candles, i, cfg.lookback),
            ) else {
                return Vote::Skip;
            };
            if c.close > hi {
                Vote::Long
            } else if c.close < lo {
                Vote::Short
            } else {
                Vote::Skip
            }
        }
        FilterKind::Trend => {
            let Some(sma) = sma_close(candles, i, cfg.lookback) else {
                return Vote::Skip;
            };
            if c.close > sma {
                Vote::Long
            } else if c.close < sma {
                Vote::Short
            } else {
                Vote::Skip
            }
        }
        FilterKind::Momentum => {
            // Thrust vs the close `lookback` bars ago (causal: excludes bar i).
            // step_bp is the thrust threshold: long at +step, short at -step.
            let n = cfg.lookback;
            if n == 0 || i < n || cfg.step_bp <= 0 {
                return Vote::Skip;
            }
            let base = candles[i - n].close.0;
            if base <= 0 {
                return Vote::Skip;
            }
            let up = base + scale(base, cfg.step_bp, 10_000);
            let dn = base - scale(base, cfg.step_bp, 10_000);
            if c.close.0 >= up {
                Vote::Long
            } else if c.close.0 <= dn {
                Vote::Short
            } else {
                Vote::Skip
            }
        }
        FilterKind::Chop => {
            // Range veto as a PASS-THROUGH voter: SKIP on dead-flat bars
            // (range/mean <= step_bp), else echo the bar's own direction
            // (close vs open) so chop never invents a side, only abstains.
            let n = cfg.lookback.max(1);
            if i + 1 < n || cfg.step_bp <= 0 {
                return Vote::Skip;
            }
            let from = i + 1 - n;
            let (mut hi, mut lo) = (candles[from].high.0, candles[from].low.0);
            let mut sum: i128 = 0;
            for k in candles[from..=i].iter() {
                hi = hi.max(k.high.0);
                lo = lo.min(k.low.0);
                sum += k.high.0 as i128 - k.low.0 as i128;
            }
            let mean_range = sum / n as i128;
            let px = c.close.0.max(1) as i128;
            if mean_range * 10_000 <= cfg.step_bp as i128 * px {
                return Vote::Skip;
            }
            if c.close > c.open {
                Vote::Long
            } else if c.close < c.open {
                Vote::Short
            } else {
                Vote::Skip
            }
        }
        FilterKind::Funding => {
            // Carry veto: pure abstain for now. Directional sleeves decide
            // sides; a live funding feed wires here when the venue adapter
            // lands (Task 7 Step 4) — until then it never blocks a vote.
            Vote::Skip
        }
    }
}

/// Weighted vote totals `(long, short)`, saturating.
pub fn combine_votes(candles: &[Candle], i: usize, sleeves: &[SleeveCfg]) -> (i64, i64) {
    let mut long = 0i64;
    let mut short = 0i64;
    for s in sleeves {
        let w = s.weight_bp.max(0);
        match filter_vote(s.kind, candles, i, s) {
            Vote::Long => long = long.saturating_add(w),
            Vote::Short => short = short.saturating_add(w),
            Vote::Skip => {}
        }
    }
    (long, short)
}

/// Port of TS `accountScaledLeverageCap` (ladder-engine.ts): tiers by equity
/// dollars, discounted by committed budget. Integer-only.
pub fn account_scaled_leverage_cap(
    equity: Money,
    per_position_budget_bp: i64,
    configured_max: i64,
) -> i64 {
    let cap = configured_max.max(1);
    if equity.0 <= 0 {
        return 1;
    }
    let dollars = equity.0 / 1_000_000;
    let size_cap = if dollars < 500 {
        10
    } else if dollars < 5000 {
        25
    } else if dollars < 25000 {
        50
    } else {
        150
    };
    let eff = if per_position_budget_bp <= 1000 {
        size_cap
    } else if per_position_budget_bp <= 2500 {
        size_cap * 3 / 4
    } else {
        size_cap / 2
    };
    eff.max(1).min(cap)
}

/// Conviction-scaled leverage: 1 at no agreement, `cap` at unanimity.
pub fn conviction_leverage(cap: i64, conviction_bp: i64) -> i64 {
    if cap <= 1 {
        return 1;
    }
    let c = conviction_bp.clamp(0, 10_000);
    1 + (cap - 1) * c / 10_000
}

/// Entry price from the base grid geometry for a combined side.
fn entry_price(side: Side, candle: &Candle, base: &PaperEngineConfig) -> Money {
    let step = Money(scale(candle.open.0, base.step_bp, 10_000));
    match side {
        Side::Long => worse_price(Money(candle.open.0 - step.0), base.slippage_bps, true),
        Side::Short => worse_price(Money(candle.open.0 + step.0), base.slippage_bps, false),
    }
}

/// Target/stop exit check for an open sleeve (same rule as the engine).
fn exit_signal(
    pos: &OpenSleeve,
    candle: &Candle,
    base: &PaperEngineConfig,
) -> Option<(FillReason, Money)> {
    let step = Money(scale(candle.open.0, base.step_bp, 10_000));
    let target_delta = Money(scale(step.0, base.target_ratio_x100, 100));
    let stop_delta = Money(step.0 * base.grid_max_grids);
    let (target, stop) = match pos.side {
        Side::Long => (
            Money(pos.entry_price.0 + target_delta.0),
            Money(pos.entry_price.0 - stop_delta.0),
        ),
        Side::Short => (
            Money(pos.entry_price.0 - target_delta.0),
            Money(pos.entry_price.0 + stop_delta.0),
        ),
    };
    let target_hit = match pos.side {
        Side::Long => candle.high >= target,
        Side::Short => candle.low <= target,
    };
    if target_hit {
        return Some((FillReason::Target, target));
    }
    let stop_hit = match pos.side {
        Side::Long => candle.low <= stop,
        Side::Short => candle.high >= stop,
    };
    if stop_hit {
        let is_buy = pos.side == Side::Short;
        Some((
            FillReason::Stop,
            worse_price(stop, base.slippage_bps, is_buy),
        ))
    } else {
        None
    }
}

/// Walk `candles` (oldest-first) with one combined-signal position: every bar
/// votes all sleeves in parallel, the weighted majority takes the side, and
/// conviction scales leverage inside the account cap. Exits reuse the base
/// target/stop and bypass count/throughput halt so a halt never strands
/// inventory (same contract as `run_paper_engine`).
pub fn run_sleeve_backtest(
    candles: &[Candle],
    base: &PaperEngineConfig,
    sleeves: &[SleeveCfg],
    capital: Money,
    limits: &RiskLimits,
) -> (Vec<SleeveFillEvent>, Ledger) {
    let day_start = capital;
    let mut peak = capital;
    // Closed-round-trip PnL only: open entries never move the equity window.
    let mut closed_realized: i64 = 0;
    let mut tracker = ThroughputTracker::default();
    let tp_key = ThroughputTracker::key("sleeves", "COMBINED", "1h");
    let tp_limits = ThroughputLimits::strict();
    let mut ledger = Ledger::new();
    let mut events = Vec::new();
    let mut position: Option<(OpenSleeve, i64)> = None; // + entry fill net

    for (i, candle) in candles.iter().enumerate() {
        match position {
            None => {
                let (long_w, short_w) = combine_votes(candles, i, sleeves);
                let side = if long_w > short_w {
                    Side::Long
                } else if short_w > long_w {
                    Side::Short
                } else {
                    continue; // tie or silence: clean HOLD
                };
                let total = long_w.saturating_add(short_w);
                let spread = if long_w >= short_w {
                    long_w - short_w
                } else {
                    short_w - long_w
                };
                let conviction = (spread as i128 * 10_000 / total as i128) as i64;
                let price = entry_price(side, candle, base);
                let current = Money(capital.0 + closed_realized);
                if current.0 > peak.0 {
                    peak = current;
                }
                let budget_bp = base.max_position_size_pct * 100;
                // Strictest voting-side ceiling wins: a Grid max-1 sleeve must
                // never open at 10x on another sleeve's votes.
                let side_votes_long = side == Side::Long;
                let cfg_max = sleeves
                    .iter()
                    .filter(|s| {
                        matches!(
                            (filter_vote(s.kind, candles, i, s), side_votes_long),
                            (Vote::Long, true) | (Vote::Short, false)
                        )
                    })
                    .map(|s| s.max_leverage)
                    .min()
                    .unwrap_or(1);
                let cap = account_scaled_leverage_cap(current, budget_bp, cfg_max);
                let lev = conviction_leverage(cap, conviction);
                let margin = Money(scale(capital.0, base.max_position_size_pct, 100));
                let notional = Money(margin.0.saturating_mul(lev));
                let qty_abs = scale(notional.0, 1_000_000, price.0.max(1));
                let qty_signed = if side == Side::Long {
                    qty_abs
                } else {
                    -qty_abs
                };

                let eq = EquityWindow {
                    current,
                    peak,
                    day_start,
                };
                let tp = tracker.window(&tp_key, candle.open_ts_ms, 3_600_000);
                let intent = TradeIntent {
                    trades_today: events.len() as u32,
                    ..TradeIntent::default()
                };
                let Ok((appr, halt)) = approve_full(
                    capital,
                    &eq,
                    notional,
                    notional,
                    Money::ZERO,
                    lev,
                    limits,
                    &intent,
                    &tp,
                    &tp_limits,
                ) else {
                    continue; // Rejected: skip this bar's entry, no trade.
                };
                if !halt.is_empty() {
                    continue; // Throughput halt: approved book, no new entries.
                }
                let fill = submit(
                    appr,
                    Order {
                        qty_base_micros: qty_signed,
                        price_micros: price,
                        fee_bp: base.fee_bp,
                    },
                );
                events.push(SleeveFillEvent {
                    bar: i,
                    side,
                    reason: FillReason::Entry,
                    price,
                    qty_base_micros: qty_signed,
                    fee: fill.fee_micros,
                    leverage: lev,
                    long_weight_bp: long_w,
                    short_weight_bp: short_w,
                });
                ledger.apply(fill);
                tracker.record(
                    &tp_key,
                    candle.open_ts_ms,
                    fill.proceeds_micros.0,
                    fill.fee_micros.0,
                );
                let entry_net = fill.proceeds_micros.0.saturating_sub(fill.fee_micros.0);
                position = Some((
                    OpenSleeve {
                        side,
                        entry_price: price,
                        qty_base_micros: qty_signed,
                    },
                    entry_net,
                ));
            }
            Some((pos, entry_net)) => {
                let Some((reason, exit_price)) = exit_signal(&pos, candle, base) else {
                    continue;
                };
                let exit_qty = -pos.qty_base_micros;
                let current = Money(capital.0 + closed_realized);
                if current.0 > peak.0 {
                    peak = current;
                }
                let eq = EquityWindow {
                    current,
                    peak,
                    day_start,
                };
                let tp = tracker.window(&tp_key, candle.open_ts_ms, 3_600_000);
                let exit_intent = TradeIntent {
                    trades_today: 0,
                    ..TradeIntent::default()
                };
                let Ok((appr, _)) = approve_full(
                    capital,
                    &eq,
                    Money::ZERO,
                    Money::ZERO,
                    Money::ZERO,
                    1,
                    limits,
                    &exit_intent,
                    &tp,
                    &tp_limits,
                ) else {
                    continue; // Fail closed; a zero-size close only trips floor/drawdown.
                };
                let fill = submit(
                    appr,
                    Order {
                        qty_base_micros: exit_qty,
                        price_micros: exit_price,
                        // Mirror engine.rs: a resting target exit pays maker,
                        // a stop crosses the spread and pays taker. Without
                        // this every sleeves backtest overcharged target
                        // exits by 4bp, biasing the comparison against
                        // high-target-hit-rate combinations.
                        fee_bp: if reason == FillReason::Target {
                            base.maker_fee_bp
                        } else {
                            base.fee_bp
                        },
                    },
                );
                events.push(SleeveFillEvent {
                    bar: i,
                    side: pos.side,
                    reason,
                    price: exit_price,
                    qty_base_micros: exit_qty,
                    fee: fill.fee_micros,
                    leverage: 1,
                    long_weight_bp: 0,
                    short_weight_bp: 0,
                });
                ledger.apply(fill);
                tracker.record(
                    &tp_key,
                    candle.open_ts_ms,
                    fill.proceeds_micros.0,
                    fill.fee_micros.0,
                );
                let exit_net = fill.proceeds_micros.0.saturating_sub(fill.fee_micros.0);
                closed_realized =
                    closed_realized.saturating_add(entry_net.saturating_add(exit_net));
                position = None;
            }
        }
    }

    (events, ledger)
}
