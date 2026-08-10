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


def get_db_connection():
    conn = sqlite3.connect(DB_PATH)
    conn.execute("PRAGMA foreign_keys = ON")
    return conn


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
    for candle in candles:
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
            print(f"  Insert error: {e}")

    conn.commit()
    return inserted


def fetch_range(
    symbol: str,
    interval: str,
    start_dt: datetime,
    end_dt: datetime,
    fetch_fn: Optional[callable] = None,
    conn: Optional[sqlite3.Connection] = None,
) -> int:
    """Fetch all klines for a date range with pagination.

    Raises FetchError (instead of silently truncating) when a page cannot be
    fetched, a page comes back empty, data is non-contiguous, or pagination
    makes no progress — the caller reports INCOMPLETE with a non-zero exit.
    ``fetch_fn`` and ``conn`` are injectable for tests.
    """
    if fetch_fn is None:
        fetch_fn = fetch_klines
    own_conn = conn is None
    if own_conn:
        conn = get_db_connection()
    exchange_id = get_or_create_exchange(conn, "binance")
    pair_id = get_or_create_trading_pair(conn, exchange_id, symbol)

    start_ms = int(start_dt.timestamp() * 1000)
    end_ms = int(end_dt.timestamp() * 1000)

    print(f"\nFetching {symbol} {interval} from {start_dt.isoformat()} to {end_dt.isoformat()}")
    print(f"Expected candles: ~{((end_dt - start_dt).total_seconds() / 60 / 5):,.0f}")

    total_inserted = 0
    total_requests = 0
    current_ms = start_ms
    prev_close_ms = None

    try:
        while current_ms < end_ms:
            candles = fetch_fn(symbol.replace("/", ""), interval, current_ms, end_ms, 1000)
            total_requests += 1

            if candles is None:
                raise FetchError(f"page fetch failed at {current_ms} (request {total_requests})")
            if not candles:
                raise FetchError(f"empty page at {current_ms}: no data before end of range {end_ms}")
            if prev_close_ms is not None and candles[0][0] != prev_close_ms + 1:
                raise FetchError(f"data gap: expected next candle at {prev_close_ms + 1}, got {candles[0][0]}")

            inserted = insert_candles(conn, exchange_id, pair_id, interval, candles)
            total_inserted += inserted

            last_ts = candles[-1][6]  # close_time
            if last_ts >= end_ms:
                break
            if last_ts < current_ms:
                raise FetchError(f"pagination made no progress at {current_ms}")
            prev_close_ms = last_ts
            current_ms = last_ts + 1

            if total_requests % 10 == 0:
                progress = (current_ms - start_ms) / (end_ms - start_ms) * 100
                print(f"  Progress: {progress:.1f}% | Requests: {total_requests} | Inserted: {total_inserted:,}")

            time.sleep(RATE_LIMIT_DELAY)
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
    args = parser.parse_args(argv)

    global DB_PATH
    DB_PATH = args.db

    # 5-year range: 2021-06-01 to 2026-06-01
    start = datetime(2021, 6, 1, tzinfo=timezone.utc)
    end = datetime(2026, 6, 1, tzinfo=timezone.utc)

    print("=" * 60)
    print("BINANCE 5-YEAR HISTORICAL DATA FETCHER")
    print("=" * 60)
    print(f"Range: {start.date()} to {end.date()}")
    print(f"Symbols: {args.symbols}")
    print(f"Interval: 5m")
    print(f"DB: {DB_PATH}")
    print("=" * 60)

    grand_total = 0
    try:
        for symbol in args.symbols:
            count = fetch_range(symbol, "5m", start, end)
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
