#!/usr/bin/env bash
# Soft coverage report for openfsd.
# Fails only if tests fail — never fails on coverage floor.
# For hard floors use: bash scripts/check-coverage.sh 80 (see AGENTS.md §8).
#
# Does NOT use -race: CI already runs `go test -race ./...` separately.
# Locally run `go test -race ./...` for the race gate; use this for cover profile only.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

OUT="${COVER_OUT:-cover.out}"
MODULE="$(go list -m -f '{{.Path}}' 2>/dev/null || echo "github.com/renorris/openfsd")"

echo "==> go test -coverprofile=${OUT} ./..."
go test -coverprofile="${OUT}" ./...

echo
echo "==> go tool cover -func=${OUT} (full tree)"
go tool cover -func="${OUT}"

# Soft summary: overall total, and in-scope total excluding cmd/ (soft-excluded from floors).
echo
echo "==> In-scope summary (excluding cmd/* from floor measurement)"
if [[ -f "${OUT}" ]]; then
  # Total line from cover -func
  total_line="$(go tool cover -func="${OUT}" | grep -E '^total:' || true)"
  echo "    full tree: ${total_line:-n/a}"

  # Filter profile lines that belong to cmd/ packages, recompute func coverage if possible.
  # cover.out mode:set lines look like: path:start.col,end.col num count
  # Package paths in profile use module-relative or full paths depending on Go version.
  filtered="$(mktemp)"
  # Keep header (mode: ...) and all non-cmd body lines
  if head -n1 "${OUT}" | grep -q '^mode:'; then
    head -n1 "${OUT}" >"${filtered}"
    # Drop lines whose file path is under /cmd/ or starts with cmd/
    tail -n +2 "${OUT}" | grep -vE '(^|/)cmd/' >>"${filtered}" || true
    in_scope_line="$(go tool cover -func="${filtered}" 2>/dev/null | grep -E '^total:' || true)"
    echo "    excluding cmd/*: ${in_scope_line:-n/a (no statements or empty)}"
    rm -f "${filtered}"
  else
    echo "    (unexpected coverprofile format; skipped filter)"
  fi

fi

echo
echo "Coverage report complete (soft — no floor enforced; use scripts/check-coverage.sh for hard floors)."
echo "See AGENTS.md §8. Race detection: run go test -race ./... separately."
exit 0
