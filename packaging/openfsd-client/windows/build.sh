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

# ISCC is a native Windows tool. Use mixed paths (D:/a/...) not backslashes:
# bash double-quotes turn \a in D:\a\... into an escape, which corrupts /D args
# and ISCC reports "more than one script filename".
winpath() {
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -m "$1"
  elif command -v cygpath.exe >/dev/null 2>&1; then
    cygpath.exe -m "$1"
  else
    echo "$1" | sed -e 's|^/\([a-zA-Z]\)/|\1:/|' -e 's|\\|/|g'
  fi
}

BIN_WIN="$(winpath "${BIN_PATH}")"
DIST_WIN="$(winpath "${DIST_ROOT}")"
ISS_WIN="$(winpath "${ISS}")"
# Keep ISCC as the env path; call through cmd if needed.
ISCC_EXE="${ISCC}"

echo "==> Inno Setup ${VERSION} (VersionInfo ${VI_VERSION})"
echo "    ISCC=${ISCC_EXE}"
echo "    script=${ISS_WIN}"
echo "    SourceBin=${BIN_WIN}"
echo "    OutputDir=${DIST_WIN}"

# Build a response-style arg list without bash backslash escapes.
# Prefer cmd.exe /c with a carefully quoted line (spaces in Program Files).
run_iscc() {
  local icon_def=""
  if [[ -f "${ICO}" ]]; then
    icon_def="/DSetupIcon=$(winpath "${ICO}")"
  fi
  # MSYS2_ARG_CONV_EXCL prevents msys from rewriting /D* as paths.
  MSYS2_ARG_CONV_EXCL='/D*' \
    "${ISCC_EXE}" \
    "/DMyAppVersion=${VERSION}" \
    "/DMyAppVersionInfo=${VI_VERSION}" \
    "/DSourceBin=${BIN_WIN}" \
    "/DOutputDir=${DIST_WIN}" \
    ${icon_def:+"${icon_def}"} \
    "${ISS_WIN}"
}

run_iscc

# Also ship portable zip of the bare binary for power users.
PORTABLE="${DIST_ROOT}/${BIN_NAME}-${VERSION}-windows-${GOARCH}-portable.zip"
if command -v zip >/dev/null 2>&1; then
  (cd "${BIN_DIR}" && zip -9 -j "${PORTABLE}" "${BIN_NAME}.exe")
elif command -v powershell.exe >/dev/null 2>&1; then
  powershell.exe -NoProfile -Command "Compress-Archive -Force -Path '$(cygpath -w "${BIN_PATH}" 2>/dev/null || echo "${BIN_PATH}")' -DestinationPath '$(cygpath -w "${PORTABLE}" 2>/dev/null || echo "${PORTABLE}")'"
fi

echo "==> Windows artifacts in ${DIST_ROOT}"
ls -la "${DIST_ROOT}"/*windows* 2>/dev/null || ls -la "${DIST_ROOT}"
