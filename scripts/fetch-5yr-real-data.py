#!/usr/bin/env python3
"""
Paginated Binance historical klines fetcher for NeuraTrade backtesting.
Fetches 5m candles from Binance REST API with automatic pagination.
Inserts directly into NeuraTrade SQLite ohlcv_data table.
"""

import sqlite3
import time
import json
import urllib.request
import urllib.error
import sys
from datetime import datetime, timezone
from typing import List, Tuple, Optional

DB_PATH = "/Users/irfandi/.neuratrade/data/neuratrade.db"
BINANCE_API = "https://api.binance.com/api/v3/klines"
RATE_LIMIT_DELAY = 0.15  # ~6-7 requests/sec to stay under Binance limits


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
        (name, "https://api.binance.com")
    )
    conn.commit()
    lid = cur.lastrowid
    if lid is None:
        raise RuntimeError("Failed to insert exchange")
    return lid


def get_or_create_trading_pair(conn: sqlite3.Connection, exchange_id: int, symbol: str) -> int:
    # Convert BTCUSDT -> BTC/USDT for display
    display_symbol = symbol
    if "/" not in symbol and "USDT" in symbol:
        display_symbol = symbol.replace("USDT", "") + "/USDT"
    
    base = display_symbol.replace("/USDT", "").replace("/USDC", "").replace("/BUSD", "")
    quote = "USDT"
    
    cur = conn.execute("SELECT id FROM trading_pairs WHERE exchange_id = ? AND symbol = ?", (exchange_id, display_symbol))
    row = cur.fetchone()
    if row:
        return row[0]
    cur = conn.execute(
        "INSERT INTO trading_pairs (exchange_id, symbol, base_currency, quote_currency, is_active) VALUES (?, ?, ?, ?, 1)",
        (exchange_id, display_symbol, base, quote)
    )
    conn.commit()
    lid = cur.lastrowid
    if lid is None:
        raise RuntimeError("Failed to insert trading pair")
    return lid


def fetch_klines(symbol: str, interval: str, start_ms: int, end_ms: int, limit: int = 1000) -> List[List]:
    """Fetch a single page of klines from Binance."""
    url = f"{BINANCE_API}?symbol={symbol}&interval={interval}&startTime={start_ms}&endTime={end_ms}&limit={limit}"
    req = urllib.request.Request(url, headers={"User-Agent": "NeuraTrade-Backtest/1.0"})
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            return data
    except urllib.error.HTTPError as e:
        if e.code == 429:
            print(f"  Rate limited. Sleeping 60s...")
            time.sleep(60)
            return fetch_klines(symbol, interval, start_ms, end_ms, limit)
        print(f"  HTTP Error {e.code}: {e.reason}")
        return []
    except Exception as e:
        print(f"  Error: {e}")
        return []


def insert_candles(conn: sqlite3.Connection, exchange_id: int, pair_id: int, 
                   timeframe: str, candles: List[List]) -> int:
    """Insert candles into ohlcv_data table."""
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
            conn.execute(
                """INSERT OR IGNORE INTO ohlcv_data 
                   (exchange_id, trading_pair_id, timeframe, open_price, high_price, low_price, close_price, volume, timestamp)
                   VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)""",
                (exchange_id, pair_id, timeframe, open_p, high_p, low_p, close_p, volume, ts)
            )
            inserted += 1
        except Exception as e:
            print(f"  Insert error: {e}")
    
    conn.commit()
    return inserted


def fetch_range(symbol: str, interval: str, start_dt: datetime, end_dt: datetime) -> int:
    """Fetch all klines for a date range with pagination."""
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
    
    while current_ms < end_ms:
        candles = fetch_klines(symbol.replace("/", ""), interval, current_ms, end_ms, 1000)
        total_requests += 1
        
        if not candles:
            break
        
        inserted = insert_candles(conn, exchange_id, pair_id, interval, candles)
        total_inserted += inserted
        
        # Next page starts from the last candle's close time + 1ms
        last_ts = candles[-1][6]  # close_time
        if last_ts >= end_ms or last_ts <= current_ms:
            break
        current_ms = last_ts + 1
        
        if total_requests % 10 == 0:
            progress = (current_ms - start_ms) / (end_ms - start_ms) * 100
            print(f"  Progress: {progress:.1f}% | Requests: {total_requests} | Inserted: {total_inserted:,}")
        
        time.sleep(RATE_LIMIT_DELAY)
    
    conn.close()
    print(f"  Complete: {total_requests} requests, {total_inserted:,} candles inserted")
    return total_inserted


def main():
    # 5-year range: 2021-06-01 to 2026-06-01
    start = datetime(2021, 6, 1, tzinfo=timezone.utc)
    end = datetime(2026, 6, 1, tzinfo=timezone.utc)
    
    symbols = ["BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT"]
    
    print("=" * 60)
    print("BINANCE 5-YEAR HISTORICAL DATA FETCHER")
    print("=" * 60)
    print(f"Range: {start.date()} to {end.date()}")
    print(f"Symbols: {symbols}")
    print(f"Interval: 5m")
    print(f"DB: {DB_PATH}")
    print("=" * 60)
    
    grand_total = 0
    for symbol in symbols:
        count = fetch_range(symbol, "5m", start, end)
        grand_total += count
        print()
    
    print("=" * 60)
    print(f"GRAND TOTAL: {grand_total:,} candles inserted")
    print("=" * 60)


if __name__ == "__main__":
    main()
