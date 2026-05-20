#!/usr/bin/env python3
"""Validate or search scalping rule candidates against observed soak telemetry.

The script evaluates 5-minute forward returns from scalping_cycle_telemetry rows
and requires a candidate rule to pass both training and validation DB sets before
it is considered evidence-backed.
"""

from __future__ import annotations

import argparse
import itertools
import json
import sqlite3
import sys
from dataclasses import dataclass
from datetime import datetime
from pathlib import Path
from typing import Iterable


ROUND_TRIP_FEE_PCT = 0.12
DEFAULT_SEARCH_MAX_RESULTS = 20
DEFAULT_PORTFOLIO_POOL_SIZE = 50
DEFAULT_MAX_PORTFOLIO_RULES = 2

BUY_SEARCH_GRIDS = {
    "max_spread": [0.02, 0.04, 0.06, 0.08, 0.10, 0.14, 0.22],
    "min_imbalance": [0.10, 0.20, 0.30, 0.40, 0.50, 0.60],
    "max_range": [35.0, 45.0, 55.0, 65.0, 75.0, 85.0, 100.0],
    "min_recent": [-0.15, -0.05, 0.0, 0.03, 0.05, 0.10, 0.15],
    "min_change_24h": [-0.06, -0.02, 0.0, 0.01, 0.02],
}

SELL_SEARCH_GRIDS = {
    "max_spread": [0.02, 0.04, 0.06, 0.08, 0.10, 0.14, 0.22],
    "max_imbalance": [-0.10, -0.20, -0.30, -0.40, -0.50, -0.60],
    "min_range": [15.0, 25.0, 35.0, 45.0, 55.0, 65.0, 75.0, 85.0, 95.0],
    "max_recent": [-0.15, -0.10, -0.05, 0.0, 0.05, 0.10, 0.20],
    "max_change_24h": [-0.08, -0.05, -0.03, -0.01, 0.0, 0.03, 0.08],
}


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


def load_train_validation_observations(
    args: argparse.Namespace,
    hold_seconds: int,
) -> tuple[list[Observation], list[Observation]]:
    if args.validation_split_ratio > 0:
        return split_observations(
            load_observations(args.train_db, hold_seconds, args.fee_pct),
            args.validation_split_ratio,
        )
    return (
        load_observations(args.train_db, hold_seconds, args.fee_pct),
        load_observations(args.validation_db, hold_seconds, args.fee_pct),
    )


def split_observations(observations: list[Observation], validation_split_ratio: float) -> tuple[list[Observation], list[Observation]]:
    if validation_split_ratio <= 0 or validation_split_ratio >= 1:
        raise ValueError("--validation-split-ratio must be greater than 0 and less than 1")
    if len(observations) < 2:
        raise ValueError("--validation-split-ratio requires at least two observations")

    ordered = sorted(observations, key=lambda obs: (obs.timestamp, obs.symbol))
    validation_count = max(1, int(round(len(ordered) * validation_split_ratio)))
    if validation_count >= len(ordered):
        validation_count = len(ordered) - 1
    split_index = len(ordered) - validation_count
    return ordered[:split_index], ordered[split_index:]


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
    cumulative = 0.0
    peak = 0.0
    max_drawdown = 0.0

    for obs in observations:
        if obs.timestamp < next_allowed_by_symbol.get(obs.symbol, -1):
            continue
        if not rule.matches(obs):
            continue

        net_pct = rule.net_pct(obs)
        values.append(net_pct)
        symbols.add(obs.symbol)
        next_allowed_by_symbol[obs.symbol] = obs.timestamp + hold_seconds
        cumulative += net_pct
        if cumulative > peak:
            peak = cumulative
        drawdown = peak - cumulative
        if drawdown > max_drawdown:
            max_drawdown = drawdown

    wins = sum(1 for value in values if value > 0)
    losses = sum(1 for value in values if value < 0)
    breakevens = len(values) - wins - losses
    net = sum(values)
    best = max(values) if values else 0.0
    return {
        "trades": len(values),
        "wins": wins,
        "losses": losses,
        "breakevens": breakevens,
        "win_rate": wins / len(values) if values else 0.0,
        "net_pct": net,
        "avg_net_pct": net / len(values) if values else 0.0,
        "net_pct_excluding_best": net - best if values else 0.0,
        "max_drawdown_pct": max_drawdown,
        "worst_trade_pct": min(values) if values else 0.0,
        "best_trade_pct": best,
        "symbols": len(symbols),
    }


