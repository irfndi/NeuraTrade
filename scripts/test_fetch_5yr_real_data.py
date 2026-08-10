"""Tests for scripts/fetch-5yr-real-data.py.

Runs under stdlib unittest (pytest-compatible classes):
    cd scripts && python3 -m unittest -v test_fetch_5yr_real_data
    python3 -m pytest scripts/test_fetch_5yr_real_data.py -q   # if pytest available
"""

import importlib.util
import io
import json
import os
import sqlite3
import unittest
import urllib.error
from datetime import datetime, timedelta, timezone
from unittest.mock import patch

_HERE = os.path.dirname(os.path.abspath(__file__))


def _load_module():
    path = os.path.join(_HERE, "fetch-5yr-real-data.py")
    spec = importlib.util.spec_from_file_location("fetch_5yr_real_data", path)
    mod = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(mod)
    return mod


f5 = _load_module()

INTERVAL_MS = 300_000  # 5m candle


def candle(open_ms: int, interval_ms: int = INTERVAL_MS) -> list:
    """One Binance kline row: [open_time, o, h, l, c, vol, close_time, ...]."""
    return [open_ms, "1.0", "1.0", "1.0", "1.0", "100.0", open_ms + interval_ms - 1, "ignored"]


class FakeResponse:
    def __init__(self, payload: bytes):
        self._payload = payload

    def __enter__(self):
        return self

    def __exit__(self, *exc):
        return False

    def read(self):
        return self._payload


def make_conn() -> sqlite3.Connection:
    """In-memory DB mirroring the real NeuraTrade sqlite schema (see
    services/backend-api/database/sqlite_migrations/077_create_ohlcv_candles_compat.sql)."""
    conn = sqlite3.connect(":memory:")
    conn.executescript(
        """
        PRAGMA foreign_keys = ON;
        CREATE TABLE exchanges (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            name TEXT NOT NULL,
            api_url TEXT,
            is_active INTEGER NOT NULL DEFAULT 1
        );
        CREATE TABLE trading_pairs (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            exchange_id INTEGER NOT NULL REFERENCES exchanges(id),
            symbol TEXT NOT NULL,
            base_currency TEXT,
            quote_currency TEXT,
            is_active INTEGER NOT NULL DEFAULT 1
        );
        CREATE TABLE ohlcv_data (
            id INTEGER PRIMARY KEY AUTOINCREMENT,
            exchange_id INTEGER NOT NULL,
            trading_pair_id INTEGER NOT NULL,
            timeframe TEXT NOT NULL,
            open_price NUMERIC NOT NULL,
            high_price NUMERIC NOT NULL,
            low_price NUMERIC NOT NULL,
            close_price NUMERIC NOT NULL,
            volume NUMERIC NOT NULL,
            timestamp DATETIME NOT NULL,
            created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
            UNIQUE(exchange_id, trading_pair_id, timeframe, timestamp)
        );
        """
    )
    return conn


def start_dt() -> datetime:
    return datetime(2021, 6, 1, tzinfo=timezone.utc)


