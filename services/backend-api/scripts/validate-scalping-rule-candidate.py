#!/usr/bin/env python3
"""Validate a scalping rule candidate against observed soak telemetry.

The script evaluates 5-minute forward returns from scalping_cycle_telemetry rows
and requires a candidate rule to pass both training and validation DB sets before
it is considered evidence-backed.
"""

from __future__ import annotations

import argparse
import json
import sqlite3
import sys
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Iterable


ROUND_TRIP_FEE_PCT = 0.12


@dataclass(frozen=True)
class Observation:
    timestamp: float
    symbol: str
    spread: float
    imbalance: float
    range_position: float
    change_24h: float
    recent_change: float
    buy_net_pct: float
    sell_net_pct: float


@dataclass(frozen=True)
class Rule:
    side: str
    min_imbalance: float | None
    max_imbalance: float | None
    max_spread: float | None
    min_range: float | None
    max_range: float | None
    min_recent: float | None
    max_recent: float | None
    min_change_24h: float | None
    max_change_24h: float | None

    def matches(self, obs: Observation) -> bool:
        return (
            in_min(obs.imbalance, self.min_imbalance)
            and in_max(obs.imbalance, self.max_imbalance)
            and in_max(obs.spread, self.max_spread)
            and in_min(obs.range_position, self.min_range)
            and in_max(obs.range_position, self.max_range)
            and in_min(obs.recent_change, self.min_recent)
            and in_max(obs.recent_change, self.max_recent)
            and in_min(obs.change_24h, self.min_change_24h)
            and in_max(obs.change_24h, self.max_change_24h)
        )

    def net_pct(self, obs: Observation) -> float:
        if self.side == "buy":
            return obs.buy_net_pct
        return obs.sell_net_pct


def in_min(value: float, floor: float | None) -> bool:
    return floor is None or value >= floor


def in_max(value: float, ceiling: float | None) -> bool:
    return ceiling is None or value <= ceiling


def parse_timestamp(value: str) -> float:
    normalized = value.replace("Z", "+00:00")
    try:
        return datetime.fromisoformat(normalized).timestamp()
    except ValueError:
        pass

    if "+" in normalized:
        main, offset = normalized.rsplit("+", 1)
        suffix = "+" + offset
    else:
        main = normalized
        suffix = ""
    if "." in main:
        head, frac = main.split(".", 1)
        normalized = f"{head}.{(frac + '000000')[:6]}{suffix}"
    else:
        normalized = main + suffix
    return datetime.fromisoformat(normalized).timestamp()


def load_observations(paths: Iterable[Path], hold_seconds: int, fee_pct: float) -> list[Observation]:
    observations: list[Observation] = []
    for path in paths:
        observations.extend(load_observations_from_db(path, hold_seconds, fee_pct))
    observations.sort(key=lambda obs: (obs.timestamp, obs.symbol))
    return observations


