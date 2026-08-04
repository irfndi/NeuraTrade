#!/usr/bin/env python3
import csv
import json
import subprocess
import sys
from pathlib import Path

CLI_DIR = Path("/Users/irfandi/Coding/2025/NeuraTrade/services/neuratrade-cli-ts")
OUT = Path.home() / ".neuratrade" / "logs" / "multi-strategy-scan.csv"
LOG = Path.home() / ".neuratrade" / "logs" / "multi-strategy-scan.log"

SYMBOLS = [
    "ETH/USDT", "BTC/USDT", "SOL/USDT", "ADA/USDT", "XRP/USDT",
    "BNB/USDT", "ALGO/USDT", "FLOKI/USDT", "LINK/USDT", "DOGE/USDT",
]
STRATEGIES = [
    "gridScalp", "trendFollowing", "meanReversion", "breakout",
    "emaPullback", "momentum", "dualEmaCross", "microScalp",
]

OUT.parent.mkdir(parents=True, exist_ok=True)
if not OUT.exists():
    OUT.write_text("symbol,strategy,aggregateReturnPct,profitableWindowsPct,maxDrawdownPct,totalTrades\n")

# Load already-scanned pairs
done = set()
with OUT.open() as f:
    for row in csv.DictReader(f):
        done.add((row["symbol"], row["strategy"]))

total = len(SYMBOLS) * len(STRATEGIES)
count = len(done)

with LOG.open("a") as log:
    def logprint(msg):
        print(msg)
        print(msg, file=log)

    for sym in SYMBOLS:
        for strat in STRATEGIES:
            if (sym, strat) in done:
                continue
            count += 1
            logprint(f"[{count}/{total}] {sym} / {strat}")
            cmd = [
                "bun", "run", "dist/index.js", "scalp", "walk-forward",
                "--strategy", strat,
                "--symbol", sym,
                "--timeframe", "15m",
                "--capital", "20",
                "--realistic",
                "--train-window", "1500",
                "--test-window", "500",
                "--json",
            ]
            try:
                result = subprocess.run(
                    cmd,
                    cwd=CLI_DIR,
                    capture_output=True,
                    text=True,
                    timeout=300,
                )
                lines = [l for l in result.stdout.splitlines() if l.strip()]
                raw = lines[-1] if lines else ""
                if '"windows"' in raw:
                    d = json.loads(raw)
                    ret = d.get("aggregateReturnPct", d.get("totalReturnPct", 0))
                    win = d.get("profitableWindowsPct", 0)
                    dd = d.get("maxDrawdownPct", 0)
                    trades = d.get("totalTrades", d.get("avgTradesPerWindow", 0))
                    row = f"{sym},{strat},{ret},{win},{dd},{trades}\n"
                else:
                    row = f"{sym},{strat},ERROR,0,0,0\n"
            except Exception as e:
                row = f"{sym},{strat},ERROR,0,0,0\n"
                logprint(f"  exception: {e}")
            with OUT.open("a") as f:
                f.write(row)
            logprint(f"  {row.strip()}")
            done.add((sym, strat))

    logprint("")
    logprint("=== Best strategy per symbol ===")
    rows = list(csv.DictReader(OUT.open()))
    best = {}
    for r in rows:
        if r["aggregateReturnPct"] == "ERROR":
            continue
        try:
            ret = float(r["aggregateReturnPct"])
        except ValueError:
            continue
        sym = r["symbol"]
        if sym not in best or ret > best[sym][0]:
            best[sym] = (ret, r)
    for sym, (ret, r) in sorted(best.items(), key=lambda x: -x[1][0]):
        logprint(
            f"{sym:12} {r['strategy']:18} ret={ret:8.2f}% "
            f"winPct={float(r['profitableWindowsPct']):6.1f}% "
            f"dd={float(r['maxDrawdownPct']):6.2f}% trades={r['totalTrades']}"
        )
