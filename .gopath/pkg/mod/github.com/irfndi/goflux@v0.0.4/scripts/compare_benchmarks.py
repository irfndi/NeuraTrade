#!/usr/bin/env python3
"""Compare benchmark results and check for regressions"""

import sys
import re


def parse_benchmark_file(filename):
    """Parse benchmark output file into a dictionary"""
    results = {}
    with open(filename, "r") as f:
        for line in f:
            # Match lines like: BenchmarkSMA-20    5000000    3.5 ns/op    2 B/op    1 allocs/op
            match = re.search(
                r"Benchmark(\w+)-\d+\s+(\d+)\s+(\d+)\s+ns/op\s+(\d+\.\d+)\s+B/op\s+(\d+)\s+allocs/op",
                line,
            )
            if match:
                name = match.group(1)
                ops = int(match.group(2))
                ns_per_op = float(match.group(3))
                b_per_op = int(match.group(4))
                allocs_per_op = int(match.group(5))

                results[name] = {
                    "ns_per_op": ns_per_op,
                    "b_per_op": b_per_op,
                    "allocs_per_op": allocs_per_op,
                }
    return results


def compare_benchmarks(current_file, baseline_file, threshold=1.1):
    """Compare current benchmark results with baseline and check for regressions"""
    current = parse_benchmark_file(current_file)
    baseline = parse_benchmark_file(baseline_file)

    print(f"\n{'=' * 60}")
    print(f"Performance Comparison (threshold: {threshold * 100}% regression)")
    print(f"{'=' * 60}")

    print(
        f"\n{'=' * 60}{'Benchmark Name':<30}{'Current':<20}{'Baseline':<20}{'Change':<10}"
    )
    print(f"{'-' * 60}")

    regressions = []

    for name in current:
        if name in baseline:
            current_ns = current[name]["ns_per_op"]
            baseline_ns = baseline[name]["ns_per_op"]

            change_percent = (current_ns - baseline_ns) / baseline_ns

            print(
                f"{name:<30}{current_ns:>15.2f}{baseline_ns:>15.2f}{change_percent:>15.2%}"
            )

            # Check for regression (performance got worse by threshold)
            if change_percent > threshold:
                regressions.append(
                    {
                        "name": name,
                        "current_ns": current_ns,
                        "baseline_ns": baseline_ns,
                        "change_percent": change_percent,
                    }
                )
        else:
            print(
                f"{name:<30}{current[name]['ns_per_op']:>15.2f}{'N/A':>15}{'N/A':>10}"
            )

    if regressions:
        print(f"\n{'=' * 60}{'⚠️  PERFORMANCE REGRESSIONS DETECTED':<20}")
        print(f"{'-' * 60}")
        for r in regressions:
            print(f"  {r['name']}: {r['change_percent']:.1%} slower")
            print(f"    Current: {r['current_ns']:.2f} ns/op")
            print(f"    Baseline: {r['baseline_ns']:.2f} ns/op")

        sys.exit(1)
    else:
        print(f"\n{'=' * 60}✅ No performance regressions detected")
        sys.exit(0)


if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(
            "Usage: python compare_benchmarks.py <current_results.txt> <baseline_results.txt> [threshold]"
        )
        sys.exit(1)

    current_file = sys.argv[1]
    baseline_file = sys.argv[2]
    threshold = float(sys.argv[3]) if len(sys.argv) > 3 else 1.1

    compare_benchmarks(current_file, baseline_file, threshold)
