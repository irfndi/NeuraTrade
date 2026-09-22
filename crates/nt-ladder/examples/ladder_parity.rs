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
        max_drawdown_pct: 0, // fixture has no maxDrawdownPct → TS default 0
    }
}

/// Same bar under the drawdown fixture's knob (`maxDrawdownPct 8`).
fn bar_dd(i: u64, o: f64, h: f64, l: f64, c: f64) -> BarContext {
    BarContext {
        max_drawdown_pct: 8,
        ..bar(i, o, h, l, c)
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

/// Boundary fixture (bend/fixtures/ladder-boundary.md): one rung fills, price
/// craters through the legacy boundary, the WHOLE ladder force-closes at the
/// boundary, losses count, the post-loss pause engages, and a further bar
/// just ticks the pause down. `stop_ratio_x100 = 0` is the fixture's legacy
/// boundary — TS-exact on the exit-bar step, which is what pins 93.049.
/// `158` (the soak's stopRatio) instead proves the LADDER-STEP-ANCHOR
/// deviation: boundary from the rung's FROZEN entry-time step (96.42), not
/// TS's exit-bar step (96.424774) — a deliberate divergence, so it carries
/// no TS pin by design.
struct BoundaryRun {
    fills: Vec<usize>,
    closes: Vec<usize>,
    exit_prices: Vec<i64>,
    fill_qty_deltas: f64,
    final_capital: f64,
    wins: u64,
    losses: u64,
    paused_after_stop: u64,
    paused_after_free_tick: u64,
    free_tick_closes: usize,
    long_rungs_left: usize,
    long_base: i64,
}

fn replay_boundary(stop_ratio_x100: i64) -> BoundaryRun {
    let cfg = LadderConfig {
        grid_pause_after_loss_bars: 3,
        stop_ratio_x100,
        ..cfg()
    };
    let mut state = LadderState::fresh(m(100.0), cfg);
    let bars = [
        (0u64, 100.0, 100.0, 100.0, 100.0),
        (1, 100.0, 99.5, 98.9, 99.3),
        (2, 99.3, 99.3, 90.0, 90.0),
    ];
    let mut fills = Vec::new();
    let mut closes = Vec::new();
    let mut fill_qty_deltas = 0.0f64;
    let mut exit_prices = Vec::new();

    for (i, o, h, l, c) in bars {
        let ev = advance_bar(&mut state, &bar(i, o, h, l, c), opts(1));
        fills.push(ev.fills.len());
        closes.push(ev.closes.len());
        for f in &ev.fills {
            // Fixture pins rung1 qty 50/99 (bar 1) and rung2 qty 50/98
            // (bar 2, capital still 100 — nothing closed before the crater).
            let expected = if f.rung_index == 1 {
                50.0 / 99.0
            } else {
                50.0 / 98.0
            };
            fill_qty_deltas = fill_qty_deltas.max((f.qty as f64 / 1e6 - expected).abs());
        }
        exit_prices.extend(ev.closes.iter().map(|c| c.exit_price.0));
    }
    let paused_after_stop = state.paused;
    // Only the LONG side must clear; the short rungs stay armed-but-empty
    // throughout (fixture: "Short rungs stay armed-but-empty").
    let long_rungs_left = state.rungs(Side::Long).len();
    let long_base = state.long_base.0;

    // Free tick: replay bar 0 while paused — no events, counter decrements.
    let free = advance_bar(&mut state, &bar(0, 100.0, 100.0, 100.0, 100.0), opts(1));

    BoundaryRun {
        fills,
        closes,
        exit_prices,
        fill_qty_deltas,
        final_capital: state.capital.0 as f64 / 1e6,
        wins: state.total_wins,
        losses: state.total_losses,
        paused_after_stop,
        paused_after_free_tick: state.paused,
        free_tick_closes: free.closes.len(),
        long_rungs_left,
        long_base,
    }
}

/// Drawdown fixture (bend/fixtures/ladder-drawdown.md): a flat account 9%
/// under peak with an 8% kill line must re-anchor peak to capital and take
/// the post-lock pause (the ENA-shadow latch fix), keep armed rungs across
/// pause ticks (a07b3dd0), and fill again once the pause expires. Starts at
/// bar 1 on fresh state — TS's exact shape — so the pinned `peak 91 /
/// paused 3 AFTER BAR 1` lands on the same bar. Returns (capital,
/// per-bar (fills, closes)).
fn replay_drawdown() -> (f64, Vec<(usize, usize)>) {
    let cfg = LadderConfig {
        grid_pause_after_loss_bars: 3,
        ..cfg()
    };
    let mut state = LadderState::fresh(m(100.0), cfg);
    state.capital = m(91.0); // opens 9% under peak 100 — past the 8% line
    let bars = [
        (1u64, 100.0, 100.1, 99.9, 100.0),
        (2, 100.0, 100.1, 99.9, 100.0),
        (3, 100.0, 100.1, 99.9, 100.0),
        (4, 100.0, 100.1, 99.9, 100.0),
        (5, 100.0, 99.0, 98.9, 99.3),
    ];
    let mut events = Vec::new();

    for (i, o, h, l, c) in bars {
        let ev = advance_bar(&mut state, &bar_dd(i, o, h, l, c), opts(1));
        events.push((ev.fills.len(), ev.closes.len()));
        match i {
            1 => {
                // Re-anchor fires on THIS bar (flat, 9% >= 8%): peak 100 -> 91,
                // pause 3 — then the gates see 0% drawdown and seed anyway,
                // all within the same call (TS block order).
                assert_eq!(
                    state.peak_capital.0, 91_000_000,
                    "peak re-anchored to capital"
                );
                assert_eq!(state.paused, 3, "pause taken on the re-anchor bar");
                assert_eq!(state.rungs(Side::Long).len(), 2, "long rungs armed");
                assert_eq!(state.rungs(Side::Short).len(), 2, "short rungs armed");
                assert_eq!(state.capital.0, 91_000_000, "capital untouched");
            }
            2..=4 => {
                assert_eq!(events[i as usize - 1], (0, 0), "paused bar emits nothing");
                assert!(state.paused >= 1 || i == 4, "pause still ticking");
            }
            5 => {
                assert_eq!(ev.fills.len(), 1, "reseeded rung fills");
                assert_eq!(ev.closes.len(), 0, "no close on the fill bar");
                let rung = state.rungs(Side::Long)[0];
                assert!(rung.filled, "rung 1 filled");
                assert_eq!(rung.entry_price.0, 99_000_000, "entry 99");
                assert_eq!(rung.entry_bar, 5, "entryBar 5 (window index)");
                assert_eq!(rung.entry_ts_ms, 4_500_000, "entryTimestamp");
                assert!(
                    (ev.fills[0].qty as f64 / 1e6 - 45.5 / 99.0).abs() < 1e-6,
                    "qty = 45.5/99: {}",
                    ev.fills[0].qty as f64 / 1e6
                );
                assert!(!state.rungs(Side::Long)[1].filled, "rung 2 stays armed");
                assert_eq!(state.base(Side::Long).0, 100_000_000, "longBase 100");
                assert_eq!(state.total_wins + state.total_losses, 0, "no closes");
            }
            _ => unreachable!(),
        }
        if i == 4 {
            assert_eq!(state.paused, 0, "pause expired after bar 4");
            assert_eq!(
                state.rungs(Side::Long).len(),
                2,
                "armed rungs survive the pause ticks"
            );
        }
    }
    assert_eq!(state.capital.0, 91_000_000, "capital still 91 after bar 5");
    (state.capital.0 as f64 / 1e6, events)
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

    // ---- Boundary (ladder-boundary.md): whole-ladder stop-out + pause ----
    let b = replay_boundary(0);
    // Shapes include the seed bar (index 0, no events); TS's vectors start at
    // bar 1 because its pass never processes bar 0.
    println!("Boundary (legacy stop, pause 3)");
    println!(
        "  fills/closes   {:?}/{:?}  (TS bars 1-2: [1, 1]/[0, 2])",
        b.fills, b.closes
    );
    println!(
        "  exit prices    {:?}  (TS: [93.049, 93.049])",
        b.exit_prices
    );
    println!("  wins/losses    {}/{}  (TS: 0/2)", b.wins, b.losses);
    println!(
        "  final capital  {:.15}  (TS: 94.447135770975057503)",
        b.final_capital
    );
    println!(
        "  paused         {} -> {} after free tick, {} closes (TS: 3 -> 2, 0)",
        b.paused_after_stop, b.paused_after_free_tick, b.free_tick_closes
    );

    assert_eq!(
        b.fills,
        vec![0, 1, 1],
        "boundary fill shape (seed, rung1 bar1, rung2 bar2)"
    );
    assert_eq!(b.closes, vec![0, 0, 2], "boundary close shape");
    assert!(
        b.exit_prices.iter().all(|p| *p == 93_049_000),
        "legacy boundary must be the TS-exact exit-bar-step 93.049: {:?}",
        b.exit_prices
    );
    assert_eq!((b.wins, b.losses), (0, 2), "boundary win/loss");
    assert_eq!(b.long_rungs_left, 0, "long ladder cleared");
    assert_eq!(b.long_base, 0, "side base reset");
    assert_eq!(b.paused_after_stop, 3, "post-loss pause");
    assert_eq!(b.paused_after_free_tick, 2, "free tick consumes one pause");
    assert_eq!(b.free_tick_closes, 0, "paused bar emits nothing");
    let b_delta = (b.final_capital - 94.447_135_770_975_05).abs();
    println!("  capital delta  {b_delta:.3e} USDT  (budget 1e-6)");
    assert!(b_delta < 1e-6, "boundary capital drifted: {b_delta:.3e}");
    assert!(
        b.fill_qty_deltas < 1e-6,
        "boundary fill qty drifted: {}",
        b.fill_qty_deltas
    );

    // ---- Boundary, stopRatio 1.58: the frozen-step (LADDER-STEP-ANCHOR)
    // deviation. min entry 98 - rung.step 1.0 * 1.58 = 96.42; TS would use
    // the exit-bar step (99.3 * 1% * 1.58) and give 96.424774.
    let s = replay_boundary(158);
    println!("Boundary (stopRatio 1.58, frozen step)");
    println!(
        "  exit prices    {:?}  (frozen-step 96.42; TS exit-bar form 96.424774)",
        s.exit_prices
    );
    assert_eq!(s.closes, vec![0, 0, 2], "stopRatio close shape");
    assert!(
        s.exit_prices.iter().all(|p| *p == 96_420_000),
        "stopRatio boundary must use the frozen rung step: {:?}",
        s.exit_prices
    );
    assert_eq!(s.paused_after_stop, 3, "stopRatio pause");

    // ---- Drawdown (ladder-drawdown.md): re-anchor + paused-then-retry ----
    let (dd_capital, dd_events) = replay_drawdown();
    println!("Drawdown (capital 91, peak 100, maxDrawdownPct 8)");
    println!("  events/bar     {dd_events:?}  (TS: no events bars 1-4, 1 fill bar 5)");
    println!("  final capital  {dd_capital}  (TS: 91, untouched)");

    println!(
        "ladder_parity: V1 {v1_delta:.3e} / V1b {v1b_delta:.3e} / boundary {b_delta:.3e} / \
         drawdown {dd_capital} — event shapes and pins match"
    );
}
