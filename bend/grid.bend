import Base

# Rung-ladder signal kernel (P4). Mirrors nt-grid `evaluate` exactly:
# integer math throughout, anchor = first close, at most one signal per
# candle, up side takes precedence. Prices in cents so every division is
# bit-identical to the Rust micros math. Pure: no IO, never places orders.
# Parity vector (same as nt-grid check): anchor 100.00, step 130bp, 2 rungs,
# closes 102.00/99.00/97.00 -> (at 1, up 1, 101.30) then (at 3, down 2, 97.40).
# Bend idiom per `bend guide`: computed bools branch in helpers that match on
# their parameter; only the structural List fold recurses.

type Sig is Data:
  Sig{at: U32, up: Bool, rung: U32, price: U32}

type Acc is Type:
  Acc{sigs: List<Sig>, hi: U32, lo: U32}

def cap_go(gt: Bool, k: U32, m: U32) -> U32:
  match gt:
    case True{}:
      m
    case False{}:
      k

def cap(+k: U32, +m: U32) -> U32:
  cap_go(U32.is_gt(k, m), k, m)

# Up-side rung: outermost rung at or below the close; 0 below the anchor.
def up_go(below: Bool, scaled: U32, step: U32, rungs: U32) -> U32:
  match below:
    case True{}:
      {0 : U32}
    case False{}:
      cap(U32.div(U32.sub(scaled, {10000 : U32}), step), rungs)

def up_k(+scaled: U32, step: U32, rungs: U32) -> U32:
  up_go(U32.is_lt(scaled, {10000 : U32}), scaled, step, rungs)

# Down-side rung: outermost rung at or above the close; 0 above the anchor.
def dn_go(above: Bool, scaled: U32, step: U32, rungs: U32) -> U32:
  match above:
    case True{}:
      {0 : U32}
    case False{}:
      cap(U32.div(U32.sub({10000 : U32}, scaled), step), rungs)

def dn_k(+scaled: U32, step: U32, rungs: U32) -> U32:
  dn_go(U32.is_ge(scaled, {10000 : U32}), scaled, step, rungs)

def level(anchor: U32, k: U32, step: U32, up: Bool) -> U32:
  match up:
    case True{}:
      U32.div(U32.mul(anchor, U32.add({10000 : U32}, U32.mul(k, step))), {10000 : U32})
    case False{}:
      U32.div(U32.mul(anchor, U32.sub({10000 : U32}, U32.mul(k, step))), {10000 : U32})

def scaled(c: U32, anchor: U32) -> U32:
  U32.div(U32.mul(c, {10000 : U32}), anchor)
def candle_lo(broke_lo: Bool, idx: U32, sigs: List<Sig>, anchor: U32, +kd: U32, step: U32, +hi: U32) -> Acc:
  match broke_lo:
    case True{}:
      sig = {Sig{idx, False{}, kd, level(anchor, kd, step, False{})} : Sig}
      Acc{sig <> sigs, hi, kd}
    case False{}:
      Acc{sigs, hi, hi}

def candle_go(broke_hi: Bool, c: U32, +anchor: U32, +step: U32, rungs: U32, idx: U32, sigs: List<Sig>, +k: U32, hi: U32, lo: U32) -> Acc:
  match broke_hi:
    case True{}:
      sig = {Sig{idx, True{}, k, level(anchor, k, step, True{})} : Sig}
      Acc{sig <> sigs, k, lo}
    case False{}:
      +kd = dn_k(scaled(c, anchor), step, rungs)
      candle_lo(U32.is_gt(kd, lo), idx, sigs, anchor, kd, step, hi)

def fold(cs: List<U32>, +anchor: U32, +step: U32, +rungs: U32, +idx: U32, acc: Acc) -> Acc:
  match cs:
    case Nil{}:
      acc
    case Con{+c, rest}:
      match acc:
        case Acc{sigs, +hi, lo}:
          +k = up_k(scaled(c, anchor), step, rungs)
          fold(rest, anchor, step, rungs, U32.inc(idx), candle_go(U32.is_gt(k, hi), c, anchor, step, rungs, idx, sigs, k, hi, lo))

def pack_go(up: Bool, base: U32) -> U32:
  match up:
    case True{}:
      U32.add(base, {512 : U32})
    case False{}:
      base

def pack(s: Sig) -> U32:
  match s:
    case Sig{at, up, rung, price}:
      base = U32.add(at, U32.add(U32.mul(rung, {16 : U32}), U32.mul(price, {2048 : U32})))
      pack_go(up, base)

def checksum(ss: List<Sig>, +w: U32) -> U32:
  match ss:
    case Nil{}:
      {0 : U32}
    case Con{h, t}:
      U32.add(U32.mul(pack(h), w), checksum(t, U32.mul(w, {31 : U32})))

def result(a: Acc) -> U32:
  match a:
    case Acc{sigs, hi, lo}:
      checksum(sigs, {1 : U32})

def main() -> U32:
  result(fold([{10200 : U32}, {9900 : U32}, {9700 : U32}], {10000 : U32}, {130 : U32}, {2 : U32}, {1 : U32}, Acc{Nil{}, {0 : U32}, {0 : U32}}))