def evaluate_portfolio(observations: list[Observation], rules: list[Rule], hold_seconds: int) -> dict[str, object]:
    values: list[float] = []
    symbols: set[str] = set()
    side_counts: dict[str, int] = {}
    next_allowed_by_symbol: dict[str, float] = {}
    cumulative = 0.0
    peak = 0.0
    max_drawdown = 0.0

    for obs in observations:
        if obs.timestamp < next_allowed_by_symbol.get(obs.symbol, -1):
            continue
        matched_rule = next((rule for rule in rules if rule.matches(obs)), None)
        if matched_rule is None:
            continue

        net_pct = matched_rule.net_pct(obs)
        values.append(net_pct)
        symbols.add(obs.symbol)
        side_counts[matched_rule.side] = side_counts.get(matched_rule.side, 0) + 1
        next_allowed_by_symbol[obs.symbol] = obs.timestamp + hold_seconds
        cumulative += net_pct
        if cumulative > peak:
            peak = cumulative
        drawdown = peak - cumulative
        if drawdown > max_drawdown:
            max_drawdown = drawdown

    wins = sum(1 for value in values if value > 0)
    losses = sum(1 for value in values if value < 0)
    breakevens = len(values) - wins - losses
    net = sum(values)
    best = max(values) if values else 0.0
    return {
        "trades": len(values),
        "wins": wins,
        "losses": losses,
        "breakevens": breakevens,
        "win_rate": wins / len(values) if values else 0.0,
        "net_pct": net,
        "avg_net_pct": net / len(values) if values else 0.0,
        "net_pct_excluding_best": net - best if values else 0.0,
        "max_drawdown_pct": max_drawdown,
        "worst_trade_pct": min(values) if values else 0.0,
        "best_trade_pct": best,
        "symbols": len(symbols),
        "side_counts": side_counts,
    }


def rule_payload(rule: Rule, hold_seconds: int, fee_pct: float) -> dict[str, object]:
    return {
        "side": rule.side,
        "min_imbalance": rule.min_imbalance,
        "max_imbalance": rule.max_imbalance,
        "max_spread": rule.max_spread,
        "min_range": rule.min_range,
        "max_range": rule.max_range,
        "min_recent": rule.min_recent,
        "max_recent": rule.max_recent,
        "min_24h": rule.min_change_24h,
        "max_24h": rule.max_change_24h,
        "hold_seconds": hold_seconds,
        "fee_pct": fee_pct,
    }


def portfolio_payload(rules: list[Rule], hold_seconds: int, fee_pct: float) -> list[dict[str, object]]:
    return [rule_payload(rule, hold_seconds, fee_pct) for rule in rules]


def validate_group(name: str, stats: dict[str, object], args: argparse.Namespace, validation: bool) -> list[str]:
    prefix = "validation_" if validation else ""
    failures: list[str] = []
    min_trades = getattr(args, f"min_{prefix}trades")
    min_symbols = getattr(args, f"min_{prefix}symbols")
    min_net = getattr(args, f"min_{prefix}net_pct")
    min_net_ex_best = getattr(args, f"min_{prefix}net_pct_excluding_best")
    min_drawdown = getattr(args, f"min_{prefix}drawdown_pct")

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
    if stats["max_drawdown_pct"] < min_drawdown:
        failures.append(
            f"{name}.max_drawdown_pct={stats['max_drawdown_pct']:.6f} "
            f"below minimum={min_drawdown:.6f}"
        )
    return failures


def gate_deficit_value(actual: float, minimum: float) -> float:
    if actual >= minimum:
        return 0.0
    if minimum > 0:
        return (minimum - actual) / minimum
    return minimum - actual


