// ponytail: runtime-compare for the sleeves backtester (not a test suite —
// AGENTS.md opt-in keeps new *_test.rs out; examples/*.rs is the pattern).
use nt_grid::{
    FilterKind, PaperEngineConfig, Side, SleeveCfg, account_scaled_leverage_cap, combine_votes,
    conviction_leverage, filter_vote, run_sleeve_backtest,
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

// Rising staircase: closes climb ~1.5%/bar, lows dip 1.2% under open so
// grid LONG joins breakout (new highs) + trend (above 3-SMA) on bars 2+.
fn rising() -> Vec<Candle> {
    let opens = [
        100_000_000,
        101_000_000,
        102_500_000,
        104_000_000,
        105_500_000,
        107_000_000,
        108_500_000,
        110_000_000,
    ];
    let mut v = Vec::new();
    for (b, o) in opens.iter().enumerate() {
        v.push(c(
            b as i64,
            *o,
            *o + *o * 4 / 1000,
            *o - *o * 12 / 1000,
            *o + *o * 3 / 1000,
        ));
    }
    v
}

fn main() {
    let base = cfg();
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
    ];
    let bars = rising();

    // Bars 2+: grid + breakout + trend all LONG (unanimous 10_000).
    for b in 2..8usize {
        let (l, s) = combine_votes(&bars, b, &sleeves);
        assert_eq!((l, s), (10_000, 0), "bar {b}: unanimous LONG");
        let g = filter_vote(FilterKind::Grid, &bars, b, &sleeves[0]);
        let bo = filter_vote(FilterKind::Breakout, &bars, b, &sleeves[1]);
        let t = filter_vote(FilterKind::Trend, &bars, b, &sleeves[2]);
        assert!(
            format!("{g:?}") == "Long" && format!("{bo:?}") == "Long" && format!("{t:?}") == "Long",
            "bar {b}: all filters LONG"
        );
    }
    // Flat bar: grid silent (low never reaches buy_level), no votes.
    let flat = vec![
        c(0, 100_000_000, 100_300_000, 99_800_000, 100_100_000),
        c(1, 100_000_000, 100_200_000, 99_900_000, 100_050_000),
        c(2, 100_000_000, 100_200_000, 99_900_000, 100_050_000),
        c(3, 100_000_000, 100_200_000, 99_900_000, 100_050_000),
    ];
    let grid_only = vec![SleeveCfg {
        kind: FilterKind::Grid,
        weight_bp: 4_000,
        max_leverage: 1,
        step_bp: 100,
        lookback: 0,
    }];
    let (l, _s) = combine_votes(&flat, 3, &grid_only);
    assert_eq!(l, 0, "flat bar: grid silent, no votes");

    // Leverage tiers mirror TS accountScaledLeverageCap exactly.
    assert_eq!(account_scaled_leverage_cap(Money::usdt(100), 1000, 10), 10);
    assert_eq!(account_scaled_leverage_cap(Money::usdt(100), 5000, 10), 5);
    assert_eq!(account_scaled_leverage_cap(Money::usdt(1000), 1000, 99), 25);
    assert_eq!(
        account_scaled_leverage_cap(Money::usdt(10000), 1000, 99),
        50
    );
    assert_eq!(
        account_scaled_leverage_cap(Money::usdt(30000), 1000, 999),
        150
    );
    assert_eq!(conviction_leverage(10, 0), 1);
    assert_eq!(conviction_leverage(10, 5_000), 5);
    assert_eq!(conviction_leverage(10, 10_000), 10);

    // End to end: bar 0 votes grid-only LONG (5000/0, breakout+trend need
    // warmup), so the first entry carries the bar-0 weights at conviction
    // leverage. Later bars vote unanimous once lookbacks fill.
    let capital = Money::usdt(1000);
    let limits = RiskLimits::live();
    let (events, ledger) = run_sleeve_backtest(&bars, &base, &sleeves, capital, &limits);
    assert!(!events.is_empty(), "sleeves opened");
    let first = events
        .iter()
        .find(|e| format!("{:?}", e.reason) == "Entry")
        .unwrap();
    assert_eq!(first.side, Side::Long);
    assert_eq!((first.long_weight_bp, first.short_weight_bp), (5_000, 0));
    assert!(first.leverage > 1, "conviction scales leverage");
    let t = ledger.totals();
    assert_eq!(t.fills as usize, events.len());
    println!("nt-grid sleeves_check ok");
}
