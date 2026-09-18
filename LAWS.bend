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
