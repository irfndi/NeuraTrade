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

use nt_execution::{Order, submit};
use nt_ledger::Ledger;
use nt_market::Candle;
use nt_risk::{
    EquityWindow, Money, RiskLimits, ThroughputLimits, ThroughputTracker, TradeIntent, approve_full,
};

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
    /// Per-ticker realized cost in basis points of notional: exchange fee
    /// plus minimum-size rounding drag for THIS symbol. Slippage is NOT in
    /// here — it stays in `slippage_bps` (`worse_price` moves the fill
    /// price; this only sizes the fee line). The global 6bp default is a
    /// research floor, not a trading promise: PUMPFUN-class drag fails
    /// sizing against its own edge while ETH passes. Feed per-symbol.
    pub fee_bp: i64,
    /// Maker fee in bp of notional for resting target exits (honest schedule
    /// `maker0.02`, see `nt_execution::HONEST_MAKER_FEE_BP` /
    /// `champion-soak.json` `honestFees`). Entries and stop exits stay taker
    /// (`fee_bp`): only a target exit is modelled as a resting limit fill.
    pub maker_fee_bp: i64,
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
/// Open position carried across incremental shadow ticks (Task 7 Step 5).
/// Same data the engine already holds in `OpenPosition` + entry net, made
/// persistable so an exit whose entry sat in a prior tick still fires.
#[derive(Debug, Clone, Copy)]
pub struct ResumePosition {
    pub side: Side,
    pub entry_price: Money,
    pub qty_base_micros: i64,
    pub entry_net: i64,
}

/// Resume seed for incremental replay (`nt-cli shadow --resume`).
/// `peak=None` starts at `capital`; `window_fills` reseeds the hourly
/// throughput window so halts match a continuous run. `bar_offset` shifts
/// emitted `PaperFillEvent.bar` so appended per-tick ledgers match the
/// continuous run's indices (each tick replays a slice starting at 0).
/// `event_count` is cumulative fill bookkeeping for the ledger view only
/// (it does NOT gate trades — see the day boundary below); `cum_*`
/// accumulate ledger totals so tick output prints continuous-equivalent PnL.
///
/// Day boundary: `day_index` is the UTC day index of the last bar processed
/// (`open_ts_ms / 86_400_000`, floor) and `day_fills` the fills recorded
/// inside that day — the ONLY source of `trades_today`, never the cumulative
/// `event_count` (a cumulative count would permanently halt every replay at
/// `RiskLimits::live().max_trades_per_day = 10` total fills). `None`/`0`
/// means unset: a fresh run, where the first bar establishes the boundary.
/// `day_start_capital` is the capital in force at that day's first bar, so
/// the daily-loss denominator uses the real boundary rather than being
/// re-derived from the current tick's capital.
#[derive(Debug, Clone, Default)]
pub struct ResumeState {
    pub position: Option<ResumePosition>,
    pub closed_realized: i64,
    pub peak: Option<Money>,
    /// (open_ts_ms, gross_micros, fee_micros), pruned to the window horizon.
    pub window_fills: Vec<(i64, i64, i64)>,
    /// Bars consumed by prior ticks; added to each emitted event's `bar`.
    pub bar_offset: usize,
    /// Fills emitted by prior ticks (cumulative, ledger bookkeeping only).
    pub event_count: usize,
    /// Ledger totals accumulated by prior ticks.
    pub cum_fills: u64,
    pub cum_gross: i64,
    pub cum_fees: i64,
    /// UTC day index of the last processed bar; `None` = unset.
    pub day_index: Option<i64>,
    /// Fills inside `day_index` (reset once at each rollover).
    pub day_fills: u32,
    /// Capital in force at `day_index`'s first bar; `None` = unset.
    pub day_start_capital: Option<Money>,
}

/// End state for the `--resume` file: everything the next tick needs.
#[derive(Debug, Clone)]
pub struct EndState {
    pub position: Option<ResumePosition>,
    pub closed_realized: i64,
    pub peak: Money,
    pub window_fills: Vec<(i64, i64, i64)>,
    /// Prior offset + bars consumed this tick; the next tick's offset.
    pub bar_offset: usize,
    /// Prior count + fills emitted this tick.
    pub event_count: usize,
    /// Prior totals + this tick's ledger totals.
    pub cum_fills: u64,
    pub cum_gross: i64,
    pub cum_fees: i64,
    /// UTC day index of the last processed bar (`open_ts_ms / 86_400_000`,
    /// floor); `None` only when the tick saw no bars.
    pub day_index: Option<i64>,
    /// Fills inside `day_index`.
    pub day_fills: u32,
    /// Capital in force at `day_index`'s first bar.
    pub day_start_capital: Money,
}

