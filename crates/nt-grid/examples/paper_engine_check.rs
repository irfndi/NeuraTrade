// ponytail: single runnable check for nt-grid's stateful paper engine (not
// a test suite — this repo's opt-in test-file policy keeps new #[test] /
// *_test.rs files out unless explicitly approved; `examples/*.rs` is the
// established runtime-compare pattern here, same as check.rs and
// parity_checksum.rs in this crate).
//
// Fixture: 6 candles, oldest-first, prices in micro-USDT. Candle 0 and 5
// are flat (no trigger, confirm the engine stays quiet outside a setup).
// Candle 1 triggers a LONG entry, candle 2 exercises the TARGET exit path;
// candle 3 triggers a SHORT entry, candle 4 exercises the STOP exit path
// (and target-checked-first-but-not-hit) — so both the target and stop
// branches, and both sides, run. Candle 2 and 4's `open` are deliberately
// different from their position's entry-bar `open`, so a bug that freezes
// `step` from the entry bar (instead of recomputing it from the current
// bar, per the brief) would change the derived target/stop and this
// fixture's numbers would stop lining up.
//
// Config: step_bp=100 (1.00%), target_ratio_x100=100 (target = 1.00x
// step), grid_max_grids=2 (stop = 2x step), slippage_bps=50 (0.50%),
// max_position_size_pct=10 (of capital). capital=1000 USDT,
// RiskLimits::live() (min_capital 100 USDT, max_position_size_pct 10,
// max_notional_pct 100 — this fixture's 10%/10% sit exactly at the caps
// and must NOT be rejected, since `position_risk_violations` only rejects
// when strictly greater than the limit).
//
// Expected values below are derived BY HAND from the brief's formulas
// (task-7-step3-brief.md), using truncating integer division throughout
// (Rust's `/` on integers truncates toward zero, matched here) — not by
// running the TS engine. Every intermediate is spelled out so a reviewer
// can check the arithmetic without recomputing it.
//
//   step(open) = open * 100 / 10_000
//   buy_level  = open - step         sell_level = open + step
//   worse_buy(p)  = p * (10_000 + 50) / 10_000   (buy pays more)
//   worse_sell(p) = p * (10_000 - 50) / 10_000   (sell receives less)
//   position_value = 1_000_000_000 * 10 / 100 = 100_000_000
//   qty_abs = position_value * 1_000_000 / entry_price
//   notional = qty_abs * price / 1_000_000        fee = notional * 6 / 10_000
//   proceeds = -(signed_qty * price / 1_000_000)
//
// --- Candle 1 (bar 1): LONG ENTRY ---
//   open=100_000_000 -> step=1_000_000
//   buy_level=99_000_000, sell_level=101_000_000
//   low=98_500_000 <= buy_level -> long triggers
//   entry_price = worse_buy(99_000_000) = 99_000_000*10_050/10_000 = 99_495_000
//   qty_abs = 100_000_000*1_000_000/99_495_000 = 1_005_075 (truncated)
//   notional = 1_005_075*99_495_000/1_000_000 = 99_999_937 (truncated)
//   fee = 99_999_937*6/10_000 = 59_999 (truncated)
//   proceeds = -(1_005_075*99_495_000/1_000_000) = -99_999_937
//
// --- Candle 2 (bar 2): LONG TARGET EXIT ---
//   open=100_500_000 (recomputed) -> step=1_005_000
//   target = 99_495_000 + 1_005_000*100/100 = 100_500_000
//   high=100_600_000 >= target -> target exit fires (NOT slippage-adjusted)
//   exit_price = 100_500_000
//   exit_qty = -1_005_075
//   notional = 1_005_075*100_500_000/1_000_000 = 101_010_037 (truncated)
//   fee = 101_010_037*6/10_000 = 60_606 (truncated)
//   proceeds = -(-1_005_075*100_500_000/1_000_000) = 101_010_037
//
// --- Candle 3 (bar 3): SHORT ENTRY ---
//   open=100_000_000 -> step=1_000_000
//   buy_level=99_000_000, sell_level=101_000_000
//   low=99_300_000 > buy_level (no long); high=101_600_000 >= sell_level -> short triggers
//   entry_price = worse_sell(101_000_000) = 101_000_000*9_950/10_000 = 100_495_000
//   qty_abs = 100_000_000*1_000_000/100_495_000 = 995_074 (truncated); signed = -995_074
//   notional = 995_074*100_495_000/1_000_000 = 99_999_961 (truncated)
//   fee = 99_999_961*6/10_000 = 59_999 (truncated)
//   proceeds = -(-995_074*100_495_000/1_000_000) = 99_999_961
//
// --- Candle 4 (bar 4): SHORT STOP EXIT ---
//   open=100_800_000 (recomputed) -> step=1_008_000
//   target = 100_495_000 - 1_008_000*100/100 = 99_487_000
//   low=100_200_000 > target -> target does NOT fire
//   stop = 100_495_000 + 1_008_000*2 = 102_511_000
//   high=102_600_000 >= stop -> stop fires
//   exit_price = worse_buy(102_511_000) = 102_511_000*10_050/10_000 = 103_023_555
//   exit_qty = 995_074 (closing buy)
//   notional = 995_074*103_023_555/1_000_000 = 102_516_060 (truncated)
//   fee = 102_516_060*6/10_000 = 61_509 (truncated)
//   proceeds = -(995_074*103_023_555/1_000_000) = -102_516_060
//
// --- Totals over 4 fills ---
//   gross = -99_999_937 + 101_010_037 + 99_999_961 + (-102_516_060) = -1_505_999
//   fees  =      59_999 +      60_606 +      59_999 +       61_509 =    242_113
//   net   = gross - fees = -1_748_112
//   render: gross "-1.50", fees "0.24", net "-1.74" (Money::render truncates
//   to 2dp: (|v| % 1_000_000) / 10_000)
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
    let candles = vec![
        // bar 0: flat, no trigger.
        c(0, 100_000_000, 100_300_000, 99_800_000, 100_100_000),
        // bar 1: long entry.
        c(1, 100_000_000, 100_500_000, 98_500_000, 99_500_000),
        // bar 2: long target exit.
        c(2, 100_500_000, 100_600_000, 99_900_000, 100_400_000),
        // bar 3: short entry.
        c(3, 100_000_000, 101_600_000, 99_300_000, 101_200_000),
        // bar 4: short stop exit.
        c(4, 100_800_000, 102_600_000, 100_200_000, 102_300_000),
        // bar 5: flat, no trigger.
        c(5, 102_300_000, 102_500_000, 102_100_000, 102_200_000),
    ];

    let cfg = PaperEngineConfig {
        step_bp: 100,
        target_ratio_x100: 100,
        grid_max_grids: 2,
        slippage_bps: 50,
        max_position_size_pct: 10,
    };
    let capital = Money::usdt(1000);
    let limits = RiskLimits::live();

    let (events, ledger) = run_paper_engine(&candles, &cfg, capital, &limits);

    for e in &events {
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
            "bar={} side={side} reason={reason} price={} ({}) qty_base_micros={} fee={} ({})",
            e.bar,
            e.price.0,
            e.price.render(),
            e.qty_base_micros,
            e.fee.0,
            e.fee.render(),
        );
    }

    let totals = ledger.totals();
    println!(
        "totals: fills={} gross={} ({}) fees={} ({}) net={} ({})",
        totals.fills,
        totals.gross_micros,
        Money(totals.gross_micros).render(),
        totals.fees_micros,
        Money(totals.fees_micros).render(),
        totals.net_micros(),
        Money(totals.net_micros()).render(),
    );

    // Hand-derived expectations from the comment block above.
    assert_eq!(events.len(), 4, "expected 4 fills: {events:?}");

    assert_eq!(events[0].bar, 1);
    assert_eq!(events[0].side, Side::Long);
    assert_eq!(events[0].reason, FillReason::Entry);
    assert_eq!(events[0].price, Money(99_495_000));
    assert_eq!(events[0].qty_base_micros, 1_005_075);
    assert_eq!(events[0].fee, Money(59_999));

    assert_eq!(events[1].bar, 2);
    assert_eq!(events[1].side, Side::Long);
    assert_eq!(events[1].reason, FillReason::Target);
    assert_eq!(events[1].price, Money(100_500_000));
    assert_eq!(events[1].qty_base_micros, -1_005_075);
    assert_eq!(events[1].fee, Money(60_606));

    assert_eq!(events[2].bar, 3);
    assert_eq!(events[2].side, Side::Short);
    assert_eq!(events[2].reason, FillReason::Entry);
    assert_eq!(events[2].price, Money(100_495_000));
    assert_eq!(events[2].qty_base_micros, -995_074);
    assert_eq!(events[2].fee, Money(59_999));

    assert_eq!(events[3].bar, 4);
    assert_eq!(events[3].side, Side::Short);
    assert_eq!(events[3].reason, FillReason::Stop);
    assert_eq!(events[3].price, Money(103_023_555));
    assert_eq!(events[3].qty_base_micros, 995_074);
    assert_eq!(events[3].fee, Money(61_509));

    assert_eq!(totals.fills, 4);
    assert_eq!(totals.gross_micros, -1_505_999);
    assert_eq!(totals.fees_micros, 242_113);
    assert_eq!(totals.net_micros(), -1_748_112);

    println!("nt-grid paper_engine_check ok");
}
