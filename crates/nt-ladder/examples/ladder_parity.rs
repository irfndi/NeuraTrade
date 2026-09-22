// Ladder parity: replay bend/fixtures/ladder-6bar.md through the Rust
// advance_bar and compare against the TS-pinned outcome.
//
// The fixture pins TS `advanceLadderBar` and the backtest twin agreeing to
// ~12dp on the same bars, so a divergence here is a port bug, not a model
// difference. Budgets are printed next to the measured max delta, the same
// shape as nt-grid's grid_parity.rs.
//
// Fixture bars (O/H/L/C, ts = i*900000): never edit bars 0-3 — the V1/V1b
// pins below would silently change.
//   i=0: 100/100/100/100   (seed bar: rungs armed, nothing processed)
//   i=1: 100/101/98.8/99.0 (dip fills long rung1 @99, short rung1 @101)
//   i=2: 99.0/101.2/99.0/100.8 (rally takes both targets @100)
//   i=3: 100.8/100.8/100.8/100.8 (flat re-seed at a new base)
use nt_ladder::{
    BarContext, Candle, CloseReason, LadderConfig, LadderState, Side, SizingOptions, advance_bar,
};
use nt_risk::Money;

fn m(v: f64) -> Money {
    Money((v * 1_000_000.0).round() as i64)
}

/// Fixture knobs (test-file `baseOptions`).
fn cfg() -> LadderConfig {
    LadderConfig {
        grid_step_bp: 100, // gridStepPct 1.0
        grid_max_grids: 5,
        grid_pause_after_loss_bars: 0,
        rungs: 2,
        target_ratio_x100: 100, // targetRatio 1.0
        only_with_trend: false,
        chop_gate_adx: 0,
        max_hold_bars: 0, // unused in this fixture
        stop_ratio_x100: 0,
        conservative_intrabar: true,
    }
}

/// Sizing options matching the fixture: leverage 1 (V1) or 3 (V1b), no
/// contract spec (fixture has no spec — TS `baseOptions` sets none), so the
/// specs-absent branch runs and qty is raw capital/price.
fn opts(leverage: i64) -> SizingOptions {
    SizingOptions {
        max_position_pct: 100, // positionFraction 1
        max_notional_pct: None,
        rungs: 2,
        leverage,
        fully_dynamic: false,
        max_leverage: 10,
        spec: None,
        // Fixture feePct is 0.05 percent = 5bp. No cross term.
        maker_fee_bp: 5,
        taker_exit_fee_bp: None,
        live_entry_cross_bps: 0,
    }
}

fn bar(i: u64, o: f64, h: f64, l: f64, c: f64) -> BarContext {
    BarContext {
        bar_index: i,
        candle: Candle {
            open: m(o),
            high: m(h),
            low: m(l),
            close: m(c),
            ts_ms: (i * 900_000) as i64,
        },
        step: m(o * 0.01),
        slippage_bp: 0, // slippageBps 0
        rung_count: 2,
        target_ratio_x100: 100,
        max_hold_bars: 0,
        ms_per_bar: 900_000,
        conservative_intrabar: true,
    }
}

struct Run {
    final_capital: f64,
    wins: u64,
    losses: u64,
    fills_by_bar: Vec<usize>,
    closes_by_bar: Vec<usize>,
    close_reasons: Vec<CloseReason>,
    max_qty_delta_units: f64,
}

