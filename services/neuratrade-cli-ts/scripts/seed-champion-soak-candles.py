#!/usr/bin/env python3
"""Seed / sync champion paper/demo DBs with bybit-futures OHLCV from main cache.

Isolated soak homes must not share the live kill-switch DB.

  # full replace (first boot)
  python3 scripts/seed-champion-soak-candles.py

  # incremental append of newer candles (cron every 15m)
  python3 scripts/seed-champion-soak-candles.py --incremental
"""

from __future__ import annotations

import argparse
import sqlite3
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

SRC = Path("/root/.neuratrade/data/neuratrade.db")
HOMES = (
    Path("/root/.neuratrade-champion-paper"),
    Path("/root/.neuratrade-champion-demo"),
)
EXCHANGE = "bybit-futures"
SYMBOLS = (
    "BTC/USDT:USDT",
    "ETH/USDT:USDT",
    "SOL/USDT:USDT",
    "LINK/USDT:USDT",
)
TIMEFRAMES = ("5m", "15m")
TIMEFRAME_MINUTES = {"5m": 5, "15m": 15}
LOOKBACK_DAYS = 120
# Champion soaks write these DBs on the same 900s cadence. python's sqlite3
# default busy timeout is only 5s, which is also the soak app's own tolerance
# (src/services/sqlite.ts), so a writer mid-iteration must be waited out here
# rather than turning into a failed sync cycle.
BUSY_TIMEOUT_MS = 30_000


def open_candle_cutoff_iso(timeframe: str, now: datetime | None = None) -> str:
    """Start of the currently forming bucket (exclusive upper bound).

    Source cache may hold the still-open candle; copying it freezes an
    unfinished OHLCV row that INSERT OR IGNORE then keeps forever.
    """
    now = now or datetime.now(timezone.utc)
    minutes = TIMEFRAME_MINUTES[timeframe]
    floored = now.replace(second=0, microsecond=0) - timedelta(
        minutes=now.minute % minutes
    )
    return floored.strftime("%Y-%m-%dT%H:%M:%S.000Z")


def cols(con: sqlite3.Connection, table: str, schema: str | None = None) -> list[str]:
    if schema:
        rows = con.execute(f"PRAGMA {schema}.table_info({table})").fetchall()
    else:
        rows = con.execute(f"PRAGMA table_info({table})").fetchall()
    return [r[1] for r in rows]


def ensure_exchange_and_pairs(
    dst: sqlite3.Connection, src_ex_id: int, incremental: bool
) -> dict[int, int]:
    """Return src_pair_id -> dst_pair_id map."""
    ex_cols = set(cols(dst, "exchanges"))
    if "api_url" in ex_cols:
        dst.execute(
            """
            INSERT OR REPLACE INTO exchanges (id, name, api_url, is_active)
            VALUES (?, ?, '', 1)
            """,
            (src_ex_id, EXCHANGE),
        )
    else:
        dst.execute(
            "INSERT OR REPLACE INTO exchanges (id, name) VALUES (?, ?)",
            (src_ex_id, EXCHANGE),
        )

    pair_cols = cols(dst, "trading_pairs")
    src_pair_cols = cols(dst, "trading_pairs", schema="src")
    has_pair_ex = "exchange_id" in pair_cols
    ph = ",".join("?" for _ in SYMBOLS)

    # Full-seed only: clear this exchange's rows before re-copying. The
    # ohlcv_data DELETE is intentionally NOT here — it belongs to the write
    # transaction in seed(), where the candle copy that follows it is atomic
    # with it. Doing it here would run as a bare autocommit, so a crash between
    # it and the re-insert would leave the soak DB with no candles.
    # These pair DELETEs get their own short transaction for the same reason:
    # the re-insert happens later, after the source scan.
    if not incremental:
        dst.execute("BEGIN IMMEDIATE")
        try:
            if has_pair_ex:
                dst.execute(
                    f"DELETE FROM trading_pairs WHERE exchange_id = ? AND symbol IN ({ph})",
                    (src_ex_id, *SYMBOLS),
                )
            else:
                dst.execute(
                    f"DELETE FROM trading_pairs WHERE symbol IN ({ph})", SYMBOLS
                )
        except BaseException:
            dst.execute("ROLLBACK")
            raise
        dst.commit()

    if has_pair_ex:
        common = [c for c in pair_cols if c in src_pair_cols]
        dst.execute(
            f"""
            INSERT OR IGNORE INTO trading_pairs ({",".join(common)})
            SELECT {",".join("s." + c for c in common)}
            FROM src.trading_pairs s
            WHERE s.exchange_id = ? AND s.symbol IN ({ph})
            """,
            (src_ex_id, *SYMBOLS),
        )
        # Also REPLACE missing ids if IGNORE skipped due to unique(symbol) only
        pairs = dst.execute(
            f"""
            SELECT id, symbol FROM trading_pairs
            WHERE {"exchange_id = ? AND " if has_pair_ex else ""}symbol IN ({ph})
            """,
            (src_ex_id, *SYMBOLS) if has_pair_ex else SYMBOLS,
        ).fetchall()
    else:
        for sym in SYMBOLS:
            base, rest = sym.split("/", 1)
            quote = rest.split(":", 1)[0]
            src_row = dst.execute(
                """
                SELECT id FROM src.trading_pairs
                WHERE exchange_id = ? AND symbol = ?
                """,
                (src_ex_id, sym),
            ).fetchone()
            if not src_row:
                raise SystemExit(f"missing src pair {sym}")
            src_pid = int(src_row[0])
            insert_cols = ["id", "symbol"]
            values: list[object] = [src_pid, sym]
            if "base_currency" in pair_cols:
                insert_cols.append("base_currency")
                values.append(base)
            if "quote_currency" in pair_cols:
                insert_cols.append("quote_currency")
                values.append(quote)
            if "is_futures" in pair_cols:
                insert_cols.append("is_futures")
                values.append(1)
            if "is_active" in pair_cols:
                insert_cols.append("is_active")
                values.append(1)
            placeholders = ",".join("?" for _ in insert_cols)
            dst.execute(
                f"INSERT OR IGNORE INTO trading_pairs ({','.join(insert_cols)}) VALUES ({placeholders})",
                values,
            )
        pairs = dst.execute(
            f"SELECT id, symbol FROM trading_pairs WHERE symbol IN ({ph})",
            SYMBOLS,
        ).fetchall()

    print(f"  pairs={pairs}")
    src_pairs = dst.execute(
        f"""
        SELECT id, symbol FROM src.trading_pairs
        WHERE exchange_id = ? AND symbol IN ({ph})
        """,
        (src_ex_id, *SYMBOLS),
    ).fetchall()
    dst_by_sym = {sym: int(pid) for pid, sym in pairs}
    return {int(sid): dst_by_sym[sym] for sid, sym in src_pairs}


