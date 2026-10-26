#!/usr/bin/env python3
"""
Paginated Binance historical klines fetcher for NeuraTrade backtesting.
Fetches 5m candles from Binance REST API with automatic pagination.
Inserts directly into NeuraTrade SQLite ohlcv_data table.
"""

import argparse
import json
import os
import random
import sqlite3
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from typing import Callable, List, Optional

QUOTE_CURRENCIES = ("USDT", "USDC", "BUSD")
BINANCE_API = "https://api.binance.com/api/v3/klines"
RATE_LIMIT_DELAY = 0.15  # ~6-7 requests/sec to stay under Binance limits
MAX_RETRIES = 5
BASE_BACKOFF_SECONDS = 1.0


def _default_db_path() -> str:
    """Resolve the DB path from env (NEURATRADE_HOME / NEURATRADE_DB) with a portable default."""
    home = os.environ.get("NEURATRADE_HOME") or os.path.expanduser("~/.neuratrade")
    return os.path.join(home, "data", "neuratrade.db")


DB_PATH = os.environ.get("NEURATRADE_DB") or _default_db_path()
_DEFAULT_DB = DB_PATH  # import-time default; main may override DB_PATH via --db


class FetchError(RuntimeError):
    """Raised when a range cannot be fetched completely (error, gap, or stall)."""


def get_db_connection(db_path: Optional[str] = None):
    # Create the parent dir for a fresh install; sqlite3 only errors on a
    # missing directory, never a missing file.
    target = db_path or DB_PATH
    parent = os.path.dirname(os.path.abspath(target))
    os.makedirs(parent, exist_ok=True)
    conn = sqlite3.connect(target)
    conn.execute("PRAGMA foreign_keys = ON")
    return conn


def _interval_ms(interval: str) -> int:
    """Duration of an interval string ('5m', '1h', ...) in milliseconds."""
    multipliers = {"m": 60_000, "h": 3_600_000, "d": 86_400_000, "w": 604_800_000}
    value, unit = interval[:-1], interval[-1].lower()
    if not value.isdigit() or unit not in multipliers:
        raise ValueError(f"unsupported interval {interval!r}")
    return int(value) * multipliers[unit]


def _parse_boundary(raw: str) -> datetime:
    """Parse an ISO date/datetime for --start/--end; naive values are UTC."""
    try:
        parsed = datetime.fromisoformat(raw)
    except ValueError as exc:
        raise argparse.ArgumentTypeError(
            f"invalid date {raw!r}: expected an ISO date or datetime"
        ) from exc
    if parsed.tzinfo is None:
        parsed = parsed.replace(tzinfo=timezone.utc)
    return parsed.astimezone(timezone.utc)


def get_or_create_exchange(conn: sqlite3.Connection, name: str) -> int:
    cur = conn.execute("SELECT id FROM exchanges WHERE LOWER(name) = LOWER(?)", (name,))
    row = cur.fetchone()
    if row:
        return row[0]
    cur = conn.execute(
        "INSERT INTO exchanges (name, api_url, is_active) VALUES (?, ?, 1)",
        (name, "https://api.binance.com"),
    )
    conn.commit()
    lid = cur.lastrowid
    if lid is None:
        raise RuntimeError("Failed to insert exchange")
    return lid


def get_or_create_trading_pair(conn: sqlite3.Connection, exchange_id: int, symbol: str) -> int:
    """Get or create a trading pair, deriving base/quote from the actual symbol suffix.

    Accepts both bare (BTCUSDT) and display (BTC/USDT) forms. The quote
    currency comes from the suffix that was stripped (USDT/USDC/BUSD), never a
    hardcoded value.
    """
    if "/" in symbol:
        display_symbol = symbol
        base_currency, quote_currency = symbol.split("/", 1)
    else:
        display_symbol = None
        base_currency = quote_currency = None
        for quote in QUOTE_CURRENCIES:
            if symbol.endswith(quote):
                base_currency = symbol[: -len(quote)]
                quote_currency = quote
                display_symbol = f"{base_currency}/{quote}"
                break
        if display_symbol is None:
            raise ValueError(
                f"Unsupported symbol {symbol!r}: expected a {', '.join(QUOTE_CURRENCIES)} suffix"
            )

    cur = conn.execute(
        "SELECT id FROM trading_pairs WHERE exchange_id = ? AND symbol = ?",
        (exchange_id, display_symbol),
    )
    row = cur.fetchone()
    if row:
        return row[0]
    cur = conn.execute(
        "INSERT INTO trading_pairs (exchange_id, symbol, base_currency, quote_currency, is_active) "
        "VALUES (?, ?, ?, ?, 1)",
        (exchange_id, display_symbol, base_currency, quote_currency),
    )
    conn.commit()
    lid = cur.lastrowid
    if lid is None:
        raise RuntimeError("Failed to insert trading pair")
    return lid


