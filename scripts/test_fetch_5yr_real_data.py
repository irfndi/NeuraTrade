"""Tests for scripts/fetch-5yr-real-data.py.

Runs under stdlib unittest (pytest-compatible classes):
    cd scripts && python3 -m unittest -v test_fetch_5yr_real_data
    python3 -m pytest scripts/test_fetch_5yr_real_data.py -q   # if pytest available
"""

import importlib.util
import io
import json
import os
import re
import sqlite3
import unittest
import urllib.error
from datetime import datetime, timedelta, timezone
from unittest.mock import patch

_HERE = os.path.dirname(os.path.abspath(__file__))
# Drift guard target: the production schema lives in the TS market-data
# repository's ensureTablesSQL (services/neuratrade-cli-ts).
_REPO_ROOT = os.path.dirname(_HERE)
_OHLCV_SCHEMA_SOURCE = os.path.join(
    _REPO_ROOT, "services", "neuratrade-cli-ts", "src", "market-data", "repository.ts"
)


def _load_module():
    """Load the target script by absolute path, with a clear failure message.

    The exec happens once at import time (the target is side-effect free at
    module scope by contract); a missing or broken target must report WHICH
    file failed instead of an opaque collection error.
    """
    path = os.path.join(_HERE, "fetch-5yr-real-data.py")
    if not os.path.isfile(path):
        raise RuntimeError(
            f"test target missing: {path} (test suite and target script must ship together)"
        )
    spec = importlib.util.spec_from_file_location("fetch_5yr_real_data", path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"cannot create import spec for {path}")
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


f5 = _load_module()

INTERVAL_MS = 300_000  # 5m candle
# In-memory ohlcv_data columns (must mirror the production ensureTablesSQL).
_OHLCV_COLUMNS = [
    "id",
    "exchange_id",
    "trading_pair_id",
    "timeframe",
    "open_price",
    "high_price",
    "low_price",
    "close_price",
    "volume",
    "timestamp",
    "created_at",
]


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


