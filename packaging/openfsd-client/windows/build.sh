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

# ISCC is a native Windows tool. Calling it from MSYS2 bash with multiple
# /Dname=value args is fragile (path conversion + quoting → "more than one
# script filename"). Bake defines into a one-shot wrapper .iss and invoke
# ISCC with only that single script path via PowerShell (no MSYS argv munging).
winpath_m() {
  # Mixed path: D:/a/... (safe in .iss double-quoted strings)
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -m "$1"
  elif command -v cygpath.exe >/dev/null 2>&1; then
    cygpath.exe -m "$1"
  else
    echo "$1" | sed -e 's|^/\([a-zA-Z]\)/|\1:/|' -e 's|\\|/|g'
  fi
}

winpath_w() {
  # Windows path: D:\a\... (for PowerShell literal paths)
  if command -v cygpath >/dev/null 2>&1; then
    cygpath -w "$1"
  elif command -v cygpath.exe >/dev/null 2>&1; then
    cygpath.exe -w "$1"
  else
    winpath_m "$1" | sed 's|/|\\|g'
  fi
}

BIN_WIN="$(winpath_m "${BIN_PATH}")"
DIST_WIN="$(winpath_m "${DIST_ROOT}")"
ISS_WIN="$(winpath_m "${ISS}")"
ISCC_EXE="${ISCC}"

echo "==> Inno Setup ${VERSION} (VersionInfo ${VI_VERSION})"
echo "    ISCC=${ISCC_EXE}"
echo "    script=${ISS_WIN}"
echo "    SourceBin=${BIN_WIN}"
echo "    OutputDir=${DIST_WIN}"

run_iscc() {
  local wrap wrap_m iscc_w wrap_w
  # Same directory as openfsd-client.iss so relative paths (LicenseFile, etc.) resolve.
  wrap="${SCRIPT_DIR}/_iscc_wrapper.iss"
  # iss double-quoted strings: escape " and \ only
  iss_escape() {
    printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'
  }
  {
    printf '#define MyAppVersion "%s"\n' "$(iss_escape "${VERSION}")"
    printf '#define MyAppVersionInfo "%s"\n' "$(iss_escape "${VI_VERSION}")"
    printf '#define SourceBin "%s"\n' "$(iss_escape "${BIN_WIN}")"
    printf '#define OutputDir "%s"\n' "$(iss_escape "${DIST_WIN}")"
    if [[ -f "${ICO}" ]]; then
      printf '#define SetupIcon "%s"\n' "$(iss_escape "$(winpath_m "${ICO}")")"
    fi
    # Full copy (not #include): relative paths like LicenseFile stay rooted at SCRIPT_DIR.
    cat "${ISS}"
  } >"${wrap}"
  # shellcheck disable=SC2064
  trap 'rm -f "${wrap}"' RETURN

  wrap_m="$(winpath_m "${wrap}")"
  echo "    wrapper=${wrap_m}"

  iscc_w="$(winpath_w "${ISCC_EXE}")"
  wrap_w="$(winpath_w "${wrap}")"

  if command -v powershell.exe >/dev/null 2>&1; then
    # Single-quoted PowerShell literals — no expansion, no MSYS /D conversion.
    powershell.exe -NoProfile -Command \
      "& '$(printf '%s' "${iscc_w}" | sed "s/'/''/g")' '$(printf '%s' "${wrap_w}" | sed "s/'/''/g")'"
  else
    # Fallback: still only one script arg (no /D flags).
    MSYS2_ARG_CONV_EXCL='*' "${ISCC_EXE}" "${wrap_m}"
  fi
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
