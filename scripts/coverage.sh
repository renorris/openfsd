#!/usr/bin/env bash
# Soft coverage report for openfsd.
# Fails only if tests fail — never fails on coverage floor (floors are phased; see AGENTS.md §8).
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

  # List packages that exist under pkg/ and internal/ for floor tracking later
  echo "    target packages present:"
  found_target=0
  for p in \
    "${MODULE}/pkg/protocol" \
    "${MODULE}/pkg/fsdclient" \
    "${MODULE}/internal/geo" \
    "${MODULE}/internal/auth" \
    "${MODULE}/internal/postoffice" \
    "${MODULE}/internal/session" \
    "${MODULE}/internal/metar" \
    "${MODULE}/internal/server" \
    "${MODULE}/internal/db" \
    "${MODULE}/internal/web"
  do
    if go list "$p" >/dev/null 2>&1; then
      echo "      - $p"
      found_target=1
    fi
  done
  if [[ "$found_target" -eq 0 ]]; then
    echo "      (none yet — legacy fsd/db/web; floors soft until package split)"
  fi
fi

echo
echo "Coverage report complete (soft — no floor enforced yet; cmd/* soft-excluded from summary)."
echo "See AGENTS.md §8. Race detection: run go test -race ./... separately (CI Test step)."
exit 0
