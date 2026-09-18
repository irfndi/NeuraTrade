//! Stateful single-position paper grid engine (P5 Step 3, narrowed scope).
//!
//! Ports the core entry/target/stop mechanic of
//! `services/neuratrade-cli-ts/src/paper-trading/grid-engine.ts`
//! (`resolveGridEntry` / `resolveGridPositionExit`) with leverage fixed at
//! 1 and the trend filter, chop gate, max-hold-bars and
//! pause-after-loss-bars dropped — see the task brief
//! (`.superpowers/sdd/2026-09-18-rust-bend-strangler-e2e/task-7-step3-brief.md`)
//! for the exact formulas and the scope ruling. This module is additive:
//! it does not touch [`crate::GridConfig`] or [`crate::evaluate`] (the
//! multi-rung ladder used by the Bend-parity example), which are a
//! different, unrelated model (portfolio ladder vs. single position).
//!
//! Every entry AND exit order is minted only via `nt_risk::approve` ->
//! `nt_execution::submit` — `submit` has no other constructor for its
//! `RiskApproval` seal, so this module can never place an order the risk
//! gate didn't approve first (the plan's Global Constraint: "risk path
//! remains the only path to execution"). All money is integer micro-USDT
//! (`nt_risk::Money`) — no `f32`/`f64` anywhere in this file.
//!
//! Deviation from the brief's literal signature: the brief describes the
//! entry point as taking a `Panel`, but `nt_market::Panel`'s public API
//! (`len`, `is_empty`, `latest_close`, `closes`) exposes only closing
//! prices, not per-candle open/high/low — and this task's scope explicitly
//! forbids editing `nt-market` (even to add a trivial accessor). This
//! engine needs open/high/low every bar (target/stop are intrabar-touch
//! checks against high/low, not close), so [`run_paper_engine`] takes
//! `&[nt_market::Candle]` (oldest-first, the same order `Panel::new` sorts
//! into) directly instead.

use nt_execution::{HONEST_TAKER_EXIT_BP, Order, submit};
use nt_ledger::Ledger;
use nt_market::Candle;
use nt_risk::{EquityWindow, Money, RiskLimits, approve};

/// Config for the stateful paper engine. Distinct from [`crate::GridConfig`]
/// (which drives the multi-rung [`crate::evaluate`] ladder and is untouched
/// by this module): this engine tracks ONE position end to end through
/// risk -> execution -> ledger, so it needs entry/exit knobs `evaluate`
/// doesn't have.
#[derive(Debug, Clone, Copy)]
pub struct PaperEngineConfig {
    /// Grid step as basis points of the bar's own open price (`mid`).
    /// Mirrors TS `gridStepPct` (a float percent) but in integer bp, same
    /// convention as [`crate::GridConfig::step_bp`]: `step_bp = 130` means
    /// 1.30%. Recomputed fresh every bar from that bar's `open` — never
    /// cached from the entry bar (see [`run_paper_engine`] doc).
    pub step_bp: i64,
    /// Target distance from entry, in units of `step`, fixed-point x100
    /// (2 decimal places): `target_ratio_x100 = 150` means TS's
    /// `targetRatio = 1.50` in `entryPrice +/- step * targetRatio`.
    pub target_ratio_x100: i64,
    /// Stop distance from entry, in whole units of `step` (TS
    /// `gridMaxGrids`, already an integer grid count in the source).
    pub grid_max_grids: i64,
    /// Slippage in basis points, applied per the worse-for-trader rule (see
    /// [`worse_price`]). TS derives `slippageFactor = 1 + slippageBps /
    /// 10000` from this same knob elsewhere in `grid-engine.ts`.
    pub slippage_bps: i64,
    /// Position sizing as a whole percent of capital (TS `maxPositionPct`).
    /// Exchange contract-spec rounding (min qty, tick size) is out of scope
    /// for this pass — see the task brief.
    pub max_position_size_pct: i64,
}

/// Which side a fill's position was on.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum Side {
    Long,
    Short,
}

/// Why a fill happened.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum FillReason {
    Entry,
    Target,
    Stop,
}

/// One recorded paper-engine fill, with the position context a bare
/// `nt_execution::Fill` doesn't carry (side, reason, which bar).
#[derive(Debug, Clone, Copy)]
pub struct PaperFillEvent {
    pub bar: usize,
    pub side: Side,
    pub reason: FillReason,
    pub price: Money,
    pub qty_base_micros: i64,
    pub fee: Money,
}

#[derive(Debug, Clone, Copy)]
struct OpenPosition {
    side: Side,
    entry_price: Money,
    qty_base_micros: i64,
}

/// `a * num / denom`, truncating toward zero via an `i128` intermediate
/// (same overflow-avoidance convention `nt_risk::pct_of` uses for percent
/// math). All three arguments are micro-USDT-scale or small integer
/// ratios in this module, well inside `i128` range.
fn scale(a: i64, num: i64, denom: i64) -> i64 {
    ((a as i128 * num as i128) / denom as i128) as i64
}

/// Apply slippage on the worse-for-trader side: `is_buy = true` widens the
/// price up (a buy pays more), `is_buy = false` narrows it down (a sell
/// receives less). This is the brief's deliberately simplified symmetric
/// rule (`price * (10000 +/- bps) / 10000`) — NOT TS's asymmetric
/// `times`/`div` (`price * slippageFactor` vs `price / slippageFactor`),
/// which the brief calls out as a first-pass approximation for integer
/// math. See the task report's "deviations" section.
fn worse_price(price: Money, bps: i64, is_buy: bool) -> Money {
    if is_buy {
        Money(scale(price.0, 10_000 + bps, 10_000))
    } else {
        Money(scale(price.0, 10_000 - bps, 10_000))
    }
}

