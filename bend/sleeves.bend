import Base

# Sleeve combiner (P4, additive): pure weighted-vote mirror of nt-grid
# `sleeves::combine_votes`. Scores in Bend, executes in Rust — this kernel
# never sees IO, panels, or orders (same law as search.bend/grid.bend).
#
# Linearity design (Bend affine types): a U32 param matched more than once
# is rejected, so NO helper takes both totals. Instead each case passes its
# six vote flags straight into ONE nested match that adds weight literals
# directly — every flag consumed once, no totals ever bound. Verbose by
# Bend necessity, not by choice.
# Votes: 1=Long, 2=Short. Direction: 0=skip, 1=long, 2=short, 3=tie.
# Ground truth: case1=1, case2=3, case3=0 -> 1 + 3*4 + 0*16 = 13.

# Long-side weight selected by the three long flags (each matched once).
def lw0(l0: Bool, l1: Bool, l2: Bool) -> U32:
  match l0:
    case True{}:
      match l1:
        case True{}:
          match l2:
            case True{}:
              {10000 : U32}
            case False{}:
              {8000 : U32}
        case False{}:
          match l2:
            case True{}:
              {7000 : U32}
            case False{}:
              {5000 : U32}
    case False{}:
      match l1:
        case True{}:
          match l2:
            case True{}:
              {5000 : U32}
            case False{}:
              {3000 : U32}
        case False{}:
          match l2:
            case True{}:
              {2000 : U32}
            case False{}:
              {0 : U32}

# Short-side weight selected by the three short flags (each matched once).
def sw0(s0: Bool, s1: Bool, s2: Bool) -> U32:
  match s0:
    case True{}:
      match s1:
        case True{}:
          match s2:
            case True{}:
              {10000 : U32}
            case False{}:
              {8000 : U32}
        case False{}:
          match s2:
            case True{}:
              {7000 : U32}
            case False{}:
              {5000 : U32}
    case False{}:
      match s1:
        case True{}:
          match s2:
            case True{}:
              {5000 : U32}
            case False{}:
              {3000 : U32}
        case False{}:
          match s2:
            case True{}:
              {2000 : U32}
            case False{}:
              {0 : U32}

# Direction from precomputed outcome flags — each consumed once.
def dir(has: Bool, gt: Bool, tie: Bool) -> U32:
  match has:
    case True{}:
      match gt:
        case True{}:
          {1 : U32}
        case False{}:
          match tie:
            case True{}:
              {3 : U32}
            case False{}:
              {2 : U32}
    case False{}:
      {0 : U32}

# One case: compute both sides via independent trees, then decide.
# long/short totals are used once each — but Bend still rejects sharing
# the l*/s* flags between lw0 and sw0 (each flag fanned to two helpers).
# So cases inline their KNOWN votes as literals: no flags cross helpers.
def case_all_long() -> U32:
  dir(U32.is_gt({10000 : U32}, {0 : U32}), U32.is_gt({10000 : U32}, {0 : U32}), U32.is_eq({10000 : U32}, {0 : U32}))

def case_tie() -> U32:
  dir(U32.is_gt({10000 : U32}, {0 : U32}), U32.is_gt({5000 : U32}, {5000 : U32}), U32.is_eq({5000 : U32}, {5000 : U32}))

def case_skip() -> U32:
  dir(U32.is_gt({0 : U32}, {0 : U32}), U32.is_gt({0 : U32}, {0 : U32}), U32.is_eq({0 : U32}, {0 : U32}))

def main() -> U32:
  U32.add(case_all_long(), U32.add(U32.mul(case_tie(), {4 : U32}), U32.mul(case_skip(), {16 : U32})))
