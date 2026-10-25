#!/usr/bin/env python3
"""Regression guard: champion candle sync must not collide with soak writers.

The soak apps write paper_trades / grid_paper_state / ladder_paper_state on the
same 900s cadence as this sync, tolerating only `busy_timeout = 5000`
(src/services/sqlite.ts). A sync that holds dst's write lock longer than that,
or that has no busy timeout at all, fails the cycle with
"database is locked" (observed 2026-10-25T20:46:48Z in the demo soak log).

Run:  python3 scripts/test_seed_champion_soak_candles.py
"""

from __future__ import annotations

import importlib.util
import sqlite3
import sys
import threading
import time
from datetime import datetime, timedelta, timezone
from pathlib import Path

HERE = Path(__file__).resolve().parent
SEED_PY = HERE / "seed-champion-soak-candles.py"

# The soak app's own tolerance. Anything shorter than this in the sync is a bug.
SOAK_BUSY_TIMEOUT_MS = 5000
# 120 days of 5m candles per symbol — the production lookback. Small enough to
# stay fast, large enough that the source scan is a real cost.
LOOKBACK_CANDLES_PER_SYMBOL = 34560


def load_module() -> object:
    spec = importlib.util.spec_from_file_location("seed", SEED_PY)
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def build_pair(tmp: Path) -> tuple[Path, Path]:
    """A main cache DB and a soak DB with the shape the script expects.

    Volume is deliberately close to production (4 symbols x 2 timeframes over
    the 120-day lookback, ~46k candles): the whole point of the ordering guard
    is that the write lock must not span a scan of this size.
    """
    src, dst = tmp / "main.db", tmp / "soak.db"
    for path in (src, dst):
        con = sqlite3.connect(path)
        con.executescript(
            """
            CREATE TABLE exchanges (
                id INTEGER PRIMARY KEY, name TEXT, api_url TEXT, is_active INT
            );
            CREATE TABLE trading_pairs (
                id INTEGER PRIMARY KEY, exchange_id INT, symbol TEXT,
                base_currency TEXT, quote_currency TEXT, is_futures INT,
                is_active INT, UNIQUE(exchange_id, symbol)
            );
            CREATE TABLE ohlcv_data (
                id INTEGER PRIMARY KEY, exchange_id INT, trading_pair_id INT,
                timeframe TEXT, timestamp TEXT, open TEXT, high TEXT, low TEXT,
                close TEXT, volume TEXT
            );
            CREATE TABLE risk_kill_switch (
                id INTEGER PRIMARY KEY, engaged INT, reason TEXT, updated_at TEXT
            );
            """
        )
        con.commit()
        con.close()
    syms = ("BTC/USDT:USDT", "ETH/USDT:USDT", "SOL/USDT:USDT", "LINK/USDT:USDT")
    base = datetime(2026, 6, 27, tzinfo=timezone.utc)
    con = sqlite3.connect(src)
    con.execute("INSERT INTO exchanges VALUES (1, 'bybit-futures', '', 1)")
    for i, sym in enumerate(syms, start=10):
        con.execute(
            "INSERT INTO trading_pairs VALUES (?, 1, ?, ?, ?, 1, 1)",
            (i, sym, sym.split("/")[0], "USDT"),
        )
        for k in range(LOOKBACK_CANDLES_PER_SYMBOL):
            ts = base + timedelta(minutes=5 * k)
            con.execute(
                "INSERT INTO ohlcv_data (exchange_id, trading_pair_id,"
                " timeframe, timestamp, open, high, low, close, volume)"
                " VALUES (1, ?, ?, ?, '1','1','1','1','1')",
                (i, "5m" if k % 3 else "15m", ts.strftime("%Y-%m-%dT%H:%M:%S.000Z")),
            )
    con.commit()
    con.close()
    return src, dst