def gate_deficit(stats: dict[str, object], args: argparse.Namespace, validation: bool) -> float:
    prefix = "validation_" if validation else ""
    min_trades = getattr(args, f"min_{prefix}trades")
    min_symbols = getattr(args, f"min_{prefix}symbols")
    min_net = getattr(args, f"min_{prefix}net_pct")
    min_net_ex_best = getattr(args, f"min_{prefix}net_pct_excluding_best")
    min_drawdown = getattr(args, f"min_{prefix}drawdown_pct")

    return sum(
        [
            gate_deficit_value(float(stats["trades"]), float(min_trades)),
            gate_deficit_value(float(stats["symbols"]), float(min_symbols)),
            gate_deficit_value(float(stats["wins"]), float(args.min_wins)),
            gate_deficit_value(float(stats["losses"]), float(args.min_losses)),
            gate_deficit_value(float(stats["net_pct"]), float(min_net)),
            gate_deficit_value(float(stats["net_pct_excluding_best"]), float(min_net_ex_best)),
            gate_deficit_value(float(stats["max_drawdown_pct"]), float(min_drawdown)),
        ]
    )


def candidate_gate_deficit(
    train_stats: dict[str, object],
    validation_stats: dict[str, object],
    args: argparse.Namespace,
) -> float:
    return gate_deficit(train_stats, args, validation=False) + gate_deficit(validation_stats, args, validation=True)


def rank_rule_candidates(
    observations: list[Observation],
    rules: list[Rule],
    hold_seconds: int,
    pool_size: int,
) -> list[tuple[Rule, dict[str, object]]]:
    ranked: list[tuple[Rule, dict[str, object]]] = []
    for rule in rules:
        stats = evaluate(observations, rule, hold_seconds)
        if stats["trades"] == 0:
            continue
        ranked.append((rule, stats))

    ranked.sort(
        key=lambda item: (
            item[1]["net_pct_excluding_best"],
            item[1]["net_pct"],
            item[1]["trades"],
            item[1]["avg_net_pct"],
        ),
        reverse=True,
    )
    return ranked[:pool_size]


def search_result_rank_key(result: dict[str, object]) -> tuple[object, ...]:
    validation = result["validation"]
    train = result["train"]
    failures = result.get("failures", [])
    return (
        result.get("gate_deficit", float("inf")),
        -validation["trades"],
        -train["trades"],
        -validation["symbols"],
        -train["symbols"],
        -validation["net_pct"],
        -train["net_pct"],
        len(failures),
    )


def ranked_near_misses(near_misses: list[dict[str, object]], limit: int) -> list[dict[str, object]]:
    if limit <= 0:
        return []
    near_misses.sort(key=search_result_rank_key)
    return near_misses[:limit]


def search_candidate_rules(side: str | None) -> list[Rule]:
    sides = ("buy", "sell") if side in (None, "both") else (side,)
    rules: list[Rule] = []
    seen: set[tuple[object, ...]] = set()

    def append(rule: Rule) -> None:
        key = (
            rule.side,
            rule.min_imbalance,
            rule.max_imbalance,
            rule.max_spread,
            rule.min_range,
            rule.max_range,
            rule.min_recent,
            rule.max_recent,
            rule.min_change_24h,
            rule.max_change_24h,
        )
        if key not in seen:
            seen.add(key)
            rules.append(rule)

    if "buy" in sides:
        for max_spread in BUY_SEARCH_GRIDS["max_spread"]:
            for min_imbalance in BUY_SEARCH_GRIDS["min_imbalance"]:
                for max_range in BUY_SEARCH_GRIDS["max_range"]:
                    for min_recent in BUY_SEARCH_GRIDS["min_recent"]:
                        for min_change_24h in BUY_SEARCH_GRIDS["min_change_24h"]:
                            append(
                                Rule(
                                    side="buy",
                                    min_imbalance=min_imbalance,
                                    max_imbalance=None,
                                    max_spread=max_spread,
                                    min_range=None,
                                    max_range=max_range,
                                    min_recent=min_recent,
                                    max_recent=None,
                                    min_change_24h=min_change_24h,
                                    max_change_24h=None,
                                )
                            )
    if "sell" in sides:
        for max_spread in SELL_SEARCH_GRIDS["max_spread"]:
            for max_imbalance in SELL_SEARCH_GRIDS["max_imbalance"]:
                for min_range in SELL_SEARCH_GRIDS["min_range"]:
                    for max_recent in SELL_SEARCH_GRIDS["max_recent"]:
                        for max_change_24h in SELL_SEARCH_GRIDS["max_change_24h"]:
                            append(
                                Rule(
                                    side="sell",
                                    min_imbalance=None,
                                    max_imbalance=max_imbalance,
                                    max_spread=max_spread,
                                    min_range=min_range,
                                    max_range=None,
                                    min_recent=None,
                                    max_recent=max_recent,
                                    min_change_24h=None,
                                    max_change_24h=max_change_24h,
                                )
                            )
    return rules