class PaginationTests(unittest.TestCase):
    def test_normal_multi_page_pagination(self):
        """Two pages, contiguous, terminates when the last close >= end_ms."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=25)  # 5 candles of 5m
        start_ms = int(start.timestamp() * 1000)
        end_ms = int(end.timestamp() * 1000)

        # Page 1: 4 candles; page 2: 2 candles (last close >= end_ms -> stop).
        pages = [
            [candle(start_ms + i * INTERVAL_MS) for i in range(4)],
            [candle(start_ms + 4 * INTERVAL_MS), candle(start_ms + 5 * INTERVAL_MS)],
        ]
        calls = []

        def fake_fetch(symbol, interval, cur_ms, e_ms, limit):
            calls.append(cur_ms)
            return pages.pop(0)

        with patch("time.sleep") as sleep:
            total = f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=fake_fetch, conn=conn)

        self.assertEqual(total, 6)
        self.assertEqual(calls, [start_ms, start_ms + 4 * INTERVAL_MS])
        self.assertEqual(sleep.call_count, 1)  # one inter-page delay
        rows = conn.execute("SELECT COUNT(*) FROM ohlcv_data").fetchone()[0]
        self.assertEqual(rows, 6)

    def test_single_page_end_of_range_termination(self):
        """A page whose last candle reaches end_ms terminates after one request."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=10)
        start_ms = int(start.timestamp() * 1000)
        end_ms = int(end.timestamp() * 1000)
        # Two candles; last close (start_ms + 2*300000 - 1) >= end_ms (start_ms + 600000).
        page = [candle(start_ms), candle(start_ms + INTERVAL_MS)]

        with patch("time.sleep"):
            total = f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=lambda *a, **k: page, conn=conn)

        self.assertEqual(total, 2)

    def test_empty_page_raises(self):
        """An empty page mid-range is a data gap, not a silent 'Complete'."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=10)

        with self.assertRaises(f5.FetchError) as ctx:
            f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=lambda *a, **k: [], conn=conn)
        self.assertIn("empty page", str(ctx.exception))

    def test_failed_page_aborts(self):
        """fetch_fn returning None (persistent fetch failure) aborts loudly."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=10)

        with self.assertRaises(f5.FetchError) as ctx:
            f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=lambda *a, **k: None, conn=conn)
        self.assertIn("page fetch failed", str(ctx.exception))

    def test_non_contiguous_page_raises(self):
        """Next page not starting at prev close + 1ms is a gap."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=25)
        start_ms = int(start.timestamp() * 1000)
        pages = [
            [candle(start_ms), candle(start_ms + INTERVAL_MS)],
            [candle(start_ms + 3 * INTERVAL_MS)],  # skips open at +2*INTERVAL
        ]

        def fake_fetch(*a, **k):
            return pages.pop(0)

        with self.assertRaises(f5.FetchError) as ctx:
            f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=fake_fetch, conn=conn)
        self.assertIn("data gap", str(ctx.exception))

    def test_no_progress_page_raises(self):
        """A page that does not advance current_ms must not loop forever."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=10)
        start_ms = int(start.timestamp() * 1000)
        # Candle's close_time < current_ms (already-inserted data returned again).
        stale = [candle(start_ms - INTERVAL_MS)]

        with self.assertRaises(f5.FetchError) as ctx:
            f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=lambda *a, **k: stale, conn=conn)
        self.assertIn("no progress", str(ctx.exception))

    def test_main_incomplete_returns_nonzero(self):
        """main() exits 1 and reports INCOMPLETE when a range fails."""
        with patch.object(f5, "fetch_range", side_effect=f5.FetchError("boom")), patch("sys.stdout"):
            rc = f5.main(["--db", ":memory:", "--symbols", "BTCUSDT"])
        self.assertEqual(rc, 1)

    def test_main_success_returns_zero(self):
        with patch.object(f5, "fetch_range", return_value=42), patch("sys.stdout"):
            rc = f5.main(["--db", ":memory:", "--symbols", "BTCUSDT"])
        self.assertEqual(rc, 0)


class RetryTests(unittest.TestCase):
    def test_429_then_success_recovers(self):
        """Two 429s then success: bounded retry with backoff, no recursion."""
        err = urllib.error.HTTPError("url", 429, "Too Many Requests", {"Retry-After": "0.5"}, None)
        resp = FakeResponse(json.dumps([[0, "1", "1", "1", "1", "1", 299999, "x"]]).encode())
        with patch.object(f5.urllib.request, "urlopen", side_effect=[err, err, resp]) as urlopen, patch("time.sleep") as sleep:
            result = f5.fetch_klines("BTCUSDT", "5m", 0, 999999)
        self.assertEqual(result, [[0, "1", "1", "1", "1", "1", 299999, "x"]])
        self.assertEqual(urlopen.call_count, 3)
        self.assertEqual(sleep.call_count, 2)
        # First wait honors Retry-After (0.5s + jitter).
        first_wait = sleep.call_args_list[0][0][0]
        self.assertGreaterEqual(first_wait, 0.5)
        self.assertLess(first_wait, 0.5 * 1.2 + 0.001)
        err.close()

    def test_429_exhaustion_returns_none(self):
        """Persistent 429 stops after MAX_RETRIES attempts instead of recursing."""
        err = urllib.error.HTTPError("url", 429, "Too Many Requests", {}, None)
        with patch.object(f5.urllib.request, "urlopen", side_effect=err) as urlopen, patch("time.sleep"):
            result = f5.fetch_klines("BTCUSDT", "5m", 0, 999999)
        self.assertIsNone(result)
        self.assertEqual(urlopen.call_count, f5.MAX_RETRIES)
        err.close()

    def test_5xx_is_retried(self):
        err = urllib.error.HTTPError("url", 503, "Service Unavailable", {}, None)
        resp = FakeResponse(b"[]")
        with patch.object(f5.urllib.request, "urlopen", side_effect=[err, resp]) as urlopen, patch("time.sleep"):
            result = f5.fetch_klines("BTCUSDT", "5m", 0, 999999)
        self.assertEqual(result, [])
        self.assertEqual(urlopen.call_count, 2)
        err.close()

    def test_non_retryable_4xx_fails_immediately(self):
        err = urllib.error.HTTPError("url", 400, "Bad Request", {}, None)
        with patch.object(f5.urllib.request, "urlopen", side_effect=err) as urlopen, patch("time.sleep"):
            result = f5.fetch_klines("BTCUSDT", "5m", 0, 999999)
        self.assertIsNone(result)
        self.assertEqual(urlopen.call_count, 1)
        err.close()

    def test_transient_network_error_retried(self):
        with patch.object(
            f5.urllib.request, "urlopen", side_effect=[OSError("reset"), FakeResponse(b"[]")]
        ) as urlopen, patch("time.sleep"):
            result = f5.fetch_klines("BTCUSDT", "5m", 0, 999999)
        self.assertEqual(result, [])
        self.assertEqual(urlopen.call_count, 2)


