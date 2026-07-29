#!/usr/bin/env bash
# Package openfsd-client for the current OS (or TARGET=windows|darwin|linux).
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=common.sh
source "${SCRIPT_DIR}/common.sh"

TARGET="${TARGET:-$(uname -s | tr '[:upper:]' '[:lower:]')}"
case "${TARGET}" in
  mingw*|msys*|cygwin*|windows)
    bash "${SCRIPT_DIR}/windows/build.sh"
    ;;
  darwin|macos|osx)
    bash "${SCRIPT_DIR}/macos/build.sh"
    ;;
  linux)
    bash "${SCRIPT_DIR}/linux/build.sh"
    ;;
  *)
    echo "usage: TARGET=windows|darwin|linux $0" >&2
    echo "unknown TARGET=${TARGET}" >&2
    exit 1
    ;;
esac

write_checksums "${DIST_ROOT}"
echo "==> done. Artifacts: ${DIST_ROOT}"
ls -la "${DIST_ROOT}"
