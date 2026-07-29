#!/usr/bin/env bash
# Optional: compare third_party/client-profiles mirror vs embed tree.
# Schema version drift is allowed with a note (mirror may lag at v1 while
# embed is schema_version 2). Fails only when the same basenames differ in
# client_id / primary_binary sha1 (when both sides parse those fields).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

EMBED="internal/clientinject/profiles"
MIRROR="third_party/client-profiles"

if [[ ! -d "$EMBED" ]]; then
  echo "skip: $EMBED missing"
  exit 0
fi
if [[ ! -d "$MIRROR" ]]; then
  echo "skip: $MIRROR missing"
  exit 0
fi

echo "==> Client profile sync check (embed ↔ third_party mirror)"
failed=0
notes=0

extract_field() {
  local file="$1" field="$2"
  # shellcheck disable=SC2002
  grep -E "^${field}:" "$file" 2>/dev/null | head -1 | sed -E "s/^${field}:[[:space:]]*//" | tr -d '"' || true
}

for emb in "$EMBED"/*.yaml "$EMBED"/*.yml; do
  [[ -f "$emb" ]] || continue
  base="$(basename "$emb")"
  mir="$MIRROR/$base"
  if [[ ! -f "$mir" ]]; then
    echo "  NOTE: embed $base has no third_party mirror"
    notes=$((notes + 1))
    continue
  fi
  es="$(extract_field "$emb" schema_version)"
  ms="$(extract_field "$mir" schema_version)"
  if [[ -z "$ms" ]]; then
    ms="$(extract_field "$mir" profile_version)"
  fi
  if [[ -n "$es" && -n "$ms" && "$es" != "$ms" ]]; then
    echo "  NOTE: $base schema drift embed=$es mirror=$ms (allowed; embed is canonical)"
    notes=$((notes + 1))
  fi
  ec="$(extract_field "$emb" client_id)"
  mc="$(extract_field "$mir" client_id)"
  if [[ -n "$ec" && -n "$mc" && "$ec" != "$mc" ]]; then
    echo "  FAIL: $base client_id embed=$ec mirror=$mc"
    failed=1
  fi
  # Prefer the primary_binary sha1 (not installer sha1 under installer:)
  esha_pe="$(awk '/^primary_binary:/{p=1} p&&/sha1:/{print $2; exit}' "$emb" | tr -d '"' || true)"
  msha_pe="$(awk '/^primary_binary:/{p=1} p&&/sha1:/{print $2; exit}' "$mir" | tr -d '"' || true)"
  if [[ -n "$esha_pe" && -n "$msha_pe" && "$esha_pe" != "$msha_pe" ]]; then
    echo "  FAIL: $base primary_binary.sha1 embed=$esha_pe mirror=$msha_pe"
    failed=1
  fi
  echo "  OK $base (client_id match; schema notes above if any)"
done

if [[ "$failed" -ne 0 ]]; then
  echo "Client profile sync FAILED"
  exit 1
fi
echo "Client profile sync passed (notes=$notes)"
exit 0