class InsertCountTests(unittest.TestCase):
    def test_insert_count_excludes_ignored_duplicates(self):
        """Re-running the same candles returns 0 inserted (INSERT OR IGNORE)."""
        conn = make_conn()
        self.addCleanup(conn.close)
        ex = f5.get_or_create_exchange(conn, "binance")
        pair = f5.get_or_create_trading_pair(conn, ex, "BTCUSDT")
        candles = [candle(0), candle(INTERVAL_MS)]

        first = f5.insert_candles(conn, ex, pair, "5m", candles)
        second = f5.insert_candles(conn, ex, pair, "5m", candles)

        self.assertEqual(first, 2)
        self.assertEqual(second, 0)
        rows = conn.execute("SELECT COUNT(*) FROM ohlcv_data").fetchone()[0]
        self.assertEqual(rows, 2)

    def test_insert_empty_returns_zero(self):
        conn = make_conn()
        self.addCleanup(conn.close)
        self.assertEqual(f5.insert_candles(conn, 1, 1, "5m", []), 0)


class QuoteAndPathTests(unittest.TestCase):
    def test_quote_derived_from_suffix(self):
        conn = make_conn()
        self.addCleanup(conn.close)
        ex = f5.get_or_create_exchange(conn, "binance")
        cases = {
            "BTCUSDT": ("BTC/USDT", "BTC", "USDT"),
            "ETHUSDC": ("ETH/USDC", "ETH", "USDC"),
            "BNBBUSD": ("BNB/BUSD", "BNB", "BUSD"),
            "BTC/USDT": ("BTC/USDT", "BTC", "USDT"),
        }
        for symbol, (display, base, quote) in cases.items():
            pair = f5.get_or_create_trading_pair(conn, ex, symbol)
            row = conn.execute(
                "SELECT symbol, base_currency, quote_currency FROM trading_pairs WHERE id = ?", (pair,)
            ).fetchone()
            self.assertEqual(tuple(row), (display, base, quote), symbol)

    def test_unsupported_symbol_raises(self):
        conn = make_conn()
        self.addCleanup(conn.close)
        with self.assertRaises(ValueError):
            f5.get_or_create_trading_pair(conn, 1, "FOOXYZ")

    def test_default_db_path_uses_env_home(self):
        with patch.dict(os.environ, {}, clear=True):
            default = f5._default_db_path()
            self.assertTrue(default.endswith(os.path.join(".neuratrade", "data", "neuratrade.db")))
        with patch.dict(os.environ, {"NEURATRADE_HOME": "/tmp/x"}, clear=True):
            self.assertEqual(f5._default_db_path(), os.path.join("/tmp", "x", "data", "neuratrade.db"))
        with patch.dict(os.environ, {"NEURATRADE_DB": "/tmp/d.db"}, clear=True):
            # _default_db_path ignores NEURATRADE_DB (it is the fallback); the
            # env var wins in the module-level composition.
            self.assertNotIn("d.db", f5._default_db_path())
            self.assertEqual(os.environ.get("NEURATRADE_DB") or f5._default_db_path(), "/tmp/d.db")

    def test_main_accepts_db_arg(self):
        with patch.object(f5, "fetch_range", return_value=0), patch("sys.stdout"):
            f5.main(["--db", "/tmp/custom.db", "--symbols", "BTCUSDT"])
        self.assertEqual(f5.DB_PATH, "/tmp/custom.db")


if __name__ == "__main__":
    unittest.main()