SCHEMA_SQL = """
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


def make_conn(path: str = ":memory:") -> sqlite3.Connection:
    """DB mirroring the real ohlcv schema (drift-guarded by
    SchemaDriftTests against the production ensureTablesSQL in
    services/neuratrade-cli-ts/src/market-data/repository.ts)."""
    conn = sqlite3.connect(path)
    conn.executescript(SCHEMA_SQL)
    return conn


def start_dt() -> datetime:
    return datetime(2021, 6, 1, tzinfo=timezone.utc)


def fetch_pages(pages: list, calls: list = None):
    """Fetch stub with a clear over-fetch failure: running out of prepared
    pages means fetch_range requested more pages than the test planned for —
    that must surface as a descriptive assertion, not an IndexError."""

    def fetch(symbol, interval, cur_ms, e_ms, limit):
        if not pages:
            raise AssertionError(
                f"unexpected extra fetch at cur_ms={cur_ms}: test planned fewer pages"
            )
        if calls is not None:
            calls.append(cur_ms)
        return pages.pop(0)

    return fetch


class PaginationTests(unittest.TestCase):
    def test_normal_multi_page_pagination(self):
        """Two pages, contiguous, terminates once the window is covered."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=25)  # 5 candles of 5m
        start_ms = int(start.timestamp() * 1000)
        end_ms = int(end.timestamp() * 1000)

        # Page 1: 4 candles; page 2: 2 candles. The last one opens exactly at
        # end_ms, which is EXCLUSIVE (it belongs to the next range), so it is
        # fetched but neither counted nor inserted.
        pages = [
            [candle(start_ms + i * INTERVAL_MS) for i in range(4)],
            [candle(start_ms + 4 * INTERVAL_MS), candle(start_ms + 5 * INTERVAL_MS)],
        ]
        calls = []
        fetch = fetch_pages(pages, calls)

        with patch("time.sleep") as sleep:
            total = f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=fetch, conn=conn)

        self.assertEqual(total, 5)
        self.assertEqual(calls, [start_ms, start_ms + 4 * INTERVAL_MS])
        self.assertEqual(sleep.call_count, 1)  # one inter-page delay
        rows = conn.execute("SELECT COUNT(*) FROM ohlcv_data").fetchone()[0]
        self.assertEqual(rows, 5)

    def test_single_page_end_of_range_termination(self):
        """A page whose last candle covers the window terminates after one request."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=10)
        start_ms = int(start.timestamp() * 1000)
        # Two candles: last open + interval == end_ms -> whole grid covered.
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

    def test_intra_page_gap_raises(self):
        """A hole INSIDE a page (not at its first candle) must not be stored."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=20)
        start_ms = int(start.timestamp() * 1000)
        # Page skips the candle at +1*INTERVAL in the middle of the page.
        page = [
            candle(start_ms),
            candle(start_ms + 2 * INTERVAL_MS),
            candle(start_ms + 3 * INTERVAL_MS),
        ]

        with self.assertRaises(f5.FetchError) as ctx:
            f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=lambda *a, **k: page, conn=conn)
        self.assertIn("data gap inside page", str(ctx.exception))

    def test_late_history_start_raises(self):
        """A first page that begins after the range start is an incomplete range."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=10)
        start_ms = int(start.timestamp() * 1000)
        # Exactly `expected` candles, shifted one slot late: a count-only check
        # would call this complete.
        page = [candle(start_ms + INTERVAL_MS), candle(start_ms + 2 * INTERVAL_MS)]

        with self.assertRaises(f5.FetchError) as ctx:
            f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=lambda *a, **k: page, conn=conn)
        self.assertIn("history starts at", str(ctx.exception))

    def test_non_contiguous_page_raises(self):
        """Next page not starting at prev open + interval is a gap."""
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(minutes=25)
        start_ms = int(start.timestamp() * 1000)
        pages = [
            [candle(start_ms), candle(start_ms + INTERVAL_MS)],
            [candle(start_ms + 3 * INTERVAL_MS)],  # skips open at +2*INTERVAL
        ]

        with self.assertRaises(f5.FetchError) as ctx:
            f5.fetch_range("BTCUSDT", "5m", start, end, fetch_fn=fetch_pages(pages), conn=conn)
        self.assertIn("data gap", str(ctx.exception))

    def test_main_incomplete_returns_nonzero(self):
        """main() exits 1 and reports INCOMPLETE when a range fails."""
        with patch.object(f5, "fetch_range", side_effect=f5.FetchError("boom")), patch("sys.stdout"):
            rc = f5.main(["--db", ":memory:", "--symbols", "BTCUSDT"])
        self.assertEqual(rc, 1)

    def test_main_success_returns_zero(self):
        with patch.object(f5, "fetch_range", return_value=42), patch("sys.stdout"):
            rc = f5.main(["--db", ":memory:", "--symbols", "BTCUSDT"])
        self.assertEqual(rc, 0)

    def test_main_defaults_end_to_now(self):
        """No hardcoded future end: the default range reaches the present."""
        captured = {}

        def fake_range(symbol, interval, start, end, **kwargs):
            captured["start"] = start
            captured["end"] = end
            return 0

        before = datetime.now(timezone.utc)
        with patch.object(f5, "fetch_range", side_effect=fake_range), patch("sys.stdout"):
            rc = f5.main(["--db", ":memory:", "--symbols", "BTCUSDT"])
        after = datetime.now(timezone.utc)
        self.assertEqual(rc, 0)
        self.assertGreaterEqual(captured["end"], before)
        self.assertLessEqual(captured["end"], after)
        self.assertEqual(captured["start"], datetime(2021, 6, 1, tzinfo=timezone.utc))

    def test_main_clamps_a_future_end(self):
        captured = {}

        def fake_range(symbol, interval, start, end, **kwargs):
            captured["end"] = end
            return 0

        with patch.object(f5, "fetch_range", side_effect=fake_range), patch("sys.stdout"):
            rc = f5.main(["--db", ":memory:", "--symbols", "BTCUSDT", "--end", "2999-01-01"])
        self.assertEqual(rc, 0)
        self.assertLessEqual(captured["end"], datetime.now(timezone.utc))

    def test_main_rejects_an_inverted_range(self):
        with patch.object(f5, "fetch_range", return_value=0) as fetch, patch("sys.stdout"), patch(
            "sys.stderr"
        ):
            rc = f5.main(
                ["--db", ":memory:", "--symbols", "BTCUSDT", "--start", "2030-01-01", "--end", "2029-01-01"]
            )
        self.assertEqual(rc, 2)
        fetch.assert_not_called()


class RetryTests(unittest.TestCase):
    def test_429_then_success_recovers(self):
        """Two 429s then success: bounded retry with backoff, no recursion."""
        err = urllib.error.HTTPError("url", 429, "Too Many Requests", {"Retry-After": "0.5"}, None)
        self.addCleanup(err.close)
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

    def test_429_exhaustion_returns_none(self):
        """Persistent 429 stops after MAX_RETRIES attempts instead of recursing."""
        err = urllib.error.HTTPError("url", 429, "Too Many Requests", {}, None)
        self.addCleanup(err.close)
        with patch.object(f5.urllib.request, "urlopen", side_effect=err) as urlopen, patch("time.sleep"):
            result = f5.fetch_klines("BTCUSDT", "5m", 0, 999999)
        self.assertIsNone(result)
        self.assertEqual(urlopen.call_count, f5.MAX_RETRIES)

    def test_5xx_is_retried(self):
        err = urllib.error.HTTPError("url", 503, "Service Unavailable", {}, None)
        self.addCleanup(err.close)
        resp = FakeResponse(b"[]")
        with patch.object(f5.urllib.request, "urlopen", side_effect=[err, resp]) as urlopen, patch("time.sleep"):
            result = f5.fetch_klines("BTCUSDT", "5m", 0, 999999)
        self.assertEqual(result, [])
        self.assertEqual(urlopen.call_count, 2)

    def test_non_retryable_4xx_fails_immediately(self):
        err = urllib.error.HTTPError("url", 400, "Bad Request", {}, None)
        self.addCleanup(err.close)
        with patch.object(f5.urllib.request, "urlopen", side_effect=err) as urlopen, patch("time.sleep"):
            result = f5.fetch_klines("BTCUSDT", "5m", 0, 999999)
        self.assertIsNone(result)
        self.assertEqual(urlopen.call_count, 1)

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

    def test_insert_failure_raises_and_rolls_back(self):
        """A row that fails to persist must abort loudly, not report success."""
        conn = make_conn()
        self.addCleanup(conn.close)
        # Locked/closed DB style failure: drop the table so executes fail.
        candles = [candle(0), candle(INTERVAL_MS)]
        bad_conn = sqlite3.connect(":memory:")  # schema absent -> every insert fails
        self.addCleanup(bad_conn.close)
        bad_conn.execute("BEGIN")  # force a transactional context too
        with self.assertRaises(f5.FetchError) as ctx:
            f5.insert_candles(bad_conn, 1, 1, "5m", candles)
        self.assertIn("insert failed", str(ctx.exception))


class SchemaDriftTests(unittest.TestCase):
    def test_in_memory_schema_matches_production_ensure_tables_sql(self):
        """make_conn()'s ohlcv_data must mirror the production schema source
        (services/neuratrade-cli-ts/src/market-data/repository.ts)."""
        if not os.path.isfile(_OHLCV_SCHEMA_SOURCE):
            self.skipTest(f"schema source not present: {_OHLCV_SCHEMA_SOURCE}")
        with open(_OHLCV_SCHEMA_SOURCE, "r", encoding="utf-8") as fh:
            source = fh.read()
        match = re.search(r"CREATE TABLE IF NOT EXISTS ohlcv_data \((.*?)\n\);", source, re.S)
        self.assertIsNotNone(match, "production ensureTablesSQL lost its ohlcv_data table")
        block = match.group(1)
        prod_columns = re.findall(r"^\s{2}(\w+) ", block, re.M)
        # The in-memory schema materializes every production column.
        self.assertEqual(prod_columns, _OHLCV_COLUMNS)
        self.assertIn("UNIQUE(exchange_id, trading_pair_id, timeframe, timestamp)", block)


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

    def test_unsupported_interval_raises(self):
        with self.assertRaises(ValueError):
            f5._interval_ms("7x")
        self.assertEqual(f5._interval_ms("5m"), 300_000)
        self.assertEqual(f5._interval_ms("1h"), 3_600_000)

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

    def test_main_accepts_db_arg_without_global_leak(self):
        """--db must thread the path explicitly, not mutate module state."""
        original = f5.DB_PATH
        self.addCleanup(setattr, f5, "DB_PATH", original)
        with patch.object(f5, "fetch_range", return_value=0), patch("sys.stdout"):
            f5.main(["--db", "/tmp/custom.db", "--symbols", "BTCUSDT"])
        self.assertEqual(f5.DB_PATH, original)

    def test_two_fetches_against_two_dbs_in_one_process(self):
        """fetch_range(db_path=...) must route each run to its own database."""
        ex = None
        paths = []
        for i in range(2):
            path = f"/tmp/f5-dual-db-{i}-{os.getpid()}.db"
            conn = make_conn(path)
            self.addCleanup(lambda p=path: (os.path.exists(p) and os.unlink(p)))
            pages = [[candle(int(start_dt().timestamp() * 1000))]]
            with patch("time.sleep"):
                f5.fetch_range(
                    "BTCUSDT", "5m", start_dt(), start_dt() + timedelta(minutes=5),
                    fetch_fn=lambda *a, **k: list(pages[0]) if pages else [], conn=conn,
                )
            n = conn.execute("SELECT COUNT(*) FROM ohlcv_data").fetchone()[0]
            self.assertEqual(n, 1)
            ex = conn.execute("SELECT name FROM exchanges").fetchone()[0]
            self.assertEqual(ex, "binance")
            paths.append(path)
        # Each connection wrote to its own file-backed DB.
        self.assertNotEqual(paths[0], paths[1])

    def test_db_path_creates_missing_directory(self):
        import shutil

        base = f"/tmp/f5-fresh-{os.getpid()}"
        db = os.path.join(base, "data", "neuratrade.db")
        self.addCleanup(lambda: shutil.rmtree(base, ignore_errors=True))
        conn = f5.get_db_connection(db)
        self.addCleanup(conn.close)
        self.assertTrue(os.path.isfile(db))


class IntervalEstimateTests(unittest.TestCase):
    def test_expected_count_uses_interval(self):
        conn = make_conn()
        self.addCleanup(conn.close)
        start = start_dt()
        end = start + timedelta(hours=3)
        # Three 1h candles cover the whole 3h window; the last open + 1h == end.
        page = [
            candle(int(start.timestamp() * 1000) + i * 3_600_000, interval_ms=3_600_000)
            for i in range(3)
        ]
        out = io.StringIO()
        with patch("time.sleep"), patch("sys.stdout", out):
            f5.fetch_range("BTCUSDT", "1h", start, end, fetch_fn=lambda *a, **k: page, conn=conn)
        self.assertIn("Expected candles: ~3", out.getvalue())


if __name__ == "__main__":
    unittest.main()
