// ponytail: parity-checksum printer for the Bend kernel gate (not a test).
// Packs nt-grid `evaluate` output with the SAME scheme as bend/grid.bend
// `pack`/`checksum`, so the expected value is derived, never baked in.
use nt_grid::{GridConfig, evaluate};
use nt_market::{Candle, Panel};
use nt_risk::Money;

fn c(ts: i64, usdt: i64) -> Candle {
    Candle {
        open_ts_ms: ts,
        open: Money::usdt(usdt),
        high: Money::usdt(usdt),
        low: Money::usdt(usdt),
        close: Money::usdt(usdt),
        volume_base_micros: 1_000_000,
    }
}

fn main() {
    let cfg = GridConfig {
        step_bp: 130,
        rungs: 2,
    };
    // Same vector as check.rs, in whole USDT (== cents x100 in Bend).
    let p = Panel::new(vec![c(1, 100), c(2, 102), c(3, 99), c(4, 97)]);
    let s = evaluate(&cfg, &p);
    // Bend cons builds the list reversed; checksum weights head by 1, x31 each step.
    let mut sum: u32 = 0;
    let mut w: u32 = 1;
    for sig in s.iter().rev() {
        let rung = u32::try_from(sig.rung.abs()).unwrap_or(u32::MAX);
        let up = if sig.rung > 0 { 512 } else { 0 };
        let price_cents = (sig.price.0 / 10_000) as u32;
        let pack = sig.at as u32 + rung * 16 + up + price_cents * 2048;
        sum = sum.wrapping_add(pack.wrapping_mul(w));
        w = w.wrapping_mul(31);
    }
    println!("{sum}");
}
