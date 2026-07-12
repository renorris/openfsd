#!/usr/bin/env bash
# Hygiene greps for openfsd (AGENTS.md).
# Targets target-layout trees pkg/ and internal/ only.
# No-op success if those directories are missing or empty of .go files.
# Legacy fsd/ will move into these trees; do not add panics/prints in new packages.
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
  # find may return nothing; that is OK
  find "$dir" -name '*.go' ! -name '*_test.go' -print 2>/dev/null || true
}

echo "==> Hygiene: panic( in non-test Go under pkg/ and internal/"
panic_hits=""
while IFS= read -r f; do
  [[ -z "$f" ]] && continue
  if grep -nE '\bpanic\s*\(' "$f" 2>/dev/null; then
    panic_hits+="$f"$'\n'
    failed=1
  fi
done < <(list_non_test_go pkg; list_non_test_go internal)

if [[ -z "${panic_hits}" ]]; then
  echo "    OK (no hits, or dirs absent)"
fi

echo "==> Hygiene: reflect usage in pkg/protocol (when present)"
if [[ -d pkg/protocol ]]; then
  reflect_hit=0
  while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    # Match import "reflect" or reflect. usage
    if grep -nE '"reflect"|\breflect\.' "$f" 2>/dev/null; then
      reflect_hit=1
      failed=1
    fi
  done < <(list_non_test_go pkg/protocol)
  if [[ "$reflect_hit" -eq 0 ]]; then
    echo "    OK"
  fi
else
  echo "    skip (pkg/protocol not present yet)"
fi

echo "==> Hygiene: fmt.Print* in non-test Go under pkg/ and internal/"
print_hits=0
while IFS= read -r f; do
  [[ -z "$f" ]] && continue
  if grep -nE '\bfmt\.Print(f|ln)?\s*\(' "$f" 2>/dev/null; then
    print_hits=1
    failed=1
  fi
done < <(list_non_test_go pkg; list_non_test_go internal)

if [[ "$print_hits" -eq 0 ]]; then
  echo "    OK (no hits, or dirs absent)"
fi

if [[ "$failed" -ne 0 ]]; then
  echo
  echo "Hygiene check FAILED. See AGENTS.md (no panic/reflect/fmt.Print in library code)."
  exit 1
fi

echo
echo "Hygiene check passed."
exit 0
