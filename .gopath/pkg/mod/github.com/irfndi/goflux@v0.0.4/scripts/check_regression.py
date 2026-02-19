#!/usr/bin/env python3
"""Check benchmark results for regressions and exit with error if found"""

import sys
from compare_benchmarks import compare_benchmarks

if __name__ == "__main__":
    if len(sys.argv) != 3:
        print(
            "Usage: python check_regression.py <current_results.txt> <baseline_results.txt> [threshold]"
        )
        sys.exit(1)

    current_file = sys.argv[1]
    baseline_file = sys.argv[2]
    threshold = float(sys.argv[3]) if len(sys.argv) > 3 else 1.1

    # Run comparison - will exit with 1 if regression detected
    compare_benchmarks(current_file, baseline_file, threshold)
