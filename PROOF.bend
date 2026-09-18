import LAWS.bend as Laws

# PROOF.bend — the gate: `bend PROOF.bend` elaborates LAWS.bend
# (laws + their proofs) and runs this entry point. It fails while
# any law is open or false. No new claims here — proofs live beside
# their laws so the checker links them by name.

def main() -> IO(Unit):
  do IO<Unit>:
    IO.print("laws hold")
