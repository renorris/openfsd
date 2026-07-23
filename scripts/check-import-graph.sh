#!/usr/bin/env bash
# Forbidden import-edge checks for openfsd (AGENTS.md §2).
# Skips a pattern with success if the package tree is absent.
#
# Checks direct imports of each package under a pattern (./pkg/... style).
# Pure packages (pkg/protocol, internal/geo) must be stdlib-only:
# stdlib import paths have no '.' in the first path element (e.g. fmt, net/http).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

MODULE="$(go list -m -f '{{.Path}}' 2>/dev/null || echo "github.com/renorris/openfsd")"
failed=0

# True if at least one package matches the pattern (e.g. $MODULE/pkg/protocol/...).
pkgs_exist() {
  local pattern="$1"
  local list
  list="$(go list "$pattern" 2>/dev/null || true)"
  [[ -n "$list" ]]
}

# Stdlib heuristic: first path element contains no '.' (fmt, encoding/json, net/http).
# Rejects module paths (github.com/...), domain-qualified modules, and local module imports.
is_stdlib_import() {
  local imp="$1"
  local first="${imp%%/*}"
  [[ "$first" != *.* ]]
}

# For each package matching pattern, fail if any direct import matches a forbidden prefix.
# "from_label" is only for messages; pattern is a go list pattern (may end in /...).
check_no_imports() {
  local from_label="$1"
  local pattern="$2"
  shift 2
  local forbidden=("$@")

  if ! pkgs_exist "$pattern"; then
    echo "    skip $from_label (package missing)"
    return 0
  fi

  local pkg hit=0
  while IFS= read -r pkg; do
    [[ -z "$pkg" ]] && continue
    local imports
    imports="$(go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' "$pkg" 2>/dev/null || true)"
    local imp
    for imp in $imports; do
      for bad in "${forbidden[@]}"; do
        case "$imp" in
          "$bad"|"$bad"/*)
            echo "    FAIL: $pkg imports $imp (forbidden: $bad)"
            hit=1
            failed=1
            ;;
        esac
      done
    done
  done < <(go list "$pattern" 2>/dev/null || true)

  if [[ "$hit" -eq 0 ]]; then
    echo "    OK $from_label (direct imports under $pattern)"
  fi
}

# Fail if any package under pattern has a non-stdlib direct import.
check_stdlib_only() {
  local from_label="$1"
  local pattern="$2"

  if ! pkgs_exist "$pattern"; then
    echo "    skip $from_label (package missing)"
    return 0
  fi

  local pkg hit=0
  while IFS= read -r pkg; do
    [[ -z "$pkg" ]] && continue
    local imports
    imports="$(go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' "$pkg" 2>/dev/null || true)"
    local imp
    for imp in $imports; do
      if ! is_stdlib_import "$imp"; then
        echo "    FAIL: $pkg imports $imp (must be stdlib only; third-party and module imports forbidden)"
        hit=1
        failed=1
      fi
    done
  done < <(go list "$pattern" 2>/dev/null || true)

  if [[ "$hit" -eq 0 ]]; then
    echo "    OK $from_label (stdlib-only under $pattern)"
  fi
}

# Fail if any package under pattern has a non-stdlib import other than allowed prefixes.
# Used for internal/sweatbox: stdlib + internal/geo + pkg/twrfiles (AGENTS.md §1).
check_imports_allowlist() {
  local from_label="$1"
  local pattern="$2"
  shift 2
  local allowed=("$@")

  if ! pkgs_exist "$pattern"; then
    echo "    skip $from_label (package missing)"
    return 0
  fi

  local pkg hit=0
  while IFS= read -r pkg; do
    [[ -z "$pkg" ]] && continue
    local imports
    imports="$(go list -f '{{range .Imports}}{{.}}{{"\n"}}{{end}}' "$pkg" 2>/dev/null || true)"
    local imp
    for imp in $imports; do
      if is_stdlib_import "$imp"; then
        continue
      fi
      local ok=0
      local a
      for a in "${allowed[@]}"; do
        if [[ "$imp" == "$a" || "$imp" == "$a"/* ]]; then
          ok=1
          break
        fi
      done
      if [[ "$ok" -eq 0 ]]; then
        echo "    FAIL: $pkg imports $imp (allowed non-stdlib: ${allowed[*]})"
        hit=1
        failed=1
      fi
    done
  done < <(go list "$pattern" 2>/dev/null || true)

  if [[ "$hit" -eq 0 ]]; then
    echo "    OK $from_label (stdlib + allowlist under $pattern)"
  fi
}

echo "==> Import graph: forbidden edges (AGENTS.md §2)"
echo "    module: $MODULE"
echo "    note: checks direct imports of every package matched by each pattern"

# pkg/protocol — stdlib only (no third-party, no module-internal)
check_stdlib_only "pkg/protocol" "${MODULE}/pkg/protocol/..."

# pkg/twrfiles — stdlib only (.apt/.air wire format; no third-party, no module-internal)
check_stdlib_only "pkg/twrfiles" "${MODULE}/pkg/twrfiles/..."

# pkg/fsdclient — must not import internal/*
check_no_imports "pkg/fsdclient" "${MODULE}/pkg/fsdclient/..." \
  "${MODULE}/internal"

# internal/session — must not import postoffice, server, web, metar
check_no_imports "internal/session" "${MODULE}/internal/session/..." \
  "${MODULE}/internal/postoffice" \
  "${MODULE}/internal/server" \
  "${MODULE}/internal/web" \
  "${MODULE}/internal/metar"

# internal/geo — stdlib only
check_stdlib_only "internal/geo" "${MODULE}/internal/geo/..."

# internal/web — must not import session, postoffice, metar, sweatbox, server
# (service-HTTP DTOs live in internal/serviceapi; web talks to FSD over HTTP only)
check_no_imports "internal/web" "${MODULE}/internal/web/..." \
  "${MODULE}/internal/session" \
  "${MODULE}/internal/postoffice" \
  "${MODULE}/internal/metar" \
  "${MODULE}/internal/sweatbox" \
  "${MODULE}/internal/server"

# internal/serviceapi — pure DTOs: no orchestration packages
check_no_imports "internal/serviceapi" "${MODULE}/internal/serviceapi/..." \
  "${MODULE}/internal/server" \
  "${MODULE}/internal/session" \
  "${MODULE}/internal/postoffice" \
  "${MODULE}/internal/web" \
  "${MODULE}/internal/sweatbox" \
  "${MODULE}/internal/metar" \
  "${MODULE}/internal/db" \
  "${MODULE}/internal/auth"

# internal/db — must not import server, session, web, fsdclient
check_no_imports "internal/db" "${MODULE}/internal/db/..." \
  "${MODULE}/internal/server" \
  "${MODULE}/internal/session" \
  "${MODULE}/internal/web" \
  "${MODULE}/pkg/fsdclient"

# internal/auth — must not import server, session, web
check_no_imports "internal/auth" "${MODULE}/internal/auth/..." \
  "${MODULE}/internal/server" \
  "${MODULE}/internal/session" \
  "${MODULE}/internal/web"

# internal/sweatbox — pure sim: stdlib + internal/geo + pkg/twrfiles (AGENTS.md §1)
# protocol encode lives in server host; no third-party / other internal packages
check_imports_allowlist "internal/sweatbox" "${MODULE}/internal/sweatbox/..." \
  "${MODULE}/internal/geo" \
  "${MODULE}/pkg/twrfiles"

if [[ "$failed" -ne 0 ]]; then
  echo
  echo "Import graph check FAILED. See AGENTS.md §2."
  exit 1
fi

echo
echo "Import graph check passed."
exit 0