def _retry_after_seconds(error: urllib.error.HTTPError) -> float:
    headers = getattr(error, "headers", None) or getattr(error, "hdrs", None)
    if not headers:
        return 0.0
    raw = headers.get("Retry-After")
    if not raw:
        return 0.0
    try:
        return float(raw)
    except (TypeError, ValueError):
        return 0.0


def fetch_klines(
    symbol: str, interval: str, start_ms: int, end_ms: int, limit: int = 1000, max_retries: int = MAX_RETRIES
) -> Optional[List[List]]:
    """Fetch a single page of klines from Binance.

    Returns the page as a list, or None if the page could not be fetched after
    ``max_retries`` attempts (so callers can abort loudly). An empty list means
    Binance returned no data for the window. Rate-limit (429) and 5xx responses
    are retried with exponential backoff + jitter, honoring Retry-After when
    present; other 4xx errors are permanent and fail immediately. Retries are
    bounded — no recursion.
    """
    url = f"{BINANCE_API}?symbol={symbol}&interval={interval}&startTime={start_ms}&endTime={end_ms}&limit={limit}"
    for attempt in range(1, max_retries + 1):
        req = urllib.request.Request(url, headers={"User-Agent": "NeuraTrade-Backtest/1.0"})
        try:
            with urllib.request.urlopen(req, timeout=30) as resp:
                return json.loads(resp.read().decode("utf-8"))
        except urllib.error.HTTPError as e:
            retryable = e.code == 429 or e.code >= 500
            if not retryable:
                print(f"  HTTP Error {e.code}: {e.reason}")
                return None
            if attempt == max_retries:
                print(f"  HTTP {e.code} after {max_retries} attempts: giving up on this page")
                return None
            wait = _retry_after_seconds(e) if e.code == 429 else 0.0
            if wait <= 0:
                wait = BASE_BACKOFF_SECONDS * (2 ** (attempt - 1))
            wait += random.uniform(0.0, wait * 0.2)  # jitter
            print(f"  HTTP {e.code} (attempt {attempt}/{max_retries}): retrying in {wait:.1f}s")
            time.sleep(wait)
        except Exception as e:
            if attempt == max_retries:
                print(f"  Error after {max_retries} attempts: {e}")
                return None
            wait = BASE_BACKOFF_SECONDS * (2 ** (attempt - 1))
            print(f"  Transient error (attempt {attempt}/{max_retries}): {e}; retrying in {wait:.1f}s")
            time.sleep(wait)
    return None  # pragma: no cover - loop always returns