def seed(dst_path: Path, *, incremental: bool) -> None:
    print(f"{'syncing' if incremental else 'seeding'} {dst_path}")
    dst = sqlite3.connect(dst_path)
    # Explicit transaction control: BEGIN IMMEDIATE .. dst.commit() below.
    # Without isolation_level = None python opens an implicit DEFERRED
    # transaction at the first DML and holds the write lock until the very
    # end, spanning the heavy source scan.
    dst.isolation_level = None
    dst.execute(f"PRAGMA busy_timeout = {BUSY_TIMEOUT_MS}")
    dst.execute("PRAGMA foreign_keys=OFF")
    dst.execute("ATTACH DATABASE ? AS src", (str(SRC),))

    src_ex = dst.execute(
        "SELECT id FROM src.exchanges WHERE name = ?", (EXCHANGE,)
    ).fetchone()
    if not src_ex:
        raise SystemExit(f"missing exchange {EXCHANGE} in {SRC}")
    src_ex_id = int(src_ex[0])

    src_to_dst = ensure_exchange_and_pairs(dst, src_ex_id, incremental)
    src_ids = list(src_to_dst.keys())
    id_ph = ",".join("?" for _ in src_ids)
    tf_ph = ",".join("?" for _ in TIMEFRAMES)

    since_clause = ""
    params: list[object] = [src_ex_id, *src_ids, *TIMEFRAMES]
    cutoffs = {tf: open_candle_cutoff_iso(tf) for tf in TIMEFRAMES}
    print(f"  closed_through={cutoffs}")
    if incremental:
        # Per-(pair, timeframe) watermark — a fresh tip on one symbol must not
        # hide a stale gap on another. Previously GROUP BY timeframe only.
        watermarks = {
            (row[0], row[1]): row[2]
            for row in dst.execute(
                """
                SELECT trading_pair_id, timeframe, MAX(timestamp)
                FROM ohlcv_data
                WHERE exchange_id = ?
                GROUP BY trading_pair_id, timeframe
                """,
                (src_ex_id,),
            ).fetchall()
        }
        print(f"  watermarks={watermarks}")
        # Build OR of (pair = P AND timeframe = X AND timestamp > wm AND timestamp < cutoff_X)
        # The < cutoff excludes the still-open candle from the source cache.
        parts: list[str] = []
        for sid, did in src_to_dst.items():
            for tf in TIMEFRAMES:
                wm = watermarks.get((did, tf))
                cutoff = cutoffs[tf]
                if wm:
                    parts.append(
                        "(trading_pair_id = ? AND timeframe = ? "
                        "AND timestamp > ? AND timestamp < ?)"
                    )
                    params.extend([sid, tf, wm, cutoff])
                else:
                    parts.append(
                        "(trading_pair_id = ? AND timeframe = ? "
                        "AND timestamp >= datetime('now', ?) AND timestamp < ?)"
                    )
                    params.extend([sid, tf, f"-{LOOKBACK_DAYS} days", cutoff])
        since_clause = f"AND ({' OR '.join(parts)})"
    else:
        # Full seed: lookback window, still excluding the open candle per timeframe.
        params.append(f"-{LOOKBACK_DAYS} days")
        parts = []
        for tf in TIMEFRAMES:
            parts.append("(timeframe = ? AND timestamp < ?)")
            params.extend([tf, cutoffs[tf]])
        since_clause = (
            "AND timestamp >= datetime('now', ?) "
            f"AND ({' OR '.join(parts)})"
        )

    dst.execute("DROP TABLE IF EXISTS _ohlcv_tmp")
    dst.execute(
        f"""
        CREATE TEMP TABLE _ohlcv_tmp AS
        SELECT * FROM src.ohlcv_data
        WHERE exchange_id = ?
          AND trading_pair_id IN ({id_ph})
          AND timeframe IN ({tf_ph})
          {since_clause}
        """,
        params,
    )
    ntmp = dst.execute("SELECT COUNT(*) FROM _ohlcv_tmp").fetchone()[0]
    print(f"  tmp_candles={ntmp}")

    for sid, did in src_to_dst.items():
        if sid != did:
            dst.execute(
                "UPDATE _ohlcv_tmp SET trading_pair_id = ? WHERE trading_pair_id = ?",
                (did, sid),
            )
    dst.execute("UPDATE _ohlcv_tmp SET exchange_id = ?", (src_ex_id,))

    ohlcv_cols = cols(dst, "ohlcv_data")
    cols_no_id = [c for c in ohlcv_cols if c != "id"]
    tmp_cols = {r[1] for r in dst.execute("PRAGMA table_info(_ohlcv_tmp)").fetchall()}
    use_cols = [c for c in cols_no_id if c in tmp_cols]

    # Write phase. The source scan above is complete, so the dst write lock is
    # taken only now, for the writes below — never for the scan. IMMEDIATE
    # acquires it up front (no deferred upgrade, so no SQLITE_BUSY on upgrade).
    dst.execute("BEGIN IMMEDIATE")

    if not incremental:
        dst.execute("DELETE FROM ohlcv_data WHERE exchange_id = ?", (src_ex_id,))

    before = dst.execute("SELECT COUNT(*) FROM ohlcv_data").fetchone()[0]
    dst.execute(
        f"""
        INSERT OR IGNORE INTO ohlcv_data ({",".join(use_cols)})
        SELECT {",".join(use_cols)} FROM _ohlcv_tmp
        """
    )
    after = dst.execute("SELECT COUNT(*) FROM ohlcv_data").fetchone()[0]
    latest = dst.execute(
        "SELECT MAX(timestamp) FROM ohlcv_data WHERE exchange_id = ?", (src_ex_id,)
    ).fetchone()[0]

    if incremental:
        # P0: incremental sync must never clear an engaged kill switch.
        # Create the row only when missing; otherwise preserve state.
        dst.execute(
            """
            INSERT INTO risk_kill_switch (id, engaged, reason, updated_at)
            VALUES (1, 0, '', datetime('now'))
            ON CONFLICT(id) DO NOTHING
            """
        )
    else:
        dst.execute(
            """
            INSERT INTO risk_kill_switch (id, engaged, reason, updated_at)
            VALUES (1, 0, '', datetime('now'))
            ON CONFLICT(id) DO UPDATE SET
              engaged = 0,
              reason = '',
              updated_at = datetime('now')
            """
        )
    dst.commit()
    dst.execute("DETACH DATABASE src")
    dst.close()
    print(
        f"  candles={after} (+{after - before}) latest={latest} "
        f"size_mb={dst_path.stat().st_size / 1e6:.1f}"
    )


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument(
        "--incremental",
        action="store_true",
        help="Append only candles newer than current soak max timestamp",
    )
    args = ap.parse_args()
    if not SRC.exists():
        print(f"missing source db: {SRC}", file=sys.stderr)
        return 1
    for home in HOMES:
        db = home / "data" / "neuratrade.db"
        if not db.exists():
            print(
                f"missing {db}; start soak once so migrations create schema",
                file=sys.stderr,
            )
            return 1
        seed(db, incremental=args.incremental)
    print("done")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
