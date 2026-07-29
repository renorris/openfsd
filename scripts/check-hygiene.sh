#!/usr/bin/env bash
# Hygiene greps for openfsd (AGENTS.md §10).
# Scans non-test .go under pkg/ and internal/.
#
# Limitation: greps can still match string literals (false positives).
# Pure // and * block-comment lines are skipped. Prefer fixing real call sites;
# if a string false positive is unavoidable, restructure the string — do not
# weaken patterns casually.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

failed=0

# Collect non-test Go files under a directory if it exists.
list_non_test_go() {
  local dir="$1"
  if [[ ! -d "$dir" ]]; then
    return 0
  fi
  find "$dir" -name '*.go' ! -name '*_test.go' -print 2>/dev/null || true
}

# Read file paths from stdin; grep pattern; skip pure // and * comment lines.
# Prints hits; returns 0 if clean, 1 if any match (also sets failed=1).
# Note: pipelines must tolerate zero matches under set -o pipefail.
scan_files() {
  local pattern="$1"
  local any=0
  local f hits
  while IFS= read -r f; do
    [[ -z "$f" || ! -f "$f" ]] && continue
    hits="$(grep -nE "$pattern" "$f" 2>/dev/null | grep -vE '^[0-9]+:[[:space:]]*//' | grep -vE '^[0-9]+:[[:space:]]*\*' || true)"
    if [[ -n "$hits" ]]; then
      while IFS= read -r line; do
        [[ -z "$line" ]] && continue
        echo "${f}:${line}"
      done <<<"$hits"
      any=1
      failed=1
    fi
  done
  if [[ "$any" -ne 0 ]]; then
    return 1
  fi
  return 0
}

files="$( { list_non_test_go pkg; list_non_test_go internal; } | grep -v '^$' || true)"

# Process substitution feeds stdin without a pipeline subshell so failed=1
# sticks in this shell. Do not use printf | scan_files (subshell loses failed).
# Manual self-check: temporary non-test panic( under internal/ → expect exit 1.

echo "==> Hygiene: panic( in non-test Go under pkg/ and internal/"
if [[ -z "$files" ]]; then
  echo "    OK (no hits, or dirs absent)"
elif scan_files '\bpanic\s*\(' < <(printf '%s\n' "$files"); then
  echo "    OK"
fi

echo "==> Hygiene: reflect usage in pkg/protocol"
if [[ ! -d pkg/protocol ]]; then
  echo "    skip (pkg/protocol missing)"
else
  proto_files="$(list_non_test_go pkg/protocol | grep -v '^$' || true)"
  if [[ -z "$proto_files" ]]; then
    echo "    OK"
  elif scan_files '"reflect"|\breflect\.' < <(printf '%s\n' "$proto_files"); then
    echo "    OK"
  fi
fi

echo "==> Hygiene: fmt.Print* / log.Print* / log.Fatal* / log.Panic* under pkg/ and internal/"
if [[ -z "$files" ]]; then
  echo "    OK (no hits, or dirs absent)"
else
  print_clean=1
  if ! scan_files '\bfmt\.Print(f|ln)?\s*\(' < <(printf '%s\n' "$files"); then
    print_clean=0
  fi
  if ! scan_files '\blog\.Print(f|ln)?\s*\(' < <(printf '%s\n' "$files"); then
    print_clean=0
  fi
  if ! scan_files '\blog\.(Fatal|Fatalf|Fatalln|Panic|Panicf|Panicln)\s*\(' < <(printf '%s\n' "$files"); then
    print_clean=0
  fi
  if [[ "$print_clean" -eq 1 ]]; then
    echo "    OK"
  fi
fi

# ---------------------------------------------------------------------------
# No PE binaries / *.exe / *.dll in git (client injector legal posture).
# Allowlist: none today. .research/ is gitignored and must not be tracked.
# ---------------------------------------------------------------------------
echo "==> Hygiene: no PE binaries / *.exe / *.dll in git"

# Fail if .research/ is somehow tracked (gitignored local RE extracts).
research_hit=0
while IFS= read -r -d '' f; do
  echo "    FAIL: .research/ must not be tracked (gitignored RE extracts only): $f"
  research_hit=1
  failed=1
done < <(git ls-files -z -- '.research' '.research/*' 2>/dev/null || true)

pe_hit=0
while IFS= read -r -d '' f; do
  [[ -z "$f" || ! -f "$f" ]] && continue
  base="$(basename "$f")"
  # Extension check (case-insensitive).
  case "${base}" in
    *.exe|*.EXE|*.dll|*.DLL|*.exe.*|*.dll.*)
      echo "    FAIL: tracked binary extension: $f"
      pe_hit=1
      failed=1
      continue
      ;;
  esac
  # Skip known text-only extensions before PE magic read (speed; legal posture
  # still catches *.exe/*.dll above and MZ on remaining paths).
  case "${base}" in
    *.go|*.md|*.txt|*.yml|*.yaml|*.json|*.xml|*.html|*.css|*.js|*.ts|*.sh|*.mod|*.sum|*.toml|*.csv|*.svg|*.gitignore|*.editorconfig|Makefile|Dockerfile*|LICENSE*|NOTICE*|*.proto|*.rhai)
      continue
      ;;
  esac
  # PE magic "MZ" at start of file (Windows PE / DOS stub).
  magic="$(dd if="$f" bs=2 count=1 2>/dev/null | LC_ALL=C od -An -tx1 | tr -d ' \n')"
  if [[ "$magic" == "4d5a" ]]; then
    echo "    FAIL: PE magic MZ at start of tracked file: $f"
    pe_hit=1
    failed=1
  fi
done < <(git ls-files -z 2>/dev/null || true)

if [[ "$pe_hit" -eq 0 && "$research_hit" -eq 0 ]]; then
  echo "    OK"
fi

if [[ "$failed" -ne 0 ]]; then
  echo
  echo "Hygiene check FAILED. See AGENTS.md (no panic/reflect/fmt.Print/log.Print in library code;"
  echo "no PE/*.exe/*.dll commits; no tracked .research/)."
  echo "Note: matches in string literals can be false positives; restructure if needed."
  exit 1
fi

echo
echo "Hygiene check passed."
exit 0
