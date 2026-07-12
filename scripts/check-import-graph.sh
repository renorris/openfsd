#!/usr/bin/env bash
# Forbidden import-edge checks for openfsd (AGENTS.md §2).
# Documents and enforces edges when packages exist; no-op success if absent.
# A Go TestImportGraph may replace or supplement this once packages land.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

MODULE="$(go list -m -f '{{.Path}}' 2>/dev/null || echo "github.com/renorris/openfsd")"
failed=0

# Return 0 if package path exists in the module build list.
pkg_exists() {
  local import_path="$1"
  go list "$import_path" >/dev/null 2>&1
}

# Fail if importer's transitive/direct imports include any forbidden path prefix.
# Uses go list -f '{{.Imports}}' on the package.
check_no_imports() {
  local from="$1"
  shift
  local forbidden=("$@")

  if ! pkg_exists "$from"; then
    echo "    skip $from (not present yet)"
    return 0
  fi

  local imports
  imports="$(go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' "$from" 2>/dev/null || true)"
  # Also check test imports lightly via deps of the package only (direct Imports).
  local hit=0
  local imp
  for imp in $imports; do
    for bad in "${forbidden[@]}"; do
      case "$imp" in
        "$bad"|"$bad"/*)
          echo "    FAIL: $from imports $imp (forbidden: $bad)"
          hit=1
          failed=1
          ;;
      esac
    done
  done
  if [[ "$hit" -eq 0 ]]; then
    echo "    OK $from"
  fi
}

echo "==> Import graph: forbidden edges (AGENTS.md §2)"
echo "    module: $MODULE"

# pkg/protocol — anything in module except stdlib
if pkg_exists "${MODULE}/pkg/protocol"; then
  echo "    checking pkg/protocol is free of module-internal imports..."
  proto_imports="$(go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' "${MODULE}/pkg/protocol" 2>/dev/null || true)"
  hit=0
  for imp in $proto_imports; do
    case "$imp" in
      "${MODULE}"|"${MODULE}"/*)
        echo "    FAIL: pkg/protocol imports $imp (must be stdlib only within module)"
        hit=1
        failed=1
        ;;
    esac
  done
  if [[ "$hit" -eq 0 ]]; then
    echo "    OK ${MODULE}/pkg/protocol"
  fi
else
  echo "    skip ${MODULE}/pkg/protocol (not present yet)"
fi

# pkg/fsdclient — must not import internal/*
check_no_imports "${MODULE}/pkg/fsdclient" \
  "${MODULE}/internal"

# internal/session — must not import postoffice, server, web, metar
check_no_imports "${MODULE}/internal/session" \
  "${MODULE}/internal/postoffice" \
  "${MODULE}/internal/server" \
  "${MODULE}/internal/web" \
  "${MODULE}/internal/metar"

# internal/geo — anything openfsd except stdlib
if pkg_exists "${MODULE}/internal/geo"; then
  echo "    checking internal/geo is free of module-internal imports..."
  geo_imports="$(go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' "${MODULE}/internal/geo" 2>/dev/null || true)"
  hit=0
  for imp in $geo_imports; do
    case "$imp" in
      "${MODULE}"|"${MODULE}"/*)
        echo "    FAIL: internal/geo imports $imp (must be stdlib only within module)"
        hit=1
        failed=1
        ;;
    esac
  done
  if [[ "$hit" -eq 0 ]]; then
    echo "    OK ${MODULE}/internal/geo"
  fi
else
  echo "    skip ${MODULE}/internal/geo (not present yet)"
fi

# internal/web — must not import server, session, postoffice, metar
check_no_imports "${MODULE}/internal/web" \
  "${MODULE}/internal/server" \
  "${MODULE}/internal/session" \
  "${MODULE}/internal/postoffice" \
  "${MODULE}/internal/metar"

# internal/db — must not import server, session, web, fsdclient
check_no_imports "${MODULE}/internal/db" \
  "${MODULE}/internal/server" \
  "${MODULE}/internal/session" \
  "${MODULE}/internal/web" \
  "${MODULE}/pkg/fsdclient"

# internal/auth — must not import server, session, web
check_no_imports "${MODULE}/internal/auth" \
  "${MODULE}/internal/server" \
  "${MODULE}/internal/session" \
  "${MODULE}/internal/web"

if [[ "$failed" -ne 0 ]]; then
  echo
  echo "Import graph check FAILED. See AGENTS.md §2."
  exit 1
fi

echo
echo "Import graph check passed (missing packages skipped)."
exit 0
