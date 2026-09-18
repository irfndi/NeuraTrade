import Base

# Autoresearch promotion guards (P4/Task 6 Step 3, narrow slice). Mirrors
# services/neuratrade-cli-ts/autoresearch/goals.ts `checkKeepGuards` /
# `meetsClaimBars` exactly (KEEP_GUARDS v2 / CLAIM_BARS constants below).
# Pure: no IO, no order placement, matches every other file in bend/.
#
# NON-OBVIOUS BEND GOTCHA (found while writing this file): F32 arithmetic
# and comparisons (F32.add, F32.is_gt, ...) do NOT reduce under the plain
# interpreter (`bend file.bend` on an F32 expression prints the unevaluated
# call, e.g. `F32.is_gt(0.01, 0.0)`, not `True{}`). They DO work once
# compiled: `bend file.bend -o out && ./out`. Always verify F32-using Bend
# code via the compiled binary, never trust an interpreter run of it.

type GuardInput is Data:
  GuardInput{
    median_log_return: F32,
    win_rate_pct: F32,
    median_drawdown_pct: F32,
    trades_per_sym_month: F32,
    expectancy_pct: F32,
  }

# Bit i set = guard i failed (0 = all guards pass). Bit order matches
# checkKeepGuards's push order in goals.ts: 0=log_return_nonpositive,
# 1=winrate_below_52, 2=drawdown_above_12, 3=throughput_below_4,
# 4=expectancy_nonpositive. A packed U32 instead of goals.ts's string list —
# simpler for a first Bend port; the bit positions are the "reason".
type GuardResult is Data:
  GuardResult{ok: Bool, violations: U32}

def bit_if(fail: Bool, bit: U32) -> U32:
  U32.mul(Bool.to_u32(fail), bit)

# KEEP_GUARDS v2 (goals.ts): medianLogReturn > 0, winRatePct >= 52,
# medianDrawdownPct <= 12, tradesPerSymMonth >= 4, expectancyPct > 0.
def check_keep_guards(input: GuardInput) -> GuardResult:
  match input:
    case GuardInput{mlr, wr, dd, tpm, ep}:
      fail0 = Bool.not(F32.is_gt(mlr, {0.0 : F32}))
      fail1 = Bool.not(F32.is_ge(wr, {52.0 : F32}))
      fail2 = Bool.not(F32.is_le(dd, {12.0 : F32}))
      fail3 = Bool.not(F32.is_ge(tpm, {4.0 : F32}))
      fail4 = Bool.not(F32.is_gt(ep, {0.0 : F32}))
      +mask = U32.add(
        bit_if(fail0, {1 : U32}),
        U32.add(
          bit_if(fail1, {2 : U32}),
          U32.add(bit_if(fail2, {4 : U32}), U32.add(bit_if(fail3, {8 : U32}), bit_if(fail4, {16 : U32}))),
        ),
      )
      GuardResult{U32.is_eq(mask, {0 : U32}), mask}

# CLAIM_BARS (goals.ts): strictly harder than KEEP_GUARDS on every
# threshold so search keeps climbing overnight instead of re-claiming v1.
def meets_claim_bars(input: GuardInput) -> Bool:
  match input:
    case GuardInput{mlr, wr, dd, tpm, ep}:
      Bool.and(
        F32.is_ge(mlr, {0.08 : F32}),
        Bool.and(
          F32.is_ge(wr, {55.0 : F32}),
          Bool.and(F32.is_le(dd, {11.0 : F32}), Bool.and(F32.is_ge(tpm, {4.0 : F32}), F32.is_ge(ep, {0.001 : F32}))),
        ),
      )

def guard_mask(r: GuardResult) -> U32:
  match r:
    case GuardResult{ok, mask}:
      mask

# Ground truth: the exact 3 fixtures from
# services/neuratrade-cli-ts/autoresearch/mutate.test.ts ("autoresearch
# guards" describe block). Packs the three check_keep_guards violation
# masks as m1 + m2*32 + m3*1024 (5 bits/mask, no overlap; max mask is 31).
# Hand-derived expected values (see bend/README or the commit message that
# added this file): case1 mask=17 (bit0 log_return + bit4 expectancy),
# case2 mask=0 (all pass), case3 mask=2 (bit1 winrate) -> 17 + 0*32 + 2*1024
# = 2065.
def main() -> U32:
  case1 = {GuardInput{F32.neg({0.01 : F32}), {55.0 : F32}, {8.0 : F32}, {40.0 : F32}, F32.neg({0.1 : F32})} : GuardInput}
  case2 = {GuardInput{{0.01 : F32}, {52.0 : F32}, {10.0 : F32}, {5.0 : F32}, {0.2 : F32}} : GuardInput}
  case3 = {GuardInput{{0.01 : F32}, {50.0 : F32}, {10.0 : F32}, {5.0 : F32}, {0.2 : F32}} : GuardInput}
  m1 = guard_mask(check_keep_guards(case1))
  m2 = guard_mask(check_keep_guards(case2))
  m3 = guard_mask(check_keep_guards(case3))
  U32.add(m1, U32.add(U32.mul(m2, {32 : U32}), U32.mul(m3, {1024 : U32})))
