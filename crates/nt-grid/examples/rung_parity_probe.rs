// ponytail: rung-ATTRIBUTION check (NOT parity proof) — whole-run replay is
// ground truth; each closed pair is attributed to its entry's rung band and
// the per-rung sums are asserted equal to the whole-run ledger. This proves
// the attribution math is internally consistent, NOT rung-concurrency
// parity: the engine is single-position while TS runs a multi-rung ladder,
// so the 0-vs-3 shadow gap still needs a direct engine-vs-TS comparison.
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
    // Same 6-candle fixture as paper_engine_check.rs: bar 0/5 flat, bar 1
    // long entry, bar 2 long target exit, bar 3 short entry, bar 4 short
    // stop exit. Bars 2h apart so the hourly throughput window never halts.
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

    // Run-anchored base: rung k sits at anchor*(10000+k*step)/10000.
    let anchor = candles[0].open.0;
    println!("anchor(base open)={anchor}");
    for k in [-2i64, -1, 1, 2] {
        let level = anchor * (10_000 + k * cfg.step_bp) / 10_000;
        println!("  rung {k:+}: {level} ({})", Money(level).render());
    }

    let (events, ledger) = run_paper_engine(&candles, &cfg, capital, &limits);
    let t = ledger.totals();
    println!(
        "whole-run: fills={} gross={} fees={} net={}",
        t.fills,
        t.gross_micros,
        t.fees_micros,
        t.net_micros()
    );
    // Ground truth from paper_engine_check.rs hand-derived asserts.
    assert_eq!(t.fills, 4);
    assert_eq!(t.gross_micros, -1_505_999);
    assert_eq!(t.fees_micros, 242_113);
    assert_eq!(t.net_micros(), -1_748_112);

    // Attribute each closed pair to its entry's rung band.
    // proceeds(fill) = -(qty * price / 1e6), truncating (fixture convention).
    let proceeds = |qty: i64, price: i64| -((qty as i128 * price as i128 / 1_000_000) as i64);
    // Floor division: entry at 99.495M vs anchor 100M sits BELOW the anchor
    // (band -1 side); trunc-toward-zero would print +0 and lie about it.
    let rung_of =
        |entry_price: i64| (entry_price * 10_000 / anchor - 10_000).div_euclid(cfg.step_bp);
    let mut per_rung: std::collections::BTreeMap<i64, (i64, i64, i64)> =
        std::collections::BTreeMap::new();
    let mut i = 0;
    while i < events.len() {
        let e = &events[i];
        let x = &events[i + 1];
        assert!(
            x.reason == FillReason::Target || x.reason == FillReason::Stop,
            "exit must follow entry"
        );
        let gross = proceeds(e.qty_base_micros, e.price.0) + proceeds(x.qty_base_micros, x.price.0);
        let fees = e.fee.0 + x.fee.0;
        let side = match e.side {
            Side::Long => "long",
            Side::Short => "short",
        };
        let k = rung_of(e.price.0);
        println!(
            "  pair {side} entry-bar={} rung={k:+} gross={gross} fees={fees} net={}",
            e.bar,
            gross - fees
        );
        let acc = per_rung.entry(k).or_insert((0, 0, 0));
        acc.0 += gross;
        acc.1 += fees;
        acc.2 += gross - fees;
        i += 2;
    }
    let (mut sg, mut sf, mut sn) = (0i64, 0i64, 0i64);
    for (k, (g, f, n)) in &per_rung {
        println!("  rung {k:+}: gross={g} fees={f} net={n}");
        sg += g;
        sf += f;
        sn += n;
    }
    // Parity gate: per-rung sums converge to the whole-run ledger exactly.
    assert_eq!(sg, t.gross_micros, "per-rung gross must equal whole-run");
    assert_eq!(sf, t.fees_micros, "per-rung fees must equal whole-run");
    assert_eq!(sn, t.net_micros(), "per-rung net must equal whole-run");
    println!("rung_parity_probe ok (per-rung sums == whole-run)");
}