def load_observations_from_db(path: Path, hold_seconds: int, fee_pct: float) -> list[Observation]:
    if not path.is_file():
        raise ValueError(f"database not found: {path}")

    conn = sqlite3.connect(path)
    conn.row_factory = sqlite3.Row
    try:
        columns = {row[1] for row in conn.execute("PRAGMA table_info(scalping_cycle_telemetry)")}
        required = {
            "exchange",
            "symbol",
            "cycle_at",
            "signal_price",
            "bid_ask_spread_pct",
            "order_book_imbalance",
            "range_position_24h",
            "price_change_24h_pct",
            "recent_price_change_pct",
        }
        missing = sorted(required - columns)
        if missing:
            raise ValueError(f"{path} missing scalping telemetry columns: {', '.join(missing)}")

        rows_by_series: dict[tuple[str, str], list[sqlite3.Row]] = {}
        for row in conn.execute(
            """
            SELECT exchange, symbol, cycle_at, signal_price, bid_ask_spread_pct,
                   order_book_imbalance, range_position_24h, price_change_24h_pct,
                   recent_price_change_pct
            FROM scalping_cycle_telemetry
            WHERE signal_price > 0
              AND bid_ask_spread_pct IS NOT NULL
              AND order_book_imbalance IS NOT NULL
              AND range_position_24h IS NOT NULL
              AND price_change_24h_pct IS NOT NULL
              AND recent_price_change_pct IS NOT NULL
            ORDER BY exchange, symbol, cycle_at
            """
        ):
            rows_by_series.setdefault((row["exchange"], row["symbol"]), []).append(row)

        observations: list[Observation] = []
        for rows in rows_by_series.values():
            timestamps = [parse_timestamp(row["cycle_at"]) for row in rows]
            future_index = 0
            for index, row in enumerate(rows):
                if future_index <= index:
                    future_index = index + 1
                target = timestamps[index] + hold_seconds
                while future_index < len(rows) and timestamps[future_index] < target:
                    future_index += 1
                if future_index >= len(rows):
                    continue

                entry_price = float(row["signal_price"])
                exit_price = float(rows[future_index]["signal_price"])
                buy_net_pct = ((exit_price - entry_price) / entry_price * 100) - fee_pct
                sell_net_pct = ((entry_price - exit_price) / entry_price * 100) - fee_pct
                observations.append(
                    Observation(
                        timestamp=timestamps[index],
                        symbol=str(row["symbol"]),
                        spread=float(row["bid_ask_spread_pct"]),
                        imbalance=float(row["order_book_imbalance"]),
                        range_position=float(row["range_position_24h"]),
                        change_24h=float(row["price_change_24h_pct"]),
                        recent_change=float(row["recent_price_change_pct"]),
                        buy_net_pct=buy_net_pct,
                        sell_net_pct=sell_net_pct,
                    )
                )
        return observations
    finally:
        conn.close()


def evaluate(observations: list[Observation], rule: Rule, hold_seconds: int) -> dict[str, object]:
    values: list[float] = []
    symbols: set[str] = set()
    next_allowed_by_symbol: dict[str, float] = {}

    for obs in observations:
        if obs.timestamp < next_allowed_by_symbol.get(obs.symbol, -1):
            continue
        if not rule.matches(obs):
            continue

        values.append(rule.net_pct(obs))
        symbols.add(obs.symbol)
        next_allowed_by_symbol[obs.symbol] = obs.timestamp + hold_seconds

    wins = sum(1 for value in values if value > 0)
    losses = len(values) - wins
    net = sum(values)
    best = max(values) if values else 0.0
    return {
        "trades": len(values),
        "wins": wins,
        "losses": losses,
        "win_rate": wins / len(values) if values else 0.0,
        "net_pct": net,
        "avg_net_pct": net / len(values) if values else 0.0,
        "net_pct_excluding_best": net - best if values else 0.0,
        "worst_trade_pct": min(values) if values else 0.0,
        "best_trade_pct": best,
        "symbols": len(symbols),
    }


