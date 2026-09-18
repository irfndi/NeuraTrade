// ponytail: single runnable check for nt-grid (not a test suite).
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
    // Anchor 100, step 130bp (champion gridStepPct 1.3), 2 rungs.
    let cfg = GridConfig {
        step_bp: 130,
        rungs: 2,
    };
    // 100 -> 102 (+200bp: crosses rung +1 at 101.30) -> 99 (down through
    // rung -1 at 98.70? 99.00 > 98.70: no cross) -> 97 (crosses -1 and -2
    // at 98.70/97.40; outermost newly touched beyond 0 is -2? -2 level 97.40
    // <= 97? No: 97.40 > 97.00, so kd = (10000-9700)/130 = 2 -> rung -2.
    let p = Panel::new(vec![c(1, 100), c(2, 102), c(3, 99), c(4, 97)]);
    let s = evaluate(&cfg, &p);
    assert_eq!(s.len(), 2, "up-cross then down-cross: {s:?}");
    assert_eq!((s[0].at, s[0].rung), (1, 1));
    assert_eq!(s[0].price, Money(101_300_000));
    assert_eq!((s[1].at, s[1].rung), (3, -2));
    assert_eq!(s[1].price, Money(97_400_000));

    // Flat panel: no signals. Empty panel: none. Zero rungs: none.
    let flat = Panel::new(vec![c(1, 100), c(2, 100), c(3, 100)]);
    assert!(evaluate(&cfg, &flat).is_empty());
    assert!(evaluate(&cfg, &Panel::default()).is_empty());
    assert!(
        evaluate(
            &GridConfig {
                step_bp: 130,
                rungs: 0
            },
            &p
        )
        .is_empty()
    );

    println!("nt-grid check ok");
}