def search_portfolio_candidates(
    train_observations: list[Observation],
    validation_observations: list[Observation],
    args: argparse.Namespace,
) -> dict[str, object]:
    pool_size = max(args.portfolio_pool_size, 1)
    max_rules = max(args.max_portfolio_rules, 1)
    base_rules = search_candidate_rules(args.side)
    if args.side in (None, "both"):
        ranked = []
        ranked.extend(
            rank_rule_candidates(train_observations, search_candidate_rules("buy"), args.hold_seconds, pool_size)
        )
        ranked.extend(
            rank_rule_candidates(train_observations, search_candidate_rules("sell"), args.hold_seconds, pool_size)
        )
    else:
        ranked = rank_rule_candidates(train_observations, base_rules, args.hold_seconds, pool_size)

    rules = []
    seen_rules: set[tuple[object, ...]] = set()
    for rule, _ in ranked:
        key = (
            rule.side,
            rule.min_imbalance,
            rule.max_imbalance,
            rule.max_spread,
            rule.min_range,
            rule.max_range,
            rule.min_recent,
            rule.max_recent,
            rule.min_change_24h,
            rule.max_change_24h,
        )
        if key in seen_rules:
            continue
        seen_rules.add(key)
        rules.append(rule)

    candidates: list[dict[str, object]] = []
    near_misses: list[dict[str, object]] = []
    evaluated_portfolios = 0
    max_rules = min(max_rules, len(rules))
    for rule_count in range(2, max_rules + 1):
        for portfolio_rules in itertools.combinations(rules, rule_count):
            evaluated_portfolios += 1
            train_stats = evaluate_portfolio(train_observations, list(portfolio_rules), args.hold_seconds)
            validation_stats = evaluate_portfolio(validation_observations, list(portfolio_rules), args.hold_seconds)
            failures = validate_group("train", train_stats, args, validation=False)
            failures.extend(validate_group("validation", validation_stats, args, validation=True))
            if failures:
                if args.near_misses > 0:
                    near_misses.append(
                        {
                            "rules": portfolio_payload(list(portfolio_rules), args.hold_seconds, args.fee_pct),
                            "train": train_stats,
                            "validation": validation_stats,
                            "failures": failures,
                            "gate_deficit": candidate_gate_deficit(train_stats, validation_stats, args),
                        }
                    )
                continue
            candidates.append(
                {
                    "rules": portfolio_payload(list(portfolio_rules), args.hold_seconds, args.fee_pct),
                    "train": train_stats,
                    "validation": validation_stats,
                }
            )

    candidates.sort(
        key=lambda candidate: (
            candidate["validation"]["net_pct"],
            candidate["train"]["net_pct"],
            candidate["validation"]["trades"],
            candidate["train"]["trades"],
            candidate["validation"]["avg_net_pct"],
        ),
        reverse=True,
    )
    max_results = max(args.max_results, 0)
    if max_results > 0:
        candidates = candidates[:max_results]
    failures = [] if candidates else ["no_candidate_portfolio_passed_train_validation_gates"]
    payload = {
        "search_portfolio": True,
        "side": args.side or "both",
        "hold_seconds": args.hold_seconds,
        "evaluated_rules": len(base_rules),
        "portfolio_pool_size": len(rules),
        "max_portfolio_rules": max_rules,
        "evaluated_portfolios": evaluated_portfolios,
        "candidate_count": len(candidates),
        "passed": bool(candidates),
        "candidates": candidates,
        "failures": failures,
    }
    if args.near_misses > 0:
        payload["near_misses"] = ranked_near_misses(near_misses, args.near_misses)
    return payload


