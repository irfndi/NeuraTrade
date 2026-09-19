//! Paper execution behind the risk gate.
//!
//! Invariant: [`submit`] requires an [`RiskApproval`] that only
//! [`approve`](nt_risk::approve) can mint. There is no other constructor,
//! so unapproved orders cannot reach execution — in Rust or any FFI.

use nt_risk::{Money, RiskApproval};

/// Taker-exit fee in basis points of notional (honest default: 0.06% = 6bp,
/// matching Bybit taker `feeRate 0.0006` and the soak `honestFees` schedule
/// maker0.02/takerExit0.06).
///
/// Per-ticker efficiency: the venue schedule is account-tier uniform, so the
/// RESEARCH default stays global — but every symbol pays a different
/// REALIZED rate once minimum-size rounding (`minOrderQty`/`minOrderAmt`)
/// lands. `Order.fee_bp` is per-order for exactly this reason: fill it from
/// the symbol's own measured bps (fee + rounding drag; slippage lives in the
/// engine's `slippage_bps`, never double-counted here). A symbol whose
/// realized drag exceeds its expected edge per trade is untradable at that
/// size no matter what the global default says.
pub const HONEST_TAKER_EXIT_BP: i64 = 6;

/// A paper order: signed qty in base units (micros), limit price in micro-USDT.
#[derive(Debug, Clone, Copy)]
pub struct Order {
    pub qty_base_micros: i64,
    pub price_micros: Money,
    pub fee_bp: i64,
}

/// A paper fill. `proceeds_micros` is signed pre-fee cash flow
/// (buys negative, sells positive); `fee_micros` is separate.
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct Fill {
    pub qty_base_micros: i64,
    pub price_micros: Money,
    pub fee_micros: Money,
    pub proceeds_micros: Money,
}

/// TECH-DEBT (from TS strangler, clever-cabin-85m): the TS
/// `pollBybitOrderFill` loop breaks on `!canStillFill(status)`, which also
/// fires on EMPTY status (order not yet indexed on testnet) — conflating
/// "never indexed" with "terminally done" and skipping the 2.5s poll window
/// for a premature history read. SOL 00:18Z (venue 112.92 vs bid 113.09,
/// marketable yet status empty/qty 0) is the exhibit. When the native live
/// path grows a fill-poll loop, break on known-terminal states only and
/// keep polling while status is empty. Price path is innocent (ruled out
/// twice: 15bps cross + marketable-yet-unfilled probe).
/// Fill an approved order at its price. Fee = |notional| * fee_bp / 10000.
pub fn submit(_approval: RiskApproval, order: Order) -> Fill {
    let notional = (order.qty_base_micros as i128).abs() * order.price_micros.0 as i128 / 1_000_000;
    let fee = notional * order.fee_bp as i128 / 10_000;
    let proceeds = -(order.qty_base_micros as i128 * order.price_micros.0 as i128 / 1_000_000);
    Fill {
        qty_base_micros: order.qty_base_micros,
        price_micros: order.price_micros,
        fee_micros: Money(fee as i64),
        proceeds_micros: Money(proceeds as i64),
    }
}
