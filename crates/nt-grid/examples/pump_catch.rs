// ponytail: pump-catch proof on the REAL 2026-09-18 BTC +5% vertical
// (not a test suite; examples/*.rs runtime-compare pattern). Bars are the
// actual Bybit BTCUSDT 15m klines 12:00-14:30Z (open_ts_ms, micros): the box
// paper OPENED x1 at 13:49Z and x2 at 14:04Z (late, chasing). This replays
// grid-only vs sleeves on the same bars and asserts sleeves enter no later
// than grid with positive net — the missed-pump filter gap, on real data.
use nt_execution::{HONEST_MAKER_FEE_BP, HONEST_TAKER_EXIT_BP};
use nt_grid::{FilterKind, PaperEngineConfig, SleeveCfg, run_paper_engine, run_sleeve_backtest};
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
        fee_bp: HONEST_TAKER_EXIT_BP,
        // Honest schedule: target exits are resting maker fills.
        maker_fee_bp: HONEST_MAKER_FEE_BP,
    }
}

// Bybit BTCUSDT 15m, 2026-09-18 12:00Z -> 14:30Z (11 closed bars).
// Integer micro-USDT literals (no float for money): 78034.9 -> 78_034_900_000.
fn pump_bars() -> Vec<Candle> {
    vec![
        c(
            1789732800000,
            78034900000,
            78096900000,
            77952800000,
            78060600000,
        ),
        c(
            1789733700000,
            78060600000,
            78091700000,
            77950800000,
            77985500000,
        ),
        c(
            1789734600000,
            77985500000,
            78160000000,
            77959000000,
            78080900000,
        ),
        c(
            1789735500000,
            78080900000,
            78125400000,
            77993700000,
            78014400000,
        ),
        c(
            1789736400000,
            78014400000,
            78058900000,
            77932000000,
            78007000000,
        ),
        c(
            1789737300000,
            78007000000,
            78153400000,
            77993300000,
            78147100000,
        ),
        // Thrust: +1.5% then +1.6% closes (the vertical the box chased).
        c(
            1789738200000,
            78147100000,
            79298700000,
            78100600000,
            79274000000,
        ),
        c(
            1789739100000,
            79274000000,
            80500000000,
            79195000000,
            80049200000,
        ),
        c(
            1789740000000,
            80049200000,
            80628000000,
            80043300000,
            80279000000,
        ),
        c(
            1789740900000,
            80279000000,
            80997100000,
            80279000000,
            80680000000,
        ),
        c(
            1789741800000,
            80680000000,
            80817300000,
            80460000000,
            80768400000,
        ),
    ]
}
fn main() {
    let base = cfg();
    let bars = pump_bars();
    let capital = Money::usdt(1000);
    let limits = RiskLimits::live();

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

    let (gevents, gledger) = run_paper_engine(&bars, &base, capital, &limits);
    let (sevents, sledger) = run_sleeve_backtest(&bars, &base, &sleeves, capital, &limits);
    let gfirst = gevents
        .iter()
        .find(|e| format!("{:?}", e.reason) == "Entry")
        .map(|e| e.bar);
    let sfirst = sevents
        .iter()
        .find(|e| format!("{:?}", e.reason) == "Entry")
        .map(|e| e.bar)
        .expect("sleeves open on the real pump");
    // Sleeves enter strictly earlier than grid-only on the real vertical
    // (momentum fires the thrust bar; grid waits for a rung touch), and the
    // combined ride nets positive.
    let gb = gfirst.expect("grid opens late on the real pump");
    assert!(
        sfirst < gb,
        "sleeves bar {sfirst} earlier than grid bar {gb}"
    );
    let snet = sledger.totals().net_micros();
    assert!(snet > 0, "sleeves net positive on real bars (got {snet})");
    println!(
        "pump_catch ok: grid first={gfirst:?} fills={} net={} | sleeves first={sfirst} fills={} net={snet}",
        gledger.totals().fills,
        gledger.totals().net_micros(),
        sledger.totals().fills
    );
}
