import Base

# Trial scheduler (P4): the parallel half of the autoresearch loop.
# The TS loop (autoresearch/loop.ts) walks trials SEQUENTIALLY per worker:
# mutate -> screen -> confirm -> keep/discard, one at a time. This kernel
# owns the parts that parallelize: given N mutated candidates and their
# screen scores, pick which survive to confirm — the exact
# SCREEN_SLACK rule from loop.ts, applied as a parallel map, not a loop.
#
# Pure: no IO, no panel reads, no order placement. The TS worker keeps
# owning panel eval + file locks + champion JSON; Bend only answers
# "which candidates deserve confirm". Same JSON drop path so soaks
# never notice the scheduler changed.
#
# SCREEN_SLACK = 0.0025 (loop.ts): near-misses pay for confirm so the
# loop never plateaus on a hard screen ceiling (v2 overnight stall).
# A candidate survives iff its screen is finite AND (champ has no screen
# yet OR screen > champScreen - slack). F32 so the rule matches TS float
# semantics; verify COMPILED (bend guide: F32 never reduces interpreted).
#
# Ground truth: hand-packed bitmask over 4 candidates.
# champScreen = 0.05, slack case screen = 0.048 (0.05-0.0025 = 0.0475,
# 0.048 > 0.0475 survives), exact-floor 0.0475 dies (not strictly >),
# no-champ accepts any finite screen. main packs b0..b3 = 11
# (b0=1, b1=2, b2=0, b3=8).

# TS Number.isFinite rejects NaN AND ±Infinity (scores sanitize to -inf
# upstream). PROBED Bend 2.0.5 codegen limits (do not regress):
# - F32.is_eq SELF-compare miscompiles: is_eq(0.06, 0.06) reads False,
#   so NaN-via-(x != x) is unusable. Never self-compare.
# - let-bound/computed Bool matched later reads 0; thread every Bool
#   through a helper parameter (bend guide idiom).
# Design: finite = strictly inside ±1e6 (scores are small floats;
# ±inf and NaN both fail the range). Distinct literals only, each F32
# binding used once per helper. Ground truth verified COMPILED
# (interpreter never reduces F32).
def above_lo(+s: F32, +lo: F32, +hi: F32) -> Bool:
  Bool.and(F32.is_gt(s, lo), F32.is_lt(s, hi))

def is_finite(+screen: F32) -> Bool:
  lo = F32.neg({1000000.0 : F32})
  hi = {1000000.0 : F32}
  above_lo(screen, lo, hi)

def above_floor(+s: F32, +floor: F32, fin: Bool) -> U32:
  U32.add(Bool.to_u32(fin), Bool.to_u32(F32.is_gt(s, floor)))

def above_champ(+s: F32, +champ: F32, fin: Bool) -> U32:
  floor = F32.sub(champ, {0.0025 : F32})
  above_floor(s, floor, fin)

# has-champ: 2 = finite AND above floor. No champ: 1 = finite.
def survive_n(+s: F32, +champ: F32, +has_champ: Bool, fin: Bool) -> U32:
  match has_champ:
    case True{}:
      above_champ(s, champ, fin)
    case False{}:
      Bool.to_u32(fin)

def want_n(has_champ: Bool) -> U32:
  match has_champ:
    case True{}:
      {2 : U32}
    case False{}:
      {1 : U32}

def survives(+screen: F32, +champ_screen: F32, +has_champ: Bool) -> Bool:
  fin = is_finite(screen)
  U32.is_eq(survive_n(screen, champ_screen, has_champ, fin), want_n(has_champ))

def bit_if(pass: Bool, bit: U32) -> U32:
  U32.mul(Bool.to_u32(pass), bit)

def main() -> U32:
  b0 = survives({0.06 : F32}, {0.05 : F32}, True{})
  b1 = survives({0.048 : F32}, {0.05 : F32}, True{})
  b2 = survives({0.0475 : F32}, {0.05 : F32}, True{})
  b3 = survives({0.01 : F32}, {0.05 : F32}, False{})
  U32.add(bit_if(b0, {1 : U32}), U32.add(bit_if(b1, {2 : U32}), U32.add(bit_if(b2, {4 : U32}), bit_if(b3, {8 : U32}))))
