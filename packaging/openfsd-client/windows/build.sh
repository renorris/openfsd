#!/usr/bin/env bash
# Build Windows installer (Inno Setup) for openfsd-client.
# Run on Windows (Git Bash / MSYS) or cross-prep binary on Windows runner.
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=../common.sh
source "${SCRIPT_DIR}/../common.sh"

GOARCH="${GOARCH:-amd64}"
BIN_DIR="${DIST_ROOT}/bin/windows-${GOARCH}"
BIN_PATH="${BIN_DIR}/${BIN_NAME}.exe"
ensure_icons

# Icon for Inno Setup (.ico)
ICO="${PKG_ROOT}/assets/icon.ico"
if [[ ! -f "${ICO}" ]] && [[ -f "${PKG_ROOT}/assets/icon-256.png" ]]; then
  if command -v magick >/dev/null 2>&1; then
    magick "${PKG_ROOT}/assets/icon-16.png" "${PKG_ROOT}/assets/icon-32.png" \
      "${PKG_ROOT}/assets/icon-48.png" "${PKG_ROOT}/assets/icon-256.png" "${ICO}"
  elif command -v convert >/dev/null 2>&1; then
    convert "${PKG_ROOT}/assets/icon-16.png" "${PKG_ROOT}/assets/icon-32.png" \
      "${PKG_ROOT}/assets/icon-48.png" "${PKG_ROOT}/assets/icon-256.png" "${ICO}"
  fi
fi
if [[ ! -f "${ICO}" ]]; then
  # Placeholder empty ico not allowed — generate minimal via PowerShell if present.
  if command -v powershell.exe >/dev/null 2>&1 && [[ -f "${PKG_ROOT}/assets/icon-256.png" ]]; then
    powershell.exe -NoProfile -Command "
      Add-Type -AssemblyName System.Drawing
      \$img = [System.Drawing.Image]::FromFile('$(cygpath -w "${PKG_ROOT}/assets/icon-256.png" 2>/dev/null || echo "${PKG_ROOT}/assets/icon-256.png")')
      \$icon = [System.Drawing.Icon]::FromHandle((New-Object System.Drawing.Bitmap \$img).GetHicon())
      \$fs = [IO.File]::Create('$(cygpath -w "${ICO}" 2>/dev/null || echo "${ICO}")')
      \$icon.Save(\$fs); \$fs.Close()
    " || true
  fi
fi

build_binary "${BIN_PATH}" windows "${GOARCH}"

ISCC="${ISCC:-}"
if [[ -z "${ISCC}" ]]; then
  for c in \
    "/c/Program Files (x86)/Inno Setup 6/ISCC.exe" \
    "/c/Program Files/Inno Setup 6/ISCC.exe" \
    "C:/Program Files (x86)/Inno Setup 6/ISCC.exe" \
    "C:/Program Files/Inno Setup 6/ISCC.exe" \
    "iscc" "ISCC.exe"; do
    if command -v "${c}" >/dev/null 2>&1 || [[ -x "${c}" ]] || [[ -f "${c}" ]]; then
      ISCC="${c}"
      break
    fi
  done
fi
if [[ -z "${ISCC}" ]] || { [[ ! -f "${ISCC}" ]] && ! command -v "${ISCC}" >/dev/null 2>&1; }; then
  echo "error: Inno Setup 6 ISCC not found. Install from https://jrsoftware.org/isinfo.php" >&2
  exit 1
fi

ISS="${SCRIPT_DIR}/openfsd-client.iss"
# VersionInfoVersion must be numeric a.b.c.d
VI_VERSION="$(echo "${VERSION}" | grep -oE '^[0-9]+(\.[0-9]+){0,3}' || true)"
if [[ -z "${VI_VERSION}" ]]; then
  VI_VERSION="0.0.0.0"
fi
# Pad to 4 components
while [[ "$(echo "${VI_VERSION}" | tr -cd '.' | wc -c)" -lt 3 ]]; do
  VI_VERSION="${VI_VERSION}.0"
done

if [[ ! -f "${ICO}" ]] && [[ -f "${PKG_ROOT}/assets/icon-512.png" ]] && command -v magick >/dev/null 2>&1; then
  magick "${PKG_ROOT}/assets/icon-512.png" -define icon:auto-resize=256,128,64,48,32,16 "${ICO}"
fi

# ISCC is a native Windows tool: all paths must be Windows-style, not /d/a/...
# Otherwise ISCC treats path segments as extra script filenames.
winpath() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$1"
  else
    # Git Bash
    if command -v cygpath.exe >/dev/null 2>&1; then
      cygpath.exe -w "$1"
    else
      echo "$1" | sed -e 's|^/\([a-zA-Z]\)/|\1:\\|' -e 's|/|\\|g'
    fi
  fi
}

BIN_WIN="$(winpath "${BIN_PATH}")"
DIST_WIN="$(winpath "${DIST_ROOT}")"
ISS_WIN="$(winpath "${ISS}")"
ISCC_WIN="$(winpath "${ISCC}")"

echo "==> Inno Setup ${VERSION} (VersionInfo ${VI_VERSION})"
echo "    ISCC=${ISCC_WIN}"
echo "    script=${ISS_WIN}"
echo "    SourceBin=${BIN_WIN}"
echo "    OutputDir=${DIST_WIN}"

# Quote defines so paths with spaces are one argument; pass script last.
ISCC_CMD=(
  "${ISCC_WIN}"
  "/DMyAppVersion=${VERSION}"
  "/DMyAppVersionInfo=${VI_VERSION}"
  "/DSourceBin=${BIN_WIN}"
  "/DOutputDir=${DIST_WIN}"
)
if [[ -f "${ICO}" ]]; then
  ICO_WIN="$(winpath "${ICO}")"
  ISCC_CMD+=("/DSetupIcon=${ICO_WIN}")
fi
ISCC_CMD+=("${ISS_WIN}")

# Run via cmd.exe so quoting matches Windows ISCC expectations under msys2.
cmd.exe //C "$(printf '%q ' "${ISCC_CMD[@]}")" 2>/dev/null || \
  "${ISCC_CMD[@]}"

# Also ship portable zip of the bare binary for power users.
PORTABLE="${DIST_ROOT}/${BIN_NAME}-${VERSION}-windows-${GOARCH}-portable.zip"
if command -v zip >/dev/null 2>&1; then
  (cd "${BIN_DIR}" && zip -9 -j "${PORTABLE}" "${BIN_NAME}.exe")
elif command -v powershell.exe >/dev/null 2>&1; then
  powershell.exe -NoProfile -Command "Compress-Archive -Force -Path '$(cygpath -w "${BIN_PATH}" 2>/dev/null || echo "${BIN_PATH}")' -DestinationPath '$(cygpath -w "${PORTABLE}" 2>/dev/null || echo "${PORTABLE}")'"
fi

echo "==> Windows artifacts in ${DIST_ROOT}"
ls -la "${DIST_ROOT}"/*windows* 2>/dev/null || ls -la "${DIST_ROOT}"
