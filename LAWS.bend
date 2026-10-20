import Base

# LAWS.bend — cross-cutting laws for the Bend search kernel (P4).
# Convention (per `bend guide`): this file STATES open claims and domain defs;
# PROOF.bend proves each law with a same-name def. `bend PROOF.bend` is the gate.
#
# - fee honesty: net score is exactly gross minus fee (no hidden skimming)
# - promotion guards: a guard-failing candidate never replaces the incumbent
# - search isolation (structural, see note): scoring is pure — no IO, no order
#   placement effects exist anywhere in the trial path; only `main` performs IO.

type Trial is Data:
  Trial{gross: U32, fee: U32, guards_ok: Bool}

def net(t: Trial) -> U32:
  match t:
    case Trial{g, f, _}:
      U32.sub(g, f)

def gross(t: Trial) -> U32:
  match t:
    case Trial{g, _, _}:
      g

def fee(t: Trial) -> U32:
  match t:
    case Trial{_, f, _}:
      f

def pick(a: Trial, b: Trial) -> Trial:
  match a:
    case Trial{_, _, False{}}:
      b
    case Trial{_, _, True{}}:
      match b:
        case Trial{_, _, False{}}:
          a
        case Trial{_, _, True{}}:
          a

law fee_honesty:
  for t: Trial
  { net(t) == U32.sub(gross(t), fee(t)) : U32 }

law pick_left_failing:
  for g1: U32
  for f1: U32
  for b: Trial
  { pick(Trial{g1, f1, False{}}, b) == b : Trial }

law pick_left_passing_right_failing:
  for g1: U32
  for f1: U32
  for g2: U32
  for f2: U32
  { pick(Trial{g1, f1, True{}}, Trial{g2, f2, False{}}) == Trial{g1, f1, True{}} : Trial }

def fee_honesty(t):
  match t:
    case Trial{g, f, _}:
      {==}

def pick_left_failing(g1, f1, b):
  {==}

def pick_left_passing_right_failing(g1, f1, g2, f2):
  {==}

# ---------------------------------------------------------------------------
# Chop-gate seed decision (2026-10-21).
#
# Mirrors the ladder's pre-seed gate on both sides of the port:
#   TS  `createLadderBarContext`/`seedLadderSide` (ladder-engine.ts)
#   Rust `advance_bar`/`seed_side` (crates/nt-ladder/src/lib.rs)
# The reason this is a law and not a comment: the Rust replay silently ran
# with the gate hardcoded off, which made every published "candidate" replay
# test a config nobody registered. These laws pin the decision table so a
# regression (gate dropped, tier order flipped) fails the gate instead of
# quietly changing a measured number.
#
# What is NOT modelled: Rung levels, sizing, fees, pauses. The laws are about
# the seed DECISION only — the axis the port diverged on.

type SeedAction is Data:
  Filled{}
  Kept{}
  Blocked{}
  Seeded{}

# TS `createLadderBarContext`: chopGateActive = threshold > 0 && adx14 >= threshold.
# 0 disarms the gate entirely — that is the live soak's value. Written with
# Bool combinators, not a match: Bend refuses to scrutinize a computed value
# ("give it its own def"), and `U32.is_gt(t, 0)` IS `t > 0`.
def chop_gate(+threshold: U32, adx: U32) -> Bool:
  Bool.and(U32.is_gt(threshold, {0 : U32}), U32.is_ge(adx, threshold))

# TS `seedLadderSide`: onlyWithTrend means long needs close > trend and short
# needs close < trend; the caller passes that comparison as `close_above`.
# No trend series => TS `trendAllows` is false, i.e. this def gets False{}.
def trend_allows(only_with_trend: Bool, close_above: Bool) -> Bool:
  match only_with_trend:
    case True{}:
      close_above
    case False{}:
      True{}

def seed_allowed(
  drawdown: Bool, chop: Bool, only_with_trend: Bool, close_above: Bool
) -> Bool:
  Bool.and(
    Bool.not(drawdown),
    Bool.and(Bool.not(chop), trend_allows(only_with_trend, close_above)),
  )

# The gate's verdict as data: Bend cannot match on `seed_allowed(...)` inline
# (computed scrutinee), so the conversion happens in a def of its own — and it
# must be defined ABOVE its caller (defs bind in file order).
def seed_result(allowed: Bool) -> SeedAction:
  match allowed:
    case True{}:
      Seeded{}
    case False{}:
      Blocked{}

# Rust `seed_side` tier order: filled rungs win, then armed rungs are KEPT
# untouched (the a07b3dd0 invariant: a blocked bar must not wipe armed rungs),
# and only an EMPTY side consults the gates.
def seed_action(
  has_filled: Bool,
  has_armed: Bool,
  drawdown: Bool,
  chop: Bool,
  only_with_trend: Bool,
  close_above: Bool,
) -> SeedAction:
  match has_filled:
    case True{}:
      Filled{}
    case False{}:
      match has_armed:
        case True{}:
          Kept{}
        case False{}:
          seed_result(seed_allowed(drawdown, chop, only_with_trend, close_above))

law gate_off_is_inert:
  for adx: U32
  { chop_gate({0 : U32}, adx) == False{} : Bool }

law gate_boundary_is_inclusive:
  for adx: U32
  { chop_gate({20 : U32}, adx) == U32.is_ge(adx, {20 : U32}) : Bool }

law clean_bar_seeds:
  for close_above: Bool
  { seed_action(False{}, False{}, False{}, False{}, False{}, close_above) == Seeded{} : SeedAction }

law chop_blocks_empty_seed:
  for close_above: Bool
  { seed_action(False{}, False{}, False{}, True{}, False{}, close_above) == Blocked{} : SeedAction }

law drawdown_blocks_empty_seed:
  for close_above: Bool
  { seed_action(False{}, False{}, True{}, False{}, False{}, close_above) == Blocked{} : SeedAction }

law trend_requires_direction:
  for dd: Bool
  for ch: Bool
  { seed_action(False{}, False{}, dd, ch, True{}, False{}) == Blocked{} : SeedAction }

law trend_satisfied_seeds:
  { seed_action(False{}, False{}, False{}, False{}, True{}, True{}) == Seeded{} : SeedAction }

law armed_kept_under_chop:
  for dd: Bool
  for close_above: Bool
  { seed_action(False{}, True{}, dd, True{}, False{}, close_above) == Kept{} : SeedAction }

law filled_dominates_gates:
  for ch: Bool
  for owt: Bool
  { seed_action(True{}, False{}, True{}, ch, owt, False{}) == Filled{} : SeedAction }

# Proofs. Each input the goal does not need is matched so the checker can
# reduce, keeping `{==}` honest (both sides compute to the same constructor).
def gate_off_is_inert(adx):
  {==}

def gate_boundary_is_inclusive(adx):
  {==}

def clean_bar_seeds(close_above):
  {==}

def chop_blocks_empty_seed(close_above):
  {==}

def drawdown_blocks_empty_seed(close_above):
  {==}

# Both gate inputs sit under `Bool.and`, whose reduction is stuck on a free
# variable, so the proof has to split on them (the goal refines per case).
def trend_requires_direction(dd, ch):
  match dd:
    case True{}:
      match ch:
        case True{}:
          {==}
        case False{}:
          {==}
    case False{}:
      match ch:
        case True{}:
          {==}
        case False{}:
          {==}

def trend_satisfied_seeds():
  {==}

def armed_kept_under_chop(dd, close_above):
  {==}

def filled_dominates_gates(ch, owt):
  {==}