def search_rule_candidates(
    train_observations: list[Observation],
    validation_observations: list[Observation],
    args: argparse.Namespace,
) -> dict[str, object]:
    candidates: list[dict[str, object]] = []
    near_misses: list[dict[str, object]] = []
    for rule in search_candidate_rules(args.side):
        train_stats = evaluate(train_observations, rule, args.hold_seconds)
        validation_stats = evaluate(validation_observations, rule, args.hold_seconds)
        failures = validate_group("train", train_stats, args, validation=False)
        failures.extend(validate_group("validation", validation_stats, args, validation=True))
        if failures:
            if args.near_misses > 0:
                near_misses.append(
                    {
                        "rule": rule_payload(rule, args.hold_seconds, args.fee_pct),
                        "train": train_stats,
                        "validation": validation_stats,
                        "failures": failures,
                        "gate_deficit": candidate_gate_deficit(train_stats, validation_stats, args),
                    }
                )
            continue
        candidates.append(
            {
                "rule": rule_payload(rule, args.hold_seconds, args.fee_pct),
                "train": train_stats,
                "validation": validation_stats,
            }
        )

    candidates.sort(
        key=lambda candidate: (
            candidate["validation"]["net_pct"],
            candidate["train"]["net_pct"],
            candidate["validation"]["trades"],
            candidate["train"]["trades"],
            candidate["validation"]["avg_net_pct"],
        ),
        reverse=True,
    )
    max_results = max(args.max_results, 0)
    if max_results > 0:
        candidates = candidates[:max_results]
    failures = [] if candidates else ["no_candidate_rule_passed_train_validation_gates"]
    payload = {
        "search_grid": True,
        "side": args.side or "both",
        "hold_seconds": args.hold_seconds,
        "evaluated_rules": len(search_candidate_rules(args.side)),
        "candidate_count": len(candidates),
        "passed": bool(candidates),
        "candidates": candidates,
        "failures": failures,
    }
    if args.near_misses > 0:
        payload["near_misses"] = ranked_near_misses(near_misses, args.near_misses)
    return payload


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--train-db", action="append", required=True, type=Path)
    parser.add_argument("--validation-db", action="append", type=Path)
    parser.add_argument(
        "--validation-split-ratio",
        type=float,
        default=0.0,
        help="chronologically reserve this fraction of --train-db observations for validation",
    )
    parser.add_argument("--search-grid", action="store_true", help="search conservative buy/sell threshold grids")
    parser.add_argument(
        "--search-portfolio",
        action="store_true",
        help="search bounded multi-rule portfolios from the strongest train-set threshold rules",
    )
    parser.add_argument("--max-results", type=int, default=DEFAULT_SEARCH_MAX_RESULTS)
    parser.add_argument("--portfolio-pool-size", type=int, default=DEFAULT_PORTFOLIO_POOL_SIZE)
    parser.add_argument("--max-portfolio-rules", type=int, default=DEFAULT_MAX_PORTFOLIO_RULES)
    parser.add_argument("--near-misses", type=int, default=0, help="include this many top failing candidates")
    parser.add_argument("--side", choices=("buy", "sell", "both"))
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
    parser.add_argument(
        "--hold-seconds-candidates",
        help="comma-separated hold durations to sweep with --search-grid or --search-portfolio",
    )
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
    parser.add_argument("--min-drawdown-pct", type=float, default=0.0)
    parser.add_argument("--min-validation-drawdown-pct", type=float, default=0.0)
    return parser.parse_args()


def parse_hold_seconds_candidates(raw: str) -> list[int]:
    holds: list[int] = []
    seen: set[int] = set()
    for part in raw.split(","):
        value = part.strip()
        if not value:
            continue
        try:
            hold = int(value)
        except ValueError as exc:
            raise ValueError("--hold-seconds-candidates must contain positive integers") from exc
        if hold <= 0:
            raise ValueError("--hold-seconds-candidates must contain positive integers")
        if hold in seen:
            continue
        seen.add(hold)
        holds.append(hold)
    if not holds:
        raise ValueError("--hold-seconds-candidates must contain at least one hold duration")
    return holds


