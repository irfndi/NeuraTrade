// Slice 3 sizing assertions — port of TS `ladderRungQty`
// (ladder-engine.ts:1243-1339) + `orderableQty` (types.ts:31-48).
//
// Numbers are the box's verified runtime values, not invented: capital 48.35
// per partition (whitelist rows carry allocatedWeight 0.25, so 200 x 0.25),
// fill 111.05424 (the SOL rung's own persisted level), rungs 2,
// maxPositionPct 100 (partition-expanded from the CLI's 50),
// maxNotionalPct 100 (hardcoded by makeLadderOptions), leverage 5x from the
// fully-dynamic branch rather than the CLI's --leverage 1.
use nt_ladder::{
    ContractSpec, SizingOptions, SkipReason, orderable_qty, per_rung_allocation, rung_qty,
};
use nt_risk::Money;

fn micros(v: f64) -> Money {
    Money((v * 1_000_000.0).round() as i64)
}

/// The soak's runtime options.
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

#[test]
fn per_rung_allocation_is_capital_times_pct_over_rungs() {
    // 48.35 * 100% / 2 = 24.175 USDT.
    assert_eq!(
        per_rung_allocation(micros(48.35), soak(None)),
        micros(24.175)
    );
}

#[test]
fn leverage_is_the_dynamic_cap_not_the_cli_flag() {
    // fullyDynamic -> Math.max(1, cap), ignoring --leverage 1.
    // capital 48.35 < 500 -> sizeCap 10; budget 1.0 -> factor 0.5 -> 5.
    let sized = rung_qty(micros(48.35), soak(None), micros(111.05424));
    assert_eq!(sized.leverage, 5, "expected 5x dynamic cap, got {sized:?}");
    assert!(sized.skip.is_none(), "unexpected skip {sized:?}");
}

#[test]
fn margin_and_notional_tie_at_the_soaks_pcts() {
    // Both percentages are 100, so marginSizedRaw == notionalSizedRaw and
    // min(a, a) === a. raw is BASE micros: 24.175 USDT / 111.05424.
    let fill = micros(111.05424);
    let alloc = per_rung_allocation(micros(48.35), soak(None));
    let expected_raw = (alloc.0 as i128 * 1_000_000 / fill.0 as i128) as i64;
    assert_eq!(expected_raw, 217_686, "hand-computed raw");
    assert_eq!(rung_qty(micros(48.35), soak(None), fill).qty, expected_raw);
}

#[test]
fn specs_absent_skips_rounding_and_the_margin_check() {
    // Trap 1: structurally different branch. A spec with a huge minQty would
    // trip a skip, and must be ignored entirely when spec is None.
    let sized = rung_qty(micros(48.35), soak(None), micros(111.05424));
    assert!(
        sized.skip.is_none(),
        "specs-absent must not skip: {sized:?}"
    );
    let alloc = per_rung_allocation(micros(48.35), soak(None));
    assert_eq!(
        sized.qty,
        (alloc.0 as i128 * 1_000_000 / micros(111.05424).0 as i128) as i64
    );
}

#[test]
fn margin_skip_is_reachable_only_above_alloc_times_leverage() {
    // Trap 2a ordering: a 1-SOL minQty raise gives notional 111.05, but
    // margin = 111.05/5 = 22.21 <= alloc 24.175, so the margin check passes
    // and the NOTIONAL check fires. Notional-space comparison:
    // 111.05 > 24.175 * 5 = 120.875 is false, so no margin skip.
    let capital = micros(48.35);
    let fill = micros(111.05424);
    let alloc = per_rung_allocation(capital, soak(None));
    let one_sol = ContractSpec {
        min_qty: 1_000_000,
        qty_step: 1_000,
    };
    let sized = rung_qty(capital, soak(Some(one_sol)), fill);
    assert_eq!(
        sized.skip,
        Some(SkipReason::MinNotionalExceedsCap {
            notional: micros(111.05424),
            cap_share: alloc
        }),
        "expected notional-cap skip, got {sized:?}"
    );
    assert_eq!(sized.qty, 0);

    // 1.5 SOL: notional 166.58 > 120.875 -> margin skip, with an EXACT margin
    // (166.58136 / 5 = 33.316272), not the code's truncating expression.
    let one_and_half = ContractSpec {
        min_qty: 1_500_000,
        qty_step: 1_000,
    };
    let m_skip = rung_qty(capital, soak(Some(one_and_half)), fill);
    assert_eq!(
        m_skip.skip,
        Some(SkipReason::MinNotionalExceedsMargin {
            notional: micros(111.05424 * 1.5),
            margin: Money(33_316_272),
            leverage: 5,
            cap: alloc,
        }),
        "expected margin skip, got {m_skip:?}"
    );
}

