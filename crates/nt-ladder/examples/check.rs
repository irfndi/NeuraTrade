// ponytail: slice-1 state fidelity check — resume round-trip, version
// rejection, rung bookkeeping. No fills, no seeding, no TS parity: those are
// slices 2-4. Money fields are the only ones assertable bit-exact against TS
// (levels/steps/bases are float there and need slice 3's rounding policy).
use nt_ladder::{LadderConfig, LadderState, ResumeError, Rung, Side, load, save};
use nt_risk::Money;

fn cfg() -> LadderConfig {
    // Champion-soak knobs, in the units LadderConfig declares.
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

fn main() {
    // 1. Fresh state round-trips unchanged.
    let fresh = LadderState::fresh(usdt(50), cfg());
    let back = load(&save(&fresh)).expect("fresh round-trip");
    assert_eq!(fresh, back, "fresh state must round-trip exactly");
    assert!(fresh.is_flat(), "fresh state has no rungs");
    assert_eq!(fresh.open_rung_count(), 0);
    assert_eq!(fresh.last_ts_ms, None);
    println!(
        "fresh round-trip ok (capital {}, peak {})",
        fresh.capital.0, fresh.peak_capital.0
    );

    // 2. A state with armed + filled rungs on both sides round-trips, and
    //    filled_qty survives so a close sends the entry size.
    let mut st = LadderState::fresh(usdt(50), cfg());
    st.long_base = usdt(100);
    st.short_base = usdt(101);
    st.paused = 3;
    st.bars_consumed = 417;
    st.last_ts_ms = Some(1_767_350_400_000);
    st.long_rungs = vec![
        Rung::armed(1, Side::Long, usdt(99), usdt(1)),
        Rung::armed(2, Side::Long, usdt(98), usdt(1)),
    ];
    // Rung 1 filled: entry price post-slippage, absolute bar, ts, qty.
    st.long_rungs[0].filled = true;
    st.long_rungs[0].entry_price = Money(99_495_000);
    st.long_rungs[0].entry_bar = 416;
    st.long_rungs[0].entry_ts_ms = 1_767_350_400_000;
    st.long_rungs[0].filled_qty = 1_005_075;
    st.short_rungs = vec![Rung::armed(1, Side::Short, usdt(102), usdt(1))];

    let back = load(&save(&st)).expect("populated round-trip");
    assert_eq!(st, back, "populated state must round-trip exactly");
    assert_eq!(back.open_rung_count(), 1, "only rung 1 is open");
    assert_eq!(back.long_rungs[0].filled_qty, 1_005_075);
    assert_eq!(back.long_rungs[0].entry_bar, 416, "absolute bar survives");
    assert_eq!(back.short_rungs.len(), 1);
    assert_eq!(back.rungs(Side::Short)[0].level, usdt(102));
    assert!(!back.is_flat(), "armed rungs mean not flat");
    println!(
        "populated round-trip ok (open_rungs={} long={} short={} paused={} bars={})",
        back.open_rung_count(),
        back.long_rungs.len(),
        back.short_rungs.len(),
        back.paused,
        back.bars_consumed
    );

    // 3. An unfilled rung with a zero qty is NOT open even if `filled` was
    //    somehow set — TS's optional filledQty makes the bare flag unsafe.
    let mut bogus = st.clone();
    bogus.short_rungs[0].filled = true;
    bogus.short_rungs[0].filled_qty = 0;
    let back = load(&save(&bogus)).expect("round-trip");
    assert_eq!(back.open_rung_count(), 1, "zero-qty rung is not open");
    assert!(back.short_rungs[0].is_filled(), "flag itself survives");
    assert!(!back.short_rungs[0].is_open());
    println!("zero-qty guard ok (filled but not open)");

    // 4. A grid resume (v2, from nt-cli) must be rejected, not parsed.
    let grid_v2 = "v2\nclosed_realized 0\npeak 1000000000\nflat\nbar_offset 0\nevent_count 0\ncum 0 0 0\nday_index 20456\nday_fills 0\nday_start 1000000000\n";
    match load(grid_v2) {
        Err(ResumeError::BadVersion(v)) => {
            println!("grid v2 rejected loudly (version {v:?})")
        }
        other => panic!("grid resume must fail with BadVersion, got {other:?}"),
    }
    match load("") {
        Err(ResumeError::BadVersion(v)) => println!("empty file rejected (version {v:?})"),
        other => panic!("empty file must fail with BadVersion, got {other:?}"),
    }

    // 5. A malformed rung line fails rather than silently dropping.
    let broken = "ladder v3\ncapital 50000000\nrung 1 long 99000000\n".to_string();
    match load(&broken) {
        Err(ResumeError::BadRungLine(_)) => println!("short rung line rejected"),
        other => panic!("short rung line must fail, got {other:?}"),
    }

    println!("nt-ladder check ok");
}
