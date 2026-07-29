#!/usr/bin/env bash
# Shared helpers for openfsd-client packaging.
set -euo pipefail

PKG_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${PKG_ROOT}/../.." && pwd)"
DIST_ROOT="${DIST_ROOT:-${REPO_ROOT}/dist/openfsd-client}"
APP_ID="com.openfsd.client-setup"
APP_NAME="openfsd Client Setup"
BIN_NAME="openfsd-client"
# Human product name used in installers / Start Menu.
PRODUCT_NAME="openfsd Client Setup"

resolve_version() {
  if [[ -n "${VERSION:-}" ]]; then
    echo "${VERSION#v}"
    return
  fi
  if git -C "${REPO_ROOT}" describe --tags --always --dirty 2>/dev/null | grep -q .; then
    git -C "${REPO_ROOT}" describe --tags --always --dirty | sed 's/^v//'
    return
  fi
  echo "0.0.0-dev"
}

VERSION="$(resolve_version)"
export VERSION PKG_ROOT REPO_ROOT DIST_ROOT APP_ID APP_NAME BIN_NAME PRODUCT_NAME

mkdir -p "${DIST_ROOT}"

ensure_icons() {
  local assets="${PKG_ROOT}/assets"
  mkdir -p "${assets}"
  if [[ ! -f "${assets}/icon-512.png" ]]; then
    if command -v rsvg-convert >/dev/null 2>&1; then
      rsvg-convert -w 512 -h 512 "${assets}/icon.svg" -o "${assets}/icon-512.png"
    elif command -v magick >/dev/null 2>&1; then
      magick -background none "${assets}/icon.svg" -resize 512x512 "${assets}/icon-512.png"
    elif command -v convert >/dev/null 2>&1; then
      convert -background none "${assets}/icon.svg" -resize 512x512 "${assets}/icon-512.png"
    else
      echo "warn: no SVG converter; skipping PNG icon" >&2
      return 0
    fi
  fi
  # Standard PNG sizes for Linux / intermediate use.
  for s in 16 32 48 64 128 256; do
    if [[ ! -f "${assets}/icon-${s}.png" ]] && [[ -f "${assets}/icon-512.png" ]]; then
      if command -v magick >/dev/null 2>&1; then
        magick "${assets}/icon-512.png" -resize "${s}x${s}" "${assets}/icon-${s}.png"
      elif command -v convert >/dev/null 2>&1; then
        convert "${assets}/icon-512.png" -resize "${s}x${s}" "${assets}/icon-${s}.png"
      fi
    fi
  done
}

build_binary() {
  local out="$1"
  local goos="${2:-$(go env GOOS)}"
  local goarch="${3:-$(go env GOARCH)}"
  echo "==> build ${BIN_NAME} (${goos}/${goarch}) -> ${out}"
  mkdir -p "$(dirname "${out}")"
  # Plain Go binary — GUI + CLI. No fyne package wrappers.
  # -s -w: smaller release binary. Windows: keep console for CLI subcommands.
  (
    cd "${REPO_ROOT}"
    CGO_ENABLED=1 GOOS="${goos}" GOARCH="${goarch}" \
      go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" \
      -o "${out}" ./cmd/openfsd-client
  )
}

write_checksums() {
  local dir="$1"
  (
    cd "${dir}"
    if command -v shasum >/dev/null 2>&1; then
      shasum -a 256 ./* > SHA256SUMS 2>/dev/null || true
    elif command -v sha256sum >/dev/null 2>&1; then
      sha256sum ./* > SHA256SUMS 2>/dev/null || true
    fi
  )
}