#[test]
fn notional_cap_skip_uses_the_lowered_cap_share() {
    // maxNotionalPct 50 -> cap share = 48.35 * 50% / 2 = 12.0875.
    let mut opts = soak(Some(ContractSpec {
        min_qty: 1_000_000,
        qty_step: 1_000,
    }));
    opts.max_notional_pct = Some(50);
    let sized = rung_qty(micros(48.35), opts, micros(111.05424));
    assert_eq!(
        sized.skip,
        Some(SkipReason::MinNotionalExceedsCap {
            notional: micros(111.05424),
            cap_share: micros(12.0875),
        }),
        "expected notional-cap skip, got {sized:?}"
    );
}

#[test]
fn orderable_qty_ceils_floors_back_then_raises_to_min_qty() {
    let fill = micros(111.05424);
    let alloc = micros(24.175);

    // raw 217686, step 1000 -> ceil 218000 -> 24.21 > cap -> floor to 217000.
    assert_eq!(
        orderable_qty(
            217_686,
            ContractSpec {
                min_qty: 0,
                qty_step: 1_000
            },
            fill,
            alloc
        ),
        217_000,
        "ceil-then-floor-back"
    );
    // Ceiled qty within the cap is kept (no floor-back).
    assert_eq!(
        orderable_qty(
            100_000,
            ContractSpec {
                min_qty: 0,
                qty_step: 1_000
            },
            micros(100.0),
            micros(50.0),
        ),
        100_000,
        "ceil within cap stays"
    );
    // step 1_000_000 (1 SOL): ceil 1_000_000 -> 111.05 > cap -> floor to 0.
    assert_eq!(
        orderable_qty(
            217_686,
            ContractSpec {
                min_qty: 0,
                qty_step: 1_000_000
            },
            fill,
            alloc
        ),
        0
    );
    // The minQty raise is UNCONDITIONAL — it fires even after flooring to 0,
    // which is what makes the two skips reachable at all.
    assert_eq!(
        orderable_qty(
            217_686,
            ContractSpec {
                min_qty: 200_000,
                qty_step: 1_000_000
            },
            fill,
            alloc
        ),
        200_000
    );
    // Non-positive raw and zero step pass through without panicking.
    assert_eq!(
        orderable_qty(
            0,
            ContractSpec {
                min_qty: 5,
                qty_step: 0
            },
            fill,
            alloc
        ),
        0
    );
    assert_eq!(
        orderable_qty(
            -1,
            ContractSpec {
                min_qty: 5,
                qty_step: 1_000
            },
            fill,
            alloc
        ),
        0
    );
}

#[test]
fn real_sol_spec_sizes_the_rung() {
    // Bybit SOL: minQty 0.2, qtyStep 0.001 (base micros).
    let sol = ContractSpec {
        min_qty: 200_000,
        qty_step: 1_000,
    };
    let sized = rung_qty(micros(48.35), soak(Some(sol)), micros(111.05424));
    assert!(sized.skip.is_none(), "SOL rung must size: {sized:?}");
    assert_eq!(sized.qty, 217_000, "SOL rung qty {sized:?}");
    assert_eq!(sized.leverage, 5);
}

#[test]
fn guards_skip_before_anything_else() {
    let broke = rung_qty(Money(0), soak(None), micros(111.05424));
    assert_eq!(broke.skip, Some(SkipReason::PerRungAllocationZero));
    let bad_px = rung_qty(micros(48.35), soak(None), Money(0));
    assert_eq!(bad_px.skip, Some(SkipReason::NonPositiveFillPrice));
}
