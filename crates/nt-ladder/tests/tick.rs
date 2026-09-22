// Slice 4 assertions — port of TS `advanceLadderBar` / `fillLadderSide` /
// `closeLadderTargets`. Deliberately small: each test pins ONE behaviour that
// a plausible refactor would break, not a full replay (that is slice 5's
// parity example).
use nt_ladder::{
    BarContext, Candle, CloseReason, ContractSpec, LadderConfig, LadderState, Side, SizingOptions,
    advance_bar,
};
use nt_risk::Money;

fn micros(v: f64) -> Money {
    Money((v * 1_000_000.0).round() as i64)
}

fn cfg() -> LadderConfig {
    LadderConfig {
        grid_step_bp: 130,
        grid_max_grids: 2,
        grid_pause_after_loss_bars: 2,
        rungs: 2,
        target_ratio_x100: 195,
        only_with_trend: false,
        chop_gate_adx: 0,
        max_hold_bars: 39,
        stop_ratio_x100: 158,
        conservative_intrabar: true,
    }
}

fn opts() -> SizingOptions {
    SizingOptions {
        max_position_pct: 100,
        max_notional_pct: Some(100),
        rungs: 2,
        leverage: 1,
        fully_dynamic: true,
        max_leverage: 10,
        // Generous spec so sizing never blocks these tests.
        spec: Some(ContractSpec {
            min_qty: 0,
            qty_step: 1_000,
        }),
        maker_fee_bp: 5,
        taker_exit_fee_bp: None,
        live_entry_cross_bps: 0,
    }
}

fn ctx(open: f64, high: f64, low: f64, close: f64, ts_ms: i64, bar_index: u64) -> BarContext {
    BarContext {
        bar_index,
        candle: Candle {
            open: micros(open),
            high: micros(high),
            low: micros(low),
            close: micros(close),
            ts_ms,
        },
        step: micros(open * 0.013),
        slippage_bp: 20,
        rung_count: 2,
        target_ratio_x100: 195,
        max_hold_bars: 39,
        ms_per_bar: 15 * 60 * 1000,
        conservative_intrabar: true,
        max_drawdown_pct: 0,
    }
}

#[test]
fn flat_bar_seeds_both_sides_then_a_touch_fills_progressively() {
    let mut state = LadderState::fresh(micros(50.0), cfg());
    // Seed anchors on the bar's OPEN (TS `setSideBase(w, side, candle.open)`),
    // and rung k sits k full steps BELOW the anchor: longs at 98.7 and 97.4,
    // shorts at 101.3 and 102.6. Rung 1 is NOT at the anchor.
    let ev = advance_bar(
        &mut state,
        &ctx(100.0, 100.0, 100.0, 100.0, 1_000_000, 0),
        opts(),
    );
    assert_eq!(ev.fills.len(), 0, "no touch on the seeding bar: {ev:?}");
    assert_eq!(state.long_base, micros(100.0), "anchor is the bar's open");
    assert_eq!(state.rungs(Side::Long).len(), 2, "long ladder seeded");
    assert_eq!(
        state.rungs(Side::Long)[0].level,
        micros(98.7),
        "rung 1 is one step below"
    );
    assert_eq!(
        state.rungs(Side::Long)[1].level,
        micros(97.4),
        "rung 2 is two steps below"
    );
    assert_eq!(
        state.rungs(Side::Short)[0].level,
        micros(101.3),
        "short rung 1 is one step above"
    );

    // Price falls to 98.6: touches long rung 1 (98.7) only, so only it fills.
    let ev = advance_bar(
        &mut state,
        &ctx(99.0, 99.5, 98.6, 99.0, 2_000_000, 1),
        opts(),
    );
    assert_eq!(ev.fills.len(), 1, "only the first rung fills: {ev:?}");
    assert_eq!(ev.fills[0].side, Side::Long);
    assert_eq!(ev.fills[0].rung_index, 1);
    assert!(state.rungs(Side::Long)[0].filled);

    // Deeper: rung 2 (97.4) fills now that rung 1 is filled (progressive guard).
    let ev = advance_bar(
        &mut state,
        &ctx(98.0, 98.3, 97.3, 97.4, 3_000_000, 2),
        opts(),
    );
    assert_eq!(
        ev.fills.len(),
        1,
        "second rung fills after the first: {ev:?}"
    );
    assert_eq!(ev.fills[0].rung_index, 2);
    assert!(state.rungs(Side::Long)[1].filled);
}