def run_search_for_hold(args: argparse.Namespace, hold_seconds: int) -> dict[str, object]:
    hold_args = argparse.Namespace(**vars(args))
    hold_args.hold_seconds = hold_seconds
    train_observations, validation_observations = load_train_validation_observations(hold_args, hold_seconds)
    if hold_args.search_portfolio:
        return search_portfolio_candidates(train_observations, validation_observations, hold_args)
    if hold_args.search_grid:
        return search_rule_candidates(train_observations, validation_observations, hold_args)
    raise ValueError("--hold-seconds-candidates requires --search-grid or --search-portfolio")


def run_hold_sweep(args: argparse.Namespace, holds: list[int]) -> dict[str, object]:
    results = [run_search_for_hold(args, hold_seconds) for hold_seconds in holds]
    passed = any(result["passed"] for result in results)
    candidate_count = sum(int(result["candidate_count"]) for result in results)
    failures = [] if passed else ["no_hold_candidate_passed_train_validation_gates"]
    return {
        "hold_sweep": True,
        "search_mode": "portfolio" if args.search_portfolio else "grid",
        "holds": holds,
        "candidate_count": candidate_count,
        "passed": passed,
        "results": results,
        "failures": failures,
    }


def main() -> int:
    args = parse_args()
    if args.hold_seconds <= 0:
        raise ValueError("--hold-seconds must be positive")
    if args.fee_pct < 0:
        raise ValueError("--fee-pct must be non-negative")
    if args.min_drawdown_pct < 0:
        raise ValueError("--min-drawdown-pct must be non-negative")
    if args.min_validation_drawdown_pct < 0:
        raise ValueError("--min-validation-drawdown-pct must be non-negative")
    if args.max_results < 0:
        raise ValueError("--max-results must be zero or greater")
    if args.near_misses < 0:
        raise ValueError("--near-misses must be zero or greater")
    if args.validation_split_ratio < 0 or args.validation_split_ratio >= 1:
        raise ValueError("--validation-split-ratio must be greater than 0 and less than 1")
    if args.validation_split_ratio > 0 and args.validation_db:
        raise ValueError("--validation-split-ratio cannot be combined with --validation-db")
    if args.validation_split_ratio <= 0 and not args.validation_db:
        raise ValueError("--validation-db is required unless --validation-split-ratio is set")

    if args.search_grid and args.search_portfolio:
        raise ValueError("--search-grid cannot be combined with --search-portfolio")

    if args.hold_seconds_candidates:
        if not args.search_grid and not args.search_portfolio:
            raise ValueError("--hold-seconds-candidates requires --search-grid or --search-portfolio")
        holds = parse_hold_seconds_candidates(args.hold_seconds_candidates)
        payload = run_hold_sweep(args, holds)
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0 if payload["passed"] else 1

    train_observations, validation_observations = load_train_validation_observations(args, args.hold_seconds)

    if args.search_portfolio:
        if args.portfolio_pool_size <= 0:
            raise ValueError("--portfolio-pool-size must be positive")
        if args.max_portfolio_rules < 2:
            raise ValueError("--max-portfolio-rules must be at least 2")
        if args.max_portfolio_rules > 3:
            raise ValueError("--max-portfolio-rules above 3 is intentionally unsupported")
        payload = search_portfolio_candidates(train_observations, validation_observations, args)
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0 if payload["passed"] else 1

    if args.search_grid:
        payload = search_rule_candidates(train_observations, validation_observations, args)
        print(json.dumps(payload, indent=2, sort_keys=True))
        return 0 if payload["passed"] else 1

    if args.side in (None, "both"):
        raise ValueError("--side buy|sell is required unless --search-grid is enabled")

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
    train_stats = evaluate(train_observations, rule, args.hold_seconds)
    validation_stats = evaluate(validation_observations, rule, args.hold_seconds)
    failures = validate_group("train", train_stats, args, validation=False)
    failures.extend(validate_group("validation", validation_stats, args, validation=True))

    payload = {
        "rule": rule_payload(rule, args.hold_seconds, args.fee_pct),
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
