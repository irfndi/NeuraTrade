// ponytail: single runnable check for nt-risk (not a test suite).
// Fails (non-zero exit) if guard semantics break.
use nt_risk::{
    EquityWindow, Money, RiskLimits, ThroughputLimits, ThroughputWindow, allowlist_violations,
    approve, basic_risk_violations, live_trading_violations, throughput_violations,
    trade_count_violations,
};

fn window(current: Money) -> EquityWindow {
    EquityWindow {
        current,
        peak: Money::usdt(200),
        day_start: Money::usdt(200),
    }
}

fn main() {
    let live = RiskLimits::live();

    // Integer rendering only — no float anywhere in the risk path.
    assert_eq!(Money(49_490_000).render(), "49.49");
    assert_eq!(Money::usdt(50).render(), "50.00");

    // Demo-blocker shape: ETH 49.49 < min 100 (live) must block.
    assert_eq!(basic_risk_violations(Money(49_490_000), &live).len(), 1);

    // Drawdown guard: 200 -> 160 is 20% > max 15.
    let w = EquityWindow {
        current: Money::usdt(160),
        peak: Money::usdt(200),
        day_start: Money::usdt(200),
    };
    let v = approve(
        Money::usdt(160),
        &w,
        Money::usdt(10),
        Money::usdt(10),
        Money::usdt(5),
        1,
        &live,
    );
    assert!(v.is_err(), "20% drawdown must block");

    // Daily-loss guard: 200 -> 185 is 7% > max 5 (drawdown 7% < 15 ok).
    let w = EquityWindow {
        current: Money::usdt(185),
        peak: Money::usdt(190),
        day_start: Money::usdt(200),
    };
    let v = approve(
        Money::usdt(185),
        &w,
        Money::usdt(10),
        Money::usdt(10),
        Money::usdt(5),
        1,
        &live,
    );
    assert!(v.is_err(), "7% daily loss must block");

    // Healthy book passes and mints the seal.
    let ok = approve(
        Money::usdt(200),
        &window(Money::usdt(200)),
        Money::usdt(10),
        Money::usdt(10),
        Money::usdt(5),
        1,
        &live,
    );
    assert!(ok.is_ok(), "healthy book must approve");

    // Live kill switch: paper-mode limits (live_trading_enabled=false) must
    // block a live-tagged order, but never touch a non-live one.
    let paper_limits = RiskLimits {
        live_trading_enabled: false,
        ..live.clone()
    };
    assert_eq!(live_trading_violations(true, &paper_limits).len(), 1);
    assert!(live_trading_violations(false, &paper_limits).is_empty());
    assert!(live_trading_violations(true, &live).is_empty());

    // Daily fill cap: at-or-past the max blocks; below it passes.
    assert_eq!(trade_count_violations(10, &live).len(), 1);
    assert!(trade_count_violations(9, &live).is_empty());

    // Allowlist: symbol not listed, product type not listed, leverage over
    // cap — three independent violations in one call.
    let scoped = RiskLimits {
        allowed_symbols: Some(vec!["BTC/USDT:USDT".to_string()]),
        ..live.clone()
    };
    let v = allowlist_violations("ETH/USDT:USDT", Some("COIN-FUTURES"), Some(20), &scoped);
    assert_eq!(v.len(), 3, "symbol + product type + leverage must all fire");
    assert!(
        allowlist_violations("BTC/USDT:USDT", Some("USDT-FUTURES"), Some(5), &scoped).is_empty()
    );
    // Unknown product type fails an active allowlist, same as guards.ts.
    assert_eq!(
        allowlist_violations("BTC/USDT:USDT", None, None, &scoped).len(),
        1
    );
    // No allowlists configured at all -> nothing can violate.
    let unrestricted = RiskLimits {
        allowed_symbols: None,
        allowed_product_types: None,
        max_leverage: None,
        ..live.clone()
    };
    assert!(allowlist_violations("ANY/PAIR", None, Some(999), &unrestricted).is_empty());

    // Soak edge: 50.00 passes a 50 floor.
    let soak = RiskLimits {
        min_capital: Money::usdt(50),
        ..live
    };
    assert!(basic_risk_violations(Money::usdt(50), &soak).is_empty());

    // Throughput breaker (Aug-18 shape): 99 trades/hour must halt.
    let lim = ThroughputLimits::strict();
    let spiral = ThroughputWindow {
        trades: 99,
        gross_micros: -2_170_000,
        fees_micros: 1_540_000,
    };
    let v = throughput_violations(&spiral, &lim);
    assert!(
        v.iter().any(|s| s.contains("trades exceeds max")),
        "spiral must halt"
    );

    // Fee-dominated window (60% share) halts even at low count.
    let drag = ThroughputWindow {
        trades: 5,
        gross_micros: 40_000,
        fees_micros: 60_000,
    };
    assert!(
        throughput_violations(&drag, &lim)
            .iter()
            .any(|s| s.contains("fee share"))
    );

    // Healthy flow passes.
    let calm = ThroughputWindow {
        trades: 5,
        gross_micros: 500_000,
        fees_micros: 30_000,
    };
    assert!(throughput_violations(&calm, &lim).is_empty());

    println!("nt-risk check ok");
}