/// Walk `candles` (oldest-first) bar by bar with a single-position state
/// machine: a flat bar may only be checked for entry, a bar with an open
/// position may only be checked for exit — never both in the same bar
/// (matches TS's `state.side === null` branch split; a position opened
/// during bar N is first eligible for an exit check on bar N+1).
///
/// `step` (`mid.times(gridStepPct/100)` in TS) is recomputed from each
/// bar's OWN `open` — both for whichever check applies that bar. It is
/// never frozen from the entry bar, even at exit time.
///
/// Every order, entry or exit, is minted only through `nt_risk::approve`
/// -> `nt_execution::submit`; a rejected approval skips that bar's action
/// with no panic and no trade (fail-closed, matching the risk-gate
/// invariant). Exit orders are approved with zero position/notional value
/// (closing an existing position adds no new size), so in practice only
/// the capital floor / drawdown checks could ever reject an exit.
///
/// `capital` and `limits` are held fixed for the whole run — this pass
/// does not track equity/drawdown across fills (no realized-PnL feedback
/// into `capital`). That is a first-pass simplification beyond what the
/// brief's formulas require; see the task report.
pub fn run_paper_engine(
    candles: &[Candle],
    cfg: &PaperEngineConfig,
    capital: Money,
    limits: &RiskLimits,
) -> (Vec<PaperFillEvent>, Ledger) {
    let window = EquityWindow {
        current: capital,
        peak: capital,
        day_start: capital,
    };
    let mut ledger = Ledger::new();
    let mut events = Vec::new();
    let mut position: Option<OpenPosition> = None;

    for (i, candle) in candles.iter().enumerate() {
        let step = Money(scale(candle.open.0, cfg.step_bp, 10_000));

        match position {
            None => {
                let buy_level = Money(candle.open.0 - step.0);
                let sell_level = Money(candle.open.0 + step.0);
                // Long checked first, then short — matches TS's two
                // independent `if`s with an early return on the first.
                let attempt = if candle.low <= buy_level {
                    Some((Side::Long, worse_price(buy_level, cfg.slippage_bps, true)))
                } else if candle.high >= sell_level {
                    Some((
                        Side::Short,
                        worse_price(sell_level, cfg.slippage_bps, false),
                    ))
                } else {
                    None
                };
                let Some((side, entry_price)) = attempt else {
                    continue;
                };

                let position_value = Money(scale(capital.0, cfg.max_position_size_pct, 100));
                let qty_abs = scale(position_value.0, 1_000_000, entry_price.0.max(1));
                let qty_signed = if side == Side::Long { qty_abs } else { -qty_abs };

                let Ok(appr) = approve(
                    capital,
                    &window,
                    position_value,
                    position_value,
                    Money::ZERO,
                    1,
                    limits,
                ) else {
                    continue; // Rejected: skip this bar's entry, no trade.
                };
                let fill = submit(
                    appr,
                    Order {
                        qty_base_micros: qty_signed,
                        price_micros: entry_price,
                        fee_bp: HONEST_TAKER_EXIT_BP,
                    },
                );
                events.push(PaperFillEvent {
                    bar: i,
                    side,
                    reason: FillReason::Entry,
                    price: entry_price,
                    qty_base_micros: qty_signed,
                    fee: fill.fee_micros,
                });
                ledger.apply(fill);
                position = Some(OpenPosition {
                    side,
                    entry_price,
                    qty_base_micros: qty_signed,
                });
            }
            Some(pos) => {
                let target_delta = Money(scale(step.0, cfg.target_ratio_x100, 100));
                let stop_delta = Money(step.0 * cfg.grid_max_grids);
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
                // Target checked first; stop only if target didn't fire.
                let target_hit = match pos.side {
                    Side::Long => candle.high >= target,
                    Side::Short => candle.low <= target,
                };
                let exit = if target_hit {
                    // Target exits are NOT slippage-adjusted (TS:
                    // `theoreticalExitPrice: target`, no multiply).
                    Some((FillReason::Target, target))
                } else {
                    let stop_hit = match pos.side {
                        Side::Long => candle.low <= stop,
                        Side::Short => candle.high >= stop,
                    };
                    if stop_hit {
                        // Long stop sells (worse = lower); short stop buys
                        // (worse = higher).
                        let is_buy = pos.side == Side::Short;
                        Some((
                            FillReason::Stop,
                            worse_price(stop, cfg.slippage_bps, is_buy),
                        ))
                    } else {
                        None
                    }
                };
                let Some((reason, exit_price)) = exit else {
                    continue;
                };

                let exit_qty = -pos.qty_base_micros;
                let Ok(appr) = approve(
                    capital,
                    &window,
                    Money::ZERO,
                    Money::ZERO,
                    Money::ZERO,
                    1,
                    limits,
                ) else {
                    // Fail closed rather than force an unapproved trade,
                    // even though a zero-size close should never be
                    // rejected outside a capital-floor breach.
                    continue;
                };
                let fill = submit(
                    appr,
                    Order {
                        qty_base_micros: exit_qty,
                        price_micros: exit_price,
                        fee_bp: HONEST_TAKER_EXIT_BP,
                    },
                );
                events.push(PaperFillEvent {
                    bar: i,
                    side: pos.side,
                    reason,
                    price: exit_price,
                    qty_base_micros: exit_qty,
                    fee: fill.fee_micros,
                });
                ledger.apply(fill);
                position = None;
            }
        }
    }

    (events, ledger)
}
