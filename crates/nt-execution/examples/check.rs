// ponytail: single runnable check for nt-execution (not a test suite).
use nt_execution::{HONEST_TAKER_EXIT_BP, Order, submit};
use nt_risk::{EquityWindow, Money, RiskLimits, approve};

fn main() {
    let live = RiskLimits::live();
    let w = EquityWindow {
        current: Money::usdt(200),
        peak: Money::usdt(200),
        day_start: Money::usdt(200),
    };
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

    // Buy 1 unit @ 50 USDT with honest taker-exit fee: fee = 50*6/10000.
    let fill = submit(
        seal,
        Order {
            qty_base_micros: 1_000_000,
            price_micros: Money::usdt(50),
            fee_bp: HONEST_TAKER_EXIT_BP,
        },
    );
    assert_eq!(fill.fee_micros, Money(30_000), "0.06% of 50 USDT");
    assert_eq!(
        fill.proceeds_micros,
        Money(-50_000_000),
        "buy cash out, pre-fee"
    );

    // Blocked books never reach submit: approve errs, no seal exists.
    let bad = approve(
        Money(49_490_000),
        &w,
        Money::usdt(10),
        Money::usdt(10),
        Money::usdt(5),
        1,
        &live,
    );
    assert!(
        bad.is_err(),
        "49.49 under live 100 floor must not mint a seal"
    );

    println!("nt-execution check ok");
}
