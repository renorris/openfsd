#!/usr/bin/env bash
# Fail if overall statement coverage (excluding cmd/) is below the floor.
# Also enforces pure-package hard floors (see AGENTS.md §8).
#
# Usage: scripts/check-coverage.sh [overall_floor_percent]
# Default overall floor: 80
#
# Hard floors (fail CI):
#   pkg/protocol ≥98, pkg/twrfiles ≥98, pkg/afvprotocol ≥98, internal/geo ≥98,
#   internal/auth ≥95, internal/postoffice ≥90, internal/sweatbox ≥95,
#   internal/cluster ≥90, internal/clientinject/cilus ≥98,
#   internal/clientinject/vpilotconfig ≥98
# Soft / reported only:
#   internal/web ≥80, internal/afv ≥80 (P0), overall aspirational 90
set -euo pipefail

FLOOR="${1:-80}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

COVER_OUT="$(mktemp)"
trap 'rm -f "$COVER_OUT"' EXIT

# shellcheck disable=SC2046
go test -coverprofile="$COVER_OUT" $(go list ./... | grep -v '/cmd/')

TOTAL_LINE="$(go tool cover -func="$COVER_OUT" | tail -1)"
echo "coverage total line: $TOTAL_LINE"

# Statement-weighted package + overall floors from cover.out
python3 - "$COVER_OUT" "$FLOOR" <<'PY'
import re, sys
from collections import defaultdict

cover_out, overall_floor = sys.argv[1], float(sys.argv[2])
stmts = defaultdict(lambda: [0, 0])  # pkg -> [total, covered]
with open(cover_out) as f:
    next(f)
    for line in f:
        line = line.strip()
        m = re.match(r"(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)", line)
        if not m:
            continue
        path = m.group(1)
        nstmt, count = int(m.group(6)), int(m.group(7))
        if "/openfsd/" in path:
            rel = path.split("/openfsd/", 1)[1]
        else:
            rel = path
        pkg = "/".join(rel.split("/")[:-1])  # drop filename
        stmts[pkg][0] += nstmt
        if count > 0:
            stmts[pkg][1] += nstmt

hard = {
    "pkg/protocol": 98.0,
    "pkg/twrfiles": 98.0,
    "pkg/afvprotocol": 98.0,
    "internal/geo": 98.0,
    "internal/auth": 95.0,
    "internal/postoffice": 90.0,
    "internal/sweatbox": 95.0,
    "internal/cluster": 90.0,
    "internal/clientinject/cilus": 98.0,
    "internal/clientinject/vpilotconfig": 98.0,
}
soft = {
    "internal/web": 80.0,  # aspirational PE-era floor; report only
    "internal/afv": 80.0,  # soft through P0; hard 85 later
}

failed = False
print("--- package floors ---")
for pkg, floor in sorted(hard.items()):
    t, c = stmts.get(pkg, [0, 0])
    pct = 100.0 * c / t if t else 0.0
    status = "OK" if pct + 1e-9 >= floor else "FAIL"
    print(f"{status}: {pkg} {pct:.1f}% (floor {floor:.0f}%, {c}/{t})")
    if status == "FAIL":
        failed = True

for pkg, floor in sorted(soft.items()):
    t, c = stmts.get(pkg, [0, 0])
    pct = 100.0 * c / t if t else 0.0
    note = "meets" if pct + 1e-9 >= floor else "below (soft)"
    print(f"SOFT: {pkg} {pct:.1f}% (aspirational {floor:.0f}%, {note}, {c}/{t})")

total_t = sum(v[0] for v in stmts.values())
total_c = sum(v[1] for v in stmts.values())
overall = 100.0 * total_c / total_t if total_t else 0.0
print("--- overall ---")
print(f"overall {overall:.1f}% (floor {overall_floor:.0f}%, aspirational 90%)")
if overall + 1e-9 < overall_floor:
    print(f"FAIL: coverage {overall:.1f}% < floor {overall_floor:.0f}%", file=sys.stderr)
    failed = True
else:
    print(f"OK: coverage {overall:.1f}% >= floor {overall_floor:.0f}%")

if failed:
    sys.exit(1)
PY