def check_write_lock_spans_only_the_writes(tmp: Path) -> None:
    """The write lock must span only the dst writes, never the source scan.

    Measured deterministically with sqlite3's trace callback rather than by
    timing a racing writer thread: the callback records every statement, so we
    can assert the temp-table build and the source scan happen BEFORE
    BEGIN IMMEDIATE, and that exactly one write transaction exists.

    A timing-based version of this check passes even when the lock is held
    across the scan (CREATE TEMP TABLE writes to the temp database, not dst,
    so a competing writer is barely delayed) — which is why it is traced.
    """
    src, dst = build_pair(tmp)
    mod = load_module()
    mod.SRC = src

    traced: list[str] = []
    real_connect = mod.sqlite3.connect

    def traced_connect(*args: object, **kwargs: object) -> sqlite3.Connection:
        con = real_connect(*args, **kwargs)
        con.set_trace_callback(traced.append)
        return con

    mod.sqlite3.connect = traced_connect  # type: ignore[method-assign]
    try:
        mod.seed(dst, incremental=True)  # pairs exist
        traced.clear()
        mod.seed(dst, incremental=True)  # measured run
    finally:
        mod.sqlite3.connect = real_connect  # type: ignore[method-assign]

    def find(pred: object) -> int:
        for i, sql in enumerate(traced):
            if pred(sql.upper()):  # type: ignore[operator]
                return i
        return -1

    begins = [
        i for i, sql in enumerate(traced)
        if sql.strip().upper().startswith("BEGIN IMMEDIATE")
    ]
    commits = [
        i for i, sql in enumerate(traced)
        if sql.strip().upper().startswith("COMMIT")
    ]
    # find() passes an already-uppercased statement to the predicate.
    temp_build = find(lambda s: "TEMP TABLE _OHLCV_TMP" in s)
    attach = find(lambda s: s.strip().startswith("ATTACH"))
    ohlcv_insert = find(lambda s: "INSERT OR IGNORE INTO OHLCV_DATA" in s)

    assert len(begins) == 1, (
        f"expected exactly one write transaction, saw {len(begins)}"
        f" — the copy must be one BEGIN IMMEDIATE per destination DB"
    )
    assert len(commits) == 1, f"expected exactly one COMMIT, saw {len(commits)}"
    assert attach != -1 and attach < begins[0], (
        "ATTACH must precede the write transaction"
    )
    assert temp_build != -1 and temp_build < begins[0], (
        "the source scan (CREATE TEMP TABLE, ~138k candles) must run BEFORE"
        " BEGIN IMMEDIATE, otherwise the write lock is held across the whole"
        " scan and a soak writer is made to wait for it"
    )
    assert ohlcv_insert != -1 and begins[0] < ohlcv_insert < commits[0], (
        "the candle copy must sit inside the write transaction"
    )
    print(
        f"ok  scan at stmt {temp_build} < BEGIN {begins[0]} < candle copy"
        f" {ohlcv_insert} < COMMIT {commits[0]} (lock excludes the scan)"
    )


def check_sync_survives_a_writer_that_outlasts_soak(tmp: Path) -> None:
    """Hold dst's write lock longer than the soak app would ever tolerate."""
    src, dst = build_pair(tmp)
    mod = load_module()
    mod.SRC = src

    holder = sqlite3.connect(dst, timeout=30)
    holder.execute("PRAGMA journal_mode = WAL")
    holder.execute("PRAGMA busy_timeout = 5000")
    holder.execute("BEGIN IMMEDIATE")
    holder.execute(
        "INSERT INTO risk_kill_switch (id, engaged, reason, updated_at)"
        " VALUES (1, 1, 'manual stop', datetime('now'))"
    )

    outcome: dict[str, object] = {}

    def run_sync() -> None:
        try:
            mod.seed(dst, incremental=True)
            outcome["ok"] = True
        except Exception as err:  # noqa: BLE001 - report the real failure
            outcome["ok"] = False
            outcome["err"] = f"{type(err).__name__}: {err}"

    thread = threading.Thread(target=run_sync)
    t0 = time.perf_counter()
    thread.start()
    # Longer than SOAK_BUSY_TIMEOUT_MS: the pre-fix sync fails here.
    time.sleep((SOAK_BUSY_TIMEOUT_MS / 1000) + 2)
    holder.execute("COMMIT")
    holder.close()
    thread.join()

    assert outcome.get("ok"), f"sync failed under contention: {outcome.get('err')}"
    con = sqlite3.connect(dst)
    candles = con.execute("SELECT COUNT(*) FROM ohlcv_data").fetchone()[0]
    switch = con.execute(
        "SELECT engaged, reason FROM risk_kill_switch WHERE id = 1"
    ).fetchone()
    con.close()
    assert candles > 0, "sync reported success but copied no candles"
    assert switch == (1, "manual stop"), (
        f"incremental sync clobbered the kill switch: {switch!r}"
    )
    print(
        f"ok  survived a {time.perf_counter() - t0:.1f}s writer,"
        f" copied {candles} candles, kill switch preserved"
    )


