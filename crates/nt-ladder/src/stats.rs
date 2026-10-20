//! Causal market statistics for the ladder's chop gate.
//!
//! Port of TS `makeCausalSymbolStats` (`src/scalping/symbol-stats.ts`) at the
//! only field the ladder reads: `adx14`. The TS provider is causal by
//! construction — Wilder's recursion at bar `i` depends only on bars
//! `0..=i` — so one pass over the panel equals calling the provider per bar.
//! (That is the `clever-cabin-dt8` property: no future data leaks in.)
//!
//! Semantics are copied verbatim from `fillAdxSeries`, including the two
//! quirks that matter for parity:
//!   * the initial smoothing SUMS the first `period` values instead of
//!     averaging them (harmless for a ratio, but it is what TS does), and
//!   * ADX is `NaN` — reported as 0 here — until bar index `2*period`, i.e.
//!     the gate is inert for the first 28 bars of a 14-period series.

use crate::Candle;

const PERIOD: usize = 14;

/// Wilder ADX(14) per bar. Bars before index `2 * PERIOD` yield 0.0, matching
/// TS's `Number.isNaN(adxSeries[i]) ? 0 : adxSeries[i]`.
pub fn adx14_series(candles: &[Candle]) -> Vec<f64> {
    let n = candles.len();
    let mut adx = vec![0.0f64; n];
    if n <= PERIOD * 2 {
        return adx;
    }
    let px = |m: crate::Money| -> f64 { m.0 as f64 / 1_000_000.0 };
    let mut tr = vec![0.0f64; n];
    let mut plus_dm = vec![0.0f64; n];
    let mut minus_dm = vec![0.0f64; n];
    for i in 1..n {
        let high = px(candles[i].high);
        let low = px(candles[i].low);
        let prev_high = px(candles[i - 1].high);
        let prev_low = px(candles[i - 1].low);
        let prev_close = px(candles[i - 1].close);
        tr[i] = (high - low)
            .max((high - prev_close).abs())
            .max((low - prev_close).abs());
        let up_move = high - prev_high;
        let down_move = prev_low - low;
        plus_dm[i] = if up_move > down_move && up_move > 0.0 {
            up_move
        } else {
            0.0
        };
        minus_dm[i] = if down_move > up_move && down_move > 0.0 {
            down_move
        } else {
            0.0
        };
    }
    let p = PERIOD as f64;
    let (mut sp, mut sm, mut st) = (0.0f64, 0.0f64, 0.0f64);
    for i in 1..=PERIOD {
        sp += plus_dm[i];
        sm += minus_dm[i];
        st += tr[i];
    }
    let mut dx_values: Vec<f64> = Vec::with_capacity(PERIOD);
    let mut running = 0.0f64;
    for i in (PERIOD + 1)..n {
        sp = sp * (p - 1.0) / p + plus_dm[i];
        sm = sm * (p - 1.0) / p + minus_dm[i];
        st = st * (p - 1.0) / p + tr[i];
        let plus_di = if st == 0.0 { 0.0 } else { (sp / st) * 100.0 };
        let minus_di = if st == 0.0 { 0.0 } else { (sm / st) * 100.0 };
        let dx = if plus_di + minus_di == 0.0 {
            0.0
        } else {
            ((plus_di - minus_di).abs() / (plus_di + minus_di)) * 100.0
        };
        dx_values.push(dx);
        if dx_values.len() == PERIOD {
            running = dx_values.iter().sum::<f64>() / p;
            adx[i] = running;
        } else if dx_values.len() > PERIOD {
            running = (running * (p - 1.0) + dx) / p;
            adx[i] = running;
        }
    }
    adx
}