#[test]
fn unorderable_size_is_a_clean_hold_not_a_fill() {
    // A floor-unorderable spec must not mark the rung filled and must emit no
    // event, so paper and live stay aligned.
    let mut tight = opts();
    tight.spec = Some(ContractSpec {
        min_qty: 1_000_000,
        qty_step: 1_000,
    });
    let mut state = LadderState::fresh(micros(50.0), cfg());
    let _ = advance_bar(
        &mut state,
        &ctx(100.0, 100.0, 100.0, 100.0, 1_000_000, 0),
        tight,
    );
    let ev = advance_bar(
        &mut state,
        &ctx(99.0, 99.5, 98.6, 99.0, 2_000_000, 1),
        tight,
    );
    assert_eq!(ev.fills.len(), 0, "floor-unorderable must not fill: {ev:?}");
    assert!(!state.rungs(Side::Long)[0].filled, "rung stays armed");
}

#[test]
fn target_exit_uses_the_frozen_rung_step_and_pays_pnl() {
    let mut state = LadderState::fresh(micros(50.0), cfg());
    let _ = advance_bar(
        &mut state,
        &ctx(100.0, 100.0, 100.0, 100.0, 1_000_000, 0),
        opts(),
    );
    let _ = advance_bar(
        &mut state,
        &ctx(99.0, 99.5, 98.6, 99.0, 2_000_000, 1),
        opts(),
    );
    let fill = &state.rungs(Side::Long)[0];
    assert!(fill.filled, "rung 1 filled on the 98.6 touch");
    let frozen_step = fill.step;
    let entry = fill.entry_price;
    // Target = entry + frozen_step * 1.95. Long fill slipped UP, so the target
    // is above the raw level.
    let target = Money(entry.0 + (frozen_step.0 * 195 / 100));
    let ev = advance_bar(
        &mut state,
        &ctx(
            101.0,
            target.0 as f64 / 1e6 + 0.01,
            100.0,
            101.0,
            4_000_000,
            2,
        ),
        opts(),
    );
    assert_eq!(ev.closes.len(), 1, "target close: {ev:?}");
    assert_eq!(ev.closes[0].reason, CloseReason::Target);
    assert_eq!(ev.closes[0].side, Side::Long);
    assert!(ev.closes[0].pnl.0 > 0, "long target is a win: {ev:?}");
    assert_eq!(state.capital, ev.closes[0].capital_after);
    assert_eq!(state.total_wins, 1);
}

#[test]
fn cannot_target_exit_on_the_fill_bar_unless_opting_out() {
    // Fill and target on the SAME bar, with the exit blocked by conservative
    // intrabar. Both halves need their own state: once the rung is filled it
    // cannot fill again, so the opt-out case needs a fresh ladder.
    let mut blocked = LadderState::fresh(micros(50.0), cfg());
    let _ = advance_bar(
        &mut blocked,
        &ctx(100.0, 100.0, 100.0, 100.0, 1_000_000, 0),
        opts(),
    );
    // Narrow bar: touches ONLY long rung 1 (98.7), and its high reaches the
    // target (entry ~98.7 + frozen step 1.3 * 1.95 ~ 101.2) in one bar.
    let ev = advance_bar(
        &mut blocked,
        &ctx(99.0, 101.2, 98.6, 101.0, 2_000_000, 1),
        opts(),
    );
    assert_eq!(ev.fills.len(), 1, "the fill lands: {ev:?}");
    assert_eq!(ev.closes.len(), 0, "same-bar target blocked: {ev:?}");
    assert!(blocked.rungs(Side::Long)[0].filled, "rung stays open");

    // Opting out allows it. Fresh state so the same bar fills AND closes.
    let mut allowed = LadderState::fresh(micros(50.0), cfg());
    let _ = advance_bar(
        &mut allowed,
        &ctx(100.0, 100.0, 100.0, 100.0, 1_000_000, 0),
        opts(),
    );
    // Target = entry 98.8974 + frozen step 1.3 * 1.95 = 101.4324, so the bar
    // must reach 101.44. That is above the short rung at 101.3, so the short
    // side fills too — assert on the LONG close specifically.
    let mut aggressive = ctx(99.0, 101.5, 98.6, 101.0, 2_000_000, 1);
    aggressive.conservative_intrabar = false;
    let ev = advance_bar(&mut allowed, &aggressive, opts());
    let long_closes: Vec<_> = ev.closes.iter().filter(|c| c.side == Side::Long).collect();
    assert_eq!(
        long_closes.len(),
        1,
        "opt-out allows the same-bar exit: {ev:?}"
    );
    assert_eq!(long_closes[0].reason, CloseReason::Target);
}

