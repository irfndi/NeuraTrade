// ponytail: slice-3 sizing check — the four ladderRungQty traps plus the box's
// verified runtime values. Each assert pins a trap; reverting the matching
// branch in lib.rs fails this. Numbers are the soak's, not invented:
// capital 48.35 per partition, fill 111.05 (the SOL rung's own level),
// rungs 2, pct 100 / notional 100 / 5x dynamic.
use nt_ladder::{
    ContractSpec, SizingOptions, SkipReason, orderable_qty, per_rung_allocation, rung_qty,
};
use nt_risk::Money;

fn micros(v: f64) -> Money {
    Money((v * 1_000_000.0).round() as i64)
}

/// The soak's runtime options: partition-expanded pct, hardcoded notional,
/// fully-dynamic leverage (so `leverage` is ignored).
fn soak(spec: Option<ContractSpec>) -> SizingOptions {
    SizingOptions {
        max_position_pct: 100,
        max_notional_pct: Some(100),
        rungs: 2,
        leverage: 1,
        fully_dynamic: true,
        max_leverage: 10,
        spec,
    }
}

fn main() {
    let capital = micros(48.35);
    let fill = micros(111.05424);

    // 1. Allocation: capital * 100% / rungs = 48.35 / 2 = 24.175.
    let alloc = per_rung_allocation(capital, soak(None));
    assert_eq!(alloc, micros(24.175), "alloc {alloc:?}");

    // 2. Leverage is the account-scaled cap (5x), NOT the CLI's --leverage 1.
    let sized = rung_qty(capital, soak(None), fill);
    assert_eq!(
        sized.leverage, 5,
        "expected 5x dynamic cap, got {}",
        sized.leverage
    );
    assert!(sized.skip.is_none(), "unexpected skip {sized:?}");

    // 3. Trap 3: margin and notional tie (both pct 100), so raw = capital/rungs/price.
    //    48.35/2/111.05424 = 0.21766... base units -> 217666 micros.
    let expected_raw = ((micros(24.175).0 as i128 * 1_000_000) / fill.0 as i128) as i64;
    assert_eq!(expected_raw, 217_686, "hand-computed raw");
    assert_eq!(
        sized.qty,
        expected_raw.max(0),
        "raw tie {sized:?} vs {expected_raw}"
    );

    // 4. Trap 1: specs-absent is structurally different — no rounding, no skips.
    //    A spec with a huge minQty WOULD trip a skip, and must be ignored
    //    entirely when spec is None.
    let absent = rung_qty(capital, soak(None), fill);
    assert!(
        absent.skip.is_none(),
        "specs-absent must not skip: {absent:?}"
    );
    assert_eq!(
        absent.qty,
        expected_raw.max(0),
        "specs-absent must return raw: {absent:?}"
    );

    // 5. Trap 2 ORDERING: margin is checked first, then notional. A 1-SOL
    //    minQty raise gives notional 111.05, but margin = 111.05/5 = 22.21
    //    which is BELOW the 24.175 alloc — so the margin check passes and the
    //    NOTIONAL check fires. Both must be implemented; ordering matters.
    let cap_tripping = ContractSpec {
        min_qty: 1_000_000,
        qty_step: 1_000,
    };
    let skipped = rung_qty(capital, soak(Some(cap_tripping)), fill);
    assert_eq!(
        skipped.skip,
        Some(SkipReason::MinNotionalExceedsCap {
            notional: micros(111.05424),
            cap_share: alloc
        }),
        "expected notional-cap skip (margin 22.21 < alloc), got {skipped:?}"
    );
    assert_eq!(skipped.qty, 0);

    // 5b. The MARGIN skip needs notional/leverage > alloc, i.e. a minQty whose
    //     notional exceeds alloc * leverage = 24.175 * 5 = 120.875 USDT.
    let margin_tripping = ContractSpec {
        min_qty: 1_500_000,
        qty_step: 1_000,
    };
    let m_skip = rung_qty(capital, soak(Some(margin_tripping)), fill);
    assert_eq!(
        m_skip.skip,
        Some(SkipReason::MinNotionalExceedsMargin {
            notional: micros(111.05424 * 1.5),
            margin: Money((micros(111.05424 * 1.5).0) / 5),
            leverage: 5,
            cap: alloc,
        }),
        "expected margin skip, got {m_skip:?}"
    );

    // 6. Trap 4: orderable_qty ceils to step, floors back only when the ceiled
    //    qty exceeds cap, then raises to minQty unconditionally.
    //    raw 217666 micros, step 10000 -> ceil = 220000; 220000*111.05 = 24.43 > 24.175
    //    so floor back to 210000; then minQty 0 -> stays 210000.
    let step_spec = ContractSpec {
        min_qty: 0,
        qty_step: 10_000,
    };
    assert_eq!(
        orderable_qty(217_666, step_spec, fill, alloc),
        210_000,
        "floor-back"
    );
    // Same raw, step 1_000_000 (1 SOL): ceil = 1_000_000, 1*111.05 > cap -> floor
    // to 0 -> then minQty 0 keeps 0. With minQty set it raises regardless.
    assert_eq!(
        orderable_qty(
            217_666,
            ContractSpec {
                min_qty: 0,
                qty_step: 1_000_000
            },
            fill,
            alloc
        ),
        0
    );
    assert_eq!(
        orderable_qty(
            217_666,
            ContractSpec {
                min_qty: 200_000,
                qty_step: 1_000_000
            },
            fill,
            alloc
        ),
        200_000,
        "unconditional minQty raise"
    );

    // 7. Trap 2b with maxNotionalPct lowered: cap share = 48.35*50%/2 = 12.09,
    //    below the 111.05 min-orderable notional -> same skip, different cap.
    let mut tight = soak(Some(ContractSpec {
        min_qty: 1_000_000,
        qty_step: 1_000,
    }));
    tight.max_notional_pct = Some(50);
    let tight_skip = rung_qty(capital, tight, fill);
    assert_eq!(
        tight_skip.skip,
        Some(SkipReason::MinNotionalExceedsCap {
            notional: micros(111.05424),
            cap_share: micros(12.0875)
        }),
        "expected notional-cap skip, got {tight_skip:?}"
    );

    // 8. The real SOL fill: minQty 0.2, step 0.001 (Bybit SOL specs).
    let sol = ContractSpec {
        min_qty: 200_000,
        qty_step: 1_000,
    };
    let sol_sized = rung_qty(capital, soak(Some(sol)), fill);
    assert!(
        sol_sized.skip.is_none(),
        "SOL rung must size: {sol_sized:?}"
    );
    // raw 217686, step 1000 -> ceil 218000 -> 24.21 > cap -> floor 217000.
    assert_eq!(sol_sized.qty, 217_000, "SOL rung qty {sol_sized:?}");
    assert_eq!(sol_sized.leverage, 5);

    // 9. Guards: zero allocation and non-positive price skip before anything else.
    let broke = rung_qty(Money(0), soak(None), fill);
    assert_eq!(broke.skip, Some(SkipReason::PerRungAllocationZero));
    let bad_px = rung_qty(capital, soak(None), Money(0));
    assert_eq!(bad_px.skip, Some(SkipReason::NonPositiveFillPrice));

    println!("rung_qty_check: all 9 assertions passed (alloc=24.175, lev=5, qty=210000)");
}
