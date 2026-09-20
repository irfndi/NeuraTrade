// ponytail: regression check for the resumed day-start denominator.
// A resume file with no `day_start` line (v1, or the first tick after the
// v2 deploy) used to fall back to the raw deposit while `current` was
// `capital + closed_realized`. With capital 10M and closed_realized -2M the
// daily-loss gate read (10M-8M)/10M = 20% against a 5% cap and halted every
// tick. Also covers the tick-entry rollover path a per-interval soak
// loop actually hits at midnight (it never spans midnight inside one tick).
//
// Run: cd crates && cargo run -q -p nt-grid --example day_start_check
use nt_execution::{HONEST_MAKER_FEE_BP, HONEST_TAKER_EXIT_BP};
use nt_grid::{PaperEngineConfig, ResumeState, run_paper_engine, run_paper_engine_from};
use nt_market::Candle;
use nt_risk::{Money, RiskLimits};

const DAY_MS: i64 = 86_400_000;

fn cfg() -> PaperEngineConfig {
    // paper_engine_check geometry: 100bp step, 1.0x target, 2-grid stop.
    PaperEngineConfig {
        step_bp: 100,
        target_ratio_x100: 100,
        grid_max_grids: 2,
        slippage_bps: 50,
        max_position_size_pct: 10,
        fee_bp: HONEST_TAKER_EXIT_BP,
        maker_fee_bp: HONEST_MAKER_FEE_BP,
    }
}

/// Bars that ENTER (low reaches open - step) so the gate is actually
/// evaluated — a flat panel proves nothing because no order is minted.
fn entering_bars(day: i64, n: usize) -> Vec<Candle> {
    (0..n)
        .map(|i| Candle {
            open_ts_ms: day * DAY_MS + i as i64 * 3_600_000,
            open: Money(100_000_000),
            high: Money(102_000_000),
            low: Money(97_000_000),
            close: Money(100_000_000),
            volume_base_micros: 1_000_000,
        })
        .collect()
}

fn main() {
    // Production shadow config: 10 USDT per symbol. RiskLimits::live()
    // defaults min_capital to 100 and nt-cli's floor is 1 — a floor at or
    // above capital rejects every entry, so pin it below the partition.
    let limits = RiskLimits {
        min_capital: Money(1_000_000),
        ..RiskLimits::live()
    };
    let capital = Money(10 * 1_000_000);

    // 1. v1-shaped resume: no day_start, negative realized, same day.
    //    The denominator must land on equity (42M), not the deposit (50M).
    let v1 = ResumeState {
        day_index: None,
        day_fills: 0,
        day_start_capital: None,
        closed_realized: -800_000,
        peak: Some(Money(10_000_000)),
        ..ResumeState::default()
    };
    let (_ev, _l, end) =
        run_paper_engine_from(&entering_bars(20_456, 6), &cfg(), capital, &limits, &v1);
    assert_eq!(
        end.day_start_capital.0, 9_200_000,
        "v1 fallback must anchor on equity, got {}",
        end.day_start_capital.0
    );
    println!("v1 resume: day_start=9200000 (equity, not the 10M deposit)");

    // 2. Tick-entry rollover with carried losses: prior day down 0.8M, resume
    //    into the NEXT day. day_start must re-anchor on equity and the
    //    carried day_fills must reset (3 -> the 6 this tick actually fills).
    let carry = ResumeState {
        day_index: Some(20_456),
        day_fills: 3,
        day_start_capital: Some(Money(10_000_000)),
        closed_realized: -800_000,
        peak: Some(Money(10_000_000)),
        ..ResumeState::default()
    };
    let (_ev, _l, end) =
        run_paper_engine_from(&entering_bars(20_457, 6), &cfg(), capital, &limits, &carry);
    assert_eq!(
        end.day_fills, 6,
        "rollover must reset the carried count, then count this tick's fills"
    );
    assert_eq!(
        end.day_start_capital.0, 9_200_000,
        "rollover denominator must anchor on equity, got {}",
        end.day_start_capital.0
    );
    assert_eq!(
        end.day_index,
        Some(20_457),
        "day index must advance to the last bar's day"
    );
    println!("midnight rollover: day_start=9200000 day_fills=6 day_index=20457");

    // 3. Continuous vs incremental across the same boundary must agree.
    let mut panel = entering_bars(20_456, 12);
    panel.extend(entering_bars(20_457, 12));
    let (c_ev, _c_l) = run_paper_engine(&panel, &cfg(), capital, &limits);
    let (d1_ev, _l1, d1_end) = run_paper_engine_from(
        &entering_bars(20_456, 12),
        &cfg(),
        capital,
        &limits,
        &ResumeState::default(),
    );
    let carry2 = ResumeState {
        position: d1_end.position,
        closed_realized: d1_end.closed_realized,
        peak: Some(d1_end.peak),
        window_fills: d1_end.window_fills,
        bar_offset: d1_end.bar_offset,
        event_count: d1_end.event_count,
        cum_fills: d1_end.cum_fills,
        cum_gross: d1_end.cum_gross,
        cum_fees: d1_end.cum_fees,
        day_index: d1_end.day_index,
        day_fills: d1_end.day_fills,
        day_start_capital: Some(d1_end.day_start_capital),
    };
    let (d2_ev, _l2, _d2_end) = run_paper_engine_from(
        &entering_bars(20_457, 12),
        &cfg(),
        capital,
        &limits,
        &carry2,
    );
    assert_eq!(
        c_ev.len(),
        d1_ev.len() + d2_ev.len(),
        "incremental fills {} != continuous {}",
        d1_ev.len() + d2_ev.len(),
        c_ev.len()
    );
    println!(
        "continuous={} incremental={} (boundary agrees)",
        c_ev.len(),
        d1_ev.len() + d2_ev.len(),
    );

    // 4. Control: one day's fills must still be capped at 10/day.
    let (same_ev, _l) = run_paper_engine(&entering_bars(20_456, 24), &cfg(), capital, &limits);
    assert_eq!(same_ev.len(), 10, "single-day cap must still bind");
    println!("single-day control: fills=10 (cap in force), day_index=20456");

    println!("nt-grid day_start_check ok");
}
