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

/// Maker (target-resting) fee in basis points of notional — the second half
/// of the champion soak's `honestFees` schedule
/// (`services/neuratrade-cli-ts/autoresearch/results/champion-soak.json`:
/// `maker0.02/takerExit0.06 rescored 2026-09-07`, mirrored by
/// [`HONEST_TAKER_EXIT_BP`] on the exit side). Entries and stop exits cross
/// the spread (taker); a target exit rests on the book as a maker limit, so
/// it pays the maker rate. Same per-symbol caveat as the taker constant:
/// the global default is a research floor, `Order.fee_bp` stays per-order.
pub const HONEST_MAKER_FEE_BP: i64 = 2;

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

/// MIRROR (fixed in TS 997cb97f+, clever-cabin-85m): TS
/// `bybitOrderStatusCanStillFill("")` is now `true` — empty status
/// (not-yet-indexed on testnet) polls through the 2.5s window; the cancel
/// path uses an inline resting-state list that EXCLUDES "" (never cancel an
/// unknown orderId). SOL 00:18Z was the exhibit (venue 112.92 vs bid 113.09,
/// marketable yet status empty/qty 0). When the native live path grows a
/// fill-poll loop, mirror this split: poll-continue INCLUDES "", cancel
/// EXCLUDES "". Price path is innocent (ruled out twice).
///
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
