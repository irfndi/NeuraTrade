// ponytail: single runnable check for nt-market (not a test suite).
use nt_market::{Candle, Panel};
use nt_risk::Money;

fn c(ts: i64, close: i64) -> Candle {
    Candle {
        open_ts_ms: ts,
        open: Money::usdt(close),
        high: Money::usdt(close + 1),
        low: Money::usdt(close - 1),
        close: Money::usdt(close),
        volume_base_micros: 1_000_000,
    }
}

fn main() {
    // Unsorted input comes out oldest-first.
    let p = Panel::new(vec![c(3000, 52), c(1000, 50), c(2000, 51)]);
    assert_eq!(p.len(), 3);
    assert_eq!(p.latest_close(), Some(Money::usdt(52)));
    let closes: Vec<Money> = p.closes().collect();
    assert_eq!(
        closes,
        vec![Money::usdt(50), Money::usdt(51), Money::usdt(52)]
    );
    assert!(Panel::default().is_empty());

    println!("nt-market check ok");
}
