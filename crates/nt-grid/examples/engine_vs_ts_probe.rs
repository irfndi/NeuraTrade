// ponytail: additive TS-parity probe — replays paper_engine_check.rs's fixed
// 6-candle fixture through run_paper_engine and pins the TS-expected
// hand-derived numbers (fills=4 gross=-1505999 fees=242113 net=-1748112).
// Prints a per-fill table for eyeball; asserts do the pinning.
use nt_execution::HONEST_TAKER_EXIT_BP;
use nt_grid::{FillReason, PaperEngineConfig, Side, run_paper_engine};
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

fn main() {
    // Same 6-candle fixture as paper_engine_check.rs (bars 2h apart so the
    // throughput window never halts mid-fixture).
    let h = 3_600_000;
    let candles = vec![
        c(0, 100_000_000, 100_300_000, 99_800_000, 100_100_000),
        c(2 * h, 100_000_000, 100_500_000, 98_500_000, 99_500_000),
        c(4 * h, 100_500_000, 100_600_000, 99_900_000, 100_400_000),
        c(6 * h, 100_000_000, 101_600_000, 99_300_000, 101_200_000),
        c(8 * h, 100_800_000, 102_600_000, 100_200_000, 102_300_000),
        c(10 * h, 102_300_000, 102_500_000, 102_100_000, 102_200_000),
    ];

    let cfg = PaperEngineConfig {
        step_bp: 100,
        target_ratio_x100: 100,
        grid_max_grids: 2,
        slippage_bps: 50,
        max_position_size_pct: 10,
        fee_bp: HONEST_TAKER_EXIT_BP,
    };
    let capital = Money::usdt(1000);
    let limits = RiskLimits::live();

    let (events, ledger) = run_paper_engine(&candles, &cfg, capital, &limits);

    // Hand-derived TS-expected fills from paper_engine_check.rs header:
    // (bar, side, reason, price, qty_base_micros, fee).
    let expected: [(usize, Side, FillReason, i64, i64, i64); 4] = [
        (
            1,
            Side::Long,
            FillReason::Entry,
            99_495_000,
            1_005_075,
            59_999,
        ),
        (
            2,
            Side::Long,
            FillReason::Target,
            100_500_000,
            -1_005_075,
            60_606,
        ),
        (
            3,
            Side::Short,
            FillReason::Entry,
            100_495_000,
            -995_074,
            59_999,
        ),
        (
            4,
            Side::Short,
            FillReason::Stop,
            103_023_555,
            995_074,
            61_509,
        ),
    ];

    println!("bar side reason | price (rust/ts) | qty (rust/ts) | fee (rust/ts)");
    assert_eq!(events.len(), expected.len(), "expected 4 fills: {events:?}");
    for (e, x) in events.iter().zip(expected.iter()) {
        let side = match e.side {
            Side::Long => "long",
            Side::Short => "short",
        };
        let reason = match e.reason {
            FillReason::Entry => "entry",
            FillReason::Target => "target",
            FillReason::Stop => "stop",
        };
        println!(
            "{side:>5} {reason:>6} bar={} | price {} (exp {}) | qty {} (exp {}) | fee {} (exp {})",
            e.bar, e.price.0, x.3, e.qty_base_micros, x.4, e.fee.0, x.5,
        );
        assert_eq!(e.bar, x.0);
        assert_eq!(e.side, x.1);
        assert_eq!(e.reason, x.2);
        assert_eq!(e.price, Money(x.3));
        assert_eq!(e.qty_base_micros, x.4);
        assert_eq!(e.fee, Money(x.5));
    }

    let totals = ledger.totals();
    println!(
        "fills rust={} ts=4 | gross rust={} ts=-1505999 | fees rust={} ts=242113 | net rust={} ts=-1748112",
        totals.fills,
        totals.gross_micros,
        totals.fees_micros,
        totals.net_micros(),
    );
    assert_eq!(totals.fills, 4);
    assert_eq!(totals.gross_micros, -1_505_999);
    assert_eq!(totals.fees_micros, 242_113);
    assert_eq!(totals.net_micros(), -1_748_112);

    println!("nt-grid engine_vs_ts_probe ok");
}