def insert_candles(
    conn: sqlite3.Connection, exchange_id: int, pair_id: int, timeframe: str, candles: List[List]
) -> int:
    """Insert candles into ohlcv_data; returns rows ACTUALLY inserted.

    INSERT OR IGNORE skips duplicates, so cursor.rowcount is 1 for an inserted
    row and 0 for an ignored one — re-runs don't inflate the count.
    """
    if not candles:
        return 0

    inserted = 0
    for idx, candle in enumerate(candles):
        # Binance kline format: [open_time, open, high, low, close, volume, close_time, ...]
        ts_ms = candle[0]
        open_p = float(candle[1])
        high_p = float(candle[2])
        low_p = float(candle[3])
        close_p = float(candle[4])
        volume = float(candle[5])

        ts = datetime.fromtimestamp(ts_ms / 1000, tz=timezone.utc).isoformat()

        try:
            cur = conn.execute(
                """INSERT OR IGNORE INTO ohlcv_data
                   (exchange_id, trading_pair_id, timeframe, open_price, high_price, low_price, close_price, volume, timestamp)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                (exchange_id, pair_id, timeframe, open_p, high_p, low_p, close_p, volume, ts),
            )
            if cur.rowcount > 0:
                inserted += 1
        except Exception as e:
            # Never swallow a failed insert for an ingestion script whose whole
            # purpose is completeness: roll back the partial batch and abort
            # with the row index and reason so the caller reports INCOMPLETE.
            conn.rollback()
            raise FetchError(
                f"insert failed at candle index {idx} (open_time={ts_ms}): {e}",
            ) from e

    conn.commit()
    return inserted


def _check_page_contiguous(candles: List[List], interval_ms: int, page_start_ms: int) -> None:
    """Every candle inside a page must be one interval after the previous one.

    The across-page check only inspects the first candle of a page, so a page
    truncated or stitched mid-way would otherwise land in the DB as a silent
    hole (clever-cabin-237f).
    """
    for idx in range(1, len(candles)):
        expected_open = candles[idx - 1][0] + interval_ms
        if candles[idx][0] != expected_open:
            raise FetchError(
                f"data gap inside page starting at {page_start_ms}: candle {idx} "
                f"open {candles[idx][0]}, expected {expected_open}"
            )


def fetch_range(
    symbol: str,
    interval: str,
    start_dt: datetime,
    end_dt: datetime,
    fetch_fn: Optional[callable] = None,
    conn: Optional[sqlite3.Connection] = None,
    db_path: Optional[str] = None,
) -> int:
    """Fetch all klines for a date range with pagination.

    Raises FetchError (instead of silently truncating) when a page cannot be
    fetched, a page comes back empty, data is non-contiguous, pagination makes
    no progress, or a row insert fails — the caller reports INCOMPLETE with a
    non-zero exit. ``fetch_fn``, ``conn`` and ``db_path`` are injectable for
    tests; two fetches against different DBs can run in one process.
    """
    if fetch_fn is None:
        fetch_fn = fetch_klines
    own_conn = conn is None
    if own_conn:
        conn = get_db_connection(db_path or DB_PATH)
    exchange_id = get_or_create_exchange(conn, "binance")
    pair_id = get_or_create_trading_pair(conn, exchange_id, symbol)

    interval_ms = _interval_ms(interval)
    # Candle open times sit on a fixed grid. Align both bounds DOWN to it so
    # the expected count is exact and every grid slot is requested, whatever
    # wall-clock timestamps the caller passed.
    start_ms = int(start_dt.timestamp() * 1000)
    start_ms -= start_ms % interval_ms
    end_ms = int(end_dt.timestamp() * 1000)
    end_ms -= end_ms % interval_ms
    span_ms = end_ms - start_ms
    if span_ms <= 0:
        raise FetchError(
            f"empty range: start {start_dt.isoformat()} is not before end {end_dt.isoformat()}"
        )
    grid_start = datetime.fromtimestamp(start_ms / 1000, tz=timezone.utc)
    grid_end = datetime.fromtimestamp(end_ms / 1000, tz=timezone.utc)
    print(f"\nFetching {symbol} {interval} from {grid_start.isoformat()} to {grid_end.isoformat()}")
    expected = span_ms // interval_ms
    print(f"Expected candles: ~{expected:,}")

    total_inserted = 0
    total_requests = 0
    current_ms = start_ms
    prev_open_ms = None
    # Every open time fetched in this run. INSERT OR IGNORE hides duplicates
    # from a previous run, so coverage is verified against what was FETCHED
    # (the grid), not against total_inserted.
    seen: set = set()

    try:
        while current_ms < end_ms:
            raw = fetch_fn(symbol.replace("/", ""), interval, current_ms, end_ms, 1000)
            total_requests += 1

            if raw is None:
                raise FetchError(f"page fetch failed at {current_ms} (request {total_requests})")
            # end_ms is exclusive: Binance may return the candle forming at the
            # boundary, which belongs to the NEXT range.
            candles = [c for c in raw if c[0] < end_ms]
            if not candles:
                raise FetchError(f"empty page at {current_ms}: no data before end of range {end_ms}")
            if prev_open_ms is None and candles[0][0] != start_ms:
                # History that begins after the requested start (a pair listed
                # later, or a truncated first page): the count alone would
                # still look complete if enough later candles came back.
                raise FetchError(
                    f"history starts at {candles[0][0]}, expected the range grid to begin at {start_ms}"
                )
            if prev_open_ms is not None and candles[0][0] != prev_open_ms + interval_ms:
                raise FetchError(
                    f"data gap: expected next candle at {prev_open_ms + interval_ms}, got {candles[0][0]}"
                )
            _check_page_contiguous(candles, interval_ms, current_ms)
            seen.update(c[0] for c in candles)

            inserted = insert_candles(conn, exchange_id, pair_id, interval, candles)
            total_inserted += inserted

            # Termination is coverage-based: the window's candle grid starts at
            # ``start_ms``/interval; once the last returned open time has no
            # successor below ``end_ms`` every slot is covered. Breaking on
            # close_time instead could strand the tail of a truncated page.
            last_open = candles[-1][0]
            next_open = last_open + interval_ms
            if next_open >= end_ms:
                break
            # Progress is guaranteed: the first page starts at start_ms and
            # every later page must start at prev+interval, so next_open is
            # always strictly greater than current_ms.
            prev_open_ms = last_open
            current_ms = next_open

            if total_requests % 10 == 0 and span_ms > 0:
                progress = (current_ms - start_ms) / span_ms * 100
                print(f"  Progress: {progress:.1f}% | Requests: {total_requests} | Inserted: {total_inserted:,}")

            time.sleep(RATE_LIMIT_DELAY)

        # Completeness gate: a truncated page, an early stop or a hole that
        # slipped past the per-page checks would otherwise look exactly like a
        # complete 5-year series. Fail loudly instead (clever-cabin-237f).
        if len(seen) != expected:
            first = min(seen) if seen else None
            last = max(seen) if seen else None
            raise FetchError(
                f"incomplete range for {symbol} {interval}: fetched {len(seen):,} candles, "
                f"expected {expected:,} ({grid_start.isoformat()}..{grid_end.isoformat()}; "
                f"first open={first}, last open={last})"
            )
    finally:
        if own_conn:
            conn.close()

    print(f"  Complete: {total_requests} requests, {total_inserted:,} candles inserted")
    return total_inserted


def main(argv: Optional[List[str]] = None) -> int:
    parser = argparse.ArgumentParser(description="Fetch Binance historical klines into NeuraTrade SQLite.")
    parser.add_argument(
        "--db",
        default=_DEFAULT_DB,
        help="NeuraTrade SQLite DB path (default: $NEURATRADE_DB or ~/.neuratrade/data/neuratrade.db)",
    )
    parser.add_argument(
        "--symbols",
        nargs="+",
        default=["BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT"],
        help="Trading pairs to fetch, e.g. BTCUSDT ETHUSDC",
    )
    parser.add_argument(
        "--start",
        type=_parse_boundary,
        default="2021-06-01",
        help="Range start (ISO date/datetime, UTC when naive; default: 2021-06-01)",
    )
    parser.add_argument(
        "--end",
        type=_parse_boundary,
        default=None,
        help="Range end (ISO date/datetime, UTC when naive; default: now, clamped to now)",
    )
    args = parser.parse_args(argv)
    db_path = args.db  # threaded explicitly; no module-global mutation

    now = datetime.now(timezone.utc)
    start = args.start
    # Never request candles that do not exist yet: a fixed future end makes the
    # strict empty-page rule abort every run before that date, and a fixed past
    # end silently stops short of the present (clever-cabin-46da).
    end = args.end if args.end is not None else now
    if end > now:
        print(f"End {end.date()} is in the future; clamping to now ({now.date()})")
        end = now
    if end <= start:
        print(f"Invalid range: start {start.date()} is not before end {end.date()}", file=sys.stderr)
        return 2

    print("=" * 60)
    print("BINANCE 5-YEAR HISTORICAL DATA FETCHER")
    print("=" * 60)
    print(f"Range: {start.date()} to {end.date()}")
    print(f"Symbols: {args.symbols}")
    print(f"Interval: 5m")
    print(f"DB: {db_path}")
    print("=" * 60)

    grand_total = 0
    try:
        for symbol in args.symbols:
            count = fetch_range(symbol, "5m", start, end, db_path=db_path)
            grand_total += count
            print()
    except FetchError as e:
        print("=" * 60)
        print(f"INCOMPLETE: {e}")
        print("Fetched data is partial; run again to resume (INSERT OR IGNORE is idempotent).")
        print("=" * 60)
        return 1

    print("=" * 60)
    print(f"GRAND TOTAL: {grand_total:,} candles inserted")
    print("=" * 60)
    return 0


if __name__ == "__main__":
    sys.exit(main())