fn replay(leverage: i64) -> Run {
    let mut state = LadderState::fresh(m(100.0), cfg());
    let mut fills_by_bar = Vec::new();
    let mut closes_by_bar = Vec::new();
    let mut close_reasons = Vec::new();
    let mut max_qty_delta_units = 0.0f64;

    for i in 0..4u64 {
        // TS's full pass starts at bar index 1 (bar 0 is the seed/prev
        // reference). advance_bar is a single-bar mutator, so replay the same
        // shape: bar 0 seeds, bars 1-3 are processed.
        if i == 0 {
            let ev = advance_bar(
                &mut state,
                &bar(0, 100.0, 100.0, 100.0, 100.0),
                opts(leverage),
            );
            assert_eq!(ev.fills.len(), 0, "seed bar must not fill");
            assert_eq!(ev.closes.len(), 0, "seed bar must not close");
        } else {
            let bars = [
                (1u64, 100.0, 101.0, 98.8, 99.0),
                (2, 99.0, 101.2, 99.0, 100.8),
                (3, 100.8, 100.8, 100.8, 100.8),
            ];
            let (idx, o, h, l, c) = bars.iter().find(|(x, ..)| *x == i).unwrap();
            let ev = advance_bar(&mut state, &bar(*idx, *o, *h, *l, *c), opts(leverage));
            // Fixture pins bar-1 qty: long 50/99, short 50/101. Compare against
            // the EXACT ratio, not the 8-significant-digit value the fixture
            // prints — TS's own value is Decimal-exact, and my earlier
            // expectation was the truncated print.
            for f in &ev.fills {
                let expected = match (f.side, f.rung_index) {
                    (Side::Long, 1) => 50.0 / 99.0,
                    (Side::Short, 1) => 50.0 / 101.0,
                    _ => 0.0,
                };
                if expected > 0.0 {
                    let got = f.qty as f64 / 1e6;
                    max_qty_delta_units = max_qty_delta_units.max((got - expected).abs());
                }
            }
            fills_by_bar.push(ev.fills.len());
            closes_by_bar.push(ev.closes.len());
            close_reasons.extend(ev.closes.iter().map(|c| c.reason));
        }
    }

    Run {
        final_capital: state.capital.0 as f64 / 1e6,
        wins: state.total_wins,
        losses: state.total_losses,
        fills_by_bar,
        closes_by_bar,
        close_reasons,
        max_qty_delta_units,
    }
}

fn main() {
    // ---- V1 (leverage 1): the fixture's primary pin ----
    let v1 = replay(1);
    println!("V1 (leverage 1)");
    println!("  fills by bar   {:?}  (TS: [2, 0, 0])", v1.fills_by_bar);
    println!("  closes by bar  {:?}  (TS: [0, 2, 0])", v1.closes_by_bar);
    println!(
        "  close reasons  {:?}  (TS: [Target, Target])",
        v1.close_reasons
    );
    println!("  wins/losses    {}/{}  (TS: 2/0)", v1.wins, v1.losses);
    println!(
        "  final capital  {:.12}  (TS: 100.902_125_210_021_002_f64)",
        v1.final_capital
    );

    assert_eq!(v1.fills_by_bar, vec![2, 0, 0], "V1 fill shape");
    assert_eq!(v1.closes_by_bar, vec![0, 2, 0], "V1 close shape");
    assert_eq!(
        v1.close_reasons,
        vec![CloseReason::Target, CloseReason::Target],
        "V1 reasons"
    );
    assert_eq!((v1.wins, v1.losses), (2, 0), "V1 win/loss");

    // Capital budget. `Money` is micro-USDT (i64), so it holds 6 decimals;
    // TS carries arbitrary-precision Decimal and its pin has 15. The
    // representation ceiling is therefore 1e-6 USDT, NOT a port tolerance —
    // a tighter budget would fail on correct code. The event shape, reasons,
    // win/loss and qty all match exactly, which is the real parity claim.
    let v1_delta = (v1.final_capital - 100.902_125_210_021_f64).abs();
    println!("  capital delta  {v1_delta:.3e} USDT  (budget 1e-6 = Money's 6dp ceiling)");
    assert!(
        v1_delta < 1e-6,
        "V1 capital drifted past the micro-USDT ceiling: {v1_delta:.3e}"
    );

    // Qty budget: the fixture pins 50/99 and 50/101 exactly.
    println!(
        "  max qty delta  {:.3e} base units  (budget 1e-6 = Money's 6dp ceiling)",
        v1.max_qty_delta_units
    );
    assert!(
        v1.max_qty_delta_units < 1e-6,
        "V1 qty drifted: {}",
        v1.max_qty_delta_units
    );

    // ---- V1b (leverage 3): same event shape, different capital ----
    let v1b = replay(3);
    println!("V1b (leverage 3)");
    println!("  fills by bar   {:?}  (TS: [2, 0, 0])", v1b.fills_by_bar);
    println!("  closes by bar  {:?}  (TS: [0, 2, 0])", v1b.closes_by_bar);
    println!(
        "  final capital  {:.12}  (TS: 102.71852683)",
        v1b.final_capital
    );
    assert_eq!(v1b.fills_by_bar, vec![2, 0, 0], "V1b fill shape");
    assert_eq!(v1b.closes_by_bar, vec![0, 2, 0], "V1b close shape");
    let v1b_delta = (v1b.final_capital - 102.71852683).abs();
    println!("  capital delta  {v1b_delta:.3e} USDT  (budget 1e-6)");
    assert!(v1b_delta < 1e-6, "V1b capital drifted: {v1b_delta:.3e}");

    println!(
        "ladder_parity: V1 capital delta {v1_delta:.3e} / V1b {v1b_delta:.3e} — event shapes and pins match"
    );
}
