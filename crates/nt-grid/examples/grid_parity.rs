// ponytail: GRID half of Gate 4 — same-engine parity, both sides EXECUTED.
// Reads the CSV pair written by the TS runner
// (services/neuratrade-cli-ts/grid_parity_fixture.ts, which runs
// `runGridBacktest` on the paper_engine_check fixture) and replays the SAME
// candles through Rust `run_paper_engine`, asserting agreement on side, bars,
// and prices within an integer-rounding budget.
//
// This does NOT cover the LADDER engine the box soaks run (multi-rung); that
// port is P5 Step 3 scope. It closes the grid half honestly instead of
// pretending a fill/PnL diff against the ladder soak means something.
//
// Divergence budget:
//   LONG leg (buy then sell): TS multiplies by `(1+slip)` for the buy and
//     divides by `(1+slip)` for the sell — mathematically symmetric, so Rust's
//     integer `x*(10000±bps)/10000` matches to <= 1 micro.
//   SHORT leg: TS enters at `sellLevel / (1+slip)` and stops at
//     `stop / (1+slip)`, while Rust uses `x*(10000-bps)/10000`. These differ
//     by O(slip^2) (~2.5k micros at 50bp) — a known simplification recorded at
//     engine.rs:124 ("deliberately simplified symmetric rule — NOT TS's
//     asymmetric times/div"). Asserted as a bounded delta, not parity.
// Long delta <= 1u; short delta <= 3000u (bound = 1.01 * the measured ~2525u).
use nt_execution::HONEST_TAKER_EXIT_BP;
use nt_grid::{FillReason, PaperEngineConfig, Side, run_paper_engine};
use nt_market::Candle;
use nt_risk::{Money, RiskLimits};
use std::fs;

struct FxTrade {
    side: Side,
    entry_price: i64,
    exit_price: i64,
    exit_reason: String,
}

fn num(s: &str) -> i64 {
    s.trim().parse().expect("fixture number")
}

fn main() {
    let candles_raw = fs::read_to_string("nt-grid/examples/grid_parity_fixture.candles.csv")
        .expect("fixture missing — regenerate via services/neuratrade-cli-ts/grid_parity_fixture.ts");
    let candles: Vec<Candle> = candles_raw
        .lines()
        .filter(|l| !l.trim().is_empty())
        .map(|l| {
            let f: Vec<&str> = l.split(',').collect();
            assert_eq!(f.len(), 5, "candles.csv wants 5 cols");
            Candle {
                open_ts_ms: num(f[0]),
                open: Money(num(f[1])),
                high: Money(num(f[2])),
                low: Money(num(f[3])),
                close: Money(num(f[4])),
                volume_base_micros: 1_000_000,
            }
        })
        .collect();

    let trades_raw =
        fs::read_to_string("nt-grid/examples/grid_parity_fixture.trades.csv")
            .expect("fixture missing — regenerate via services/neuratrade-cli-ts/grid_parity_fixture.ts");
    let trades: Vec<FxTrade> = trades_raw
        .lines()
        .filter(|l| !l.trim().is_empty())
        .map(|l| {
            let f: Vec<&str> = l.split(',').collect();
            assert_eq!(f.len(), 4, "trades.csv wants 4 cols");
            FxTrade {
                side: match f[0].trim() {
                    "long" => Side::Long,
                    "short" => Side::Short,
                    other => panic!("unknown side {other}"),
                },
                entry_price: num(f[1]),
                exit_price: num(f[2]),
                exit_reason: f[3].trim().to_string(),
            }
        })
        .collect();

    // paper_engine_check geometry (step 100bp, target 1.0x, stop 2 grids,
    // slippage 50bp, pos 10%, fee 6bp), capital 1000 (fixture's capital).
    let cfg = PaperEngineConfig {
        step_bp: 100,
        target_ratio_x100: 100,
        grid_max_grids: 2,
        slippage_bps: 50,
        max_position_size_pct: 10,
        fee_bp: HONEST_TAKER_EXIT_BP,
    };
    let capital = Money(1000 * 1_000_000);
    let limits = RiskLimits::live();
    let (events, _ledger) = run_paper_engine(&candles, &cfg, capital, &limits);

    assert_eq!(
        events.len(),
        trades.len() * 2,
        "rust fill count {} != 2x ts trades {}",
        events.len(),
        trades.len()
    );
    println!("ts trades={} rust fills={}", trades.len(), events.len());

    let mut max_price_delta: i64 = 0;
    let budget = |t: &FxTrade| -> i64 {
        match t.side {
            // Symmetric leg: integer math matches TS to rounding.
            Side::Long => 1,
            // Asymmetric leg: O(slip^2) known deviation, see header.
            Side::Short => 3000,
        }
    };
    for (t, pair) in trades.iter().zip(events.chunks(2)) {
        let (entry, exit) = (&pair[0], &pair[1]);
        assert_eq!(entry.side, t.side, "side mismatch");
        assert_eq!(entry.reason, FillReason::Entry, "first fill must be entry");
        let want_reason = match t.exit_reason.as_str() {
            "target" => FillReason::Target,
            "stop" => FillReason::Stop,
            other => panic!("unknown exit reason {other}"),
        };
        assert_eq!(
            exit.reason, want_reason,
            "exit reason mismatch (rust={:?} ts={})",
            exit.reason, t.exit_reason
        );
        let price_delta = (entry.price.0 - t.entry_price)
            .abs()
            .max((exit.price.0 - t.exit_price).abs());
        let cap = budget(t);
        assert!(
            price_delta <= cap,
            "{:?} leg price divergence {price_delta}u exceeds budget {cap}u",
            t.side
        );
        max_price_delta = max_price_delta.max(price_delta);
        println!(
            "  {:<5} exit={:<6} price rust={}/{} ts={}/{} (delta {price_delta}u)",
            match t.side {
                Side::Long => "long",
                Side::Short => "short",
            },
            t.exit_reason,
            entry.price.0,
            exit.price.0,
            t.entry_price,
            t.exit_price,
        );
    }
    println!(
        "nt-grid grid_parity ok (long<=1u symmetric, short<=3000u asymmetric-slip; max observed {max_price_delta}u)"
    );
}