def check_idempotent(tmp: Path) -> None:
    """Re-running must not duplicate or lose candles."""
    src, dst = build_pair(tmp)
    mod = load_module()
    mod.SRC = src
    mod.seed(dst, incremental=True)
    mod.seed(dst, incremental=True)
    con = sqlite3.connect(dst)
    rows = con.execute(
        "SELECT trading_pair_id, timeframe, timestamp, COUNT(*) c"
        " FROM ohlcv_data GROUP BY 1,2,3 HAVING c > 1"
    ).fetchall()
    total = con.execute("SELECT COUNT(*) FROM ohlcv_data").fetchone()[0]
    con.close()
    assert not rows, f"duplicate candles after two syncs: {rows[:3]}"
    assert total > 0, "sync left the soak DB empty"
    print(f"ok  idempotent: {total} candles, no duplicates")


def check_full_seed_deletes_are_atomic(tmp: Path) -> None:
    """Full-seed destructive DELETEs must each sit inside a transaction.

    The cron path is --incremental, which never reaches this branch, so the
    full-seed path is otherwise untested. A bare-autocommit DELETE there means
    a crash between the delete and the re-insert leaves the soak DB with no
    pairs (or no candles), which the incremental tests would never catch.
    """
    src, dst = build_pair(tmp)
    mod = load_module()
    mod.SRC = src

    traced: list[str] = []
    real_connect = mod.sqlite3.connect

    def traced_connect(*args: object, **kwargs: object) -> sqlite3.Connection:
        con = real_connect(*args, **kwargs)
        con.set_trace_callback(traced.append)
        return con

    mod.sqlite3.connect = traced_connect  # type: ignore[method-assign]
    try:
        mod.seed(dst, incremental=False)  # full seed
    finally:
        mod.sqlite3.connect = real_connect  # type: ignore[method-assign]

    upper = [s.upper() for s in traced]
    begins = [i for i, s in enumerate(upper) if s.strip().startswith("BEGIN IMMEDIATE")]
    commits = [i for i, s in enumerate(upper) if s.strip().startswith("COMMIT")]

    def bracketed(idx: int, label: str) -> None:
        lo = max((b for b in begins if b < idx), default=None)
        hi = min((c for c in commits if c > idx), default=None)
        assert lo is not None and hi is not None and lo < idx < hi, (
            f"{label} at stmt {idx} runs as a bare autocommit — a crash before"
            f" the re-insert would leave the soak DB with rows missing"
        )

    for i, s in enumerate(upper):
        if s.strip().startswith("DELETE FROM TRADING_PAIRS"):
            bracketed(i, "DELETE FROM trading_pairs")
        if s.strip().startswith("DELETE FROM OHLCV_DATA"):
            bracketed(i, "DELETE FROM ohlcv_data")

    # And the seed must still have produced a complete DB.
    con = sqlite3.connect(dst)
    pairs = con.execute("SELECT COUNT(*) FROM trading_pairs").fetchone()[0]
    candles = con.execute("SELECT COUNT(*) FROM ohlcv_data").fetchone()[0]
    con.close()
    assert pairs == 4, f"full seed left {pairs} pairs, expected 4"
    assert candles > 0, "full seed left no candles"
    print(
        f"ok  full-seed DELETEs are transactional;"
        f" {pairs} pairs / {candles} candles after seeding"
    )


def main() -> int:
    import tempfile

    load_module()
    failures = 0
    for name, fn in (
        ("write lock spans only the writes", check_write_lock_spans_only_the_writes),
        ("survives a long writer", check_sync_survives_a_writer_that_outlasts_soak),
        ("idempotent re-run", check_idempotent),
        ("full-seed deletes are atomic", check_full_seed_deletes_are_atomic),
    ):
        try:
            with tempfile.TemporaryDirectory() as tmp:
                fn(Path(tmp))
        except AssertionError as err:
            failures += 1
            print(f"FAIL {name}: {err}")
        except Exception as err:  # noqa: BLE001
            failures += 1
            print(f"ERROR {name}: {type(err).__name__}: {err}")
    print("PASS" if not failures else f"FAILED ({failures})")
    return 1 if failures else 0


if __name__ == "__main__":
    sys.exit(main())
