// ponytail: single runnable check for nt-ledger (not a test suite).
use nt_execution::{HONEST_TAKER_EXIT_BP, Order, submit};
use nt_ledger::Ledger;
use nt_risk::{EquityWindow, Money, RiskLimits, approve};

fn main() {
    let live = RiskLimits::live();
    let w = EquityWindow {
        current: Money::usdt(200),
        peak: Money::usdt(200),
        day_start: Money::usdt(200),
    };
    let mut ledger = Ledger::new();
    assert!(ledger.is_empty());

    // Round trip: buy 1 @ 50, sell 1 @ 51, honest fees.
    for (qty, price) in [(1_000_000i64, 50i64), (-1_000_000i64, 51i64)] {
        let seal = approve(
            Money::usdt(200),
            &w,
            Money::usdt(10),
            Money::usdt(10),
            Money::usdt(5),
            1,
            &live,
        )
        .expect("healthy book must approve");
        ledger.apply(submit(
            seal,
            Order {
                qty_base_micros: qty,
                price_micros: Money::usdt(price),
                fee_bp: HONEST_TAKER_EXIT_BP,
            },
        ));
    }
    assert_eq!(ledger.len(), 2);
    let t = ledger.totals();
    assert_eq!(t.fills, 2);
    // Cash flow: -50 buy, +51 sell (pre-fee); fees: 30000 + 30600.
    assert_eq!(t.gross_micros, -50_000_000 + 51_000_000);
    assert_eq!(t.fees_micros, 30_000 + 30_600);
    assert_eq!(t.net_micros(), 939_400);

    println!("nt-ledger check ok");
}