def validate_group(name: str, stats: dict[str, object], args: argparse.Namespace, validation: bool) -> list[str]:
    prefix = "validation_" if validation else ""
    failures: list[str] = []
    min_trades = getattr(args, f"min_{prefix}trades")
    min_symbols = getattr(args, f"min_{prefix}symbols")
    min_net = getattr(args, f"min_{prefix}net_pct")
    min_net_ex_best = getattr(args, f"min_{prefix}net_pct_excluding_best")

    if stats["trades"] < min_trades:
        failures.append(f"{name}.trades={stats['trades']} below minimum={min_trades}")
    if stats["symbols"] < min_symbols:
        failures.append(f"{name}.symbols={stats['symbols']} below minimum={min_symbols}")
    if stats["wins"] < args.min_wins:
        failures.append(f"{name}.wins={stats['wins']} below minimum={args.min_wins}")
    if stats["losses"] < args.min_losses:
        failures.append(f"{name}.losses={stats['losses']} below minimum={args.min_losses}")
    if stats["net_pct"] < min_net:
        failures.append(f"{name}.net_pct={stats['net_pct']:.6f} below minimum={min_net:.6f}")
    if stats["net_pct_excluding_best"] < min_net_ex_best:
        failures.append(
            f"{name}.net_pct_excluding_best={stats['net_pct_excluding_best']:.6f} "
            f"below minimum={min_net_ex_best:.6f}"
        )
    return failures


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--train-db", action="append", required=True, type=Path)
    parser.add_argument("--validation-db", action="append", required=True, type=Path)
    parser.add_argument("--side", choices=("buy", "sell"), required=True)
    parser.add_argument("--min-imbalance", type=float)
    parser.add_argument("--max-imbalance", type=float)
    parser.add_argument("--max-spread", type=float)
    parser.add_argument("--min-range", type=float)
    parser.add_argument("--max-range", type=float)
    parser.add_argument("--min-recent", type=float)
    parser.add_argument("--max-recent", type=float)
    parser.add_argument("--min-24h", dest="min_change_24h", type=float)
    parser.add_argument("--max-24h", dest="max_change_24h", type=float)
    parser.add_argument("--hold-seconds", type=int, default=300)
    parser.add_argument("--fee-pct", type=float, default=ROUND_TRIP_FEE_PCT)
    parser.add_argument("--min-trades", type=int, default=20)
    parser.add_argument("--min-validation-trades", type=int, default=1)
    parser.add_argument("--min-wins", type=int, default=1)
    parser.add_argument("--min-losses", type=int, default=1)
    parser.add_argument("--min-symbols", type=int, default=3)
    parser.add_argument("--min-validation-symbols", type=int, default=1)
    parser.add_argument("--min-net-pct", type=float, default=0.0)
    parser.add_argument("--min-validation-net-pct", type=float, default=0.0)
    parser.add_argument("--min-net-pct-excluding-best", type=float, default=0.0)
    parser.add_argument("--min-validation-net-pct-excluding-best", type=float, default=0.0)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.hold_seconds <= 0:
        raise ValueError("--hold-seconds must be positive")
    if args.fee_pct < 0:
        raise ValueError("--fee-pct must be non-negative")

    rule = Rule(
        side=args.side,
        min_imbalance=args.min_imbalance,
        max_imbalance=args.max_imbalance,
        max_spread=args.max_spread,
        min_range=args.min_range,
        max_range=args.max_range,
        min_recent=args.min_recent,
        max_recent=args.max_recent,
        min_change_24h=args.min_change_24h,
        max_change_24h=args.max_change_24h,
    )
    train_stats = evaluate(load_observations(args.train_db, args.hold_seconds, args.fee_pct), rule, args.hold_seconds)
    validation_stats = evaluate(
        load_observations(args.validation_db, args.hold_seconds, args.fee_pct),
        rule,
        args.hold_seconds,
    )
    failures = validate_group("train", train_stats, args, validation=False)
    failures.extend(validate_group("validation", validation_stats, args, validation=True))

    payload = {
        "rule": {
            "side": args.side,
            "min_imbalance": args.min_imbalance,
            "max_imbalance": args.max_imbalance,
            "max_spread": args.max_spread,
            "min_range": args.min_range,
            "max_range": args.max_range,
            "min_recent": args.min_recent,
            "max_recent": args.max_recent,
            "min_24h": args.min_change_24h,
            "max_24h": args.max_change_24h,
            "hold_seconds": args.hold_seconds,
            "fee_pct": args.fee_pct,
        },
        "train": train_stats,
        "validation": validation_stats,
        "passed": not failures,
        "failures": failures,
    }
    print(json.dumps(payload, indent=2, sort_keys=True))
    return 0 if not failures else 1


if __name__ == "__main__":
    try:
        sys.exit(main())
    except Exception as exc:  # noqa: BLE001 - CLI should report any validation/setup error.
        print(f"validate-scalping-rule-candidate: {exc}", file=sys.stderr)
        sys.exit(2)