#[test]
fn max_hold_closes_at_the_clock_not_the_price() {
    // maxHoldBars 39 x 15m = 585 min. Fire on the bar where the clock runs out.
    let mut state = LadderState::fresh(micros(50.0), cfg());
    let _ = advance_bar(
        &mut state,
        &ctx(100.0, 100.0, 100.0, 100.0, 1_000_000, 0),
        opts(),
    );
    let _ = advance_bar(
        &mut state,
        &ctx(99.0, 99.5, 98.6, 99.0, 1_900_000, 1),
        opts(),
    );
    assert!(state.rungs(Side::Long)[0].filled, "rung filled at 98.6");
    // Just under the clock: 584 min elapsed.
    // entry_ts is 1_900_000, so 584 min lands at 36_940_000 and 585 min at
    // 37_000_000. The clock is measured from the FILL, not from zero.
    let ev = advance_bar(
        &mut state,
        &ctx(99.0, 99.0, 98.0, 99.0, 36_939_000, 2),
        opts(),
    );
    assert_eq!(ev.closes.len(), 0, "not yet at max-hold: {ev:?}");
    // At the clock: 585 min elapsed.
    let ev = advance_bar(
        &mut state,
        &ctx(99.0, 99.0, 98.0, 99.0, 37_000_000, 3),
        opts(),
    );
    assert_eq!(ev.closes.len(), 1, "max-hold fires: {ev:?}");
    assert_eq!(ev.closes[0].reason, CloseReason::MaxHold);
    assert_eq!(ev.closes[0].closed_ts_ms, 37_000_000);
}

#[test]
fn pause_decays_before_anything_else() {
    let mut state = LadderState::fresh(micros(50.0), cfg());
    state.paused = 2;
    let before = state.rungs(Side::Long).len();
    let ev = advance_bar(
        &mut state,
        &ctx(100.0, 100.0, 100.0, 100.0, 1_000_000, 0),
        opts(),
    );
    assert_eq!(ev.fills.len() + ev.closes.len(), 0, "paused bar is inert");
    assert_eq!(state.paused, 1);
    assert_eq!(
        state.rungs(Side::Long).len(),
        before,
        "no seed while paused"
    );
}

#[test]
fn side_clears_once_all_its_rungs_close() {
    let mut state = LadderState::fresh(micros(50.0), cfg());
    let _ = advance_bar(
        &mut state,
        &ctx(100.0, 100.0, 100.0, 100.0, 1_000_000, 0),
        opts(),
    );
    let _ = advance_bar(
        &mut state,
        &ctx(99.0, 99.5, 98.6, 99.0, 1_900_000, 1),
        opts(),
    );
    assert!(state.rungs(Side::Long)[0].filled, "rung filled at 98.6");
    // Run max-hold out so the single filled rung closes.
    let ev = advance_bar(
        &mut state,
        &ctx(99.0, 99.0, 98.0, 99.0, 37_000_000, 2),
        opts(),
    );
    assert_eq!(ev.closes.len(), 1, "max-hold closes the rung: {ev:?}");
    assert!(state.rungs(Side::Long).is_empty(), "side clears when flat");
    assert_eq!(state.long_base, Money::ZERO, "base resets with the side");
}
