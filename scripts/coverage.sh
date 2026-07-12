#!/usr/bin/env bash
# Soft coverage report for openfsd.
# Fails only if tests fail — never fails on coverage floor (floors are phased; see AGENTS.md).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUT="${COVER_OUT:-cover.out}"

echo "==> go test -race -coverprofile=${OUT} ./..."
go test -race -coverprofile="${OUT}" ./...

echo
echo "==> go tool cover -func=${OUT}"
go tool cover -func="${OUT}"

echo
echo "Coverage report complete (soft — no floor enforced yet). See AGENTS.md §8."
exit 0
