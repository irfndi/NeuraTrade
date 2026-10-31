#!/usr/bin/env bash
# Gate a Bun service's dependency audit on high/critical advisories only.
#
# `bun audit` exits 1 on ANY finding, including moderate/low ones, which made a
# newly-published moderate advisory in a transitive dependency fail CI even
# though the workflow's stated intent ("Gate ... on high/critical") is narrower.
# This wrapper preserves that intent while still failing closed when the audit
# itself crashes or emits an unrecognised shape.
#
# Usage: bun-audit-gate.sh <service-dir>
set -uo pipefail

service_dir="${1:?usage: bun-audit-gate.sh <service-dir>}"
report="$(mktemp)"
trap 'rm -f "$report"' EXIT

cd "$service_dir" || exit 1

# `bun audit --json` exits 1 whenever it REPORTS advisories, so exit status
# alone cannot distinguish findings from a crash. Require a parseable report:
# empty output means the audit genuinely failed and must not read as a pass.
bun audit --json > "$report" 2>/dev/null
if [ ! -s "$report" ]; then
  echo "bun audit produced no report; a crash must not read as a passing gate" >&2
  exit 1
fi

python3 - "$report" <<'PY'
import json
import sys

UNKNOWN_SHAPE = 2  # distinct from findings (1) so shape drift stands out

path = sys.argv[1]
try:
    data = json.load(open(path))
except (json.JSONDecodeError, OSError) as err:
    print(f"audit JSON unreadable: {err}", file=sys.stderr)
    sys.exit(UNKNOWN_SHAPE)


def advisory_pairs(obj):
    """Return [(package, advisory)] for known bun audit shapes, else None."""
    if isinstance(obj, dict) and isinstance(obj.get("vulnerabilities"), dict):
        obj = obj["vulnerabilities"]
    if not isinstance(obj, dict):
        return None
    # An EMPTY dict is a clean report, not shape drift: `bun audit --json`
    # prints `{}` when there are no advisories. Treating it as unknown made a
    # clean audit fail the gate.
    if not obj:
        return []
    out = []
    for pkg, val in obj.items():
        if not isinstance(val, (dict, list)):
            return None
        for adv in val if isinstance(val, list) else [val]:
            if not isinstance(adv, dict) or "severity" not in adv:
                return None
            out.append((pkg, adv))
    return out


pairs = advisory_pairs(data)
if pairs is None:
    print(
        "unexpected bun audit --json shape: expected a map of "
        "package -> advisory (or list of advisories, or wrapped in "
        "{'vulnerabilities': {...}}), each advisory with a "
        "'severity' key; got " + type(data).__name__ + ". Update this "
        "gate for the new shape before trusting its result.",
        file=sys.stderr,
    )
    sys.exit(UNKNOWN_SHAPE)

bad = [
    (pkg, a["severity"], a.get("url"))
    for pkg, a in pairs
    if str(a["severity"]).lower() in ("high", "critical")
]
lower = [
    (pkg, a["severity"], a.get("url"))
    for pkg, a in pairs
    if str(a["severity"]).lower() not in ("high", "critical")
]
if lower:
    print(f"noted {len(lower)} below-threshold advisory(ies); not gating on them")
print("high/critical findings:", bad if bad else "none")
sys.exit(1 if bad else 0)
PY
