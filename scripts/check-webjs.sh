#!/usr/bin/env bash
# Run Node unit tests for pure first-party web modules (airport-editor parse/format).
# Sources live under internal/web/static/; tests under webjs/ (outside go:embed).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/webjs"

if ! command -v node >/dev/null 2>&1; then
  echo "check-webjs: node not found (need Node >= 20)" >&2
  exit 1
fi

# engines.node is ">=20"; fail early with a clear message.
NODE_MAJOR="$(node -p "process.versions.node.split('.')[0]")"
if [[ "$NODE_MAJOR" -lt 20 ]]; then
  echo "check-webjs: Node >= 20 required (found $(node --version))" >&2
  exit 1
fi

echo "check-webjs: node $(node --version)"

# Portable: expand test files without bash mapfile/globstar.
TESTS=()
while IFS= read -r f; do
  TESTS+=("$f")
done < <(find ./airport-editor -name '*.test.js' | LC_ALL=C sort)

if [[ ${#TESTS[@]} -eq 0 ]]; then
  echo "check-webjs: no tests found under webjs/airport-editor" >&2
  exit 1
fi

node --test "${TESTS[@]}"
echo "check-webjs: ok (${#TESTS[@]} files)"
