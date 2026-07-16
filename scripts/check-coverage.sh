#!/usr/bin/env bash
# Fail if overall statement coverage (excluding cmd/) is below the floor.
# Usage: scripts/check-coverage.sh [floor_percent]
# Default floor: 80
set -euo pipefail

FLOOR="${1:-80}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

COVER_OUT="$(mktemp)"
trap 'rm -f "$COVER_OUT"' EXIT

# shellcheck disable=SC2046
go test -coverprofile="$COVER_OUT" $(go list ./... | grep -v '/cmd/')

TOTAL_LINE="$(go tool cover -func="$COVER_OUT" | tail -1)"
# e.g. total: (statements) 80.6%
PCT="$(echo "$TOTAL_LINE" | awk '{print $NF}' | tr -d '%')"

echo "coverage total: ${PCT}% (floor ${FLOOR}%)"
echo "$TOTAL_LINE"

# awk comparison handles floats
awk -v pct="$PCT" -v floor="$FLOOR" 'BEGIN {
  if (pct+0 < floor+0) {
    printf("FAIL: coverage %.1f%% < floor %s%%\n", pct, floor) > "/dev/stderr"
    exit 1
  }
  printf("OK: coverage %.1f%% >= floor %s%%\n", pct, floor)
}'
