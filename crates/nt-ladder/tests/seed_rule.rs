// ponytail: slice-2 regression tests for the a07b3dd0 seed rule.
// The rule: gates apply to the EMPTY seed only, and a blocked bar must
// NEVER wipe armed rungs. The anti-pattern it replaces is re-deriving
// levels from the current bar's open on every flat bar, which wipes armed
// rungs on any blocked bar (the demo's 220 seedless HOLDs).
use nt_ladder::{
    LadderConfig, LadderState, Rung, SeedContext, SeedOutcome, Side, load, save, seed_bar,
    seed_side,
};
use nt_risk::Money;

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

fn usdt(v: i64) -> Money {
    Money(v * 1_000_000)
}

fn ctx(open: Money) -> SeedContext {
    SeedContext {
        open,
        close: open,
        step: usdt(1),
        trend: None,
        only_with_trend: false,
        chop_gate_active: false,
        drawdown_breached: false,
        rung_count: 2,
    }
}

#[test]
fn keeps_armed_rungs_across_blocked_bars() {
    let mut st = LadderState::fresh(usdt(50), cfg());
    let open = usdt(100);

    let (l, s) = seed_bar(&mut st, &ctx(open));
    assert_eq!(l, SeedOutcome::Seeded);
    assert_eq!(s, SeedOutcome::Seeded);
    let long_levels: Vec<i64> = st.rungs(Side::Long).iter().map(|r| r.level.0).collect();
    assert_eq!(
        long_levels,
        vec![99_000_000, 98_000_000],
        "long rungs sit below the base"
    );
    let short_levels: Vec<i64> = st.rungs(Side::Short).iter().map(|r| r.level.0).collect();
    assert_eq!(
        short_levels,
        vec![101_000_000, 102_000_000],
        "short rungs sit above the base"
    );

    // A blocked bar with a wildly different open must NOT move anything.
    let blocked = SeedContext {
        open: usdt(80),
        drawdown_breached: true,
        ..ctx(open)
    };
    assert_eq!(
        seed_side(&mut st, Side::Long, &blocked),
        SeedOutcome::ArmedRungsKept
    );
    assert_eq!(
        seed_side(&mut st, Side::Short, &blocked),
        SeedOutcome::ArmedRungsKept
    );
    let after: Vec<i64> = st.rungs(Side::Long).iter().map(|r| r.level.0).collect();
    assert_eq!(
        after, long_levels,
        "armed rung levels must survive a blocked bar"
    );
    assert_eq!(
        st.long_base, open,
        "the base must not re-anchor on a blocked bar"
    );
}

#[test]
fn gates_block_only_the_empty_seed() {
    let mut st = LadderState::fresh(usdt(50), cfg());

    // Chop gate with nothing armed: stays empty, no base anchored.
    let chop = SeedContext {
        chop_gate_active: true,
        ..ctx(usdt(100))
    };
    assert_eq!(
        seed_side(&mut st, Side::Long, &chop),
        SeedOutcome::BlockedEmpty
    );
    assert!(st.rungs(Side::Long).is_empty());
    assert_eq!(st.long_base, Money::ZERO);

    // Once armed, the same chop gate cannot clear them.
    assert_eq!(
        seed_side(&mut st, Side::Long, &ctx(usdt(100))),
        SeedOutcome::Seeded
    );
    assert_eq!(
        seed_side(&mut st, Side::Long, &chop),
        SeedOutcome::ArmedRungsKept
    );
    assert_eq!(st.rungs(Side::Long).len(), 2);
}

#[test]
fn a_filled_rung_freezes_the_whole_side() {
    let mut st = LadderState::fresh(usdt(50), cfg());
    seed_bar(&mut st, &ctx(usdt(100)));
    let levels: Vec<i64> = st.rungs(Side::Long).iter().map(|r| r.level.0).collect();

    st.long_rungs[0].filled = true;
    st.long_rungs[0].filled_qty = 1_000_000;

    // Even an allowed bar with a different open cannot reseed a side with a fill.
    let later = SeedContext {
        open: usdt(200),
        ..ctx(usdt(200))
    };
    assert_eq!(
        seed_side(&mut st, Side::Long, &later),
        SeedOutcome::FilledRungsPresent
    );
    let after: Vec<i64> = st.rungs(Side::Long).iter().map(|r| r.level.0).collect();
    assert_eq!(
        after, levels,
        "a filled rung freezes every level on that side"
    );
    assert_eq!(st.long_base, usdt(100), "and the base");
}

#[test]
fn trend_gate_is_side_directional() {
    let mut st = LadderState::fresh(usdt(50), cfg());
    let base = SeedContext {
        close: usdt(90),
        trend: Some(usdt(95)),
        only_with_trend: true,
        ..ctx(usdt(100))
    };

    // Close 90 < trend 95 blocks a long, allows a short.
    assert_eq!(
        seed_side(&mut st, Side::Long, &base),
        SeedOutcome::BlockedEmpty
    );
    assert_eq!(seed_side(&mut st, Side::Short, &base), SeedOutcome::Seeded);

    // With the short side armed, a bar that would have blocked the seed now
    // must keep the rungs instead.
    let flipped = SeedContext {
        close: usdt(99),
        ..base
    };
    assert_eq!(
        seed_side(&mut st, Side::Short, &flipped),
        SeedOutcome::ArmedRungsKept
    );
    assert_eq!(
        st.short_rungs[0].level.0, 101_000_000,
        "level anchored at seed time"
    );
}

#[test]
fn seeded_state_round_trips_and_survives_a_blocked_bar() {
    // The rule must hold across a persist/reload, not just in memory: the
    // reloaded state carries armed rungs, so a blocked bar keeps them.
    let mut st = LadderState::fresh(usdt(50), cfg());
    seed_bar(&mut st, &ctx(usdt(100)));
    st.bars_consumed = 7;
    st.last_ts_ms = Some(1_767_350_400_000);

    let mut reloaded = load(&save(&st)).expect("round-trip");
    let levels: Vec<i64> = reloaded
        .rungs(Side::Long)
        .iter()
        .map(|r| r.level.0)
        .collect();

    let blocked = SeedContext {
        open: usdt(60),
        drawdown_breached: true,
        ..ctx(usdt(60))
    };
    assert_eq!(
        seed_side(&mut reloaded, Side::Long, &blocked),
        SeedOutcome::ArmedRungsKept
    );
    let after: Vec<i64> = reloaded
        .rungs(Side::Long)
        .iter()
        .map(|r| r.level.0)
        .collect();
    assert_eq!(after, levels, "reloaded armed rungs survive a blocked bar");
    assert_eq!(
        reloaded.bars_consumed, 7,
        "bookkeeping survived the round-trip"
    );

    let _ = Rung::armed(1, Side::Long, Money::ZERO, Money::ZERO);
}