pub fn run_paper_engine(
    candles: &[Candle],
    cfg: &PaperEngineConfig,
    capital: Money,
    limits: &RiskLimits,
) -> (Vec<PaperFillEvent>, Ledger) {
    let (events, ledger, _) =
        run_paper_engine_from(candles, cfg, capital, limits, &ResumeState::default());
    (events, ledger)
}

/// Same as [`run_paper_engine`], but resumes from / returns tick state so
/// incremental replays converge to the continuous run on the same panel.
pub fn run_paper_engine_from(
    candles: &[Candle],
    cfg: &PaperEngineConfig,
    capital: Money,
    limits: &RiskLimits,
    resume: &ResumeState,
) -> (Vec<PaperFillEvent>, Ledger, EndState) {
    // Day boundary (UTC day index = `open_ts_ms / 86_400_000`, floor). A tick
    // that starts mid-day carries the persisted boundary and fill count
    // forward, so its daily window continues; a tick whose first bar falls on
    // a strictly later day than the persisted one — or a fresh run, where the
    // first bar establishes the boundary — re-anchors at this tick's capital.
    // The per-bar check below re-runs the same rule at every boundary, so a
    // panel spanning several days inside ONE tick rolls over exactly once per
    // day (previously `day_start` was re-derived from `capital` on every
    // tick, resetting the daily-loss denominator mid-day).
    let first_day = candles.first().map(|c| c.open_ts_ms / 86_400_000);
    let rollover = match (first_day, resume.day_index) {
        (Some(today), Some(prev)) => today > prev,
        (Some(_), None) => true, // fresh run: first bar establishes it.
        (None, _) => false,      // empty panel: nothing to establish.
    };
    let mut day_index = first_day.or(resume.day_index).unwrap_or(0);
    let mut day_fills = if rollover { 0 } else { resume.day_fills };
    let mut day_start = if rollover {
        capital
    } else {
        resume.day_start_capital.unwrap_or(capital)
    };
    let mut peak = resume.peak.unwrap_or(capital);
    // Closed-round-trip PnL only: open entries never move the equity window
    // (an entry's -99M proceeds is inventory, not a 10% daily loss).
    let mut closed_realized: i64 = resume.closed_realized;
    let mut tracker = ThroughputTracker::default();
    let tp_key = ThroughputTracker::key("paper", "PAPER", "1h");
    let tp_limits = ThroughputLimits::strict();
    for &(ts, gross, fee) in &resume.window_fills {
        tracker.record(&tp_key, ts, gross, fee);
    }
    // Mirror of the tracker window for the end state (tracker has no dump).
    let mut session_fills: Vec<(i64, i64, i64)> = resume.window_fills.clone();
    let mut ledger = Ledger::new();
    let mut events = Vec::new();
    let mut position: Option<(OpenPosition, i64)> = resume.position.map(|p| {
        (
            OpenPosition {
                side: p.side,
                entry_price: p.entry_price,
                qty_base_micros: p.qty_base_micros,
            },
            p.entry_net,
        )
    });

    for (i, candle) in candles.iter().enumerate() {
        // Per-bar day boundary: a panel spanning several days inside ONE tick
        // rolls over exactly once per day, matching a per-day tick split.
        let bar_day = candle.open_ts_ms / 86_400_000;
        if bar_day > day_index {
            day_index = bar_day;
            day_fills = 0;
            // Re-anchor on equity (capital + closed realized), not raw
            // `capital`: re-anchoring on the fixed deposit would make the
            // daily-loss denominator ignore prior-day PnL entirely.
            day_start = Money(capital.0 + closed_realized);
        }
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
                let qty_signed = if side == Side::Long {
                    qty_abs
                } else {
                    -qty_abs
                };

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
                // Daily-count gate sees ONLY fills inside the current UTC
                // day. Cumulative `event_count` is ledger bookkeeping; using
                // it here halted every replay permanently at
                // `max_trades_per_day = 10` total fills across days.
                let intent = TradeIntent {
                    trades_today: day_fills,
                    ..TradeIntent::default()
                };
                let Ok((appr, halt)) = approve_full(
                    capital,
                    &eq,
                    position_value,
                    position_value,
                    Money::ZERO,
                    1,
                    limits,
                    &intent,
                    &tp,
                    &tp_limits,
                ) else {
                    continue; // Rejected: skip this bar's entry, no trade.
                };
                if !halt.is_empty() {
                    continue; // Throughput halt: approved book, no new entries.
                };
                let fill = submit(
                    appr,
                    Order {
                        qty_base_micros: qty_signed,
                        price_micros: entry_price,
                        fee_bp: cfg.fee_bp,
                    },
                );
                events.push(PaperFillEvent {
                    bar: resume.bar_offset.saturating_add(i),
                    side,
                    reason: FillReason::Entry,
                    price: entry_price,
                    qty_base_micros: qty_signed,
                    fee: fill.fee_micros,
                });
                day_fills = day_fills.saturating_add(1);
                ledger.apply(fill);
                tracker.record(
                    &tp_key,
                    candle.open_ts_ms,
                    fill.proceeds_micros.0,
                    fill.fee_micros.0,
                );
                session_fills.push((candle.open_ts_ms, fill.proceeds_micros.0, fill.fee_micros.0));
                let entry_net = fill.proceeds_micros.0.saturating_sub(fill.fee_micros.0);
                position = Some((
                    OpenPosition {
                        side,
                        entry_price,
                        qty_base_micros: qty_signed,
                    },
                    entry_net,
                ));
            }
            Some((pos, entry_net)) => {
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
                // Exit bypasses daily-count and ignores throughput halt: a halt
                // must never strand inventory. Only hard v (floor/drawdown)
                // can still block a close.
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
                        // Maker schedule for a resting target exit (TS's
                        // `theoreticalExitPrice: target` is a resting limit
                        // fill); stops and entries pay taker.
                        fee_bp: if reason == FillReason::Target {
                            cfg.maker_fee_bp
                        } else {
                            cfg.fee_bp
                        },
                    },
                );
                events.push(PaperFillEvent {
                    bar: resume.bar_offset.saturating_add(i),
                    side: pos.side,
                    reason,
                    price: exit_price,
                    qty_base_micros: exit_qty,
                    fee: fill.fee_micros,
                });
                day_fills = day_fills.saturating_add(1);
                ledger.apply(fill);
                tracker.record(
                    &tp_key,
                    candle.open_ts_ms,
                    fill.proceeds_micros.0,
                    fill.fee_micros.0,
                );
                session_fills.push((candle.open_ts_ms, fill.proceeds_micros.0, fill.fee_micros.0));
                let exit_net = fill.proceeds_micros.0.saturating_sub(fill.fee_micros.0);
                closed_realized =
                    closed_realized.saturating_add(entry_net.saturating_add(exit_net));
                position = None;
            }
        }
    }

    let last_ts = candles.last().map(|c| c.open_ts_ms).unwrap_or(i64::MIN);
    // Mirror of Tracker's expiry (`now - ts < window_ms`): keep fills the
    // next tick's window() would still see relative to the last bar.
    session_fills.retain(|(ts, _, _)| last_ts.saturating_sub(*ts) < 3_600_000);
    let tick = ledger.totals();
    let end = EndState {
        position: position.map(|(p, entry_net)| ResumePosition {
            side: p.side,
            entry_price: p.entry_price,
            qty_base_micros: p.qty_base_micros,
            entry_net,
        }),
        closed_realized,
        peak,
        window_fills: session_fills,
        bar_offset: resume.bar_offset.saturating_add(candles.len()),
        event_count: resume.event_count.saturating_add(events.len()),
        cum_fills: resume.cum_fills.saturating_add(tick.fills),
        cum_gross: resume.cum_gross.saturating_add(tick.gross_micros),
        cum_fees: resume.cum_fees.saturating_add(tick.fees_micros),
        day_index: candles
            .last()
            .map(|c| c.open_ts_ms / 86_400_000)
            .or(resume.day_index),
        day_fills,
        day_start_capital: day_start,
    };
    (events, ledger, end)
}
