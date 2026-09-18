// ponytail: pump-catch proof — grid-only vs sleeves on a vertical rally
// (not a test suite; examples/*.rs runtime-compare pattern). The rally
// mimics the 2026-09-18 BTC +5% vertical: tight 0.3% dips (grid SILENT,
// low never reaches buy_level) with closes staircasing +1.5%/bar. Grid
// alone never fires; Breakout+Trend+Momentum carry the combined vote LONG
// from the first closed thrust bar — entries ride the move, not the top.
use nt_grid::{
    FilterKind, PaperEngineConfig, SleeveCfg, filter_vote, run_paper_engine, run_sleeve_backtest,
};
use nt_market::Candle;
use nt_risk::{Money, RiskLimits};

fn c(ts: i64, open: i64, high: i64, low: i64, close: i64) -> Candle {
    Candle {
        open_ts_ms: ts,
        open: Money(open),
        high: Money(high),
        low: Money(low),
        close: Money(close),
        volume_base_micros: 1_000_000,
    }
}

fn cfg() -> PaperEngineConfig {
    PaperEngineConfig {
        step_bp: 100,
        target_ratio_x100: 100,
        grid_max_grids: 2,
        slippage_bps: 50,
        max_position_size_pct: 10,
        fee_bp: 6,
    }
}

// Vertical rally: closes +1.5%/bar, wicks tight (low only 0.3% under open,
// high 0.5% over) — grid rung touch (open +/-1%) never triggers.
fn rally() -> Vec<Candle> {
    let opens = [
        100_000_000,
        101_500_000,
        103_000_000,
        104_500_000,
        106_000_000,
        107_500_000,
        109_000_000,
        110_500_000,
    ];
    let mut v = Vec::new();
    for (b, o) in opens.iter().enumerate() {
        v.push(c(
            b as i64,
            *o,
            *o + *o * 5 / 1000,
            *o - *o * 3 / 1000,
            *o + *o * 4 / 1000,
        ));
    }
    v
}

fn main() {
    let base = cfg();
    let bars = rally();
    let capital = Money::usdt(1000);
    let limits = RiskLimits::live();

    // Grid-only: rung touch never fires on tight-wick verticals.
    let (gevents, _) = run_paper_engine(&bars, &base, capital, &limits);
    assert_eq!(gevents.len(), 0, "grid-only flat through the rally");

    // Sleeves: breakout + trend + momentum all LONG from bar 2.
    let sleeves = vec![
        SleeveCfg {
            kind: FilterKind::Grid,
            weight_bp: 5_000,
            max_leverage: 10,
            step_bp: 100,
            lookback: 0,
        },
        SleeveCfg {
            kind: FilterKind::Breakout,
            weight_bp: 3_000,
            max_leverage: 10,
            step_bp: 100,
            lookback: 2,
        },
        SleeveCfg {
            kind: FilterKind::Trend,
            weight_bp: 2_000,
            max_leverage: 10,
            step_bp: 100,
            lookback: 3,
        },
        SleeveCfg {
            kind: FilterKind::Momentum,
            weight_bp: 4_000,
            max_leverage: 10,
            step_bp: 100,
            lookback: 2,
        },
    ];
    for b in [2usize, 3, 4] {
        let mom = filter_vote(FilterKind::Momentum, &bars, b, &sleeves[3]);
        assert_eq!(format!("{mom:?}"), "Long", "bar {b}: momentum LONG");
    }
    let (events, ledger) = run_sleeve_backtest(&bars, &base, &sleeves, capital, &limits);
    let first = events
        .iter()
        .find(|e| format!("{:?}", e.reason) == "Entry")
        .expect("sleeves open on the rally");
    // First thrust bar (bar 2), not the top: entry bar < last bar.
    assert!(first.bar <= 3, "entry rides the move (bar {})", first.bar);
    assert!(first.leverage >= 1, "leverage sane");
    println!(
        "pump_catch ok: grid=0 fills, sleeves entry bar={} lev={} fills={}",
        first.bar,
        first.leverage,
        ledger.totals().fills
    );
}
